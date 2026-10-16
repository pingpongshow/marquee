package server

import (
	"encoding/json"
	"strings"
	"testing"

	"marquee/internal/api"
	"marquee/internal/plexsync"
	"marquee/internal/settings"
)

// TestPlexSyncSettings covers the Plex sync settings: the token is write-only, automatic
// sync is off by default and needs a server and token, and the plex_sync alert is its own
// switch.
func TestPlexSyncSettings(t *testing.T) {
	h := newHarness(t)
	var res api.AuthResult
	h.do("POST", "/setup", map[string]any{"serverName": "T", "username": "admin", "password": "correct horse", "device": device}, &res)
	h.token = res.Token
	h.handlers.PlexSync = &plexsync.Service{DB: h.db, Settings: h.store}

	var st api.PlexSyncStatus
	if code := h.do("GET", "/plex-sync", nil, &st); code != 200 || st.Settings.Enabled || st.Settings.TokenSet || st.Settings.IntervalMinutes != 30 ||
		!st.Settings.WatchState || st.Reachable != nil || st.NextRunAt != nil {
		t.Fatalf("defaults: %d %+v", code, st)
	}
	if code := h.do("PATCH", "/plex-sync", map[string]any{"enabled": true}, nil); code != 400 {
		t.Errorf("automatic sync without a server: %d", code)
	}
	if code := h.do("POST", "/plex-sync/preview", nil, nil); code != 400 {
		t.Errorf("preview without a server: %d", code)
	}
	if code := h.do("POST", "/plex-sync/run", nil, nil); code != 400 {
		t.Errorf("run without a server: %d", code)
	}
	if code := h.do("PATCH", "/plex-sync", map[string]any{"intervalMinutes": 2}, nil); code != 400 {
		t.Errorf("2-minute interval: %d", code)
	}
	var raw json.RawMessage
	if code := h.do("PATCH", "/plex-sync", map[string]any{"url": "http://127.0.0.1:1/", "token": "very-secret-plex-token",
		"intervalMinutes": 60, "ratings": false}, &raw); code != 200 {
		t.Fatalf("patch: %d %s", code, raw)
	}
	if strings.Contains(string(raw), "very-secret-plex-token") {
		t.Fatal("the token came back")
	}
	json.Unmarshal(raw, &st)
	if !st.Settings.TokenSet || st.Settings.Url != "http://127.0.0.1:1" || st.Settings.IntervalMinutes != 60 || st.Settings.Ratings ||
		st.Reachable == nil || *st.Reachable {
		t.Errorf("after patch: %+v reachable %v", st.Settings, st.Reachable)
	}
	if code := h.do("PATCH", "/plex-sync", map[string]any{"enabled": true}, &st); code != 200 || !st.Settings.Enabled || st.NextRunAt == nil {
		t.Errorf("enable: %d %+v", code, st)
	}
	if code := h.do("PUT", "/plex-sync/users/5", map[string]any{"userId": 1}, nil); code != 404 {
		t.Errorf("unknown Plex account: %d", code)
	}
	h.db.Exec(`INSERT INTO plex_sync_users (plex_account_id, name) VALUES (5, 'Someone')`)
	if code := h.do("PUT", "/plex-sync/users/5", map[string]any{"userId": 999}, nil); code != 400 {
		t.Errorf("unknown Marquee user: %d", code)
	}
	if code := h.do("PUT", "/plex-sync/users/5", map[string]any{"userId": 1}, &st); code != 200 || len(st.Users) != 1 || *st.Users[0].UserId != 1 {
		t.Errorf("link: %d %+v", code, st.Users)
	}
	if code := h.do("PATCH", "/plex-sync", map[string]any{"token": "", "enabled": false}, &st); code != 200 || st.Settings.TokenSet {
		t.Errorf("remove token: %d %+v", code, st.Settings)
	}

	// The plex_sync alert is its own switch, kept when the alert list is replaced.
	var s api.ServerSettings
	h.do("PATCH", "/settings", map[string]any{"notifications": map[string]any{"plexSyncAlert": true, "dvrQuotaAlert": true}}, &s)
	h.do("PATCH", "/settings", map[string]any{"notifications": map[string]any{"alerts": []string{"task_failed"}}}, &s)
	if !*s.Notifications.PlexSyncAlert || !*s.Notifications.DvrQuotaAlert || len(*s.Notifications.Alerts) != 1 || !h.store.Get().Notifications.AlertOn(settings.AlertPlexSync) {
		t.Errorf("alert kept: %v %v", *s.Notifications.PlexSyncAlert, *s.Notifications.Alerts)
	}
	h.do("PATCH", "/settings", map[string]any{"notifications": map[string]any{"plexSyncAlert": false}}, &s)
	if *s.Notifications.PlexSyncAlert || h.store.Get().Notifications.AlertOn(settings.AlertPlexSync) || !*s.Notifications.DvrQuotaAlert {
		t.Error("alert off")
	}
}
