package server

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"math/rand/v2"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"

	"rustdesk-manager/internal/config"
	"rustdesk-manager/internal/db"
)

// Request bodies below mirror what the RustDesk client sends
// (reference/rustdesk/src/hbbs_http/sync.rs and src/server/connection.rs).

type env struct {
	t   *testing.T
	srv *httptest.Server
	db  *gorm.DB
	web *http.Client // admin UI client with a cookie jar
	dir string
}

func (e *env) cfgDataDir() string { return e.dir }

func newJar() (http.CookieJar, error) { return cookiejar.New(nil) }

// useMySQL points the test at a fresh MariaDB/MySQL database when
// RDM_TEST_MYSQL is set to a server DSN without database name, e.g.
// RDM_TEST_MYSQL="root:pw@tcp(127.0.0.1:3307)/". The database is dropped afterwards.
func useMySQL(t *testing.T) {
	base := os.Getenv("RDM_TEST_MYSQL")
	if base == "" {
		return
	}
	admin, err := sql.Open("mysql", base)
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("rdm_test_%d_%d", time.Now().UnixNano(), rand.IntN(1e6))
	if _, err := admin.Exec("CREATE DATABASE " + name + " CHARACTER SET utf8mb4"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec("DROP DATABASE " + name)
		admin.Close()
	})
	t.Setenv("DB_DRIVER", "mysql")
	t.Setenv("DB_DSN", base+name+"?parseTime=true&charset=utf8mb4&loc=UTC")
}

