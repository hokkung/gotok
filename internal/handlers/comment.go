package handlers

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/hokkung/gotok/internal/middleware"
	"github.com/hokkung/gotok/internal/service"
)

// ListComments godoc
//
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

	comments, next, err := h.video.ListComments(c.Request.Context(), id, cursor, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load comments"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"comments": comments, "next": next})
}

// CreateComment godoc
//
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
	text := c.PostForm("text")

	comment, count, err := h.video.CreateComment(c.Request.Context(), u.ID, u.Name, id, text)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrEmptyComment):
			c.JSON(http.StatusBadRequest, gin.H{"error": "comment cannot be empty"})
		case errors.Is(err, service.ErrVideoNotFound):
			c.JSON(http.StatusNotFound, gin.H{"error": "video not found"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "could not create comment"})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"comment": comment, "count": count})
}
