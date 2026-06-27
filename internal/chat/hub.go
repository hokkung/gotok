// Package chat implements the real-time WebSocket chat layer with Redis
// pub/sub for horizontal scalability. Each server instance runs a Hub that
// manages local WebSocket connections. Messages are routed across instances
// via Redis pub/sub channels keyed per user.
package chat

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"

	"github.com/hokkung/gotok/internal/models"
)

// StoreInterface is the subset of the store that the chat hub needs. Keeping it
// as an interface makes the hub testable without a real database.
type StoreInterface interface {
	CreateMessage(ctx context.Context, convID, senderID int64, text string) (*models.Message, []int64, error)
	MarkRead(ctx context.Context, convID, userID, msgID int64) error
	IsParticipant(ctx context.Context, convID, userID int64) (bool, error)
}

// Envelope is the wire format for messages sent to and received from WebSocket
// clients.
type Envelope struct {
	Type           string `json:"type"`
	ConversationID int64  `json:"conversation_id,omitempty"`
	UserID         int64  `json:"user_id,omitempty"`
	MessageID      int64  `json:"message_id,omitempty"`
	Text           string `json:"text,omitempty"`

	// Populated for outgoing "message" events.
	ID         int64  `json:"id,omitempty"`
	SenderID   int64  `json:"sender_id,omitempty"`
	SenderName string `json:"sender_name,omitempty"`
	SenderAvatar string `json:"sender_avatar,omitempty"`
	CreatedAt  int64  `json:"created_at,omitempty"`

	// Populated for "presence" events.
	Online bool `json:"online,omitempty"`
}

// Client represents a single WebSocket connection.
type Client struct {
	userID int64
	send   chan []byte
}

// Hub manages local WebSocket connections and coordinates with the Redis broker
// for cross-instance message delivery.
type Hub struct {
	mu      sync.RWMutex
	clients map[int64]map[*Client]struct{} // userID → set of connections
	store   StoreInterface
	broker  Broker
	logger  *zap.Logger

	register   chan *Client
	unregister chan *Client
}

// Option configures a Hub.
type Option func(*Hub)

// WithLogger sets the hub's logger.
func WithLogger(lg *zap.Logger) Option {
	return func(h *Hub) { h.logger = lg }
}

// WithWriteBufferSize sets the per-client send buffer size (default 64).
func WithWriteBufferSize(n int) Option {
	return func(h *Hub) {
		// Applied in NewHub when creating clients.
	}
}

// NewHub creates a Hub wired to the given store and broker.
func NewHub(st StoreInterface, b Broker, opts ...Option) *Hub {
	h := &Hub{
		clients:    make(map[int64]map[*Client]struct{}),
		store:      st,
		broker:     b,
		logger:     zap.NewNop(),
		register:   make(chan *Client, 64),
		unregister: make(chan *Client, 64),
	}
	for _, opt := range opts {
		opt(h)
	}
	return h
}

// Run is the hub's event loop. It must be started as a goroutine and respects
// the provided context for graceful shutdown.
func (h *Hub) Run(ctx context.Context) {
	for {
		select {
		case c := <-h.register:
			h.addClient(ctx, c)
		case c := <-h.unregister:
			h.removeClient(ctx, c)
		case <-ctx.Done():
			h.closeAll()
			return
		}
	}
}

// Register adds a new client connection to the hub.
func (h *Hub) Register(c *Client) {
	h.register <- c
}

// Unregister removes a client connection from the hub.
func (h *Hub) Unregister(c *Client) {
	h.unregister <- c
}

// NewClient creates a Client for the given user.
func (h *Hub) NewClient(userID int64) *Client {
	return &Client{
		userID: userID,
		send:   make(chan []byte, 64),
	}
}

// Send returns the send channel for writing to this client's WebSocket.
func (c *Client) Send() <-chan []byte { return c.send }

// UserID returns the authenticated user id for this client.
func (c *Client) UserID() int64 { return c.userID }

// addClient registers a client locally and subscribes to the user's Redis
// channel on first connection.
func (h *Hub) addClient(ctx context.Context, c *Client) {
	h.mu.Lock()
	first := len(h.clients[c.userID]) == 0
	if h.clients[c.userID] == nil {
		h.clients[c.userID] = make(map[*Client]struct{})
	}
	h.clients[c.userID][c] = struct{}{}
	h.mu.Unlock()

	if first {
		// Subscribe to Redis for this user's messages.
		go func() {
			if err := h.broker.Subscribe(ctx, c.userID, func(payload []byte) {
				h.deliverLocal(c.userID, payload)
			}); err != nil && ctx.Err() == nil {
				h.logger.Error("redis subscribe", zap.Int64("user_id", c.userID), zap.Error(err))
			}
		}()
		// Set presence.
		if err := h.broker.SetPresence(ctx, c.userID, true); err != nil && ctx.Err() == nil {
			h.logger.Error("set presence", zap.Int64("user_id", c.userID), zap.Error(err))
		}
	}
}

// removeClient unregisters a client locally and unsubscribes from Redis when
// the last connection for that user disconnects.
func (h *Hub) removeClient(ctx context.Context, c *Client) {
	h.mu.Lock()
	conns := h.clients[c.userID]
	delete(conns, c)
	last := len(conns) == 0
	if last {
		delete(h.clients, c.userID)
	}
	h.mu.Unlock()

	close(c.send)

	if last {
		// Use a background context so unsubscribe/presence-clear still works
		// during shutdown after the parent ctx is cancelled.
		bgCtx := context.Background()
		if err := h.broker.Unsubscribe(bgCtx, c.userID); err != nil {
			h.logger.Error("redis unsubscribe", zap.Int64("user_id", c.userID), zap.Error(err))
		}
		if err := h.broker.SetPresence(bgCtx, c.userID, false); err != nil {
			h.logger.Error("clear presence", zap.Int64("user_id", c.userID), zap.Error(err))
		}
	}
}

