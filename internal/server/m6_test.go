package server

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
	"github.com/pquerna/otp/totp"

	"rustdesk-manager/internal/db"
)

func TestTOTPLogin(t *testing.T) {
	e := newEnv(t)
	e.login()
	e.admin("POST", "/admin/api/users", `{"name":"alice","password":"alice-pass"}`)

	// alice (not an admin) signs in to the web console for her own account page.
	alice := newEnv2(e)
	if code, body := alice.admin("POST", "/admin/api/auth/login", `{"username":"alice","password":"alice-pass"}`); code != 200 {
		t.Fatalf("alice web login: %d %s", code, body)
	}
	if code, _ := alice.admin("GET", "/admin/api/devices", ""); code != 403 {
		t.Fatalf("non-admin reached admin API: %d", code)
	}
	_, body := alice.admin("POST", "/admin/api/me/totp/setup", "")
	secret := jsonMap(t, body)["secret"].(string)
	if !strings.HasPrefix(jsonMap(t, body)["qr"].(string), "data:image/png;base64,") {
		t.Fatal("no QR code")
	}
	if code, _ := alice.admin("POST", "/admin/api/me/totp/enable", `{"code":"000000"}`); code != 400 {
		t.Fatalf("wrong code enabled 2FA: %d", code)
	}
	code0, _ := totp.GenerateCode(secret, time.Now())
	if code, body := alice.admin("POST", "/admin/api/me/totp/enable", `{"code":"`+code0+`"}`); code != 204 {
		t.Fatalf("enable: %d %s", code, body)
	}

	// RustDesk app login now needs a second step (flutter login.dart flow).
	_, body = e.post("/api/login", `{"username":"alice","password":"alice-pass","id":"555000111","uuid":"dXVpZA==","type":"account"}`)
	m := jsonMap(t, body)
	if m["type"] != "email_check" || m["tfa_type"] != "tfa_check" || m["secret"] == "" || m["access_token"] != nil {
		t.Fatalf("password step: %s", body)
	}
	ticket := m["secret"].(string)
	if code, _ := e.post("/api/login", `{"type":"email_code","tfaCode":"123456","verificationCode":"123456","secret":"`+ticket+`","username":"alice"}`); code != 401 {
		t.Fatalf("wrong 2FA code accepted: %d", code)
	}
	// The code used for enabling can't be replayed; wait for a fresh one if needed.
	code1 := freshCode(t, secret, code0)
	_, body = e.post("/api/login", `{"type":"email_code","tfaCode":"`+code1+`","verificationCode":"`+code1+`","secret":"`+ticket+`",
		"username":"alice","id":"555000111","uuid":"dXVpZA=="}`)
	if m := jsonMap(t, body); m["type"] != "access_token" || m["access_token"] == "" {
		t.Fatalf("2FA step: %s", body)
	}

	// Web console login with 2FA.
	_, body = e.admin("POST", "/admin/api/auth/logout", "")
	web := newEnv2(e)
	_, body = web.admin("POST", "/admin/api/auth/login", `{"username":"alice","password":"alice-pass"}`)
	if jsonMap(t, body)["tfa_required"] != true {
		t.Fatalf("web login without 2FA step: %s", body)
	}
	wt := jsonMap(t, body)["ticket"].(string)
	if code, _ := web.admin("POST", "/admin/api/auth/login/2fa", `{"ticket":"`+wt+`","code":"`+code1+`"}`); code != 401 {
		t.Fatalf("replayed code accepted: %d", code)
	}

	// An admin can reset a user's 2FA (lost phone).
	e.login()
	e.admin("PATCH", "/admin/api/users/2", `{"reset_totp":true}`)
	_, body = e.post("/api/login", `{"username":"alice","password":"alice-pass","type":"account"}`)
	if jsonMap(t, body)["type"] != "access_token" {
		t.Fatalf("2FA still on after reset: %s", body)
	}
}

