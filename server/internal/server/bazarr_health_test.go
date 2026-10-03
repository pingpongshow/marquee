package server

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"marquee/internal/api"
	"marquee/internal/bazarr"
	"marquee/internal/bazarr/bazarrtest"
)

// TestBazarrAndLibraryHealth covers Bazarr subtitles (META-12) and library health (ADM-11)
// over HTTP, against a fake Bazarr that sees the movies under /movies.
func TestBazarrAndLibraryHealth(t *testing.T) {
	h := newHarness(t)
	var res api.AuthResult
	h.do("POST", "/setup", map[string]any{"serverName": "T", "username": "admin", "password": "correct horse", "device": device}, &res)
	h.token = res.Token
	var lib api.Library
	h.do("POST", "/libraries", map[string]any{"name": "Movies", "type": "movies", "paths": []string{filepath.Join(h.media, "video/Movies")}}, &lib)
	exec := func(q string, args ...any) int64 {
		r, err := h.db.Exec(q, args...)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := r.LastInsertId()
		return id
	}
	movie := func(title, rel string, w, ht int) (int64, int64) {
		id := exec(`INSERT INTO items(library_id, type, title, sort_title, match_state) VALUES (?, 'movie', ?, ?, 'matched')`, lib.Id, title, title)
		p := filepath.Join(h.media, "video/Movies", rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte("video"), 0o644)
		v := exec(`INSERT INTO media_versions(item_id) VALUES (?)`, id)
		f := exec(`INSERT INTO media_files(version_id, library_id, path, size, mtime, duration_ms, width, height, bitrate_kbps)
			VALUES (?, ?, ?, 5, 1, 6000000, ?, ?, 5000)`, v, lib.Id, p, w, ht)
		exec(`INSERT INTO streams(file_id, kind, codec) VALUES (?, 'video', 'h264')`, f)
		return id, f
	}
	heat, heatFile := movie("Heat", "Heat (1995)/Heat (1995).mkv", 1920, 1080)
	old, _ := movie("Old", "Old (1950)/Old (1950).avi", 640, 480)
	show := exec(`INSERT INTO items(library_id, type, title, sort_title) VALUES (?, 'show', 'A Show', 'A Show')`, lib.Id)

	fake := (&bazarrtest.Fake{
		Local: func(p string) string {
			return filepath.Join(h.media, "video/Movies", strings.TrimPrefix(p, "/movies/"))
		},
		Movies: []bazarr.Movie{{RadarrID: 7, Title: "Heat", Path: "/movies/Heat (1995)/Heat (1995).mkv",
			Missing: []bazarr.Language{{Name: "English", Code2: "en", Code3: "eng"}}}},
	}).Start(t)

	// Not set up yet.
	var st api.BazarrStatus
	if code := h.do("GET", fmt.Sprintf("/items/%d/subtitles/bazarr", heat), nil, &st); code != 200 || st.Configured || st.Languages == nil {
		t.Fatalf("unconfigured status: %d %+v", code, st)
	}
	if code := h.do("POST", fmt.Sprintf("/items/%d/subtitles/bazarr", heat), map[string]any{"language": "en"}, nil); code != 404 {
		t.Fatalf("unconfigured download: %d", code)
	}
	var checks []api.HealthCheck
	h.do("GET", "/library-health", nil, &checks)
	if len(checks) != 8 {
		t.Fatalf("checks: %+v", checks)
	}
	for _, c := range checks {
		if c.Id == "missingSubtitles" && (c.Available == nil || *c.Available) {
			t.Errorf("missingSubtitles without Bazarr: %+v", c)
		}
	}

	// Settings: the key is write-only.
	if code := h.do("PATCH", "/settings", map[string]any{"integrations": map[string]any{"bazarrUrl": "not a url"}}, nil); code != 400 {
		t.Errorf("bad URL accepted: %d", code)
	}
	var set api.ServerSettings
	h.do("PATCH", "/settings", map[string]any{"integrations": map[string]any{"bazarrUrl": fake.URL + "/", "bazarrApiKey": bazarrtest.APIKey}}, &set)
	if i := set.Integrations; i == nil || *i.BazarrUrl != fake.URL || !*i.BazarrApiKeySet {
		t.Fatalf("settings: %+v", set.Integrations)
	}

	h.do("GET", fmt.Sprintf("/items/%d/subtitles/bazarr", heat), nil, &st)
	if !st.Configured || !st.Managed || len(st.Languages) != 1 || st.Languages[0].Code2 != "en" || st.Languages[0].Have {
		t.Fatalf("status: %+v", st)
	}
	h.do("GET", fmt.Sprintf("/items/%d/subtitles/bazarr", show), nil, &st)
	if !st.Configured || st.Managed {
		t.Fatalf("show status: %+v", st)
	}
	if code := h.do("POST", fmt.Sprintf("/items/%d/subtitles/bazarr", old), map[string]any{"language": "en"}, nil); code != 404 {
		t.Errorf("unmanaged download: %d", code)
	}
	if code := h.do("POST", fmt.Sprintf("/items/%d/subtitles/bazarr", heat), map[string]any{"language": "en; rm"}, nil); code != 400 {
		t.Errorf("bad language: %d", code)
	}

	// Library health sees what Bazarr wants.
	var page api.HealthIssuePage
	h.do("GET", "/library-health/missingSubtitles", nil, &page)
	if page.Total != 1 || page.Items[0].Item.Id != heat || page.Items[0].Detail != "Missing English" {
		t.Fatalf("missing subtitles: %+v", page)
	}

	// A download runs in the background; the new file shows up on the item.
	if code := h.do("POST", fmt.Sprintf("/items/%d/subtitles/bazarr", heat), map[string]any{"language": "en"}, nil); code != 202 {
		t.Fatalf("download: %d", code)
	}
	waitSubtitles := func(n int) {
		t.Helper()
		for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(20 * time.Millisecond) {
			var got int
			h.db.QueryRow(`SELECT COUNT(*) FROM streams WHERE file_id = ? AND kind = 'subtitle'`, heatFile).Scan(&got)
			if got == n {
				return
			}
		}
		t.Fatalf("subtitle streams never reached %d; Bazarr calls %v", n, fake.Calls())
	}
	waitSubtitles(1)
	var d api.ItemDetail
	h.do("GET", fmt.Sprintf("/items/%d", heat), nil, &d)
	found := false
	for _, s := range d.Versions[0].Files[0].Streams {
		found = found || (s.Kind == "subtitle" && s.Language != nil && *s.Language == "eng")
	}
	if !found {
		t.Fatalf("detail streams: %+v", d.Versions[0].Files[0].Streams)
	}
	h.do("GET", fmt.Sprintf("/items/%d/subtitles/bazarr", heat), nil, &st)
	if len(st.Languages) != 1 || !st.Languages[0].Have {
		t.Fatalf("after download: %+v", st)
	}

	// Manual search, best first, then a pick.
	var cands []api.BazarrCandidate
	if code := h.do("GET", fmt.Sprintf("/items/%d/subtitles/bazarr/search", heat), nil, &cands); code != 200 || len(cands) != 2 ||
		cands[0].Score != 95 || cands[0].Subtitle != "b64:best" || !*cands[0].Hi || *cands[0].Release != "Some.Release.1080p, Other" {
		t.Fatalf("search: %d %+v", code, cands)
	}
	if code := h.do("POST", fmt.Sprintf("/items/%d/subtitles/bazarr/search", heat), map[string]any{"provider": cands[0].Provider, "subtitle": cands[0].Subtitle, "hi": true}, nil); code != 202 {
		t.Fatalf("pick: %d", code)
	}
	waitSubtitles(2)

	// Bazarr down: status reports it, actions are 502.
	fake.Fail = true
	h.do("GET", fmt.Sprintf("/items/%d/subtitles/bazarr", heat), nil, &st)
	if st.Error == nil || st.Managed {
		t.Errorf("status while down: %+v", st)
	}
	if code := h.do("GET", fmt.Sprintf("/items/%d/subtitles/bazarr/search", heat), nil, nil); code != 502 {
		t.Errorf("search while down: %d", code)
	}
	fake.Fail = false

	// Health: issues with summaries, ignore and restore.
	h.do("GET", "/library-health/upgrades", nil, &page)
	if page.Total != 1 || page.Items[0].Item.Id != old || page.Items[0].Detail != "480p · 5.0 Mbps" || page.Items[0].Path == nil {
		t.Fatalf("upgrades: %+v", page)
	}
	if code := h.do("PUT", fmt.Sprintf("/library-health/upgrades/ignored/%d", old), nil, nil); code != 204 {
		t.Fatalf("ignore: %d", code)
	}
	h.do("GET", "/library-health/upgrades", nil, &page)
	if page.Total != 0 || len(page.Items) != 0 {
		t.Fatalf("after ignore: %+v", page)
	}
	if code := h.do("DELETE", fmt.Sprintf("/library-health/upgrades/ignored/%d", old), nil, nil); code != 204 {
		t.Fatalf("unignore: %d", code)
	}
	h.do("GET", "/library-health/upgrades", nil, &page)
	if page.Total != 1 {
		t.Fatalf("after unignore: %+v", page)
	}
	if code := h.do("PUT", fmt.Sprintf("/library-health/nope/ignored/%d", old), nil, nil); code != 404 {
		t.Errorf("unknown check ignore: %d", code)
	}
	if code := h.do("GET", "/library-health/nope", nil, nil); code != 404 {
		t.Errorf("unknown check: %d", code)
	}

	// Other people: health is admin-only; Bazarr works for anyone who can see the item.
	var kid, teen api.User
	var other api.Library
	h.do("POST", "/libraries", map[string]any{"name": "Shows", "type": "shows", "paths": []string{filepath.Join(h.media, "video/Shows")}}, &other)
	h.do("POST", "/users", map[string]any{"username": "kid", "isManaged": true, "restrictions": map[string]any{"libraryIds": []int64{other.Id}}}, &kid)
	h.do("POST", "/users", map[string]any{"username": "teen", "isManaged": true}, &teen)
	var sw api.AuthResult
	h.do("POST", fmt.Sprintf("/profiles/%d/switch", teen.Id), map[string]any{"device": map[string]any{"clientId": "teen-client", "name": "Teen", "platform": "web"}}, &sw)
	h.token = sw.Token
	if code := h.do("GET", "/library-health", nil, nil); code != 403 {
		t.Errorf("non-admin health: %d", code)
	}
	if code := h.do("PUT", fmt.Sprintf("/library-health/upgrades/ignored/%d", old), nil, nil); code != 403 {
		t.Errorf("non-admin ignore: %d", code)
	}
	if code := h.do("GET", fmt.Sprintf("/items/%d/subtitles/bazarr", heat), nil, &st); code != 200 || !st.Managed {
		t.Errorf("non-admin status: %d %+v", code, st)
	}
	// Switching signed this device out of the admin profile.
	h.token = ""
	h.do("POST", "/auth/login", map[string]any{"username": "admin", "password": "correct horse", "device": device}, &sw)
	h.token = sw.Token
	if code := h.do("POST", fmt.Sprintf("/profiles/%d/switch", kid.Id), map[string]any{"device": map[string]any{"clientId": "kid-client", "name": "Kid", "platform": "web"}}, &sw); code != 200 {
		t.Fatalf("switch to kid: %d", code)
	}
	h.token = sw.Token
	if code := h.do("GET", fmt.Sprintf("/items/%d/subtitles/bazarr", heat), nil, nil); code != 404 {
		t.Errorf("hidden item status: %d", code)
	}
	if code := h.do("POST", fmt.Sprintf("/items/%d/subtitles/bazarr", heat), map[string]any{"language": "en"}, nil); code != 404 {
		t.Errorf("hidden item download: %d", code)
	}
	h.token = ""
	if code := h.do("GET", fmt.Sprintf("/items/%d/subtitles/bazarr", heat), nil, nil); code != 401 {
		t.Errorf("anonymous: %d", code)
	}
}
