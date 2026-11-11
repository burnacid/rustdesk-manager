package adminapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"rustdesk-manager/internal/auth"
	"rustdesk-manager/internal/db"
	"rustdesk-manager/internal/httpx"
	"rustdesk-manager/internal/metrics"
)

type passkeyJSON struct {
	ID         uint       `json:"id"`
	Name       string     `json:"name"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
}

type passkeyFinishReq struct {
	Ticket     string          `json:"ticket"`
	Name       string          `json:"name"`
	Credential json.RawMessage `json:"credential"` // the browser's PublicKeyCredential as JSON
}

func (a *API) passkeysReady(w http.ResponseWriter) bool {
	if a.passkeys == nil {
		httpx.Error(w, http.StatusServiceUnavailable, "passkeys are not available: check PUBLIC_URL")
		return false
	}
	return true
}

// GET /admin/api/me/passkeys
func (a *API) listPasskeys(w http.ResponseWriter, r *http.Request) {
	var rows []db.Passkey
	if err := a.db.Where("user_id = ?", currentUser(r).ID).Order("id").Find(&rows).Error; err != nil {
		serverError(w, err)
		return
	}
	out := make([]passkeyJSON, 0, len(rows))
	for _, p := range rows {
		out = append(out, passkeyJSON{ID: p.ID, Name: p.Name, CreatedAt: p.CreatedAt, LastUsedAt: p.LastUsedAt})
	}
	httpx.JSON(w, http.StatusOK, out)
}

// POST /admin/api/me/passkeys/register/begin -> {ticket, options}
func (a *API) passkeyRegisterBegin(w http.ResponseWriter, r *http.Request) {
	if !a.passkeysReady(w) {
		return
	}
	opts, ticket, err := a.passkeys.BeginRegister(currentUser(r))
	if err != nil {
		serverError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ticket": ticket, "options": opts})
}

// POST /admin/api/me/passkeys/register/finish {ticket, name, credential}
func (a *API) passkeyRegisterFinish(w http.ResponseWriter, r *http.Request) {
	if !a.passkeysReady(w) {
		return
	}
	var req passkeyFinishReq
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		name = "Passkey"
	}
	if len(name) > 100 {
		name = name[:100]
	}
	u := currentUser(r)
	p, err := a.passkeys.FinishRegister(u, req.Ticket, name, req.Credential)
	if errors.Is(err, auth.ErrPasskey) {
		httpx.Error(w, http.StatusBadRequest, "the passkey could not be verified, please try again")
		return
	} else if err != nil {
		serverError(w, err)
		return
	}
	slog.Info("passkey added", "user", u.Name)
	httpx.JSON(w, http.StatusCreated, passkeyJSON{ID: p.ID, Name: p.Name, CreatedAt: p.CreatedAt})
}

// DELETE /admin/api/me/passkeys/{id}
func (a *API) deletePasskey(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	res := a.db.Where("id = ? AND user_id = ?", id, currentUser(r).ID).Delete(&db.Passkey{})
	if res.Error != nil {
		serverError(w, res.Error)
		return
	}
	if res.RowsAffected == 0 {
		httpx.Error(w, http.StatusNotFound, "not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /admin/api/auth/passkey/begin -> {ticket, options}
func (a *API) passkeyLoginBegin(w http.ResponseWriter, r *http.Request) {
	if !a.passkeysReady(w) {
		return
	}
	if a.limiter.Blocked("ip:" + remoteIP(r)) {
		httpx.Error(w, http.StatusTooManyRequests, "too many failed attempts, try again later")
		return
	}
	opts, ticket, err := a.passkeys.BeginLogin()
	if err != nil {
		serverError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ticket": ticket, "options": opts})
}

// POST /admin/api/auth/passkey/finish {ticket, credential} -> {user}
// A passkey already combines possession and a PIN or biometric, so TOTP is skipped.
func (a *API) passkeyLoginFinish(w http.ResponseWriter, r *http.Request) {
	if !a.passkeysReady(w) {
		return
	}
	var req passkeyFinishReq
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request")
		return
	}
	ipKey := "ip:" + remoteIP(r)
	if a.limiter.Blocked(ipKey) {
		httpx.Error(w, http.StatusTooManyRequests, "too many failed attempts, try again later")
		return
	}
	u, err := a.passkeys.FinishLogin(req.Ticket, req.Credential)
	if err != nil || u.Status == db.UserDisabled {
		a.limiter.Fail(ipKey)
		metrics.Login("passkey", false)
		slog.Warn("passkey login failed", "ip", remoteIP(r))
		httpx.Error(w, http.StatusUnauthorized, "this passkey was not accepted")
		return
	}
	metrics.Login("passkey", true)
	if err := a.StartWebSession(w, r, u); err != nil {
		serverError(w, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"user": toUserJSON(u)})
}
