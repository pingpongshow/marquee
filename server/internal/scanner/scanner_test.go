package scanner

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"marquee/internal/db"
	"marquee/internal/library"
	"marquee/internal/probe"
)

// fakeProber returns a fixed video result, or music tags derived from the file name
// ("Artist|Album|NN|Title.flac").
type fakeProber struct{ video *probe.Result }

func (f fakeProber) Probe(_ context.Context, p string) (*probe.Result, error) {
	if ext(p) == "flac" {
		parts := strings.Split(strings.TrimSuffix(filepath.Base(p), ".flac"), "|")
		return &probe.Result{Container: "flac", DurationMS: 200_000, Tags: map[string]string{
			"album_artist": parts[0], "artist": parts[0], "album": parts[1], "track": parts[2], "title": parts[3],
		}, Streams: []probe.Stream{{Kind: "audio", Codec: "flac"}}}, nil
	}
	return f.video, nil
}

type env struct {
	t    *testing.T
	db   *sql.DB
	root string
	sc   *Scanner
}

func newEnv(t *testing.T) *env {
	dir := t.TempDir()
	d, err := db.Open(context.Background(), filepath.Join(dir, "t.db"), filepath.Join(dir, "b"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	raw, _ := os.ReadFile("../probe/testdata/episode.json")
	v, err := probe.Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	return &env{t: t, db: d, root: filepath.Join(dir, "media"), sc: &Scanner{DB: d, Prober: fakeProber{v}, Workers: 3}}
}

func (e *env) touch(rel string) {
	p := filepath.Join(e.root, rel)
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(rel), 0o644); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) lib(name string, typ library.Type, sub string) library.Library {
	path := filepath.Join(e.root, sub)
	os.MkdirAll(path, 0o755)
	l, err := library.NewStore(e.db).Create(context.Background(), name, typ, []string{path}, library.Options{})
	if err != nil {
		e.t.Fatal(err)
	}
	return l
}

func (e *env) scan(l library.Library) Stats {
	st, err := e.sc.Scan(context.Background(), l, []string{"*_staging.*", "*.part"}, nil)
	if err != nil {
		e.t.Fatal(err)
	}
	return st
}

func (e *env) count(q string, args ...any) int {
	var n int
	if err := e.db.QueryRow(q, args...).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

func TestScanShows(t *testing.T) {
	e := newEnv(t)
	e.touch("tv/Archer (2009)/Archer (2009) - S01E01 - Mole Hunt Bluray-1080p.mkv")
	e.touch("tv/Archer (2009)/Archer (2009) - S01E02 - Training Day Bluray-1080p.mkv")
	e.touch("tv/Archer (2009)/Archer (2009) - S00E04 - Heart of Archness (1).mkv")
	e.touch("tv/Archer (2009)/Archer (2009) - S01E01 - Mole Hunt Bluray-1080p.en.srt")
	e.touch("tv/Archer (2009)/poster.jpg")
	e.touch("tv/Pachinko/Pachinko.S02E02.1080p.HEVC.x265-MeGusta.mkv")
	e.touch("tv/Pachinko/Pachinko.S02E02.720p.WEB.x265-MiNX.mkv")
	e.touch("tv/Twisted Metal/Twisted.Metal.S01E01.WEB/Twisted Metal_S01E01_WLUDRV.mkv")
	e.touch("tv/Show/Season 02/Show - S02E05.mkv")
	e.touch("tv/Show/Featurettes/Making Of.mkv")
	e.touch("tv/Show/Show - S01E01.mkv.part")
	l := e.lib("TV", library.Shows, "tv")

	st := e.scan(l)
	if st.Added != 8 || st.Skipped != 0 || st.Failed != 0 {
		t.Fatalf("stats: %s", st)
	}
	// The featurette is an extra of its show (LIB-8), not an episode.
	if n := e.count(`SELECT COUNT(*) FROM items x JOIN items s ON s.id = x.parent_id
		WHERE x.extra_type = 'featurette' AND x.title = 'Making Of' AND s.type = 'show' AND s.title = 'Show'`); n != 1 {
		t.Errorf("featurette linked to its show = %d", n)
	}
	if n := e.count(`SELECT COUNT(*) FROM items WHERE type = 'show'`); n != 4 {
		t.Errorf("shows = %d", n)
	}
	if n := e.count(`SELECT leaf_count FROM items WHERE type = 'show' AND title = 'Archer'`); n != 3 {
		t.Errorf("archer episodes = %d", n)
	}
	if n := e.count(`SELECT COUNT(*) FROM items WHERE type = 'season' AND title = 'Specials'`); n != 1 {
		t.Errorf("specials = %d", n)
	}
	// Two releases of the same episode become two versions of one item.
	if n := e.count(`SELECT COUNT(*) FROM media_versions v JOIN items i ON i.id = v.item_id WHERE i.type = 'episode' AND i.idx = 2 AND i.grandparent_id = (SELECT id FROM items WHERE title = 'Pachinko')`); n != 2 {
		t.Errorf("pachinko versions = %d", n)
	}
	if n := e.count(`SELECT COUNT(*) FROM streams WHERE external_path LIKE '%.en.srt' AND language = 'eng'`); n != 1 {
		t.Errorf("external subs = %d", n)
	}
	if n := e.count(`SELECT COUNT(*) FROM artwork WHERE kind = 'poster' AND source = 'local' AND selected = 1`); n != 1 {
		t.Errorf("posters = %d", n)
	}
	if n := e.count(`SELECT COUNT(*) FROM markers WHERE kind = 'chapter'`); n != 8*5 {
		t.Errorf("chapters = %d", n)
	}

	// Rescan with no changes does nothing.
	if st := e.scan(l); st.Unchanged != 8 || st.Added+st.Updated != 0 {
		t.Fatalf("rescan: %s", st)
	}

	// Watch state survives renaming the show folder.
	var epID int64
	e.db.QueryRow(`SELECT i.id FROM items i JOIN media_versions v ON v.item_id = i.id JOIN media_files f ON f.version_id = v.id WHERE f.path LIKE '%S01E02%'`).Scan(&epID)
	e.db.Exec(`INSERT INTO users(id, username, display_name) VALUES (1, 'u', 'u')`)
	e.db.Exec(`INSERT INTO user_item_state(user_id, item_id, view_offset_ms) VALUES (1, ?, 5000)`, epID)
	if err := os.Rename(filepath.Join(e.root, "tv/Archer (2009)"), filepath.Join(e.root, "tv/Archer")); err != nil {
		t.Fatal(err)
	}
	st = e.scan(l)
	if st.Moved != 3 || st.Missing != 0 {
		t.Fatalf("after rename: %s", st)
	}
	if n := e.count(`SELECT view_offset_ms FROM user_item_state WHERE item_id = ?`, epID); n != 5000 {
		t.Errorf("watch state lost after rename")
	}

	// Deleting a file marks it unavailable rather than removing the item.
	os.Remove(filepath.Join(e.root, "tv/Show/Season 02/Show - S02E05.mkv"))
	if st := e.scan(l); st.Missing != 1 {
		t.Fatalf("after delete: %s", st)
	}
	if n := e.count(`SELECT available FROM items WHERE type = 'show' AND title = 'Show'`); n != 0 {
		t.Error("show with only missing files should be unavailable")
	}
	// It comes back when the file returns.
	e.touch("tv/Show/Season 02/Show - S02E05.mkv")
	if st := e.scan(l); st.Updated+st.Restored+st.Moved == 0 {
		t.Fatalf("after restore: %s", st)
	}
	if n := e.count(`SELECT available FROM items WHERE type = 'show' AND title = 'Show'`); n != 1 {
		t.Error("show should be available again")
	}
}

func TestScanMoviesAndOfflineRoot(t *testing.T) {
	e := newEnv(t)
	e.touch("movies/A Quiet Place (2018)/A Quiet Place (2018) Bluray-1080p.mkv")
	e.touch("movies/A Quiet Place (2018)/fanart.jpg")
	e.touch("movies/Studio Ghibli Film Collection/[AnimeRG] Kiki's Delivery Service (1989) [1080p].mkv")
	e.touch("movies/Studio Ghibli Film Collection/[AnimeRG] The Red Turtle (2016) [1080p].mkv")
	e.touch("movies/The Matrix (1999)/The Matrix (1999) {edition-Director's Cut}.mkv")
	e.touch("movies/The Matrix (1999)/The Matrix (1999).mkv")
	e.touch("movies/The Matrix (1999)/Featurettes/Making of.mkv")
	e.touch("movies/Long Movie (1980)/Long Movie (1980) cd1.avi")
	e.touch("movies/Long Movie (1980)/Long Movie (1980) cd2.avi")
	l := e.lib("Movies", library.Movies, "movies")

	st := e.scan(l)
	if st.Added != 8 || st.Skipped != 0 {
		t.Fatalf("stats: %s", st)
	}
	if n := e.count(`SELECT COUNT(*) FROM items x JOIN items m ON m.id = x.parent_id
		WHERE x.extra_type = 'featurette' AND m.title = 'The Matrix'`); n != 1 {
		t.Errorf("featurette linked to The Matrix = %d", n)
	}
	if n := e.count(`SELECT COUNT(*) FROM items WHERE type = 'movie'`); n != 5 {
		t.Errorf("movies = %d", n)
	}
	if n := e.count(`SELECT COUNT(*) FROM media_versions v JOIN items i ON i.id = v.item_id WHERE i.title = 'The Matrix'`); n != 2 {
		t.Errorf("matrix versions = %d", n)
	}
	if n := e.count(`SELECT COUNT(*) FROM media_versions v JOIN items i ON i.id = v.item_id WHERE i.title = 'Long Movie'`); n != 1 {
		t.Errorf("multi-part should share one version, got %d", n)
	}
	if n := e.count(`SELECT COUNT(*) FROM items WHERE title = 'The Red Turtle' AND year = 2016`); n != 1 {
		t.Error("collection folder movie not parsed from file name")
	}

	// Library folder goes offline: nothing is marked missing.
	os.Rename(filepath.Join(e.root, "movies"), filepath.Join(e.root, "movies-offline"))
	if _, err := e.sc.Scan(context.Background(), l, nil, nil); err == nil {
		t.Fatal("expected error when every library folder is unavailable")
	}
	if n := e.count(`SELECT COUNT(*) FROM media_files WHERE available = 0`); n != 0 {
		t.Errorf("offline root marked %d files missing", n)
	}
}

func TestScanMusic(t *testing.T) {
	e := newEnv(t)
	e.touch("music/Weezer/Blue/Weezer|Blue|1|My Name Is Jonas.flac")
	e.touch("music/Weezer/Blue/Weezer|Blue|2|No One Else.flac")
	e.touch("music/Weezer/Blue/cover.jpg")
	e.touch("music/Weezer/Blue/Weezer|Blue|1|My Name Is Jonas.lrc")
	e.touch("music/&ME,Rampa/Send Return/&ME,Rampa|Send Return|1|Before The Flood.flac")
	e.touch("music/AC_DC/Back In Black/AC/DC|Back In Black|1|Hells Bells_staging.flac")
	l := e.lib("Music", library.Music, "music")

	st := e.scan(l)
	if st.Added != 3 {
		t.Fatalf("stats: %s", st)
	}
	if n := e.count(`SELECT COUNT(*) FROM items WHERE type = 'artist'`); n != 2 {
		t.Errorf("artists = %d", n)
	}
	if n := e.count(`SELECT COUNT(*) FROM items WHERE type = 'artist' AND title = '&ME'`); n != 1 {
		t.Error("collaboration should be filed under its first artist")
	}
	if n := e.count(`SELECT child_count FROM items WHERE type = 'album' AND title = 'Blue'`); n != 2 {
		t.Errorf("album tracks = %d", n)
	}
	if n := e.count(`SELECT COUNT(*) FROM sidecars WHERE kind = 'lyrics'`); n != 1 {
		t.Errorf("lyrics = %d", n)
	}
	if n := e.count(`SELECT COUNT(*) FROM artwork WHERE kind = 'poster' AND source = 'local'`); n != 1 {
		t.Errorf("album art = %d", n)
	}
}

// Files deleted to the media trash (ADM-11) are never scanned back in, even with no ignore
// patterns at all.
func TestScanIgnoresMediaTrash(t *testing.T) {
	e := newEnv(t)
	e.touch("movies/Heat (1995)/Heat (1995).mkv")
	e.touch("movies/" + library.TrashDir + "/2026-10-01/Heat copy/Heat (1995).mkv")
	l := e.lib("Movies", library.Movies, "movies")
	st, err := e.sc.Scan(context.Background(), l, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if st.Found != 1 || st.Added != 1 {
		t.Fatalf("stats: %s", st)
	}
	if n := e.count(`SELECT COUNT(*) FROM media_files WHERE path LIKE '%' || ? || '%'`, library.TrashDir); n != 0 {
		t.Errorf("trashed files scanned: %d", n)
	}
}
