package handlers

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/hokkung/gotok/internal/middleware"
	"github.com/hokkung/gotok/internal/models"
)

// allowedVideo maps an accepted MIME type to its file extension.
var allowedVideo = map[string]string{
	"video/mp4":        ".mp4",
	"video/webm":       ".webm",
	"video/quicktime":  ".mov",
	"video/x-matroska": ".mkv",
}

// extToMime is the reverse lookup used when the browser omits a content type.
var extToMime = map[string]string{
	".mp4":  "video/mp4",
	".webm": "video/webm",
	".mov":  "video/quicktime",
	".mkv":  "video/x-matroska",
}

// UploadPage renders the upload form.
func (h *Handlers) UploadPage(c *gin.Context) {
	data := h.base(c, "Upload")
	data["MaxUploadMB"] = h.cfg.MaxUploadMB
	c.HTML(http.StatusOK, "upload.html", data)
}

// Upload godoc
//
//	@Summary		Upload a video
//	@Description	Handles a multipart video upload: validates type/size, stores it on the local filesystem, and records metadata. Accepted types: mp4, webm, mov, mkv.
//	@Tags			upload
//	@Accept			mpfd
//	@Produce		json
//	@Param			file	formData	file	true	"Video file (mp4, webm, mov, mkv)"
//	@Param			title	formData	string	false	"Video title (defaults to the filename)"
//	@Success		200		{object}	UploadResponse
//	@Failure		400		{object}	ErrorResponse
//	@Failure		401		{object}	ErrorResponse
//	@Failure		413		{object}	ErrorResponse
//	@Failure		500		{object}	ErrorResponse
//	@Security		Session
//	@Router			/api/upload [post]
func (h *Handlers) Upload(c *gin.Context) {
	file, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "no file provided"})
		return
	}

	mime := file.Header.Get("Content-Type")
	ext, ok := allowedVideo[mime]
	if !ok {
		// Fall back to the file extension.
		fext := strings.ToLower(filepath.Ext(file.Filename))
		if m, ok2 := extToMime[fext]; ok2 {
			ext = fext
			mime = m
		}
	}
	if ext == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported file type; use mp4, webm, mov or mkv"})
		return
	}

	maxBytes := h.cfg.MaxUploadMB * 1024 * 1024
	if file.Size > maxBytes {
		c.JSON(http.StatusRequestEntityTooLarge,
			gin.H{"error": fmt.Sprintf("file too large (max %dMB)", h.cfg.MaxUploadMB)})
		return
	}

	title := strings.TrimSpace(c.PostForm("title"))
	if title == "" {
		title = strings.TrimSuffix(filepath.Base(file.Filename), filepath.Ext(file.Filename))
	}

	stored := fmt.Sprintf("%d-%s%s", time.Now().UnixNano(), randID(6), ext)
	dst := filepath.Join(h.cfg.UploadDir, stored)
	if err := c.SaveUploadedFile(file, dst); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not save file"})
		return
	}

	u := middleware.UserFromContext(c)
	id, err := h.store.CreateVideo(c.Request.Context(), &models.Video{
		UserID:   u.ID,
		Title:    title,
		Filename: stored,
		FilePath: dst,
		MimeType: mime,
		Size:     file.Size,
	})
	if err != nil {
		if rmErr := os.Remove(dst); rmErr != nil {
			h.logger.Error("remove orphaned upload", zap.String("path", dst), zap.Error(rmErr))
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not create record"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"id": id, "filename": stored, "title": title})
}
