package server

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"marquee/internal/api"
)

// A recorded (Live TV) season downloads as a zip of its episodes like any other season.
func TestRecordedSeasonZipDownload(t *testing.T) {
	h := newHarness(t)
	var res api.AuthResult
	h.do("POST", "/setup", map[string]any{"serverName": "T", "username": "admin", "password": "correct horse", "device": device}, &res)
	h.token = res.Token
	exec := func(q string, args ...any) int64 {
		r, err := h.db.Exec(q, args...)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := r.LastInsertId()
		return id
	}
	lib := exec(`INSERT INTO libraries(name, type, recordings) VALUES ('Recordings', 'shows', 1)`)
	show := exec(`INSERT INTO items(library_id, type, title, sort_title) VALUES (?, 'show', 'News', 'News')`, lib)
	season := exec(`INSERT INTO items(library_id, type, title, sort_title, parent_id, idx) VALUES (?, 'season', 'Season 1', 'Season 1', ?, 1)`, lib, show)
	ep := exec(`INSERT INTO items(library_id, type, title, sort_title, parent_id, grandparent_id, idx) VALUES (?, 'episode', 'Ep 1', 'Ep 1', ?, ?, 1)`,
		lib, season, show)
	p := filepath.Join(h.media, "rec", "News S01E01.ts")
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte("recording"), 0o644)
	v := exec(`INSERT INTO media_versions(item_id) VALUES (?)`, ep)
	exec(`INSERT INTO media_files(version_id, library_id, path, size, mtime) VALUES (?, ?, ?, 9, 1)`, v, lib, p)

	resp, body := h.raw("GET", fmt.Sprintf("/api/v1/download/zip/%d", season), nil)
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil || len(zr.File) != 1 {
		t.Fatalf("zip: %v %+v", err, zr)
	}
}
