package handlers

import (
	"path/filepath"

	"github.com/gin-gonic/gin"
)

// ServeFile streams an uploaded video. gin's c.File uses http.ServeFile under
// the hood, which honours HTTP Range requests so clients can seek/scrub.
func (h *Handlers) ServeFile(c *gin.Context) {
	name := filepath.Base(c.Param("filename"))
	c.File(filepath.Join(h.cfg.UploadDir, name))
}
