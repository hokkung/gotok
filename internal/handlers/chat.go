package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/hokkung/gotok/internal/chat"
	"github.com/hokkung/gotok/internal/middleware"
	"github.com/hokkung/gotok/internal/service"
)

// ChatPage renders the chat inbox (conversation list) page.
func (h *Handlers) ChatPage(c *gin.Context) {
	c.HTML(http.StatusOK, "chat.html", h.base(c, "Chat"))
}

// ListConversations godoc
//
//	@Summary		List conversations
//	@Description	Returns a cursor-paginated list of the current user's conversations with last message preview and unread count.
//	@Tags			chat
//	@Produce		json
//	@Param			cursor	query		int	false	"Conversation ID cursor (0 for first page)"
//	@Param			limit	query		int	false	"Page size (1-50)"	default(20)	maximum(50)
//	@Success		200		{object}	object{conversations=[]models.ConversationPreview,next=int64}
//	@Failure		401		{object}	ErrorResponse
//	@Failure		500		{object}	ErrorResponse
//	@Security		Session
//	@Router			/api/conversations [get]
func (h *Handlers) ListConversations(c *gin.Context) {
	u := middleware.UserFromContext(c)
	cursor, _ := strconv.ParseInt(c.DefaultQuery("cursor", "0"), 10, 64)
	limit, _ := strconv.ParseInt(c.DefaultQuery("limit", "20"), 10, 64)

	convs, next, err := h.chat.ListConversations(c.Request.Context(), u.ID, cursor, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load conversations"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"conversations": convs, "next": next})
}

// CreateConversationRequest is the body for POST /api/conversations.
type CreateConversationRequest struct {
	UserID  int64   `json:"user_id"`            // for DM
	UserIDs []int64 `json:"user_ids,omitempty"` // for group
	Title   string  `json:"title,omitempty"`    // group title
}

// CreateConversation godoc
//
//	@Summary		Create a conversation
//	@Description	Creates a 1-on-1 DM (user_id) or a group conversation (user_ids + title). For DMs, returns the existing conversation if one already exists.
//	@Tags			chat
//	@Accept			json
//	@Produce		json
//	@Param			body	body		CreateConversationRequest	true	"Conversation details"
//	@Success		200		{object}	object{conversation=models.Conversation}
//	@Failure		400		{object}	ErrorResponse
//	@Failure		401		{object}	ErrorResponse
//	@Failure		500		{object}	ErrorResponse
//	@Security		Session
//	@Router			/api/conversations [post]
func (h *Handlers) CreateConversation(c *gin.Context) {
	u := middleware.UserFromContext(c)
	var req CreateConversationRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request"})
		return
	}

	var conv any
	var err error
	switch {
	case len(req.UserIDs) >= 2:
		conv, err = h.chat.CreateGroupConversation(c.Request.Context(), u.ID, req.UserIDs, req.Title)
	case req.UserID != 0:
		conv, err = h.chat.CreateDMConversation(c.Request.Context(), u.ID, req.UserID)
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "user_id or user_ids is required"})
		return
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not create conversation"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"conversation": conv})
}

