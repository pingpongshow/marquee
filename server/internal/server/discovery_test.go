package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"marquee/internal/api"
	"marquee/internal/auth"
	"marquee/internal/avatars"
	"marquee/internal/db"
	"marquee/internal/items"
	"marquee/internal/library"
	"marquee/internal/netclass"
	"marquee/internal/settings"
	"marquee/internal/soundprint"
	"marquee/internal/tasks"
)

// fakeSidecar serves /embed_docs with keyword vectors: space, love, or neither.
func fakeSidecar(t *testing.T) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/embed_docs" {
			http.NotFound(w, r)
			return
		}
		var in struct {
			Texts []string `json:"texts"`
			Kind  string   `json:"kind"`
		}
		json.NewDecoder(r.Body).Decode(&in)
		out := [][]float32{}
		for _, t := range in.Texts {
			t = strings.ToLower(t)
			v := []float32{0.05, 0.05, 0.05}
			switch {
			case strings.Contains(t, "space") && strings.Contains(t, "love"):
				v[0], v[1] = 1, 1
			case strings.Contains(t, "space"):
				v[0] = 1
			case strings.Contains(t, "love"):
				v[1] = 1
			default:
				v[2] = 1
			}
			out = append(out, v)
		}
		json.NewEncoder(w).Encode(map[string]any{"model": "fake-bge", "embeddings": out})
	}))
	t.Cleanup(srv.Close)
	return srv
}

