package items

import (
	"context"
	"testing"
)

// Removing from Continue Watching hides a movie, and an episode's whole show, until the
// title is watched again; the resume position stays.
func TestHideFromContinue(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	s := NewStore(d)
	uid := mustExec(t, d, `INSERT INTO users(username, display_name) VALUES ('ann', 'Ann')`)
	lib := mustExec(t, d, `INSERT INTO libraries(name, type) VALUES ('Mixed', 'movies')`)
	item := func(typ, title string, parent, grand any, idx int) int64 {
		return mustExec(t, d, `INSERT INTO items(library_id, type, title, sort_title, parent_id, grandparent_id, idx) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			lib, typ, title, title, parent, grand, idx)
	}
	movie := item("movie", "Heat", nil, nil, 0)
	show := item("show", "Lost", nil, nil, 0)
	season := item("season", "Season 1", show, nil, 1)
	ep := item("episode", "Pilot", season, show, 1)
	progress := func(id int64, when string) {
		mustExec(t, d, `INSERT INTO user_item_state(user_id, item_id, view_offset_ms, last_viewed_at) VALUES (?, ?, 60000, ?)
			ON CONFLICT(user_id, item_id) DO UPDATE SET last_viewed_at = excluded.last_viewed_at`, uid, id, when)
	}
	progress(movie, "2026-01-01T10:00:00.000Z")
	progress(ep, "2026-01-02T10:00:00.000Z")
	acc := Access{UserID: uid}
	titles := func() map[string]bool {
		got, err := s.ContinueWatching(ctx, acc, []int64{lib}, 20)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for _, it := range got {
			out[it.Title] = true
		}
		return out
	}
	if got := titles(); !got["Heat"] || !got["Pilot"] {
		t.Fatalf("before: %v", got)
	}
	if err := s.HideFromContinue(ctx, uid, movie); err != nil {
		t.Fatal(err)
	}
	if err := s.HideFromContinue(ctx, uid, ep); err != nil {
		t.Fatal(err)
	}
	if got := titles(); got["Heat"] || got["Pilot"] {
		t.Fatalf("hidden still listed: %v", got)
	}
	var offset int64
	d.QueryRow(`SELECT view_offset_ms FROM user_item_state WHERE user_id = ? AND item_id = ?`, uid, movie).Scan(&offset)
	if offset != 60000 {
		t.Fatalf("resume position lost: %d", offset)
	}
	// Watched again: back on the list.
	progress(movie, "2099-01-01T10:00:00.000Z")
	if got := titles(); !got["Heat"] || got["Pilot"] {
		t.Fatalf("after watching again: %v", got)
	}
	if err := s.HideFromContinue(ctx, uid, season); err != ErrNotFound {
		t.Fatalf("a season can't be removed: %v", err)
	}
}
