package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"live/internal/middleware"
)

// ToggleLike flips the requesting client's like on a video and returns the new
// state and total count.
func (h *Handlers) ToggleLike(c *gin.Context) {
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
	liked, count, err := h.store.ToggleLike(u.ID, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not toggle like"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"liked": liked, "count": count})
}

// View increments a video's view counter (called once per video by the client).
func (h *Handlers) View(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad id"})
		return
	}
	h.store.IncrementViews(id)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
