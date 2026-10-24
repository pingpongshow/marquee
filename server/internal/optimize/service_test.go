package optimize

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"marquee/internal/db"
	"marquee/internal/library"
	"marquee/internal/mediatrash"
	"marquee/internal/settings"
)

type env struct {
	t     *testing.T
	db    *sql.DB
	dir   string
	lib   int64
	music int64
	cfg   settings.Settings
	mu    sync.Mutex
}

func newEnv(t *testing.T) *env {
	t.Helper()
	dir := t.TempDir()
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(dir, "t.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	media := filepath.Join(dir, "media")
	os.MkdirAll(media, 0o755)
	lib, err := library.NewStore(d).Create(ctx, "Movies", library.Movies, []string{media}, library.Options{})
	if err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Join(dir, "music"), 0o755)
	music, err := library.NewStore(d).Create(ctx, "Music", library.Music, []string{filepath.Join(dir, "music")}, library.Options{})
	if err != nil {
		t.Fatal(err)
	}
	e := &env{t: t, db: d, dir: dir, lib: lib.ID, music: music.ID, cfg: settings.Defaults()}
	e.cfg.Transcoder.NVENCSessions = 0
	return e
}

func (e *env) settings() settings.Settings {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.cfg
}

func (e *env) set(fn func(*settings.Settings)) {
	e.mu.Lock()
	fn(&e.cfg)
	e.mu.Unlock()
}

type fileSpec struct {
	lib        int64
	typ, path  string
	size       int64
	w, h, kbps int
	codec, hdr string
	available  bool
	extra      bool
	audio      []AudioInfo
}

func (e *env) add(f fileSpec) (item, file int64) {
	e.t.Helper()
	if f.lib == 0 {
		f.lib = e.lib
	}
	if f.typ == "" {
		f.typ = "movie"
	}
	if f.codec == "" {
		f.codec = "h264"
	}
	title := filepath.Base(f.path)
	r, err := e.db.Exec(`INSERT INTO items (library_id, type, title, sort_title) VALUES (?, ?, ?, ?)`, f.lib, f.typ, title, title)
	if err != nil {
		e.t.Fatal(err)
	}
	item, _ = r.LastInsertId()
	if f.extra {
		e.db.Exec(`UPDATE items SET extra_type = 'trailer' WHERE id = ?`, item)
	}
	r, _ = e.db.Exec(`INSERT INTO media_versions (item_id) VALUES (?)`, item)
	v, _ := r.LastInsertId()
	var hdr any
	if f.hdr != "" {
		hdr = f.hdr
	}
	r, err = e.db.Exec(`INSERT INTO media_files (version_id, library_id, path, size, mtime, duration_ms, bitrate_kbps, width, height, video_codec, hdr_format, available)
		VALUES (?, ?, ?, ?, 100, 7200000, ?, ?, ?, ?, ?, ?)`, v, f.lib, f.path, f.size, f.kbps, f.w, f.h, f.codec, hdr, f.available)
	if err != nil {
		e.t.Fatal(err)
	}
	file, _ = r.LastInsertId()
	for i, a := range f.audio {
		e.db.Exec(`INSERT INTO streams (file_id, stream_index, kind, codec, channels, bitrate_kbps, is_default) VALUES (?, ?, 'audio', ?, ?, ?, ?)`,
			file, i+1, a.Codec, a.Channels, a.Kbps, a.Default)
	}
	return item, file
}

func ptrBool(b bool) *bool { return &b }

