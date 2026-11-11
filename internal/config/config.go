// Package config loads settings from environment variables.
package config

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	Listen        string // address the server binds to, e.g. ":21114"
	DBDriver      string // "sqlite" or "mysql"
	DBDSN         string // MySQL/MariaDB DSN, or SQLite file path
	DataDir       string // holds the SQLite file and recordings
	PublicURL     string // external URL of this server, used for OIDC redirects
	AdminUser     string // initial admin username (only used when no users exist)
	AdminPassword string // initial admin password; generated and logged if empty
	SessionSecret string // signs admin UI session cookies
	LogLevel      string
	TrustProxy    bool // use X-Forwarded-For / X-Real-IP (only behind your own reverse proxy)
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func Load() (*Config, error) {
	c := &Config{
		Listen:        env("LISTEN", ":21114"),
		DBDriver:      strings.ToLower(env("DB_DRIVER", "sqlite")),
		DBDSN:         env("DB_DSN", ""),
		DataDir:       env("DATA_DIR", "./data"),
		PublicURL:     strings.TrimRight(env("PUBLIC_URL", "http://localhost:21114"), "/"),
		AdminUser:     env("ADMIN_USER", "admin"),
		AdminPassword: env("ADMIN_INITIAL_PASSWORD", ""),
		SessionSecret: env("SESSION_SECRET", ""),
		LogLevel:      env("LOG_LEVEL", "info"),
		TrustProxy:    env("TRUST_PROXY", "false") == "true",
	}
	if c.DBDriver != "sqlite" && c.DBDriver != "mysql" {
		return nil, fmt.Errorf("DB_DRIVER must be sqlite or mysql, got %q", c.DBDriver)
	}
	if err := os.MkdirAll(c.RecordingsDir(), 0o750); err != nil {
		return nil, fmt.Errorf("create data dir: %w (DATA_DIR=%s must be writable by the user the server runs as; "+
			"the Docker image runs as uid 65532, so use a named volume or chown a mounted folder to 65532)", err, c.DataDir)
	}
	if c.DBDriver == "sqlite" && c.DBDSN == "" {
		c.DBDSN = filepath.Join(c.DataDir, "rustdesk-manager.db")
	}
	if c.DBDriver == "mysql" && c.DBDSN == "" {
		return nil, fmt.Errorf("DB_DSN is required for mysql, e.g. user:pass@tcp(db:3306)/rustdesk?parseTime=true&charset=utf8mb4")
	}
	if c.SessionSecret == "" {
		// Persist a generated secret so sessions survive restarts.
		secret, err := loadOrCreateSecret(filepath.Join(c.DataDir, "session.secret"))
		if err != nil {
			return nil, err
		}
		c.SessionSecret = secret
	}
	return c, nil
}

func (c *Config) RecordingsDir() string { return filepath.Join(c.DataDir, "recordings") }

func loadOrCreateSecret(path string) (string, error) {
	if b, err := os.ReadFile(path); err == nil && len(b) > 0 {
		return strings.TrimSpace(string(b)), nil
	}
	s := RandomHex(32)
	if err := os.WriteFile(path, []byte(s), 0o600); err != nil {
		return "", fmt.Errorf("write session secret: %w", err)
	}
	return s, nil
}

// RandomHex returns n random bytes encoded as hex.
func RandomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
