// Package config holds GoTok's configuration. Values are bound from the
// environment (or a local .env file) into the Config struct via struct tags;
// sensible defaults keep the app runnable with zero configuration.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
)

// Config holds application configuration. Fields tagged with `env:"..."` are
// populated from the process environment (or .env); defaults apply when unset.
type Config struct {
	DataDir      string `env:"GOTOK_DATA_DIR" envDefault:"data"`
	UploadDir    string `env:"GOTOK_UPLOAD_DIR"` // derived from DataDir when empty
	MaxUploadMB  int64  `env:"GOTOK_MAX_UPLOAD_MB" envDefault:"200"`
	ListenAddr   string `env:"GOTOK_LISTEN_ADDR" envDefault:":8080"`
	CookieSecret string `env:"GOTOK_COOKIE_SECRET"` // generated + persisted when empty

	// DatabaseURL is the PostgreSQL connection string. Required (no SQLite
	// fallback) to support horizontal scaling with multiple writer instances.
	DatabaseURL string `env:"GOTOK_DATABASE_URL" envDefault:"postgres://gotok:gotok@localhost:5432/gotok?sslmode=disable"`

	// RedisAddr is the address of the Redis instance used for WebSocket
	// pub/sub (cross-instance message routing) and online-presence tracking.
	RedisAddr string `env:"GOTOK_REDIS_ADDR" envDefault:"localhost:6379"`

	// Dev enables development-only features (e.g. the Swagger UI). Off by
	// default; turned on with GOTOK_DEV=true.
	Dev bool `env:"GOTOK_DEV" envDefault:"false"`
}

// Load reads a local .env file (if present), then binds the environment into a
// Config. Paths under DataDir are derived when not set, the upload directory is
// created, and the cookie secret is generated + persisted on first run so
// sessions survive restarts.
func Load() (*Config, error) {
	// godotenv.Load is a no-op (returns an error we ignore) when .env is absent,
	// e.g. in production where values come from the real environment.
	_ = godotenv.Load()

	var cfg Config
	if err := env.Parse(&cfg); err != nil {
		return nil, fmt.Errorf("parse env: %w", err)
	}

	// Derive sub-paths from DataDir unless they were set explicitly.
	if cfg.UploadDir == "" {
		cfg.UploadDir = filepath.Join(cfg.DataDir, "uploads")
	}

	if err := os.MkdirAll(cfg.UploadDir, 0o755); err != nil {
		return nil, err
	}

	// Use an explicit secret from env when provided; otherwise generate and
	// persist one so it survives restarts.
	if cfg.CookieSecret == "" {
		secret, err := loadOrCreateSecret(filepath.Join(cfg.DataDir, "cookie_secret"))
		if err != nil {
			return nil, err
		}
		cfg.CookieSecret = secret
	}

	return &cfg, nil
}

func loadOrCreateSecret(path string) (string, error) {
	if b, err := os.ReadFile(path); err == nil && len(b) >= 32 {
		return string(b), nil
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	s := hex.EncodeToString(b)
	if err := os.WriteFile(path, []byte(s), 0o600); err != nil {
		return "", err
	}
	return s, nil
}
