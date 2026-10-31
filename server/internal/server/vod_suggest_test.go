package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"marquee/internal/requests"
	"marquee/internal/settings"
	"marquee/internal/vod"
	"marquee/internal/vodsave"
	"marquee/internal/vodsuggest"
)

// Suggestions from on demand: Home's Watch on VOD row only for apps that ask (vod=true),
// with vodEntries and items empty, hideable like any row; the on-demand pages' Recommended
// for you and Not interested; Also on VOD on item pages; Muse only with includeVod; and
// Discover marking titles the caller can watch on demand. A profile without Live TV gets none.
func TestVodSuggestionsAPI(t *testing.T) {
	h := newHarness(t)
	var res struct {
		Token string `json:"token"`
	}
	h.do("POST", "/setup", map[string]any{"serverName": "T", "username": "admin", "password": "correct horse", "device": device}, &res)
	h.token = res.Token
	ctx := context.Background()
	seerr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/search":
			w.Write([]byte(`{"page":1,"totalPages":1,"results":[
				{"id":100,"mediaType":"movie","title":"Ronin","releaseDate":"1998-01-01"},
				{"id":700,"mediaType":"tv","name":"1899","firstAirDate":"2022-01-01","mediaInfo":{"status":4}},
				{"id":555,"mediaType":"movie","title":"Elsewhere","releaseDate":"2020-01-01"},
				{"id":101,"mediaType":"movie","title":"Thief","releaseDate":"1981-01-01","mediaInfo":{"status":5}}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(seerr.Close)
	h.store.Update(ctx, func(s *settings.Settings) error {
		s.Integrations.VODEnabled = true
		s.Integrations.VODSources = []settings.VODSource{{ID: "p", Name: "Films", URL: "http://127.0.0.1:1/films.m3u", Enabled: true}}
		s.Integrations.SeerrURL, s.Integrations.SeerrAPIKey = seerr.URL, "key"
		return nil
	})
	h.handlers.VOD = &vod.Service{DB: h.db, Settings: h.store, Streams: &vod.Streams{}}
	h.handlers.VODSaves = &vodsave.Service{DB: h.db, Settings: h.store, VOD: h.handlers.VOD}
	h.handlers.VODSuggest = &vodsuggest.Service{DB: h.db, Items: h.handlers.Items, VOD: h.handlers.VOD, InLibrary: h.handlers.VODSaves.InLibrary}
	h.handlers.Requests = &requests.Service{DB: h.db, Settings: h.store}

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
	heat := exec(`INSERT INTO items(library_id, type, title, sort_title, year) VALUES (?, 'movie', 'Heat', 'Heat', 1995)`, lib)
	exec(`INSERT INTO external_ids(item_id, provider, value) VALUES (?, 'tmdb', '949')`, heat)
	for i, tm := range []int{100, 101} {
		exec(`INSERT INTO tmdb_recommendations(item_id, tmdb_id, rank) VALUES (?, ?, ?)`, heat, tm, i+1)
	}
	exec(`INSERT INTO user_item_state(user_id, item_id, play_count, last_viewed_at, rating, favorite_at) VALUES (?, ?, 1, ?, 9, ?)`,
		uid, heat, time.Now().UTC().Format(time.RFC3339), time.Now().UTC().Format(time.RFC3339))
	film := func(title string, tmdb int) int64 {
		return exec(`INSERT INTO vod_movies(source_id, source_key, title, stream_url, added_at, tmdb_id, match_state, genres, poster_url)
			VALUES ('p', ?, ?, 'x', '2026-01-01T00:00:00Z', ?, 'matched', '["Crime"]', 'http://x/p.jpg')`, title, title, tmdb)
	}
	ronin := film("Ronin", 100)
	thief := film("Thief", 101)
	exec(`INSERT INTO vod_series(source_id, source_key, title, added_at, tmdb_id, match_state) VALUES ('p', 's1', '1899', '2026-01-01T00:00:00Z', 700, 'matched')`)

	type suggestion struct {
		Kind   string
		Key    string
		Reason string
		Movie  *struct {
			Id        int64
			Title     string
			InLibrary *bool
		}
	}
	type hub struct {
		Id         string
		Items      []struct{ Title string }
		VodEntries *[]suggestion
	}
	hubs := func(q string) map[string]hub {
		t.Helper()
		var list []hub
		if code := h.do("GET", "/hubs/home"+q, nil, &list); code != 200 {
			t.Fatalf("hubs: %d", code)
		}
		out := map[string]hub{}
		for _, x := range list {
			out[x.Id] = x
		}
		return out
	}
	// Older apps (no vod=true) don't get the row.
	if _, ok := hubs("")["vod-suggestions"]; ok {
		t.Fatal("the Watch on VOD row went to an app that didn't ask for it")
	}
	row, ok := hubs("?vod=true")["vod-suggestions"]
	if !ok || row.Items == nil || len(row.Items) != 0 || row.VodEntries == nil {
		t.Fatalf("Watch on VOD: %+v", row)
	}
	if es := *row.VodEntries; len(es) != 2 || es[0].Key != fmt.Sprintf("movie:%d", ronin) || es[0].Movie.Title != "Ronin" ||
		es[0].Reason != "Because you love Heat" || es[0].Movie.InLibrary == nil || *es[0].Movie.InLibrary {
		t.Fatalf("entries: %+v", es)
	}
	// It's in the layout, so it can be hidden like any row.
	var layout struct {
		Rows []struct {
			Id     string
			Hidden bool
		}
	}
	h.do("GET", "/me/home-layout", nil, &layout)
	found := false
	var rows []map[string]any
	for _, r := range layout.Rows {
		found = found || r.Id == "vod-suggestions"
		rows = append(rows, map[string]any{"id": r.Id, "hidden": r.Id == "vod-suggestions"})
	}
	if !found {
		t.Fatalf("not in the layout: %+v", layout)
	}
	if code := h.do("PUT", "/me/home-layout", map[string]any{"rows": rows}, nil); code != 200 {
		t.Fatalf("layout: %d", code)
	}
	if _, ok := hubs("?vod=true")["vod-suggestions"]; ok {
		t.Error("a hidden row was sent")
	}

	// The on-demand pages: Recommended for you, by kind; Not interested.
	var list []suggestion
	if code := h.do("GET", "/livetv/vod/suggestions?kind=movie", nil, &list); code != 200 || len(list) != 2 {
		t.Fatalf("suggestions: %d %+v", code, list)
	}
	if code := h.do("DELETE", fmt.Sprintf("/livetv/vod/suggestions/movie/%d", ronin), nil, nil); code != 204 {
		t.Fatalf("hide: %d", code)
	}
	if code := h.do("DELETE", "/livetv/vod/suggestions/movie/9999", nil, nil); code != 404 {
		t.Errorf("hide a missing film: %d", code)
	}
	h.do("GET", "/livetv/vod/suggestions?kind=movie", nil, &list)
	if len(list) != 1 || list[0].Movie.Id != thief {
		t.Fatalf("after Not interested: %+v", list)
	}

	// Also on VOD on Heat's page.
	if code := h.do("GET", fmt.Sprintf("/items/%d/vod-similar", heat), nil, &list); code != 200 || len(list) != 1 || list[0].Movie.Id != thief ||
		list[0].Reason != "Similar to Heat" {
		t.Fatalf("also on VOD: %d %+v", code, list)
	}
	if code := h.do("GET", "/items/99999/vod-similar", nil, &list); code != 404 {
		t.Errorf("missing item: %d", code)
	}

	// Muse: on-demand titles only with includeVod.
	exec(`INSERT INTO tags(kind, name) VALUES ('genre', 'Crime')`)
	exec(`INSERT INTO item_tags(item_id, tag_id) SELECT ?, id FROM tags WHERE kind='genre' AND name='Crime'`, heat)
	var muse struct {
		Items []struct{ Title string }
		Vod   *[]suggestion
	}
	if code := h.do("POST", "/muse/video", map[string]any{"prompt": "crime"}, &muse); code != 200 || muse.Vod != nil {
		t.Fatalf("muse without includeVod: %d %+v", code, muse)
	}
	if code := h.do("POST", "/muse/video", map[string]any{"prompt": "crime", "includeVod": true}, &muse); code != 200 || muse.Vod == nil ||
		len(*muse.Vod) != 1 || (*muse.Vod)[0].Movie.Id != thief {
		t.Fatalf("muse with includeVod: %d %+v", code, muse.Vod)
	}

	// Discover: Watch now on VOD.
	var page struct {
		Results []struct {
			TmdbId int64
			Vod    *struct {
				Kind    string
				Id      int64
				CanSave bool
				Save    *struct{ State string }
			}
		}
	}
	if code := h.do("GET", "/requests/search?q=x", nil, &page); code != 200 || len(page.Results) != 4 {
		t.Fatalf("search: %d %+v", code, page)
	}
	if v := page.Results[0].Vod; v == nil || v.Kind != "movie" || v.Id != ronin {
		t.Errorf("Ronin on demand: %+v", v)
	}
	if v := page.Results[1].Vod; v == nil || v.Kind != "series" {
		t.Errorf("1899 on demand (partly here): %+v", v)
	}
	if page.Results[3].Vod != nil {
		t.Errorf("Thief is all here, so not offered on demand: %+v", page.Results[3].Vod)
	}
	if page.Results[2].Vod != nil {
		t.Errorf("Elsewhere isn't on demand: %+v", page.Results[2].Vod)
	}
	if v := page.Results[0].Vod; v == nil || v.CanSave {
		t.Errorf("an M3U film can't be downloaded: %+v", v)
	}

	// Download from VOD: with an Xtream Codes source and save folders, an admin can queue
	// the film or the show's missing episodes; a queued film says so.
	h.db.Exec(`UPDATE vod_movies SET source_key = 'xtream:' || source_key`)
	h.store.Update(ctx, func(s *settings.Settings) error {
		s.Integrations.VODSources[0].Kind, s.Integrations.VODSources[0].Username, s.Integrations.VODSources[0].Password = settings.VODKindXtream, "u", "p"
		s.Integrations.VODSources[0].URL = "http://127.0.0.1:1"
		s.VODSave.MoviesFolder, s.VODSave.ShowsFolder = h.media+"/video/Movies", h.media+"/video/Shows"
		return nil
	})
	h.do("GET", "/requests/search?q=x", nil, &page)
	if v := page.Results[0].Vod; v == nil || !v.CanSave || v.Save != nil {
		t.Errorf("Ronin can be downloaded: %+v", v)
	}
	if v := page.Results[1].Vod; v == nil || !v.CanSave {
		t.Errorf("1899 can be downloaded: %+v", v)
	}
	var queuedRaw json.RawMessage
	if code := h.do("POST", fmt.Sprintf("/livetv/vod/movies/%d/save", ronin), nil, &queuedRaw); code != 200 {
		t.Fatalf("queue Ronin: %d %s", code, queuedRaw)
	}
	h.do("GET", "/requests/search?q=x", nil, &page)
	if v := page.Results[0].Vod; v == nil || v.Save == nil || v.Save.State != "queued" {
		t.Errorf("Ronin queued: %+v", v)
	}

	// A profile without Live TV: no row, 403 from the endpoints, nothing on Discover.
	h.db.Exec(`UPDATE users SET is_admin=0, restrictions='{"liveTv":false,"canRequest":true}' WHERE id=?`, uid)
	if _, ok := hubs("?vod=true")["vod-suggestions"]; ok {
		t.Error("the row for a profile without Live TV")
	}
	if code := h.do("GET", "/livetv/vod/suggestions", nil, nil); code != 403 {
		t.Errorf("suggestions without Live TV: %d", code)
	}
	page.Results = nil
	h.do("GET", "/requests/search?q=x", nil, &page)
	if len(page.Results) == 0 || page.Results[0].Vod != nil {
		t.Error("on demand marked for a profile without Live TV")
	}
}
