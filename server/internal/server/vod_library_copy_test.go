package server

import (
	"context"
	"fmt"
	"testing"

	"marquee/internal/api"
	"marquee/internal/settings"
	"marquee/internal/vod"
	"marquee/internal/vodsave"
)

// A film or show on demand that the library has points at the library's copy (the best
// one, in a library the caller can see), and a show's episodes at theirs, so the apps play
// the library's first. Someone who can't see that library gets the stream on demand only.
func TestVodPagesPointAtTheLibraryCopy(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	var res api.AuthResult
	h.do("POST", "/setup", map[string]any{"serverName": "T", "username": "admin", "password": "correct horse", "device": device}, &res)
	admin := res.Token
	h.token = admin
	h.store.Update(ctx, func(s *settings.Settings) error {
		s.Integrations.VODEnabled = true
		s.Integrations.VODSources = []settings.VODSource{{ID: "x", Name: "Films", URL: "http://127.0.0.1:1", Kind: settings.VODKindXtream,
			Username: "u", Password: "p", Enabled: true}}
		return nil
	})
	h.handlers.VOD = &vod.Service{DB: h.db, Settings: h.store, Streams: &vod.Streams{}}
	h.handlers.VODSaves = &vodsave.Service{DB: h.db, Settings: h.store, VOD: h.handlers.VOD}

	exec := func(q string, args ...any) int64 {
		t.Helper()
		r, err := h.db.Exec(q, args...)
		if err != nil {
			t.Fatalf("%s: %v", q, err)
		}
		id, _ := r.LastInsertId()
		return id
	}
	file := func(lib, item int64, height int) {
		v := exec(`INSERT INTO media_versions(item_id) VALUES (?)`, item)
		exec(`INSERT INTO media_files(version_id, library_id, path, size, mtime, duration_ms, width, height, bitrate_kbps, available)
			VALUES (?, ?, ?, 5, 100, 1800000, ?, ?, 4000, 1)`, v, lib, fmt.Sprint("/m/", item, ".mkv"), height*16/9, height)
	}
	movies := exec(`INSERT INTO libraries(name, type) VALUES ('Movies', 'movies')`)
	other := exec(`INSERT INTO libraries(name, type) VALUES ('Other', 'movies')`)
	tv := exec(`INSERT INTO libraries(name, type) VALUES ('TV', 'shows')`)
	// Two copies of Ronin: the 1080p one is the best.
	sd := exec(`INSERT INTO items(library_id, type, title, sort_title, year) VALUES (?, 'movie', 'Ronin', 'Ronin', 1998)`, movies)
	hd := exec(`INSERT INTO items(library_id, type, title, sort_title, year) VALUES (?, 'movie', 'Ronin', 'Ronin', 1998)`, movies)
	for item, height := range map[int64]int{sd: 480, hd: 1080} {
		exec(`INSERT INTO external_ids(item_id, provider, value) VALUES (?, 'tmdb', '100')`, item)
		file(movies, item, height)
	}
	show := exec(`INSERT INTO items(library_id, type, title, sort_title, year) VALUES (?, 'show', '1899', '1899', 2022)`, tv)
	exec(`INSERT INTO external_ids(item_id, provider, value) VALUES (?, 'tmdb', '700')`, show)
	season := exec(`INSERT INTO items(library_id, type, title, sort_title, parent_id, idx) VALUES (?, 'season', 'Season 1', 's1', ?, 1)`, tv, show)
	e1 := exec(`INSERT INTO items(library_id, type, title, sort_title, parent_id, grandparent_id, idx) VALUES (?, 'episode', 'One', 'one', ?, ?, 1)`, tv, season, show)
	file(tv, e1, 1080)

	ronin := exec(`INSERT INTO vod_movies(source_id, source_key, title, stream_url, added_at, tmdb_id, match_state, overview)
		VALUES ('x', 'xtream:1', 'Ronin', 'x', '2026-01-01T00:00:00Z', 100, 'matched', 'A heist.')`)
	elsewhere := exec(`INSERT INTO vod_movies(source_id, source_key, title, stream_url, added_at, tmdb_id, match_state, overview)
		VALUES ('x', 'xtream:2', 'Elsewhere', 'x', '2026-01-01T00:00:00Z', 555, 'matched', 'Not here.')`)
	series := exec(`INSERT INTO vod_series(source_id, source_key, title, added_at, tmdb_id, match_state) VALUES ('x', 'xtream:9', '1899', '2026-01-01T00:00:00Z', 700, 'matched')`)
	v1 := exec(`INSERT INTO vod_episodes(series_id, source_key, season, episode, title, stream_url) VALUES (?, '91', 1, 1, 'One', '91.mkv')`, series)
	v2 := exec(`INSERT INTO vod_episodes(series_id, source_key, season, episode, title, stream_url) VALUES (?, '92', 1, 2, 'Two', '92.mkv')`, series)

	var film api.VodMovieDetail
	if code := h.do("GET", fmt.Sprintf("/livetv/vod/movies/%d", ronin), nil, &film); code != 200 {
		t.Fatalf("film: %d", code)
	}
	if film.LibraryItemId == nil || *film.LibraryItemId != hd {
		t.Errorf("Ronin's library copy: %v, want %d", film.LibraryItemId, hd)
	}
	var none api.VodMovieDetail
	h.do("GET", fmt.Sprintf("/livetv/vod/movies/%d", elsewhere), nil, &none)
	if none.LibraryItemId != nil {
		t.Errorf("a film the library doesn't have points at %d", *none.LibraryItemId)
	}
	var sh api.VodSeriesDetail
	if code := h.do("GET", fmt.Sprintf("/livetv/vod/series/%d", series), nil, &sh); code != 200 {
		t.Fatalf("series: %d", code)
	}
	if sh.LibraryItemId == nil || *sh.LibraryItemId != show {
		t.Errorf("1899's library copy: %v, want %d", sh.LibraryItemId, show)
	}
	got := map[int64]*int64{}
	for _, e := range sh.Episodes {
		got[e.Id] = e.LibraryItemId
	}
	if got[v1] == nil || *got[v1] != e1 || got[v2] != nil {
		t.Errorf("episodes' library copies: %v %v", got[v1], got[v2])
	}
	if sh.NextEpisode == nil || sh.NextEpisode.LibraryItemId == nil || *sh.NextEpisode.LibraryItemId != e1 {
		t.Errorf("the next episode: %+v", sh.NextEpisode)
	}

	// Someone who only sees the Other library gets the streams on demand.
	var u api.User
	if code := h.do("POST", "/users", map[string]any{"username": "kit", "password": "kit-password-1",
		"restrictions": map[string]any{"libraryIds": []int64{other}}}, &u); code != 201 {
		t.Fatalf("create kit: %d", code)
	}
	h.token = ""
	var login api.AuthResult
	h.do("POST", "/auth/login", map[string]any{"username": "kit", "password": "kit-password-1", "device": device}, &login)
	h.token = login.Token
	var kits api.VodMovieDetail
	if code := h.do("GET", fmt.Sprintf("/livetv/vod/movies/%d", ronin), nil, &kits); code != 200 || kits.LibraryItemId != nil {
		t.Errorf("kit's Ronin: %d %v", code, kits.LibraryItemId)
	}
	var sh2 api.VodSeriesDetail
	h.do("GET", fmt.Sprintf("/livetv/vod/series/%d", series), nil, &sh2)
	if sh2.LibraryItemId != nil {
		t.Errorf("kit's 1899: %v", sh2.LibraryItemId)
	}
}
