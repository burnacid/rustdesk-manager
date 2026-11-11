package auth

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"gorm.io/gorm"

	"rustdesk-manager/internal/db"
)

// Passkeys handles WebAuthn sign-in for the web console. The relying party is
// the host of PUBLIC_URL, so passkeys only work when the console is opened at
// that address (and browsers require HTTPS, except on localhost).
type Passkeys struct {
	wa *webauthn.WebAuthn
	db *gorm.DB

	mu       sync.Mutex
	sessions map[string]ceremony
}

type ceremony struct {
	data    webauthn.SessionData
	userID  uint // 0 for a login
	expires time.Time
}

const (
	ceremonyTTL   = 5 * time.Minute
	maxCeremonies = 1000
)

var ErrPasskey = errors.New("passkey not accepted")

func NewPasskeys(gdb *gorm.DB, publicURL string) (*Passkeys, error) {
	u, err := url.Parse(publicURL)
	if err != nil || u.Hostname() == "" {
		return nil, errors.New("PUBLIC_URL is not a valid URL")
	}
	wa, err := webauthn.New(&webauthn.Config{
		RPID:          u.Hostname(),
		RPDisplayName: "RustDesk Manager",
		RPOrigins:     []string{u.Scheme + "://" + u.Host},
	})
	if err != nil {
		return nil, err
	}
	return &Passkeys{wa: wa, db: gdb, sessions: map[string]ceremony{}}, nil
}

// waUser adapts a user and their stored passkeys to the library.
type waUser struct {
	u     *db.User
	creds []webauthn.Credential
}

func userHandle(id uint) []byte { return []byte(strconv.FormatUint(uint64(id), 10)) }

func (w *waUser) WebAuthnID() []byte                         { return userHandle(w.u.ID) }
func (w *waUser) WebAuthnName() string                       { return w.u.Name }
func (w *waUser) WebAuthnCredentials() []webauthn.Credential { return w.creds }
func (w *waUser) WebAuthnDisplayName() string {
	if w.u.DisplayName != "" {
		return w.u.DisplayName
	}
	return w.u.Name
}

func (p *Passkeys) loadUser(u *db.User) (*waUser, error) {
	var rows []db.Passkey
	if err := p.db.Where("user_id = ?", u.ID).Find(&rows).Error; err != nil {
		return nil, err
	}
	out := &waUser{u: u}
	for _, r := range rows {
		var c webauthn.Credential
		if err := json.Unmarshal([]byte(r.Data), &c); err == nil {
			out.creds = append(out.creds, c)
		}
	}
	return out, nil
}

func (p *Passkeys) save(userID uint, data *webauthn.SessionData) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now()
	for k, c := range p.sessions {
		if now.After(c.expires) {
			delete(p.sessions, k)
		}
	}
	if len(p.sessions) >= maxCeremonies {
		for k := range p.sessions { // under a flood, drop an arbitrary one
			delete(p.sessions, k)
			break
		}
	}
	ticket := newSecret()
	p.sessions[ticket] = ceremony{data: *data, userID: userID, expires: now.Add(ceremonyTTL)}
	return ticket
}

// take returns and removes a ceremony; each ticket works once.
func (p *Passkeys) take(ticket string, userID uint) (webauthn.SessionData, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	c, ok := p.sessions[ticket]
	delete(p.sessions, ticket)
	if !ok || time.Now().After(c.expires) || c.userID != userID {
		return webauthn.SessionData{}, false
	}
	return c.data, true
}

func credID(id []byte) string { return base64.RawURLEncoding.EncodeToString(id) }

// BeginRegister starts adding a passkey for a signed-in user.
func (p *Passkeys) BeginRegister(u *db.User) (*protocol.CredentialCreation, string, error) {
	wu, err := p.loadUser(u)
	if err != nil {
		return nil, "", err
	}
	exclude := make([]protocol.CredentialDescriptor, 0, len(wu.creds))
	for _, c := range wu.creds {
		exclude = append(exclude, c.Descriptor())
	}
	opts, data, err := p.wa.BeginRegistration(wu,
		webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired),
		webauthn.WithExclusions(exclude))
	if err != nil {
		return nil, "", err
	}
	return opts, p.save(u.ID, data), nil
}

// FinishRegister checks the browser's answer and stores the new passkey.
func (p *Passkeys) FinishRegister(u *db.User, ticket, name string, credential []byte) (*db.Passkey, error) {
	data, ok := p.take(ticket, u.ID)
	if !ok {
		return nil, ErrPasskey
	}
	parsed, err := protocol.ParseCredentialCreationResponseBytes(credential)
	if err != nil {
		return nil, ErrPasskey
	}
	wu, err := p.loadUser(u)
	if err != nil {
		return nil, err
	}
	cred, err := p.wa.CreateCredential(wu, data, parsed)
	if err != nil {
		return nil, ErrPasskey
	}
	blob, err := json.Marshal(cred)
	if err != nil {
		return nil, err
	}
	row := &db.Passkey{UserID: u.ID, Name: name, CredentialID: credID(cred.ID), Data: string(blob)}
	if err := p.db.Create(row).Error; err != nil {
		return nil, err
	}
	return row, nil
}

// BeginLogin starts a username-less sign-in.
func (p *Passkeys) BeginLogin() (*protocol.CredentialAssertion, string, error) {
	opts, data, err := p.wa.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		return nil, "", err
	}
	return opts, p.save(0, data), nil
}

// FinishLogin checks the browser's answer and returns the signed-in user.
// The caller still has to check the user's status.
func (p *Passkeys) FinishLogin(ticket string, credential []byte) (*db.User, error) {
	data, ok := p.take(ticket, 0)
	if !ok {
		return nil, ErrPasskey
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(credential)
	if err != nil {
		return nil, ErrPasskey
	}
	var found *waUser
	handler := func(rawID, handle []byte) (webauthn.User, error) {
		id, err := strconv.ParseUint(string(handle), 10, 32)
		if err != nil {
			return nil, ErrPasskey
		}
		var u db.User
		if err := p.db.First(&u, uint(id)).Error; err != nil {
			return nil, ErrPasskey
		}
		wu, err := p.loadUser(&u)
		if err != nil {
			return nil, err
		}
		found = wu
		return wu, nil
	}
	cred, err := p.wa.ValidateDiscoverableLogin(handler, data, parsed)
	if err != nil || found == nil {
		return nil, ErrPasskey
	}
	blob, _ := json.Marshal(cred) // keeps the sign counter current
	now := time.Now().UTC()
	p.db.Model(&db.Passkey{}).Where("user_id = ? AND credential_id = ?", found.u.ID, credID(cred.ID)).
		Updates(map[string]any{"data": string(blob), "last_used_at": now})
	return found.u, nil
}
