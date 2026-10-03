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

// A season (or whole show) downloads as one zip of its episodes' original files.
func TestSeasonZipDownload(t *testing.T) {
	h := newHarness(t)
	var res api.AuthResult
	h.do("POST", "/setup", map[string]any{"serverName": "T", "username": "admin", "password": "correct horse", "device": device}, &res)
	h.token = res.Token
	var lib api.Library
	h.do("POST", "/libraries", map[string]any{"name": "Shows", "type": "shows", "paths": []string{filepath.Join(h.media, "video/Shows")}}, &lib)
	exec := func(q string, args ...any) int64 {
		r, err := h.db.Exec(q, args...)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := r.LastInsertId()
		return id
	}
	show := exec(`INSERT INTO items(library_id, type, title, sort_title) VALUES (?, 'show', 'Severance', 'Severance')`, lib.Id)
	season := exec(`INSERT INTO items(library_id, type, title, sort_title, parent_id, idx) VALUES (?, 'season', 'Season 1', 'Season 1', ?, 1)`, lib.Id, show)
	for i := 1; i <= 2; i++ {
		ep := exec(`INSERT INTO items(library_id, type, title, sort_title, parent_id, grandparent_id, idx) VALUES (?, 'episode', ?, ?, ?, ?, ?)`,
			lib.Id, fmt.Sprintf("Ep %d", i), fmt.Sprintf("Ep %d", i), season, show, i)
		p := filepath.Join(h.media, "video/Shows/Severance/Season 1", fmt.Sprintf("Severance S01E0%d.mkv", i))
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte(fmt.Sprintf("episode %d", i)), 0o644)
		v := exec(`INSERT INTO media_versions(item_id) VALUES (?)`, ep)
		exec(`INSERT INTO media_files(version_id, library_id, path, size, mtime) VALUES (?, ?, ?, 9, 1)`, v, lib.Id, p)
	}
	resp, body := h.raw("GET", fmt.Sprintf("/api/v1/download/zip/%d", season), nil)
	if resp.StatusCode != 200 || resp.Header.Get("Content-Disposition") != `attachment; filename="Severance - Season 1.zip"` {
		t.Fatalf("season: %d %v", resp.StatusCode, resp.Header)
	}
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil || len(zr.File) != 2 || zr.File[0].Name != "Severance S01E01.mkv" {
		t.Fatalf("zip: %v %v", err, zr)
	}
	// The whole show: a folder per season.
	_, body = h.raw("GET", fmt.Sprintf("/api/v1/download/zip/%d", show), nil)
	zr, _ = zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if zr == nil || len(zr.File) != 2 || zr.File[1].Name != "Season 1/Severance S01E02.mkv" {
		t.Fatalf("show zip: %+v", zr)
	}
	// Movies and episodes download on their own, not as a zip.
	if resp, _ := h.raw("GET", fmt.Sprintf("/api/v1/download/zip/%d", season+1), nil); resp.StatusCode != 400 {
		t.Errorf("episode as zip: %d", resp.StatusCode)
	}
}
