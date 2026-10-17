package vodsave

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"marquee/internal/library"
	"marquee/internal/mediatrash"
	"marquee/internal/probe"
	"marquee/internal/scanner"
	"marquee/internal/vod"
)

func TestCompare(t *testing.T) {
	q := func(w, h int, codec, hdr string, kbps int) Quality {
		return Quality{Width: w, Height: h, Codec: codec, HDR: hdr, BitrateKbps: kbps}
	}
	for _, c := range []struct {
		name     string
		vod, lib Quality
		want     string
	}{
		{"higher tier", q(1920, 1080, "h264", "", 4000), q(1280, 720, "hevc", "", 9000), VerdictBetter},
		{"lower tier", q(1280, 720, "hevc", "hdr10", 20000), q(1920, 800, "h264", "", 3000), VerdictWorse}, // letterboxed is 1080p
		{"4k beats 1080p", q(3840, 1600, "hevc", "", 15000), q(1920, 1080, "h264", "", 30000), VerdictBetter},
		{"hdr at the same tier", q(3840, 2160, "hevc", "hdr10", 15000), q(3840, 2160, "hevc", "", 40000), VerdictBetter},
		{"sdr vs library hdr", q(3840, 2160, "hevc", "", 40000), q(3840, 2160, "hevc", "dolby_vision", 15000), VerdictWorse},
		{"much higher bitrate", q(1920, 1080, "h264", "", 12000), q(1920, 1080, "h264", "", 5000), VerdictBetter},
		{"slightly higher bitrate", q(1920, 1080, "h264", "", 6000), q(1920, 1080, "h264", "", 5000), VerdictSame},
		{"hevc counts more", q(1920, 1080, "hevc", "", 6000), q(1920, 1080, "h264", "", 6000), VerdictBetter}, // 9000 vs 6000: 1.5×
		{"hevc a little less", q(1920, 1080, "hevc", "", 5000), q(1920, 1080, "h264", "", 6000), VerdictSame}, // 7500 vs 6000
		{"hevc at the same bitrate", q(1920, 1080, "hevc", "", 7000), q(1920, 1080, "h264", "", 6000), VerdictBetter},
		{"h264 vs hevc", q(1920, 1080, "h264", "", 8000), q(1920, 1080, "hevc", "", 6000), VerdictSame},        // 8000 vs 9000
		{"library much higher", q(1920, 1080, "h264", "", 3000), q(1920, 1080, "av1", "", 6000), VerdictWorse}, // 3000 vs 9000
		{"bitrate unknown", q(1920, 1080, "h264", "", 0), q(1920, 1080, "h264", "", 5000), VerdictSame},
		{"vod unknown", Quality{}, q(1920, 1080, "h264", "", 5000), VerdictSame},
		{"library unknown", q(1920, 1080, "h264", "", 5000), Quality{}, VerdictSame},
	} {
		got, reason := Compare(c.vod, c.lib)
		if got != c.want || reason == "" {
			t.Errorf("%s: %s (%s), want %s", c.name, got, reason, c.want)
		}
	}
	if s := q(1920, 1080, "hevc", "hdr10", 8000).Summary(); s != "1080p HEVC HDR10 8.0 Mb/s" {
		t.Errorf("summary %q", s)
	}
	if s := q(1280, 720, "h264", "", 0).Summary(); s != "720p H.264" {
		t.Errorf("summary %q", s)
	}
}