// freshCode waits until the authenticator shows a code different from prev.
func freshCode(t *testing.T, secret, prev string) string {
	t.Helper()
	deadline := time.Now().Add(35 * time.Second)
	for time.Now().Before(deadline) {
		c, _ := totp.GenerateCode(secret, time.Now())
		if c != prev {
			return c
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatal("no fresh TOTP code")
	return ""
}

// newEnv2 is a second browser (own cookie jar) on the same server.
func newEnv2(e *env) *env {
	c := *e
	jar, _ := newJar()
	c.web = &http.Client{Jar: jar}
	return &c
}

// fakeIdP is a minimal OpenID Connect provider for tests.
type fakeIdP struct {
	srv   *httptest.Server
	key   *rsa.PrivateKey
	mu    sync.Mutex
	codes map[string]string // auth code -> nonce
	sub   string
	email string
}

func newFakeIdP(t *testing.T) *fakeIdP {
	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	f := &fakeIdP{key: key, codes: map[string]string{}, sub: "user-123", email: "alice@example.com"}
	mux := http.NewServeMux()
	f.srv = httptest.NewServer(mux)
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"issuer": f.srv.URL, "authorization_endpoint": f.srv.URL + "/auth", "token_endpoint": f.srv.URL + "/token",
			"jwks_uri": f.srv.URL + "/keys", "id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/keys", func(w http.ResponseWriter, _ *http.Request) {
		json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"}}})
	})
	mux.HandleFunc("/auth", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("code_challenge") == "" {
			http.Error(w, "PKCE required", 400)
			return
		}
		f.mu.Lock()
		f.codes["authcode"] = q.Get("nonce")
		f.mu.Unlock()
		http.Redirect(w, r, q.Get("redirect_uri")+"?state="+url.QueryEscape(q.Get("state"))+"&code=authcode", http.StatusFound)
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		f.mu.Lock()
		nonce := f.codes[r.Form.Get("code")]
		f.mu.Unlock()
		signer, _ := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: key}, (&jose.SignerOptions{}).WithHeader("kid", "k1"))
		idt, _ := jwt.Signed(signer).Claims(map[string]any{
			"iss": f.srv.URL, "aud": "rdm-client", "sub": f.sub, "nonce": nonce,
			"email": f.email, "email_verified": true, "preferred_username": "alice.sso", "name": "Alice SSO",
			"iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix(),
		}).Serialize()
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"access_token": "at", "token_type": "Bearer", "id_token": idt, "expires_in": 3600})
	})
	t.Cleanup(f.srv.Close)
	return f
}

