package stats

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"marquee/internal/db"
)

func seeded(t *testing.T) *sql.DB {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "m.db"), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	exec := func(q string, args ...any) {
		if _, err := d.ExecContext(ctx, q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	exec(`INSERT INTO users (id, username, display_name) VALUES (1, 'ann', 'Ann'), (2, 'bo', 'Bo')`)
	exec(`INSERT INTO libraries (id, name, type) VALUES (1, 'Movies', 'movies'), (2, 'Music', 'music')`)
	exec(`INSERT INTO items (id, library_id, type, title, sort_title, duration_ms) VALUES
		(10, 1, 'movie', 'Heat', 'heat', 10000000),
		(20, 2, 'artist', 'Low', 'low', 0), (21, 2, 'album', 'Hey What', 'hey what', 0)`)
	exec(`INSERT INTO items (id, library_id, type, title, sort_title, duration_ms, parent_id, grandparent_id) VALUES
		(22, 2, 'track', 'White Horses', 'white horses', 240000, 21, 20)`)
	now := time.Now().UTC()
	at := func(d time.Duration) string { return now.Add(d).Format("2006-01-02T15:04:05.000Z") }
	// Ann: Heat for 2 h (paused overnight: capped at the film's 2.8 h), two plays of a track.
	exec(`INSERT INTO play_history (user_id, item_id, item_title, started_at, stopped_at, decision, network_class) VALUES
		(1, 10, 'Heat', ?, ?, 'transcode', 'remote'),
		(1, 22, 'White Horses', ?, ?, 'direct_play', 'local'),
		(1, 22, 'White Horses', ?, ?, 'direct_play', 'local'),
		(2, 10, 'Heat', ?, ?, 'direct_play', 'local')`,
		at(-20*time.Hour), at(-5*time.Hour), at(-3*time.Hour), at(-3*time.Hour+4*time.Minute), at(-2*time.Hour), at(-2*time.Hour+4*time.Minute),
		at(-40*24*time.Hour), at(-40*24*time.Hour+time.Hour))
	return d
}

func TestBuild(t *testing.T) {
	d := seeded(t)
	r, err := Build(context.Background(), d, 30, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if r.Plays != 3 || r.Users != 1 {
		t.Fatalf("plays %d users %d", r.Plays, r.Users)
	}
	if r.Hours < 2.9 || r.Hours > 3 { // 2.78 (capped) + 2 × 4 min
		t.Fatalf("hours %.2f", r.Hours)
	}
	if len(r.Artists) != 1 || r.Artists[0].Title != "Low" || r.Artists[0].Plays != 2 {
		t.Fatalf("artists %+v", r.Artists)
	}
	if len(r.Albums) != 1 || r.Albums[0].Title != "Hey What" {
		t.Fatalf("albums %+v", r.Albums)
	}
	if r.Methods["direct_play"] != 2 || r.Methods["transcode"] != 1 || r.Remote != 1 || r.Local != 2 {
		t.Fatalf("methods %+v local %d remote %d", r.Methods, r.Local, r.Remote)
	}
	all, err := Build(context.Background(), d, 0, 2, 10)
	if err != nil || all.Plays != 1 || len(all.Movies) != 1 || all.Movies[0].Title != "Heat" {
		t.Fatalf("user 2 all-time: %+v %v", all, err)
	}
}

func TestBuildOnRealDatabase(t *testing.T) {
	path := os.Getenv("MARQUEE_STATS_DB")
	if path == "" {
		t.Skip("MARQUEE_STATS_DB not set")
	}
	d, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	r, err := Build(context.Background(), d, 0, 0, 5)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("plays %d hours %.1f users %d days %d top %+v artists %+v platforms %+v methods %v", r.Plays, r.Hours, r.Users, len(r.Days), r.Movies, r.Artists, r.Platforms, r.Methods)
}
