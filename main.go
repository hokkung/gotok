// Command gotok is a minimal, single-binary TikTok-style vertical-video web
// app: Gin + SQLite + vanilla JS, no frontend build step.
package main

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"

	"live/internal/config"
	"live/internal/handlers"
	"live/internal/logger"
	"live/internal/middleware"
	"live/internal/store"
)

func main() {
	// Build the logger first so every subsequent step (config, store, server)
	// can report through zap. If zap itself fails to initialise there is no
	// logger to log with, so we panic — a genuine environment error.
	lg, err := logger.New()
	if err != nil {
		panic("init logger: " + err.Error())
	}
	defer func() { _ = lg.Sync() }()

	cfg, err := config.Load()
	if err != nil {
		lg.Fatal("config", zap.Error(err))
	}
	st, err := store.New(cfg.DBPath, lg)
	if err != nil {
		lg.Fatal("store", zap.Error(err))
	}
	defer func() {
		if err := st.Close(); err != nil {
			lg.Error("close store", zap.Error(err))
		}
	}()

	// gin.New() gives a bare engine; we add the zap-backed logger + recovery
	// middleware (replacing gin.Default's std-library logging) and then Auth.
	r := gin.New()
	r.Use(middleware.GinLogger(lg), middleware.GinRecovery(lg))
	r.MaxMultipartMemory = 32 << 20 // 32 MiB in memory; larger spills to temp files.
	r.Use(middleware.Auth(st))
	r.LoadHTMLGlob("web/templates/*")

	h := handlers.New(cfg, st, lg)

	r.Static("/static", "./web/static")
	r.GET("/", func(c *gin.Context) { c.Redirect(http.StatusFound, "/feed") })
	r.GET("/feed", h.FeedPage)
	r.GET("/upload", h.UploadPage)
	r.GET("/uploads/:filename", h.ServeFile)

	// Auth pages and actions. Google & Facebook SSO are stubs (501) for now; the
	// demo login lets the auth-gated actions be exercised end-to-end.
	r.GET("/login", h.LoginPage)
	r.POST("/logout", h.Logout)
	r.POST("/auth/demo", h.LoginDemo)
	r.POST("/auth/google", h.LoginGoogle)
	r.POST("/auth/facebook", h.LoginFacebook)

	api := r.Group("/api")
	{
		api.GET("/videos", h.ListVideos)
		api.GET("/me", h.Me)
		api.POST("/videos/:id/view", h.View)
		api.POST("/videos/:id/like", middleware.RequireAuth(), h.ToggleLike)
		api.GET("/videos/:id/comments", h.ListComments)
		api.POST("/videos/:id/comments", middleware.RequireAuth(), h.CreateComment)
		api.POST("/upload", h.Upload)
	}

	lg.Info("GoTok listening", zap.String("addr", cfg.ListenAddr))
	if err := r.Run(cfg.ListenAddr); err != nil {
		// lg.Fatal would skip the deferred st.Close(); log and return instead so
		// the store closes cleanly on shutdown.
		lg.Error("server", zap.Error(err))
	}
}
