package server

import (
	"fmt"
	"testing"
)

// hb sends a heartbeat with the client's last applied modified_at and returns the reply.
func (e *env) hb(id, uuid string, modifiedAt int64) map[string]any {
	e.t.Helper()
	_, body := e.post("/api/heartbeat", fmt.Sprintf(`{"id":%q,"uuid":%q,"ver":1004004,"modified_at":%d}`, id, uuid, modifiedAt))
	return jsonMap(e.t, body)
}

func pushed(t *testing.T, resp map[string]any) (map[string]any, int64) {
	t.Helper()
	s, ok := resp["strategy"].(map[string]any)
	if !ok {
		return nil, 0
	}
	return s["config_options"].(map[string]any), int64(resp["modified_at"].(float64))
}

func TestStrategyPush(t *testing.T) {
	e := newEnv(t)
	e.login()
	e.post("/api/sysinfo", `{"id":"500000001","uuid":"u1","hostname":"KIOSK"}`)

	// Default strategy is empty: nothing is pushed.
	if opts, _ := pushed(t, e.hb("500000001", "u1", 0)); opts != nil {
		t.Fatalf("empty default pushed %v", opts)
	}

	// Blocked keys are refused with an explanation.
	if code, _ := e.admin("POST", "/admin/api/strategies", `{"name":"Bad","options":{"api-server":"http://evil"}}`); code != 400 {
		t.Fatalf("blocked key accepted: %d", code)
	}

	// Assign a strategy through the device group.
	e.admin("POST", "/admin/api/strategies", `{"name":"Locked down","options":{"enable-clipboard":"N","enable-file-transfer":"N"}}`) // id 2
	e.admin("POST", "/admin/api/device-groups", `{"name":"Kiosks","strategy_id":2}`)
	e.admin("PATCH", "/admin/api/devices/1", `{"group_id":1}`)

	opts, v1 := pushed(t, e.hb("500000001", "u1", 0))
	if opts["enable-clipboard"] != "N" || opts["enable-file-transfer"] != "N" || v1 == 0 {
		t.Fatalf("first push: %v %d", opts, v1)
	}
	// Device applied it: no further pushes.
	if opts, _ := pushed(t, e.hb("500000001", "u1", v1)); opts != nil {
		t.Fatalf("pushed again after apply: %v", opts)
	}
	_, body := e.admin("GET", "/admin/api/devices/1", "")
	eff := jsonMap(t, body)["strategy"].(map[string]any)
	if eff["source"] != "group" || eff["status"] != "applied" {
		t.Fatalf("effective strategy: %v", eff)
	}

	// Remove file transfer from the strategy: it must be reset with "".
	e.admin("PATCH", "/admin/api/strategies/2", `{"options":{"enable-clipboard":"N"}}`)
	opts, v2 := pushed(t, e.hb("500000001", "u1", v1))
	if opts["enable-clipboard"] != "N" || opts["enable-file-transfer"] != "" || v2 == v1 {
		t.Fatalf("push after removing a key: %v", opts)
	}
	// Once applied, the reset key is dropped and the remaining options go out
	// under their own version in the same reply.
	opts, v3 := pushed(t, e.hb("500000001", "u1", v2))
	if _, has := opts["enable-file-transfer"]; has || opts["enable-clipboard"] != "N" {
		t.Fatalf("push after reset applied: %v", opts)
	}
	if opts, _ := pushed(t, e.hb("500000001", "u1", v3)); opts != nil {
		t.Fatalf("not stable: %v", opts)
	}

	// A device-level strategy wins over the group's.
	e.admin("POST", "/admin/api/strategies", `{"name":"Full","options":{"access-mode":"full"}}`) // id 3
	e.admin("PATCH", "/admin/api/devices/1", `{"strategy_id":3}`)
	opts, _ = pushed(t, e.hb("500000001", "u1", v3))
	if opts["access-mode"] != "full" || opts["enable-clipboard"] != "" {
		t.Fatalf("device strategy should win and reset clipboard: %v", opts)
	}

	// The default can't be deleted; deleting another strategy falls back.
	if code, _ := e.admin("DELETE", "/admin/api/strategies/1", ""); code != 400 {
		t.Fatalf("deleted default: %d", code)
	}
	if code, _ := e.admin("DELETE", "/admin/api/strategies/3", ""); code != 204 {
		t.Fatalf("delete strategy: %d", code)
	}
	_, body = e.admin("GET", "/admin/api/devices/1", "")
	if src := jsonMap(t, body)["strategy"].(map[string]any)["source"]; src != "group" {
		t.Fatalf("fallback after delete: %v", src)
	}
}
