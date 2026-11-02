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
	j, err := s.Create(ctx, user, 0, item, 0, "medium")
	if err != nil {
		t.Fatal(err)
	}
	if !s.next(ctx, false) {
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
	j, err := s.Create(ctx, user, 0, item, 0, "medium")
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
	s.next(ctx, false)
	if entries, _ := os.ReadDir(s.Dir); len(entries) != 0 {
		t.Errorf("orphaned file left: %v", entries[0].Name())
	}
}

// Songs convert to M4A on a worker of their own, so one doesn't wait for a film's conversion,
// and the film's worker leaves them alone.
func TestConvertSong(t *testing.T) {
	s, user, item := newService(t)
	ctx := context.Background()
	exec := func(q string, args ...any) int64 {
		r, err := s.DB.Exec(q, args...)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := r.LastInsertId()
		return id
	}
	var lib int64
	if err := s.DB.QueryRow(`SELECT library_id FROM items WHERE id = ?`, item).Scan(&lib); err != nil {
		t.Fatal(err)
	}
	track := exec(`INSERT INTO items (library_id, type, title, sort_title) VALUES (?, 'track', 'Song', 'Song')`, lib)
	v := exec(`INSERT INTO media_versions (item_id) VALUES (?)`, track)
	f := exec(`INSERT INTO media_files (version_id, library_id, path, size, mtime, duration_ms, available) VALUES (?, ?, ?, 1, 1, 60000, 1)`,
		v, lib, filepath.Join(s.Dir, "song.flac"))
	exec(`INSERT INTO streams (file_id, stream_index, kind, codec, channels) VALUES (?, 0, 'audio', 'flac', 2)`, f)

	j, err := s.Create(ctx, user, 0, track, 0, "medium")
	if err != nil {
		t.Fatal(err)
	}
	if s.next(ctx, false) {
		t.Fatal("the film's worker took a song")
	}
	if !s.next(ctx, true) {
		t.Fatal("the songs' worker found nothing")
	}
	j, err = s.Get(ctx, user, j.ID)
	if err != nil || j.Status != "ready" || filepath.Ext(j.Path) != ".m4a" {
		t.Fatalf("job: %+v %v", j, err)
	}
	if _, err := os.Stat(j.Path); err != nil {
		t.Fatal(err)
	}
	film, err := s.Create(ctx, user, 0, item, 0, "medium")
	if err != nil {
		t.Fatal(err)
	}
	if s.next(ctx, true) {
		t.Fatal("the songs' worker took a film")
	}
	if !s.next(ctx, false) {
		t.Fatal("the film's worker found nothing")
	}
	if film, err = s.Get(ctx, user, film.ID); err != nil || filepath.Ext(film.Path) != ".mp4" {
		t.Fatalf("film: %+v %v", film, err)
	}
}

// Each device gets its own conversion: the first to finish deletes it, and another device
// asking for the same item mustn't be left with nothing to fetch.
func TestOneConversionPerDevice(t *testing.T) {
	s, user, item := newService(t)
	ctx := context.Background()
	phone, err := s.Create(ctx, user, 1, item, 0, "medium")
	if err != nil {
		t.Fatal(err)
	}
	again, err := s.Create(ctx, user, 1, item, 0, "medium")
	if err != nil || again.ID != phone.ID {
		t.Fatalf("the same device should reuse its job: %v %v", again.ID, err)
	}
	tablet, err := s.Create(ctx, user, 2, item, 0, "medium")
	if err != nil || tablet.ID == phone.ID {
		t.Fatalf("another device should get its own job: %v %v", tablet.ID, err)
	}
	if err := s.Delete(ctx, user, phone.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, user, tablet.ID); err != nil {
		t.Fatalf("deleting one device's job took the other's: %v", err)
	}
}
