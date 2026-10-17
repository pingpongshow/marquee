package lyrics

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"marquee/internal/db"
)

// FetchAll looks up every track not yet tried online, stores what LRCLIB has, skips tracks
// LRCLIB already had nothing for, and asks for a themes index when it found lyrics.
func TestFetchAll(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	d, err := db.Open(ctx, filepath.Join(dir, "t.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	d.Exec(`INSERT INTO libraries (id, name, type) VALUES (1, 'Music', 'music')`)
	d.Exec(`INSERT INTO items (id, library_id, type, title, sort_title) VALUES (100, 1, 'artist', 'Band', 'Band')`)
	for i, title := range []string{"Ocean Song", "Silent Track", "Already Tried"} {
		id := int64(i + 1)
		d.Exec(`INSERT INTO items (id, library_id, type, title, sort_title, grandparent_id, duration_ms) VALUES (?, 1, 'track', ?, ?, 100, 200000)`, id, title, title)
		d.Exec(`INSERT INTO media_versions (id, item_id) VALUES (?, ?)`, id, id)
		d.Exec(`INSERT INTO media_files (version_id, library_id, path, size, mtime) VALUES (?, 1, ?, 1, 1)`, id, filepath.Join(dir, title+".flac"))
	}
	// Track 3: LRCLIB already had nothing (not asked again until a month passes).
	d.Exec(`INSERT INTO lyrics (item_id, text, source) VALUES (3, '', 'lrclib')`)
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Query().Get("track_name") == "Ocean Song" {
			w.Write([]byte(`{"plainLyrics":"waves on the shore\nsalt in the air"}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	fetched := 0
	s := &Service{DB: d, Online: func() bool { return true }, BaseURL: srv.URL, FetchPause: 1, Fetched: func() { fetched++ }}
	msg, err := s.FetchAll(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Errorf("asked LRCLIB %d times, want 2 (not the already-tried track)", calls.Load())
	}
	var text, source string
	d.QueryRow(`SELECT text, source FROM lyrics WHERE item_id = 1`).Scan(&text, &source)
	if source != "lrclib" || text == "" {
		t.Errorf("track 1: %q from %q", text, source)
	}
	d.QueryRow(`SELECT source FROM lyrics WHERE item_id = 2`).Scan(&source)
	if source != "lrclib" {
		t.Errorf("track 2 should be marked as tried online, got %q", source)
	}
	if fetched != 1 {
		t.Errorf("themes index asked %d times", fetched)
	}
	if done, total := s.FetchProgress(); done != 2 || total != 2 {
		t.Errorf("progress %d/%d", done, total)
	}
	t.Log(msg)
	// A second run has nothing left to ask.
	calls.Store(0)
	s.FetchAll(ctx)
	if calls.Load() != 0 {
		t.Errorf("second run asked LRCLIB %d times", calls.Load())
	}
	// Online lookups off: nothing happens.
	off := &Service{DB: d, Online: func() bool { return false }}
	if msg, _ := off.FetchAll(ctx); msg == "" {
		t.Error("expected a message when online lyrics are off")
	}
}

// LRCLIB failing (down, rate limiting, a timeout) isn't "no lyrics": the track is asked
// again next time instead of being remembered as having none for a month.
func TestLRCLIBFailureNotCachedAsNone(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	d, err := db.Open(ctx, filepath.Join(dir, "t.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	d.Exec(`INSERT INTO libraries (id, name, type) VALUES (1, 'Music', 'music')`)
	d.Exec(`INSERT INTO items (id, library_id, type, title, sort_title, duration_ms) VALUES (1, 1, 'track', 'Song', 'Song', 200000)`)
	d.Exec(`INSERT INTO media_versions (id, item_id) VALUES (1, 1)`)
	d.Exec(`INSERT INTO media_files (version_id, library_id, path, size, mtime) VALUES (1, 1, ?, 1, 1)`, filepath.Join(dir, "Song.flac"))
	var down atomic.Bool
	down.Store(true)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if down.Load() {
			http.Error(w, "busy", http.StatusServiceUnavailable)
			return
		}
		w.Write([]byte(`{"plainLyrics":"la la la"}`))
	}))
	defer srv.Close()
	s := &Service{DB: d, Online: func() bool { return true }, BaseURL: srv.URL}
	if _, err := s.Get(ctx, 1); err != ErrNone {
		t.Fatalf("while LRCLIB is down: %v", err)
	}
	down.Store(false)
	l, err := s.Get(ctx, 1)
	if err != nil || len(l.Lines) != 1 || l.Source != "lrclib" {
		t.Fatalf("after LRCLIB recovered: %+v %v", l, err)
	}
}
