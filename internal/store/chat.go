package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/hokkung/gotok/internal/models"
)

// GetOrCreateDMConversation returns the existing 1-on-1 conversation between
// userA and userB, or creates a new one if none exists. The participant order
// does not matter — the same pair always resolves to the same conversation.
func (s *Store) GetOrCreateDMConversation(ctx context.Context, userA, userB int64) (*models.Conversation, error) {
	if userA == userB {
		return nil, fmt.Errorf("cannot create a DM with yourself")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	now := time.Now().Unix()

	var convID int64
	err = tx.QueryRowContext(ctx, `
		SELECT c.id FROM conversations c
		WHERE c.type = 'dm'
		  AND EXISTS (SELECT 1 FROM conversation_participants cp WHERE cp.conversation_id = c.id AND cp.user_id = $1)
		  AND EXISTS (SELECT 1 FROM conversation_participants cp WHERE cp.conversation_id = c.id AND cp.user_id = $2)`,
		userA, userB).Scan(&convID)

	if err == nil {
		return s.getConversationTx(ctx, tx, convID)
	}
	if err != sql.ErrNoRows {
		return nil, err
	}

	err = tx.QueryRowContext(ctx,
		`INSERT INTO conversations (type, title, created_at) VALUES ('dm', '', $1) RETURNING id`,
		now,
	).Scan(&convID)
	if err != nil {
		return nil, err
	}

	_, err = tx.ExecContext(ctx,
		`INSERT INTO conversation_participants (conversation_id, user_id, joined_at) VALUES ($1, $2, $3), ($1, $4, $5)`,
		convID, userA, now, userB, now)
	if err != nil {
		return nil, err
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}

	return &models.Conversation{ID: convID, Type: models.ConversationTypeDM, CreatedAt: now}, nil
}

// CreateGroupConversation creates a new group conversation with the given title
// and participant list. The creator is implicitly the first participant.
func (s *Store) CreateGroupConversation(ctx context.Context, title string, userIDs []int64) (*models.Conversation, error) {
	if len(userIDs) < 2 {
		return nil, fmt.Errorf("group conversation requires at least 2 participants")
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	now := time.Now().Unix()
	var convID int64
	err = tx.QueryRowContext(ctx,
		`INSERT INTO conversations (type, title, created_at) VALUES ('group', $1, $2) RETURNING id`,
		title, now,
	).Scan(&convID)
	if err != nil {
		return nil, err
	}

	for _, uid := range userIDs {
		if _, err = tx.ExecContext(ctx,
			`INSERT INTO conversation_participants (conversation_id, user_id, joined_at) VALUES ($1, $2, $3)`,
			convID, uid, now); err != nil {
			return nil, err
		}
	}

	if err = tx.Commit(); err != nil {
		return nil, err
	}

	return &models.Conversation{ID: convID, Type: models.ConversationTypeGroup, Title: title, CreatedAt: now}, nil
}

// GetConversation returns a conversation by id.
func (s *Store) GetConversation(ctx context.Context, convID int64) (*models.Conversation, error) {
	var c models.Conversation
	err := s.db.QueryRowContext(ctx,
		`SELECT id, type, title, created_at FROM conversations WHERE id = $1`, convID,
	).Scan(&c.ID, &c.Type, &c.Title, &c.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &c, nil
}

// getConversationTx reads a conversation inside a transaction.
func (s *Store) getConversationTx(ctx context.Context, tx *sql.Tx, convID int64) (*models.Conversation, error) {
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return s.GetConversation(ctx, convID)
}

// ListConversations returns a page of conversations for the given user, most
// recently active first, with the last message preview and unread count.
// afterID=0 means first page; pagination is keyed on conversation id.
func (s *Store) ListConversations(ctx context.Context, userID, afterID, limit int64) ([]models.ConversationPreview, error) {
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.id, c.type, c.title, c.created_at,
		       COALESCE(last_msg.text, '')    AS last_msg_text,
		       COALESCE(last_msg.created_at, 0) AS last_msg_at,
		       COALESCE(last_msg.sender_id, 0) AS last_msg_sender,
		       COALESCE(unread.cnt, 0)        AS unread_count,
		       COALESCE(other_u.id, 0)        AS other_user_id,
		       COALESCE(other_u.name, '')     AS other_user_name,
		       COALESCE(other_u.avatar_url, '') AS other_user_avatar
		FROM conversation_participants cp
		JOIN conversations c ON c.id = cp.conversation_id
		LEFT JOIN LATERAL (
		    SELECT text, created_at, sender_id FROM messages
		    WHERE conversation_id = c.id ORDER BY id DESC LIMIT 1
		) last_msg ON true
		LEFT JOIN LATERAL (
		    SELECT COUNT(*) AS cnt FROM messages m
		    WHERE m.conversation_id = c.id AND m.id > cp.last_read_msg_id AND m.sender_id <> $1
		) unread ON true
		LEFT JOIN LATERAL (
		    SELECT u.id, u.name, u.avatar_url FROM conversation_participants cp2
		    JOIN users u ON u.id = cp2.user_id
		    WHERE cp2.conversation_id = c.id AND cp2.user_id <> $1
		    LIMIT 1
		) other_u ON true
		WHERE cp.user_id = $1 AND ($2 = 0 OR c.id < $2)
		ORDER BY COALESCE(last_msg.created_at, c.created_at) DESC
		LIMIT $3`,
		userID, afterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []models.ConversationPreview
	for rows.Next() {
		var p models.ConversationPreview
		if err := rows.Scan(
			&p.ID, &p.Type, &p.Title, &p.CreatedAt,
			&p.LastMsgText, &p.LastMsgAt, &p.LastMsgSender,
			&p.UnreadCount,
			&p.OtherUserID, &p.OtherUserName, &p.OtherAvatar,
		); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// CreateMessage inserts a message into a conversation and returns the message
// along with the list of participant user IDs (for WebSocket broadcasting).
func (s *Store) CreateMessage(ctx context.Context, convID, senderID int64, text string) (*models.Message, []int64, error) {
	now := time.Now().Unix()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, err
	}
	defer tx.Rollback()

	var id int64
	err = tx.QueryRowContext(ctx,
		`INSERT INTO messages (conversation_id, sender_id, text, created_at) VALUES ($1, $2, $3, $4) RETURNING id`,
		convID, senderID, text, now,
	).Scan(&id)
	if err != nil {
		return nil, nil, err
	}

	rows, err := tx.QueryContext(ctx,
		`SELECT user_id FROM conversation_participants WHERE conversation_id = $1`, convID)
	if err != nil {
		return nil, nil, err
	}
	var participants []int64
	for rows.Next() {
		var uid int64
		if err := rows.Scan(&uid); err != nil {
			rows.Close()
			return nil, nil, err
		}
		participants = append(participants, uid)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}

	if err = tx.Commit(); err != nil {
		return nil, nil, err
	}

	var senderName, senderAvatar string
	err = s.db.QueryRowContext(ctx,
		`SELECT name, avatar_url FROM users WHERE id = $1`, senderID,
	).Scan(&senderName, &senderAvatar)
	if err != nil {
		senderName = ""
		senderAvatar = ""
	}

	return &models.Message{
		ID:             id,
		ConversationID: convID,
		SenderID:       senderID,
		SenderName:     senderName,
		SenderAvatar:   senderAvatar,
		Text:           text,
		CreatedAt:      now,
	}, participants, nil
}

// ListMessages returns a page of messages for a conversation, newest first.
// beforeID=0 means first page; pagination is keyed on message id.
func (s *Store) ListMessages(ctx context.Context, convID, beforeID, limit int64) ([]models.Message, error) {
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT m.id, m.conversation_id, m.sender_id,
		       COALESCE(u.name, ''), COALESCE(u.avatar_url, ''),
		       m.text, m.created_at
		FROM messages m
		LEFT JOIN users u ON u.id = m.sender_id
		WHERE m.conversation_id = $1 AND ($2 = 0 OR m.id < $2)
		ORDER BY m.id DESC
		LIMIT $3`,
		convID, beforeID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []models.Message
	for rows.Next() {
		var m models.Message
		if err := rows.Scan(&m.ID, &m.ConversationID, &m.SenderID, &m.SenderName, &m.SenderAvatar, &m.Text, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// MarkRead updates the last-read message id for a participant in a conversation.
// Only advances forward (uses GREATEST) so out-of-order read receipts don't
// regress the cursor.
func (s *Store) MarkRead(ctx context.Context, convID, userID, msgID int64) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE conversation_participants SET last_read_msg_id = GREATEST(last_read_msg_id, $3)
		 WHERE conversation_id = $1 AND user_id = $2`,
		convID, userID, msgID)
	return err
}

// IsParticipant reports whether the given user is a participant in the
// conversation.
func (s *Store) IsParticipant(ctx context.Context, convID, userID int64) (bool, error) {
	var exists bool
	err := s.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM conversation_participants WHERE conversation_id = $1 AND user_id = $2)`,
		convID, userID).Scan(&exists)
	return exists, err
}

// GetParticipants returns the user IDs of all participants in a conversation.
func (s *Store) GetParticipants(ctx context.Context, convID int64) ([]int64, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT user_id FROM conversation_participants WHERE conversation_id = $1`, convID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []int64
	for rows.Next() {
		var uid int64
		if err := rows.Scan(&uid); err != nil {
			return nil, err
		}
		out = append(out, uid)
	}
	return out, rows.Err()
}

// GetConversationParticipants returns detailed participant info for a
// conversation (used for group chat member lists).
func (s *Store) GetConversationParticipants(ctx context.Context, convID int64) ([]models.ConversationParticipant, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT cp.user_id, COALESCE(u.name, ''), COALESCE(u.avatar_url, ''), cp.last_read_msg_id
		FROM conversation_participants cp
		LEFT JOIN users u ON u.id = cp.user_id
		WHERE cp.conversation_id = $1
		ORDER BY cp.joined_at`, convID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []models.ConversationParticipant
	for rows.Next() {
		var p models.ConversationParticipant
		if err := rows.Scan(&p.UserID, &p.Name, &p.AvatarURL, &p.LastReadMsgID); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
