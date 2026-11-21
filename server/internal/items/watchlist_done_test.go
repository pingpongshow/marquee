package items

import (
	"context"
	"testing"
)

// A film leaves the watchlist once it's watched, and a show once every episode is; one put
// back afterwards stays (USER-8).
func TestWatchlistDropsWatchedTitles(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	s := NewStore(d)
	ann := mustExec(t, d, `INSERT INTO users(username, display_name) VALUES ('ann', 'Ann')`)
	lib := mustExec(t, d, `INSERT INTO libraries(name, type) VALUES ('Video', 'movies')`)
	film := mustExec(t, d, `INSERT INTO items(library_id, type, title, sort_title) VALUES (?, 'movie', 'Film', 'Film')`, lib)
	show := mustExec(t, d, `INSERT INTO items(library_id, type, title, sort_title) VALUES (?, 'show', 'Show', 'Show')`, lib)
	season := mustExec(t, d, `INSERT INTO items(library_id, type, title, sort_title, parent_id) VALUES (?, 'season', 'S1', 'S1', ?)`, lib, show)
	ep := func(n string) int64 {
		return mustExec(t, d, `INSERT INTO items(library_id, type, title, sort_title, parent_id, grandparent_id) VALUES (?, 'episode', ?, ?, ?, ?)`, lib, n, n, season, show)
	}
	e1, e2 := ep("E1"), ep("E2")
	listed := func(id int64) bool {
		t.Helper()
		var n int
		d.QueryRow(`SELECT COUNT(*) FROM user_item_state WHERE user_id = ? AND item_id = ? AND watchlisted_at IS NOT NULL`, ann, id).Scan(&n)
		return n == 1
	}
	for _, id := range []int64{film, show} {
		if err := s.SetWatchlist(ctx, ann, id, true); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.SetWatched(ctx, ann, film, true); err != nil {
		t.Fatal(err)
	}
	if listed(film) {
		t.Fatal("a watched film stayed on the watchlist")
	}
	s.SetWatched(ctx, ann, e1, true)
	if !listed(show) {
		t.Fatal("the show left the watchlist with an episode to go")
	}
	s.SetWatched(ctx, ann, e2, true)
	if listed(show) {
		t.Fatal("a finished show stayed on the watchlist")
	}
	// Put back after watching: it stays, even when watched again.
	s.SetWatchlist(ctx, ann, film, true)
	d.Exec(`UPDATE user_item_state SET play_count = play_count + 1 WHERE user_id = ? AND item_id = ?`, ann, film)
	if !listed(film) {
		t.Fatal("a film put back on the watchlist was taken off again")
	}
}
