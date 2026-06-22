package main

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"

	"live/internal/config"
	"live/internal/handlers"
	"live/internal/middleware"
	"live/internal/store"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	st, err := store.New(cfg.DBPath)
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	defer st.Close()

	r := gin.Default()
	r.MaxMultipartMemory = 32 << 20 // 32 MiB in memory; larger spills to temp files.
	r.Use(middleware.Auth(st))
	r.LoadHTMLGlob("web/templates/*")

	h := handlers.New(cfg, st)

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

	log.Printf("GoTok listening on http://localhost%s", cfg.ListenAddr)
	if err := r.Run(cfg.ListenAddr); err != nil {
		log.Fatalf("server: %v", err)
	}
}
