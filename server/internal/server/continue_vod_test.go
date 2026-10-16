package server

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"marquee/internal/settings"
	"marquee/internal/vod"
)

// Home's Continue Watching row has films and episodes on demand beside the library's
// titles in entries (newest first, with the total), while items stays library-only for
// older apps; the whole list pages; on-demand entries can be removed; a profile without
// Live TV gets only the library's.
func TestContinueWatchingWithVod(t *testing.T) {
	h := newHarness(t)
	var res struct {
		Token string `json:"token"`
	}
	h.do("POST", "/setup", map[string]any{"serverName": "T", "username": "admin", "password": "correct horse", "device": device}, &res)
	h.token = res.Token
	admin := h.token
	ctx := context.Background()
	h.store.Update(ctx, func(s *settings.Settings) error {
		s.Integrations.VODEnabled = true
		s.Integrations.VODSources = []settings.VODSource{{ID: "p", Name: "Films", URL: "http://127.0.0.1:1/films.m3u", Enabled: true}}
		return nil
	})
	h.handlers.VOD = &vod.Service{DB: h.db, Settings: h.store, Streams: &vod.Streams{}}

	exec := func(q string, args ...any) int64 {
		t.Helper()
		r, err := h.db.Exec(q, args...)
		if err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		id, _ := r.LastInsertId()
		return id
	}
	var uid int64
	h.db.QueryRow("SELECT id FROM users WHERE username='admin'").Scan(&uid)
	lib := exec(`INSERT INTO libraries(name, type) VALUES ('Movies', 'movies')`)
	exec(`INSERT INTO library_paths(library_id, path) VALUES (?, ?)`, lib, h.media+"/video/Movies")
	for i, when := range []string{"2026-01-02T10:00:00.000Z", "2026-01-04T10:00:00.000Z"} {
		item := exec(`INSERT INTO items(library_id, type, title, sort_title, duration_ms) VALUES (?, 'movie', ?, ?, 6000000)`, lib,
			fmt.Sprint("Film ", i), fmt.Sprint("Film ", i))
		exec(`INSERT INTO user_item_state(user_id, item_id, view_offset_ms, last_viewed_at) VALUES (?, ?, 60000, ?)`, uid, item, when)
	}
	film := exec(`INSERT INTO vod_movies(source_id, source_key, title, stream_url, runtime_ms, added_at) VALUES ('p', 'a', 'On Demand', 'x', 5000000, '2026-01-01T00:00:00Z')`)
	exec(`INSERT INTO vod_progress(user_id, movie_id, position_ms, updated_at) VALUES (?, ?, 120000, '2026-01-03T10:00:00Z')`, uid, film)
	series := exec(`INSERT INTO vod_series(source_id, source_key, title, poster_url, added_at) VALUES ('p', 'xtream:1', 'A Show', 'http://x/p.jpg', '2026-01-01T00:00:00Z')`)
	ep := exec(`INSERT INTO vod_episodes(series_id, source_key, season, episode, title, stream_url, runtime_ms) VALUES (?, '9', 1, 2, 'Two', '9.mkv', 1800000)`, series)
	exec(`INSERT INTO vod_episode_progress(user_id, episode_id, series_id, position_ms, updated_at) VALUES (?, ?, ?, 300000, '2026-01-01T10:00:00Z')`, uid, ep, series)

	type entry struct {
		Kind       string
		Key        string
		PositionMs int64
		DurationMs int64
		Item       *struct{ Title string }
		VodMovie   *struct{ Title string }
		VodEpisode *struct {
			Title       string
			SeriesTitle string
		}
		SeriesPosterUrl string
	}
	type hub struct {
		Id      string
		Items   []struct{ Title string }
		Entries []entry
		Total   int
	}
	keys := func(es []entry) string {
		var out []string
		for _, e := range es {
			out = append(out, e.Key)
		}
		return strings.Join(out, " ")
	}
	home := func() hub {
		t.Helper()
		var hubs []hub
		if code := h.do("GET", "/hubs/home", nil, &hubs); code != 200 {
			t.Fatalf("hubs: %d", code)
		}
		for _, x := range hubs {
			if x.Id == "continue-watching" {
				return x
			}
		}
		t.Fatalf("no continue watching: %+v", hubs)
		return hub{}
	}
	cw := home()
	if len(cw.Items) != 2 || cw.Items[0].Title != "Film 1" || cw.Items[1].Title != "Film 0" {
		t.Fatalf("items aren't the library's: %+v", cw.Items)
	}
	want := fmt.Sprintf("item:%d vodMovie:%d item:%d vodEpisode:%d", 2, film, 1, ep)
	if cw.Total != 4 || keys(cw.Entries) != want {
		t.Fatalf("entries: %d %s, want %s", cw.Total, keys(cw.Entries), want)
	}
	if e := cw.Entries[1]; e.Kind != "vodMovie" || e.VodMovie.Title != "On Demand" || e.PositionMs != 120000 || e.DurationMs != 5000000 {
		t.Errorf("film: %+v", e)
	}
	if e := cw.Entries[3]; e.Kind != "vodEpisode" || e.VodEpisode.SeriesTitle != "A Show" || e.SeriesPosterUrl == "" {
		t.Errorf("episode: %+v", e)
	}

	// Pages.
	var page struct {
		Total   int
		Entries []entry
	}
	h.do("GET", "/hubs/continue-watching?limit=3", nil, &page)
	if page.Total != 4 || len(page.Entries) != 3 {
		t.Fatalf("page 1: %+v", page)
	}
	h.do("GET", "/hubs/continue-watching?offset=3&limit=3", nil, &page)
	if page.Total != 4 || keys(page.Entries) != fmt.Sprintf("vodEpisode:%d", ep) {
		t.Fatalf("page 2: %+v", page)
	}

	// Removing.
	if code := h.do("DELETE", fmt.Sprintf("/hubs/continue-watching/vod/movie/%d", film), nil, nil); code != 204 {
		t.Fatalf("remove film: %d", code)
	}
	if code := h.do("DELETE", fmt.Sprintf("/hubs/continue-watching/vod/episode/%d", ep), nil, nil); code != 204 {
		t.Fatalf("remove episode: %d", code)
	}
	if code := h.do("DELETE", "/hubs/continue-watching/vod/movie/9999", nil, nil); code != 404 {
		t.Errorf("remove missing: %d", code)
	}
	if code := h.do("DELETE", "/hubs/continue-watching/vod/show/1", nil, nil); code != 404 && code != 400 {
		t.Errorf("remove a show: %d", code)
	}
	if cw := home(); cw.Total != 2 || keys(cw.Entries) != "item:2 item:1" {
		t.Fatalf("after removing: %+v", cw)
	}
	// The library's own removal still works.
	if code := h.do("DELETE", "/hubs/continue-watching/2", nil, nil); code != 204 {
		t.Fatalf("remove item: %d", code)
	}
	if cw := home(); cw.Total != 1 || len(cw.Items) != 1 {
		t.Fatalf("after removing an item: %+v", cw)
	}

	// A profile without Live TV gets the library's only, and can't remove on-demand ones.
	h.do("POST", "/users", map[string]any{"username": "kid", "password": "kid-password-1", "restrictions": map[string]any{"liveTv": false}}, nil)
	var kid struct {
		Token string `json:"token"`
	}
	h.token = ""
	h.do("POST", "/auth/login", map[string]any{"username": "kid", "password": "kid-password-1", "device": device}, &kid)
	h.token = kid.Token
	var kidID int64
	h.db.QueryRow("SELECT id FROM users WHERE username='kid'").Scan(&kidID)
	exec(`INSERT INTO vod_progress(user_id, movie_id, position_ms, updated_at) VALUES (?, ?, 120000, '2026-02-01T10:00:00Z')`, kidID, film)
	h.do("GET", "/hubs/continue-watching", nil, &page)
	if page.Total != 0 {
		t.Errorf("kid: %+v", page)
	}
	if code := h.do("DELETE", fmt.Sprintf("/hubs/continue-watching/vod/movie/%d", film), nil, nil); code != 403 {
		t.Errorf("kid removing: %d", code)
	}
	h.token = admin
}
