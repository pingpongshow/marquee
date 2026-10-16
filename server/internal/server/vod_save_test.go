package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// Saving to the library is for administrators only, and its folders must exist and be
// writable when they're set.
func TestVodSaveAdminOnlyAndFolders(t *testing.T) {
	h := newHarness(t)
	var res struct {
		Token string `json:"token"`
	}
	h.do("POST", "/setup", map[string]any{"serverName": "T", "username": "admin", "password": "correct horse", "device": device}, &res)
	h.token = res.Token
	admin := h.token

	// Defaults.
	var s struct {
		VodSave struct {
			Quality             string
			ConcurrentPerSource int
			ScanAfterSave       bool
			SkipExisting        bool
			PauseWhileWatching  bool
			PauseScope          string
			RetryFailed         bool
			MoviesFolder        string
		}
	}
	h.do("GET", "/settings", nil, &s)
	if v := s.VodSave; v.Quality != "original" || v.ConcurrentPerSource != 1 || !v.ScanAfterSave || !v.SkipExisting ||
		!v.PauseWhileWatching || v.PauseScope != "source" || !v.RetryFailed || v.MoviesFolder != "" {
		t.Fatalf("defaults: %+v", v)
	}
	movies := filepath.Join(h.media, "video", "Movies")
	var raw json.RawMessage
	if code := h.do("PATCH", "/settings", map[string]any{"vodSave": map[string]any{"moviesFolder": movies, "quality": "transcode",
		"videoCodec": "h264", "concurrentPerSource": 2}}, &raw); code != 200 {
		t.Fatalf("save: %d %s", code, raw)
	}
	json.Unmarshal(raw, &s)
	if s.VodSave.MoviesFolder != movies || s.VodSave.Quality != "transcode" || s.VodSave.ConcurrentPerSource != 2 || !s.VodSave.SkipExisting {
		t.Fatalf("saved: %+v", s.VodSave)
	}
	for name, body := range map[string]map[string]any{
		"missing folder":  {"showsFolder": filepath.Join(h.media, "nope")},
		"relative folder": {"showsFolder": "video/Shows"},
		"concurrency":     {"concurrentPerSource": 9},
		"quality":         {"quality": "best"},
	} {
		if code := h.do("PATCH", "/settings", map[string]any{"vodSave": body}, &raw); code != 400 {
			t.Errorf("%s: %d %s", name, code, raw)
		}
	}
	if os.Getuid() != 0 {
		ro := filepath.Join(h.media, "readonly")
		os.MkdirAll(ro, 0o555)
		if code := h.do("PATCH", "/settings", map[string]any{"vodSave": map[string]any{"showsFolder": ro}}, &raw); code != 400 {
			t.Errorf("read-only folder: %d %s", code, raw)
		}
	}

	// Another user can't see or use the queue.
	h.do("POST", "/users", map[string]any{"username": "friend", "password": "friend-password"}, nil)
	h.token = ""
	var fr struct {
		Token string `json:"token"`
	}
	h.do("POST", "/auth/login", map[string]any{"username": "friend", "password": "friend-password", "device": device}, &fr)
	h.token = fr.Token
	for _, c := range []struct{ method, path string }{
		{"GET", "/livetv/vod/saves"}, {"POST", "/livetv/vod/movies/1/save"}, {"POST", "/livetv/vod/episodes/1/save"},
		{"POST", "/livetv/vod/series/1/save?season=1"}, {"DELETE", "/livetv/vod/saves/1"}, {"POST", "/livetv/vod/saves/1/retry"},
		{"POST", "/livetv/vod/saves/1/top"}, {"POST", "/livetv/vod/saves/1/pause"}, {"POST", "/livetv/vod/saves/1/resume"},
		{"POST", "/livetv/vod/saves/pause"}, {"POST", "/livetv/vod/saves/resume"}, {"POST", "/livetv/vod/saves/clear"},
	} {
		if code := h.do(c.method, c.path, nil, nil); code != 403 {
			t.Errorf("%s %s as a non-admin: %d", c.method, c.path, code)
		}
	}
	h.token = ""
	if code := h.do("GET", "/livetv/vod/saves", nil, nil); code != 401 {
		t.Errorf("signed out: %d", code)
	}
	// The admin gets through (saving isn't set up in this harness).
	h.token = admin
	if code := h.do("GET", "/livetv/vod/saves", nil, nil); code != 503 {
		t.Errorf("admin: %d", code)
	}
}
