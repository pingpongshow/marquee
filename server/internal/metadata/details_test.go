package metadata

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"marquee/internal/db"
	"marquee/internal/settings"
)

// TMDB extras: original language, countries, keywords and recommendations, at match time
// (appended to the details request) and by the backfill for items matched before.
func TestVideoExtras(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(dir, "t.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	store, _ := settings.Open(ctx, d)
	store.Update(ctx, func(s *settings.Settings) error { s.Metadata.TMDBAPIKey = "k"; return nil })
	var appended []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		appended = append(appended, r.URL.Path+"?"+r.URL.Query().Get("append_to_response"))
		var body any
		switch r.URL.Path {
		case "/search/movie":
			body = map[string]any{"results": []map[string]any{{"id": 496243, "title": "Parasite", "release_date": "2019-05-30", "vote_count": 9000}}}
		case "/movie/496243":
			body = map[string]any{"id": 496243, "title": "Parasite", "original_title": "기생충", "release_date": "2019-05-30",
				"original_language": "ko", "production_countries": []map[string]any{{"iso_3166_1": "KR", "name": "South Korea"}},
				"keywords":        map[string]any{"keywords": []map[string]any{{"id": 1, "name": "Class Differences"}, {"id": 2, "name": "dark comedy"}, {"id": 3, "name": "class differences"}}},
				"recommendations": map[string]any{"results": []map[string]any{{"id": 11}, {"id": 22}, {"id": 0}}}}
		case "/tv/1399":
			body = map[string]any{"id": 1399, "name": "Thrones", "original_language": "en", "origin_country": []string{"US"},
				"production_countries": []map[string]any{{"iso_3166_1": "GB"}, {"iso_3166_1": "US"}},
				"keywords":             map[string]any{"results": []map[string]any{{"name": "dragon"}}},
				"recommendations":      map[string]any{"results": []map[string]any{{"id": 33}}}}
		default:
			w.WriteHeader(404)
			return
		}
		json.NewEncoder(w).Encode(body)
	}))
	defer srv.Close()
	mustExec(t, d, `INSERT INTO libraries(id, name, type) VALUES (1, 'Movies', 'movies'), (2, 'Shows', 'shows')`)
	mustExec(t, d, `INSERT INTO items(id, library_id, type, title, sort_title, year) VALUES (10, 1, 'movie', 'Parasite', 'Parasite', 2019)`)
	// Matched before extras existed.
	mustExec(t, d, `INSERT INTO items(id, library_id, type, title, sort_title, match_state) VALUES (20, 2, 'show', 'Thrones', 'Thrones', 'matched')`)
	mustExec(t, d, `INSERT INTO external_ids(item_id, provider, value) VALUES (20, 'tmdb', '1399')`)

	svc := &Service{DB: d, Settings: store}
	c, _ := svc.tmdb(store.Get().General.MetadataLanguage)
	c.BaseURL = srv.URL
	if err := svc.MatchLibrary(ctx, 1, "movies", "", nil); err != nil {
		t.Fatal(err, appended)
	}
	str := func(q string) string {
		var v string
		d.QueryRow(q).Scan(&v)
		return v
	}
	tags := func(id int, kind string) string {
		return str(`SELECT group_concat(name, ',') FROM (SELECT t.name FROM item_tags it JOIN tags t ON t.id = it.tag_id
			WHERE it.item_id = ` + strconv.Itoa(id) + ` AND t.kind = '` + kind + `' ORDER BY t.name)`)
	}
	if !strings.Contains(strings.Join(appended, " "), "/movie/496243?credits,images,release_dates,keywords,recommendations") {
		t.Errorf("extras not appended: %v", appended)
	}
	if v := str(`SELECT original_language FROM items WHERE id = 10`); v != "ko" {
		t.Errorf("language %q", v)
	}
	if v := tags(10, "country"); v != "KR" {
		t.Errorf("countries %q", v)
	}
	if v := tags(10, "keyword"); v != "class differences,dark comedy" {
		t.Errorf("keywords %q", v)
	}
	if v := str(`SELECT group_concat(tmdb_id || ':' || rank, ',') FROM (SELECT * FROM tmdb_recommendations WHERE item_id = 10 ORDER BY rank)`); v != "11:1,22:2" {
		t.Errorf("recommendations %q", v)
	}

	DetailsPace = 0
	msg, err := svc.FetchVideoDetails(ctx)
	if err != nil || !strings.Contains(msg, "1 of 1") {
		t.Fatalf("backfill: %q %v", msg, err)
	}
	if v := tags(20, "country"); v != "GB,US" {
		t.Errorf("show countries %q", v)
	}
	if v := tags(20, "keyword") + " " + str(`SELECT tmdb_id FROM tmdb_recommendations WHERE item_id = 20`); v != "dragon 33" {
		t.Errorf("show extras %q", v)
	}
	if msg, _ := svc.FetchVideoDetails(ctx); !strings.Contains(msg, "Every matched") {
		t.Errorf("second backfill: %q", msg)
	}
}
