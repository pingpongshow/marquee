package downloads

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"marquee/internal/db"
	"marquee/internal/playback"
	"marquee/internal/settings"
)

// fakeFFmpeg writes its last argument (the output file), as a conversion would.
func fakeFFmpeg(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "ffmpeg")
	if err := os.WriteFile(p, []byte("#!/bin/sh\nfor last; do :; done\necho converted > \"$last\"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func newService(t *testing.T) (*Service, int64, int64) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	database, err := db.Open(ctx, filepath.Join(dir, "t.db"), filepath.Join(dir, "b"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	store, err := settings.Open(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	exec := func(q string, args ...any) int64 {
		r, err := database.Exec(q, args...)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := r.LastInsertId()
		return id
	}
	user := exec(`INSERT INTO users (username, display_name) VALUES ('u', 'U')`)
	lib := exec(`INSERT INTO libraries (name, type) VALUES ('Movies', 'movies')`)
	item := exec(`INSERT INTO items (library_id, type, title, sort_title) VALUES (?, 'movie', 'Film', 'Film')`, lib)
	v := exec(`INSERT INTO media_versions (item_id) VALUES (?)`, item)
	f := exec(`INSERT INTO media_files (version_id, library_id, path, size, mtime, duration_ms, available) VALUES (?, ?, ?, 1, 1, 60000, 1)`,
		v, lib, filepath.Join(dir, "film.mkv"))
	exec(`INSERT INTO streams (file_id, stream_index, kind, codec, width, height) VALUES (?, 0, 'video', 'h264', 1920, 1080)`, f)
	s := &Service{DB: database, Dir: filepath.Join(dir, "downloads"),
		Playback: &playback.Manager{DB: database, Settings: store, FFmpeg: fakeFFmpeg(t)}}
	s.Request = func(ctx context.Context, userID, itemID, fileID int64) (playback.Request, error) {
		return playback.Request{UserID: userID, ItemID: itemID, FileID: fileID}, nil
	}
	return s, user, item
}

func TestConvert(t *testing.T) {
	s, user, item := newService(t)
	ctx := context.Background()
	j, err := s.Create(ctx, user, item, 0, "medium")
	if err != nil {
		t.Fatal(err)
	}
	if !s.next(ctx) {
		t.Fatal("no job run")
	}
	j, err = s.Get(ctx, user, j.ID)
	if err != nil || j.Status != "ready" || j.Path == "" {
		t.Fatalf("job: %+v %v", j, err)
	}
	if _, err := os.Stat(j.Path); err != nil {
		t.Fatal(err)
	}
}

// A download deleted while its conversion was being prepared mustn't leave a converted file
// on the server that no job points to (nothing would ever remove it).
func TestDeletedWhilePreparingLeavesNoFile(t *testing.T) {
	s, user, item := newService(t)
	ctx := context.Background()
	j, err := s.Create(ctx, user, item, 0, "medium")
	if err != nil {
		t.Fatal(err)
	}
	req := s.Request
	s.Request = func(ctx context.Context, userID, itemID, fileID int64) (playback.Request, error) {
		if err := s.Delete(ctx, user, j.ID); err != nil { // the app deletes it now
			t.Fatal(err)
		}
		return req(ctx, userID, itemID, fileID)
	}
	s.next(ctx)
	if entries, _ := os.ReadDir(s.Dir); len(entries) != 0 {
		t.Errorf("orphaned file left: %v", entries[0].Name())
	}
}