// libFile adds a library item with one file of a quality; for an episode, show is the
// show's item and se/ep its numbers.
func (e *env) libFile(lib int64, typ, title string, year, tmdb int, path string, w, h int, codec string, kbps int) int64 {
	e.t.Helper()
	d := e.s.DB
	exec := func(q string, args ...any) int64 {
		e.t.Helper()
		r, err := d.Exec(q, args...)
		if err != nil {
			e.t.Fatal(err)
		}
		id, _ := r.LastInsertId()
		return id
	}
	item := exec(`INSERT INTO items(library_id, type, title, sort_title, year) VALUES(?,?,?,?,?)`, lib, typ, title, title, year)
	if tmdb > 0 {
		exec(`INSERT INTO external_ids(item_id, provider, value) VALUES(?, 'tmdb', ?)`, item, fmt.Sprint(tmdb))
	}
	v := exec(`INSERT INTO media_versions(item_id) VALUES(?)`, item)
	exec(`INSERT INTO media_files(version_id, library_id, path, size, mtime, width, height, video_codec, bitrate_kbps)
		VALUES(?,?,?,1,1,?,?,?,?)`, v, lib, path, w, h, codec, kbps)
	return item
}

func (e *env) libID(name string) int64 {
	var id int64
	e.s.DB.QueryRow("SELECT id FROM libraries WHERE name=?", name).Scan(&id)
	return id
}

// Films are found in the library by TMDB id, else by title and year (not one with another
// TMDB id); the comparison drives what may be saved: the same or worse is refused, better
// only as an upgrade.
func TestMatchAndSaveRules(t *testing.T) {
	films := `[{"name":"The Matrix (1999)","stream_id":"7","container_extension":"mkv","tmdb":"603"},
		{"name":"EN - Heat (1995)","stream_id":"8","container_extension":"mkv"},
		{"name":"Heat (1986)","stream_id":"9","container_extension":"mkv"},
		{"name":"Alien (1979)","stream_id":"10","container_extension":"mkv","tmdb":"348"},
		{"name":"Brazil (1985)","stream_id":"11","container_extension":"mkv"}]`
	e := newEnv(t, films, nil)
	ctx := context.Background()
	movies := e.libID("Movies")
	matrix := e.movieID("The Matrix (1999)")
	e.s.DB.Exec("UPDATE vod_movies SET match_state='matched' WHERE id=?", matrix)
	alien := e.movieID("Alien (1979)")
	e.s.DB.Exec("UPDATE vod_movies SET match_state='matched' WHERE id=?", alien)
	// The library: The Matrix by TMDB id under another title, Heat (1995) by name only,
	// Alien with another TMDB id under the same name.
	e.libFile(movies, "movie", "Matrix, The", 1999, 603, filepath.Join(e.movies, "M", "m.mkv"), 1920, 1080, "hevc", 8000)
	e.libFile(movies, "movie", "Heat", 1995, 0, filepath.Join(e.movies, "Heat (1995)", "Heat.mkv"), 1280, 720, "h264", 4000)
	e.libFile(movies, "movie", "Alien", 1979, 999, filepath.Join(e.movies, "A", "a.mkv"), 1920, 1080, "h264", 8000)
	store := func(id int64, w, h int, codec string, kbps int) {
		if err := e.vod.StoreMovieQuality(ctx, id, vod.Quality{Width: w, Height: h, VideoCodec: codec, BitrateKbps: kbps, Origin: "info"}); err != nil {
			t.Fatal(err)
		}
	}
	store(matrix, 1280, 720, "h264", 3000)
	store(e.movieID("EN - Heat (1995)"), 1920, 1080, "h264", 6000)
	store(e.movieID("Heat (1986)"), 1920, 1080, "h264", 6000)
	store(alien, 1920, 1080, "h264", 8000)

	check := func(title, want string) Match {
		t.Helper()
		m, err := e.s.MovieMatch(ctx, e.movieID(title))
		if err != nil || m.Verdict != want {
			t.Fatalf("%s: %+v %v, want %s", title, m, err, want)
		}
		return m
	}
	if m := check("The Matrix (1999)", VerdictWorse); m.Library == nil || m.Library.Quality.Summary() != "1080p HEVC 8.0 Mb/s" {
		t.Errorf("matrix: %+v", m.Library)
	}
	check("EN - Heat (1995)", VerdictBetter)
	check("Heat (1986)", VerdictNone)   // another year
	check("Alien (1979)", VerdictNone)  // the library's Alien is another film
	check("Brazil (1985)", VerdictNone) // not there; nothing to check
	// Nothing in the library: no probe was asked for.
	if e.s.qualityPending(qualityKey{false, e.movieID("Brazil (1985)")}) {
		t.Error("probing a film the library doesn't have")
	}

	// Worse: refused.
	var inLib *InLibraryError
	if _, err := e.s.SaveMovie(ctx, matrix, 1, true); !errors.As(err, &inLib) || inLib.Match.Verdict != VerdictWorse {
		t.Fatalf("worse: %v", err)
	}
	// Better: refused unless as an upgrade, which goes into the library copy's folder.
	heat := e.movieID("EN - Heat (1995)")
	if _, err := e.s.SaveMovie(ctx, heat, 1, false); !errors.As(err, &inLib) || inLib.Match.Verdict != VerdictBetter {
		t.Fatalf("better without upgrade: %v", err)
	}
	r, err := e.s.SaveMovie(ctx, heat, 1, true)
	if err != nil || r.Queued != 1 || r.Upgrades != 1 {
		t.Fatalf("upgrade: %+v %v", r, err)
	}
	if v := r.Saves[0]; v.ReplaceFileID == 0 || v.TargetPath != filepath.Join(e.movies, "Heat (1995)", "Heat (1995).mkv") {
		t.Errorf("upgrade queued: %+v", v)
	}
	// Not in the library: saved as usual.
	if r, err := e.s.SaveMovie(ctx, e.movieID("Heat (1986)"), 1, false); err != nil || r.Queued != 1 || r.Upgrades != 0 {
		t.Errorf("new: %+v %v", r, err)
	}
	e.noSecrets()
}

