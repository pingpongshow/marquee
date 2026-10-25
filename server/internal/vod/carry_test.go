package vod

import (
	"context"
	"database/sql"
	"strings"
	"testing"
)

// Pairing: TMDB ids first and across the list, then titles (case and spacing aside, years at
// most one apart, never across two TMDB ids); one row each, the same category preferred.
func TestPairRenumbered(t *testing.T) {
	gone := []stored{
		{id: 1, key: "a", title: "The Matrix (1999)", category: "Action", tmdb: 603},
		{id: 2, key: "b", title: "Monster Family 2", category: "KIDS", tmdb: 77},
		{id: 3, key: "c", title: "Monster Family 2", category: "FAMILY", tmdb: 77},
		{id: 4, key: "d", title: "Other  Film", year: 2004},
		{id: 5, key: "e", title: "Dune", year: 1984, tmdb: 841},
		{id: 6, key: "f", title: "Gone For Good", tmdb: 9},
	}
	fresh := []listed{
		{key: "A", title: "The Matrix", tmdb: 603}, // renamed: TMDB still finds it
		{key: "C", title: "Monster Family 2", category: "FAMILY", tmdb: 77},
		{key: "B", title: "Monster Family 2", category: "KIDS", tmdb: 77},
		{key: "D", title: "other film", year: 2005},         // no TMDB id: by title, a year apart
		{key: "E", title: "Dune", year: 2021, tmdb: 438631}, // another film of the same name
		{key: "N", title: "Brand New", tmdb: 1},
	}
	got := map[string]int64{}
	for k, g := range pair(gone, fresh) {
		got[k] = g.id
	}
	want := map[string]int64{"A": 1, "B": 2, "C": 3, "D": 4}
	if len(got) != len(want) {
		t.Fatalf("pairs %v, want %v", got, want)
	}
	for k, id := range want {
		if got[k] != id {
			t.Errorf("%s took row %d, want %d", k, got[k], id)
		}
	}
}

func rowID(t *testing.T, db *sql.DB, q string, args ...any) int64 {
	t.Helper()
	var id int64
	if err := db.QueryRow(q, args...).Scan(&id); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return id
}

