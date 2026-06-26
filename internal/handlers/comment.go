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

// ListComments returns a JSON page of comments for a video (newest first).
// Query params: cursor=<id of last item>, limit=<1..50>.
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

	comments, err := h.store.ListComments(id, cursor, limit)
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

// CreateComment accepts a new comment (form field "text") and returns the
// created comment plus the refreshed comment count for the video.
func (h *Handlers) CreateComment(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad id"})
		return
	}
	u := middleware.UserFromContext(c)
	if _, err := h.store.GetVideo(u.ID, id); err != nil {
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

	comment, count, err := h.store.CreateComment(u.ID, u.Name, id, text)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not create comment"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"comment": comment, "count": count})
}
