package handlers

import (
	"errors"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/hokkung/gotok/internal/middleware"
	"github.com/hokkung/gotok/internal/service"
	"github.com/hokkung/gotok/internal/store"
)

// sessionCookie mirrors middleware.SessionCookie; kept here so handlers don't
// depend on the magic string when setting/clearing the cookie.
const sessionCookie = middleware.SessionCookie

func setSessionCookie(c *gin.Context, token string) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(sessionCookie, token, int(service.SessionTTL.Seconds()), "/", "", false, true)
}

func clearSessionCookie(c *gin.Context) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(sessionCookie, "", -1, "/", "", false, true)
}

// LoginPage renders the SSO login page. Google and Facebook buttons are wired to
// placeholder endpoints that return "coming soon"; only the demo login works
// until real SSO providers are implemented.
func (h *Handlers) LoginPage(c *gin.Context) {
	data := h.base(c, "Log in")
	data["Next"] = validNext(c.Query("next"), "/feed")
	c.HTML(http.StatusOK, "login.html", data)
}

// Me godoc
//
//	@Summary		Current user
//	@Description	Returns the currently logged-in user, or null when the request is anonymous.
//	@Tags			auth
//	@Produce		json
//	@Success		200	{object}	MeResponse
//	@Router			/api/me [get]
func (h *Handlers) Me(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"user": middleware.UserFromContext(c)})
}

// LoginDemo godoc
//
//	@Summary		Demo login
//	@Description	Creates (or reuses) a "demo" user and starts a session cookie so the auth-gated actions (like/comment) can be exercised. Returns the user and a redirect target.
//	@Tags			auth
//	@Accept			mpfd
//	@Produce		json
//	@Param			next	formData	string	false	"Relative path to redirect to after login"
//	@Success		200		{object}	LoginDemoResponse
//	@Failure		500		{object}	ErrorResponse
//	@Router			/auth/demo [post]
func (h *Handlers) LoginDemo(c *gin.Context) {
	u, token, err := h.auth.LoginDemo(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not start demo session"})
		return
	}
	setSessionCookie(c, token)
	c.JSON(http.StatusOK, gin.H{"ok": true, "user": u, "redirect": validNext(c.PostForm("next"), "/feed")})
}

// Login authenticates an email/password account and starts a session. A single
// generic "invalid email or password" message is returned for both an unknown
// email and a wrong password to prevent user enumeration.
func (h *Handlers) Login(c *gin.Context) {
	u, token, err := h.auth.LoginWithPassword(c.Request.Context(), c.PostForm("email"), c.PostForm("password"))
	if err != nil {
		switch {
		case errors.Is(err, service.ErrMissingCredentials):
			c.JSON(http.StatusBadRequest, gin.H{"error": "email and password are required"})
		case errors.Is(err, service.ErrInvalidCredentials):
			c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid email or password"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "could not log in"})
		}
		return
	}
	setSessionCookie(c, token)
	c.JSON(http.StatusOK, gin.H{"ok": true, "user": u, "redirect": validNext(c.PostForm("next"), "/feed")})
}

// Register creates a new email/password account and starts a session.
func (h *Handlers) Register(c *gin.Context) {
	u, token, err := h.auth.Register(c.Request.Context(), c.PostForm("name"), c.PostForm("email"), c.PostForm("password"))
	if err != nil {
		switch {
		case errors.Is(err, service.ErrNameRequired):
			c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
		case errors.Is(err, service.ErrInvalidEmail):
			c.JSON(http.StatusBadRequest, gin.H{"error": "a valid email is required"})
		case errors.Is(err, service.ErrPasswordTooShort):
			c.JSON(http.StatusBadRequest, gin.H{"error": "password must be at least 8 characters"})
		case errors.Is(err, store.ErrEmailExists):
			c.JSON(http.StatusConflict, gin.H{"error": "an account with that email already exists"})
		default:
			c.JSON(http.StatusInternalServerError, gin.H{"error": "could not create account"})
		}
		return
	}
	setSessionCookie(c, token)
	c.JSON(http.StatusOK, gin.H{"ok": true, "user": u, "redirect": validNext(c.PostForm("next"), "/feed")})
}

// LoginGoogle godoc
//
//	@Summary		Google SSO (not implemented)
//	@Description	Placeholder for Google sign-in. Always returns 501 until real SSO is wired up.
//	@Tags			auth
//	@Produce		json
//	@Failure		501	{object}	ErrorResponse
//	@Router			/auth/google [post]
func (h *Handlers) LoginGoogle(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{"error": "Google sign-in is coming soon"})
}

// LoginFacebook godoc
//
//	@Summary		Facebook SSO (not implemented)
//	@Description	Placeholder for Facebook sign-in. Always returns 501 until real SSO is wired up.
//	@Tags			auth
//	@Produce		json
//	@Failure		501	{object}	ErrorResponse
//	@Router			/auth/facebook [post]
func (h *Handlers) LoginFacebook(c *gin.Context) {
	c.JSON(http.StatusNotImplemented, gin.H{"error": "Facebook sign-in is coming soon"})
}

// Logout ends the current session (if any) and clears the cookie.
func (h *Handlers) Logout(c *gin.Context) {
	if token, err := c.Cookie(sessionCookie); err == nil && token != "" {
		_ = h.auth.Logout(c.Request.Context(), token)
	}
	clearSessionCookie(c)
	c.Redirect(http.StatusFound, "/feed")
}

// validNext returns next only if it is a safe relative path (starts with "/" but
// not "//"), preventing open-redirect via the next parameter. Otherwise fallback.
func validNext(next, fallback string) string {
	if len(next) > 0 && next[0] == '/' && !strings.HasPrefix(next, "//") {
		return next
	}
	return fallback
}
