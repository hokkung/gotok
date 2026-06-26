package handlers

import (
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/hokkung/gotok/internal/middleware"
)

// ToggleLike godoc
//	@Summary		Toggle like on a video
//	@Description	Flips the requesting client's like on a video and returns the new state and total count.
//	@Tags			likes
//	@Produce		json
//	@Param			id	path		int	true	"Video ID"
//	@Success		200	{object}	ToggleLikeResponse
//	@Failure		400	{object}	ErrorResponse
//	@Failure		401	{object}	ErrorResponse
//	@Failure		404	{object}	ErrorResponse
//	@Failure		500	{object}	ErrorResponse
//	@Security		Session
//	@Router			/api/videos/{id}/like [post]
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

// View godoc
//	@Summary		Record a video view
//	@Description	Increments a video's view counter (called once per video by the client).
//	@Tags			videos
//	@Produce		json
//	@Param			id	path		int	true	"Video ID"
//	@Success		200	{object}	OkResponse
//	@Failure		400	{object}	ErrorResponse
//	@Router			/api/videos/{id}/view [post]
func (h *Handlers) View(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad id"})
		return
	}
	h.store.IncrementViews(id)
	c.JSON(http.StatusOK, gin.H{"ok": true})
}
