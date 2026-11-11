package server

import (
	"strings"
	"testing"

	"rustdesk-manager/internal/db"
)

// Calls mirror flutter/lib/models/ab_model.dart: actions must return 200 with an empty body.

func TestPersonalAddressBook(t *testing.T) {
	e := newEnv(t)
	e.login()
	e.admin("POST", "/admin/api/users", `{"name":"alice","password":"alice-pass"}`)
	tok := e.clientLogin("alice", "alice-pass", "555000111")
	e.post("/api/sysinfo", `{"id":"300000001","uuid":"u1","hostname":"SERVER-1","username":"svc","os":"linux / Ubuntu 24.04"}`)

	code, body := e.clientReq("POST", "/api/ab/personal", tok, "")
	guid, _ := jsonMap(t, body)["guid"].(string)
	if code != 200 || guid == "" {
		t.Fatalf("personal: %d %s", code, body)
	}
	if _, body := e.clientReq("POST", "/api/ab/personal", tok, ""); jsonMap(t, body)["guid"] != guid {
		t.Fatal("personal guid changed between calls")
	}
	action := func(method, path, payload string) {
		t.Helper()
		if code, body := e.clientReq(method, path, tok, payload); code != 200 || body != "" {
			t.Fatalf("%s %s: %d %q", method, path, code, body)
		}
	}
	action("POST", "/api/ab/tag/add/"+guid, `{"name":"servers","color":4294198070}`)
	action("POST", "/api/ab/tag/add/"+guid, `{"name":"office","color":4280391411}`)
	// Personal books store the hash; the client strips "password" before sending.
	action("POST", "/api/ab/peer/add/"+guid, `{"id":"300000001","alias":"Main server","tags":["servers","nope"],"hash":"aGFzaA=="}`)
	action("POST", "/api/ab/peer/add/"+guid, `{"id":"300000002","alias":"","tags":[]}`)
	if code, body := e.clientReq("POST", "/api/ab/peer/add/"+guid, tok, `{"id":"300000002"}`); code == 200 && body == "" {
		t.Fatal("duplicate peer accepted")
	}
	action("PUT", "/api/ab/peer/update/"+guid, `{"id":"300000002","tags":["office"],"note":"front desk"}`)
	action("PUT", "/api/ab/tag/rename/"+guid, `{"old":"servers","new":"infra"}`)
	action("PUT", "/api/ab/tag/update/"+guid, `{"name":"infra","color":4278190335}`)

	_, body = e.clientReq("POST", "/api/ab/peers?current=1&pageSize=100&ab="+guid, tok, "")
	peers := jsonMap(t, body)["data"].([]any)
	p1 := peers[0].(map[string]any)
	if p1["alias"] != "Main server" || p1["hash"] != "aGFzaA==" || p1["same_server"] != true ||
		p1["hostname"] != "SERVER-1" || p1["platform"] != "Linux" || p1["forceAlwaysRelay"] != "false" {
		t.Fatalf("peer 1: %v", p1)
	}
	if tags := p1["tags"].([]any); len(tags) != 1 || tags[0] != "infra" {
		t.Fatalf("tags after rename (unknown tag dropped): %v", tags)
	}
	p2 := peers[1].(map[string]any)
	if p2["note"] != "front desk" || p2["same_server"] != false {
		t.Fatalf("peer 2: %v", p2)
	}

	_, body = e.clientReq("POST", "/api/ab/tags/"+guid, tok, "")
	if !strings.Contains(body, `{"name":"infra","color":4278190335}`) {
		t.Fatalf("tags: %s", body)
	}
	action("DELETE", "/api/ab/tag/"+guid, `["infra"]`)
	action("DELETE", "/api/ab/peer/"+guid, `["300000002"]`)
	_, body = e.clientReq("POST", "/api/ab/peers?current=1&pageSize=100&ab="+guid, tok, "")
	if m := jsonMap(t, body); m["total"] != float64(1) || len(m["data"].([]any)[0].(map[string]any)["tags"].([]any)) != 0 {
		t.Fatalf("after deletes: %s", body)
	}

	// The hash is encrypted in the database.
	var row db.AbPeer
	e.db.Where("peer_id = ?", "300000001").First(&row)
	if row.Hash == "" || strings.Contains(row.Hash, "aGFzaA") {
		t.Fatalf("hash stored in plain text: %q", row.Hash)
	}

	// Another user can't open alice's personal book.
	e.admin("POST", "/admin/api/users", `{"name":"bob","password":"bob-pass1"}`)
	bob := e.clientLogin("bob", "bob-pass1", "555000222")
	if code, _ := e.clientReq("POST", "/api/ab/peers?ab="+guid, bob, ""); code != 404 {
		t.Fatalf("bob read alice's personal book: %d", code)
	}
}

