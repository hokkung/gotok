// Package middleware provides Gin HTTP middleware for GoTok, notably the
// session-based auth that loads the logged-in user into the request context.
package middleware

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"live/internal/models"
	"live/internal/store"
)

// SessionCookie is the name of the auth session cookie.
const SessionCookie = "session"

// UserKey is the gin.Context key holding the logged-in *models.User (absent
// when the request is anonymous).
const UserKey = "user"

// Auth loads the logged-in user (if any) from the session cookie and stores it
// in the request context. It never blocks a request — gating is done by
// RequireAuth on specific routes.
func Auth(st *store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		if token, err := c.Cookie(SessionCookie); err == nil && token != "" {
			if u, err := st.GetUserBySession(token); err == nil && u != nil {
				c.Set(UserKey, u)
			}
		}
		c.Next()
	}
}

// UserFromContext returns the logged-in user, or nil if the request is
// anonymous.
func UserFromContext(c *gin.Context) *models.User {
	if v, ok := c.Get(UserKey); ok {
		if u, ok := v.(*models.User); ok {
			return u
		}
	}
	return nil
}

// RequireAuth aborts with 401 when there is no logged-in user. The frontend
// treats 401 as "redirect to /login".
func RequireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		if UserFromContext(c) == nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "login required"})
			return
		}
		c.Next()
	}
}
