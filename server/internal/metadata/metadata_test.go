package metadata

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"marquee/internal/db"
	"marquee/internal/items"
	"marquee/internal/metadata/tmdb"
	"marquee/internal/settings"
)

func TestBestMatch(t *testing.T) {
	res := []tmdb.SearchResult{
		{ID: 1, Title: "It", ReleaseDate: "1990-11-18", VoteCount: 1000},
		{ID: 2, Title: "It", ReleaseDate: "2017-09-06", VoteCount: 18000},
		{ID: 3, Title: "It Follows", ReleaseDate: "2014-05-17"},
	}
	if got := bestMatch(res, "IT", 2017); got != 2 {
		t.Errorf("IT (2017) → %d", got)
	}
	if got := bestMatch(res, "It", 1990); got != 1 {
		t.Errorf("It (1990) → %d", got)
	}
	if got := bestMatch(res, "It", 0); got != 2 {
		t.Errorf("It (no year) should prefer the popular one, got %d", got)
	}
	if got := bestMatch(res, "Something Else", 2017); got != 0 {
		t.Errorf("unrelated title matched %d", got)
	}
	if got := bestMatch([]tmdb.SearchResult{{ID: 9, Title: "Heat", ReleaseDate: "1972-01-01"}}, "Heat", 1995); got != 0 {
		t.Errorf("year mismatch matched %d", got)
	}
	if got := bestMatch([]tmdb.SearchResult{{ID: 5, Title: "A Dog of Flanders", FirstAirDate: "1975-01-05"}}, "Dog of Flanders", 0); got != 5 {
		t.Errorf("leading article: %d", got)
	}
	godzilla := []tmdb.SearchResult{{ID: 7, Title: "Godzilla Raids Again", ReleaseDate: "1955-04-24"}, {ID: 8, Title: "Godzilla", ReleaseDate: "1954-11-03"}}
	if got := soleYearMatch(godzilla, "Godzilla Gigantis The Fire Monster", 1955); got != 7 {
		t.Errorf("alternative-title match: %d", got)
	}
	if got := soleYearMatch([]tmdb.SearchResult{{ID: 3, Title: "DEATH BEFORE DISHONOUR", ReleaseDate: "2026-01-01"}}, "The Dish", 2026); got != 0 {
		t.Errorf("unrelated title matched on year alone: %d", got)
	}
	if got := soleYearMatch(append(godzilla, tmdb.SearchResult{ID: 9, ReleaseDate: "1955-01-01"}), "Godzilla Gigantis", 1955); got != 0 {
		t.Errorf("ambiguous year should not match: %d", got)
	}
}

func fakeTMDB(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("api_key") != "k" {
			w.WriteHeader(401)
			return
		}
		var body any
		switch {
		case r.URL.Path == "/search/movie":
			body = map[string]any{"results": []map[string]any{{"id": 603, "title": "The Matrix", "release_date": "1999-03-30", "vote_count": 25000}}}
		case r.URL.Path == "/movie/603":
			body = map[string]any{
				"id": 603, "title": "The Matrix", "original_title": "The Matrix", "overview": "A hacker learns the truth.",
				"tagline": "Welcome to the Real World.", "release_date": "1999-03-30", "vote_average": 8.2, "imdb_id": "tt0133093",
				"genres":               []map[string]any{{"name": "Action"}, {"name": "Science Fiction"}},
				"production_companies": []map[string]any{{"name": "Warner Bros."}},
				"poster_path":          "/p.jpg", "backdrop_path": "/b.jpg",
				"credits": map[string]any{
					"cast": []map[string]any{{"id": 6384, "name": "Keanu Reeves", "character": "Neo", "order": 0, "profile_path": "/k.jpg"}},
					"crew": []map[string]any{{"id": 9339, "name": "Lana Wachowski", "job": "Director"}},
				},
				"images":        map[string]any{"posters": []any{}, "backdrops": []any{}, "logos": []map[string]any{{"file_path": "/l.png", "iso_639_1": "en"}}},
				"release_dates": map[string]any{"results": []map[string]any{{"iso_3166_1": "US", "release_dates": []map[string]any{{"certification": "R"}}}}},
			}
		default:
			w.WriteHeader(404)
			return
		}
		json.NewEncoder(w).Encode(body)
	}))
}

