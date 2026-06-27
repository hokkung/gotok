package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/hokkung/gotok/internal/middleware"
)

// maxCommentLen is the hard cap on a single comment's length.
const maxCommentLen = 500

// ListComments godoc
//	@Summary		List comments for a video
//	@Description	Returns a cursor-paginated page of comments for a video (newest first).
//	@Tags			comments
//	@Produce		json
//	@Param			id		path		int	true	"Video ID"
//	@Param			cursor	query		int	false	"ID of the last item seen (0 for first page)"
//	@Param			limit	query		int	false	"Page size (1-50)"	default(20)	maximum(50)
//	@Success		200		{object}	ListCommentsResponse
//	@Failure		400		{object}	ErrorResponse
//	@Failure		500		{object}	ErrorResponse
//	@Router			/api/videos/{id}/comments [get]
func (h *Handlers) ListComments(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad id"})
		return
	}
	cursor, _ := strconv.ParseInt(c.DefaultQuery("cursor", "0"), 10, 64)
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if limit <= 0 || limit > 50 {
		limit = 20
	}

	comments, err := h.store.ListComments(c.Request.Context(), id, cursor, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load comments"})
		return
	}
	next := int64(0)
	if len(comments) > 0 {
		next = comments[len(comments)-1].ID
	}
	c.JSON(http.StatusOK, gin.H{"comments": comments, "next": next})
}

// CreateComment godoc
//	@Summary		Create a comment on a video
//	@Description	Accepts a new comment (form field "text") and returns the created comment plus the refreshed comment count.
//	@Tags			comments
//	@Accept			mpfd
//	@Produce		json
//	@Param			id		path		int		true	"Video ID"
//	@Param			text	formData	string	true	"Comment text (max 500 characters)"
//	@Success		200		{object}	CreateCommentResponse
//	@Failure		400		{object}	ErrorResponse
//	@Failure		401		{object}	ErrorResponse
//	@Failure		404		{object}	ErrorResponse
//	@Failure		500		{object}	ErrorResponse
//	@Security		Session
//	@Router			/api/videos/{id}/comments [post]
func (h *Handlers) CreateComment(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad id"})
		return
	}
	u := middleware.UserFromContext(c)
	if _, err := h.store.GetVideo(c.Request.Context(), u.ID, id); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "video not found"})
		return
	}

	text := strings.TrimSpace(c.PostForm("text"))
	if text == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "comment cannot be empty"})
		return
	}
	if len([]rune(text)) > maxCommentLen {
		text = string([]rune(text)[:maxCommentLen])
	}

	comment, count, err := h.store.CreateComment(c.Request.Context(), u.ID, u.Name, id, text)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not create comment"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"comment": comment, "count": count})
}