// An episode's quality comes from the series' details (info.video) or else a probe; a
// season's save queues only what the library lacks, and upgrades when asked; the
// episodes' library copies are found by the show's TMDB id, season and episode.
func TestEpisodeMatchesAndSeasonSave(t *testing.T) {
	e := newEnv(t, "[]", nil)
	e.src.series = `[{"name":"Kings (2011)","series_id":"201","category_id":"1","tmdb":"77"}]`
	e.src.info["201"] = `{"seasons":[],"episodes":{"1":[
		{"id":"3001","episode_num":"1","title":"One","container_extension":"mkv","season":"1",
		 "info":{"video":{"codec_name":"hevc","width":1920,"height":1080,"color_transfer":"smpte2084"},"bitrate":"9000"}},
		{"id":"3002","episode_num":"2","title":"Two","container_extension":"mkv","season":"1",
		 "info":{"video":{"codec_name":"h264","width":1280,"height":720},"bitrate":"3000"}},
		{"id":"3003","episode_num":"3","title":"Three","container_extension":"mkv","season":"1"},
		{"id":"3004","episode_num":"4","title":"Four","container_extension":"mkv","season":"1"}]}}`
	ctx := context.Background()
	e.vod.Refresh(ctx, false)
	var seriesID int64
	e.s.DB.QueryRow("SELECT id FROM vod_series").Scan(&seriesID)
	e.s.DB.Exec("UPDATE vod_series SET match_state='matched', tmdb_id=77 WHERE id=?", seriesID)
	if err := e.vod.LoadEpisodes(ctx, seriesID, false); err != nil {
		t.Fatal(err)
	}
	probed := 0
	e.s.ProbeRemote = func(ctx context.Context, link, ua string) (vod.Quality, error) {
		probed++
		if !strings.Contains(link, "/series/") {
			t.Errorf("probe link %q", link)
		}
		return vod.Quality{Width: 1920, Height: 1080, VideoCodec: "h264", BitrateKbps: 5000}, nil
	}
	e.s.ProbeGap = 1
	shows := e.libID("Shows")
	d := e.s.DB
	r, _ := d.Exec(`INSERT INTO items(library_id, type, title, sort_title, year) VALUES(?, 'show', 'Kings', 'Kings', 2011)`, shows)
	show, _ := r.LastInsertId()
	d.Exec(`INSERT INTO external_ids(item_id, provider, value) VALUES(?, 'tmdb', '77')`, show)
	r, _ = d.Exec(`INSERT INTO items(library_id, type, title, sort_title, parent_id, idx) VALUES(?, 'season', 'Season 1', 'Season 1', ?, 1)`, shows, show)
	season, _ := r.LastInsertId()
	ep := func(n, w, h int, codec string, kbps int) {
		r, _ := d.Exec(`INSERT INTO items(library_id, type, title, sort_title, parent_id, grandparent_id, idx) VALUES(?, 'episode', ?, ?, ?, ?, ?)`,
			shows, fmt.Sprint("E", n), fmt.Sprint(n), season, show, n)
		id, _ := r.LastInsertId()
		r, _ = d.Exec(`INSERT INTO media_versions(item_id) VALUES(?)`, id)
		v, _ := r.LastInsertId()
		d.Exec(`INSERT INTO media_files(version_id, library_id, path, size, mtime, width, height, video_codec, bitrate_kbps)
			VALUES(?,?,?,1,1,?,?,?,?)`, v, shows, filepath.Join(e.shows, "Kings (2011)", "Season 1", fmt.Sprintf("Kings - S01E%02d.mkv", n)), w, h, codec, kbps)
	}
	ep(1, 1920, 1080, "h264", 8000) // the source's is HDR: better
	ep(2, 1920, 1080, "h264", 8000) // the source's is 720p: worse
	ep(3, 1920, 1080, "h264", 5000) // probed: the same
	// Episode 4 isn't in the library.

	matches, err := e.s.EpisodeMatches(ctx, seriesID)
	if err != nil {
		t.Fatal(err)
	}
	byNum := map[int]Match{}
	rows, _ := d.Query("SELECT id, episode FROM vod_episodes WHERE series_id=?", seriesID)
	for rows.Next() {
		var id int64
		var n int
		rows.Scan(&id, &n)
		byNum[n] = matches[id]
	}
	rows.Close()
	if byNum[1].Verdict != VerdictBetter || byNum[2].Verdict != VerdictWorse || byNum[4].Verdict != VerdictNone {
		t.Fatalf("matches: %+v", byNum)
	}
	if m := byNum[3]; m.Verdict != VerdictSame || !m.Pending {
		t.Fatalf("episode 3 isn't waiting for a probe: %+v", m)
	}
	var roll Rollup
	for _, m := range byNum {
		roll.Add(m)
	}
	if roll != (Rollup{None: 1, Better: 1, Same: 1, Worse: 1, Pending: 1}) {
		t.Errorf("rollup: %+v", roll)
	}
	// The background check probes it (once), and then it's known.
	e.start()
	e.waitFor("the probe", func() bool { return !e.s.qualityPending(qualityKey{true, idOf(t, e, "3003")}) })
	matches, _ = e.s.EpisodeMatches(ctx, seriesID)
	if m := matches[idOf(t, e, "3003")]; m.Verdict != VerdictSame || m.Pending || !m.VODKnown || probed != 1 {
		t.Fatalf("after the probe: %+v (%d probes)", m, probed)
	}
	e.s.SetQueuePaused(ctx, true)
	one := 1
	r2, err := e.s.SaveSeries(ctx, seriesID, &one, 1, false)
	if err != nil || r2.Queued != 1 || r2.Skipped != 3 || r2.Upgrades != 0 {
		t.Fatalf("season: %+v %v", r2, err)
	}
	r2, err = e.s.SaveSeries(ctx, seriesID, &one, 1, true)
	if err != nil || r2.Queued != 1 || r2.Already != 1 || r2.Skipped != 2 || r2.Upgrades != 1 {
		t.Fatalf("season with upgrades: %+v %v", r2, err)
	}
	// One episode the library has as well: refused.
	var inLib *InLibraryError
	if _, err := e.s.SaveEpisode(ctx, idOf(t, e, "3002"), 1, true); !errors.As(err, &inLib) {
		t.Errorf("worse episode: %v", err)
	}
}

