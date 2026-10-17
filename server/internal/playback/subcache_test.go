package playback

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"marquee/internal/db"
)

// A cached subtitle conversion is made again when its source file changed since: stream ids
// are reused (a downloaded subtitle removed and another added), so the cache key alone
// would serve the old track.
func TestSubtitleCacheFollowsSource(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Open(context.Background(), filepath.Join(dir, "t.db"), filepath.Join(dir, "b"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	// A stand-in FFmpeg that copies the input (after -i) to the output (the last argument).
	ff := filepath.Join(dir, "ffmpeg")
	os.WriteFile(ff, []byte("#!/bin/sh\nin=\nprev=\nfor a; do [ \"$prev\" = \"-i\" ] && in=$a; prev=$a; done\ncp \"$in\" \"$a\"\n"), 0o755)
	exec := func(q string, args ...any) int64 {
		r, err := database.Exec(q, args...)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := r.LastInsertId()
		return id
	}
	lib := exec(`INSERT INTO libraries (name, type) VALUES ('Movies', 'movies')`)
	item := exec(`INSERT INTO items (library_id, type, title, sort_title) VALUES (?, 'movie', 'Film', 'Film')`, lib)
	v := exec(`INSERT INTO media_versions (item_id) VALUES (?)`, item)
	f := exec(`INSERT INTO media_files (version_id, library_id, path, size, mtime) VALUES (?, ?, ?, 1, 1)`, v, lib, filepath.Join(dir, "film.mkv"))
	srt := filepath.Join(dir, "sub.srt")
	os.WriteFile(srt, []byte("WEBVTT\n\nfirst"), 0o644)
	sid := exec(`INSERT INTO streams (file_id, kind, codec, external_path) VALUES (?, 'subtitle', 'subrip', ?)`, f, srt)

	m := &Manager{DB: database, FFmpeg: ff}
	m.sessions = map[string]*Session{"s1": {ID: "s1", FileID: f, Path: filepath.Join(dir, "film.mkv"), lastActive: time.Now()}}
	h := m.StreamHandler(filepath.Join(dir, "cache"))
	path := StreamPrefix + "s1/subtitles/" + strconv.FormatInt(sid, 10) + ".vtt"
	if body := get(t, h, path).Body.String(); !strings.Contains(body, "first") {
		t.Fatalf("first: %q", body)
	}
	// The track is replaced by another one under the same stream id.
	os.WriteFile(srt, []byte("WEBVTT\n\nsecond"), 0o644)
	later := time.Now().Add(time.Minute)
	os.Chtimes(srt, later, later)
	if body := get(t, h, path).Body.String(); !strings.Contains(body, "second") {
		t.Fatalf("stale subtitle served: %q", body)
	}
}
