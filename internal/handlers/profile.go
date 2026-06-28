package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/hokkung/gotok/internal/middleware"
	"github.com/hokkung/gotok/internal/service"
)

// allowedImage maps an accepted avatar MIME type to its file extension.
var allowedImage = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/webp": ".webp",
}

// maxAvatarMB is the per-upload size cap for a profile photo.
const maxAvatarMB = 5

// ProfilePage renders a user's profile: their avatar/name/bio, video & liked
// counts, and a tabbed grid of uploaded or liked videos. An unknown user id
// renders a 404.
func (h *Handlers) ProfilePage(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.String(http.StatusNotFound, "user not found")
		return
	}
	result, err := h.profile.GetProfile(c.Request.Context(), id)
	if err != nil {
		c.String(http.StatusNotFound, "user not found")
		return
	}

	data := h.base(c, result.User.Name)
	data["Profile"] = result.User
	data["VideoCount"] = result.VideoCount
	data["LikedCount"] = result.LikedCount
	data["Initial"] = profileInitial(result.User.Name)
	isOwner := false
	isLoggedIn := false
	if u := middleware.UserFromContext(c); u != nil {
		isLoggedIn = true
		if u.ID == id {
			isOwner = true
		}
	}
	data["IsOwner"] = isOwner
	data["IsLoggedIn"] = isLoggedIn
	c.HTML(http.StatusOK, "profile.html", data)
}

// ListVideosByUser godoc
//
//	@Summary		List a user's videos
//	@Description	Returns a cursor-paginated page of a single user's videos (newest first) with the requesting viewer's like state.
//	@Tags			users
//	@Produce		json
//	@Param			id		path		int	true	"User ID"
//	@Param			cursor	query		int	false	"ID of the last item seen (0 for first page)"
//	@Param			limit	query		int	false	"Page size (1-50)"	default(24)	maximum(50)
//	@Success		200		{object}	ListVideosResponse
//	@Failure		400		{object}	ErrorResponse
//	@Failure		500		{object}	ErrorResponse
//	@Router			/api/users/{id}/videos [get]
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

	videos, next, err := h.video.ListUserVideos(c.Request.Context(), id, viewerID, cursor, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load videos"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"videos": videos, "next": next})
}

// ListLikedVideos godoc
//
//	@Summary		List videos a user has liked
//	@Description	Returns a cursor-paginated page of the videos a user has liked (most recently liked first) with the requesting viewer's like state.
//	@Tags			users
//	@Produce		json
//	@Param			id		path		int	true	"User ID"
//	@Param			cursor	query		int	false	"ID of the last item seen (0 for first page)"
//	@Param			limit	query		int	false	"Page size (1-50)"	default(24)	maximum(50)
//	@Success		200		{object}	ListVideosResponse
//	@Failure		400		{object}	ErrorResponse
//	@Failure		500		{object}	ErrorResponse
//	@Router			/api/users/{id}/liked [get]
func (h *Handlers) ListLikedVideos(c *gin.Context) {
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

	videos, next, err := h.video.ListUserLikedVideos(c.Request.Context(), id, viewerID, cursor, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not load liked videos"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"videos": videos, "next": next})
}

// EditProfile godoc
//
//	@Summary		Edit profile
//	@Description	Updates the current user's editable profile fields (name, bio, and optionally a new avatar image). Only the logged-in user can edit their own profile; on success it returns the updated user.
//	@Tags			users
//	@Accept			mpfd
//	@Produce		json
//	@Param			name	formData	string	true	"Display name"
//	@Param			bio		formData	string	false	"Profile bio (max 160 characters)"
//	@Param			file	formData	file	false	"Avatar image (jpg, png, webp; max 5MB)"
//	@Success		200		{object}	EditProfileResponse
//	@Failure		400		{object}	ErrorResponse
//	@Failure		401		{object}	ErrorResponse
//	@Failure		413		{object}	ErrorResponse
//	@Failure		500		{object}	ErrorResponse
//	@Security		Session
//	@Router			/api/profile [post]
func (h *Handlers) EditProfile(c *gin.Context) {
	u := middleware.UserFromContext(c)

	name := strings.TrimSpace(c.PostForm("name"))
	bio := strings.TrimSpace(c.PostForm("bio"))

	avatarURL := ""
	file, err := c.FormFile("file")
	if err == nil {
		mime := file.Header.Get("Content-Type")
		ext, ok := allowedImage[mime]
		if !ok {
			fext := strings.ToLower(filepath.Ext(file.Filename))
			if m := extToImageMime(fext); m != "" {
				ext = fext
			}
		}
		if ext == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported image type; use jpg, png or webp"})
			return
		}
		if file.Size > maxAvatarMB*1024*1024 {
			c.JSON(http.StatusRequestEntityTooLarge,
				gin.H{"error": fmt.Sprintf("image too large (max %dMB)", maxAvatarMB)})
			return
		}

		stored := fmt.Sprintf("avatar-%d-%s%s", time.Now().UnixNano(), randID(6), ext)
		dst := filepath.Join(h.cfg.UploadDir, stored)
		if err := c.SaveUploadedFile(file, dst); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "could not save image"})
			return
		}
		if old := u.AvatarURL; strings.HasPrefix(old, "/uploads/") {
			if rmErr := os.Remove(filepath.Join(h.cfg.UploadDir, filepath.Base(old))); rmErr != nil && !os.IsNotExist(rmErr) {
				h.logger.Warn("remove old avatar", zap.String("path", old))
			}
		}
		avatarURL = "/uploads/" + stored
	}

	updated, err := h.profile.UpdateProfile(c.Request.Context(), u.ID, name, bio, avatarURL)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrProfileNameRequired):
			c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
		case errors.Is(err, service.ErrBioTooLong):
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("bio too long (max %d characters)", service.MaxBioRunes)})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "could not update profile"})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": updated})
}

// extToImageMime reverses a file extension to an image MIME type (returns "" for
// unknown), mirroring the video upload fallback logic.
func extToImageMime(ext string) string {
	switch ext {
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	}
	return ""
}

// profileInitial returns the uppercase first rune of a name for the avatar
// badge, falling back to "?" for empty names.
func profileInitial(name string) string {
	if name == "" {
		return "?"
	}
	return strings.ToUpper(string([]rune(name)[:1]))
}
