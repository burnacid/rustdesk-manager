// Package adminapi is the JSON API behind the admin web UI (/admin/api).
package adminapi

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"rustdesk-manager/internal/abook"
	"rustdesk-manager/internal/auth"
	"rustdesk-manager/internal/config"
	"rustdesk-manager/internal/db"
	"rustdesk-manager/internal/httpx"
	"rustdesk-manager/internal/live"
	"rustdesk-manager/internal/secret"
	"rustdesk-manager/internal/settings"
	"rustdesk-manager/internal/sso"
	"rustdesk-manager/internal/strategy"
)

const (
	sessionCookie = "rdm_session"
	// csrfHeader must be present on every state-changing request. Browsers can't
	// add custom headers to cross-site form posts, so this blocks CSRF.
	csrfHeader = "X-Requested-With"
)

// DeviceForgetter is implemented by the client API to drop cached device state.
type DeviceForgetter interface{ ForgetDevice(rustdeskID string) }

type API struct {
	cfg      *config.Config
	db       *gorm.DB
	live     *live.State
	devices  DeviceForgetter
	limiter  *auth.Limiter
	abooks   *abook.Service
	strat    *strategy.Service
	box      *secret.Box
	settings *settings.Store
	sso      *sso.Service
	pending  *auth.Pending
	passkeys *auth.Passkeys // nil when PUBLIC_URL can't be used as a relying party
}

// Deps are the shared services the admin API uses.
type Deps struct {
	Cfg        *config.Config
	DB         *gorm.DB
	Live       *live.State
	Devices    DeviceForgetter
	Limiter    *auth.Limiter
	Abooks     *abook.Service
	Strategies *strategy.Service
	Box        *secret.Box
	Settings   *settings.Store
	SSO        *sso.Service
	Passkeys   *auth.Passkeys
}

func New(d Deps) *API {
	return &API{cfg: d.Cfg, db: d.DB, live: d.Live, devices: d.Devices, limiter: d.Limiter,
		abooks: d.Abooks, strat: d.Strategies, box: d.Box, settings: d.Settings, sso: d.SSO,
		pending: auth.NewPending(), passkeys: d.Passkeys}
}

func (a *API) Routes(r chi.Router) {
	r.Use(csrfGuard)
	r.Post("/auth/login", a.login)
	r.Post("/auth/login/2fa", a.login2FA)
	r.Post("/auth/logout", a.logout)
	r.Get("/auth/sso", a.ssoProviders)
	r.Get("/auth/sso/{name}", a.ssoStart)
	r.Post("/auth/passkey/begin", a.passkeyLoginBegin)
	r.Post("/auth/passkey/finish", a.passkeyLoginFinish)

	// Any signed-in user: their own account.
	r.Group(func(r chi.Router) {
		r.Use(a.requireUser, a.recordEvents)
		r.Get("/auth/me", a.me)
		r.Post("/me/password", a.changeOwnPassword)
		r.Post("/me/totp/setup", a.totpSetup)
		r.Post("/me/totp/enable", a.totpEnable)
		r.Post("/me/totp/disable", a.totpDisable)
		r.Get("/me/passkeys", a.listPasskeys)
		r.Post("/me/passkeys/register/begin", a.passkeyRegisterBegin)
		r.Post("/me/passkeys/register/finish", a.passkeyRegisterFinish)
		r.Delete("/me/passkeys/{id}", a.deletePasskey)
	})

	r.Group(func(r chi.Router) {
		r.Use(a.requireAdmin, a.recordEvents)
		r.Get("/dashboard", a.dashboard)

		r.Get("/settings", a.getSettings)
		r.Patch("/settings", a.updateSettings)
		r.Get("/oidc-providers", a.listOIDCProviders)
		r.Post("/oidc-providers", a.createOIDCProvider)
		r.Patch("/oidc-providers/{id}", a.updateOIDCProvider)
		r.Delete("/oidc-providers/{id}", a.deleteOIDCProvider)
		r.Get("/deploy-tokens", a.listDeployTokens)
		r.Post("/deploy-tokens", a.createDeployToken)
		r.Delete("/deploy-tokens/{id}", a.deleteDeployToken)
		r.Get("/api-tokens", a.listAPITokens)
		r.Post("/api-tokens", a.createAPIToken)
		r.Delete("/api-tokens/{id}", a.deleteAPIToken)
		r.Get("/recordings", a.listRecordings)
		r.Get("/recordings/{id}/file", a.recordingFile)
		r.Delete("/recordings/{id}", a.deleteRecording)

		r.Get("/devices", a.listDevices)
		r.Post("/devices/bulk", a.bulkDevices)
		r.Get("/devices/{id}", a.getDevice)
		r.Patch("/devices/{id}", a.updateDevice)
		r.Delete("/devices/{id}", a.deleteDevice)
		r.Post("/devices/{id}/disconnect", a.disconnectDevice)
		r.Post("/devices/{id}/reset-uuid", a.resetDeviceUUID)

		r.Get("/device-groups", a.listDeviceGroups)
		r.Post("/device-groups", a.createDeviceGroup)
		r.Patch("/device-groups/{id}", a.updateDeviceGroup)
		r.Delete("/device-groups/{id}", a.deleteDeviceGroup)

		r.Get("/users", a.listUsers)
		r.Post("/users", a.createUser)
		r.Patch("/users/{id}", a.updateUser)
		r.Delete("/users/{id}", a.deleteUser)
		r.Get("/users/{id}/sessions", a.listUserSessions)
		r.Delete("/users/{id}/sessions/{sid}", a.revokeUserSession)

		r.Get("/user-groups", a.listUserGroups)
		r.Post("/user-groups", a.createUserGroup)
		r.Patch("/user-groups/{id}", a.updateUserGroup)
		r.Delete("/user-groups/{id}", a.deleteUserGroup)

		r.Get("/address-books", a.listAddressBooks)
		r.Post("/address-books", a.createAddressBook)
		r.Get("/address-books/{id}", a.getAddressBook)
		r.Patch("/address-books/{id}", a.updateAddressBook)
		r.Delete("/address-books/{id}", a.deleteAddressBook)
		r.Put("/address-books/{id}/shares", a.setAddressBookShares)
		r.Get("/address-books/{id}/peers", a.listAddressBookPeers)
		r.Post("/address-books/{id}/peers", a.addAddressBookPeer)
		r.Patch("/address-books/{id}/peers/{peer}", a.updateAddressBookPeer)
		r.Delete("/address-books/{id}/peers/{peer}", a.deleteAddressBookPeer)
		r.Post("/address-books/{id}/tags", a.addAddressBookTag)
		r.Delete("/address-books/{id}/tags/{name}", a.deleteAddressBookTag)

		r.Get("/strategies", a.listStrategies)
		r.Post("/strategies", a.createStrategy)
		r.Patch("/strategies/{id}", a.updateStrategy)
		r.Delete("/strategies/{id}", a.deleteStrategy)

		r.Get("/audit/conns", a.listAuditConns)
		r.Get("/audit/files", a.listAuditFiles)
		r.Get("/audit/alarms", a.listAuditAlarms)
		r.Get("/audit/admin", a.listAdminEvents)
		r.Get("/audit/{kind}/export", a.exportAudit)
	})
	r.NotFound(func(w http.ResponseWriter, _ *http.Request) {
		httpx.Error(w, http.StatusNotFound, "not found")
	})
}

func csrfGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Bearer API tokens aren't sent automatically by browsers, so they need no CSRF check.
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Header.Get(csrfHeader) == "" && !hasBearer(r) {
			httpx.Error(w, http.StatusForbidden, "missing "+csrfHeader+" header")
			return
		}
		next.ServeHTTP(w, r)
	})
}

type ctxKey struct{}

func currentUser(r *http.Request) *db.User { return r.Context().Value(ctxKey{}).(*db.User) }

func (a *API) sessionUser(r *http.Request) *db.User {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return nil
	}
	tok, err := auth.Lookup(a.db, auth.KindWeb, c.Value)
	if err != nil {
		return nil
	}
	return &tok.User
}

// requireUser allows any signed-in user (for their own account page).
func (a *API) requireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := a.sessionUser(r)
		if u == nil {
			httpx.Error(w, http.StatusUnauthorized, "not logged in")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, u)))
	})
}

// requireAdmin accepts the session cookie, or an API token as "Authorization: Bearer".
func (a *API) requireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var u *db.User
		if hasBearer(r) {
			u = a.apiTokenUser(r)
			r = r.WithContext(context.WithValue(r.Context(), viaKey{}, "api"))
		} else {
			u = a.sessionUser(r)
		}
		if u == nil {
			httpx.Error(w, http.StatusUnauthorized, "not logged in")
			return
		}
		if !u.IsAdmin {
			httpx.Error(w, http.StatusForbidden, "admins only")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, u)))
	})
}

// pathID parses the {id} URL parameter as a database id.
func pathID(w http.ResponseWriter, r *http.Request) (uint, bool) {
	n, err := strconv.ParseUint(chi.URLParam(r, "id"), 10, 32)
	if err != nil || n == 0 {
		httpx.Error(w, http.StatusBadRequest, "invalid id")
		return 0, false
	}
	return uint(n), true
}

// parseTime accepts RFC 3339 or YYYY-MM-DD; returns nil when empty or invalid.
func parseTime(s string) *time.Time {
	s = strings.TrimSpace(s)
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			t = t.UTC()
			return &t
		}
	}
	return nil
}

// likeEscape is appended to LIKE conditions. '!' is used because backslash
// escaping differs between SQLite and MySQL.
const likeEscape = " ESCAPE '!'"

// likePattern escapes a search term for a "LIKE ?"+likeEscape condition.
func likePattern(q string) string {
	r := strings.NewReplacer(`!`, `!!`, `%`, `!%`, `_`, `!_`)
	return "%" + r.Replace(strings.TrimSpace(q)) + "%"
}

func serverError(w http.ResponseWriter, err error) {
	logError(err)
	httpx.Error(w, http.StatusInternalServerError, "internal error")
}
