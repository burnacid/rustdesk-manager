// Package auth handles passwords, opaque tokens and login throttling.
package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	"rustdesk-manager/internal/db"
)

const (
	KindClient = "client" // RustDesk app login (Bearer token)
	KindWeb    = "web"    // admin UI session (cookie)
	KindAPI    = "api"    // admin API token for scripts (Bearer)

	WebSessionTTL = 12 * time.Hour
)

var ErrInvalid = errors.New("invalid credentials")

// dummyHash keeps login timing similar for unknown users.
var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("dummy-password"), bcrypt.DefaultCost)

func HashPassword(pw string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(h), err
}

// CheckPassword returns the user when name and password match an enabled account.
func CheckPassword(gdb *gorm.DB, name, password string) (*db.User, error) {
	var u db.User
	err := gdb.Where("name = ?", strings.TrimSpace(name)).Limit(1).Find(&u).Error
	if err != nil {
		return nil, err
	}
	if u.ID == 0 || u.PasswordHash == "" {
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return nil, ErrInvalid
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		return nil, ErrInvalid
	}
	if u.Status == db.UserDisabled {
		return nil, errors.New("user is disabled")
	}
	return &u, nil
}

// NewSecret returns 32 random bytes, URL-safe base64 encoded.
func NewSecret() string { return newSecret() }

func newSecret() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

func HashToken(t string) string {
	sum := sha256.Sum256([]byte(t))
	return hex.EncodeToString(sum[:])
}

// IssueToken stores a new token row and returns the plain token (shown once).
func IssueToken(gdb *gorm.DB, t *db.Token) (string, error) {
	plain := newSecret()
	t.Hash = HashToken(plain)
	t.LastUsedAt = time.Now().UTC()
	if err := gdb.Create(t).Error; err != nil {
		return "", err
	}
	return plain, nil
}

// Lookup resolves a plain token of the given kind to its row and user.
func Lookup(gdb *gorm.DB, kind, plain string) (*db.Token, error) {
	if plain == "" {
		return nil, ErrInvalid
	}
	var t db.Token
	err := gdb.Preload("User").Where("hash = ? AND kind = ?", HashToken(plain), kind).Limit(1).Find(&t).Error
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	if t.ID == 0 || (t.ExpiresAt != nil && now.After(*t.ExpiresAt)) || t.User.Status == db.UserDisabled {
		return nil, ErrInvalid
	}
	// Avoid a write on every request: refresh last-used at most once a minute.
	if now.Sub(t.LastUsedAt) > time.Minute {
		gdb.Model(&t).Update("last_used_at", now)
	}
	return &t, nil
}

func Revoke(gdb *gorm.DB, plain string) error {
	return gdb.Where("hash = ?", HashToken(plain)).Delete(&db.Token{}).Error
}

// Limiter throttles failed logins per key (IP or username).
type Limiter struct {
	mu       sync.Mutex
	failures map[string][]time.Time
	Max      int
	Window   time.Duration
}

func NewLimiter(max int, window time.Duration) *Limiter {
	return &Limiter{failures: map[string][]time.Time{}, Max: max, Window: window}
}

func (l *Limiter) prune(key string, now time.Time) []time.Time {
	kept := l.failures[key][:0]
	for _, t := range l.failures[key] {
		if now.Sub(t) < l.Window {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		delete(l.failures, key)
		return nil
	}
	l.failures[key] = kept
	return kept
}

func (l *Limiter) Blocked(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.prune(key, time.Now())) >= l.Max
}

func (l *Limiter) Fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.failures[key] = append(l.prune(key, time.Now()), time.Now())
}

func (l *Limiter) Reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.failures, key)
}
