package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"marquee/internal/vod"
	"marquee/internal/vodsave"
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

	// More save folders: they must exist and be writable, with names of their own; each
	// gets an id and says whether a library holds it.
	anime := filepath.Join(h.media, "Anime")
	os.MkdirAll(anime, 0o755)
	dest := func(name, folder, kind string) map[string]any {
		return map[string]any{"name": name, "folder": folder, "kind": kind}
	}
	for name, ds := range map[string][]any{
		"missing folder":  {dest("Anime", filepath.Join(h.media, "nope"), "both")},
		"duplicate names": {dest("Anime", anime, "both"), dest("anime", movies, "movies")},
		"relative folder": {dest("Anime", "Anime", "both")},
		"kind":            {dest("Anime", anime, "music")},
		"no name":         {dest(" ", anime, "both")},
	} {
		if code := h.do("PATCH", "/settings", map[string]any{"vodSave": map[string]any{"destinations": ds}}, &raw); code != 400 {
			t.Errorf("destinations, %s: %d %s", name, code, raw)
		}
	}
	if os.Getuid() != 0 {
		ro := filepath.Join(h.media, "readonly2")
		os.MkdirAll(ro, 0o555)
		if code := h.do("PATCH", "/settings", map[string]any{"vodSave": map[string]any{"destinations": []any{dest("RO", ro, "both")}}}, &raw); code != 400 {
			t.Errorf("read-only destination: %d %s", code, raw)
		}
	}
	type destOut struct {
		ID, Name, Folder, Kind string
		InLibrary              *bool
	}
	var ds struct {
		VodSave struct{ Destinations []destOut }
	}
	if code := h.do("PATCH", "/settings", map[string]any{"vodSave": map[string]any{"destinations": []any{dest(" Anime ", anime, "both")}}}, &ds); code != 200 {
		t.Fatalf("destination: %d", code)
	}
	got := ds.VodSave.Destinations
	if len(got) != 1 || got[0].ID == "" || got[0].Name != "Anime" || got[0].Folder != anime || got[0].Kind != "both" || got[0].InLibrary == nil || *got[0].InLibrary {
		t.Fatalf("destinations: %+v", got)
	}
	// Renaming keeps it (by id); a library holding the folder shows.
	h.do("POST", "/libraries", map[string]any{"name": "Anime", "type": "movies", "paths": []string{anime}}, nil)
	keep := map[string]any{"id": got[0].ID, "name": "Animation", "folder": anime, "kind": "shows"}
	if code := h.do("PATCH", "/settings", map[string]any{"vodSave": map[string]any{"destinations": []any{keep}}}, &ds); code != 200 {
		t.Fatalf("rename: %d", code)
	}
	if g := ds.VodSave.Destinations; len(g) != 1 || g[0].ID != got[0].ID || g[0].Name != "Animation" || g[0].InLibrary == nil || !*g[0].InLibrary {
		t.Errorf("renamed: %+v", g)
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
		{"POST", "/livetv/vod/movies/1/save?destination=" + got[0].ID}, {"POST", "/livetv/vod/episodes/1/save?destination=" + got[0].ID},
		{"POST", "/livetv/vod/series/1/save?destination=" + got[0].ID}, {"GET", "/settings"},
	} {
		if code := h.do(c.method, c.path, nil, nil); code != 403 {
			t.Errorf("%s %s as a non-admin: %d", c.method, c.path, code)
		}
	}
	if code := h.do("PATCH", "/settings", map[string]any{"vodSave": map[string]any{"destinations": []any{}}}, nil); code != 403 {
		t.Errorf("changing save folders as a non-admin: %d", code)
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

	// With saving set up, a save folder of the wrong kind, or one that isn't there, is a 400.
	h.handlers.VOD = &vod.Service{DB: h.db, Settings: h.store, Streams: &vod.Streams{}}
	h.handlers.VODSaves = &vodsave.Service{DB: h.db, Settings: h.store, VOD: h.handlers.VOD}
	for _, path := range []string{
		"/livetv/vod/movies/1/save?destination=" + got[0].ID, // Animation takes shows only now
		"/livetv/vod/movies/1/save?destination=nope",
		"/livetv/vod/episodes/1/save?destination=nope",
		"/livetv/vod/series/1/save?destination=nope",
	} {
		var e struct{ Code string }
		if code := h.do("POST", path, nil, &e); code != 400 || e.Code != "bad_destination" {
			t.Errorf("%s: %d %+v", path, code, e)
		}
	}
	if code := h.do("GET", "/livetv/vod/saves", nil, nil); code != 200 {
		t.Errorf("admin, set up: %d", code)
	}
}
