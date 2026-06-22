// Package config holds GoTok's configuration, loaded from sensible defaults
// (ports, paths, upload limits) and a persisted cookie secret.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
)

// Config holds application configuration.
type Config struct {
	DataDir      string
	UploadDir    string
	DBPath       string
	MaxUploadMB  int64
	ListenAddr   string
	CookieSecret string
}

// Load returns a config with sensible defaults. The cookie secret is generated
// randomly on first run and persisted so anonymous client ids survive restarts.
func Load() (*Config, error) {
	dataDir := "data"
	uploadDir := filepath.Join(dataDir, "uploads")
	dbPath := filepath.Join(dataDir, "app.db")

	if err := os.MkdirAll(uploadDir, 0o755); err != nil {
		return nil, err
	}

	secret, err := loadOrCreateSecret(filepath.Join(dataDir, "cookie_secret"))
	if err != nil {
		return nil, err
	}

	return &Config{
		DataDir:      dataDir,
		UploadDir:    uploadDir,
		DBPath:       dbPath,
		MaxUploadMB:  200,
		ListenAddr:   ":8080",
		CookieSecret: secret,
	}, nil
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
