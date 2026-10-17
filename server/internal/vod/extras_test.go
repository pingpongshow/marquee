package vod

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"marquee/internal/db"
	"marquee/internal/metadata/tmdb"
)

// extrasMatcher answers the backfill's one-request lookups, noting when each came.
type extrasMatcher struct {
	mu    sync.Mutex
	asked []int
	at    []time.Time
}

func (f *extrasMatcher) MatchMovie(context.Context, string, int) (*tmdb.Movie, error) {
	return nil, nil
}
func (f *extrasMatcher) MovieByID(context.Context, int) (*tmdb.Movie, error) {
	return nil, tmdb.ErrNotFound
}

func (f *extrasMatcher) note(id int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, id)
	f.at = append(f.at, time.Now())
}

func (f *extrasMatcher) MovieExtras(_ context.Context, id int) (*tmdb.Movie, error) {
	f.note(id)
	if id == 404 {
		return nil, tmdb.ErrNotFound
	}
	m := &tmdb.Movie{ID: id, VoteCount: 1000 + id}
	m.Recommendations.Results = []tmdb.SearchResult{{ID: id + 1}}
	m.Keywords.Keywords = []tmdb.Keyword{{Name: "Heist"}}
	return m, nil
}

func (f *extrasMatcher) TVExtras(_ context.Context, id int) (*tmdb.TVShow, error) {
	f.note(id)
	t := &tmdb.TVShow{ID: id, VoteCount: 50}
	t.Recommendations.Results = []tmdb.SearchResult{{ID: 9}}
	return t, nil
}

// The backfill fills in matched titles without data (best rated first, a TMDB 404 getting
// empty data so it isn't asked again), skips titles that have it and unmatched ones, keeps
// its pace, stops at a limit or when its context ends, and resumes where it stopped.
func TestBackfillExtras(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	d, err := db.Open(ctx, filepath.Join(dir, "t.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	f := &extrasMatcher{}
	s := &Service{DB: d, Matcher: f}
	film := func(key string, tmdbID int, rating float64, state string, recs any) {
		if _, err := d.Exec(`INSERT INTO vod_movies(source_id, source_key, title, stream_url, added_at, tmdb_id, match_state, rating, tmdb_recs)
			VALUES ('s', ?, ?, 'x', '2026-01-01T00:00:00Z', ?, ?, ?, ?)`, key, key, tmdbID, state, rating, recs); err != nil {
			t.Fatal(err)
		}
	}
	film("a", 100, 8, "matched", nil)
	film("b", 200, 9, "matched", nil)
	film("has", 300, 9.5, "matched", "[1]")
	film("pending", 400, 9.9, "pending", nil)
	film("gone", 404, 5, "matched", nil)
	film("c", 500, 7, "matched", nil)
	d.Exec(`INSERT INTO vod_series(source_id, source_key, title, added_at, tmdb_id, match_state, rating) VALUES ('s', 's', 'S', '2026-01-01T00:00:00Z', 700, 'matched', 7.5)`)

	if n, _ := s.MissingExtras(ctx); n != 5 {
		t.Fatalf("missing: %d", n)
	}
	// A limit of two: the best rated two.
	n, err := s.BackfillExtras(ctx, 2, 30*time.Millisecond)
	if err != nil || n != 2 || len(f.asked) != 2 || f.asked[0] != 200 || f.asked[1] != 100 {
		t.Fatalf("first batch: %d %v %v", n, err, f.asked)
	}
	if gap := f.at[1].Sub(f.at[0]); gap < 25*time.Millisecond {
		t.Errorf("not paced: %v apart", gap)
	}
	var recs, keywords string
	var votes int
	d.QueryRow(`SELECT tmdb_recs, keywords, votes FROM vod_movies WHERE source_key='b'`).Scan(&recs, &keywords, &votes)
	if recs != "[201]" || keywords != `["heist"]` || votes != 1200 {
		t.Errorf("stored: %s %s %d", recs, keywords, votes)
	}
	// The rest, carrying on where it stopped; titles with data and unmatched ones aren't asked for.
	n, err = s.BackfillExtras(ctx, 0, time.Millisecond)
	if err != nil || n != 3 || len(f.asked) != 5 {
		t.Fatalf("second batch: %d %v %v", n, err, f.asked)
	}
	for _, id := range f.asked {
		if id == 300 || id == 400 {
			t.Errorf("asked for %d", id)
		}
	}
	d.QueryRow(`SELECT tmdb_recs FROM vod_movies WHERE source_key='gone'`).Scan(&recs)
	if recs != "[]" {
		t.Errorf("a title TMDB doesn't have: %q", recs)
	}
	d.QueryRow(`SELECT tmdb_recs, votes FROM vod_series`).Scan(&recs, &votes)
	if recs != "[9]" || votes != 50 {
		t.Errorf("series: %s %d", recs, votes)
	}
	if n, _ := s.MissingExtras(ctx); n != 0 {
		t.Errorf("still missing %d", n)
	}

	// A context that ends (the window closing) stops it without an error.
	film("d", 600, 6, "matched", nil)
	film("e", 601, 6, "matched", nil)
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if n, err := s.BackfillExtras(cctx, 0, time.Millisecond); err != nil || n != 0 {
		t.Errorf("cancelled: %d %v", n, err)
	}
}
