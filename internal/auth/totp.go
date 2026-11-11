package auth

import (
	"bytes"
	"encoding/base64"
	"image/png"
	"strings"
	"sync"
	"time"

	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"
)

const totpPeriod = 30

// NewTOTP creates a secret for an authenticator app. It returns the secret,
// the otpauth:// URL and a QR code as a PNG data URL.
func NewTOTP(issuer, account string) (secret, url, qr string, err error) {
	key, err := totp.Generate(totp.GenerateOpts{Issuer: issuer, AccountName: account, Period: totpPeriod})
	if err != nil {
		return "", "", "", err
	}
	img, err := key.Image(240, 240)
	if err != nil {
		return "", "", "", err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return "", "", "", err
	}
	return key.Secret(), key.URL(), "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes()), nil
}

// CheckTOTP validates a 6-digit code, allowing one step of clock drift either way.
// It returns the matched time step; codes at or before lastStep are rejected so a
// code can't be replayed.
func CheckTOTP(secret, code string, lastStep int64) (int64, bool) {
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	if len(code) != 6 || secret == "" {
		return 0, false
	}
	now := time.Now().UTC()
	step := now.Unix() / totpPeriod
	for _, d := range []int64{0, -1, 1} {
		s := step + d
		if s <= lastStep {
			continue
		}
		want, err := totp.GenerateCodeCustom(secret, time.Unix(s*totpPeriod, 0).UTC(),
			totp.ValidateOpts{Period: totpPeriod, Digits: otp.DigitsSix, Algorithm: otp.AlgorithmSHA1})
		if err == nil && want == code {
			return s, true
		}
	}
	return 0, false
}

// Pending holds logins that passed the password step and wait for a 2FA code.
type Pending struct {
	mu    sync.Mutex
	items map[string]pendingItem
}

type pendingItem struct {
	userID   uint
	expires  time.Time
	attempts int
	extra    string // caller data, e.g. the device info of a client login
}

const pendingTTL = 5 * time.Minute

func NewPending() *Pending { return &Pending{items: map[string]pendingItem{}} }

// Start returns a ticket for the 2FA step.
func (p *Pending) Start(userID uint, extra string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	for k, v := range p.items {
		if now.After(v.expires) {
			delete(p.items, k)
		}
	}
	t := newSecret()
	p.items[t] = pendingItem{userID: userID, expires: now.Add(pendingTTL), extra: extra}
	return t
}

// Get returns the pending login for a ticket. Each failed code counts; after five
// failures the ticket is gone and the user must start again.
func (p *Pending) Get(ticket string) (userID uint, extra string, ok bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	it, ok := p.items[ticket]
	if !ok || time.Now().After(it.expires) {
		delete(p.items, ticket)
		return 0, "", false
	}
	return it.userID, it.extra, true
}

func (p *Pending) Fail(ticket string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if it, ok := p.items[ticket]; ok {
		it.attempts++
		if it.attempts >= 5 {
			delete(p.items, ticket)
		} else {
			p.items[ticket] = it
		}
	}
}

func (p *Pending) Done(ticket string) {
	p.mu.Lock()
	delete(p.items, ticket)
	p.mu.Unlock()
}