func newEnv(t *testing.T) *env {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("DATA_DIR", dir)
	t.Setenv("ADMIN_INITIAL_PASSWORD", "admin-pass")
	useMySQL(t)
	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	gdb, err := db.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Bootstrap(gdb, cfg); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(New(cfg, gdb).Router())
	t.Cleanup(func() {
		srv.Close()
		if sqlDB, err := gdb.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	jar, _ := cookiejar.New(nil)
	return &env{t: t, srv: srv, db: gdb, web: &http.Client{Jar: jar}, dir: dir}
}

// post sends a client-style POST and returns status and body.
func (e *env) post(path, body string) (int, string) {
	e.t.Helper()
	resp, err := http.Post(e.srv.URL+path, "application/json", strings.NewReader(body))
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func (e *env) admin(method, path, body string) (int, string) {
	e.t.Helper()
	req, _ := http.NewRequest(method, e.srv.URL+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Requested-With", "rdm")
	resp, err := e.web.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func jsonMap(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("not JSON: %q", s)
	}
	return m
}

const sysinfoBody = `{"cpu":"Intel(R) Core(TM) i7, 3.6GHz, 8/4 cores","memory":"16GB",
	"os":"windows / Windows 11 Pro - 26300","hostname":"PC-01","username":"stefan",
	"version":"1.4.4","id":"123456789","uuid":"dXVpZC1h","preset-device-group-name":"Office",
	"preset-note":"front desk"}`

func TestDeviceRegistrationAndHeartbeat(t *testing.T) {
	e := newEnv(t)
	e.db.Create(&db.DeviceGroup{Name: "Office"})

	// Unknown device: server asks for sysinfo.
	code, body := e.post("/api/heartbeat", `{"id":"123456789","uuid":"dXVpZC1h","ver":1004004,"modified_at":0}`)
	if code != 200 || jsonMap(t, body)["sysinfo"] != true {
		t.Fatalf("heartbeat for unknown device: %d %s", code, body)
	}

	code, body = e.post("/api/sysinfo", sysinfoBody)
	if code != 200 || body != "SYSINFO_UPDATED" {
		t.Fatalf("sysinfo: %d %q", code, body)
	}
	var dev db.Device
	e.db.Preload("DeviceGroup").Where("rustdesk_id = ?", "123456789").First(&dev)
	if dev.Hostname != "PC-01" || dev.Username != "stefan" || dev.Note != "front desk" ||
		dev.DeviceGroup == nil || dev.DeviceGroup.Name != "Office" {
		t.Fatalf("device not stored as expected: %+v", dev)
	}

	// Another machine claiming the same id is refused.
	_, body = e.post("/api/sysinfo", strings.Replace(sysinfoBody, "dXVpZC1h", "b3RoZXI=", 1))
	if body == "SYSINFO_UPDATED" {
		t.Fatal("uuid mismatch was accepted")
	}

	// Known device with live sessions: empty reply.
	code, body = e.post("/api/heartbeat", `{"id":"123456789","uuid":"dXVpZC1h","ver":1004004,"conns":[7,8],"modified_at":0}`)
	if code != 200 || len(jsonMap(t, body)) != 0 {
		t.Fatalf("heartbeat: %d %s", code, body)
	}

	// Admin disconnects conn 7; the next heartbeat carries it, once.
	e.login()
	code, body = e.admin("POST", "/admin/api/devices/1/disconnect", `{"conn_ids":[7]}`)
	if code != 200 {
		t.Fatalf("disconnect: %d %s", code, body)
	}
	_, body = e.post("/api/heartbeat", `{"id":"123456789","uuid":"dXVpZC1h","ver":1004004,"conns":[7,8],"modified_at":0}`)
	if d, _ := jsonMap(t, body)["disconnect"].([]any); len(d) != 1 || d[0] != float64(7) {
		t.Fatalf("expected disconnect [7], got %s", body)
	}
	_, body = e.post("/api/heartbeat", `{"id":"123456789","uuid":"dXVpZC1h","ver":1004004,"conns":[8],"modified_at":0}`)
	if _, ok := jsonMap(t, body)["disconnect"]; ok {
		t.Fatalf("disconnect delivered twice: %s", body)
	}

	// Device list shows it online with its live session.
	_, body = e.admin("GET", "/admin/api/devices?status=online", "")
	list := jsonMap(t, body)
	if list["total"] != float64(1) {
		t.Fatalf("devices list: %s", body)
	}
	d0 := list["data"].([]any)[0].(map[string]any)
	if d0["online"] != true || len(d0["active_conns"].([]any)) != 1 {
		t.Fatalf("device not online with 1 conn: %s", body)
	}
}

func TestConnAuditLifecycleAndRetries(t *testing.T) {
	e := newEnv(t)
	newEvt := `{"ip":"203.0.113.5","action":"new","id":"123456789","uuid":"dXVpZC1h","conn_id":3,"session_id":0,"nonce":"n-1"}`
	for i := 0; i < 2; i++ { // second post is a client retry with the same nonce
		code, body := e.post("/api/audit/conn", newEvt)
		if code != 200 || body != "" {
			t.Fatalf("audit new (attempt %d): %d %q", i, code, body)
		}
	}
	code, body := e.post("/api/audit/conn", `{"peer":["987654321","Laptop"],"type":0,"primary_auth":3,"two_factor":1,
		"id":"123456789","uuid":"dXVpZC1h","conn_id":3,"session_id":18446744073709551615,"nonce":"n-2"}`)
	if code != 200 || body != "" {
		t.Fatalf("audit login: %d %q", code, body)
	}
	e.post("/api/audit/conn", `{"action":"close","id":"123456789","uuid":"dXVpZC1h","conn_id":3,"session_id":1,"nonce":"n-3"}`)

	var rows []db.AuditConn
	e.db.Find(&rows)
	if len(rows) != 1 {
		t.Fatalf("expected 1 session row, got %d", len(rows))
	}
	r := rows[0]
	if r.IP != "203.0.113.5" || r.PeerID != "987654321" || r.PeerName != "Laptop" || r.PrimaryAuth != 3 ||
		r.TwoFactor != 1 || r.SessionID != "18446744073709551615" || r.AuthedAt == nil || r.ClosedAt == nil {
		t.Fatalf("session row: %+v", r)
	}
}

func TestFileAndAlarmAudit(t *testing.T) {
	e := newEnv(t)
	file := `{"id":"123456789","uuid":"dXVpZC1h","peer_id":"987654321","conn_id":3,"type":0,
		"path":"C:\\Users\\stefan\\Documents","is_file":false,
		"info":"{\"ip\":\"203.0.113.5\",\"name\":\"Laptop\",\"num\":2,\"files\":[[\"a.txt\",10],[\"b.txt\",5]]}",
		"nonce":"f-1"}`
	alarm := `{"id":"123456789","uuid":"dXVpZC1h","typ":2,"info":"{\"ip\":\"198.51.100.9\"}","conn_id":4,"nonce":"a-1"}`
	for i := 0; i < 2; i++ {
		if code, body := e.post("/api/audit/file", file); code != 200 || body != "" {
			t.Fatalf("file audit: %d %q", code, body)
		}
		if code, body := e.post("/api/audit/alarm", alarm); code != 200 || body != "" {
			t.Fatalf("alarm audit: %d %q", code, body)
		}
	}
	var files, alarms int64
	e.db.Model(&db.AuditFile{}).Count(&files)
	e.db.Model(&db.AuditAlarm{}).Count(&alarms)
	if files != 1 || alarms != 1 {
		t.Fatalf("retries not deduplicated: files=%d alarms=%d", files, alarms)
	}

	e.login()
	_, body := e.admin("GET", "/admin/api/audit/files", "")
	info := jsonMap(t, body)["data"].([]any)[0].(map[string]any)["info"].(map[string]any)
	if info["num"] != float64(2) {
		t.Fatalf("file info not parsed: %s", body)
	}
}

func TestAdminAuth(t *testing.T) {
	e := newEnv(t)
	if code, _ := e.admin("GET", "/admin/api/devices", ""); code != 401 {
		t.Fatalf("devices without login: %d", code)
	}
	if code, _ := e.admin("POST", "/admin/api/auth/login", `{"username":"admin","password":"wrong"}`); code != 401 {
		t.Fatalf("wrong password: %d", code)
	}
	// Missing CSRF header is rejected even with valid credentials.
	resp, _ := e.web.Post(e.srv.URL+"/admin/api/auth/login", "application/json",
		strings.NewReader(`{"username":"admin","password":"admin-pass"}`))
	if resp.StatusCode != 403 {
		t.Fatalf("login without CSRF header: %d", resp.StatusCode)
	}
	e.login()
	if code, body := e.admin("GET", "/admin/api/auth/me", ""); code != 200 || !strings.Contains(body, `"admin"`) {
		t.Fatalf("me: %d %s", code, body)
	}
	e.admin("POST", "/admin/api/auth/logout", "")
	if code, _ := e.admin("GET", "/admin/api/auth/me", ""); code != 401 {
		t.Fatalf("me after logout: %d", code)
	}
}

func (e *env) login() {
	e.t.Helper()
	if code, body := e.admin("POST", "/admin/api/auth/login", `{"username":"admin","password":"admin-pass"}`); code != 200 {
		e.t.Fatalf("login: %d %s", code, body)
	}
}
