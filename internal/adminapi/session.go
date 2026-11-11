package adminapi

import (
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"rustdesk-manager/internal/auth"
	"rustdesk-manager/internal/db"
	"rustdesk-manager/internal/httpx"
	"rustdesk-manager/internal/sso"
	"rustdesk-manager/internal/version"
)

func logError(err error) { slog.Error("admin api", "err", err) }

type userJSON struct {
	ID          uint   `json:"id"`
	Name        string `json:"name"`
	DisplayName string `json:"display_name"`
	Email       string `json:"email"`
	IsAdmin     bool   `json:"is_admin"`
	TOTPEnabled bool   `json:"totp_enabled"`
}

func toUserJSON(u *db.User) userJSON {
	return userJSON{ID: u.ID, Name: u.Name, DisplayName: u.DisplayName, Email: u.Email, IsAdmin: u.IsAdmin,
		TOTPEnabled: u.TOTPEnabled}
}

func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// StartWebSession signs a user in to the web console (sets the session cookie).
// Admins get the full console; other users only their own account page.
func (a *API) StartWebSession(w http.ResponseWriter, r *http.Request, u *db.User) error {
	expires := time.Now().UTC().Add(auth.WebSessionTTL)
	plain, err := auth.IssueToken(a.db, &db.Token{Kind: auth.KindWeb, UserID: u.ID, ExpiresAt: &expires,
		DeviceInfo: r.UserAgent()})
	if err != nil {
		return err
	}
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: plain, Path: "/", Expires: expires,
		HttpOnly: true, SameSite: http.SameSiteLaxMode,
		Secure: strings.HasPrefix(a.cfg.PublicURL, "https://"),
	})
	a.db.Model(u).Update("last_login_at", time.Now().UTC())
	slog.Info("web login", "user", u.Name, "ip", remoteIP(r))
	return nil
}

// POST /admin/api/auth/login {username, password}
// -> {user} when signed in, or {tfa_required: true, ticket} when a 2FA code is needed.
func (a *API) login(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request")
		return
	}
	ipKey := "ip:" + remoteIP(r)
	userKey := "user:" + strings.ToLower(strings.TrimSpace(req.Username))
	if a.limiter.Blocked(ipKey) || a.limiter.Blocked(userKey) {
		httpx.Error(w, http.StatusTooManyRequests, "too many failed attempts, try again later")
		return
	}
	u, err := auth.CheckPassword(a.db, req.Username, req.Password)
	if err != nil {
		if errors.Is(err, auth.ErrInvalid) {
			a.limiter.Fail(ipKey)
			a.limiter.Fail(userKey)
			slog.Warn("web login failed", "user", req.Username, "ip", remoteIP(r))
			httpx.Error(w, http.StatusUnauthorized, "invalid username or password")
			return
		}
		httpx.Error(w, http.StatusForbidden, err.Error())
		return
	}
	if u.TOTPEnabled {
		httpx.JSON(w, http.StatusOK, map[string]any{"tfa_required": true, "ticket": a.pending.Start(u.ID, "")})
		return
	}
	a.limiter.Reset(userKey)
	if err := a.StartWebSession(w, r, u); err != nil {
		serverError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"user": toUserJSON(u)})
}

// POST /admin/api/auth/login/2fa {ticket, code}
func (a *API) login2FA(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Ticket string `json:"ticket"`
		Code   string `json:"code"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request")
		return
	}
	ipKey := "ip:" + remoteIP(r)
	if a.limiter.Blocked(ipKey) {
		httpx.Error(w, http.StatusTooManyRequests, "too many failed attempts, try again later")
		return
	}
	userID, _, ok := a.pending.Get(req.Ticket)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "this sign-in has expired, please start again")
		return
	}
	var u db.User
	if err := a.db.First(&u, userID).Error; err != nil || u.Status == db.UserDisabled {
		a.pending.Done(req.Ticket)
		httpx.Error(w, http.StatusUnauthorized, "invalid username or password")
		return
	}
	if !auth.VerifyUserTOTP(a.db, a.box, &u, req.Code) {
		a.pending.Fail(req.Ticket)
		a.limiter.Fail(ipKey)
		a.limiter.Fail("user:" + strings.ToLower(u.Name))
		httpx.Error(w, http.StatusUnauthorized, "wrong code")
		return
	}
	a.pending.Done(req.Ticket)
	if err := a.StartWebSession(w, r, &u); err != nil {
		serverError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"user": toUserJSON(&u)})
}

// GET /admin/api/auth/sso: enabled SSO providers for the login page.
func (a *API) ssoProviders(w http.ResponseWriter, _ *http.Request) {
	out := []map[string]string{}
	for _, p := range a.sso.Enabled() {
		out = append(out, map[string]string{"name": p.Name})
	}
	httpx.JSON(w, http.StatusOK, out)
}

// GET /admin/api/auth/sso/{name}: start an SSO login for the web console.
func (a *API) ssoStart(w http.ResponseWriter, r *http.Request) {
	code, err := a.sso.Begin(sso.KindWeb, chi.URLParam(r, "name"), "", "", "")
	if err != nil {
		http.Redirect(w, r, "/?sso_error="+url.QueryEscape(err.Error()), http.StatusFound)
		return
	}
	http.Redirect(w, r, a.sso.StartURL(code), http.StatusFound)
}

// POST /admin/api/auth/logout
func (a *API) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil {
		_ = auth.Revoke(a.db, c.Value)
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true})
	w.WriteHeader(http.StatusNoContent)
}

// GET /admin/api/auth/me
func (a *API) me(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, map[string]any{"user": toUserJSON(currentUser(r)), "version": version.Version})
}
