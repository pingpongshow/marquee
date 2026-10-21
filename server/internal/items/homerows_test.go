package items

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestHolidaysOn(t *testing.T) {
	ids := func(day string) []string {
		d, _ := time.Parse("2006-01-02", day)
		var out []string
		for _, h := range HolidaysOn(d) {
			out = append(out, h.ID)
		}
		return out
	}
	for day, want := range map[string][]string{
		"2026-09-30": nil,
		"2026-10-01": {"halloween"},
		"2026-10-31": {"halloween", "thanksgiving"},
		"2026-10-26": {"halloween"},
		"2026-10-27": {"halloween", "thanksgiving"}, // Thanksgiving 2026 is 26 November
		"2026-11-01": {"thanksgiving"},
		"2026-11-25": {"thanksgiving", "christmas"},
		"2026-11-26": {"thanksgiving", "christmas"},
		"2026-11-27": {"christmas"},
		"2026-12-25": {"christmas"},
		"2026-12-26": nil,
	} {
		got := ids(day)
		if len(got) != len(want) {
			t.Errorf("%s: got %v, want %v", day, got, want)
			continue
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("%s: got %v, want %v", day, got, want)
			}
		}
	}
}

func TestThanksgivingDay(t *testing.T) {
	for year, want := range map[int]int{2024: 28, 2025: 27, 2026: 26, 2027: 25, 2028: 23} {
		if d := thanksgivingDay(year, time.UTC); d.Day() != want || d.Month() != time.November || d.Weekday() != time.Thursday {
			t.Errorf("%d: got %v, want 23..28 Nov Thursday %d", year, d, want)
		}
	}
}

func TestHomeRowQueries(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	s := NewStore(d)
	uid := mustExec(t, d, `INSERT INTO users(username, display_name) VALUES ('ann', 'Ann')`)
	lib := mustExec(t, d, `INSERT INTO libraries(name, type) VALUES ('Movies', 'movies')`)
	add := func(title string, rating float64, genre string) int64 {
		id := mustExec(t, d, `INSERT INTO items(library_id, type, title, sort_title, audience_rating) VALUES (?, 'movie', ?, ?, ?)`, lib, title, title, rating)
		mustExec(t, d, `INSERT OR IGNORE INTO tags(kind, name) VALUES ('genre', ?)`, genre)
		var tag int64
		d.QueryRow(`SELECT id FROM tags WHERE kind = 'genre' AND name = ?`, genre).Scan(&tag)
		mustExec(t, d, `INSERT INTO item_tags(item_id, tag_id) VALUES (?, ?)`, id, tag)
		return id
	}
	elf := add("Elf", 7, "Comedy")
	add("Shelf Life", 9, "Comedy")
	heat := add("Heat", 8.3, "Crime")
	add("Ronin", 7.2, "Crime")
	add("Halloween", 7.7, "Horror")
	kw := mustExec(t, d, `INSERT INTO tags(kind, name) VALUES ('keyword', 'christmas')`)
	mustExec(t, d, `INSERT INTO item_tags(item_id, tag_id) VALUES (?, ?)`, heat, kw) // set at Christmas
	acc := Access{UserID: uid}
	libs := []int64{lib}
	titles := func(list []Summary, err error) string {
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, it := range list {
			out = append(out, it.Title)
		}
		return strings.Join(out, ",")
	}
	// The title word counts double; "Shelf Life" isn't an elf.
	if got := titles(s.HolidayVideos(ctx, acc, christmas, libs, 10)); got != "Elf,Heat" {
		t.Errorf("christmas: %s", got)
	}
	if got := titles(s.HolidayVideos(ctx, acc, halloween_, libs, 10)); got != "Halloween" {
		t.Errorf("halloween: %s", got)
	}
	if got := titles(s.TopRated(ctx, acc, libs, 10)); got != "Shelf Life,Heat,Halloween" {
		t.Errorf("top rated: %s", got)
	}
	mustExec(t, d, `INSERT INTO user_item_state(user_id, item_id, play_count, last_viewed_at) VALUES (?, ?, 1, ?)`, uid, heat, isoUTC(time.Now()))
	mustExec(t, d, `INSERT INTO play_history(user_id, item_id, item_title, started_at) VALUES (?, ?, 'Heat', ?)`, uid, heat, isoUTC(time.Now()))
	mustExec(t, d, `INSERT INTO play_history(user_id, item_id, item_title, started_at) VALUES (?, ?, 'Elf', ?)`, uid, elf, isoUTC(time.Now().AddDate(0, 0, -40)))
	if got := titles(s.Trending(ctx, acc, libs, 10)); got != "Heat" {
		t.Errorf("trending: %s", got)
	}
	if got, err := s.TasteGenres(ctx, acc, libs, 2); err != nil || strings.Join(got, ",") != "Crime,Comedy" {
		t.Errorf("genres: %v %v", got, err)
	}
	if got := titles(s.GenreVideos(ctx, acc, "crime", libs, 10)); got != "Ronin" { // Heat is watched
		t.Errorf("crime: %s", got)
	}
	// Loved a while ago: Ronin (rated 9, four months back); Heat was just watched.
	var ronin int64
	d.QueryRow(`SELECT id FROM items WHERE title = 'Ronin'`).Scan(&ronin)
	mustExec(t, d, `INSERT INTO user_item_state(user_id, item_id, play_count, last_viewed_at, rating) VALUES (?, ?, 1, ?, 9)`, uid, ronin, isoUTC(time.Now().AddDate(0, -4, 0)))
	mustExec(t, d, `UPDATE user_item_state SET rating = 10 WHERE item_id = ?`, heat)
	if got := titles(s.WatchAgain(ctx, acc, libs, 10)); got != "Ronin" {
		t.Errorf("watch again: %s", got)
	}
	if got := titles(s.Discover(ctx, acc, libs, DiscoverQuery{MinRating: 7.5, Newest: true}, 10)); got != "Shelf Life,Halloween" {
		t.Errorf("discover: %s", got)
	}
}
