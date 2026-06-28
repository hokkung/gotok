// Package handlers contains the HTTP handlers for the GoTok web app, split one
// file per feature (feed, upload, like, comment, video, auth, chat). Handlers
// are thin: they parse HTTP input, delegate business logic to the service
// layer, and map errors to HTTP status codes.
package handlers

import (
	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"github.com/hokkung/gotok/internal/chat"
	"github.com/hokkung/gotok/internal/config"
	"github.com/hokkung/gotok/internal/middleware"
	"github.com/hokkung/gotok/internal/service"
)

// Handlers groups all HTTP handlers and their dependencies.
type Handlers struct {
	cfg     *config.Config
	video   *service.VideoService
	auth    *service.AuthService
	profile *service.ProfileService
	chat    *service.ChatService
	hub     *chat.Hub
	logger  *zap.Logger
}

// New builds a Handlers value wired to the given services, hub, and logger. A
// nil logger is replaced with a no-op logger.
func New(cfg *config.Config, vs *service.VideoService, as *service.AuthService, ps *service.ProfileService, cs *service.ChatService, hub *chat.Hub, lg *zap.Logger) *Handlers {
	if lg == nil {
		lg = zap.NewNop()
	}
	return &Handlers{
		cfg:     cfg,
		video:   vs,
		auth:    as,
		profile: ps,
		chat:    cs,
		hub:     hub,
		logger:  lg,
	}
}

// base returns the template data shared by every page: the page title and the
// logged-in user (nil when anonymous) so the layout can render the nav widget.
func (h *Handlers) base(c *gin.Context, title string) gin.H {
	return gin.H{"Title": title, "User": middleware.UserFromContext(c)}
}
