package server

import (
	"encoding/json"
	"strings"
	"testing"
)

// An Xtream Codes movie source's password is write-only: never returned, kept when a save
// leaves it out, and required for such a source.
func TestVodXtreamSourceSettings(t *testing.T) {
	h := newHarness(t)
	var res struct {
		Token string `json:"token"`
	}
	h.do("POST", "/setup", map[string]any{"serverName": "T", "username": "admin", "password": "correct horse", "device": device}, &res)
	h.token = res.Token

	src := map[string]any{"id": "xc", "name": "Dispatcharr", "kind": "xtream", "url": "http://dispatcharr.test:9191/",
		"username": "marquee", "password": "xc-secret-pw", "enabled": true}
	var raw json.RawMessage
	if code := h.do("PATCH", "/settings", map[string]any{"integrations": map[string]any{"vodSources": []any{src}}}, &raw); code != 200 {
		t.Fatalf("save: %d %s", code, raw)
	}
	check := func(body json.RawMessage) {
		t.Helper()
		if strings.Contains(string(body), "xc-secret-pw") {
			t.Fatal("the password came back")
		}
		var s struct {
			Integrations struct {
				VodSources []struct {
					Kind, Url, Username string
					PasswordSet         bool
				}
			}
		}
		json.Unmarshal(body, &s)
		v := s.Integrations.VodSources
		if len(v) != 1 || v[0].Kind != "xtream" || !v[0].PasswordSet || v[0].Username != "marquee" || v[0].Url != "http://dispatcharr.test:9191" {
			t.Fatalf("sources: %s", body)
		}
	}
	check(raw)
	h.do("GET", "/settings", nil, &raw)
	check(raw)

	// Saved again without the password (as the web does): it's kept.
	delete(src, "password")
	src["name"] = "Dispatcharr films"
	if code := h.do("PATCH", "/settings", map[string]any{"integrations": map[string]any{"vodSources": []any{src}}}, &raw); code != 200 {
		t.Fatalf("resave: %d %s", code, raw)
	}
	check(raw)

	// A new Xtream Codes source needs one.
	if code := h.do("PATCH", "/settings", map[string]any{"integrations": map[string]any{"vodSources": []any{
		map[string]any{"name": "New", "kind": "xtream", "url": "http://x.test", "username": "u"}}}}, nil); code != 400 {
		t.Fatalf("no password: %d", code)
	}
	// Existing M3U sources read as m3u.
	if code := h.do("PATCH", "/settings", map[string]any{"integrations": map[string]any{"vodSources": []any{
		map[string]any{"name": "Pluto", "url": "https://example.test/pluto.m3u"}}}}, &raw); code != 200 ||
		!strings.Contains(string(raw), `"kind":"m3u"`) || strings.Contains(string(raw), "passwordSet") {
		t.Fatalf("m3u: %s", raw)
	}
}
