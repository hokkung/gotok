package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// ChatPage renders the chat inbox (conversation list) page.
func (h *Handlers) ChatPage(c *gin.Context) {
	c.HTML(http.StatusOK, "chat.html", h.base(c, "Chat"))
}
