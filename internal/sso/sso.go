// Package sso implements single sign-on with any OpenID Connect provider, for the
// RustDesk app and for the web console.
//
// RustDesk app flow (reference/rustdesk/src/hbbs_http/account.rs):
//  1. POST /api/oidc/auth {op, id, uuid, deviceInfo} -> {code, url}; the app opens url.
//  2. The browser goes to the provider and comes back to /api/oidc/callback.
//  3. The app polls GET /api/oidc/auth-query?code=&id=&uuid= every second. Until the
//     login is done we answer {"error":"No authed oidc is found"}, which means "keep waiting".
package sso

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
	"gorm.io/gorm"

	"rustdesk-manager/internal/db"
	"rustdesk-manager/internal/httpx"
	"rustdesk-manager/internal/metrics"
	"rustdesk-manager/internal/secret"
)

const (
	KindClient = "client"
	KindWeb    = "web"

	sessionTTL = 10 * time.Minute
	// NotYet is the message the RustDesk app treats as "still waiting".
	NotYet = "No authed oidc is found"
)

// Hooks connect SSO to the parts that create logins.
type Hooks struct {
	// ClientLogin returns the RustDesk app login response for a user.
	ClientLogin func(u *db.User, deviceID, deviceUUID, deviceInfo string) (map[string]any, error)
	// WebLogin starts a web console session (sets the cookie).
	WebLogin func(w http.ResponseWriter, r *http.Request, u *db.User) error
}

type Service struct {
	db        *gorm.DB
	box       *secret.Box
	publicURL string
	hooks     Hooks

	mu    sync.Mutex
	cache map[uint]cached
}

type cached struct {
	updated  time.Time
	provider *oidc.Provider
	oauth    oauth2.Config
	verifier *oidc.IDTokenVerifier
}

func New(gdb *gorm.DB, box *secret.Box, publicURL string) *Service {
	return &Service{db: gdb, box: box, publicURL: publicURL, cache: map[uint]cached{}}
}

func (s *Service) SetHooks(h Hooks) { s.hooks = h }

// RedirectURL is what admins register at their identity provider.
func (s *Service) RedirectURL() string { return s.publicURL + "/api/oidc/callback" }

