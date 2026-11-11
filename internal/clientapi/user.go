package clientapi

import (
	"context"
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

// Login flow of the client: flutter/lib/common/widgets/login.dart and
// flutter/lib/models/user_model.dart. The client shows the "error" text through its
// translation table, so known keys such as "Wrong credentials" appear translated.

// userPayload is the client's UserPayload (flutter/lib/common/hbbs/hbbs.dart and
// src/hbbs_http/account.rs). "info" is required by the Rust side for SSO logins.
type userPayload struct {
	Name        string         `json:"name"`
	DisplayName string         `json:"display_name"`
	Avatar      string         `json:"avatar"`
	Email       string         `json:"email"`
	Note        string         `json:"note"`
	Status      int            `json:"status"`
	IsAdmin     bool           `json:"is_admin"`
	Info        map[string]any `json:"info"`
}

func toUserPayload(u *db.User) userPayload {
	return userPayload{Name: u.Name, DisplayName: u.DisplayName, Email: u.Email, Note: u.Note,
		Status: u.Status, IsAdmin: u.IsAdmin, Info: map[string]any{}}
}

type loginReq struct {
	Username         string          `json:"username"`
	Password         string          `json:"password"`
	ID               string          `json:"id"`   // RustDesk id of the client logging in
	UUID             string          `json:"uuid"` // its machine uuid
	AutoLogin        bool            `json:"autoLogin"`
	Type             string          `json:"type"` // "account", or "email_code" for the 2FA step
	VerificationCode string          `json:"verificationCode"`
	TfaCode          string          `json:"tfaCode"`
	Secret           string          `json:"secret"` // ticket from the password step
	DeviceInfo       json.RawMessage `json:"deviceInfo"`
}

// POST /api/login
//   - {type:"account", username, password} -> {type:"access_token", access_token, user}
//     or, with 2FA on, {type:"email_check", tfa_type:"tfa_check", secret:<ticket>, user}
//   - {type:"email_code", tfaCode, secret:<ticket>} -> {type:"access_token", ...}
//
// Failure: non-200 with {error}.
func (a *API) login(w http.ResponseWriter, r *http.Request) {
	var req loginReq
	if err := httpx.Decode(r, &req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "Invalid request")
		return
	}
	ipKey := "ip:" + clientIP(r)
	userKey := "user:" + strings.ToLower(strings.TrimSpace(req.Username))
	if a.limiter.Blocked(ipKey) || a.limiter.Blocked(userKey) {
		httpx.Error(w, http.StatusTooManyRequests, "Too many failed attempts, try again later")
		return
	}
	switch req.Type {
	case "", "account":
		a.loginPassword(w, r, req, ipKey, userKey)
	case "email_code", "tfa_code":
		a.loginSecondFactor(w, r, req, ipKey, userKey)
	default:
		httpx.Error(w, http.StatusBadRequest, "Unsupported login type")
	}
}

func (a *API) loginPassword(w http.ResponseWriter, r *http.Request, req loginReq, ipKey, userKey string) {
	u, err := auth.CheckPassword(a.db, req.Username, req.Password)
	if err != nil {
		if errors.Is(err, auth.ErrInvalid) {
			a.limiter.Fail(ipKey)
			a.limiter.Fail(userKey)
			metrics.Login("client", false)
			slog.Warn("client login failed", "user", req.Username, "ip", clientIP(r), "device", req.ID)
			httpx.Error(w, http.StatusUnauthorized, "Wrong credentials")
			return
		}
		httpx.Error(w, http.StatusForbidden, err.Error())
		return
	}
	if u.TOTPEnabled {
		ticket := a.pending.Start(u.ID, "")
		httpx.JSON(w, http.StatusOK, map[string]any{
			"type": "email_check", "tfa_type": "tfa_check", "secret": ticket, "user": toUserPayload(u),
		})
		return
	}
	a.limiter.Reset(userKey)
	a.finishLogin(w, r, u, req.ID, req.UUID, req.DeviceInfo)
}

