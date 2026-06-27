package chat

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// Broker is the interface for cross-instance message delivery and presence
// tracking. The compile-time check ensures RedisBroker satisfies it.
//
//go:generate echo
type Broker interface {
	Publish(ctx context.Context, userID int64, payload []byte) error
	Subscribe(ctx context.Context, userID int64, onMsg func([]byte)) error
	Unsubscribe(ctx context.Context, userID int64) error
	SetPresence(ctx context.Context, userID int64, online bool) error
	IsOnline(ctx context.Context, userID int64) bool
}

// channelName returns the Redis pub/sub channel for a user's direct delivery.
func channelName(userID int64) string {
	return fmt.Sprintf("chat:user:%d", userID)
}

// presenceKey returns the Redis key used for online-presence tracking.
func presenceKey(userID int64) string {
	return fmt.Sprintf("presence:%d", userID)
}

// RedisBroker implements Broker using Redis pub/sub for message routing and
// Redis keys with TTL for presence.
type RedisBroker struct {
	rdb    *redis.Client
	mu     sync.Mutex
	subs   map[int64]*redis.PubSub
	logger *zap.Logger
}

var _ Broker = (*RedisBroker)(nil)

// NewRedisBroker creates a Broker backed by the given Redis client.
func NewRedisBroker(rdb *redis.Client, lg *zap.Logger) *RedisBroker {
	if lg == nil {
		lg = zap.NewNop()
	}
	return &RedisBroker{
		rdb:    rdb,
		subs:   make(map[int64]*redis.PubSub),
		logger: lg,
	}
}

// Publish sends a payload to the given user's Redis pub/sub channel. All
// instances with an active subscription for that user will receive it.
func (b *RedisBroker) Publish(ctx context.Context, userID int64, payload []byte) error {
	return b.rdb.Publish(ctx, channelName(userID), payload).Err()
}

// Subscribe creates a Redis subscription for the user and invokes onMsg for
// each incoming message. If a subscription already exists for this user, it is
// a no-op (the first subscription wins).
func (b *RedisBroker) Subscribe(ctx context.Context, userID int64, onMsg func([]byte)) error {
	b.mu.Lock()
	if _, exists := b.subs[userID]; exists {
		b.mu.Unlock()
		return nil
	}
	b.mu.Unlock()

	sub := b.rdb.Subscribe(ctx, channelName(userID))

	b.mu.Lock()
	if existing, exists := b.subs[userID]; exists {
		b.mu.Unlock()
		_ = existing.Close()
		return nil
	}
	b.subs[userID] = sub
	b.mu.Unlock()

	go func() {
		ch := sub.Channel()
		for msg := range ch {
			onMsg([]byte(msg.Payload))
		}
	}()

	return nil
}

// Unsubscribe closes the Redis subscription for the user, if one exists.
func (b *RedisBroker) Unsubscribe(ctx context.Context, userID int64) error {
	b.mu.Lock()
	sub, ok := b.subs[userID]
	if ok {
		delete(b.subs, userID)
	}
	b.mu.Unlock()

	if !ok {
		return nil
	}
	return sub.Close()
}

// SetPresence marks a user as online (sets a key with a 120s TTL) or offline
// (deletes the key). The TTL ensures crashed instances don't leave stale
// presence; the hub heartbeat refreshes it every 30s.
func (b *RedisBroker) SetPresence(ctx context.Context, userID int64, online bool) error {
	key := presenceKey(userID)
	if online {
		return b.rdb.Set(ctx, key, "1", 120*time.Second).Err()
	}
	return b.rdb.Del(ctx, key).Err()
}

// IsOnline reports whether the user has an active presence key in Redis.
func (b *RedisBroker) IsOnline(ctx context.Context, userID int64) bool {
	n, err := b.rdb.Exists(ctx, presenceKey(userID)).Result()
	if err != nil {
		return false
	}
	return n > 0
}