func random(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// Enabled returns the enabled providers, by name.
func (s *Service) Enabled() []db.OIDCProvider {
	var ps []db.OIDCProvider
	s.db.Where("enabled = ?", true).Order("name").Find(&ps)
	return ps
}

// LoginOptions is the /api/login-options reply. The client reads entries as
// objects with "name" and "icon", which it only gets from a "common-oidc/<json>" item.
func (s *Service) LoginOptions() []string {
	type op struct {
		Name string  `json:"name"`
		Icon *string `json:"icon"`
	}
	var ops []op
	for _, p := range s.Enabled() {
		ops = append(ops, op{Name: p.Name})
	}
	if len(ops) == 0 {
		return []string{}
	}
	b, _ := json.Marshal(ops)
	return []string{"common-oidc/" + string(b)}
}

func (s *Service) client(ctx context.Context, p *db.OIDCProvider) (cached, error) {
	s.mu.Lock()
	c, ok := s.cache[p.ID]
	s.mu.Unlock()
	if ok && c.updated.Equal(p.UpdatedAt) {
		return c, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	provider, err := oidc.NewProvider(ctx, p.Issuer)
	if err != nil {
		return cached{}, fmt.Errorf("can't reach the identity provider %s: %w", p.Issuer, err)
	}
	clientSecret, err := s.box.Open(p.ClientSecret)
	if err != nil {
		return cached{}, err
	}
	scopes := []string{oidc.ScopeOpenID, "email", "profile"}
	for _, sc := range strings.Fields(strings.ReplaceAll(p.Scopes, ",", " ")) {
		scopes = append(scopes, sc)
	}
	c = cached{
		updated:  p.UpdatedAt,
		provider: provider,
		oauth: oauth2.Config{ClientID: p.ClientID, ClientSecret: clientSecret, Endpoint: provider.Endpoint(),
			RedirectURL: s.RedirectURL(), Scopes: scopes},
		verifier: provider.Verifier(&oidc.Config{ClientID: p.ClientID}),
	}
	s.mu.Lock()
	s.cache[p.ID] = c
	s.mu.Unlock()
	return c, nil
}

// Begin starts a login and returns its code. Visiting StartURL(code) sends the
// browser to the provider.
func (s *Service) Begin(kind, providerName, deviceID, deviceUUID, deviceInfo string) (string, error) {
	var p db.OIDCProvider
	if err := s.db.Where("name = ? AND enabled = ?", providerName, true).Limit(1).Find(&p).Error; err != nil {
		return "", err
	}
	if p.ID == 0 {
		return "", errors.New("Unknown login provider")
	}
	s.db.Where("expires_at < ?", time.Now().UTC()).Delete(&db.OIDCSession{})
	sess := db.OIDCSession{
		Code: random(24), State: random(24), Kind: kind, ProviderID: p.ID,
		DeviceID: deviceID, DeviceUUID: deviceUUID, DeviceInfo: deviceInfo,
		Verifier: oauth2.GenerateVerifier(), Nonce: random(16), ExpiresAt: time.Now().UTC().Add(sessionTTL),
	}
	return sess.Code, s.db.Create(&sess).Error
}

func (s *Service) StartURL(code string) string { return s.publicURL + "/api/oidc/start?code=" + code }

func (s *Service) session(where string, arg string) (*db.OIDCSession, error) {
	var sess db.OIDCSession
	if err := s.db.Where(where, arg).Limit(1).Find(&sess).Error; err != nil {
		return nil, err
	}
	if sess.Code == "" || time.Now().UTC().After(sess.ExpiresAt) {
		return nil, errors.New("This login link has expired. Please start again.")
	}
	return &sess, nil
}

// GET /api/oidc/start?code= : redirect to the provider.
func (s *Service) HandleStart(w http.ResponseWriter, r *http.Request) {
	sess, err := s.session("code = ?", r.URL.Query().Get("code"))
	if err != nil {
		s.page(w, http.StatusBadRequest, "Sign-in failed", err.Error())
		return
	}
	var p db.OIDCProvider
	if s.db.First(&p, sess.ProviderID).Error != nil || !p.Enabled {
		s.page(w, http.StatusBadRequest, "Sign-in failed", "This login provider is no longer available.")
		return
	}
	c, err := s.client(r.Context(), &p)
	if err != nil {
		slog.Error("oidc provider", "provider", p.Name, "err", err)
		s.page(w, http.StatusBadGateway, "Sign-in failed", "The identity provider can't be reached. Ask your administrator to check its settings.")
		return
	}
	url := c.oauth.AuthCodeURL(sess.State, oidc.Nonce(sess.Nonce), oauth2.S256ChallengeOption(sess.Verifier))
	http.Redirect(w, r, url, http.StatusFound)
}

type claims struct {
	Subject           string `json:"sub"`
	Email             string `json:"email"`
	EmailVerified     *bool  `json:"email_verified"`
	PreferredUsername string `json:"preferred_username"`
	Name              string `json:"name"`
}

// GET /api/oidc/callback?state=&code= : finish the login.
func (s *Service) HandleCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	sess, err := s.session("state = ?", q.Get("state"))
	if err != nil {
		s.page(w, http.StatusBadRequest, "Sign-in failed", err.Error())
		return
	}
	fail := func(public string, logErr error) {
		if logErr != nil {
			metrics.Login("sso", false)
			slog.Warn("sso login failed", "err", logErr)
		}
		if sess.Kind == KindWeb {
			s.db.Delete(sess)
			http.Redirect(w, r, "/?sso_error="+template.URLQueryEscaper(public), http.StatusFound)
			return
		}
		s.db.Model(sess).Update("error", public) // the app picks this up with auth-query
		s.page(w, http.StatusForbidden, "Sign-in failed", public)
	}
	if e := q.Get("error"); e != "" {
		fail("The identity provider refused the login: "+q.Get("error_description"), errors.New(e))
		return
	}
	var p db.OIDCProvider
	if err := s.db.First(&p, sess.ProviderID).Error; err != nil {
		fail("This login provider is no longer available.", err)
		return
	}
	c, err := s.client(r.Context(), &p)
	if err != nil {
		fail("The identity provider can't be reached.", err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	tok, err := c.oauth.Exchange(ctx, q.Get("code"), oauth2.VerifierOption(sess.Verifier))
	if err != nil {
		fail("The login could not be completed.", err)
		return
	}
	rawID, _ := tok.Extra("id_token").(string)
	idt, err := c.verifier.Verify(ctx, rawID)
	if err != nil || idt.Nonce != sess.Nonce {
		fail("The identity provider's answer could not be verified.", err)
		return
	}
	var cl claims
	if err := idt.Claims(&cl); err != nil {
		fail("The identity provider's answer could not be read.", err)
		return
	}
	u, err := s.resolveUser(&p, &cl)
	if err != nil {
		fail(err.Error(), nil)
		return
	}
	metrics.Login("sso", true)
	slog.Info("sso login", "provider", p.Name, "user", u.Name, "kind", sess.Kind)

	if sess.Kind == KindWeb {
		s.db.Delete(sess)
		if err := s.hooks.WebLogin(w, r, u); err != nil {
			fail("Server error", err)
			return
		}
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	body, err := s.hooks.ClientLogin(u, sess.DeviceID, sess.DeviceUUID, sess.DeviceInfo)
	if err != nil {
		fail("Server error", err)
		return
	}
	b, _ := json.Marshal(body)
	s.db.Model(sess).Update("result", string(b))
	s.page(w, http.StatusOK, "Signed in", "You are signed in to RustDesk as "+u.Name+". You can close this window.")
}

var nameChars = regexp.MustCompile(`[^a-zA-Z0-9._@-]+`)

// resolveUser finds the user for a provider account: an existing link, else a user
// with the same verified email (then linked), else a new user if the provider allows it.
func (s *Service) resolveUser(p *db.OIDCProvider, cl *claims) (*db.User, error) {
	if cl.Subject == "" {
		return nil, errors.New("The identity provider didn't send an account id.")
	}
	var link db.OIDCIdentity
	s.db.Where("provider_id = ? AND subject = ?", p.ID, cl.Subject).Limit(1).Find(&link)
	var u db.User
	if link.ID != 0 {
		s.db.Limit(1).Find(&u, link.UserID)
	}
	email := strings.ToLower(strings.TrimSpace(cl.Email))
	verified := (cl.EmailVerified != nil && *cl.EmailVerified) || p.TrustEmail
	if u.ID == 0 && email != "" && verified {
		s.db.Where("LOWER(email) = ?", email).Limit(1).Find(&u)
	}
	if u.ID == 0 {
		if !p.AutoCreate {
			return nil, errors.New("There is no RustDesk account for " + firstNonEmpty(email, cl.PreferredUsername, "this login") +
				". Ask your administrator to create one.")
		}
		u = db.User{Name: s.uniqueName(firstNonEmpty(cl.PreferredUsername, strings.Split(email, "@")[0], "user")),
			DisplayName: cl.Name, Status: db.UserNormal}
		if verified {
			u.Email = email
		}
		if err := s.db.Create(&u).Error; err != nil {
			return nil, err
		}
		slog.Info("sso created user", "provider", p.Name, "user", u.Name)
	}
	if u.Status == db.UserDisabled {
		return nil, errors.New("Your RustDesk account is disabled.")
	}
	if link.ID == 0 {
		s.db.Create(&db.OIDCIdentity{ProviderID: p.ID, Subject: cl.Subject, UserID: u.ID, Email: email})
	}
	return &u, nil
}

func (s *Service) uniqueName(base string) string {
	base = strings.Trim(nameChars.ReplaceAllString(base, "-"), "-")
	if base == "" {
		base = "user"
	}
	if len(base) > 90 {
		base = base[:90]
	}
	name := base
	for i := 2; ; i++ {
		var n int64
		s.db.Model(&db.User{}).Where("name = ?", name).Count(&n)
		if n == 0 {
			return name
		}
		name = fmt.Sprintf("%s-%d", base, i)
	}
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

// HandleClientAuth is POST /api/oidc/auth {op, id, uuid, deviceInfo}.
func (s *Service) HandleClientAuth(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Op         string          `json:"op"`
		ID         string          `json:"id"`
		UUID       string          `json:"uuid"`
		DeviceInfo json.RawMessage `json:"deviceInfo"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, http.StatusOK, "Invalid request")
		return
	}
	info := string(req.DeviceInfo)
	if !json.Valid(req.DeviceInfo) || len(info) > 1024 {
		info = "{}"
	}
	code, err := s.Begin(KindClient, req.Op, req.ID, req.UUID, info)
	if err != nil {
		httpx.Error(w, http.StatusOK, err.Error())
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]string{"code": code, "url": s.StartURL(code)})
}

// HandleClientQuery is GET /api/oidc/auth-query?code=&id=&uuid=.
// The result (with the token) is handed out once, to the client that started the login.
func (s *Service) HandleClientQuery(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	sess, err := s.session("code = ?", q.Get("code"))
	if err != nil || sess.Kind != KindClient || sess.DeviceID != q.Get("id") || sess.DeviceUUID != q.Get("uuid") {
		httpx.Error(w, http.StatusOK, "This login has expired. Please start again.")
		return
	}
	switch {
	case sess.Error != "":
		s.db.Delete(sess)
		httpx.Error(w, http.StatusOK, sess.Error)
	case sess.Result == "":
		httpx.Error(w, http.StatusOK, NotYet)
	default:
		s.db.Delete(sess)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(sess.Result))
	}
}

var pageTmpl = template.Must(template.New("p").Parse(`<!doctype html><html lang="en"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1"><title>{{.Title}} · RustDesk Manager</title>
<style>body{font-family:system-ui,sans-serif;background:oklch(98.4% .003 190);color:oklch(20.8% .028 190);display:flex;
min-height:100vh;align-items:center;justify-content:center;margin:0;padding:16px}main{background:#fff;
border:1px solid oklch(92.9% .011 190);border-top:4px solid #0f766e;border-radius:8px;padding:24px;max-width:420px}
h1{font-size:20px;margin:0 0 8px}
@media(prefers-color-scheme:dark){body{background:oklch(12.9% .024 190);color:oklch(96.8% .006 190)}
main{background:oklch(20.8% .028 190);border-color:oklch(27.9% .03 190);border-top-color:#2dd4bf}}</style>
</head><body><main><h1>{{.Title}}</h1><p>{{.Message}}</p></main></body></html>`))

func (s *Service) page(w http.ResponseWriter, status int, title, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = pageTmpl.Execute(w, map[string]string{"Title": title, "Message": msg})
}
