// Package clientapi implements the HTTP API the stock RustDesk client calls
// (normally provided by RustDesk Server Pro). Request and response shapes follow
// the client source in reference/rustdesk; see the plan for file references.
package clientapi

import (
	"log/slog"
	"net"
	"net/http"
	"sync"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"rustdesk-manager/internal/abook"
	"rustdesk-manager/internal/auth"
	"rustdesk-manager/internal/config"
	"rustdesk-manager/internal/db"
	"rustdesk-manager/internal/live"
	"rustdesk-manager/internal/secret"
	"rustdesk-manager/internal/settings"
	"rustdesk-manager/internal/sso"
	"rustdesk-manager/internal/strategy"
)

type API struct {
	cfg      *config.Config
	db       *gorm.DB
	live     *live.State
	limiter  *auth.Limiter
	abooks   *abook.Service
	strat    *strategy.Service
	box      *secret.Box
	pending  *auth.Pending // logins waiting for a 2FA code
	settings *settings.Store
	sso      *sso.Service

	// known caches rustdesk id -> bound uuid, so heartbeats don't hit the database.
	known sync.Map
}

// Deps are the shared services the client API uses.
type Deps struct {
	Cfg        *config.Config
	DB         *gorm.DB
	Live       *live.State
	Limiter    *auth.Limiter
	Abooks     *abook.Service
	Strategies *strategy.Service
	Box        *secret.Box
	Settings   *settings.Store
	SSO        *sso.Service
}

func New(d Deps) *API {
	return &API{cfg: d.Cfg, db: d.DB, live: d.Live, limiter: d.Limiter, abooks: d.Abooks, strat: d.Strategies,
		box: d.Box, pending: auth.NewPending(), settings: d.Settings, sso: d.SSO}
}

// Routes registers the client endpoints on the /api router.
func (a *API) Routes(r chi.Router) {
	// Device side: called by every controlled device without user login.
	r.Post("/sysinfo", a.sysinfo)
	r.Post("/sysinfo_ver", a.sysinfoVer)
	r.Post("/heartbeat", a.heartbeat)
	r.Post("/audit/conn", a.auditConn)
	r.Post("/audit/file", a.auditFile)
	r.Post("/audit/alarm", a.auditAlarm)
	r.Post("/record", a.record)
	r.Post("/devices/cli", a.assignDevice) // deploy token, not a user login
	r.Post("/devices/deploy", a.deployDevice)

	// User side: the RustDesk app of a person who logs in.
	r.Post("/login", a.login)
	r.Get("/login-options", a.loginOptions)
	r.Post("/oidc/auth", a.sso.HandleClientAuth)
	r.Get("/oidc/auth-query", a.sso.HandleClientQuery)
	r.Get("/oidc/start", a.sso.HandleStart)
	r.Get("/oidc/callback", a.sso.HandleCallback)
	r.Group(func(r chi.Router) {
		r.Use(a.requireToken)
		r.Post("/currentUser", a.currentUser)
		r.Post("/logout", a.logout)
		r.Get("/users", a.accessibleUsers)
		r.Get("/peers", a.accessiblePeers)
		r.Get("/device-group/accessible", a.accessibleDeviceGroups)

		r.Post("/ab/settings", a.abSettings)
		r.Post("/ab/personal", a.abPersonal)
		r.Post("/ab/shared/profiles", a.abSharedProfiles)
		r.Post("/ab/peers", a.abPeers)
		r.Post("/ab/tags/{guid}", a.abTags)
		r.Post("/ab/peer/add/{guid}", a.abPeerAdd)
		r.Put("/ab/peer/update/{guid}", a.abPeerUpdate)
		r.Delete("/ab/peer/{guid}", a.abPeerDelete)
		r.Post("/ab/tag/add/{guid}", a.abTagAdd)
		r.Put("/ab/tag/rename/{guid}", a.abTagRename)
		r.Put("/ab/tag/update/{guid}", a.abTagUpdate)
		r.Delete("/ab/tag/{guid}", a.abTagDelete)
		r.Get("/ab", a.abLegacyGet)
		r.Post("/ab", a.abLegacyPost)
	})
}

// ForgetDevice drops cached state after an admin deletes a device or resets its uuid binding.
func (a *API) ForgetDevice(rustdeskID string) {
	a.known.Delete(rustdeskID)
	a.live.Forget(rustdeskID)
	a.strat.Forget(rustdeskID)
}

// boundUUID returns the uuid a device id is bound to ("" = unknown device).
func (a *API) boundUUID(id string) (string, error) {
	if v, ok := a.known.Load(id); ok {
		return v.(string), nil
	}
	var d db.Device
	if err := a.db.Select("uuid").Where("rustdesk_id = ?", id).Limit(1).Find(&d).Error; err != nil {
		return "", err
	}
	if d.UUID != "" {
		a.known.Store(id, d.UUID)
	}
	return d.UUID, nil
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func writeText(w http.ResponseWriter, s string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(s))
}

func logDBError(op string, err error) {
	slog.Error("database error", "op", op, "err", err)
}
