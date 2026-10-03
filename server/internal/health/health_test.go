package health

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"marquee/internal/bazarr"
	"marquee/internal/bazarr/bazarrtest"
	"marquee/internal/db"
)

type env struct {
	t   *testing.T
	db  *sql.DB
	lib int64
}

func (e *env) exec(q string, args ...any) int64 {
	e.t.Helper()
	r, err := e.db.Exec(q, args...)
	if err != nil {
		e.t.Fatalf("%s: %v", q, err)
	}
	id, _ := r.LastInsertId()
	return id
}

// movie adds a matched movie with a poster.
func (e *env) movie(title string) int64 {
	id := e.exec(`INSERT INTO items(library_id, type, title, sort_title, match_state) VALUES (?, 'movie', ?, ?, 'matched')`, e.lib, title, title)
	e.exec(`INSERT INTO artwork(item_id, kind, source, selected) VALUES (?, 'poster', 'tmdb', 1)`, id)
	return id
}

// file adds a readable file: width×height, kbps, codec, with video and audio streams.
func (e *env) file(item int64, path string, w, h, kbps int, codec string) int64 {
	v := e.exec(`INSERT INTO media_versions(item_id) VALUES (?)`, item)
	f := e.exec(`INSERT INTO media_files(version_id, library_id, path, size, mtime, duration_ms, width, height, bitrate_kbps, video_codec)
		VALUES (?, ?, ?, 1, 1, 6000000, ?, ?, ?, ?)`, v, e.lib, path, w, h, kbps, codec)
	e.exec(`INSERT INTO streams(file_id, kind, codec) VALUES (?, 'video', ?), (?, 'audio', 'aac')`, f, codec, f)
	return f
}

