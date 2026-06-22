package middleware

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

// GinLogger returns a Gin access-log middleware that emits each request through
// the provided zap logger with structured fields (method, path, status, latency,
// client ip). It replaces gin.Logger() so access logs flow through zap rather
// than the standard library logger.
func GinLogger(lg *zap.Logger) gin.HandlerFunc {
	if lg == nil {
		lg = zap.NewNop()
	}
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		lg.Info("request",
			zap.String("method", c.Request.Method),
			zap.String("path", c.Request.URL.Path),
			zap.Int("status", c.Writer.Status()),
			zap.Int("bytes", c.Writer.Size()),
			zap.Duration("latency", time.Since(start)),
			zap.String("ip", c.ClientIP()),
		)
	}
}

// GinRecovery returns a Gin middleware that recovers from panics, logs them at
// Error level with a stack trace, and aborts with 500. It replaces
// gin.Recovery() so panics are reported through zap.
func GinRecovery(lg *zap.Logger) gin.HandlerFunc {
	if lg == nil {
		lg = zap.NewNop()
	}
	return func(c *gin.Context) {
		defer func() {
			if rec := recover(); rec != nil {
				lg.Error("panic recovered",
					zap.String("path", c.Request.URL.Path),
					zap.Any("error", rec),
					zap.Stack("stack"),
				)
				c.AbortWithStatus(http.StatusInternalServerError)
			}
		}()
		c.Next()
	}
}
