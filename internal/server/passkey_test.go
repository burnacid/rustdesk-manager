package server

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/descope/virtualwebauthn"

	"rustdesk-manager/internal/db"
)

// TestPasskeyLogin runs the full WebAuthn ceremony with a software authenticator.
// PUBLIC_URL defaults to http://localhost:21114, which is the relying party here.
func TestPasskeyLogin(t *testing.T) {
	e := newEnv(t)
	e.login()
	rp := virtualwebauthn.RelyingParty{ID: "localhost", Name: "RustDesk Manager", Origin: "http://localhost:21114"}
	cred := virtualwebauthn.NewCredential(virtualwebauthn.KeyTypeEC2)

	var admin db.User
	e.db.Where("name = ?", "admin").First(&admin)
	authn := virtualwebauthn.NewAuthenticatorWithOptions(virtualwebauthn.AuthenticatorOptions{UserHandle: []byte("1")})

	// Register.
	code, body := e.admin("POST", "/admin/api/me/passkeys/register/begin", "")
	if code != 200 {
		t.Fatalf("register begin: %d %s", code, body)
	}
	var begin struct {
		Ticket  string          `json:"ticket"`
		Options json.RawMessage `json:"options"`
	}
	_ = json.Unmarshal([]byte(body), &begin)
	attOpts, err := virtualwebauthn.ParseAttestationOptions(string(begin.Options))
	if err != nil {
		t.Fatal(err)
	}
	resp := virtualwebauthn.CreateAttestationResponse(rp, authn, cred, *attOpts)
	code, body = e.admin("POST", "/admin/api/me/passkeys/register/finish",
		`{"ticket":"`+begin.Ticket+`","name":"Laptop","credential":`+resp+`}`)
	if code != 201 {
		t.Fatalf("register finish: %d %s", code, body)
	}
	authn.AddCredential(cred)
	// A ticket works once.
	if code, _ := e.admin("POST", "/admin/api/me/passkeys/register/finish",
		`{"ticket":"`+begin.Ticket+`","name":"Again","credential":`+resp+`}`); code != 400 {
		t.Fatalf("reused ticket: %d", code)
	}

	signIn := func() (int, string) {
		code, body := e.admin("POST", "/admin/api/auth/passkey/begin", "")
		if code != 200 {
			t.Fatalf("login begin: %d %s", code, body)
		}
		_ = json.Unmarshal([]byte(body), &begin)
		asOpts, err := virtualwebauthn.ParseAssertionOptions(string(begin.Options))
		if err != nil {
			t.Fatal(err)
		}
		resp := virtualwebauthn.CreateAssertionResponse(rp, authn, cred, *asOpts)
		return e.admin("POST", "/admin/api/auth/passkey/finish", `{"ticket":"`+begin.Ticket+`","credential":`+resp+`}`)
	}

	e.admin("POST", "/admin/api/auth/logout", "")
	if code, _ := e.admin("GET", "/admin/api/auth/me", ""); code != 401 {
		t.Fatalf("still signed in after logout: %d", code)
	}
	if code, body := signIn(); code != 200 || !strings.Contains(body, `"name":"admin"`) {
		t.Fatalf("passkey login: %d %s", code, body)
	}
	if code, _ := e.admin("GET", "/admin/api/auth/me", ""); code != 200 {
		t.Fatalf("not signed in after passkey login: %d", code)
	}
	var pk db.Passkey
	e.db.First(&pk)
	if pk.Name != "Laptop" || pk.LastUsedAt == nil {
		t.Fatalf("stored passkey: %+v", pk)
	}

	// Disabled users can't sign in.
	e.db.Model(&db.User{}).Where("id = ?", admin.ID).Update("status", db.UserDisabled)
	if code, _ := signIn(); code != 401 {
		t.Fatalf("disabled user passkey login: %d", code)
	}
	e.db.Model(&db.User{}).Where("id = ?", admin.ID).Update("status", db.UserNormal)

	// A removed passkey stops working.
	if code, _ := e.admin("DELETE", "/admin/api/me/passkeys/"+jsonID(pk.ID), ""); code != 204 {
		t.Fatalf("delete passkey: %d", code)
	}
	if code, _ := signIn(); code != 401 {
		t.Fatalf("deleted passkey login: %d", code)
	}
}

func jsonID(id uint) string { b, _ := json.Marshal(id); return string(b) }
