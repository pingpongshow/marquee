package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"marquee/internal/api"
)

// Away from home, switching into a two-factor account is refused however it's asked for:
// a PIN sent alongside any password used to get through without the authenticator code.
func TestRemoteSwitchIntoTwoFactorAccountRefused(t *testing.T) {
	h := newHarness(t)
	var res api.AuthResult
	if code := h.do("POST", "/setup", map[string]any{"serverName": "Tower", "username": "admin", "password": "correct horse", "device": device}, &res); code != 200 {
		t.Fatalf("setup: %d", code)
	}
	h.token = res.Token
	var mum api.User
	if code := h.do("POST", "/users", map[string]any{"username": "mum", "password": "mum-password", "pin": "1234"}, &mum); code != 201 {
		t.Fatalf("create: %d", code)
	}
	if _, err := h.db.Exec(`INSERT INTO user_totp (user_id, secret) VALUES (?, 'JBSWY3DPEHPK3PXP')`, mum.Id); err != nil {
		t.Fatal(err)
	}
	switchRemote := func(body map[string]any) int {
		var buf bytes.Buffer
		json.NewEncoder(&buf).Encode(body)
		req, _ := http.NewRequest("POST", fmt.Sprintf("%s/api/v1/profiles/%d/switch", h.srv.URL, mum.Id), &buf)
		req.Host = "tower.tail1234.ts.net" // the Tailscale name: remote
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+h.token)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	for _, body := range []map[string]any{
		{"pin": "1234", "device": device},
		{"pin": "1234", "password": "anything", "device": device},
		{"password": "mum-password", "device": device},
	} {
		if code := switchRemote(body); code != 401 {
			t.Errorf("remote switch %v: %d", body, code)
		}
	}
	// At home the PIN still works.
	if code := h.do("POST", fmt.Sprintf("/profiles/%d/switch", mum.Id), map[string]any{"pin": "1234", "device": device}, nil); code != 200 {
		t.Errorf("local switch: %d", code)
	}
}
