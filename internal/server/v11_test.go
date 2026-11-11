package server

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"rustdesk-manager/internal/db"
)

func TestAuditNote(t *testing.T) {
	e := newEnv(t)
	e.login()
	e.admin("POST", "/admin/api/users", `{"name":"alice","password":"alice-pass"}`)
	e.admin("POST", "/admin/api/users", `{"name":"bob","password":"bob-pass1"}`)
	alice := e.clientLogin("alice", "alice-pass", "987654321") // controlling side
	bob := e.clientLogin("bob", "bob-pass1", "555555555")      // unrelated user

	e.post("/api/audit/conn", `{"ip":"203.0.113.5","action":"new","id":"123456789","uuid":"dXVpZC1h","conn_id":3,"session_id":42,"nonce":"n-1"}`)
	e.post("/api/audit/conn", `{"peer":["987654321","Laptop"],"type":0,"id":"123456789","uuid":"dXVpZC1h","conn_id":3,"session_id":42,"nonce":"n-2"}`)

	q := "/api/audit/conn/active?id=123456789&session_id=42&conn_type=0"
	code, body := e.clientReq("GET", q, alice, "")
	if code != 200 || len(body) < 10 || body[0] != '"' {
		t.Fatalf("active guid: %d %s", code, body)
	}
	guid, _ := strconv.Unquote(strings.TrimSpace(body))
	if code, body := e.clientReq("GET", q, bob, ""); code != 200 || strings.TrimSpace(body) != `""` {
		t.Fatalf("other user must not get the guid: %d %s", code, body)
	}
	if code, _ := e.clientReq("GET", q, "", ""); code != 401 {
		t.Fatalf("no token: %d", code)
	}

	// The note usually arrives after the session closed.
	e.post("/api/audit/conn", `{"action":"close","id":"123456789","uuid":"dXVpZC1h","conn_id":3,"session_id":42,"nonce":"n-3"}`)
	if code, _ := e.clientReq("PUT", "/api/audit", bob, `{"guid":"`+guid+`","note":"nope"}`); code != 404 {
		t.Fatalf("other user note: %d", code)
	}
	if code, body := e.clientReq("PUT", "/api/audit", alice, `{"guid":"`+guid+`","note":"  fixed printer  "}`); code != 200 {
		t.Fatalf("note: %d %s", code, body)
	}
	var row db.AuditConn
	e.db.First(&row)
	if row.Note != "fixed printer" {
		t.Fatalf("note stored: %q", row.Note)
	}
}

func TestSwitchGrant(t *testing.T) {
	e := newEnv(t)
	now := time.Now().Unix()
	code, body := e.post("/api/switch-grant", `{"id":"123456789","switch_code_verifier":"x","timestamp":"`+strconv.FormatInt(now, 10)+`","signature":"s"}`)
	if code != 200 || jsonMap(t, body)["accepted"] != true {
		t.Fatalf("grant: %d %s", code, body)
	}
	code, body = e.post("/api/switch-grant", `{"id":"123456789","switch_code_verifier":"x","timestamp":"`+strconv.FormatInt(now-3600, 10)+`","signature":"s"}`)
	m := jsonMap(t, body)
	if code != 200 || m["accepted"] != false || m["server_time"] == nil {
		t.Fatalf("skewed grant: %d %s", code, body)
	}
}

// bearer sends a request with an API token and no CSRF header, like a script would.
func (e *env) bearer(method, path, token, body string) (int, string) {
	e.t.Helper()
	return e.clientReq(method, "/admin/api"+path, token, body)
}

func TestAPITokens(t *testing.T) {
	e := newEnv(t)
	e.login()
	code, body := e.admin("POST", "/admin/api/api-tokens", `{"name":"backup script","expires_days":30}`)
	if code != 201 {
		t.Fatalf("create: %d %s", code, body)
	}
	m := jsonMap(t, body)
	tok := m["token"].(string)
	id := jsonID(uint(m["info"].(map[string]any)["id"].(float64)))

	if code, body := e.bearer("GET", "/devices", tok, ""); code != 200 {
		t.Fatalf("list devices with token: %d %s", code, body)
	}
	// Writes work without the CSRF header, and are logged as "api".
	if code, body := e.bearer("POST", "/device-groups", tok, `{"name":"Lab"}`); code != 201 {
		t.Fatalf("write with token: %d %s", code, body)
	}
	var ev db.AdminEvent
	e.db.Where("route = ?", "/device-groups").First(&ev)
	if ev.Via != "api" || ev.UserName != "admin" || ev.Status != 201 {
		t.Fatalf("event: %+v", ev)
	}
	// Tokens can't reach the personal account pages, and aren't listed as user sessions.
	if code, _ := e.bearer("GET", "/auth/me", tok, ""); code != 401 {
		t.Fatalf("token on /auth/me: %d", code)
	}
	_, body = e.admin("GET", "/admin/api/users/1/sessions", "")
	if strings.Contains(body, `"kind":"api"`) {
		t.Fatalf("api token listed as a session: %s", body)
	}
	if code, _ := e.bearer("GET", "/devices", "wrong", ""); code != 401 {
		t.Fatalf("bad token: %d", code)
	}

	e.admin("DELETE", "/admin/api/api-tokens/"+id, "")
	if code, _ := e.bearer("GET", "/devices", tok, ""); code != 401 {
		t.Fatalf("revoked token still works: %d", code)
	}
}

