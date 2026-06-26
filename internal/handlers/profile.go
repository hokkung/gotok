package handlers

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"live/internal/middleware"
)

// ProfilePage renders a user's profile: their avatar/name and a grid of the
// videos they've uploaded. An unknown user id renders a 404.
func (h *Handlers) ProfilePage(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.String(http.StatusNotFound, "user not found")
		return
	}
	profile, err := h.store.GetUser(id)
	if err != nil {
		c.String(http.StatusNotFound, "user not found")
		return
	}
	count, _ := h.store.CountVideosByUser(id)
	data := h.base(c, profile.Name)
	data["Profile"] = profile
	data["VideoCount"] = count
	data["Initial"] = profileInitial(profile.Name)
	c.HTML(http.StatusOK, "profile.html", data)
}

// ListVideosByUser returns a JSON page of a single user's videos (newest first)
// with the requesting viewer's like state. Query params: cursor=<id>, limit.
func (h *Handlers) ListVideosByUser(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bad id"})
		return
	}
	var viewerID int64
	if u := middleware.UserFromContext(c); u != nil {
		viewerID = u.ID
	}
	cursor, _ := strconv.ParseInt(c.DefaultQuery("cursor", "0"), 10, 64)
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "24"))
	if limit <= 0 || limit > 50 {
		limit = 24
	}

	videos, err := h.store.ListVideosByUser(id, viewerID, cursor, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load videos"})
		return
	}
	next := int64(0)
	if len(videos) > 0 {
		next = videos[len(videos)-1].ID
	}
	c.JSON(http.StatusOK, gin.H{"videos": videos, "next": next})
}

// profileInitial returns the uppercase first rune of a name for the avatar
// badge, falling back to "?" for empty names.
func profileInitial(name string) string {
	if name == "" {
		return "?"
	}
	return strings.ToUpper(string([]rune(name)[:1]))
}
