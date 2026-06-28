package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"github.com/hokkung/gotok/internal/middleware"
	"github.com/hokkung/gotok/internal/store"
)

// sessionTTL is how long a login session stays valid.
const sessionTTL = 30 * 24 * time.Hour

// sessionCookie mirrors middleware.SessionCookie; kept here so handlers don't
// depend on the magic string when setting/clearing the cookie.
const sessionCookie = middleware.SessionCookie

func newSessionToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return randID(16) // fall back to the helper; never return empty
	}
	return hex.EncodeToString(b)
}

func setSessionCookie(c *gin.Context, token string) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(sessionCookie, token, int(sessionTTL.Seconds()), "/", "", false, true)
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
	bid := randID(6)
	u, err := h.store.CreateOrUpdateUser(c.Request.Context(), "demo", bid, "Demo "+bid, "demo-"+bid+"@gotok.local", "")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not start demo session"})
		return
	}
	token := newSessionToken()
	if err := h.store.CreateSession(c.Request.Context(), u.ID, token, sessionTTL); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not create session"})
		return
	}
	setSessionCookie(c, token)
	c.JSON(http.StatusOK, gin.H{"ok": true, "user": u, "redirect": validNext(c.PostForm("next"), "/feed")})
}

// Login authenticates an email/password account and starts a session. A single
// generic "invalid email or password" message is returned for both an unknown
// email and a wrong password to prevent user enumeration.
func (h *Handlers) Login(c *gin.Context) {
	email := normalizeEmail(c.PostForm("email"))
	password := c.PostForm("password")
	if email == "" || password == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "email and password are required"})
		return
	}

	u, err := h.store.GetUserByEmail(c.Request.Context(), email)
	if err != nil || u.PasswordHash == "" {
		// Unknown email, or an SSO/demo account with no password set.
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid email or password"})
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid email or password"})
		return
	}

	token := newSessionToken()
	if err := h.store.CreateSession(c.Request.Context(), u.ID, token, sessionTTL); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not create session"})
		return
	}
	setSessionCookie(c, token)
	c.JSON(http.StatusOK, gin.H{"ok": true, "user": u, "redirect": validNext(c.PostForm("next"), "/feed")})
}

// Register creates a new email/password account and starts a session.
func (h *Handlers) Register(c *gin.Context) {
	name := strings.TrimSpace(c.PostForm("name"))
	email := normalizeEmail(c.PostForm("email"))
	password := c.PostForm("password")
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name is required"})
		return
	}
	if !strings.Contains(email, "@") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "a valid email is required"})
		return
	}
	if len(password) < 8 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "password must be at least 8 characters"})
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not hash password"})
		return
	}

	u, err := h.store.CreateUserWithPassword(c.Request.Context(), name, email, string(hash))
	if err != nil {
		if errors.Is(err, store.ErrEmailExists) {
			c.JSON(http.StatusConflict, gin.H{"error": "an account with that email already exists"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not create account"})
		return
	}

	token := newSessionToken()
	if err := h.store.CreateSession(c.Request.Context(), u.ID, token, sessionTTL); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "could not create session"})
		return
	}
	setSessionCookie(c, token)
	c.JSON(http.StatusOK, gin.H{"ok": true, "user": u, "redirect": validNext(c.PostForm("next"), "/feed")})
}

// normalizeEmail trims surrounding whitespace and lower-cases the email so
// lookups are case-insensitive.
func normalizeEmail(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
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
		_ = h.store.DeleteSession(c.Request.Context(), token)
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