func TestMatchMovie(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(dir, "t.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	store, _ := settings.Open(ctx, d)
	store.Update(ctx, func(s *settings.Settings) error { s.Metadata.TMDBAPIKey = "k"; return nil })
	srv := fakeTMDB(t)
	defer srv.Close()

	mustExec(t, d, `INSERT INTO libraries(id, name, type) VALUES (1, 'Movies', 'movies')`)
	mustExec(t, d, `INSERT INTO items(id, library_id, type, title, sort_title, year, locked_fields) VALUES (10, 1, 'movie', 'Matrix', 'Matrix', 1999, '["tagline"]')`)
	mustExec(t, d, `INSERT INTO items(id, library_id, type, title, sort_title, year) VALUES (11, 1, 'movie', 'Nothing Like This', 'Nothing', 1980)`)
	mustExec(t, d, `INSERT INTO artwork(item_id, kind, source, local_path, selected) VALUES (10, 'poster', 'local', '/m/poster.jpg', 1)`)

	svc := &Service{DB: d, Settings: store}
	c, _ := svc.tmdb("en-US")
	c.BaseURL = srv.URL
	if err := svc.MatchLibrary(ctx, 1, "movies", "en-US", nil); err != nil {
		t.Fatal(err)
	}

	var title, state, summary, rating, studio string
	var tagline sql.NullString
	d.QueryRow(`SELECT title, match_state, summary, tagline, content_rating, studio FROM items WHERE id = 10`).Scan(&title, &state, &summary, &tagline, &rating, &studio)
	if title != "The Matrix" || state != "matched" || !strings.Contains(summary, "hacker") || rating != "R" || studio != "Warner Bros." {
		t.Errorf("movie: %q %q %q %q %q", title, state, summary, rating, studio)
	}
	if tagline.Valid {
		t.Error("locked tagline was overwritten")
	}
	d.QueryRow(`SELECT match_state FROM items WHERE id = 11`).Scan(&state)
	if state != "failed" {
		t.Errorf("unmatchable item state = %s", state)
	}
	var n int
	d.QueryRow(`SELECT COUNT(*) FROM artwork WHERE item_id = 10 AND kind = 'poster' AND selected = 1 AND source = 'local'`).Scan(&n)
	if n != 1 {
		t.Error("local poster should stay selected")
	}
	d.QueryRow(`SELECT COUNT(*) FROM artwork WHERE item_id = 10 AND selected = 1 AND source = 'tmdb'`).Scan(&n)
	if n != 2 { // backdrop + logo
		t.Errorf("tmdb selected artwork = %d", n)
	}
	d.QueryRow(`SELECT COUNT(*) FROM credits WHERE item_id = 10`).Scan(&n)
	if n != 2 {
		t.Errorf("credits = %d", n)
	}
	d.QueryRow(`SELECT COUNT(*) FROM external_ids WHERE item_id = 10 AND provider IN ('tmdb', 'imdb')`).Scan(&n)
	if n != 2 {
		t.Errorf("external ids = %d", n)
	}
	// Search sees the new title.
	d.QueryRow(`SELECT COUNT(*) FROM items_fts WHERE items_fts MATCH 'matrix'`).Scan(&n)
	if n != 1 {
		t.Errorf("fts = %d", n)
	}
}

func mustExec(t *testing.T, d *sql.DB, q string) {
	t.Helper()
	if _, err := d.Exec(q); err != nil {
		t.Fatal(err)
	}
}

func TestSeasonsByYear(t *testing.T) {
	show := &tmdb.TVShow{}
	for i, y := range []string{"2003-01-23", "2004-01-11", "2005-01-12", "2005-12-01"} {
		show.Seasons = append(show.Seasons, tmdb.SeasonSummary{SeasonNumber: i + 1, AirDate: y})
	}
	m := seasonsByYear(show)
	if m[2003] != 1 || m[2004] != 2 {
		t.Errorf("by year: %v", m)
	}
	if _, ok := m[2005]; ok {
		t.Error("ambiguous year 2005 should not map")
	}
}

func TestEditLocksSurviveFixMatch(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(dir, "t.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	store, _ := settings.Open(ctx, d)
	store.Update(ctx, func(s *settings.Settings) error { s.Metadata.TMDBAPIKey = "k"; return nil })
	srv := fakeTMDB(t)
	defer srv.Close()
	mustExec(t, d, `INSERT INTO libraries(id, name, type) VALUES (1, 'Movies', 'movies')`)
	mustExec(t, d, `INSERT INTO items(id, library_id, type, title, sort_title, year, match_state) VALUES (10, 1, 'movie', 'Wrong', 'Wrong', 1999, 'failed')`)

	st := items.NewStore(d)
	if err := st.Edit(ctx, 10, map[string]any{"title": "My Matrix Cut"}, nil); err != nil {
		t.Fatal(err)
	}
	svc := &Service{DB: d, Settings: store}
	c, _ := svc.tmdb("en-US")
	c.BaseURL = srv.URL
	cands, err := svc.Candidates(ctx, 10, "Matrix", 1999)
	if err != nil || len(cands) != 1 || cands[0].ID != "603" {
		t.Fatalf("candidates: %v %+v", err, cands)
	}
	if err := svc.MatchTo(ctx, 10, 603); err != nil {
		t.Fatal(err)
	}
	det, err := st.Get(ctx, items.Unrestricted, 10, false)
	if err != nil {
		t.Fatal(err)
	}
	if det.Title != "My Matrix Cut" || det.MatchState != "matched" || det.ExternalIDs["tmdb"] != "603" || !strings.Contains(det.Plot, "hacker") {
		t.Fatalf("after match: title=%q state=%s ids=%v", det.Title, det.MatchState, det.ExternalIDs)
	}
	if len(det.LockedFields) != 2 { // title + sortTitle
		t.Errorf("locked: %v", det.LockedFields)
	}
	// Unlocking hands the title back to the agent on refresh.
	st.Edit(ctx, 10, map[string]any{}, []string{"title", "sortTitle"})
	if err := svc.Refresh(ctx, 10); err != nil {
		t.Fatal(err)
	}
	det, _ = st.Get(ctx, items.Unrestricted, 10, false)
	if det.Title != "The Matrix" {
		t.Errorf("after unlock+refresh: %q", det.Title)
	}
}

func TestAbsoluteEpisodes(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	d, _ := db.Open(ctx, filepath.Join(dir, "t.db"), dir)
	defer d.Close()
	store, _ := settings.Open(ctx, d)
	mustExec(t, d, `INSERT INTO libraries(id, name, type) VALUES (1, 'Anime', 'anime')`)
	mustExec(t, d, `INSERT INTO items(id, library_id, type, title, sort_title) VALUES (1, 1, 'show', 'S', 'S')`)
	mustExec(t, d, `INSERT INTO items(id, library_id, type, title, sort_title, parent_id, idx) VALUES (2, 1, 'season', 'Season 1', '1', 1, 1)`)
	mustExec(t, d, `INSERT INTO items(id, library_id, type, title, sort_title, parent_id, grandparent_id, idx) VALUES (3, 1, 'episode', 'Episode 5', '5', 2, 1, 5)`)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		eps := []map[string]any{{"episode_number": 1, "name": "S2E1"}, {"episode_number": 2, "name": "S2E2"}, {"episode_number": 3, "name": "S2E3"}}
		json.NewEncoder(w).Encode(map[string]any{"season_number": 2, "episodes": eps})
	}))
	defer srv.Close()
	c := tmdb.New("k", "en")
	c.BaseURL = srv.URL
	svc := &Service{DB: d, Settings: store}
	first := &tmdb.Season{SeasonNumber: 1, Episodes: []tmdb.Episode{{EpisodeNumber: 1, Name: "E1"}, {EpisodeNumber: 2, Name: "E2"}}}
	show := &tmdb.TVShow{ID: 9, Seasons: []tmdb.SeasonSummary{{SeasonNumber: 1}, {SeasonNumber: 2}}}
	abs, err := svc.absoluteEpisodes(ctx, c, show, 2, first)
	if err != nil || abs == nil {
		t.Fatalf("abs: %v %v", abs, err)
	}
	if len(abs.Episodes) != 5 || abs.Episodes[4].Name != "S2E3" || abs.Episodes[4].EpisodeNumber != 5 {
		t.Fatalf("episodes: %+v", abs.Episodes)
	}
}

