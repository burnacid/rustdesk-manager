package server

import (
	"strings"
	"testing"
	"time"

	"rustdesk-manager/internal/cleanup"
	"rustdesk-manager/internal/db"
	"rustdesk-manager/internal/settings"
)

func TestAuditCSVExport(t *testing.T) {
	e := newEnv(t)
	e.post("/api/sysinfo", `{"id":"800000001","uuid":"u1","hostname":"CSV-PC"}`)
	e.post("/api/audit/conn", `{"ip":"192.0.2.1","action":"new","id":"800000001","uuid":"u1","conn_id":1,"nonce":"c1"}`)
	// A hostile peer name that would run as a formula in a spreadsheet.
	e.post("/api/audit/conn", `{"peer":["900000001","=HYPERLINK(\"http://evil\")"],"type":1,"primary_auth":2,
		"id":"800000001","uuid":"u1","conn_id":1,"nonce":"c2"}`)
	e.post("/api/audit/alarm", `{"id":"800000001","uuid":"u1","typ":2,"info":"{\"ip\":\"198.51.100.1\"}","conn_id":5,"nonce":"a1"}`)
	e.login()

	code, body := e.admin("GET", "/admin/api/audit/conns/export", "")
	if code != 200 {
		t.Fatalf("export: %d %s", code, body)
	}
	lines := strings.Split(strings.TrimSpace(strings.TrimPrefix(body, "\xEF\xBB\xBF")), "\n")
	if len(lines) != 2 || !strings.HasPrefix(lines[0], "Started,Logged in,Closed,Device ID") {
		t.Fatalf("csv: %q", body)
	}
	row := lines[1]
	if !strings.Contains(row, "CSV-PC") || !strings.Contains(row, "File transfer") || !strings.Contains(row, "One-time password") {
		t.Fatalf("row: %s", row)
	}
	if !strings.Contains(row, `"'=HYPERLINK(""http://evil"")"`) {
		t.Fatalf("formula not defused: %s", row)
	}
	_, body = e.admin("GET", "/admin/api/audit/alarms/export?device=800000001", "")
	if !strings.Contains(body, "6 failed attempts within 1 minute") {
		t.Fatalf("alarms csv: %s", body)
	}
	_, body = e.admin("GET", "/admin/api/audit/alarms/export?device=other", "")
	if strings.Count(strings.TrimSpace(body), "\n") != 0 {
		t.Fatalf("filter ignored: %s", body)
	}
}

func TestCleanup(t *testing.T) {
	e := newEnv(t)
	now := time.Now().UTC()
	old, recent := now.AddDate(0, 0, -100), now.AddDate(0, 0, -5)
	e.db.Create(&db.AuditConn{GUID: "g1", DeviceID: "1", StartedAt: old})
	e.db.Create(&db.AuditConn{GUID: "g2", DeviceID: "1", StartedAt: recent})
	e.db.Create(&db.AuditAlarm{DeviceID: "1", CreatedAt: old})
	past := now.Add(-time.Hour)
	e.db.Create(&db.Token{Hash: "h-expired", Kind: "web", UserID: 1, ExpiresAt: &past})
	e.db.Create(&db.Token{Hash: "h-client", Kind: "client", UserID: 1})

	store := settings.New(e.db)
	// Retention 0 = keep forever: only expired logins go.
	cleanup.Once(e.db, store, e.dir, now)
	var n int64
	e.db.Model(&db.AuditConn{}).Count(&n)
	if n != 2 {
		t.Fatalf("audit removed with retention off: %d left", n)
	}
	var tokens []db.Token
	e.db.Find(&tokens)
	if len(tokens) != 1 || tokens[0].Hash != "h-client" {
		t.Fatalf("tokens after cleanup: %+v", tokens)
	}

	store.SetInt(settings.AuditRetentionDays, 30)
	cleanup.Once(e.db, store, e.dir, now)
	e.db.Model(&db.AuditConn{}).Count(&n)
	var alarms int64
	e.db.Model(&db.AuditAlarm{}).Count(&alarms)
	if n != 1 || alarms != 0 {
		t.Fatalf("after 30-day retention: sessions=%d alarms=%d", n, alarms)
	}
}
