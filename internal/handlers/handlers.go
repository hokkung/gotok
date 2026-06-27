// Package handlers contains the HTTP handlers for the GoTok web app, split one
// file per feature (feed, upload, like, comment, video, auth, chat).
package handlers

import (
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/hokkung/gotok/internal/chat"
	"github.com/hokkung/gotok/internal/config"
	"github.com/hokkung/gotok/internal/middleware"
	"github.com/hokkung/gotok/internal/store"
)

// Handlers groups all HTTP handlers and their dependencies.
type Handlers struct {
	cfg    *config.Config
	store  *store.Store
	hub    *chat.Hub
	logger *zap.Logger
}

// New builds a Handlers value wired to the given config, store, hub, and
// logger. A nil logger is replaced with a no-op logger.
func New(cfg *config.Config, st *store.Store, hub *chat.Hub, lg *zap.Logger) *Handlers {
	if lg == nil {
		lg = zap.NewNop()
	}
	return &Handlers{cfg: cfg, store: st, hub: hub, logger: lg}
}

// base returns the template data shared by every page: the page title and the
// logged-in user (nil when anonymous) so the layout can render the nav widget.
func (h *Handlers) base(c *gin.Context, title string) gin.H {
	return gin.H{"Title": title, "User": middleware.UserFromContext(c)}
}
