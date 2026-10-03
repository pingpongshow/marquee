package server

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"marquee/internal/api"
	"marquee/internal/library"
)

// TestDeleteDuplicateFile covers comparing duplicates and deleting one to the trash from
// Library Health (ADM-11) over HTTP.
func TestDeleteDuplicateFile(t *testing.T) {
	h := newHarness(t)
	var res api.AuthResult
	h.do("POST", "/setup", map[string]any{"serverName": "T", "username": "admin", "password": "correct horse", "device": device}, &res)
	h.token = res.Token
	root := filepath.Join(h.media, "video/Movies")
	var lib api.Library
	h.do("POST", "/libraries", map[string]any{"name": "Movies", "type": "movies", "paths": []string{root}}, &lib)
	exec := func(q string, args ...any) int64 {
		r, err := h.db.Exec(q, args...)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := r.LastInsertId()
		return id
	}
	movie := func(title, rel string) (int64, int64, string) {
		id := exec(`INSERT INTO items(library_id, type, title, sort_title, match_state) VALUES (?, 'movie', ?, ?, 'matched')`, lib.Id, title, title)
		exec(`INSERT INTO external_ids(item_id, provider, value) VALUES (?, 'tmdb', '949')`, id)
		p := filepath.Join(root, rel)
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte("video"), 0o644)
		v := exec(`INSERT INTO media_versions(item_id) VALUES (?)`, id)
		f := exec(`INSERT INTO media_files(version_id, library_id, path, size, mtime, duration_ms, width, height, bitrate_kbps)
			VALUES (?, ?, ?, 5, 1, 6000000, 1920, 1080, 5000)`, v, lib.Id, p)
		exec(`INSERT INTO streams(file_id, kind, codec) VALUES (?, 'video', 'h264')`, f)
		return id, f, p
	}
	heat, heatFile, heatPath := movie("Heat", "Heat (1995)/Heat (1995).mkv")
	copyID, copyFile, copyPath := movie("Heat", "Heat copy/Heat (1995).mkv")

	// Duplicates list every file of both copies, with paths, to compare.
	var page api.HealthIssuePage
	if code := h.do("GET", "/library-health/duplicates", nil, &page); code != 200 || len(page.Items) != 2 {
		t.Fatalf("duplicates: %d %+v", code, page)
	}
	for _, is := range page.Items {
		if is.Files == nil || len(*is.Files) != 2 {
			t.Fatalf("files: %+v", is.Files)
		}
		f := *is.Files
		if f[0].ItemId != is.Item.Id || f[0].File.Path == nil || f[0].File.Streams == nil || len(f[0].File.Streams) != 1 || f[0].AddedAt.IsZero() {
			t.Errorf("first file should be the item's own, with path and streams: %+v", f[0])
		}
	}

	// Off by default.
	if code := h.do("DELETE", fmt.Sprintf("/files/%d", copyFile), nil, nil); code != 403 {
		t.Errorf("deletion off: %d", code)
	}
	var set api.ServerSettings
	h.do("PATCH", "/settings", map[string]any{"library": map[string]any{"allowMediaDeletion": true}}, &set)
	if set.Library.AllowMediaDeletion == nil || !*set.Library.AllowMediaDeletion {
		t.Fatalf("setting: %+v", set.Library)
	}

	// A read-only folder can't give up its file.
	os.Chmod(filepath.Dir(heatPath), 0o555)
	if code := h.do("DELETE", fmt.Sprintf("/files/%d", heatFile), nil, nil); code != 409 && os.Geteuid() != 0 {
		t.Errorf("read-only: %d", code)
	}
	os.Chmod(filepath.Dir(heatPath), 0o755)

	if code := h.do("DELETE", fmt.Sprintf("/files/%d", copyFile), nil, nil); code != 204 {
		t.Fatalf("delete: %d", code)
	}
	if _, err := os.Stat(copyPath); !os.IsNotExist(err) {
		t.Error("file still in place")
	}
	matches, _ := filepath.Glob(filepath.Join(root, library.TrashDir, "*", "Heat copy", "Heat (1995).mkv"))
	if len(matches) != 1 {
		t.Errorf("not in the trash: %v", matches)
	}
	if code := h.do("GET", fmt.Sprintf("/items/%d", copyID), nil, nil); code != 404 {
		t.Errorf("the copy's item should be gone: %d", code)
	}
	if code := h.do("GET", fmt.Sprintf("/items/%d", heat), nil, nil); code != 200 {
		t.Errorf("the other copy should stay: %d", code)
	}
	if code := h.do("DELETE", fmt.Sprintf("/files/%d", copyFile), nil, nil); code != 404 {
		t.Errorf("deleted twice: %d", code)
	}

	// Admins only.
	var kid api.User
	h.do("POST", "/users", map[string]any{"username": "kid", "isManaged": true}, &kid)
	var sw api.AuthResult
	h.do("POST", fmt.Sprintf("/profiles/%d/switch", kid.Id), map[string]any{"device": map[string]any{"clientId": "kid-client", "name": "Kid", "platform": "web"}}, &sw)
	h.token = sw.Token
	if code := h.do("DELETE", fmt.Sprintf("/files/%d", heatFile), nil, nil); code != 403 {
		t.Errorf("non-admin: %d", code)
	}
}