func idOf(t *testing.T, e *env, key string) int64 {
	t.Helper()
	var id int64
	if err := e.s.DB.QueryRow("SELECT id FROM vod_episodes WHERE source_key=?", key).Scan(&id); err != nil {
		t.Fatal(err)
	}
	return id
}

// An upgrade moves the library's file and its sidecars to the media trash, puts the new
// file in the same folder, and keeps the item: after the scan it has the same id, its
// watch state, and the new file's quality.
func TestUpgradeReplacesTheLibraryCopy(t *testing.T) {
	data := clip(t, "mkv")
	e := newEnv(t, oneFilm, nil)
	e.src.files["movie/101.mkv"] = data
	e.src.info["vod:101"] = `{"info":{"video":{"codec_name":"h264","width":1280,"height":720},"bitrate":"2000"}}`
	ctx := context.Background()
	e.s.Trash = &mediatrash.Service{DB: e.s.DB}

	// The library has a 360p copy (480p tier) with subtitles and an .nfo, scanned for real.
	dir := filepath.Join(e.movies, "Clip Film (2020)")
	os.MkdirAll(dir, 0o755)
	old := filepath.Join(dir, "Clip Film (2020) SD.mkv")
	if out, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=640x360:rate=24:duration=3",
		"-c:v", "libx264", "-preset", "ultrafast", old).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	os.WriteFile(filepath.Join(dir, "Clip Film (2020) SD.en.srt"), []byte("1\n00:00:01,000 --> 00:00:02,000\nHi\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "Clip Film (2020) SD.nfo"), []byte("<movie/>"), 0o644)
	libs := library.NewStore(e.s.DB)
	lib, _ := libs.Get(ctx, e.libID("Movies"))
	sc := &scanner.Scanner{DB: e.s.DB, Prober: probe.New("ffprobe", 2)}
	if _, err := sc.Scan(ctx, lib, nil, nil); err != nil {
		t.Fatal(err)
	}
	var item, fileID int64
	if err := e.s.DB.QueryRow(`SELECT i.id, f.id FROM items i JOIN media_versions v ON v.item_id=i.id JOIN media_files f ON f.version_id=v.id
		WHERE i.type='movie'`).Scan(&item, &fileID); err != nil {
		t.Fatal(err)
	}
	e.s.DB.Exec(`INSERT INTO user_item_state(user_id, item_id, play_count, view_offset_ms, last_viewed_at, rating) VALUES(1, ?, 2, 0, '2026-01-01T00:00:00.000Z', 8)`, item)

	movie := e.movieID("EN - Clip Film (2020)")
	// Opening the film asks for its quality to be checked (in the background).
	if m, err := e.s.MovieMatch(ctx, movie); err != nil || !m.Pending || m.Library == nil || m.Library.Quality.Tier() != 480 {
		t.Fatalf("match: %+v %v", m, err)
	}
	r, err := e.s.SaveMovie(ctx, movie, 1, true) // checks the quality now: the source's details
	if err != nil || r.Upgrades != 1 {
		t.Fatalf("save: %+v %v", r, err)
	}
	e.start()
	v := e.wait(r.Saves[0].ID, Done, Failed)
	want := filepath.Join(dir, "Clip Film (2020) WEBDL-720p.mkv")
	if v.State != Done || v.TargetPath != want || v.ReplacedPath != old {
		t.Fatalf("done: %+v", v)
	}
	for _, p := range []string{old, filepath.Join(dir, "Clip Film (2020) SD.en.srt"), filepath.Join(dir, "Clip Film (2020) SD.nfo")} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s is still there", filepath.Base(p))
		}
	}
	trashed, _ := filepath.Glob(filepath.Join(e.movies, library.TrashDir, "*", "Clip Film (2020)", "*"))
	if len(trashed) != 3 {
		t.Errorf("trash: %v", trashed)
	}
	e.waitFor("the scan", func() bool { e.scanMu.Lock(); defer e.scanMu.Unlock(); return len(e.scans) == 1 })
	if _, err := sc.Scan(ctx, lib, nil, nil); err != nil {
		t.Fatal(err)
	}
	var items, files, w int
	var plays int
	var path string
	e.s.DB.QueryRow("SELECT COUNT(*) FROM items WHERE type='movie'").Scan(&items)
	e.s.DB.QueryRow("SELECT COUNT(*) FROM media_files").Scan(&files)
	e.s.DB.QueryRow("SELECT path, width FROM media_files WHERE id=?", fileID).Scan(&path, &w)
	e.s.DB.QueryRow("SELECT play_count FROM user_item_state WHERE item_id=?", item).Scan(&plays)
	if items != 1 || files != 1 || path != want || w != 1280 || plays != 2 {
		t.Fatalf("after the scan: %d items, %d files, %s %dpx, %d plays", items, files, path, w, plays)
	}
	// Now the library's copy is as good: saving again is refused.
	var inLib *InLibraryError
	if _, err := e.s.SaveMovie(ctx, movie, 1, true); !errors.As(err, &inLib) || inLib.Match.Verdict == VerdictBetter {
		t.Errorf("again: %v", err)
	}
	e.noSecrets()
}

