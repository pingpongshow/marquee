package items

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"marquee/internal/db"
)

func testDB(t *testing.T) *sql.DB {
	t.Helper()
	dir := t.TempDir()
	d, err := db.Open(context.Background(), filepath.Join(dir, "test.db"), filepath.Join(dir, "backups"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func mustExec(t *testing.T, d *sql.DB, q string, args ...any) int64 {
	t.Helper()
	r, err := d.Exec(q, args...)
	if err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	id, _ := r.LastInsertId()
	return id
}

// at is a local time in year y as stored in play_history.
func at(y int, m time.Month, d, h, min int) string {
	return isoUTC(time.Date(y, m, d, h, min, 0, 0, time.Local))
}

func TestRecap(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	s := NewStore(d)
	uid := mustExec(t, d, `INSERT INTO users(username, display_name) VALUES ('ann', 'Ann')`)
	other := mustExec(t, d, `INSERT INTO users(username, display_name) VALUES ('bob', 'Bob')`)
	music := mustExec(t, d, `INSERT INTO libraries(name, type) VALUES ('Music', 'music')`)
	movies := mustExec(t, d, `INSERT INTO libraries(name, type) VALUES ('Movies', 'movies')`)
	item := func(typ, title string, parent, grand any, durMS int64) int64 {
		return mustExec(t, d, `INSERT INTO items(library_id, type, title, sort_title, parent_id, grandparent_id, duration_ms) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			music, typ, title, title, parent, grand, durMS)
	}
	abba := item("artist", "ABBA", nil, nil, 0)
	arrival := item("album", "Arrival", abba, nil, 0)
	sos := item("track", "SOS", arrival, abba, 200000)
	dq := item("track", "Dancing Queen", arrival, abba, 230000)
	blur := item("artist", "Blur", nil, nil, 0)
	parklife := item("album", "Parklife", blur, nil, 0)
	girls := item("track", "Girls & Boys", parklife, blur, 290000)
	genre := mustExec(t, d, `INSERT INTO tags(kind, name) VALUES ('genre', 'Pop')`)
	mustExec(t, d, `INSERT INTO item_tags(item_id, tag_id) VALUES (?, ?)`, arrival, genre)
	britpop := mustExec(t, d, `INSERT INTO tags(kind, name) VALUES ('genre', 'Britpop')`)
	mustExec(t, d, `INSERT INTO item_tags(item_id, tag_id) VALUES (?, ?)`, blur, britpop)
	film := mustExec(t, d, `INSERT INTO items(library_id, type, title, sort_title, duration_ms) VALUES (?, 'movie', 'Film', 'Film', 7200000)`, movies)

	play := func(user, id int64, started string, pos any, source string) {
		mustExec(t, d, `INSERT INTO play_history(user_id, item_id, item_title, started_at, stopped_at, position_ms, source) VALUES (?, ?, 'x', ?, ?, ?, ?)`,
			user, id, started, started, pos, source)
	}
	// Last year: ABBA once (so ABBA isn't new this year).
	play(uid, sos, at(2025, 12, 31, 23, 0), 200000, "marquee")
	// This year.
	play(uid, sos, at(2026, 1, 1, 0, 30), 200000, "marquee")      // first track, Jan 1
	play(uid, sos, at(2026, 1, 2, 9, 0), 500000, "marquee")       // position past the end: counts the track's length
	play(uid, dq, at(2026, 1, 3, 9, 0), 120000, "marquee")        // 2 min
	play(uid, dq, at(2026, 1, 3, 10, 0), 10000, "marquee")        // skipped (under 30 s)
	play(uid, girls, at(2026, 3, 10, 21, 0), nil, "plex")         // imported: whole track
	play(uid, girls, at(2026, 3, 11, 21, 0), 290000, "marquee")   // streak of 2 in March
	play(other, girls, at(2026, 3, 12, 21, 0), 290000, "marquee") // someone else
	play(uid, girls, at(2027, 1, 1, 12, 0), 290000, "marquee")    // next year
	mustExec(t, d, `INSERT INTO play_history(user_id, item_id, item_title, started_at, stopped_at, position_ms) VALUES (?, ?, 'Film', ?, ?, 0)`,
		uid, film, at(2026, 5, 1, 20, 0), isoUTC(time.Date(2026, 5, 1, 21, 30, 0, 0, time.Local)))

	r, err := s.Recap(ctx, Access{UserID: uid}, uid, 2026)
	if err != nil {
		t.Fatal(err)
	}
	// 200 + 200 + 120 + 290 + 290 seconds = 18⅓ minutes.
	if r.Plays != 5 || r.Minutes != 18 || r.Tracks != 3 || r.Artists != 2 || r.NewArtists != 1 {
		t.Fatalf("totals: %+v", r)
	}
	// Ties on plays go to more minutes.
	if len(r.TopTracks) != 3 || r.TopTracks[0].Item.ID != girls || r.TopTracks[1].Item.ID != sos || r.TopTracks[1].Plays != 2 ||
		r.TopTracks[1].Minutes != 6 || r.TopTracks[2].Item.ID != dq {
		t.Fatalf("top tracks: %+v", r.TopTracks)
	}
	if len(r.TopArtists) != 2 || r.TopArtists[0].Item.ID != abba || r.TopArtists[0].Plays != 3 || len(r.TopAlbums) != 2 || r.TopAlbums[0].Item.ID != arrival {
		t.Fatalf("top artists/albums: %+v %+v", r.TopArtists, r.TopAlbums)
	}
	if len(r.TopGenres) != 2 || r.TopGenres[0] != (GenreCount{"Pop", 3}) || r.TopGenres[1] != (GenreCount{"Britpop", 2}) {
		t.Fatalf("genres: %+v", r.TopGenres)
	}
	if r.ByMonth[0] != 8 || r.ByMonth[2] != 9 || r.ByMonth[1] != 0 {
		t.Fatalf("by month: %v", r.ByMonth)
	}
	if r.ByHour[0] != 1 || r.ByHour[9] != 2 || r.ByHour[21] != 2 || r.ByHour[10] != 0 {
		t.Fatalf("by hour: %v", r.ByHour)
	}
	if r.LongestStreakDays != 3 || r.TopDay != "2026-03-10" && r.TopDay != "2026-03-11" {
		t.Fatalf("streak/top day: %d %s %d", r.LongestStreakDays, r.TopDay, r.TopDayMinutes)
	}
	if r.FirstTrack == nil || r.FirstTrack.ID != sos {
		t.Fatalf("first track: %+v", r.FirstTrack)
	}
	if r.VideoHours < 1.49 || r.VideoHours > 1.51 {
		t.Fatalf("video hours: %v", r.VideoHours)
	}
	// Access: a profile without the music library sees counts but no items.
	r2, err := s.Recap(ctx, Access{UserID: uid, LibraryIDs: []int64{movies}}, uid, 2026)
	if err != nil || r2.Plays != 5 || len(r2.TopTracks) != 0 || r2.FirstTrack != nil {
		t.Fatalf("restricted: %+v %v", r2, err)
	}
	// An empty year is zeros, not an error.
	empty, err := s.Recap(ctx, Access{UserID: uid}, uid, 2019)
	if err != nil || empty.Plays != 0 || empty.TopTracks == nil || len(empty.TopTracks) != 0 {
		t.Fatalf("empty: %+v %v", empty, err)
	}
	years, err := s.RecapYears(ctx, uid)
	if err != nil || len(years) != 3 || years[0] != 2027 || years[2] != 2025 {
		t.Fatalf("years: %v %v", years, err)
	}

	// The top-songs playlist is made once and refreshed after.
	ids := []int64{r.TopTracks[0].Item.ID, r.TopTracks[1].Item.ID}
	p, err := s.SaveRecapPlaylist(ctx, Access{UserID: uid}, 2026, ids)
	if err != nil || p.Title != "Your Top Songs 2026" || p.ItemCount != 2 || p.Kind != "audio" {
		t.Fatalf("playlist: %+v %v", p, err)
	}
	p2, err := s.SaveRecapPlaylist(ctx, Access{UserID: uid}, 2026, []int64{dq, sos, girls})
	if err != nil || p2.ID != p.ID || p2.ItemCount != 3 {
		t.Fatalf("refresh: %+v %v", p2, err)
	}
	entries, _, _ := s.PlaylistItems(ctx, Access{UserID: uid}, p.ID, 0, 10)
	if len(entries) != 3 || entries[0].Item.ID != dq || entries[2].Item.ID != girls {
		t.Fatalf("entries: %+v", entries)
	}
	if _, err := s.SaveRecapPlaylist(ctx, Access{UserID: uid}, 2019, nil); err != ErrNotFound {
		t.Fatalf("empty playlist: %v", err)
	}
}

func TestLongestStreak(t *testing.T) {
	for _, c := range []struct {
		days []string
		want int
	}{
		{nil, 0},
		{[]string{"2026-01-01"}, 1},
		{[]string{"2026-01-01", "2026-01-02", "2026-01-04", "2026-01-05", "2026-01-06"}, 3},
		{[]string{"2026-02-28", "2026-03-01"}, 2},
		{[]string{"2026-03-28", "2026-03-29", "2026-03-30"}, 3}, // across a DST change
	} {
		if got := longestStreak(c.days); got != c.want {
			t.Errorf("%v: %d, want %d", c.days, got, c.want)
		}
	}
}
