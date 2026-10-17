package vodsuggest

import (
	"context"
	"strings"
	"testing"
	"time"
)

// The recommendations page: top picks, a Because you love row per liked title, favourite
// genres, all from the person's own taste: what someone else watched never shows up. A
// title shows in one row only; rows too short to fill aren't shown; films only when asked.
func TestRows(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	heat := e.item("movie", "Heat", 949, []string{"Crime"}, 100, 101, 102, 103, 104, 105, 106, 107)
	if err := e.store.SetFavorite(ctx, e.uid, heat, true); err != nil {
		t.Fatal(err)
	}
	for i, name := range []string{"Ronin", "Collateral", "Thief", "Manhunter", "Miami Vice", "Public Enemies", "Blackhat", "Ali"} {
		e.vodMovie(name, 100+i, `["Crime"]`, "R")
	}
	// Bob played Heat too and watched family films on demand: none of that reaches Ann.
	bob := e.exec(`INSERT INTO users(username, display_name) VALUES ('bob', 'Bob')`)
	e.exec(`INSERT INTO user_item_state(user_id, item_id, play_count, last_viewed_at) VALUES (?, ?, 1, ?)`, bob, heat, now)
	for i, name := range []string{"Paddington", "Amelie", "Up", "Wall-E", "Coco"} {
		id := e.vodMovie(name, 500+i, `["Family"]`, "PG")
		e.exec(`INSERT INTO vod_progress(user_id, movie_id, watched, updated_at) VALUES (?, ?, 1, ?)`, bob, id, now)
	}

	rows, err := e.svc.Rows(ctx, e.user(), Movie, 4) // small rows, so there are several
	if err != nil {
		t.Fatal(err)
	}
	got := map[string][]string{}
	seen := map[string]string{}
	for _, r := range rows {
		names := titles(t, e, r.Items)
		got[r.ID] = names
		for _, n := range names {
			if prev, dup := seen[n]; dup {
				t.Errorf("%s in %s and %s", n, prev, r.ID)
			}
			seen[n] = r.ID
		}
		if strings.HasPrefix(r.ID, "because:") && r.Title != "Because you love Heat" {
			t.Errorf("seed row title %q", r.Title)
		}
	}
	if len(got["top"]) != 4 || len(got["because:0"]) != 4 {
		t.Errorf("rows: %v", got)
	}
	for _, n := range []string{"Paddington", "Amelie", "Up", "Wall-E", "Coco"} {
		// Only Popular on VOD (TMDB's ratings, the same for everyone) may show them.
		if r, ok := seen[n]; ok && r != "popular" {
			t.Errorf("%s (only someone else watched it) in %s", n, r)
		}
	}
	// Series only: no films.
	rows, _ = e.svc.Rows(ctx, e.user(), Series, 30)
	for _, r := range rows {
		for _, it := range r.Items {
			if it.Kind != Series {
				t.Errorf("film in series rows: %+v", r)
			}
		}
	}
}
