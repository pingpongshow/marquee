package filecheck

import (
	"context"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"marquee/internal/db"
	"marquee/internal/health"
	"marquee/internal/settings"
	"marquee/internal/tasks"
)

type env struct {
	t   *testing.T
	db  *sql.DB
	dir string
}

func newEnv(t *testing.T) *env {
	dir := t.TempDir()
	d, err := db.Open(context.Background(), filepath.Join(dir, "t.db"), filepath.Join(dir, "b"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return &env{t: t, db: d, dir: dir}
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

func (e *env) library(typ string, recordings bool) int64 {
	rec := 0
	if recordings {
		rec = 1
	}
	return e.exec(`INSERT INTO libraries(name, type, recordings) VALUES (?, ?, ?)`, typ, typ, rec)
}

// file adds an item of type typ with one file at path (with a video stream unless typ is
// track). checked marks it checked, as the migration does for files already there. Its frame
// timing counts as checked (TestRunChecksFrameTiming clears that).
func (e *env) file(lib int64, typ, path string, durationMS int64, checked bool) int64 {
	item := e.exec(`INSERT INTO items(library_id, type, title, sort_title) VALUES (?, ?, ?, ?)`, lib, typ, filepath.Base(path), filepath.Base(path))
	v := e.exec(`INSERT INTO media_versions(item_id) VALUES (?)`, item)
	var size, mtime int64 = 1, 1
	if st, err := os.Stat(path); err == nil {
		size, mtime = st.Size(), st.ModTime().Unix()
	}
	var at any
	if checked {
		at = "2026-01-01T00:00:00.000Z"
	}
	f := e.exec(`INSERT INTO media_files(version_id, library_id, path, size, mtime, duration_ms, checked_at, timing_checked_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, '2026-01-01T00:00:00.000Z')`,
		v, lib, path, size, mtime, durationMS, at)
	kind := "video"
	if typ == "track" {
		kind = "audio"
	}
	e.exec(`INSERT INTO streams(file_id, kind, codec) VALUES (?, ?, 'x')`, f, kind)
	return f
}

func (e *env) state(id int64) (checked, reason sql.NullString) {
	e.t.Helper()
	if err := e.db.QueryRow(`SELECT checked_at, check_error FROM media_files WHERE id = ?`, id).Scan(&checked, &reason); err != nil {
		e.t.Fatal(err)
	}
	return
}

// A good new film passes; a truncated one (its end gone) is listed in Library Health →
// Unplayable files with the reason, and the alert hears about it. A file already in the
// library isn't checked, nor are music, extras or Live TV recordings (marked done).
func TestRunFindsDamagedNewFiles(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	e := newEnv(t)
	ctx := context.Background()
	good := filepath.Join(e.dir, "Good (2020).mkv")
	if out, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "testsrc=size=160x120:rate=25:duration=40",
		"-c:v", "libx264", "-preset", "ultrafast", "-g", "25", good).CombinedOutput(); err != nil {
		t.Skipf("ffmpeg couldn't make a test file: %v %s", err, out)
	}
	data, err := os.ReadFile(good)
	if err != nil {
		t.Fatal(err)
	}
	broken := filepath.Join(e.dir, "Broken (2021).mkv")
	if err := os.WriteFile(broken, data[:len(data)/2], 0o644); err != nil { // its header still says 40 s
		t.Fatal(err)
	}
	movies, music, recordings := e.library("movies", false), e.library("music", false), e.library("shows", true)
	goodID := e.file(movies, "movie", good, 40_000, false)
	brokenID := e.file(movies, "movie", broken, 40_000, false)
	oldID := e.file(movies, "movie", filepath.Join(e.dir, "Old.mkv"), 40_000, true) // not even there: never looked at
	trackID := e.file(music, "track", filepath.Join(e.dir, "Song.flac"), 40_000, false)
	recID := e.file(recordings, "episode", filepath.Join(e.dir, "Recording.ts"), 40_000, false)
	// An episode the DVR recorded into an ordinary library.
	shows := e.library("shows", false)
	dvrPath := filepath.Join(e.dir, "Show S01E01.ts")
	dvrID := e.file(shows, "episode", dvrPath, 40_000, false)
	user := e.exec(`INSERT INTO users (username, display_name) VALUES ('u', 'U')`)
	e.exec(`INSERT INTO dvr_recordings(user_id, channel_name, title, start, stop, status, path, created_at)
		VALUES (?, 'C', 'Show', '2026-01-01T00:00:00Z', '2026-01-01T01:00:00Z', 'completed', ?, '2026-01-01T00:00:00Z')`, user, dvrPath)

	var mu sync.Mutex
	var decoded []string
	var heard []Damaged
	s := &Service{DB: e.db, OnDamaged: func(_ context.Context, d []Damaged) { heard = d }}
	s.Decode = func(ctx context.Context, path string, at float64) error {
		mu.Lock()
		decoded = append(decoded, filepath.Base(path))
		mu.Unlock()
		return (&Service{}).decode(ctx, path, at) // the real test, at low priority
	}
	if !s.Pending(ctx) {
		t.Fatal("nothing pending")
	}
	msg, err := s.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if msg != "2 checked, 1 damaged" {
		t.Errorf("result %q", msg)
	}
	if c, r := e.state(goodID); !c.Valid || r.Valid {
		t.Errorf("good file: checked %v, error %v", c, r)
	}
	c, r := e.state(brokenID)
	if !c.Valid || !strings.HasPrefix(r.String, "The end doesn't decode: ") || strings.Contains(r.String, "@ 0x") {
		t.Errorf("broken file: checked %v, error %q", c, r.String)
	}
	if c, _ := e.state(oldID); c.String != "2026-01-01T00:00:00.000Z" {
		t.Errorf("file already in the library was checked again: %v", c)
	}
	for _, id := range []int64{trackID, recID, dvrID} {
		if c, r := e.state(id); !c.Valid || r.Valid {
			t.Errorf("file %d not marked done: %v %v", id, c, r)
		}
	}
	for _, d := range decoded {
		if d != filepath.Base(good) && d != filepath.Base(broken) {
			t.Errorf("decoded %s", d)
		}
	}
	if len(heard) != 1 || heard[0].FileID != brokenID || heard[0].Reason != r.String {
		t.Errorf("alert heard %+v", heard)
	}
	if s.Pending(ctx) {
		t.Error("still pending")
	}

	// Library Health lists it with the reason.
	hs := &health.Service{DB: e.db}
	issues, total, err := hs.Issues(ctx, "unplayable", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(issues) != 1 {
		t.Fatalf("unplayable: %d %+v", total, issues)
	}
	if issues[0].FileID != brokenID || issues[0].Detail != r.String {
		t.Errorf("unplayable issue %+v", issues[0])
	}

	// A second run has nothing to do.
	if msg, _ := s.Run(ctx); msg != "No new files" {
		t.Errorf("second run %q", msg)
	}
}

// Turned off (Settings → Scheduled Tasks), the task doesn't start after a scan; Run now
// still runs it. A known length that's far off fails the check without decoding.
func TestTaskTurnedOff(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	store, err := settings.Open(ctx, e.db)
	if err != nil {
		t.Fatal(err)
	}
	movies := e.library("movies", false)
	path := filepath.Join(e.dir, "Short.mkv")
	os.WriteFile(path, []byte("x"), 0o644)
	id := e.file(movies, "movie", path, 600_000, false)
	e.exec(`INSERT INTO vod_saves(source_id, kind, vod_id, source_key, file, title, expected_ms, target_path, quality, state, queue_pos, created_at)
		VALUES ('s', 'movie', 1, 'k', 'k.mkv', 'Short', 5400000, ?, 'original', 'done', 1, '2026-01-01T00:00:00Z')`, path)
	var decodes int
	fc := &Service{DB: e.db, Decode: func(context.Context, string, float64) error { decodes++; return nil }}
	sched := &tasks.Scheduler{DB: e.db, Settings: store}
	sched.Register(fc.Task())
	if err := sched.SetEnabled(ctx, TaskID, false); err != nil {
		t.Fatal(err)
	}
	if err := sched.Trigger(ctx, TaskID); err != tasks.ErrTaskDisabled {
		t.Fatalf("trigger while off: %v", err)
	}
	if c, _ := e.state(id); c.Valid || !fc.Pending(ctx) {
		t.Fatal("checked while turned off")
	}
	if err := sched.RunNow(ctx, TaskID); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(10 * time.Second)
	for {
		if c, _ := e.state(id); c.Valid {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Run now didn't check the file")
		}
		time.Sleep(20 * time.Millisecond)
	}
	for len(sched.Running()) > 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if _, r := e.state(id); r.String != "It is 0:10:00 long; expected 1:30:00" || decodes != 0 {
		t.Errorf("error %q, %d decodes", r.String, decodes)
	}
}

// Every video file's frame timing is checked once, those already in the library included:
// one timed in decode order is listed in Library Health → Frame timing errors; music and AVI
// files are marked done without a look; a changed file is checked again.
func TestRunChecksFrameTiming(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	movies, music := e.library("movies", false), e.library("music", false)
	path := func(name string) string {
		p := filepath.Join(e.dir, name)
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	goodID := e.file(movies, "movie", path("Good (2020).mp4"), 40_000, true)
	badID := e.file(movies, "movie", path("Sunshine (2007).mp4"), 40_000, true)
	aviID := e.file(movies, "movie", path("Old (1999).avi"), 40_000, true)
	trackID := e.file(music, "track", path("Song.flac"), 40_000, true)
	e.exec(`UPDATE media_files SET timing_checked_at = NULL`)
	var looked []string
	s := &Service{DB: e.db, Timing: func(_ context.Context, p string) (string, error) {
		looked = append(looked, filepath.Base(p))
		if strings.Contains(p, "Sunshine") {
			return "Frames are timed in decode order (86 of the first 190 out of order)", nil
		}
		return "", nil
	}}
	if !s.Pending(ctx) {
		t.Fatal("nothing pending")
	}
	msg, err := s.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if msg != "Frame timing of 2 checked, 1 out of order" {
		t.Errorf("result %q", msg)
	}
	if strings.Join(looked, ",") != "Good (2020).mp4,Sunshine (2007).mp4" {
		t.Errorf("looked at %v", looked)
	}
	timing := func(id int64) (checked bool, reason string) {
		var at, r sql.NullString
		e.db.QueryRow(`SELECT timing_checked_at, timing_error FROM media_files WHERE id = ?`, id).Scan(&at, &r)
		return at.Valid, r.String
	}
	for _, id := range []int64{goodID, aviID, trackID} {
		if checked, reason := timing(id); !checked || reason != "" {
			t.Errorf("file %d: checked %v, reason %q", id, checked, reason)
		}
	}
	if _, reason := timing(badID); !strings.HasPrefix(reason, "Frames are timed in decode order") {
		t.Errorf("reason %q", reason)
	}
	if s.Pending(ctx) {
		t.Error("still pending")
	}
	hs := &health.Service{DB: e.db}
	issues, total, err := hs.Issues(ctx, "frameTiming", 0, 10)
	if err != nil || total != 1 || issues[0].FileID != badID || !strings.HasPrefix(issues[0].Detail, "Frames are timed") {
		t.Fatalf("health: %v %d %+v", err, total, issues)
	}
	// Nothing more to look at.
	looked = nil
	if msg, _ := s.Run(ctx); msg != "No new files" || len(looked) > 0 {
		t.Errorf("second run %q, looked at %v", msg, looked)
	}
}