// The Large files filters, sorting, paging and totals.
func TestListFilters(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	const gb = 1_000_000_000
	_, big := e.add(fileSpec{path: "/m/uhd.mkv", size: 60 * gb, w: 3840, h: 2160, kbps: 66000, codec: "hevc", hdr: "hdr10", available: true,
		audio: []AudioInfo{{Codec: "truehd", Channels: 8}}})
	_, vc1 := e.add(fileSpec{path: "/m/vc1.mkv", size: 25 * gb, w: 1920, h: 1080, kbps: 28000, codec: "vc1", available: true})
	_, mpeg2 := e.add(fileSpec{typ: "episode", path: "/m/ep.ts", size: 8 * gb, w: 1920, h: 1080, kbps: 9000, codec: "mpeg2video", available: true})
	_, small := e.add(fileSpec{path: "/m/small.mkv", size: 2 * gb, w: 1280, h: 720, kbps: 2200, available: true})
	e.add(fileSpec{path: "/m/gone.mkv", size: 40 * gb, w: 1920, h: 1080, available: false})
	e.add(fileSpec{path: "/m/trailer.mkv", size: 30 * gb, w: 1920, h: 1080, available: true, extra: true})
	e.add(fileSpec{lib: e.music, typ: "track", path: "/music/x.flac", size: 50 * gb, available: true})

	s := &Service{DB: e.db, Settings: e.settings}
	p := preset("1080p-hevc-balanced")
	list := func(f Filter) []int64 {
		pg, err := s.List(ctx, f, p, 0, 50)
		if err != nil {
			t.Fatal(err)
		}
		var ids []int64
		for _, f := range pg.Files {
			ids = append(ids, f.FileID)
		}
		return ids
	}
	eq := func(name string, got []int64, want ...int64) {
		t.Helper()
		if len(got) != len(want) {
			t.Errorf("%s: %v, want %v", name, got, want)
			return
		}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("%s: %v, want %v", name, got, want)
				return
			}
		}
	}
	eq("all, biggest first", list(Filter{}), big, vc1, mpeg2, small)
	eq("min size", list(Filter{MinSizeBytes: 10 * gb}), big, vc1)
	eq("min bitrate", list(Filter{MinBitrateKbps: 9000}), big, vc1, mpeg2)
	eq("1080p", list(Filter{Resolution: 1080}), vc1, mpeg2)
	eq("4K", list(Filter{Resolution: 2160}), big)
	eq("old codecs", list(Filter{Codecs: []string{"VC1", "mpeg2video"}}), vc1, mpeg2)
	eq("HDR", list(Filter{HDR: ptrBool(true)}), big)
	eq("SDR", list(Filter{HDR: ptrBool(false)}), vc1, mpeg2, small)
	eq("episodes", list(Filter{Type: "episode"}), mpeg2)
	eq("library", list(Filter{LibraryID: e.music}))
	eq("by bitrate", list(Filter{Sort: "bitrate"}), big, vc1, mpeg2, small)

	pg, err := s.List(ctx, Filter{}, p, 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if pg.Total != 4 || len(pg.Files) != 2 || pg.Files[0].FileID != vc1 || pg.TotalSize != 95*gb {
		t.Fatalf("page: %+v", pg)
	}
	if pg.TotalEstimate <= 0 || pg.TotalEstimate >= pg.TotalSize {
		t.Errorf("estimate total %d of %d", pg.TotalEstimate, pg.TotalSize)
	}
	if pg.Files[1].Subtitle != " · S00E00" && !strings.Contains(pg.Files[1].Subtitle, "S00E00") {
		t.Errorf("episode subtitle: %q", pg.Files[1].Subtitle)
	}
	ids, _ := s.Matching(ctx, Filter{Resolution: 1080})
	eq("matching", ids, vc1, mpeg2)
}

// fakeFFmpeg is an FFmpeg that logs its output file and waits for a release file before
// running the real one, so tests can watch the queue.
func fakeFFmpeg(t *testing.T, dir string) (path, log, release string) {
	t.Helper()
	real, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not installed")
	}
	path, log, release = filepath.Join(dir, "ffmpeg"), filepath.Join(dir, "ffmpeg.log"), filepath.Join(dir, "release")
	script := "#!/bin/sh\nfor a; do last=\"$a\"; done\ncase \"$last\" in *" + PartSuffix + ") echo \"$last\" >> '" + log + "'\n" +
		"while [ ! -f '" + release + "' ]; do sleep 0.05; done;; esac\nexec '" + real + "' \"$@\"\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path, log, release
}

// clip makes a small video that HEVC shrinks a lot: MPEG-2 at a high bitrate, with a
// stereo audio track and an SRT subtitle.
func clip(t *testing.T, path string, seconds string) {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	srt := filepath.Join(filepath.Dir(path), "subs.srt")
	os.WriteFile(srt, []byte("1\n00:00:01,000 --> 00:00:02,000\nHello\n"), 0o644)
	out, err := exec.Command("ffmpeg", "-v", "error", "-y", "-f", "lavfi", "-i", "testsrc2=size=320x240:rate=25:duration="+seconds,
		"-f", "lavfi", "-i", "sine=frequency=440:duration="+seconds, "-i", srt,
		"-map", "0:v", "-map", "1:a", "-map", "2:s", "-c:v", "mpeg2video", "-q:v", "1", "-c:a", "ac3", "-c:s", "srt", path).CombinedOutput()
	os.Remove(srt)
	if err != nil {
		t.Fatalf("make clip: %v %s", err, out)
	}
}

