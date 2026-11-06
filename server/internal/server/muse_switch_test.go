package server

import (
	"testing"

	"marquee/internal/api"
)

// An admin can turn Muse off for the whole server: system info tells the apps, and its
// requests are refused for anything still showing it.
func TestMuseSwitch(t *testing.T) {
	h, _ := newDiscoveryHarness(t, "http://127.0.0.1:1")
	var res api.AuthResult
	h.do("POST", "/setup", map[string]any{"serverName": "T", "username": "admin", "password": "correct horse", "device": device}, &res)
	h.token = res.Token

	muse := func() bool {
		var info api.SystemInfo
		if code := h.do("GET", "/system/info", nil, &info); code != 200 || info.Muse == nil {
			t.Fatalf("system info: %d %+v", code, info.Muse)
		}
		return *info.Muse
	}
	turn := func(on bool) {
		var saved api.ServerSettings
		if code := h.do("PATCH", "/settings", map[string]any{"general": map[string]any{"muse": on}}, &saved); code != 200 || *saved.General.Muse != on {
			t.Fatalf("turn Muse to %v: %d", on, code)
		}
	}
	refused := func(path string) bool {
		var e api.Error
		code := h.do("POST", path, map[string]any{"prompt": "time travel"}, &e)
		return code == 503 && e.Code == "muse_off"
	}

	if !muse() {
		t.Fatal("Muse is off on a new server")
	}
	if refused("/muse/video") {
		t.Fatal("refused while on")
	}
	turn(false)
	if muse() {
		t.Fatal("system info still says on")
	}
	for _, path := range []string{"/muse/video", "/music/muse"} {
		if !refused(path) {
			t.Errorf("%s not refused while off", path)
		}
	}
	turn(true)
	if !muse() || refused("/muse/video") {
		t.Fatal("still off after turning it back on")
	}
}