// ListMessages godoc
//
//	@Summary		List messages in a conversation
//	@Description	Returns a cursor-paginated list of messages (newest first).
//	@Tags			chat
//	@Produce		json
//	@Param			id		path		int	true	"Conversation ID"
//	@Param			before	query		int	false	"Message ID cursor (0 for first page)"
//	@Param			limit	query		int	false	"Page size (1-100)"	default(50)	maximum(100)
//	@Success		200		{object}	object{messages=[]models.Message,next=int64}
//	@Failure		400		{object}	ErrorResponse
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse
//	@Failure		500		{object}	ErrorResponse
//	@Security		Session
//	@Router			/api/conversations/{id}/messages [get]
func (h *Handlers) ListMessages(c *gin.Context) {
	u := middleware.UserFromContext(c)
	convID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad id"})
		return
	}

	before, _ := strconv.ParseInt(c.DefaultQuery("before", "0"), 10, 64)
	limit, _ := strconv.ParseInt(c.DefaultQuery("limit", "50"), 10, 64)

	msgs, next, err := h.chat.ListMessages(c.Request.Context(), u.ID, convID, before, limit)
	if err != nil {
		if errors.Is(err, service.ErrNotParticipant) {
			c.JSON(http.StatusForbidden, gin.H{"error": "not a participant"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load messages"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"messages": msgs, "next": next})
}

// SendMessageRequest is the body for POST /api/conversations/:id/messages.
type SendMessageRequest struct {
	Text string `json:"text" binding:"required"`
}

// SendMessage is the REST fallback for sending a message. The primary path is
// via WebSocket, but this endpoint allows clients without WS support.
func (h *Handlers) SendMessage(c *gin.Context) {
	u := middleware.UserFromContext(c)
	convID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad id"})
		return
	}

	var req SendMessageRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "text is required"})
		return
	}

	msg, participants, err := h.chat.SendMessage(c.Request.Context(), u.ID, convID, req.Text)
	if err != nil {
		if errors.Is(err, service.ErrNotParticipant) {
			c.JSON(http.StatusForbidden, gin.H{"error": "not a participant"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not send message"})
		return
	}

	// Broadcast via Redis pub/sub to other participants.
	outgoing := chat.Envelope{
		Type:           "message",
		ID:             msg.ID,
		ConversationID: msg.ConversationID,
		SenderID:       msg.SenderID,
		SenderName:     msg.SenderName,
		SenderAvatar:   msg.SenderAvatar,
		Text:           msg.Text,
		CreatedAt:      msg.CreatedAt,
	}
	payload, _ := json.Marshal(outgoing)
	for _, pid := range participants {
		_ = h.hub.PublishToUser(c.Request.Context(), pid, payload)
	}

	c.JSON(http.StatusOK, gin.H{"message": msg})
}

// MarkConversationRead godoc
//
//	@Summary		Mark conversation as read
//	@Description	Advances the current user's last-read cursor in a conversation (for read receipts).
//	@Tags			chat
//	@Accept			json
//	@Produce		json
//	@Param			id		path		int	true	"Conversation ID"
//	@Param			message_id	body	int	true	"Last message ID read"
//	@Success		200		{object}	OkResponse
//	@Failure		400		{object}	ErrorResponse
//	@Failure		401		{object}	ErrorResponse
//	@Failure		403		{object}	ErrorResponse
//	@Security		Session
//	@Router			/api/conversations/{id}/read [post]
func (h *Handlers) MarkConversationRead(c *gin.Context) {
	u := middleware.UserFromContext(c)
	convID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad id"})
		return
	}

	body := struct {
		MessageID int64 `json:"message_id"`
	}{}
	_ = c.ShouldBindJSON(&body)

	if err := h.chat.MarkConversationRead(c.Request.Context(), u.ID, convID, body.MessageID); err != nil {
		if errors.Is(err, service.ErrNotParticipant) {
			c.JSON(http.StatusForbidden, gin.H{"error": "not a participant"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not mark read"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true})
}

// GetPresence godoc
//
//	@Summary		Check user online status
//	@Description	Returns whether the given user is currently online (has an active WebSocket connection on any instance).
//	@Tags			chat
//	@Produce		json
//	@Param			id	path		int	true	"User ID"
//	@Success		200	{object}	object{online=bool}
//	@Router			/api/users/{id}/presence [get]
func (h *Handlers) GetPresence(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad id"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"online": h.chat.GetPresence(c.Request.Context(), id)})
}

// HandleWebSocket upgrades to WebSocket. Must be behind RequireAuth.
func (h *Handlers) HandleWebSocket(c *gin.Context) {
	u := middleware.UserFromContext(c)
	chat.ServeWS(h.hub, h.logger, c.Writer, c.Request, u.ID)
}
