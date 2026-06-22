package handlers

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"live/internal/models"
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

// Upload handles a multipart video upload: validates type/size, stores it on the
// local filesystem, and records metadata in the database.
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

	id, err := h.store.CreateVideo(&models.Video{
		Title:    title,
		Filename: stored,
		FilePath: dst,
		MimeType: mime,
		Size:     file.Size,
	})
	if err != nil {
		if rmErr := os.Remove(dst); rmErr != nil {
			log.Printf("remove orphaned upload %s: %v", dst, rmErr)
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not create record"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"id": id, "filename": stored, "title": title})
}
