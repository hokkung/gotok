package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/hokkung/gotok/internal/models"
)

// SessionTTL is how long a login session stays valid. Exported so handlers can
// keep the cookie max-age in sync.
const SessionTTL = 30 * 24 * time.Hour

// Auth-domain errors returned by AuthService methods.
var (
	// ErrMissingCredentials is returned when email or password is empty.
	ErrMissingCredentials = errors.New("email and password are required")
	// ErrInvalidCredentials is returned for an unknown email or wrong password.
	// The same message is used for both to prevent user enumeration.
	ErrInvalidCredentials = errors.New("invalid email or password")
	// ErrNameRequired is returned when the display name is empty.
	ErrNameRequired = errors.New("name is required")
	// ErrInvalidEmail is returned when the email is not a valid address.
	ErrInvalidEmail = errors.New("a valid email is required")
	// ErrPasswordTooShort is returned when the password is under 8 characters.
	ErrPasswordTooShort = errors.New("password must be at least 8 characters")
)

// AuthStore is the persistence interface consumed by AuthService.
type AuthStore interface {
	GetUserByEmail(ctx context.Context, email string) (*models.User, error)
	CreateUserWithPassword(ctx context.Context, name, email, passwordHash string) (*models.User, error)
	CreateOrUpdateUser(ctx context.Context, provider, providerUserID, name, email, avatarURL string) (*models.User, error)
	CreateSession(ctx context.Context, userID int64, token string, ttl time.Duration) error
	DeleteSession(ctx context.Context, token string) error
}

// AuthService handles authentication, registration, and session management.
type AuthService struct {
	store AuthStore
}

// NewAuthService creates an AuthService backed by the given store.
func NewAuthService(s AuthStore) *AuthService {
	return &AuthService{store: s}
}

// LoginWithPassword authenticates an email/password account and starts a new
// session. Returns the user and a session token. Returns ErrMissingCredentials
// for empty fields and ErrInvalidCredentials for unknown email or wrong
// password.
func (s *AuthService) LoginWithPassword(ctx context.Context, email, password string) (*models.User, string, error) {
	email = normalizeEmail(email)
	if email == "" || password == "" {
		return nil, "", ErrMissingCredentials
	}

	u, err := s.store.GetUserByEmail(ctx, email)
	if err != nil || u.PasswordHash == "" {
		return nil, "", ErrInvalidCredentials
	}
	if err := bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)); err != nil {
		return nil, "", ErrInvalidCredentials
	}

	token, err := s.startSession(ctx, u.ID)
	if err != nil {
		return nil, "", fmt.Errorf("create session: %w", err)
	}
	return u, token, nil
}

// Register creates a new email/password account and starts a session. Returns
// the user and session token. Validation errors are returned for invalid input;
// the store's ErrEmailExists propagates for duplicate emails.
func (s *AuthService) Register(ctx context.Context, name, email, password string) (*models.User, string, error) {
	name = strings.TrimSpace(name)
	email = normalizeEmail(email)
	if name == "" {
		return nil, "", ErrNameRequired
	}
	if !strings.Contains(email, "@") {
		return nil, "", ErrInvalidEmail
	}
	if len(password) < 8 {
		return nil, "", ErrPasswordTooShort
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, "", fmt.Errorf("hash password: %w", err)
	}

	u, err := s.store.CreateUserWithPassword(ctx, name, email, string(hash))
	if err != nil {
		return nil, "", fmt.Errorf("create user: %w", err)
	}

	token, err := s.startSession(ctx, u.ID)
	if err != nil {
		return nil, "", fmt.Errorf("create session: %w", err)
	}
	return u, token, nil
}

// LoginDemo creates (or reuses) a "demo" user and starts a session. Returns
// the user and session token.
func (s *AuthService) LoginDemo(ctx context.Context) (*models.User, string, error) {
	bid := randomID(6)
	u, err := s.store.CreateOrUpdateUser(ctx, "demo", bid, "Demo "+bid, "demo-"+bid+"@gotok.local", "")
	if err != nil {
		return nil, "", fmt.Errorf("create demo user: %w", err)
	}

	token, err := s.startSession(ctx, u.ID)
	if err != nil {
		return nil, "", fmt.Errorf("create session: %w", err)
	}
	return u, token, nil
}

// Logout deletes the session associated with the given token.
func (s *AuthService) Logout(ctx context.Context, token string) error {
	return s.store.DeleteSession(ctx, token)
}

// startSession generates a new session token and persists it for the given user.
func (s *AuthService) startSession(ctx context.Context, userID int64) (string, error) {
	token := newSessionToken()
	if err := s.store.CreateSession(ctx, userID, token, SessionTTL); err != nil {
		return "", err
	}
	return token, nil
}

// newSessionToken generates a 64-character hex token from 32 random bytes.
func newSessionToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return randomID(16)
	}
	return hex.EncodeToString(b)
}

// randomID returns a short hex id from n random bytes.
func randomID(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "000000"
	}
	return hex.EncodeToString(b)
}

// normalizeEmail trims surrounding whitespace and lower-cases the email so
// lookups are case-insensitive.
func normalizeEmail(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}
