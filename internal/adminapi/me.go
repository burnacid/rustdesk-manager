package adminapi

import (
	"net/http"

	"golang.org/x/crypto/bcrypt"

	"rustdesk-manager/internal/auth"
	"rustdesk-manager/internal/db"
	"rustdesk-manager/internal/httpx"
)

// Endpoints every signed-in user may use for their own account.

// POST /admin/api/me/password {current, new}
// Signs the user out everywhere except this browser.
func (a *API) changeOwnPassword(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	var req struct {
		Current string `json:"current"`
		New     string `json:"new"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request")
		return
	}
	if u.PasswordHash != "" && bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.Current)) != nil {
		httpx.Error(w, http.StatusBadRequest, "the current password is wrong")
		return
	}
	if len(req.New) < minPasswordLen {
		httpx.Error(w, http.StatusBadRequest, "the new password must be at least 8 characters")
		return
	}
	hash, err := auth.HashPassword(req.New)
	if err != nil {
		serverError(w, err)
		return
	}
	a.db.Model(u).Update("password_hash", hash)
	q := a.db.Where("user_id = ?", u.ID)
	if c, err := r.Cookie(sessionCookie); err == nil {
		q = q.Where("hash <> ?", auth.HashToken(c.Value))
	}
	q.Delete(&db.Token{})
	w.WriteHeader(http.StatusNoContent)
}

// POST /admin/api/me/totp/setup -> {secret, url, qr}
// Creates a new secret; 2FA is switched on once a code from it is confirmed.
func (a *API) totpSetup(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	if u.TOTPEnabled {
		httpx.Error(w, http.StatusBadRequest, "2FA is already on; turn it off first to set up a new app")
		return
	}
	secret, url, qr, err := auth.NewTOTP("RustDesk Manager", u.Name)
	if err != nil {
		serverError(w, err)
		return
	}
	a.db.Model(u).Updates(map[string]any{"totp_secret": a.box.Seal(secret), "totp_last_step": 0})
	httpx.JSON(w, http.StatusOK, map[string]string{"secret": secret, "url": url, "qr": qr})
}

// POST /admin/api/me/totp/enable {code}
func (a *API) totpEnable(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	var req struct {
		Code string `json:"code"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request")
		return
	}
	secret, err := a.box.Open(u.TOTPSecret)
	if err != nil || secret == "" {
		httpx.Error(w, http.StatusBadRequest, "start the setup first")
		return
	}
	step, ok := auth.CheckTOTP(secret, req.Code, 0)
	if !ok {
		httpx.Error(w, http.StatusBadRequest, "that code is not right; check the time on your phone and try again")
		return
	}
	a.db.Model(u).Updates(map[string]any{"totp_enabled": true, "totp_last_step": step})
	w.WriteHeader(http.StatusNoContent)
}

// POST /admin/api/me/totp/disable {password}
func (a *API) totpDisable(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	var req struct {
		Password string `json:"password"`
	}
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid request")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(req.Password)) != nil {
		httpx.Error(w, http.StatusBadRequest, "wrong password")
		return
	}
	a.db.Model(u).Updates(map[string]any{"totp_enabled": false, "totp_secret": ""})
	w.WriteHeader(http.StatusNoContent)
}
