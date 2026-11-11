package server

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"rustdesk-manager/internal/db"
)

// clientReq sends a request like the RustDesk app: Bearer token, JSON body.
func (e *env) clientReq(method, path, token, body string) (int, string) {
	e.t.Helper()
	req, _ := http.NewRequest(method, e.srv.URL+path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

// clientLogin logs in the way flutter/lib/models/user_model.dart does and returns the token.
func (e *env) clientLogin(user, pass, deviceID string) string {
	e.t.Helper()
	code, body := e.post("/api/login", `{"username":"`+user+`","password":"`+pass+`","id":"`+deviceID+`",
		"uuid":"dXVpZA==","autoLogin":true,"type":"account","deviceInfo":{"os":"windows","type":"client","name":"LAPTOP"}}`)
	if code != 200 {
		e.t.Fatalf("login %s: %d %s", user, code, body)
	}
	m := jsonMap(e.t, body)
	if m["type"] != "access_token" || m["access_token"] == "" || m["user"].(map[string]any)["name"] != user {
		e.t.Fatalf("login response: %s", body)
	}
	return m["access_token"].(string)
}

func names(t *testing.T, body, key string) []string {
	t.Helper()
	var out []string
	for _, d := range jsonMap(t, body)["data"].([]any) {
		out = append(out, d.(map[string]any)[key].(string))
	}
	return out
}

func TestClientLoginAndAccessible(t *testing.T) {
	e := newEnv(t)
	e.login()

	// Setup through the admin API: a user group with access to device group "Shop".
	e.admin("POST", "/admin/api/device-groups", `{"name":"Shop"}`)
	e.admin("POST", "/admin/api/device-groups", `{"name":"Office"}`)
	if code, body := e.admin("POST", "/admin/api/user-groups", `{"name":"Support","device_group_ids":[1]}`); code != 201 {
		t.Fatalf("create user group: %d %s", code, body)
	}
	for _, u := range []string{`{"name":"alice","password":"alice-pass","group_id":1}`,
		`{"name":"bob","password":"bob-pass1","group_id":1}`, `{"name":"carol","password":"carol-pass"}`} {
		if code, body := e.admin("POST", "/admin/api/users", u); code != 201 {
			t.Fatalf("create user: %d %s", code, body)
		}
	}
	// Devices: one owned by bob, one in Shop, one in Office owned by carol.
	for i, id := range []string{"100000001", "100000002", "100000003"} {
		e.post("/api/sysinfo", `{"id":"`+id+`","uuid":"u`+id+`","hostname":"PC`+string(rune('1'+i))+`","os":"windows / Windows 11"}`)
	}
	e.admin("PATCH", "/admin/api/devices/1", `{"user_id":3}`)              // bob
	e.admin("PATCH", "/admin/api/devices/2", `{"group_id":1}`)             // Shop
	e.admin("PATCH", "/admin/api/devices/3", `{"group_id":2,"user_id":4}`) // Office, carol

	// Wrong password: 401 with a message the client can translate.
	code, body := e.post("/api/login", `{"username":"alice","password":"nope","type":"account"}`)
	if code != 401 || jsonMap(t, body)["error"] != "Wrong credentials" {
		t.Fatalf("wrong password: %d %s", code, body)
	}

	alice := e.clientLogin("alice", "alice-pass", "555000111")
	code, body = e.clientReq("POST", "/api/currentUser", alice, `{"id":"555000111","uuid":"dXVpZA=="}`)
	if code != 200 || jsonMap(t, body)["name"] != "alice" {
		t.Fatalf("currentUser: %d %s", code, body)
	}

	// alice sees her group (alice, bob), bob's device and the Shop device, not carol's.
	_, body = e.clientReq("GET", "/api/users?current=1&pageSize=100&accessible=&status=1", alice, "")
	if got := strings.Join(names(t, body, "name"), ","); got != "alice,bob" {
		t.Fatalf("alice users: %s", got)
	}
	_, body = e.clientReq("GET", "/api/peers?current=1&pageSize=100&accessible=&status=1", alice, "")
	if got := strings.Join(names(t, body, "id"), ","); got != "100000001,100000002" {
		t.Fatalf("alice peers: %s", got)
	}
	peer := jsonMap(t, body)["data"].([]any)[0].(map[string]any)
	if peer["user_name"] != "bob" || peer["info"].(map[string]any)["device_name"] != "PC1" {
		t.Fatalf("peer payload: %v", peer)
	}
	_, body = e.clientReq("GET", "/api/device-group/accessible?current=1&pageSize=100", alice, "")
	if got := strings.Join(names(t, body, "name"), ","); got != "Shop" {
		t.Fatalf("alice device groups: %s", got)
	}

	// The admin sees everything.
	adminTok := e.clientLogin("admin", "admin-pass", "555000222")
	_, body = e.clientReq("GET", "/api/peers?current=1&pageSize=100&accessible=&status=1", adminTok, "")
	if jsonMap(t, body)["total"] != float64(3) {
		t.Fatalf("admin peers: %s", body)
	}

	// Logging in again from the same device replaces the old token.
	alice2 := e.clientLogin("alice", "alice-pass", "555000111")
	if code, _ := e.clientReq("POST", "/api/currentUser", alice, `{}`); code != 401 {
		t.Fatalf("old token still valid: %d", code)
	}
	// Logout revokes the token.
	e.clientReq("POST", "/api/logout", alice2, `{}`)
	if code, _ := e.clientReq("POST", "/api/currentUser", alice2, `{}`); code != 401 {
		t.Fatalf("token valid after logout: %d", code)
	}

	// Disabling a user signs them out.
	bob := e.clientLogin("bob", "bob-pass1", "555000333")
	e.admin("PATCH", "/admin/api/users/3", `{"status":0}`)
	if code, _ := e.clientReq("POST", "/api/currentUser", bob, `{}`); code != 401 {
		t.Fatalf("disabled user's token still valid: %d", code)
	}
	if code, _ := e.post("/api/login", `{"username":"bob","password":"bob-pass1","type":"account"}`); code == 200 {
		t.Fatal("disabled user could log in")
	}
	if code, _ := e.clientReq("GET", "/api/users", "garbage", ""); code != 401 {
		t.Fatalf("bad token: %d", code)
	}
}

func TestAdminUserGuards(t *testing.T) {
	e := newEnv(t)
	e.login()
	if code, _ := e.admin("PATCH", "/admin/api/users/1", `{"is_admin":false}`); code != 400 {
		t.Fatalf("admin removed own admin rights: %d", code)
	}
	if code, _ := e.admin("DELETE", "/admin/api/users/1", ""); code != 400 {
		t.Fatalf("admin deleted self: %d", code)
	}
	if code, _ := e.admin("POST", "/admin/api/users", `{"name":"x","password":"short"}`); code != 400 {
		t.Fatalf("short password accepted: %d", code)
	}
	if code, _ := e.admin("POST", "/admin/api/users", `{"name":"admin","password":"long-enough"}`); code != 400 {
		t.Fatalf("duplicate username accepted: %d", code)
	}
	// Deleting a user unassigns their devices.
	e.admin("POST", "/admin/api/users", `{"name":"dave","password":"dave-pass"}`)
	e.post("/api/sysinfo", `{"id":"200000001","uuid":"u1","hostname":"D"}`)
	e.admin("PATCH", "/admin/api/devices/1", `{"user_id":2}`)
	if code, _ := e.admin("DELETE", "/admin/api/users/2", ""); code != 204 {
		t.Fatalf("delete user: %d", code)
	}
	var d db.Device
	e.db.First(&d, 1)
	if d.UserID != nil {
		t.Fatal("device still owned by deleted user")
	}
}