// An upgrade is chosen by what the source says its file is; when the file saved turns out
// worse than the library's copy (here the source claims 4K for a 720p file, and the
// library has 1080p), the library's copy stays where it is and the save fails.
func TestUpgradeKeepsABetterLibraryCopy(t *testing.T) {
	data := clip(t, "mkv") // 720p
	e := newEnv(t, oneFilm, nil)
	e.src.files["movie/101.mkv"] = data
	e.src.info["vod:101"] = `{"info":{"video":{"codec_name":"hevc","width":3840,"height":2160},"bitrate":"20000"}}`
	ctx := context.Background()
	e.s.Trash = &mediatrash.Service{DB: e.s.DB}

	dir := filepath.Join(e.movies, "Clip Film (2020)")
	os.MkdirAll(dir, 0o755)
	old := filepath.Join(dir, "Clip Film (2020) Bluray-1080p.mkv")
	if out, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=1920x1080:rate=24:duration=2",
		"-c:v", "libx264", "-preset", "ultrafast", old).CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	libs := library.NewStore(e.s.DB)
	lib, _ := libs.Get(ctx, e.libID("Movies"))
	sc := &scanner.Scanner{DB: e.s.DB, Prober: probe.New("ffprobe", 2)}
	if _, err := sc.Scan(ctx, lib, nil, nil); err != nil {
		t.Fatal(err)
	}
	r, err := e.s.SaveMovie(ctx, e.movieID("EN - Clip Film (2020)"), 1, true)
	if err != nil || r.Upgrades != 1 {
		t.Fatalf("save: %+v %v", r, err)
	}
	e.start()
	v := e.wait(r.Saves[0].ID, Done, Failed)
	if v.State != Failed || !strings.Contains(v.Error, "library's copy") || v.ReplacedPath != "" {
		t.Fatalf("saved: %+v", v)
	}
	if _, err := os.Stat(old); err != nil {
		t.Errorf("the library's copy: %v", err)
	}
	if trashed, _ := filepath.Glob(filepath.Join(e.movies, library.TrashDir, "*", "*", "*")); len(trashed) != 0 {
		t.Errorf("trash: %v", trashed)
	}
	var n int
	e.s.DB.QueryRow("SELECT COUNT(*) FROM media_files WHERE path=? AND available=1", old).Scan(&n)
	if n != 1 {
		t.Errorf("the library's record: %d", n)
	}
}
