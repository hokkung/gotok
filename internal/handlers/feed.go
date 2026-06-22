package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"live/internal/middleware"
)

// FeedPage renders the TikTok-style vertical feed.
func (h *Handlers) FeedPage(c *gin.Context) {
	c.HTML(http.StatusOK, "feed.html", h.base(c, "Feed"))
}

// ListVideos returns a JSON page of videos (newest first) for infinite scroll.
// Query params: cursor=<id of last item>, limit=<1..50>.
func (h *Handlers) ListVideos(c *gin.Context) {
	var userID int64
	if u := middleware.UserFromContext(c); u != nil {
		userID = u.ID
	}

	cursor, _ := strconv.ParseInt(c.DefaultQuery("cursor", "0"), 10, 64)
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	if limit <= 0 || limit > 50 {
		limit = 20
	}

	videos, err := h.store.ListVideos(userID, cursor, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load videos"})
		return
	}

	next := int64(0)
	if len(videos) > 0 {
		next = videos[len(videos)-1].ID
	}
	c.JSON(http.StatusOK, gin.H{
		"videos": videos,
		"next":   next,
	})
}