func (a *API) loginSecondFactor(w http.ResponseWriter, r *http.Request, req loginReq, ipKey, userKey string) {
	userID, _, ok := a.pending.Get(req.Secret)
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "Login expired, please sign in again")
		return
	}
	var u db.User
	if err := a.db.First(&u, userID).Error; err != nil || u.Status == db.UserDisabled {
		a.pending.Done(req.Secret)
		httpx.Error(w, http.StatusUnauthorized, "Wrong credentials")
		return
	}
	code := req.TfaCode
	if code == "" {
		code = req.VerificationCode
	}
	if !auth.VerifyUserTOTP(a.db, a.box, &u, code) {
		a.pending.Fail(req.Secret)
		a.limiter.Fail(ipKey)
		a.limiter.Fail(userKey)
		slog.Warn("client 2FA failed", "user", u.Name, "ip", clientIP(r))
		httpx.Error(w, http.StatusUnauthorized, "Wrong 2FA code")
		return
	}
	a.pending.Done(req.Secret)
	a.limiter.Reset(userKey)
	a.finishLogin(w, r, &u, req.ID, req.UUID, req.DeviceInfo)
}

func (a *API) finishLogin(w http.ResponseWriter, r *http.Request, u *db.User, deviceID, deviceUUID string, deviceInfo json.RawMessage) {
	body, err := a.IssueClientLogin(u, deviceID, deviceUUID, string(deviceInfo))
	if err != nil {
		logDBError("login token", err)
		httpx.Error(w, http.StatusInternalServerError, "Server error")
		return
	}
	metrics.Login("client", true)
	slog.Info("client login", "user", u.Name, "ip", clientIP(r), "device", deviceID)
	httpx.JSON(w, http.StatusOK, body)
}

// IssueClientLogin creates a RustDesk app login and returns the client's login
// response body. Logging in again from the same client replaces its old token.
// Also used by single sign-on.
func (a *API) IssueClientLogin(u *db.User, deviceID, deviceUUID, deviceInfo string) (map[string]any, error) {
	if len(deviceInfo) > 1024 || !json.Valid([]byte(deviceInfo)) {
		deviceInfo = "{}"
	}
	if deviceID != "" {
		a.db.Where("kind = ? AND user_id = ? AND device_id = ?", auth.KindClient, u.ID, deviceID).Delete(&db.Token{})
	}
	plain, err := auth.IssueToken(a.db, &db.Token{Kind: auth.KindClient, UserID: u.ID,
		DeviceID: deviceID, DeviceUUID: deviceUUID, DeviceInfo: deviceInfo})
	if err != nil {
		return nil, err
	}
	a.db.Model(u).Update("last_login_at", time.Now().UTC())
	return map[string]any{"type": "access_token", "access_token": plain, "user": toUserPayload(u)}, nil
}

type tokenKey struct{}

// requireToken checks the "Authorization: Bearer <token>" header of a logged-in client.
// The client drops its login on a 401.
func (a *API) requireToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		plain, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		tok, err := auth.Lookup(a.db, auth.KindClient, strings.TrimSpace(plain))
		if !ok || err != nil {
			httpx.Error(w, http.StatusUnauthorized, "Invalid token")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), tokenKey{}, tok)))
	})
}

func tokenFrom(r *http.Request) *db.Token { return r.Context().Value(tokenKey{}).(*db.Token) }

// POST /api/currentUser {id, uuid}
func (a *API) currentUser(w http.ResponseWriter, r *http.Request) {
	httpx.JSON(w, http.StatusOK, toUserPayload(&tokenFrom(r).User))
}

// POST /api/logout {id, uuid}
func (a *API) logout(w http.ResponseWriter, r *http.Request) {
	a.db.Delete(&db.Token{}, tokenFrom(r).ID)
	httpx.JSON(w, http.StatusOK, map[string]any{})
}

// GET /api/login-options: SSO buttons for the client's login dialog.
func (a *API) loginOptions(w http.ResponseWriter, _ *http.Request) {
	httpx.JSON(w, http.StatusOK, a.sso.LoginOptions())
}
