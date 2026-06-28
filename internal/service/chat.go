package service

import (
	"context"
	"errors"
	"fmt"

	"github.com/hokkung/gotok/internal/models"
)

// ErrNotParticipant is returned when a user is not a participant in a
// conversation.
var ErrNotParticipant = errors.New("not a participant")

// ErrCannotDMSelf is returned when trying to create a DM with oneself.
var ErrCannotDMSelf = errors.New("cannot create a DM with yourself")

// ErrConversationInput is returned when the request lacks required fields.
var ErrConversationInput = errors.New("user_id or user_ids is required")

// ErrGroupTooSmall is returned when a group conversation has fewer than 2
// participants.
var ErrGroupTooSmall = errors.New("group conversation requires at least 2 participants")

// ChatStore is the persistence interface consumed by ChatService.
type ChatStore interface {
	ListConversations(ctx context.Context, userID, afterID, limit int64) ([]models.ConversationPreview, error)
	GetOrCreateDMConversation(ctx context.Context, userA, userB int64) (*models.Conversation, error)
	CreateGroupConversation(ctx context.Context, title string, userIDs []int64) (*models.Conversation, error)
	ListMessages(ctx context.Context, convID, beforeID, limit int64) ([]models.Message, error)
	CreateMessage(ctx context.Context, convID, senderID int64, text string) (*models.Message, []int64, error)
	IsParticipant(ctx context.Context, convID, userID int64) (bool, error)
	MarkRead(ctx context.Context, convID, userID, msgID int64) error
	GetParticipants(ctx context.Context, convID int64) ([]int64, error)
}

// PresenceChecker reports whether a user is currently online. Implemented by
// the chat broker.
type PresenceChecker interface {
	IsOnline(ctx context.Context, userID int64) bool
}

// ChatService handles conversations, messages, read receipts, and presence.
type ChatService struct {
	store    ChatStore
	presence PresenceChecker
}

// NewChatService creates a ChatService backed by the given store and presence
// checker.
func NewChatService(s ChatStore, p PresenceChecker) *ChatService {
	return &ChatService{store: s, presence: p}
}

// ListConversations returns one page of the user's conversations, enriched with
// online presence for DM counterparts, plus the next pagination cursor.
func (s *ChatService) ListConversations(ctx context.Context, userID, cursor, limit int64) ([]models.ConversationPreview, int64, error) {
	convs, err := s.store.ListConversations(ctx, userID, cursor, limit)
	if err != nil {
		return nil, 0, err
	}
	for i := range convs {
		if convs[i].OtherUserID > 0 {
			convs[i].Online = s.presence.IsOnline(ctx, convs[i].OtherUserID)
		}
	}
	var next int64
	if len(convs) > 0 {
		next = convs[len(convs)-1].ID
	}
	return convs, next, nil
}

// CreateDMConversation creates or reuses a 1-on-1 DM between the requesting
// user and the target user.
func (s *ChatService) CreateDMConversation(ctx context.Context, userID, otherUserID int64) (*models.Conversation, error) {
	if userID == otherUserID {
		return nil, ErrCannotDMSelf
	}
	conv, err := s.store.GetOrCreateDMConversation(ctx, userID, otherUserID)
	if err != nil {
		return nil, fmt.Errorf("get or create DM: %w", err)
	}
	return conv, nil
}

// CreateGroupConversation creates a new group conversation with the given title
// and participant list. The creator is implicitly the first participant.
func (s *ChatService) CreateGroupConversation(ctx context.Context, userID int64, userIDs []int64, title string) (*models.Conversation, error) {
	if len(userIDs) < 2 {
		return nil, ErrGroupTooSmall
	}
	allIDs := append([]int64{userID}, userIDs...)
	conv, err := s.store.CreateGroupConversation(ctx, title, allIDs)
	if err != nil {
		return nil, fmt.Errorf("create group conversation: %w", err)
	}
	return conv, nil
}

// ListMessages returns one page of messages for a conversation. The caller must
// be a participant.
func (s *ChatService) ListMessages(ctx context.Context, userID, convID, before, limit int64) ([]models.Message, int64, error) {
	if err := s.requireParticipant(ctx, convID, userID); err != nil {
		return nil, 0, err
	}
	msgs, err := s.store.ListMessages(ctx, convID, before, limit)
	if err != nil {
		return nil, 0, err
	}
	var next int64
	if len(msgs) > 0 {
		next = msgs[len(msgs)-1].ID
	}
	return msgs, next, nil
}

// SendMessage validates participation, creates the message, and returns it
// together with the participant IDs (for broadcasting by the caller).
func (s *ChatService) SendMessage(ctx context.Context, userID, convID int64, text string) (*models.Message, []int64, error) {
	if err := s.requireParticipant(ctx, convID, userID); err != nil {
		return nil, nil, err
	}
	msg, participants, err := s.store.CreateMessage(ctx, convID, userID, text)
	if err != nil {
		return nil, nil, fmt.Errorf("create message: %w", err)
	}
	return msg, participants, nil
}

// MarkConversationRead advances the user's last-read cursor. If msgID is 0 it
// defaults to the latest message in the conversation.
func (s *ChatService) MarkConversationRead(ctx context.Context, userID, convID, msgID int64) error {
	if err := s.requireParticipant(ctx, convID, userID); err != nil {
		return err
	}
	if msgID == 0 {
		msgs, err := s.store.ListMessages(ctx, convID, 0, 1)
		if err == nil && len(msgs) > 0 {
			msgID = msgs[0].ID
		}
	}
	return s.store.MarkRead(ctx, convID, userID, msgID)
}

// GetPresence reports whether the given user is online.
func (s *ChatService) GetPresence(ctx context.Context, userID int64) bool {
	return s.presence.IsOnline(ctx, userID)
}

// GetParticipantIDs returns all participant user IDs for a conversation. Used by
// the hub for read-receipt broadcasting.
func (s *ChatService) GetParticipantIDs(ctx context.Context, convID int64) ([]int64, error) {
	return s.store.GetParticipants(ctx, convID)
}

// requireParticipant returns ErrNotParticipant when the user is not in the
// conversation. A store error is treated the same as "not a participant" to
// avoid leaking conversation existence.
func (s *ChatService) requireParticipant(ctx context.Context, convID, userID int64) error {
	ok, err := s.store.IsParticipant(ctx, convID, userID)
	if err != nil || !ok {
		return ErrNotParticipant
	}
	return nil
}