func TestAdminEventLog(t *testing.T) {
	e := newEnv(t)
	e.login()
	e.admin("POST", "/admin/api/device-groups", `{"name":"Lab"}`)
	e.admin("PATCH", "/admin/api/device-groups/1", `{"name":"Lab 2"}`)
	e.admin("GET", "/admin/api/devices", "") // reads aren't logged

	code, body := e.admin("GET", "/admin/api/audit/admin", "")
	if code != 200 {
		t.Fatalf("list: %d %s", code, body)
	}
	var page struct {
		Total int64 `json:"total"`
		Data  []struct {
			Action   string `json:"action"`
			TargetID string `json:"target_id"`
			UserName string `json:"user_name"`
		} `json:"data"`
	}
	_ = json.Unmarshal([]byte(body), &page)
	// Newest first: edit, create, sign in.
	if page.Total != 3 || page.Data[0].Action != "Edit device group" || page.Data[0].TargetID != "1" ||
		page.Data[1].Action != "Create device group" || page.Data[2].Action != "Sign in" || page.Data[2].UserName != "admin" {
		t.Fatalf("events: %s", body)
	}
	code, body = e.admin("GET", "/admin/api/audit/admin/export", "")
	if code != 200 || !strings.Contains(body, "Edit device group") {
		t.Fatalf("export: %d %s", code, body)
	}
}

func TestBulkDevices(t *testing.T) {
	e := newEnv(t)
	e.login()
	for _, id := range []string{"111111111", "222222222", "333333333"} {
		e.post("/api/sysinfo", strings.NewReplacer(`"id":"123456789"`, `"id":"`+id+`"`, "dXVpZC1h", "u-"+id).Replace(sysinfoBody))
	}
	e.admin("POST", "/admin/api/device-groups", `{"name":"Shop"}`)
	var devs []db.Device
	e.db.Order("id").Find(&devs)
	if len(devs) != 3 {
		t.Fatalf("devices: %d", len(devs))
	}
	ids := jsonID(devs[0].ID) + "," + jsonID(devs[1].ID)

	var group db.DeviceGroup
	e.db.Where("name = ?", "Shop").First(&group)
	code, body := e.admin("POST", "/admin/api/devices/bulk", `{"ids":[`+ids+`],"action":"set_group","value":`+jsonID(group.ID)+`}`)
	if code != 200 || jsonMap(t, body)["affected"] != float64(2) {
		t.Fatalf("set_group: %d %s", code, body)
	}
	var n int64
	e.db.Model(&db.Device{}).Where("device_group_id = ?", group.ID).Count(&n)
	if n != 2 {
		t.Fatalf("devices in group: %d", n)
	}
	if code, _ := e.admin("POST", "/admin/api/devices/bulk", `{"ids":[`+ids+`],"action":"set_group","value":999}`); code != 400 {
		t.Fatalf("unknown group: %d", code)
	}
	if code, _ := e.admin("POST", "/admin/api/devices/bulk", `{"ids":[`+ids+`],"action":"set_group","value":0}`); code != 200 {
		t.Fatalf("clear group: %d", code)
	}
	if code, _ := e.admin("POST", "/admin/api/devices/bulk", `{"ids":[`+ids+`],"action":"delete"}`); code != 200 {
		t.Fatalf("delete: %d", code)
	}
	e.db.Model(&db.Device{}).Count(&n)
	if n != 1 {
		t.Fatalf("devices left: %d", n)
	}
	if code, _ := e.admin("POST", "/admin/api/devices/bulk", `{"ids":[1],"action":"explode"}`); code != 400 {
		t.Fatalf("unknown action: %d", code)
	}
}

func TestMetrics(t *testing.T) {
	t.Run("off by default", func(t *testing.T) {
		e := newEnv(t)
		resp, err := http.Get(e.srv.URL + "/metrics")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 404 {
			t.Fatalf("metrics without token configured: %d", resp.StatusCode)
		}
	})
	t.Run("with token", func(t *testing.T) {
		t.Setenv("METRICS_TOKEN", "scrape-me")
		e := newEnv(t)
		e.post("/api/sysinfo", sysinfoBody)
		e.login()
		if code, _ := e.clientReq("GET", "/metrics", "nope", ""); code != 401 {
			t.Fatalf("wrong token: %d", code)
		}
		code, body := e.clientReq("GET", "/metrics", "scrape-me", "")
		if code != 200 || !strings.Contains(body, "rdm_devices 1\n") ||
			!strings.Contains(body, `rdm_login_attempts_total{channel="web",result="success"}`) ||
			!strings.Contains(body, `rdm_build_info{version=`) {
			t.Fatalf("metrics: %d %s", code, body)
		}
	})
}
