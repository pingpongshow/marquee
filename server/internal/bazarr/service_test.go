package bazarr_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"marquee/internal/bazarr"
	"marquee/internal/bazarr/bazarrtest"
	"marquee/internal/db"
)

type env struct {
	t     *testing.T
	db    *sql.DB
	media string
	fake  *bazarrtest.Fake
	svc   *bazarr.Service
}

func (e *env) exec(q string, args ...any) int64 {
	e.t.Helper()
	r, err := e.db.Exec(q, args...)
	if err != nil {
		e.t.Fatal(err)
	}
	id, _ := r.LastInsertId()
	return id
}

// file adds a video file (on disk and in the database) to an item.
func (e *env) file(lib, item int64, rel string) int64 {
	p := filepath.Join(e.media, rel)
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte("video"), 0o644)
	v := e.exec(`INSERT INTO media_versions(item_id) VALUES (?)`, item)
	return e.exec(`INSERT INTO media_files(version_id, library_id, path, size, mtime) VALUES (?, ?, ?, 5, 1)`, v, lib, p)
}

// newEnv: Marquee sees /media/Movies and /media/Shows; Bazarr sees /movies and /tv.
func newEnv(t *testing.T) *env {
	dir := t.TempDir()
	d, err := db.Open(context.Background(), filepath.Join(dir, "t.db"), filepath.Join(dir, "b"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	e := &env{t: t, db: d, media: filepath.Join(dir, "media")}
	e.fake = (&bazarrtest.Fake{
		Local: func(p string) string {
			p = strings.Replace(p, "/movies/", "/Movies/", 1)
			p = strings.Replace(p, "/tv/", "/Shows/", 1)
			return filepath.Join(e.media, p)
		},
		Movies: []bazarr.Movie{
			{RadarrID: 1, Title: "Heat", Path: "/movies/Heat (1995)/Heat (1995) Bluray-1080p.mkv", IMDbID: "tt0113277",
				Subtitles: []bazarr.Language{{Name: "English", Code2: "en", Code3: "eng"}}, // embedded: no path
				Missing:   []bazarr.Language{{Name: "French", Code2: "fr", Code3: "fra"}, {Name: "English", Code2: "en", Forced: true}}},
			{RadarrID: 2, Title: "Alien", Path: "/movies/Alien (1979)/Alien.1979.mkv", IMDbID: "tt0078748"},
		},
		Series: []bazarr.Series{{SeriesID: 10, Title: "Severance", Path: "/tv/Severance", TVDbID: 371980}},
		Episodes: []bazarr.Episode{
			{SeriesID: 10, EpisodeID: 100, Season: 1, Episode: 1, Path: "/tv/Severance/Season 01/Severance - S01E01.mkv",
				Missing: []bazarr.Language{{Name: "German", Code2: "de"}}},
			{SeriesID: 10, EpisodeID: 101, Season: 1, Episode: 2, Path: "/tv/Severance/Season 01/Severance - S01E02 - renamed.mkv"},
		},
	}).Start(t)
	e.svc = &bazarr.Service{DB: d, RetryDelay: 10 * time.Millisecond,
		Config: func() (string, string) { return e.fake.URL, bazarrtest.APIKey }}
	return e
}

type fixture struct {
	movies, shows                  int64
	heat, alien, unknown, ep1, ep2 int64
	heatFile, ep1File, alienFile   int64
}

func (e *env) library() fixture {
	var f fixture
	f.movies = e.exec(`INSERT INTO libraries(name, type) VALUES ('Movies', 'movies')`)
	f.shows = e.exec(`INSERT INTO libraries(name, type) VALUES ('Shows', 'shows')`)
	f.heat = e.exec(`INSERT INTO items(library_id, type, title, sort_title) VALUES (?, 'movie', 'Heat', 'Heat')`, f.movies)
	f.heatFile = e.file(f.movies, f.heat, "Movies/Heat (1995)/Heat (1995) Bluray-1080p.mkv")
	// Alien's file has another name in Marquee's folder; the IMDb id still finds it.
	f.alien = e.exec(`INSERT INTO items(library_id, type, title, sort_title) VALUES (?, 'movie', 'Alien', 'Alien')`, f.movies)
	f.alienFile = e.file(f.movies, f.alien, "Movies/Alien/Alien (1979).mkv")
	e.exec(`INSERT INTO external_ids(item_id, provider, value) VALUES (?, 'imdb', 'tt0078748')`, f.alien)
	f.unknown = e.exec(`INSERT INTO items(library_id, type, title, sort_title) VALUES (?, 'movie', 'Home Video', 'Home Video')`, f.movies)
	e.file(f.movies, f.unknown, "Movies/Home Video/Home Video.mkv")
	show := e.exec(`INSERT INTO items(library_id, type, title, sort_title) VALUES (?, 'show', 'Severance', 'Severance')`, f.shows)
	e.exec(`INSERT INTO external_ids(item_id, provider, value) VALUES (?, 'tvdb', '371980')`, show)
	season := e.exec(`INSERT INTO items(library_id, type, title, sort_title, parent_id, idx) VALUES (?, 'season', 'Season 1', 'Season 1', ?, 1)`, f.shows, show)
	f.ep1 = e.exec(`INSERT INTO items(library_id, type, title, sort_title, parent_id, grandparent_id, idx) VALUES (?, 'episode', 'Good News', 'Good News', ?, ?, 1)`, f.shows, season, show)
	f.ep1File = e.file(f.shows, f.ep1, "Shows/Severance/Season 01/Severance - S01E01.mkv")
	// Episode 2's file name differs; season and episode numbers find it.
	f.ep2 = e.exec(`INSERT INTO items(library_id, type, title, sort_title, parent_id, grandparent_id, idx) VALUES (?, 'episode', 'Half Loop', 'Half Loop', ?, ?, 2)`, f.shows, season, show)
	e.file(f.shows, f.ep2, "Shows/Severance/Season 01/Severance - S01E02.mkv")
	return f
}

func TestTail(t *testing.T) {
	for in, want := range map[string]string{
		"/movies/Heat (1995)/Heat (1995).mkv":             "heat (1995)/heat (1995).mkv",
		`D:\Movies\Heat (1995)\Heat (1995).mkv`:           "heat (1995)/heat (1995).mkv",
		"/media/video/Movies/Heat (1995)/Heat (1995).mkv": "heat (1995)/heat (1995).mkv",
		"Heat.mkv": "heat.mkv",
	} {
		if got := bazarr.Tail(in); got != want {
			t.Errorf("Tail(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFlag(t *testing.T) {
	var v struct{ A, B, C, D bazarr.Flag }
	if err := json.Unmarshal([]byte(`{"A":"True","B":true,"C":"False","D":null}`), &v); err != nil {
		t.Fatal(err)
	}
	if !v.A || !v.B || v.C || v.D {
		t.Fatalf("%+v", v)
	}
}

func TestResolve(t *testing.T) {
	e := newEnv(t)
	f := e.library()
	ctx := context.Background()
	cases := []struct {
		item int64
		want bazarr.Target
	}{
		{f.heat, bazarr.Target{Kind: "movie", RadarrID: 1, FileID: f.heatFile}},
		{f.alien, bazarr.Target{Kind: "movie", RadarrID: 2, FileID: f.alienFile}},
		{f.ep1, bazarr.Target{Kind: "episode", SeriesID: 10, EpisodeID: 100, FileID: f.ep1File}},
		{f.ep2, bazarr.Target{Kind: "episode", SeriesID: 10, EpisodeID: 101}},
	}
	for _, c := range cases {
		got, err := e.svc.Resolve(ctx, c.item)
		if err != nil || got.Kind != c.want.Kind || got.RadarrID != c.want.RadarrID || got.SeriesID != c.want.SeriesID ||
			got.EpisodeID != c.want.EpisodeID || (c.want.FileID != 0 && got.FileID != c.want.FileID) {
			t.Errorf("item %d: %+v %v, want %+v", c.item, got, err, c.want)
		}
	}
	if _, err := e.svc.Resolve(ctx, f.unknown); !errors.Is(err, bazarr.ErrNotManaged) {
		t.Errorf("unknown movie: %v", err)
	}
	var show int64
	e.db.QueryRow(`SELECT id FROM items WHERE type = 'show'`).Scan(&show)
	if _, err := e.svc.Resolve(ctx, show); !errors.Is(err, bazarr.ErrNotManaged) {
		t.Errorf("show: %v", err)
	}
	off := &bazarr.Service{DB: e.db, Config: func() (string, string) { return "", "" }}
	if _, err := off.Resolve(ctx, f.heat); !errors.Is(err, bazarr.ErrNotConfigured) || off.Configured() {
		t.Errorf("not configured: %v", err)
	}
	bad := &bazarr.Service{DB: e.db, Config: func() (string, string) { return e.fake.URL, "wrong" }}
	if _, err := bad.Resolve(ctx, f.heat); err == nil || !strings.Contains(err.Error(), "API key") {
		t.Errorf("wrong key: %v", err)
	}
}

func TestLanguagesSearchDownload(t *testing.T) {
	e := newEnv(t)
	f := e.library()
	ctx := context.Background()
	heat, _ := e.svc.Resolve(ctx, f.heat)
	have, missing, err := e.svc.Languages(ctx, heat)
	if err != nil || len(have) != 1 || have[0].Code2 != "en" || len(missing) != 2 {
		t.Fatalf("languages: %+v %+v %v", have, missing, err)
	}
	if d := bazarr.Describe(missing); d != "French, English (forced)" {
		t.Errorf("describe: %q", d)
	}

	res, err := e.svc.Search(ctx, heat)
	if err != nil || len(res) != 2 || res[0].Subtitle != "b64:best" || !res[0].HearingImpaired || !res[0].OriginalFormat || res[0].ReleaseInfo[1] != "Other" {
		t.Fatalf("search: %+v %v", res, err)
	}

	// A download runs in the background and attaches Bazarr's new file to the item.
	e.svc.Download(heat, bazarr.Options{Language: "fr"})
	e.svc.Download(heat, bazarr.Options{Language: "fr"}) // already running: not asked twice
	e.svc.Wait()
	if calls := e.fake.Calls(); len(calls) != 1 || calls[0] != "movie 1 fr" {
		t.Fatalf("calls: %v", calls)
	}
	var lang, path string
	if err := e.db.QueryRow(`SELECT language, external_path FROM streams WHERE file_id = ? AND kind = 'subtitle'`, f.heatFile).Scan(&lang, &path); err != nil ||
		lang != "fre" || !strings.HasSuffix(path, "Heat (1995) Bluray-1080p.fr.srt") {
		t.Fatalf("stream: %q %q %v", lang, path, err)
	}
	if _, missing, _ := e.svc.Languages(ctx, heat); len(missing) != 1 {
		t.Errorf("French still missing: %+v", missing)
	}

	ep, _ := e.svc.Resolve(ctx, f.ep1)
	e.svc.Pick(ep, bazarr.Pick{Provider: "opensubtitlescom", Subtitle: "b64:best", HI: true})
	e.svc.Wait()
	var n int
	e.db.QueryRow(`SELECT COUNT(*) FROM streams WHERE file_id = ? AND is_hearing_impaired = 1 AND language = 'eng'`, f.ep1File).Scan(&n)
	if n != 1 {
		t.Fatalf("picked subtitle not attached; calls %v", e.fake.Calls())
	}

	// Failures are logged, not attached.
	e.fake.Fail = true
	e.svc.Download(heat, bazarr.Options{Language: "de"})
	e.svc.Wait()
	e.db.QueryRow(`SELECT COUNT(*) FROM streams WHERE file_id = ?`, f.heatFile).Scan(&n)
	if n != 1 {
		t.Fatalf("streams after failure: %d", n)
	}
}

func TestWanted(t *testing.T) {
	e := newEnv(t)
	f := e.library()
	list, err := e.svc.Wanted(context.Background())
	if err != nil || len(list) != 2 {
		t.Fatalf("wanted: %+v %v", list, err)
	}
	got := map[int64]string{}
	for _, w := range list {
		got[w.ItemID] = bazarr.Describe(w.Missing)
	}
	if got[f.heat] != "French, English (forced)" || got[f.ep1] != "German" {
		t.Fatalf("wanted: %v", got)
	}
}