func count(t *testing.T, db *sql.DB, q string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(q, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
	return n
}

// A source renumbering its films: each film keeps its row (by TMDB id, else by title), so its
// progress, Not-interested mark and TMDB match stay (no new lookups), its save moves to the
// new id, and a film that really left goes.
func TestRefreshKeepsRenumberedFilms(t *testing.T) {
	s, f, _, m, uid := xtreamSetup(t)
	ctx := context.Background()
	s.Refresh(ctx, false)
	settle(t, s)
	matrix := rowID(t, s.DB, `SELECT id FROM vod_movies WHERE source_key='xtream:101'`)
	other := rowID(t, s.DB, `SELECT id FROM vod_movies WHERE source_key='xtream:102'`)
	// The Matrix is matched (TMDB 603); Other Film isn't (no TMDB id), so it can only be found by title.
	if count(t, s.DB, `SELECT COUNT(*) FROM vod_movies WHERE id=? AND match_state='matched'`, matrix) != 1 ||
		count(t, s.DB, `SELECT COUNT(*) FROM vod_movies WHERE id=? AND match_state<>'matched'`, other) != 1 {
		t.Fatal("The Matrix should be matched and Other Film not")
	}
	s.DB.Exec(`INSERT INTO vod_progress(user_id, movie_id, position_ms, watched, updated_at) VALUES (?, ?, 600000, 0, 'now')`, uid, matrix)
	s.DB.Exec(`INSERT INTO vod_progress(user_id, movie_id, position_ms, watched, updated_at) VALUES (?, ?, 0, 1, 'now')`, uid, other)
	s.DB.Exec(`INSERT INTO vod_suggestion_hidden(user_id, kind, vod_id, hidden_at) VALUES (?, 'movie', ?, 'now')`, uid, other)
	s.DB.Exec(`INSERT INTO vod_saves(source_id, kind, vod_id, source_key, file, title, target_path, quality, state, queue_pos, created_at)
		VALUES ('xc', 'movie', ?, '101', '101.mkv', 'The Matrix', '/m/The Matrix.mkv', 'original', 'queued', 1, 'now')`, matrix)
	lookups := len(m.byID) + len(m.calls)

	// Every id changes; "Third" (104) is gone for good; "Other Film" still has no TMDB id of its own.
	f.mu.Lock()
	f.streams = strings.NewReplacer(`"stream_id":"101"`, `"stream_id":"901"`, `"stream_id":102`, `"stream_id":902`,
		`{"num":4,"name":"Third","stream_id":"104","rating":"","category_id":null,"container_extension":"../x","tmdb":null}`,
		`{"num":4,"name":"Fourth","stream_id":"905","category_id":"1","container_extension":"mp4"}`).Replace(xcStreams)
	f.mu.Unlock()
	s.Refresh(ctx, false)
	settle(t, s)

	if id := rowID(t, s.DB, `SELECT id FROM vod_movies WHERE source_key='xtream:901'`); id != matrix {
		t.Errorf("The Matrix is row %d under its new id, want %d", id, matrix)
	}
	if id := rowID(t, s.DB, `SELECT id FROM vod_movies WHERE source_key='xtream:902'`); id != other {
		t.Errorf("Other Film is row %d under its new id, want %d (by title)", id, other)
	}
	var file, state string
	s.DB.QueryRow(`SELECT stream_url, match_state FROM vod_movies WHERE id=?`, matrix).Scan(&file, &state)
	if file != "901.mkv" || state != "matched" {
		t.Errorf("The Matrix: stream %q, %s", file, state)
	}
	if n := count(t, s.DB, `SELECT COUNT(*) FROM vod_progress WHERE user_id=? AND movie_id IN (?, ?)`, uid, matrix, other); n != 2 {
		t.Errorf("progress kept for %d of 2 films", n)
	}
	if count(t, s.DB, `SELECT COUNT(*) FROM vod_suggestion_hidden WHERE vod_id=?`, other) != 1 {
		t.Error("the Not-interested mark was lost")
	}
	var saveKey, saveFile string
	s.DB.QueryRow(`SELECT source_key, file FROM vod_saves WHERE vod_id=?`, matrix).Scan(&saveKey, &saveFile)
	if saveKey != "901" || saveFile != "901.mkv" {
		t.Errorf("the save points at %q, %q", saveKey, saveFile)
	}
	if count(t, s.DB, `SELECT COUNT(*) FROM vod_movies WHERE source_key IN ('xtream:104', 'xtream:101', 'xtream:102')`) != 0 {
		t.Error("old ids left behind")
	}
	if count(t, s.DB, `SELECT COUNT(*) FROM vod_movies WHERE source_key='xtream:905'`) != 1 {
		t.Error("a new film wasn't added")
	}
	// Only the new film was looked up; the kept ones stayed matched.
	if n := len(m.byID) + len(m.calls); n > lookups+1 {
		t.Errorf("%d TMDB lookups after the renumbering, want at most 1", n-lookups)
	}
}

// A source renumbering a series and its episodes: the series keeps its row and follow, its
// episodes are fetched again and keep their rows (and everyone's progress) by season and number.
func TestRefreshKeepsRenumberedSeries(t *testing.T) {
	s, f, _, _, uid := xtreamSetup(t)
	ctx := context.Background()
	s.Refresh(ctx, false)
	settle(t, s)
	kings := rowID(t, s.DB, `SELECT id FROM vod_series WHERE source_key='xtream:201'`)
	if err := s.LoadEpisodes(ctx, kings, true); err != nil {
		t.Fatal(err)
	}
	e1 := rowID(t, s.DB, `SELECT id FROM vod_episodes WHERE series_id=? AND source_key='3001'`, kings)
	e2 := rowID(t, s.DB, `SELECT id FROM vod_episodes WHERE series_id=? AND source_key='3003'`, kings)
	s.DB.Exec(`INSERT INTO vod_episode_progress(user_id, episode_id, series_id, position_ms, watched, updated_at) VALUES (?, ?, ?, 0, 1, 'now')`, uid, e1, kings)
	s.DB.Exec(`INSERT INTO vod_episode_progress(user_id, episode_id, series_id, position_ms, watched, updated_at) VALUES (?, ?, ?, 90000, 0, 'now')`, uid, e2, kings)
	s.DB.Exec(`INSERT INTO vod_follows(user_id, source_id, series_key, series_id, title, created_at) VALUES (?, 'xc', 'xtream:201', ?, 'Kings of the Hill', 'now')`, uid, kings)
	s.DB.Exec(`INSERT INTO vod_follow_episodes(user_id, source_id, series_key, episode_key, created_at) VALUES (?, 'xc', 'xtream:201', '3001', 'now')`, uid)

	f.mu.Lock()
	f.series = strings.Replace(xcSeries, `"series_id":"201"`, `"series_id":"801"`, 1)
	f.seriesInfo["801"] = strings.NewReplacer(`"id":"3001"`, `"id":"7001"`, `"id":3002`, `"id":7002`, `"id":"3003"`, `"id":"7003"`).Replace(xcInfoByMap)
	f.mu.Unlock()
	s.Refresh(ctx, false)
	settle(t, s)

	if id := rowID(t, s.DB, `SELECT id FROM vod_series WHERE source_key='xtream:801'`); id != kings {
		t.Fatalf("the series is row %d under its new id, want %d", id, kings)
	}
	var followKey string
	var followID int64
	s.DB.QueryRow(`SELECT series_key, series_id FROM vod_follows WHERE user_id=?`, uid).Scan(&followKey, &followID)
	if followKey != "xtream:801" || followID != kings {
		t.Errorf("the follow points at %q, %d", followKey, followID)
	}
	if count(t, s.DB, `SELECT COUNT(*) FROM vod_series WHERE id=? AND episodes_at IS NULL`, kings) != 1 {
		t.Error("the episodes should be fetched again")
	}
	if err := s.LoadEpisodes(ctx, kings, false); err != nil {
		t.Fatal(err)
	}
	if id := rowID(t, s.DB, `SELECT id FROM vod_episodes WHERE series_id=? AND source_key='7001'`, kings); id != e1 {
		t.Errorf("S01E01 is row %d under its new id, want %d", id, e1)
	}
	if id := rowID(t, s.DB, `SELECT id FROM vod_episodes WHERE series_id=? AND source_key='7003'`, kings); id != e2 {
		t.Errorf("S02E01 is row %d under its new id, want %d", id, e2)
	}
	var file string
	s.DB.QueryRow(`SELECT stream_url FROM vod_episodes WHERE id=?`, e1).Scan(&file)
	if file != "7001.mkv" {
		t.Errorf("S01E01 streams from %q", file)
	}
	if n := count(t, s.DB, `SELECT COUNT(*) FROM vod_episode_progress WHERE user_id=? AND episode_id IN (?, ?)`, uid, e1, e2); n != 2 {
		t.Errorf("progress kept for %d of 2 episodes", n)
	}
	if count(t, s.DB, `SELECT COUNT(*) FROM vod_follow_episodes WHERE series_key='xtream:801' AND episode_key='7001'`) != 1 {
		t.Error("the follow's episode record didn't move")
	}
	if count(t, s.DB, `SELECT COUNT(*) FROM vod_episodes WHERE series_id=?`, kings) != 3 {
		t.Error("episodes duplicated or lost")
	}
}