// deliverLocal sends a payload to all local connections for the given user.
func (h *Hub) deliverLocal(userID int64, payload []byte) {
	h.mu.RLock()
	conns := h.clients[userID]
	clients := make([]*Client, 0, len(conns))
	for c := range conns {
		clients = append(clients, c)
	}
	h.mu.RUnlock()

	for _, c := range clients {
		select {
		case c.send <- payload:
		default:
			// Client buffer full — drop the message. The client is likely slow
			// or disconnected; it will be cleaned up by the read pump.
		}
	}
}

// closeAll closes every client connection. Called during shutdown.
func (h *Hub) closeAll() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for userID, conns := range h.clients {
		for c := range conns {
			close(c.send)
		}
		delete(h.clients, userID)
	}
}

// HandleMessage processes an incoming "message" envelope from a WebSocket
// client: persists the message, then publishes it to all participants via
// Redis pub/sub.
func (h *Hub) HandleMessage(ctx context.Context, userID int64, env Envelope) error {
	// Verify the sender is a participant in the conversation.
	ok, err := h.store.IsParticipant(ctx, env.ConversationID, userID)
	if err != nil {
		return fmt.Errorf("check participation: %w", err)
	}
	if !ok {
		return fmt.Errorf("not a participant in conversation %d", env.ConversationID)
	}

	msg, participants, err := h.store.CreateMessage(ctx, env.ConversationID, userID, env.Text)
	if err != nil {
		return fmt.Errorf("create message: %w", err)
	}

	outgoing := Envelope{
		Type:           "message",
		ID:             msg.ID,
		ConversationID: msg.ConversationID,
		SenderID:       msg.SenderID,
		SenderName:     msg.SenderName,
		SenderAvatar:   msg.SenderAvatar,
		Text:           msg.Text,
		CreatedAt:      msg.CreatedAt,
	}
	payload, err := json.Marshal(outgoing)
	if err != nil {
		return fmt.Errorf("marshal message: %w", err)
	}

	// Publish to each participant's Redis channel (including the sender, so
	// multi-tab works). The local Redis subscriber delivers to connected
	// clients on this instance; other instances receive via their own
	// subscribers.
	for _, pid := range participants {
		if err := h.broker.Publish(ctx, pid, payload); err != nil {
			h.logger.Error("publish message",
				zap.Int64("participant_id", pid),
				zap.Int64("message_id", msg.ID),
				zap.Error(err))
		}
	}

	return nil
}

// HandleRead processes an incoming "read" envelope: marks messages as read and
// notifies the other participant(s) via Redis.
func (h *Hub) HandleRead(ctx context.Context, userID int64, env Envelope) error {
	if err := h.store.MarkRead(ctx, env.ConversationID, userID, env.MessageID); err != nil {
		return fmt.Errorf("mark read: %w", err)
	}

	receipt := Envelope{
		Type:           "read_receipt",
		ConversationID: env.ConversationID,
		UserID:         userID,
		MessageID:      env.MessageID,
	}
	payload, err := json.Marshal(receipt)
	if err != nil {
		return fmt.Errorf("marshal read receipt: %w", err)
	}

	// Notify all participants that this user read up to this message.
	participantIDs, err := h.getParticipantIDs(ctx, env.ConversationID)
	if err != nil {
		h.logger.Error("get participants for read receipt", zap.Error(err))
		return nil
	}
	for _, pid := range participantIDs {
		if pid == userID {
			continue
		}
		if err := h.broker.Publish(ctx, pid, payload); err != nil {
			h.logger.Error("publish read receipt",
				zap.Int64("participant_id", pid),
				zap.Error(err))
		}
	}
	return nil
}

// IsOnline reports whether the given user has at least one active WebSocket
// connection on any instance (tracked via Redis presence).
func (h *Hub) IsOnline(ctx context.Context, userID int64) bool {
	return h.broker.IsOnline(ctx, userID)
}

// PublishToUser sends a raw payload to a user's Redis pub/sub channel. Used by
// the REST SendMessage handler as a fallback for clients without WebSocket.
func (h *Hub) PublishToUser(ctx context.Context, userID int64, payload []byte) error {
	return h.broker.Publish(ctx, userID, payload)
}

// getParticipantIDs returns all participant user IDs for a conversation.
func (h *Hub) getParticipantIDs(ctx context.Context, convID int64) ([]int64, error) {
	type participantGetter interface {
		GetParticipants(context.Context, int64) ([]int64, error)
	}
	if pg, ok := h.store.(participantGetter); ok {
		return pg.GetParticipants(ctx, convID)
	}
	return nil, fmt.Errorf("store does not support GetParticipants")
}

// StartPresenceHeartbeat periodically refreshes the presence TTL for all
// locally connected users. Uses context.WithoutCancel so it survives individual
// request cancellations but still stops on app shutdown.
func (h *Hub) StartPresenceHeartbeat(ctx context.Context) {
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			h.mu.RLock()
			userIDs := make([]int64, 0, len(h.clients))
			for uid := range h.clients {
				userIDs = append(userIDs, uid)
			}
			h.mu.RUnlock()

			for _, uid := range userIDs {
				if err := h.broker.SetPresence(ctx, uid, true); err != nil {
					h.logger.Error("heartbeat presence", zap.Int64("user_id", uid), zap.Error(err))
				}
			}
			timer.Reset(30 * time.Second)
		}
	}
}

// ConnectRedis convenience function for tests / startup checks.
func ConnectRedis(addr string) *redis.Client {
	return redis.NewClient(&redis.Options{Addr: addr})
}
