package vod

import (
	"context"
	"path/filepath"
	"testing"

	"marquee/internal/db"
)

// Films and series go on and off the watchlist; the list is newest first, only titles the
// sources still list, and a film leaves it once watched (a series stays).
func TestWatchlist(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	d, err := db.Open(ctx, filepath.Join(dir, "t.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	s := &Service{DB: d}
	if _, err := d.Exec(`INSERT INTO users(id, username, display_name) VALUES (1, 'a', 'A'), (2, 'b', 'B')`); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"f1", "f2", "f3"} {
		if _, err := d.Exec(`INSERT INTO vod_movies(source_id, source_key, title, stream_url, added_at) VALUES ('s', ?, ?, 'x', '2026-01-01T00:00:00Z')`, key, key); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := d.Exec(`INSERT INTO vod_series(source_id, source_key, title, added_at) VALUES ('s', 'sx', 'S', '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatal(err)
	}
	add := func(series bool, id int64) {
		t.Helper()
		if err := s.SetWatchlisted(ctx, 1, series, id, true); err != nil {
			t.Fatal(err)
		}
	}
	add(false, 1)
	add(true, 1)
	add(false, 2)
	add(false, 3)
	add(false, 1) // again: keeps its place
	list := func() []WatchRef {
		t.Helper()
		l, err := s.Watchlist(ctx, 1, 10)
		if err != nil {
			t.Fatal(err)
		}
		return l
	}
	want := func(got []WatchRef, refs ...WatchRef) {
		t.Helper()
		if len(got) != len(refs) {
			t.Fatalf("got %+v, want %+v", got, refs)
		}
		for i := range refs {
			if got[i].Series != refs[i].Series || got[i].ID != refs[i].ID || got[i].AddedAt.IsZero() {
				t.Fatalf("got %+v, want %+v", got, refs)
			}
		}
	}
	want(list(), WatchRef{ID: 3}, WatchRef{ID: 2}, WatchRef{Series: true, ID: 1}, WatchRef{ID: 1})
	if on, _ := s.Watchlisted(ctx, 1, true, 1); !on {
		t.Error("the series isn't watchlisted")
	}
	if on, _ := s.Watchlisted(ctx, 2, true, 1); on {
		t.Error("someone else's watchlist has it")
	}

	// Watched: the film leaves the list (and comes back if marked unwatched).
	if err := s.SetWatched(ctx, 1, 2, true); err != nil {
		t.Fatal(err)
	}
	want(list(), WatchRef{ID: 3}, WatchRef{Series: true, ID: 1}, WatchRef{ID: 1})
	// Gone from the source: gone from the list.
	if _, err := d.Exec(`DELETE FROM vod_movies WHERE id=3`); err != nil {
		t.Fatal(err)
	}
	want(list(), WatchRef{Series: true, ID: 1}, WatchRef{ID: 1})
	// Taken off.
	if err := s.SetWatchlisted(ctx, 1, true, 1, false); err != nil {
		t.Fatal(err)
	}
	want(list(), WatchRef{ID: 1})
	if on, _ := s.Watchlisted(ctx, 1, true, 1); on {
		t.Error("the series is still watchlisted")
	}
}
