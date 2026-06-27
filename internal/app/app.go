// Package app wires the Gin engine, middleware, and route registration for GoTok.
package app

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
	swaggerFiles "github.com/swaggo/files"
	"github.com/swaggo/gin-swagger"

	_ "github.com/hokkung/gotok/docs" // registers the generated OpenAPI spec
	"github.com/hokkung/gotok/internal/config"
	"github.com/hokkung/gotok/internal/handlers"
	"github.com/hokkung/gotok/internal/middleware"
	"github.com/hokkung/gotok/internal/store"
)

// Run builds the HTTP server (Gin engine, middleware, routes) and blocks until
// the server stops. cfg, st, and lg must already be initialised by the caller.
func Run(cfg *config.Config, st *store.Store, lg *zap.Logger) {
	// gin.New() gives a bare engine; we add the zap-backed logger + recovery
	// middleware (replacing gin.Default's std-library logging) and then Auth.
	r := gin.New()
	r.Use(middleware.GinLogger(lg), middleware.GinRecovery(lg))
	r.MaxMultipartMemory = 32 << 20 // 32 MiB in memory; larger spills to temp files.
	r.Use(middleware.Auth(st))
	r.LoadHTMLGlob("web/templates/*")

	h := handlers.New(cfg, st, lg)

	registerRoutes(r, h)

	// The Swagger UI is a development aid; only mount it when GOTOK_DEV is set
	// so it is never exposed in a production deployment.
	if cfg.Dev {
		r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
		lg.Info("Swagger UI enabled at /swagger/index.html")
	}

	lg.Info("GoTok listening", zap.String("addr", cfg.ListenAddr))
	if err := r.Run(cfg.ListenAddr); err != nil {
		// lg.Fatal would skip deferred cleanup in main; log and return instead so
		// the store closes cleanly on shutdown.
		lg.Error("server", zap.Error(err))
	}
}

// registerRoutes wires every page and API endpoint onto the engine.
func registerRoutes(r *gin.Engine, h *handlers.Handlers) {
	r.Static("/static", "./web/static")
	r.GET("/", func(c *gin.Context) { c.Redirect(http.StatusFound, "/feed") })
	r.GET("/feed", h.FeedPage)
	r.GET("/upload", h.UploadPage)
	r.GET("/chat", h.ChatPage)
	r.GET("/uploads/:filename", h.ServeFile)
	r.GET("/u/:id", h.ProfilePage)

	// Auth pages and actions. Google & Facebook SSO are stubs (501) for now; the
	// demo login lets the auth-gated actions be exercised end-to-end.
	r.GET("/login", h.LoginPage)
	r.POST("/logout", h.Logout)
	r.POST("/auth/demo", h.LoginDemo)
	r.POST("/auth/login", h.Login)
	r.POST("/auth/register", h.Register)
	r.POST("/auth/google", h.LoginGoogle)
	r.POST("/auth/facebook", h.LoginFacebook)

	api := r.Group("/api")
	{
		api.GET("/videos", h.ListVideos)
		api.GET("/users/:id/videos", h.ListVideosByUser)
		api.GET("/users/:id/liked", h.ListLikedVideos)
		api.GET("/me", h.Me)
		api.POST("/videos/:id/view", h.View)
		api.POST("/videos/:id/like", middleware.RequireAuth(), h.ToggleLike)
		api.GET("/videos/:id/comments", h.ListComments)
		api.POST("/videos/:id/comments", middleware.RequireAuth(), h.CreateComment)
		api.POST("/upload", middleware.RequireAuth(), h.Upload)
		api.POST("/profile", middleware.RequireAuth(), h.EditProfile)
	}
}