func noLook(string) (string, error) { return "", errors.New("not here") }

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func (e *env) state(id int64) (state, msg string) {
	e.db.QueryRow(`SELECT state, error FROM optimize_jobs WHERE id = ?`, id).Scan(&state, &msg)
	return
}

func lines(p string) []string {
	b, _ := os.ReadFile(p)
	return strings.Fields(string(b))
}

// The queue: in order, one at a time (the concurrency setting), paused while someone is
// watching (the encode is suspended where it is and resumes after the grace period),
// cancelled jobs leave the original alone; a real software encode replaces the file in
// place, keeping the item and its watch state; the original goes to the trash and can
// be restored; a file being watched isn't swapped until it isn't.
func TestQueueEncodeSwapRestore(t *testing.T) {
	e := newEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	media := filepath.Join(e.dir, "media")
	var files []int64
	var items []int64
	for _, name := range []string{"A.mkv", "B.mkv", "C.mkv"} {
		p := filepath.Join(media, name)
		clip(t, p, "4")
		fi, _ := os.Stat(p)
		item, file := e.add(fileSpec{path: p, size: fi.Size(), w: 320, h: 240, codec: "mpeg2video", available: true})
		files, items = append(files, file), append(items, item)
	}
	e.db.Exec(`INSERT INTO users (id, username, password_hash, display_name) VALUES (1, 'u', 'x', 'U')`)
	if _, err := e.db.Exec(`INSERT INTO user_item_state (user_id, item_id, view_offset_ms, play_count) VALUES (1, ?, 2000, 3)`, items[0]); err != nil {
		t.Fatal(err)
	}
	ff, log, release := fakeFFmpeg(t, e.dir)
	var watching, playing atomic.Bool
	var rescans atomic.Int32
	s := &Service{DB: e.db, Settings: e.settings, FFmpeg: ff, LookPath: noLook, Trash: &mediatrash.Service{DB: e.db},
		Rescan: func(int64) { rescans.Add(1) }, Watching: watching.Load, Playing: func(int64) bool { return playing.Load() },
		Tick: 20 * time.Millisecond, Grace: 100 * time.Millisecond, BusyRetry: 20 * time.Millisecond}
	e.set(func(c *settings.Settings) {
		c.Optimize.MinSavingsPercent = 20
		c.Optimize.KeepOriginals = true // originals go to the trash, so they can be restored
		c.Optimize.Presets = []settings.OptimizePreset{{ID: "tiny", Name: "Tiny", Codec: "hevc", Quality: "small", Audio: "aac_stereo", HDR: "keep"}}
	})
	if _, _, err := s.Enqueue(ctx, files, "nope"); !errors.Is(err, ErrNoPreset) {
		t.Fatalf("unknown preset: %v", err)
	}
	q, skipped, err := s.Enqueue(ctx, files, "tiny")
	if err != nil || q != 3 || skipped != 0 {
		t.Fatalf("enqueue: %d %d %v", q, skipped, err)
	}
	if q, skipped, _ := s.Enqueue(ctx, files[:1], "tiny"); q != 0 || skipped != 1 {
		t.Fatalf("queued twice: %d %d", q, skipped)
	}
	var jobs []int64
	rows, _ := e.db.Query(`SELECT id FROM optimize_jobs ORDER BY id`)
	for rows.Next() {
		var id int64
		rows.Scan(&id)
		jobs = append(jobs, id)
	}
	rows.Close()

	// Watching from the start: nothing starts.
	watching.Store(true)
	go s.Run(ctx)
	time.Sleep(200 * time.Millisecond)
	if st, _ := e.state(jobs[0]); st != StateQueued || len(lines(log)) != 0 {
		t.Fatalf("started while watching: %s %v", st, lines(log))
	}
	if r := s.HoldReason(); !strings.Contains(r, "watching") {
		t.Errorf("hold reason: %q", r)
	}
	watching.Store(false)
	waitFor(t, "the first job to start", func() bool { return len(lines(log)) == 1 })
	time.Sleep(100 * time.Millisecond)
	if got := lines(log); len(got) != 1 || !strings.HasSuffix(got[0], "A.mkv"+PartSuffix) {
		t.Fatalf("concurrency 1, in order: %v", got)
	}
	if st, _ := e.state(jobs[1]); st != StateQueued {
		t.Fatalf("second job: %s", st)
	}
	// Someone starts watching: the running encode is suspended, and resumes after.
	watching.Store(true)
	waitFor(t, "the encode to be suspended", func() bool { a := s.Active(ctx); return len(a) == 1 && a[0].Paused != "" })
	watching.Store(false)
	waitFor(t, "the encode to resume", func() bool { a := s.Active(ctx); return len(a) == 1 && a[0].Paused == "" })

	// Cancel the second while it waits.
	if err := s.Cancel(ctx, jobs[1]); err != nil {
		t.Fatal(err)
	}
	// The first finishes while the file is being watched: it waits to be swapped.
	playing.Store(true)
	os.WriteFile(release, nil, 0o644)
	waitFor(t, "the swap to wait for the viewer", func() bool { a := s.Active(ctx); return len(a) == 1 && strings.Contains(a[0].Paused, "nobody") })
	if _, err := os.Stat(filepath.Join(media, "A.mkv"+PartSuffix)); err != nil {
		t.Fatalf("the finished file should be waiting: %v", err)
	}
	playing.Store(false)
	waitFor(t, "the first job to finish", func() bool { st, _ := e.state(jobs[0]); return st == StateDone })
	waitFor(t, "the third job to finish", func() bool {
		st, _ := e.state(jobs[2])
		return st == StateDone || st == StateFailed || st == StateSkipped
	})
	if st, msg := e.state(jobs[2]); st != StateDone {
		t.Fatalf("third: %s %s", st, msg)
	}
	if st, _ := e.state(jobs[1]); st != StateCancelled {
		t.Fatalf("cancelled: %s", st)
	}

	// The item and its watch state are kept; the record points at the new file, to be read again.
	var path string
	var size int64
	var fileItem int64
	e.db.QueryRow(`SELECT f.path, f.size, v.item_id FROM media_files f JOIN media_versions v ON v.id = f.version_id WHERE f.id = ?`, files[0]).Scan(&path, &size, &fileItem)
	if path != filepath.Join(media, "A.mkv") || size != -1 || fileItem != items[0] {
		t.Fatalf("record: %s %d item %d", path, size, fileItem)
	}
	var offset, plays int
	e.db.QueryRow(`SELECT view_offset_ms, play_count FROM user_item_state WHERE user_id = 1 AND item_id = ?`, items[0]).Scan(&offset, &plays)
	if offset != 2000 || plays != 3 {
		t.Errorf("watch state: %d %d", offset, plays)
	}
	if rescans.Load() < 1 {
		t.Error("no rescan")
	}
	if _, err := os.Stat(filepath.Join(media, "A.mkv"+PartSuffix)); !os.IsNotExist(err) {
		t.Error("work file left behind")
	}
	pg, err := s.Jobs(ctx, true, 0, 10)
	if err != nil || pg.Total != 2 || pg.Optimized != 2 || pg.SavedBytes <= 0 {
		t.Fatalf("history: %+v %v", pg, err)
	}
	var first Job
	for _, j := range pg.Jobs {
		if j.ID == jobs[0] {
			first = j
		}
	}
	if !first.CanRestore || first.AfterSize <= 0 || first.AfterSize >= first.BeforeSize {
		t.Fatalf("job: %+v", first)
	}
	src, err := s.Probe(ctx, filepath.Join(media, "A.mkv"))
	if err != nil || src.VideoCodec != "hevc" || len(src.Audio) != 1 || len(src.Subtitles) != 1 {
		t.Fatalf("result: %+v %v", src, err)
	}

	// Restore: the original comes back, the optimized file goes to the trash.
	playing.Store(true)
	if err := s.Restore(ctx, jobs[0]); !errors.Is(err, ErrBusy) {
		t.Fatalf("restore while watched: %v", err)
	}
	playing.Store(false)
	if err := s.Restore(ctx, jobs[0]); err != nil {
		t.Fatal(err)
	}
	if st, _ := e.state(jobs[0]); st != StateRestored {
		t.Fatalf("restored: %s", st)
	}
	if src, err := s.Probe(ctx, filepath.Join(media, "A.mkv")); err != nil || src.VideoCodec != "mpeg2video" {
		t.Fatalf("original back: %+v %v", src, err)
	}
	if err := s.Restore(ctx, jobs[0]); !errors.Is(err, ErrCantRestore) {
		t.Errorf("restored twice: %v", err)
	}
	trash, _ := filepath.Glob(filepath.Join(media, library.TrashDir, "*", "A*.mkv"))
	if len(trash) != 1 {
		t.Errorf("trash: %v", trash)
	}

	// Retry the cancelled one; clear finished hides the rest from the queue.
	if err := s.Retry(ctx, jobs[1]); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "the retried job", func() bool { st, _ := e.state(jobs[1]); return st == StateDone })
	if pg, err := s.Jobs(ctx, false, 0, 10); err != nil || pg.Total != 3 || pg.Jobs[0].State != StateDone {
		t.Fatalf("queue before clearing: %+v %v", pg, err)
	}
	if n, _ := s.ClearFinished(ctx); n != 3 {
		t.Errorf("cleared %d", n)
	}
	if pg, err := s.Jobs(ctx, false, 0, 10); err != nil || pg.Total != 0 {
		t.Errorf("queue after clearing: %+v", pg)
	}
}

