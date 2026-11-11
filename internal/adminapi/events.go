package adminapi

import (
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"gorm.io/gorm"

	"rustdesk-manager/internal/db"
	"rustdesk-manager/internal/httpx"
)

// The admin action log: every change made through the admin API, by whom,
// from where and with what result. Request bodies are never stored.

type viaKey struct{}

func via(r *http.Request) string {
	if v, _ := r.Context().Value(viaKey{}).(string); v != "" {
		return v
	}
	return "web"
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	return s.ResponseWriter.Write(b)
}

// recordEvents logs non-GET requests. It runs after requireUser/requireAdmin.
func (a *API) recordEvents(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			next.ServeHTTP(w, r)
			return
		}
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		route, target := "", ""
		if rc := chi.RouteContext(r.Context()); rc != nil {
			route = strings.TrimPrefix(rc.RoutePattern(), "/admin/api")
			target = rc.URLParam("id")
		}
		status := rec.status
		if status == 0 {
			status = http.StatusOK
		}
		a.logEvent(r, currentUser(r), r.Method, route, target, status)
	})
}

func (a *API) logEvent(r *http.Request, u *db.User, method, route, target string, status int) {
	ev := db.AdminEvent{Method: method, Route: route, TargetID: target, Status: status,
		IP: remoteIP(r), Via: via(r), CreatedAt: time.Now().UTC()}
	if u != nil {
		ev.UserID, ev.UserName = u.ID, u.Name
	}
	if err := a.db.Create(&ev).Error; err != nil {
		slog.Error("admin event", "err", err)
	}
}

type adminEventJSON struct {
	ID       uint      `json:"id"`
	UserID   uint      `json:"user_id"`
	UserName string    `json:"user_name"`
	Method   string    `json:"method"`
	Route    string    `json:"route"`
	Action   string    `json:"action"`
	TargetID string    `json:"target_id"`
	Status   int       `json:"status"`
	IP       string    `json:"ip"`
	Via      string    `json:"via"`
	At       time.Time `json:"created_at"`
}

func toAdminEventJSON(e *db.AdminEvent) adminEventJSON {
	return adminEventJSON{e.ID, e.UserID, e.UserName, e.Method, e.Route, describeEvent(e.Method, e.Route),
		e.TargetID, e.Status, e.IP, e.Via, e.CreatedAt}
}

// describeEvent turns "PATCH /devices/{id}" into "Edit device".
func describeEvent(method, route string) string {
	parts := strings.Split(strings.Trim(route, "/"), "/")
	var noun []string
	for _, p := range parts {
		if !strings.HasPrefix(p, "{") {
			noun = append(noun, strings.ReplaceAll(p, "-", " "))
		}
	}
	thing := singular(strings.Join(noun, " "))
	if s, ok := eventNames[method+" "+route]; ok {
		return s
	}
	verb := map[string]string{"POST": "Create", "PUT": "Set", "PATCH": "Edit", "DELETE": "Delete"}[method]
	if verb == "" {
		verb = method
	}
	return verb + " " + thing
}

var eventNames = map[string]string{
	"POST /auth/login":                        "Sign in",
	"POST /auth/logout":                       "Sign out",
	"POST /devices/{id}/disconnect":           "Disconnect device",
	"POST /devices/{id}/reset-uuid":           "Reset device binding",
	"POST /devices/bulk":                      "Bulk device action",
	"POST /oidc-providers":                    "Add SSO provider",
	"PATCH /oidc-providers/{id}":              "Edit SSO provider",
	"DELETE /oidc-providers/{id}":             "Delete SSO provider",
	"POST /api-tokens":                        "Create API token",
	"DELETE /api-tokens/{id}":                 "Revoke API token",
	"PATCH /settings":                         "Change settings",
	"POST /me/password":                       "Change own password",
	"POST /me/totp/setup":                     "Start 2FA setup",
	"POST /me/totp/enable":                    "Turn on 2FA",
	"POST /me/totp/disable":                   "Turn off 2FA",
	"POST /me/passkeys/register/begin":        "Start adding passkey",
	"POST /me/passkeys/register/finish":       "Add passkey",
	"DELETE /me/passkeys/{id}":                "Remove passkey",
	"DELETE /users/{id}/sessions/{sid}":       "Revoke user session",
	"PUT /address-books/{id}/shares":          "Set address book sharing",
	"POST /address-books/{id}/peers":          "Add address book peer",
	"PATCH /address-books/{id}/peers/{peer}":  "Edit address book peer",
	"DELETE /address-books/{id}/peers/{peer}": "Remove address book peer",
	"POST /address-books/{id}/tags":           "Add address book tag",
	"DELETE /address-books/{id}/tags/{name}":  "Remove address book tag",
}

// GET /admin/api/audit/admin?user=&from=&to=&current=&pageSize=
func (a *API) listAdminEvents(w http.ResponseWriter, r *http.Request) {
	page := httpx.ParsePage(r)
	tx := adminEventFilters(a.db.Model(&db.AdminEvent{}), r)
	var total int64
	if err := tx.Count(&total).Error; err != nil {
		serverError(w, err)
		return
	}
	var rows []db.AdminEvent
	if err := tx.Order("id DESC").Offset(page.Offset()).Limit(page.PageSize).Find(&rows).Error; err != nil {
		serverError(w, err)
		return
	}
	out := make([]adminEventJSON, 0, len(rows))
	for i := range rows {
		out = append(out, toAdminEventJSON(&rows[i]))
	}
	httpx.JSON(w, http.StatusOK, httpx.Paged[adminEventJSON]{Total: total, Data: out})
}

func adminEventFilters(tx *gorm.DB, r *http.Request) *gorm.DB {
	q := r.URL.Query()
	if u := q.Get("user"); u != "" {
		tx = tx.Where("user_name = ?", u)
	}
	if t := parseTime(q.Get("from")); t != nil {
		tx = tx.Where("created_at >= ?", *t)
	}
	if t := parseTime(q.Get("to")); t != nil {
		if len(q.Get("to")) == len("2006-01-02") {
			*t = t.Add(24 * time.Hour)
		}
		tx = tx.Where("created_at < ?", *t)
	}
	return tx
}

// singular turns "device groups" into "device group" and "strategies" into "strategy".
func singular(s string) string {
	switch {
	case strings.HasSuffix(s, "ies"):
		return strings.TrimSuffix(s, "ies") + "y"
	case strings.HasSuffix(s, "s") && !strings.HasSuffix(s, "ss"):
		return strings.TrimSuffix(s, "s")
	}
	return s
}
