package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"marquee/internal/api"
	"marquee/internal/commercials"
	"marquee/internal/library"
	"marquee/internal/mediatrash"
)

// Commercials over the API: the admin setting, per-recording status, removing and
// restoring, admins only.
func TestCommercialsAPI(t *testing.T) {
	h := newHarness(t)
	var res api.AuthResult
	h.do("POST", "/setup", map[string]any{"serverName": "T", "username": "admin", "password": "correct horse", "device": device}, &res)
	h.token = res.Token

	dir := t.TempDir()
	rec := filepath.Join(dir, "recordings")
	path := filepath.Join(rec, "TV", "News", "News - 2026-10-02.mkv")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte("not really video"), 0o644)
	comskip := filepath.Join(dir, "comskip")
	os.WriteFile(comskip, []byte("#!/bin/sh\nfor a in \"$@\"; do case \"$a\" in --output=*) out=\"${a#--output=}\";; esac; done\n"+
		"printf '600.0\\t780.0\\t0\\n' > \"$out/cut.edl\"\n"), 0o755)
	ads := &commercials.Service{DB: h.db, Comskip: filepath.Join(dir, "no-comskip"), Dir: rec,
		Mode:  func() string { return h.store.Get().Integrations.Commercials },
		Trash: &mediatrash.Service{DB: h.db}}
	h.handlers.Commercials = ads
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go ads.Run(ctx)

	// The setting: off by default; the build has no Comskip.
	var s api.ServerSettings
	h.do("GET", "/settings", nil, &s)
	if i := s.Integrations; i == nil || *i.Commercials != api.IntegrationSettingsCommercialsOff || *i.ComskipAvailable {
		t.Fatalf("settings: %+v", s.Integrations)
	}
	if code := h.do("PATCH", "/settings", map[string]any{"integrations": map[string]any{"commercials": "remove"}}, &s); code != 200 || *s.Integrations.Commercials != api.IntegrationSettingsCommercialsRemove {
		t.Fatalf("remove: %d", code)
	}
	if code := h.do("PATCH", "/settings", map[string]any{"integrations": map[string]any{"commercials": "sometimes"}}, nil); code != 400 {
		t.Fatalf("bad value: %d", code)
	}

	lib, err := library.NewStore(h.db).Create(ctx, "Recorded TV", library.Shows, []string{filepath.Join(rec, "TV")}, library.Options{})
	if err != nil {
		t.Fatal(err)
	}
	addFile := func(title, p string) (item int64) {
		r, _ := h.db.Exec(`INSERT INTO items (library_id, type, title, sort_title) VALUES (?, 'movie', ?, ?)`, lib.ID, title, title)
		item, _ = r.LastInsertId()
		r, _ = h.db.Exec(`INSERT INTO media_versions (item_id) VALUES (?)`, item)
		v, _ := r.LastInsertId()
		h.db.Exec(`INSERT INTO media_files (version_id, library_id, path, size, mtime, duration_ms, available) VALUES (?, ?, ?, 5, 100, 1800000, 1)`, v, lib.ID, p)
		return item
	}
	news := addFile("News", path)
	film := addFile("Film", filepath.Join(dir, "media", "film.mp4"))

	var scan api.CommercialScan
	if code := h.do("GET", "/items/"+itoa(news)+"/commercials", nil, &scan); code != 200 || scan.Status != api.CommercialScanStatusPending || len(scan.Breaks) != 0 || *scan.Available || *scan.CanRestore {
		t.Fatalf("pending: %d %+v", code, scan)
	}
	if code := h.do("GET", "/items/"+itoa(film)+"/commercials", nil, nil); code != 404 {
		t.Fatalf("not a recording: %d", code)
	}
	var raw json.RawMessage
	if code := h.do("POST", "/items/"+itoa(news)+"/commercials/remove", nil, &raw); code != 503 || !strings.Contains(string(raw), "comskip_unavailable") {
		t.Fatalf("no comskip: %d %s", code, raw)
	}

	// With Comskip: found, then the cut fails (this "recording" isn't video) and the file is
	// left as it was.
	ads.Comskip = comskip
	if code := h.do("POST", "/items/"+itoa(news)+"/commercials/remove", nil, &scan); code != 202 || !*scan.Available {
		t.Fatalf("remove: %d %+v", code, scan)
	}
	deadline := time.Now().Add(20 * time.Second)
	for scan.Status != api.CommercialScanStatusFailed && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
		h.do("GET", "/items/"+itoa(news)+"/commercials", nil, &scan)
	}
	if scan.Status != api.CommercialScanStatusFailed || len(scan.Breaks) != 1 || scan.Breaks[0] != (api.CommercialBreak{StartMs: 600000, EndMs: 780000}) ||
		scan.Error == nil || !strings.Contains(*scan.Error, "unchanged") {
		t.Fatalf("failed cut: %+v", scan)
	}
	if b, _ := os.ReadFile(path); string(b) != "not really video" {
		t.Error("the recording changed")
	}
	if code := h.do("POST", "/items/"+itoa(news)+"/commercials/restore", nil, &raw); code != 409 || !strings.Contains(string(raw), "no_original") {
		t.Errorf("restore without an original: %d %s", code, raw)
	}

	// Others can't see or change anything.
	h.do("POST", "/users", map[string]any{"username": "viewer", "password": "correct horse"}, nil)
	var vres api.AuthResult
	h.do("POST", "/auth/login", map[string]any{"username": "viewer", "password": "correct horse", "device": device}, &vres)
	h.token = vres.Token
	for _, c := range []struct{ method, path string }{{"GET", ""}, {"POST", "/remove"}, {"POST", "/restore"}} {
		if code := h.do(c.method, "/items/"+itoa(news)+"/commercials"+c.path, nil, nil); code != 403 {
			t.Errorf("viewer %s %s: %d", c.method, c.path, code)
		}
	}
}

// Cutting changes the files, not playback: nothing about commercials reaches the apps'
// playback or preferences, and Marker.kind stays as the shipped apps decode it.
func TestCommercialsLeavePlaybackAPIAlone(t *testing.T) {
	spec, err := api.GetSwagger()
	if err != nil {
		t.Fatal(err)
	}
	kinds := spec.Components.Schemas["Marker"].Value.Properties["kind"].Value.Enum
	if len(kinds) != 2 || kinds[0] != "intro" || kinds[1] != "credits" {
		t.Errorf("Marker.kind enum: %v", kinds)
	}
	if _, ok := spec.Components.Schemas["PlaybackSession"].Value.Properties["commercials"]; ok {
		t.Error("PlaybackSession has commercials")
	}
	for name := range spec.Components.Schemas["UserPreferences"].Value.Properties {
		if strings.Contains(strings.ToLower(name), "commercial") {
			t.Errorf("UserPreferences.%s", name)
		}
	}
}
