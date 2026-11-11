package server

import (
	"crypto/subtle"
	"fmt"
	"net/http"
	"strings"
	"time"

	"rustdesk-manager/internal/auth"
	"rustdesk-manager/internal/db"
	"rustdesk-manager/internal/metrics"
	"rustdesk-manager/internal/version"
)

var startedAt = time.Now()

// GET /metrics in the Prometheus text format. Off unless METRICS_TOKEN is set;
// then it needs "Authorization: Bearer <METRICS_TOKEN>".
func (s *Server) metrics(w http.ResponseWriter, r *http.Request) {
	if s.cfg.MetricsToken == "" {
		http.NotFound(w, r)
		return
	}
	got, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if subtle.ConstantTimeCompare([]byte(got), []byte(s.cfg.MetricsToken)) != 1 {
		w.Header().Set("WWW-Authenticate", `Bearer realm="metrics"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	now := time.Now().UTC()
	count := func(model any, where string, args ...any) int64 {
		var n int64
		tx := s.db.Model(model)
		if where != "" {
			tx = tx.Where(where, args...)
		}
		tx.Count(&n)
		return n
	}

	var b strings.Builder
	gauge := func(name, help string) {
		fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s gauge\n", name, help, name)
	}
	counter := func(name, help string) {
		fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s counter\n", name, help, name)
	}

	gauge("rdm_build_info", "Version of this server.")
	fmt.Fprintf(&b, "rdm_build_info{version=%q} 1\n", version.Version)
	gauge("rdm_uptime_seconds", "Seconds since the server started.")
	fmt.Fprintf(&b, "rdm_uptime_seconds %d\n", int64(time.Since(startedAt).Seconds()))

	gauge("rdm_devices", "Registered devices.")
	fmt.Fprintf(&b, "rdm_devices %d\n", count(&db.Device{}, ""))
	gauge("rdm_devices_online", "Devices that sent a heartbeat recently.")
	fmt.Fprintf(&b, "rdm_devices_online %d\n", len(s.live.OnlineIDs(now)))
	gauge("rdm_users", "User accounts by status.")
	fmt.Fprintf(&b, "rdm_users{status=\"active\"} %d\n", count(&db.User{}, "status = ?", db.UserNormal))
	fmt.Fprintf(&b, "rdm_users{status=\"disabled\"} %d\n", count(&db.User{}, "status = ?", db.UserDisabled))
	gauge("rdm_logins_active", "Signed-in sessions and tokens by kind.")
	for _, k := range []string{auth.KindClient, auth.KindWeb, auth.KindAPI} {
		fmt.Fprintf(&b, "rdm_logins_active{kind=%q} %d\n", k,
			count(&db.Token{}, "kind = ? AND (expires_at IS NULL OR expires_at > ?)", k, now))
	}
	gauge("rdm_remote_sessions_open", "Remote sessions that have started and not closed.")
	fmt.Fprintf(&b, "rdm_remote_sessions_open %d\n", count(&db.AuditConn{}, "closed_at IS NULL AND started_at > ?", now.Add(-24*time.Hour)))

	gauge("rdm_audit_records", "Stored audit records by kind.")
	fmt.Fprintf(&b, "rdm_audit_records{kind=\"session\"} %d\n", count(&db.AuditConn{}, ""))
	fmt.Fprintf(&b, "rdm_audit_records{kind=\"file\"} %d\n", count(&db.AuditFile{}, ""))
	fmt.Fprintf(&b, "rdm_audit_records{kind=\"alarm\"} %d\n", count(&db.AuditAlarm{}, ""))
	fmt.Fprintf(&b, "rdm_audit_records{kind=\"admin\"} %d\n", count(&db.AdminEvent{}, ""))

	counter("rdm_login_attempts_total", "Sign-in attempts since start, by channel and result.")
	for _, l := range metrics.Logins() {
		fmt.Fprintf(&b, "rdm_login_attempts_total{channel=%q,result=%q} %d\n", l.Channel, l.Result, l.Count)
	}

	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = w.Write([]byte(b.String()))
}