// A result that isn't enough smaller is thrown away (skipped), leaving the original alone
// even with keepOriginals off (the default); a truncated result fails verification.
func TestThresholdAndVerify(t *testing.T) {
	e := newEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	media := filepath.Join(e.dir, "media")
	p := filepath.Join(media, "D.mkv")
	clip(t, p, "4")
	fi, _ := os.Stat(p)
	_, file := e.add(fileSpec{path: p, size: fi.Size(), w: 320, h: 240, codec: "mpeg2video", available: true})
	e.set(func(c *settings.Settings) { c.Optimize.MinSavingsPercent = 90 })
	s := &Service{DB: e.db, Settings: e.settings, LookPath: noLook, Trash: &mediatrash.Service{DB: e.db}, Tick: 20 * time.Millisecond}
	s.HasEncoder = func(name string) bool { return name == "libx265" || name == "libx264" }
	var failures atomic.Int32
	s.OnFailed = func(Failure) { failures.Add(1) }
	before, _ := os.ReadFile(p)
	if _, _, err := s.Enqueue(ctx, []int64{file}, "1080p-hevc-small"); err != nil {
		t.Fatal(err)
	}
	go s.Run(ctx)
	var id int64
	e.db.QueryRow(`SELECT id FROM optimize_jobs`).Scan(&id)
	waitFor(t, "the job", func() bool {
		st, _ := e.state(id)
		return st != StateQueued && st != StateRunning && st != StateVerifying
	})
	st, msg := e.state(id)
	if st != StateSkipped || !strings.Contains(msg, "smaller") {
		t.Fatalf("threshold: %s %q", st, msg)
	}
	if n := failures.Load(); n != 0 {
		t.Errorf("a skip was reported as a failure %d times", n)
	}
	after, _ := os.ReadFile(p)
	if string(after) != string(before) {
		t.Error("the original changed")
	}
	if _, err := os.Stat(p + PartSuffix); !os.IsNotExist(err) {
		t.Error("work file left behind")
	}

	// Verification: a result 1 s short of 4 s is rejected.
	short := filepath.Join(e.dir, "short.mkv")
	if out, err := exec.Command("ffmpeg", "-v", "error", "-i", p, "-t", "3", "-map", "0", "-c", "copy", short).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	src, err := s.Probe(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	pl, _ := NewPlan(src, preset("same-hevc"))
	if err := s.verify(ctx, short, src, pl); err == nil || !strings.Contains(err.Error(), "long") {
		t.Errorf("truncated output accepted: %v", err)
	}
	if err := s.verify(ctx, p, src, pl); err != nil {
		t.Errorf("the original itself should verify: %v", err)
	}
	// A truncated file (cut mid-stream) doesn't pass either.
	data, _ := os.ReadFile(p)
	cut := filepath.Join(e.dir, "cut.mkv")
	os.WriteFile(cut, data[:len(data)/2], 0o644)
	if err := s.verify(ctx, cut, src, pl); err == nil {
		t.Error("half a file accepted")
	}
}

// A film whose picture ends before its audio (the file's length is the audio's) verifies:
// the end is sampled where the picture ends, not in the sound after it. A result whose
// picture is cut short of the original's still fails.
func TestVerifyPictureEndsEarly(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not installed")
	}
	p := filepath.Join(e.dir, "early.mkv")
	clip(t, p, "1") // skips without ffmpeg
	if out, err := exec.Command("ffmpeg", "-v", "error", "-y", "-f", "lavfi", "-i", "testsrc2=size=320x240:rate=25:duration=6",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=12", "-map", "0:v", "-map", "1:a", "-c:v", "mpeg2video", "-c:a", "ac3", p).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	s := &Service{DB: e.db, Settings: e.settings, LookPath: noLook}
	src, err := s.Probe(ctx, p)
	if err != nil {
		t.Fatal(err)
	}
	if src.DurationMS < 11_000 {
		t.Fatalf("the file should run to the end of the audio: %d ms", src.DurationMS)
	}
	pl, _ := NewPlan(src, preset("same-hevc"))
	if err := s.verify(ctx, p, src, pl); err != nil {
		t.Errorf("a picture shorter than the file was rejected: %v", err)
	}
	// The same audio with the picture cut at 3 s of 6.
	cut := filepath.Join(e.dir, "cut.mkv")
	if out, err := exec.Command("ffmpeg", "-v", "error", "-y", "-t", "3", "-i", p, "-i", p,
		"-map", "0:v", "-map", "1:a", "-c", "copy", cut).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if err := s.verify(ctx, cut, src, pl); err == nil {
		t.Error("a picture cut short was accepted")
	}
}

// NVENC: a session is always left free for playback, and a job gives its session up when
// playback takes the last free one.
func TestNVENCYieldsToPlayback(t *testing.T) {
	e := newEnv(t)
	e.set(func(c *settings.Settings) { c.Transcoder.NVENCSessions = 3 })
	var pb atomic.Int32
	s := &Service{DB: e.db, Settings: e.settings, Encoders: func() []string { return []string{"nvenc", "qsv", "software"} },
		PlaybackNVENC: func() int { return int(pb.Load()) }}
	if got := s.encoders("hevc"); got[0] != "nvenc" {
		t.Fatalf("free GPU: %v", got)
	}
	pb.Store(2) // 2 of 3 used by playback: the last one stays free
	if got := s.encoders("hevc"); got[0] != "qsv" {
		t.Fatalf("GPU nearly full: %v", got)
	}
	// A running NVENC job, then playback takes the last free session: the job yields.
	pb.Store(1)
	var cancelled atomic.Bool
	s.running = map[int64]*job{7: {id: 7, encoder: "nvenc", cancel: func() { cancelled.Store(true) }}}
	s.step(context.Background())
	if cancelled.Load() {
		t.Fatal("yielded with a session free")
	}
	pb.Store(2)
	s.step(context.Background())
	if !cancelled.Load() || !errors.Is(s.running[7].why, errYield) {
		t.Fatal("didn't give the session to playback")
	}
	s.HasEncoder = func(n string) bool { return n != "libsvtav1" && n != "av1_qsv" }
	pb.Store(0)
	s.running = nil
	if got := s.encoders("av1"); len(got) != 1 || got[0] != "nvenc" {
		t.Errorf("AV1 encoders: %v", got)
	}
}

// A conversion that fails (every encoder errors) is reported to OnFailed with the title and
// why, so the optimize_failed alert can be raised; the original is left alone.
func TestFailureReported(t *testing.T) {
	e := newEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := filepath.Join(e.dir, "media", "F.mkv")
	clip(t, p, "2")
	fi, _ := os.Stat(p)
	_, file := e.add(fileSpec{path: p, size: fi.Size(), w: 320, h: 240, codec: "mpeg2video", available: true})
	broken := filepath.Join(e.dir, "broken-ffmpeg")
	os.WriteFile(broken, []byte("#!/bin/sh\necho 'Conversion failed!' >&2\nexit 1\n"), 0o755)
	s := &Service{DB: e.db, Settings: e.settings, FFmpeg: broken, LookPath: noLook, Trash: &mediatrash.Service{DB: e.db}, Tick: 20 * time.Millisecond}
	s.HasEncoder = func(name string) bool { return name == "libx265" || name == "libx264" }
	got := make(chan Failure, 4)
	s.OnFailed = func(f Failure) { got <- f }
	if _, _, err := s.Enqueue(ctx, []int64{file}, "1080p-hevc-small"); err != nil {
		t.Fatal(err)
	}
	go s.Run(ctx)
	select {
	case f := <-got:
		if f.FileID != file || f.JobID == 0 || f.Title == "" || !strings.Contains(f.Error, "re-encode") {
			t.Errorf("failure = %+v", f)
		}
		if st, _ := e.state(f.JobID); st != StateFailed {
			t.Errorf("state = %s", st)
		}
	case <-time.After(60 * time.Second):
		t.Fatal("the failure wasn't reported")
	}
	if fi2, _ := os.Stat(p); fi2 == nil || fi2.Size() != fi.Size() {
		t.Error("the original changed")
	}
}
