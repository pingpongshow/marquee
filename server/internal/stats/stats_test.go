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

// Music plays sum up by what they were started from: plays, listeners, how many were played
// to the end and skipped, and tracks favourited within a week of a play from there.
func TestPlaylists(t *testing.T) {
	d := seeded(t)
	ctx := context.Background()
	now := time.Now().UTC()
	at := func(d time.Duration) string { return now.Add(d).Format("2006-01-02T15:04:05.000Z") }
	for _, p := range []struct {
		user    int64
		ctx     string
		started time.Duration
		pos     int64
	}{
		{1, "mix:daily", -10 * time.Hour, 230000}, // to the end
		{2, "mix:daily", -9 * time.Hour, 10000},   // skipped
		{1, "mix:daily", -8 * time.Hour, 100000},  // neither
		{1, "radio:mood", -7 * time.Hour, 240000}, // to the end
	} {
		if _, err := d.Exec(`INSERT INTO play_history (user_id, item_id, item_title, started_at, stopped_at, position_ms, context) VALUES (?, 22, 't', ?, ?, ?, ?)`,
			p.user, at(p.started), at(p.started+4*time.Minute), p.pos, p.ctx); err != nil {
			t.Fatal(err)
		}
	}
	d.Exec(`INSERT INTO user_item_state (user_id, item_id, favorite_at) VALUES (2, 22, ?)`, at(-8*time.Hour))
	r, err := Build(ctx, d, 30, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Playlists) != 2 {
		t.Fatalf("%+v", r.Playlists)
	}
	daily, mood := r.Playlists[0], r.Playlists[1]
	if daily.Context != "mix:daily" || daily.Label != "Daily Mixes" || daily.Plays != 3 || daily.Listeners != 2 {
		t.Errorf("daily: %+v", daily)
	}
	if daily.Finished < 0.33 || daily.Finished > 0.34 || daily.Skipped < 0.33 || daily.Skipped > 0.34 || daily.Favourited != 1 {
		t.Errorf("daily rates: %+v", daily)
	}
	if mood.Plays != 1 || mood.Finished != 1 || mood.Favourited != 0 {
		t.Errorf("mood: %+v", mood)
	}
	// One person's report counts only their plays.
	r, _ = Build(ctx, d, 30, 1, 10)
	if r.Playlists[0].Plays != 2 || r.Playlists[0].Favourited != 0 {
		t.Errorf("Ann's: %+v", r.Playlists[0])
	}
}