func newEnv(t *testing.T) *env {
	dir := t.TempDir()
	d, err := db.Open(context.Background(), filepath.Join(dir, "t.db"), filepath.Join(dir, "b"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	e := &env{t: t, db: d}
	e.lib = e.exec(`INSERT INTO libraries(name, type) VALUES ('Movies', 'movies')`)
	return e
}

func counts(t *testing.T, s *Service) map[string]Check {
	t.Helper()
	list, err := s.Checks(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]Check{}
	for _, c := range list {
		out[c.ID] = c
	}
	return out
}

func TestChecks(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	s := &Service{DB: e.db}

	// A healthy 1080p film, and a letterboxed one (1920×800 is 1080p, not 720p).
	e.file(e.movie("Fine"), "/m/Fine.mkv", 1920, 1080, 8000, "h264")
	e.file(e.movie("Scope"), "/m/Scope.mkv", 1920, 800, 6000, "h264")
	// Two copies of the same film; and one film with two 1080p files of the same edition.
	m1, m2 := e.movie("Heat"), e.movie("Heat (copy)")
	e.file(m1, "/m/Heat.mkv", 1920, 1080, 8000, "h264")
	e.file(m2, "/m/Heat2.mkv", 1920, 1080, 8000, "h264")
	e.exec(`INSERT INTO external_ids(item_id, provider, value) VALUES (?, 'tmdb', '949'), (?, 'tmdb', '949')`, m1, m2)
	twice := e.movie("Twice")
	e.file(twice, "/m/Twice.mkv", 1920, 1080, 8000, "h264")
	e.file(twice, "/m/Twice.copy.mkv", 1920, 1040, 9000, "hevc")
	// Upgrade candidates: 720p, SD, low-bitrate H.264 1080p and HEVC 4K. HEVC 1080p at
	// 2 Mbps is fine; a film with a 720p and a 4K file is judged by the 4K one.
	e.file(e.movie("HD Ready"), "/m/720.mkv", 1280, 720, 4000, "h264")
	e.file(e.movie("Old"), "/m/sd.avi", 720, 480, 1500, "mpeg4")
	e.file(e.movie("Starved"), "/m/starved.mkv", 1920, 1080, 2100, "h264")
	e.file(e.movie("Tiny 4K"), "/m/tiny4k.mkv", 3840, 2160, 4000, "hevc")
	e.file(e.movie("Efficient"), "/m/eff.mkv", 1920, 1080, 2000, "hevc")
	both := e.movie("Both")
	e.file(both, "/m/both720.mkv", 1280, 720, 4000, "h264")
	e.file(both, "/m/both4k.mkv", 3840, 2160, 40000, "hevc")
	// Unplayable: no streams; zero duration; a re-probe that failed.
	noStreams := e.movie("Silent")
	f := e.file(noStreams, "/m/silent.mkv", 1920, 1080, 8000, "h264")
	e.exec(`DELETE FROM streams WHERE file_id = ?`, f)
	f = e.file(e.movie("Zero"), "/m/zero.mkv", 1920, 1080, 8000, "h264")
	e.exec(`UPDATE media_files SET duration_ms = 0 WHERE id = ?`, f)
	e.file(e.movie("Broken"), "/m/broken.mkv", 1920, 1080, 8000, "h264")
	e.exec(`INSERT INTO probe_failures(path, library_id, size, mtime, error) VALUES ('/m/broken.mkv', ?, 1, 1, 'Invalid data found')`, e.lib)
	// Missing file.
	gone := e.movie("Gone")
	f = e.file(gone, "/m/gone.mkv", 1920, 1080, 8000, "h264")
	e.exec(`UPDATE items SET available = 0 WHERE id = ?`, gone)
	e.exec(`UPDATE media_files SET available = 0 WHERE id = ?`, f)
	// Unmatched, and a failed match, without posters.
	e.exec(`INSERT INTO items(library_id, type, title, sort_title) VALUES (?, 'movie', 'Mystery', 'Mystery')`, e.lib)
	e.exec(`INSERT INTO items(library_id, type, title, sort_title, match_state) VALUES (?, 'show', 'Odd Show', 'Odd Show', 'failed')`, e.lib)
	// Extras never count.
	extra := e.exec(`INSERT INTO items(library_id, type, title, sort_title, parent_id, extra_type) VALUES (?, 'movie', 'Trailer', 'Trailer', ?, 'trailer')`, e.lib, m1)
	e.file(extra, "/m/trailer.mp4", 640, 360, 500, "h264")
	// Playback errors: two for one film (latest message wins), one too old.
	if err := RecordPlaybackError(ctx, e.db, m1, 0, "decode failed"); err != nil {
		t.Fatal(err)
	}
	RecordPlaybackError(ctx, e.db, m1, 0, "decode failed") // a repeat within a minute counts once
	e.exec(`UPDATE playback_errors SET at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '-1 hour')`)
	RecordPlaybackError(ctx, e.db, m1, 0, "network lost")
	e.exec(`INSERT INTO playback_errors(item_id, at, message) VALUES (?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '-40 days'), 'ancient')`, m2)

	c := counts(t, s)
	want := map[string]int{"duplicates": 3, "unmatched": 2, "unavailable": 1, "unplayable": 3, "upgrades": 4,
		"playbackErrors": 1, "missingArtwork": 2, "missingSubtitles": 0}
	for id, n := range want {
		if c[id].Count != n {
			t.Errorf("%s: %d, want %d", id, c[id].Count, n)
		}
	}
	if c["missingSubtitles"].Available || !c["upgrades"].Available || c["unplayable"].Severity != "error" || c["upgrades"].Severity != "info" {
		t.Errorf("checks: %+v", c)
	}

	issues := func(id string) []Issue {
		t.Helper()
		list, total, err := s.Issues(ctx, id, 0, 50)
		if err != nil || total != len(list) {
			t.Fatalf("%s: %d %+v %v", id, total, list, err)
		}
		return list
	}
	details := func(id string) map[int64]string {
		out := map[int64]string{}
		for _, is := range issues(id) {
			out[is.ItemID] = is.Detail
		}
		return out
	}
	byTitle := func(title string) int64 {
		var id int64
		e.db.QueryRow(`SELECT id FROM items WHERE title = ?`, title).Scan(&id)
		return id
	}
	up := details("upgrades")
	for title, d := range map[string]string{"HD Ready": "720p · 4.0 Mbps", "Old": "480p · 1.5 Mbps",
		"Starved": "1080p · 2.1 Mbps (low bitrate)", "Tiny 4K": "4K · 4.0 Mbps (low bitrate)"} {
		if up[byTitle(title)] != d {
			t.Errorf("upgrade %s: %q, want %q", title, up[byTitle(title)], d)
		}
	}
	dups := issues("duplicates")
	for _, is := range dups {
		switch is.ItemID {
		case m1:
			if len(is.Related) != 1 || is.Related[0] != m2 || is.Detail != "2 copies in this library" {
				t.Errorf("heat: %+v", is)
			}
			// Both copies' files, this item's first, to compare.
			if len(is.Files) != 2 || is.Files[0].ItemID != m1 || is.Files[1].ItemID != m2 ||
				is.Files[0].ItemTitle != "Heat" || is.Files[1].ItemTitle != "Heat (copy)" || is.Files[0].AddedAt == "" {
				t.Errorf("heat files: %+v", is.Files)
			}
		case twice:
			if is.Path != "/m/Twice.copy.mkv" || is.Detail != "2 files at 1080p" {
				t.Errorf("twice: %+v", is)
			}
			if len(is.Files) != 2 || is.Files[0].ItemID != twice || is.Files[1].ItemID != twice || is.Files[0].FileID == is.Files[1].FileID {
				t.Errorf("twice files: %+v", is.Files)
			}
		}
	}
	unp := details("unplayable")
	if unp[byTitle("Silent")] != "No audio or video streams" || unp[byTitle("Zero")] != "Zero duration" || unp[byTitle("Broken")] != "Couldn't be read: Invalid data found" {
		t.Errorf("unplayable: %v", unp)
	}
	if pe := details("playbackErrors"); pe[m1] != "2 errors · network lost" {
		t.Errorf("playback errors: %v", pe)
	}
	if un := details("unmatched"); un[byTitle("Odd Show")] != "Matching failed" || un[byTitle("Mystery")] != "Not matched" {
		t.Errorf("unmatched: %v", un)
	}
	if list := issues("unavailable"); list[0].ItemID != gone || list[0].Path != "/m/gone.mkv" ||
		len(list[0].Files) != 1 || list[0].Files[0].FileID != list[0].FileID {
		t.Errorf("unavailable: %+v", list)
	}

	// Paging and ignoring.
	if list, total, _ := s.Issues(ctx, "upgrades", 1, 2); total != 4 || len(list) != 2 {
		t.Errorf("page: %d %+v", total, list)
	}
	if err := s.Ignore(ctx, "upgrades", byTitle("Old")); err != nil {
		t.Fatal(err)
	}
	s.Ignore(ctx, "upgrades", byTitle("Old")) // twice is fine
	if c := counts(t, s); c["upgrades"].Count != 3 || c["missingArtwork"].Count != 2 {
		t.Errorf("after ignore: %+v", c["upgrades"])
	}
	if _, ok := details("upgrades")[byTitle("Old")]; ok {
		t.Error("ignored item listed")
	}
	s.Unignore(ctx, "upgrades", byTitle("Old"))
	if c := counts(t, s); c["upgrades"].Count != 4 {
		t.Errorf("after unignore: %+v", c["upgrades"])
	}
	if err := s.Ignore(ctx, "nope", m1); err != ErrUnknownCheck {
		t.Errorf("unknown check: %v", err)
	}
	if err := s.Ignore(ctx, "upgrades", 99999); err != ErrNotFound {
		t.Errorf("unknown item: %v", err)
	}
	if _, _, err := s.Issues(ctx, "nope", 0, 10); err != ErrUnknownCheck {
		t.Errorf("unknown check issues: %v", err)
	}
}

func TestMissingSubtitles(t *testing.T) {
	e := newEnv(t)
	heat := e.movie("Heat")
	e.file(heat, "/media/video/Movies/Heat (1995)/Heat (1995).mkv", 1920, 1080, 8000, "h264")
	alien := e.movie("Alien")
	e.file(alien, "/media/video/Movies/Alien (1979)/Alien (1979).mkv", 1920, 1080, 8000, "h264")
	fake := (&bazarrtest.Fake{Movies: []bazarr.Movie{
		{RadarrID: 1, Path: "/movies/Heat (1995)/Heat (1995).mkv", Missing: []bazarr.Language{{Name: "English", Code2: "en"}}},
		{RadarrID: 2, Path: "/movies/Alien (1979)/Alien (1979).mkv", Missing: []bazarr.Language{{Name: "French", Code2: "fr", HI: true}}},
		{RadarrID: 3, Path: "/movies/Not Here (2000)/Not Here (2000).mkv", Missing: []bazarr.Language{{Name: "English", Code2: "en"}}},
	}}).Start(t)
	s := &Service{DB: e.db, Bazarr: &bazarr.Service{DB: e.db, Config: func() (string, string) { return fake.URL, bazarrtest.APIKey }}}
	c := counts(t, s)["missingSubtitles"]
	if !c.Available || c.Count != 2 {
		t.Fatalf("check: %+v", c)
	}
	list, total, err := s.Issues(context.Background(), "missingSubtitles", 0, 10)
	if err != nil || total != 2 {
		t.Fatalf("issues: %+v %v", list, err)
	}
	for _, is := range list {
		if is.ItemID == alien && is.Detail != "Missing French (SDH)" {
			t.Errorf("alien: %+v", is)
		}
	}
	s.Ignore(context.Background(), "missingSubtitles", alien)
	if c := counts(t, s)["missingSubtitles"]; c.Count != 1 {
		t.Errorf("after ignore: %+v", c)
	}
	// Unreachable Bazarr: the check can't run.
	fake.Fail = true
	s.Bazarr = &bazarr.Service{DB: e.db, Config: func() (string, string) { return fake.URL, bazarrtest.APIKey }}
	if c := counts(t, s)["missingSubtitles"]; c.Available {
		t.Errorf("unreachable: %+v", c)
	}
}