func TestOIDCClientLogin(t *testing.T) {
	e := newEnv(t)
	idp := newFakeIdP(t)
	e.login()
	if code, body := e.admin("POST", "/admin/api/oidc-providers", `{"name":"keycloak","issuer":"`+idp.srv.URL+`",
		"client_id":"rdm-client","client_secret":"s3cret"}`); code != 201 {
		t.Fatalf("create provider: %d %s", code, body)
	}

	// login-options in the format the client parses.
	_, body := e.clientReq("GET", "/api/login-options", "", "")
	var opts []string
	json.Unmarshal([]byte(body), &opts)
	if len(opts) != 1 || opts[0] != `common-oidc/[{"name":"keycloak","icon":null}]` {
		t.Fatalf("login options: %s", body)
	}

	start := func() (code, startURL string) {
		_, body := e.post("/api/oidc/auth", `{"op":"keycloak","id":"555000111","uuid":"dXVpZA==","deviceInfo":{"os":"windows","type":"client","name":"PC"}}`)
		m := jsonMap(t, body)
		return m["code"].(string), m["url"].(string)
	}
	query := func(code string) map[string]any {
		_, body := e.clientReq("GET", "/api/oidc/auth-query?code="+code+"&id=555000111&uuid=dXVpZA==", "", "")
		return jsonMap(t, body)
	}
	// The browser follows redirects: our start -> IdP -> our callback.
	// PUBLIC_URL is the default http://localhost:21114 in tests; point it at the test server.
	fix := func(u *url.URL) {
		if u.Host == "localhost:21114" {
			nu, _ := url.Parse(e.srv.URL)
			u.Host = nu.Host
		}
	}
	browser := &http.Client{CheckRedirect: func(req *http.Request, _ []*http.Request) error {
		fix(req.URL)
		return nil
	}}
	browse := func(u string) (int, string) {
		pu, _ := url.Parse(u)
		fix(pu)
		resp, err := browser.Get(pu.String())
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}

	// No account for this email and auto-create off: refused, and the app is told why.
	code, startURL := start()
	if q := query(code); q["error"] != "No authed oidc is found" {
		t.Fatalf("pending query: %v", q)
	}
	status, page := browse(startURL)
	if status != 403 || !strings.Contains(page, "no RustDesk account") {
		t.Fatalf("callback without account: %d %s", status, page)
	}
	if q := query(code); !strings.Contains(q["error"].(string), "no RustDesk account") {
		t.Fatalf("app not told about refusal: %v", q)
	}

	// With a user that has the same verified email, the login links and succeeds.
	e.admin("POST", "/admin/api/users", `{"name":"alice","password":"alice-pass","email":"Alice@Example.com"}`)
	code, startURL = start()
	if status, page := browse(startURL); status != 200 || !strings.Contains(page, "signed in to RustDesk as alice") {
		t.Fatalf("callback: %d %s", status, page)
	}
	q := query(code)
	user, _ := q["user"].(map[string]any)
	if q["type"] != "access_token" || q["access_token"] == "" || user["name"] != "alice" || user["info"] == nil {
		t.Fatalf("auth body: %v", q)
	}
	if code2, _ := e.clientReq("POST", "/api/currentUser", q["access_token"].(string), `{}`); code2 != 200 {
		t.Fatalf("SSO token rejected: %d", code2)
	}
	// The result is handed out only once.
	if q := query(code); q["access_token"] != nil {
		t.Fatal("token handed out twice")
	}

	// Later logins use the link even if the email changes at the provider.
	idp.email = "other@example.com"
	code, startURL = start()
	browse(startURL)
	if q := query(code); q["user"].(map[string]any)["name"] != "alice" {
		t.Fatalf("link not used: %v", q)
	}
}

func TestOIDCWebLogin(t *testing.T) {
	e := newEnv(t)
	idp := newFakeIdP(t)
	e.login()
	e.admin("POST", "/admin/api/oidc-providers", `{"name":"keycloak","issuer":"`+idp.srv.URL+`","client_id":"rdm-client","client_secret":"x"}`)
	e.admin("POST", "/admin/api/users", `{"name":"alice","password":"alice-pass","email":"alice@example.com"}`)

	b := newEnv2(e)
	nu, _ := url.Parse(e.srv.URL)
	b.web.CheckRedirect = func(req *http.Request, _ []*http.Request) error {
		if req.URL.Host == "localhost:21114" {
			req.URL.Host = nu.Host
		}
		return nil
	}
	resp, err := b.web.Get(e.srv.URL + "/admin/api/auth/sso/keycloak")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.Request.URL.Path != "/" || resp.Request.URL.RawQuery != "" {
		t.Fatalf("ended at %s", resp.Request.URL)
	}
	if code, body := b.admin("GET", "/admin/api/auth/me", ""); code != 200 || !strings.Contains(body, `"alice"`) {
		t.Fatalf("not signed in after SSO: %d %s", code, body)
	}
}

