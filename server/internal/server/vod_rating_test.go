package server

import (
	"context"
	"fmt"
	"testing"

	"marquee/internal/api"
	"marquee/internal/settings"
	"marquee/internal/vod"
)

// Films and series on demand can be rated (0–10, half stars); the rating is kept with the
// title, so it outlives the source's entry and shows again when the title comes back. What was
// watched before the memory existed is remembered when the server starts.
func TestVodRatings(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	var res api.AuthResult
	h.do("POST", "/setup", map[string]any{"serverName": "T", "username": "admin", "password": "correct horse", "device": device}, &res)
	h.token = res.Token
	h.store.Update(ctx, func(s *settings.Settings) error {
		s.Integrations.VODEnabled = true
		s.Integrations.VODSources = []settings.VODSource{{ID: "p", Name: "Films", URL: "http://127.0.0.1:1/films.m3u", Enabled: true}}
		return nil
	})
	h.handlers.VOD = &vod.Service{DB: h.db, Settings: h.store, Streams: &vod.Streams{}}
	film := func(key string) int64 {
		r, err := h.db.Exec(`INSERT INTO vod_movies(source_id, source_key, title, stream_url, added_at, tmdb_id, match_state, overview)
			VALUES ('p', ?, 'Ronin', 'http://127.0.0.1:1/r.m3u8', '2026-01-01T00:00:00Z', 100, 'matched', 'A heist.')`, key)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := r.LastInsertId()
		return id
	}
	ronin := film("f1")
	r, _ := h.db.Exec(`INSERT INTO vod_series(source_id, source_key, title, added_at, year, match_state) VALUES ('p', 's1', 'Unmatched Show', '2026-01-01T00:00:00Z', 2020, 'none')`)
	show, _ := r.LastInsertId()

	rate := func(path string, v any) int {
		return h.do("PUT", path, map[string]any{"rating": v}, nil)
	}
	if code := rate(fmt.Sprintf("/livetv/vod/movies/%d/rating", ronin), 8.5); code != 204 {
		t.Fatalf("rate film: %d", code)
	}
	if code := rate(fmt.Sprintf("/livetv/vod/movies/%d/rating", ronin), 12); code != 400 {
		t.Errorf("rating 12: %d", code)
	}
	if code := rate("/livetv/vod/movies/99999/rating", 5); code != 404 {
		t.Errorf("no such film: %d", code)
	}
	if code := rate(fmt.Sprintf("/livetv/vod/series/%d/rating", show), 3); code != 204 {
		t.Fatalf("rate series: %d", code)
	}
	var m api.VodMovieDetail
	h.do("GET", fmt.Sprintf("/livetv/vod/movies/%d", ronin), nil, &m)
	if m.UserRating == nil || *m.UserRating != 8.5 {
		t.Errorf("film's rating: %v", m.UserRating)
	}
	var sd api.VodSeriesDetail
	h.do("GET", fmt.Sprintf("/livetv/vod/series/%d", show), nil, &sd)
	if sd.UserRating == nil || *sd.UserRating != 3 {
		t.Errorf("series' rating (by title and year): %v", sd.UserRating)
	}

	// The film leaves the source and comes back under another key: its rating is there again.
	h.db.Exec(`DELETE FROM vod_movies WHERE id=?`, ronin)
	back := film("f2")
	var again api.VodMovieDetail
	h.do("GET", fmt.Sprintf("/livetv/vod/movies/%d", back), nil, &again)
	if again.UserRating == nil || *again.UserRating != 8.5 {
		t.Errorf("after coming back: %v", again.UserRating)
	}
	// Cleared.
	if code := rate(fmt.Sprintf("/livetv/vod/movies/%d/rating", back), nil); code != 204 {
		t.Fatalf("clear: %d", code)
	}
	var cleared api.VodMovieDetail
	h.do("GET", fmt.Sprintf("/livetv/vod/movies/%d", back), nil, &cleared)
	if cleared.UserRating != nil {
		t.Errorf("cleared, but %v", *cleared.UserRating)
	}

	// Watched before the memory existed: remembered at start.
	var uid int64
	h.db.QueryRow("SELECT id FROM users WHERE username='admin'").Scan(&uid)
	r, _ = h.db.Exec(`INSERT INTO vod_movies(source_id, source_key, title, stream_url, added_at, tmdb_id, match_state, tmdb_recs)
		VALUES ('p', 'f3', 'Heat', 'x', '2026-01-01T00:00:00Z', 949, 'matched', '[100]')`)
	heat, _ := r.LastInsertId()
	h.db.Exec(`INSERT INTO vod_progress(user_id, movie_id, watched, updated_at) VALUES (?, ?, 1, '2026-01-01T00:00:00Z')`, uid, heat)
	h.handlers.VOD.BackfillMemory(ctx)
	mems, err := vod.Memories(ctx, h.db, uid)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range mems {
		if m.TMDBID == 949 && m.Finished && len(m.Recs) == 1 && m.Recs[0] == 100 {
			found = true
		}
	}
	if !found {
		t.Errorf("Heat wasn't remembered: %+v", mems)
	}
}