func TestSharedAddressBook(t *testing.T) {
	e := newEnv(t)
	e.login()
	e.admin("POST", "/admin/api/user-groups", `{"name":"Support"}`)
	e.admin("POST", "/admin/api/users", `{"name":"alice","password":"alice-pass","group_id":1}`) // id 2
	e.admin("POST", "/admin/api/users", `{"name":"bob","password":"bob-pass1"}`)                 // id 3
	e.admin("POST", "/admin/api/users", `{"name":"carol","password":"carol-pass"}`)              // id 4
	code, body := e.admin("POST", "/admin/api/address-books", `{"name":"Customers","note":"all customer PCs"}`)
	if code != 201 {
		t.Fatalf("create book: %d %s", code, body)
	}
	guid := jsonMap(t, body)["guid"].(string)
	// Support group can write, bob can only read, carol has no access.
	if code, body := e.admin("PUT", "/admin/api/address-books/1/shares",
		`[{"user_group_id":1,"rule":2},{"user_id":3,"rule":1}]`); code != 200 {
		t.Fatalf("shares: %d %s", code, body)
	}
	e.admin("POST", "/admin/api/address-books/1/tags", `{"name":"vip","color":4294198070}`)

	alice := e.clientLogin("alice", "alice-pass", "555000111")
	bob := e.clientLogin("bob", "bob-pass1", "555000222")
	carol := e.clientLogin("carol", "carol-pass", "555000333")

	_, body = e.clientReq("POST", "/api/ab/shared/profiles?current=1&pageSize=100", alice, "")
	prof := jsonMap(t, body)["data"].([]any)
	if len(prof) != 1 || prof[0].(map[string]any)["rule"] != float64(2) || prof[0].(map[string]any)["owner"] != "admin" {
		t.Fatalf("alice profiles: %s", body)
	}
	_, body = e.clientReq("POST", "/api/ab/shared/profiles?current=1&pageSize=100", carol, "")
	if jsonMap(t, body)["total"] != float64(0) {
		t.Fatalf("carol sees shared book: %s", body)
	}

	// Shared books store a password (not a hash) that every member can use.
	if code, body := e.clientReq("POST", "/api/ab/peer/add/"+guid, alice,
		`{"id":"400000001","alias":"Bakery","tags":["vip"],"password":"S3cret!","hash":"ignored"}`); code != 200 || body != "" {
		t.Fatalf("alice add: %d %q", code, body)
	}
	if code, _ := e.clientReq("POST", "/api/ab/peer/add/"+guid, bob, `{"id":"400000002"}`); code != 403 {
		t.Fatalf("read-only bob could add: %d", code)
	}
	_, body = e.clientReq("POST", "/api/ab/peers?current=1&pageSize=100&ab="+guid, bob, "")
	p := jsonMap(t, body)["data"].([]any)[0].(map[string]any)
	if p["password"] != "S3cret!" || p["hash"] != nil {
		t.Fatalf("shared peer for bob: %v", p)
	}
	if code, _ := e.clientReq("POST", "/api/ab/peers?ab="+guid, carol, ""); code != 404 {
		t.Fatalf("carol read shared book: %d", code)
	}

	// The web console never sees the password.
	_, body = e.admin("GET", "/admin/api/address-books/1/peers", "")
	if strings.Contains(body, "S3cret") || !strings.Contains(body, `"has_password":true`) {
		t.Fatalf("admin peers leak or miss password flag: %s", body)
	}

	// A device installed with preset-address-book-name joins the book on registration.
	e.post("/api/sysinfo", `{"id":"400000009","uuid":"u9","hostname":"NEW-PC","preset-address-book-name":"Customers",
		"preset-address-book-tag":"vip","preset-address-book-alias":"New shop","preset-address-book-password":"pw-123"}`)
	_, body = e.clientReq("POST", "/api/ab/peers?current=1&pageSize=100&ab="+guid, alice, "")
	if !strings.Contains(body, `"id":"400000009"`) || !strings.Contains(body, `"alias":"New shop"`) || !strings.Contains(body, `"password":"pw-123"`) {
		t.Fatalf("preset peer missing: %s", body)
	}

	// Deleting alice keeps the shared book; deleting the group removes its share.
	e.admin("DELETE", "/admin/api/user-groups/1", "")
	if code, _ := e.clientReq("POST", "/api/ab/peer/add/"+guid, alice, `{"id":"400000003"}`); code != 404 {
		t.Fatalf("alice kept access after group deletion: %d", code)
	}
}