func TestRecordingUpload(t *testing.T) {
	e := newEnv(t)
	e.post("/api/sysinfo", `{"id":"600000001","uuid":"u1","hostname":"REC"}`)
	file := "incoming_600000001_20261111101500123_display0_vp9.webm"
	rec := func(q, body string) int {
		resp, err := http.Post(e.srv.URL+"/api/record?"+q, "application/octet-stream", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if rec("type=new&file="+file, "") != 403 {
		t.Fatal("upload accepted while disabled")
	}
	e.login()
	e.admin("PATCH", "/admin/api/settings", `{"accept_recordings":true}`)
	if rec("type=new&file=../../etc/passwd", "") != 400 || rec("type=new&file=incoming_999999999_20261111101500123_display0_vp9.webm", "") != 403 {
		t.Fatal("bad name or unknown device accepted")
	}
	if rec("type=new&file="+file, "") != 200 ||
		rec("type=part&file="+file+"&offset=0&length=10", "HHHHbodyAA") != 200 ||
		rec("type=part&file="+file+"&offset=10&length=5", "BBBBB") != 200 ||
		rec("type=tail&file="+file+"&offset=0&length=4", "head") != 200 {
		t.Fatal("upload sequence failed")
	}
	data, _ := os.ReadFile(filepath.Join(e.cfgDataDir(), "recordings", file))
	if string(data) != "headbodyAABBBBB" {
		t.Fatalf("file content: %q", data)
	}
	_, body := e.admin("GET", "/admin/api/recordings", "")
	r0 := jsonMap(t, body)["data"].([]any)[0].(map[string]any)
	if r0["completed"] != true || r0["size"] != float64(15) || r0["device_name"] != "REC" {
		t.Fatalf("recording row: %v", r0)
	}
	_, content := e.admin("GET", "/admin/api/recordings/1/file", "")
	if content != "headbodyAABBBBB" {
		t.Fatalf("download: %q", content)
	}
}

func TestAssignAndRegistration(t *testing.T) {
	e := newEnv(t)
	e.login()
	e.admin("POST", "/admin/api/device-groups", `{"name":"Branch"}`)
	_, body := e.admin("POST", "/admin/api/deploy-tokens", `{"name":"installer"}`)
	token := jsonMap(t, body)["token"].(string)
	e.admin("PATCH", "/admin/api/settings", `{"require_registered":true}`)

	// Unregistered devices are refused.
	if _, body := e.post("/api/sysinfo", `{"id":"700000001","uuid":"u1","hostname":"NEW"}`); body == "SYSINFO_UPDATED" {
		t.Fatal("unregistered device accepted")
	}
	// `rustdesk --assign` (core_main.rs) registers it.
	if code, body := e.clientReq("POST", "/api/devices/cli", "wrong", `{"id":"700000001","uuid":"u1","device_group_name":"Branch"}`); code != 401 {
		t.Fatalf("bad token: %d %s", code, body)
	}
	if code, body := e.clientReq("POST", "/api/devices/cli", token, `{"id":"700000001","uuid":"u1","device_group_name":"Nope"}`); code == 200 || !strings.Contains(body, "not found") {
		t.Fatalf("unknown group: %d %s", code, body)
	}
	if code, body := e.clientReq("POST", "/api/devices/cli", token,
		`{"id":"700000001","uuid":"u1","device_group_name":"Branch","device_name":"Front desk","note":"by script"}`); code != 200 || body != "" {
		t.Fatalf("assign: %d %q", code, body)
	}
	if _, body := e.post("/api/sysinfo", `{"id":"700000001","uuid":"u1","hostname":"DESKTOP-ABC"}`); body != "SYSINFO_UPDATED" {
		t.Fatalf("registered device refused: %s", body)
	}
	var d db.Device
	e.db.Preload("DeviceGroup").Where("rustdesk_id = ?", "700000001").First(&d)
	if d.DeviceGroup == nil || d.DisplayHostname() != "Front desk" || d.Hostname != "DESKTOP-ABC" || d.Note != "by script" {
		t.Fatalf("assigned device: %+v", d)
	}
	if code, body := e.clientReq("POST", "/api/devices/deploy", token, `{"id":"700000001","uuid":"u1","pk":"x"}`); code != 200 || !strings.Contains(body, "NOT_ENABLED") {
		t.Fatalf("deploy: %d %s", code, body)
	}
}