// newDiscoveryHarness is newHarness with the movie and show embedding index (USER-15/16).
func newDiscoveryHarness(t *testing.T, sidecarURL string) (*harness, *items.Embedder) {
	t.Helper()
	dir := t.TempDir()
	ctx := context.Background()
	database, err := db.Open(ctx, filepath.Join(dir, "test.db"), filepath.Join(dir, "backups"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	store, err := settings.Open(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	media := filepath.Join(dir, "media")
	for _, d := range []string{"video/Movies", "music"} {
		os.MkdirAll(filepath.Join(media, d), 0o755)
	}
	store.Update(ctx, func(s *settings.Settings) error {
		s.Library.BrowseRoots = []string{media}
		return nil
	})
	emb := &items.Embedder{DB: database, Index: items.NewVideoIndex(), Client: &soundprint.Client{BaseURL: sidecarURL, HTTP: &http.Client{Timeout: 5 * time.Second}}}
	authSvc := auth.NewService(database)
	h := New(Deps{
		Handlers: &api.Handlers{DB: database, Auth: authSvc, Settings: store,
			Libraries: library.NewStore(database), Items: items.NewStore(database), Version: "test",
			Avatars:    &avatars.Store{DB: database, Dir: filepath.Join(dir, "avatars")},
			Tasks:      &tasks.Scheduler{DB: database, Settings: store},
			Embeddings: emb},
		Auth:       authSvc,
		Classifier: netclass.New([]string{"127.0.0.0/8"}, ""),
	})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &harness{t: t, srv: srv, media: media, db: database}, emb
}

func TestMuseRecommendationsRecap(t *testing.T) {
	side := fakeSidecar(t)
	h, emb := newDiscoveryHarness(t, side.URL)
	ctx := context.Background()
	var res api.AuthResult
	h.do("POST", "/setup", map[string]any{"serverName": "T", "username": "admin", "password": "correct horse", "device": device}, &res)
	h.token = res.Token
	var me api.User
	h.do("GET", "/me", nil, &me)
	exec := func(q string, args ...any) int64 {
		r, err := h.db.Exec(q, args...)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := r.LastInsertId()
		return id
	}
	var lib api.Library
	h.do("POST", "/libraries", map[string]any{"name": "Movies", "type": "movies", "paths": []string{filepath.Join(h.media, "video/Movies")}}, &lib)
	movie := func(title, summary string, year int, rating float64) int64 {
		return exec(`INSERT INTO items(library_id, type, title, sort_title, summary, year, audience_rating) VALUES (?, 'movie', ?, ?, ?, ?, ?)`,
			lib.Id, title, title, summary, year, rating)
	}
	alien := movie("Alien", "Terror in deep space.", 1979, 8.5)
	moon := movie("Moon", "Alone in space.", 2009, 7.8)
	wall := movie("WALL-E", "A robot's love story in space.", 2008, 8.4)
	notebook := movie("The Notebook", "A love story.", 2004, 7.8)
	heat := movie("Heat", "Cops and robbers.", 1995, 8.3)
	sf := exec(`INSERT INTO tags(kind, name) VALUES ('genre', 'Science Fiction')`)
	for _, id := range []int64{alien, moon, wall} {
		exec(`INSERT INTO item_tags(item_id, tag_id) VALUES (?, ?)`, id, sf)
	}

	// Before indexing: the recommendation rows are in the layout but show nothing, and
	// related falls back to genres.
	var hubs0 []api.Hub
	h.do("GET", "/hubs/home", nil, &hubs0)
	for _, hb := range hubs0 {
		if hb.Id == "recommended" || strings.HasPrefix(hb.Id, "because-") {
			t.Fatalf("recommendation hubs without embeddings: %+v", hb)
		}
	}
	var rel []api.ItemSummary
	h.do("GET", fmt.Sprintf("/items/%d/related", alien), nil, &rel)
	if len(rel) != 2 {
		t.Fatalf("genre related: %+v", rel)
	}

	if msg, err := emb.Run(ctx); err != nil || emb.Index.Len() != 5 {
		t.Fatalf("embed: %q %v", msg, err)
	}

	// Related by meaning (Heat and The Notebook share nothing with Alien by genre).
	h.do("GET", fmt.Sprintf("/items/%d/related", alien), nil, &rel)
	if len(rel) != 4 || rel[0].Id != moon || rel[1].Id != wall {
		t.Fatalf("related: %+v", rel)
	}

	// Muse for movies.
	var mv api.MuseVideoResult
	if code := h.do("POST", "/muse/video", map[string]any{"prompt": "sci-fi movies about love"}, &mv); code != 200 {
		t.Fatalf("muse: %d", code)
	}
	if len(mv.Items) != 3 || mv.Items[0].Id != wall || mv.Understood != "Movies · Science Fiction · like 'love'" || *mv.Analysed != 1 {
		t.Fatalf("muse: %+v", mv)
	}
	h.do("POST", "/muse/video", map[string]any{"prompt": "a love story", "limit": 2}, &mv)
	if len(mv.Items) != 2 || mv.Items[0].Id != notebook {
		t.Fatalf("muse love: %+v", mv)
	}
	h.do("POST", "/muse/video", map[string]any{"prompt": "space", "types": []string{"show"}}, &mv)
	if len(mv.Items) != 0 {
		t.Fatalf("shows only: %+v", mv)
	}
	if code := h.do("POST", "/muse/video", map[string]any{"prompt": "x"}, nil); code != 400 {
		t.Fatalf("short prompt: %d", code)
	}

	// Recommendation rows follow the watchlist.
	var layout api.HomeLayout
	h.do("GET", "/me/home-layout", nil, &layout)
	var rowIDs []string
	for _, r := range layout.Rows {
		rowIDs = append(rowIDs, r.Id)
	}
	if fmt.Sprint(rowIDs[:4]) != "[continue-watching watchlist recommended because-you-watched]" {
		t.Fatalf("layout: %v", rowIDs)
	}
	hubIDs := func() string {
		var hubs []api.Hub
		h.do("GET", "/hubs/home", nil, &hubs)
		var out []string
		for _, hb := range hubs {
			out = append(out, hb.Id+"="+hb.Title)
		}
		return strings.Join(out, "|")
	}
	if got := hubIDs(); strings.Contains(got, "recommended") || strings.Contains(got, "because") {
		t.Fatalf("hubs without history: %s", got)
	}
	when := time.Now().UTC().Add(-time.Hour).Format("2006-01-02T15:04:05.000Z")
	exec(`INSERT INTO user_item_state(user_id, item_id, play_count, last_viewed_at, rating) VALUES (?, ?, 1, ?, 9)`, me.Id, alien, when)
	exec(`INSERT INTO user_item_state(user_id, item_id, play_count, last_viewed_at) VALUES (?, ?, 1, ?)`, me.Id, heat,
		time.Now().UTC().Add(-2*time.Hour).Format("2006-01-02T15:04:05.000Z"))
	got := hubIDs()
	want := fmt.Sprintf("recommended=Recommended for You|because-%d=Because you watched Alien|because-%d=Because you watched Heat", alien, heat)
	if !strings.HasPrefix(got, want) {
		t.Fatalf("hubs:\n got %s\nwant %s…", got, want)
	}
	var hubs []api.Hub
	h.do("GET", "/hubs/home", nil, &hubs)
	if hubs[1].Items[0].Id != moon || len(hubs[1].Items) != 3 {
		t.Fatalf("because alien: %+v", hubs[1].Items)
	}
	// The layout entry hides both "Because you watched" rows.
	h.do("PUT", "/me/home-layout", map[string]any{"rows": []map[string]any{{"id": "because-you-watched", "hidden": true}}}, &layout)
	if layout.Rows[0].Id != "because-you-watched" || !*layout.Rows[0].Hidden {
		t.Fatalf("saved: %+v", layout.Rows)
	}
	if got := hubIDs(); strings.Contains(got, "because") || !strings.Contains(got, "recommended") {
		t.Fatalf("hidden: %s", got)
	}

	// Listening recap (MUSIC-22).
	var music api.Library
	h.do("POST", "/libraries", map[string]any{"name": "Music", "type": "music", "paths": []string{filepath.Join(h.media, "music")}}, &music)
	artist := exec(`INSERT INTO items(library_id, type, title, sort_title) VALUES (?, 'artist', 'ABBA', 'ABBA')`, music.Id)
	album := exec(`INSERT INTO items(library_id, type, title, sort_title, parent_id) VALUES (?, 'album', 'Arrival', 'Arrival', ?)`, music.Id, artist)
	track := exec(`INSERT INTO items(library_id, type, title, sort_title, parent_id, grandparent_id, duration_ms) VALUES (?, 'track', 'SOS', 'SOS', ?, ?, 200000)`,
		music.Id, album, artist)
	year := time.Now().Year()
	for i := 0; i < 3; i++ {
		at := time.Date(year, 2, 1+i, 12, 0, 0, 0, time.Local).UTC().Format("2006-01-02T15:04:05.000Z")
		exec(`INSERT INTO play_history(user_id, item_id, item_title, started_at, stopped_at, position_ms) VALUES (?, ?, 'SOS', ?, ?, 200000)`,
			me.Id, track, at, at)
	}
	var rc api.ListeningRecap
	h.do("GET", "/me/recap", nil, &rc)
	if rc.Year != year || rc.Plays != 3 || rc.Minutes != 10 || rc.LongestStreakDays != 3 || len(rc.ByMonth) != 12 || rc.ByMonth[1] != 10 ||
		len(rc.ByHour) != 24 || len(rc.TopTracks) != 1 || rc.TopTracks[0].Item.Id != track || rc.TopArtists[0].Item.Id != artist ||
		rc.FirstTrack == nil || rc.TopDay == nil || rc.NewArtists != 1 {
		t.Fatalf("recap: %+v", rc)
	}
	var empty api.ListeningRecap
	if code := h.do("GET", "/me/recap?year=2001", nil, &empty); code != 200 || empty.Plays != 0 || empty.TopTracks == nil {
		t.Fatalf("empty recap: %d %+v", code, empty)
	}
	var years []int
	h.do("GET", "/me/recap/years", nil, &years)
	if fmt.Sprint(years) != fmt.Sprintf("[%d]", year) {
		t.Fatalf("years: %v", years)
	}
	var pl, pl2 api.Playlist
	if code := h.do("POST", "/me/recap/playlist", nil, &pl); code != 201 || pl.Title != fmt.Sprintf("Your Top Songs %d", year) || pl.ItemCount != 1 {
		t.Fatalf("playlist: %d %+v", code, pl)
	}
	h.do("POST", fmt.Sprintf("/me/recap/playlist?year=%d", year), nil, &pl2)
	var all []api.Playlist
	h.do("GET", "/playlists", nil, &all)
	if pl2.Id != pl.Id || pl2.ItemCount != 1 || len(all) != 1 {
		t.Fatalf("refresh: %+v %d", pl2, len(all))
	}
	if code := h.do("POST", "/me/recap/playlist?year=2001", nil, nil); code != 404 {
		t.Fatalf("empty playlist: %d", code)
	}
}

func TestMuseVideoWithoutSidecar(t *testing.T) {
	h, emb := newDiscoveryHarness(t, "http://127.0.0.1:1")
	var res api.AuthResult
	h.do("POST", "/setup", map[string]any{"serverName": "T", "username": "admin", "password": "correct horse", "device": device}, &res)
	h.token = res.Token
	if code := h.do("POST", "/muse/video", map[string]any{"prompt": "time travel"}, nil); code != 503 {
		t.Fatalf("no sidecar, nothing indexed: %d", code)
	}
	// The task doesn't fail without the sidecar.
	if msg, err := emb.Run(context.Background()); err != nil || !strings.Contains(msg, "isn't running") {
		t.Fatalf("run: %q %v", msg, err)
	}
	// Filters alone still work: nothing to rank by meaning.
	var mv api.MuseVideoResult
	if code := h.do("POST", "/muse/video", map[string]any{"prompt": "90s movies"}, &mv); code != 200 || mv.Understood != "Movies · 1990s" {
		t.Fatalf("filters only: %d %+v", code, mv)
	}
	// With something indexed, a down sidecar degrades to rating order.
	emb.Index.Replace("fake-bge", []*items.VideoVec{{ItemID: 1, LibraryID: 1, Type: "movie", Vec: []float32{1}}})
	if code := h.do("POST", "/muse/video", map[string]any{"prompt": "time travel"}, &mv); code != 200 {
		t.Fatalf("degraded: %d", code)
	}
}
