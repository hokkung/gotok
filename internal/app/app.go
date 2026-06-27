// Package app wires the Gin engine, middleware, and route registration for GoTok.
package app

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
	swaggerFiles "github.com/swaggo/files"
	"github.com/swaggo/gin-swagger"

	_ "github.com/hokkung/gotok/docs" // registers the generated OpenAPI spec
	"github.com/hokkung/gotok/internal/chat"
	"github.com/hokkung/gotok/internal/config"
	"github.com/hokkung/gotok/internal/handlers"
	"github.com/hokkung/gotok/internal/middleware"
	"github.com/hokkung/gotok/internal/store"
)

// Run builds the HTTP server (Gin engine, middleware, routes), starts the chat
// hub, and blocks until the context is cancelled (SIGINT/SIGTERM). On shutdown
// it gracefully drains the HTTP server and stops the hub.
func Run(ctx context.Context, cfg *config.Config, st *store.Store, lg *zap.Logger) {
	rdb := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
	if err := rdb.Ping(ctx).Err(); err != nil {
		lg.Fatal("redis ping", zap.Error(err))
	}

	broker := chat.NewRedisBroker(rdb, lg)
	hub := chat.NewHub(st, broker, chat.WithLogger(lg))

	go hub.Run(ctx)
	go hub.StartPresenceHeartbeat(ctx)

	// gin.New() gives a bare engine; we add the zap-backed logger + recovery
	// middleware (replacing gin.Default's std-library logging) and then Auth.
	r := gin.New()
	r.Use(middleware.GinLogger(lg), middleware.GinRecovery(lg))
	r.MaxMultipartMemory = 32 << 20 // 32 MiB in memory; larger spills to temp files.
	r.Use(middleware.Auth(st))
	r.LoadHTMLGlob("web/templates/*")

	h := handlers.New(cfg, st, hub, lg)

	registerRoutes(r, h)

	// The Swagger UI is a development aid; only mount it when GOTOK_DEV is set
	// so it is never exposed in a production deployment.
	if cfg.Dev {
		r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
		lg.Info("Swagger UI enabled at /swagger/index.html")
	}

	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// Start serving in a goroutine so we can block on the context for shutdown.
	go func() {
		lg.Info("GoTok listening", zap.String("addr", cfg.ListenAddr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			lg.Error("server", zap.Error(err))
		}
	}()

	<-ctx.Done()
	lg.Info("shutting down…")

	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		lg.Error("server shutdown", zap.Error(err))
	}
	_ = rdb.Close()
	lg.Info("shutdown complete")
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

	// WebSocket endpoint (auth-gated).
	r.GET("/ws", middleware.RequireAuth(), h.HandleWebSocket)

	api := r.Group("/api")
	{
		api.GET("/videos", h.ListVideos)
		api.GET("/users/:id/videos", h.ListVideosByUser)
		api.GET("/users/:id/liked", h.ListLikedVideos)
		api.GET("/users/:id/presence", h.GetPresence)
		api.GET("/me", h.Me)
		api.POST("/videos/:id/view", h.View)
		api.POST("/videos/:id/like", middleware.RequireAuth(), h.ToggleLike)
		api.GET("/videos/:id/comments", h.ListComments)
		api.POST("/videos/:id/comments", middleware.RequireAuth(), h.CreateComment)
		api.POST("/upload", middleware.RequireAuth(), h.Upload)
		api.POST("/profile", middleware.RequireAuth(), h.EditProfile)

		// Chat API (all auth-gated).
		chatAPI := api.Group("", middleware.RequireAuth())
		{
			chatAPI.GET("/conversations", h.ListConversations)
			chatAPI.POST("/conversations", h.CreateConversation)
			chatAPI.GET("/conversations/:id/messages", h.ListMessages)
			chatAPI.POST("/conversations/:id/messages", h.SendMessage)
			chatAPI.POST("/conversations/:id/read", h.MarkConversationRead)
		}
	}
}
