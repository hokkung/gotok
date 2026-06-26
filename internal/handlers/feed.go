package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/hokkung/gotok/internal/middleware"
)

// FeedPage renders the TikTok-style vertical feed.
func (h *Handlers) FeedPage(c *gin.Context) {
	c.HTML(http.StatusOK, "feed.html", h.base(c, "Feed"))
}

// ListVideos godoc
//	@Summary		List videos for the feed
//	@Description	Returns a cursor-paginated page of videos (newest first) for infinite scroll.
//	@Tags			feed
//	@Produce		json
//	@Param			cursor	query		int	false	"ID of the last item seen (0 for first page)"
//	@Param			limit	query		int	false	"Page size (1-50)"	default(20)	maximum(50)
//	@Success		200		{object}	ListVideosResponse
//	@Failure		500		{object}	ErrorResponse
//	@Router			/api/videos [get]
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