func TestSameTitleShowsPickedBySeasonFit(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	d, _ := db.Open(ctx, filepath.Join(dir, "t.db"), dir)
	defer d.Close()
	store, _ := settings.Open(ctx, d)
	store.Update(ctx, func(s *settings.Settings) error { s.Metadata.TMDBAPIKey = "k"; return nil })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body any
		switch r.URL.Path {
		case "/search/tv":
			body = map[string]any{"results": []map[string]any{
				{"id": 37854, "name": "One Piece", "first_air_date": "1999-10-20", "vote_count": 5000},
				{"id": 111110, "name": "ONE PIECE", "first_air_date": "2023-08-31", "vote_count": 3000},
			}}
		case "/tv/37854":
			seasons := []map[string]any{}
			for i := 1; i <= 22; i++ {
				seasons = append(seasons, map[string]any{"season_number": i, "episode_count": 40})
			}
			body = map[string]any{"id": 37854, "name": "One Piece", "first_air_date": "1999-10-20", "seasons": seasons}
		case "/tv/111110":
			body = map[string]any{"id": 111110, "name": "ONE PIECE", "first_air_date": "2023-08-31",
				"seasons": []map[string]any{{"season_number": 1, "episode_count": 8}, {"season_number": 2, "episode_count": 8}}}
		default:
			body = map[string]any{"episodes": []any{}}
		}
		json.NewEncoder(w).Encode(body)
	}))
	defer srv.Close()
	mustExec(t, d, `INSERT INTO libraries(id, name, type) VALUES (1, 'TV', 'shows')`)
	// A previous wrong match left year 1999 on the item; matching must use the folder's (none).
	mustExec(t, d, `INSERT INTO items(id, library_id, type, title, sort_title, scan_key, year) VALUES (1, 1, 'show', 'One Piece', 'One Piece', 't:onepiece:0', 1999)`)
	for s := 1; s <= 2; s++ {
		mustExec(t, d, fmt.Sprintf(`INSERT INTO items(id, library_id, type, title, sort_title, parent_id, idx) VALUES (%d, 1, 'season', 'S', 'S', 1, %d)`, 1000+s, s))
	}
	for s := 1; s <= 2; s++ {
		for e := 1; e <= 8; e++ {
			mustExec(t, d, fmt.Sprintf(`INSERT INTO items(library_id, type, title, sort_title, parent_id, grandparent_id, idx) VALUES (1, 'episode', 'E', 'E', %d, 1, %d)`, 1000+s, e))
		}
	}
	svc := &Service{DB: d, Settings: store}
	c, _ := svc.tmdb("en-US")
	c.BaseURL = srv.URL
	if err := svc.MatchLibrary(ctx, 1, "shows", "en-US", nil); err != nil {
		t.Fatal(err)
	}
	var tmdbID string
	d.QueryRow(`SELECT value FROM external_ids WHERE item_id = 1 AND provider = 'tmdb'`).Scan(&tmdbID)
	if tmdbID != "111110" {
		t.Fatalf("matched %s, want the 2-season show 111110", tmdbID)
	}
}
