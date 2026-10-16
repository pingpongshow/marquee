package commercials

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"marquee/internal/db"
	"marquee/internal/library"
	"marquee/internal/mediatrash"
	"marquee/internal/settings"
)

type fixture struct {
	t        *testing.T
	db       *sql.DB
	dir, rec string
	lib      int64
	log      string
	rescans  atomic.Int64
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	dir := t.TempDir()
	d, err := db.Open(context.Background(), filepath.Join(dir, "t.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	f := &fixture{t: t, db: d, dir: dir, rec: filepath.Join(dir, "recordings"), log: filepath.Join(dir, "comskip.log")}
	os.MkdirAll(filepath.Join(f.rec, "TV"), 0o755)
	lib, err := library.NewStore(d).Create(context.Background(), "Recorded TV", library.Shows, []string{filepath.Join(f.rec, "TV")}, library.Options{})
	if err != nil {
		t.Fatal(err)
	}
	f.lib = lib.ID
	f.exec(`INSERT INTO users (username, display_name) VALUES ('u', 'U')`)
	return f
}

func (f *fixture) exec(q string, args ...any) int64 {
	f.t.Helper()
	res, err := f.db.Exec(q, args...)
	if err != nil {
		f.t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

// addFile puts a file in the library (writing a placeholder unless it exists) and returns
// its item and file ids.
func (f *fixture) addFile(path string, durationMS int64) (item, file int64) {
	f.t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o755)
	if _, err := os.Stat(path); err != nil {
		os.WriteFile(path, []byte("video"), 0o644)
	}
	item = f.exec(`INSERT INTO items (library_id, type, title, sort_title) VALUES (?, 'episode', 'Show', 'Show')`, f.lib)
	v := f.exec(`INSERT INTO media_versions (item_id) VALUES (?)`, item)
	file = f.exec(`INSERT INTO media_files (version_id, library_id, path, size, mtime, duration_ms) VALUES (?, ?, ?, 5, 100, ?)`, v, f.lib, path, durationMS)
	return item, file
}

// comskip writes a fake Comskip that logs its arguments and writes edl (when not empty)
// into its --output folder before exiting with code.
func (f *fixture) comskip(edl string, code int) string {
	f.t.Helper()
	p := filepath.Join(f.dir, fmt.Sprintf("comskip-%d", code))
	script := "#!/bin/sh\necho \"$@\" >> '" + f.log + "'\n" +
		"for a in \"$@\"; do case \"$a\" in --output=*) out=\"${a#--output=}\";; esac; done\n"
	if edl != "" {
		script += "printf '" + edl + "' > \"$out/cut.edl\"\n"
	} else if code == 1 {
		script += ": > \"$out/cut.edl\"\n"
	}
	if code == 2 {
		script += "echo 'could not open the file' >&2\n"
	}
	script += "exit " + strconv.Itoa(code) + "\n"
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		f.t.Fatal(err)
	}
	return p
}

func (f *fixture) service(comskip string, mode string) *Service {
	return &Service{DB: f.db, Comskip: comskip, Dir: f.rec, WaitForFile: 5 * time.Second,
		Mode:     func() string { return mode },
		Trash:    &mediatrash.Service{DB: f.db},
		Rescan:   func(lib int64) { f.rescans.Store(lib) },
		LookPath: func(string) (string, error) { return "", errors.New("not here") }}
}

func waitStatus(t *testing.T, s *Service, file int64, want string) Status {
	t.Helper()
	deadline := time.Now().Add(60 * time.Second)
	for {
		st, err := s.Status(context.Background(), file)
		if err == nil && st.Status == want {
			return st
		}
		if time.Now().After(deadline) {
			t.Fatalf("status %+v (%v), want %s", st, err, want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestCommand(t *testing.T) {
	s := &Service{Comskip: "/usr/local/bin/comskip", INI: "/nonexistent/comskip.ini",
		LookPath: func(n string) (string, error) { return "/usr/bin/" + n, nil }}
	name, args := s.Command("/recordings/TV/Show/a.mkv", "/tmp/out")
	got := append([]string{name}, args...)
	want := []string{"/usr/bin/nice", "-n", "19", "/usr/bin/ionice", "-c3", "/usr/local/bin/comskip",
		"--quiet", "--output=/tmp/out", "--output-filename=cut", "/recordings/TV/Show/a.mkv"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("with nice and ionice:\n got %q\nwant %q", got, want)
	}

	// No ionice (e.g. macOS); the settings file is passed when it exists.
	ini := filepath.Join(t.TempDir(), "comskip.ini")
	os.WriteFile(ini, []byte("[Main Settings]\n"), 0o644)
	s.INI = ini
	s.LookPath = func(n string) (string, error) {
		if n == "ionice" {
			return "", errors.New("missing")
		}
		return "/usr/bin/" + n, nil
	}
	name, args = s.Command("/r/a.mkv", "/o")
	got = append([]string{name}, args...)
	want = []string{"/usr/bin/nice", "-n", "19", "/usr/local/bin/comskip", "--ini=" + ini, "--quiet", "--output=/o", "--output-filename=cut", "/r/a.mkv"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("without ionice:\n got %q\nwant %q", got, want)
	}
}

func TestDetectOnly(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s := f.service(f.comskip(`0.00\t30.00\t0\n600.00\t780.00\t0\n785.00\t900.00\t0\n1790.00\t1800.00\t0\n`, 0), settings.CommercialsDetect)
	go s.Run(ctx)

	// The recording finishes before the library scan has added it.
	path := filepath.Join(f.rec, "TV", "Show", "Show - S01E01.mkv")
	s.RecordingFinished(ctx, path)
	time.Sleep(100 * time.Millisecond)
	item, file := f.addFile(path, 1_800_000)
	if got, err := s.FileForItem(ctx, item); err != nil || got != file {
		t.Fatalf("file for item: %d %v", got, err)
	}

	st := waitStatus(t, s, file, StatusDetected)
	// The first 5 s are left alone, breaks 5 s apart are merged, the last 5 s are too short.
	want := []Break{{5_000, 30_000}, {600_000, 900_000}}
	if !reflect.DeepEqual(st.Breaks, want) || st.ScannedAt == nil || st.Error != "" || st.RemovedMS != 0 {
		t.Errorf("status %+v, want breaks %v", st, want)
	}
	if b, _ := os.ReadFile(path); string(b) != "video" {
		t.Error("detect only changed the file")
	}
	log, _ := os.ReadFile(f.log)
	if !strings.Contains(string(log), path) || !strings.Contains(string(log), "--output-filename=cut") {
		t.Errorf("comskip args: %s", log)
	}

	// A changed file is pending again.
	f.exec(`UPDATE media_files SET mtime = 200 WHERE id = ?`, file)
	if st, _ := s.Status(ctx, file); st.Status != StatusPending {
		t.Errorf("changed file: %+v", st)
	}

	// Files outside the recordings folder aren't recordings.
	_, other := f.addFile(filepath.Join(f.dir, "media", "film.mkv"), 1_800_000)
	if _, err := s.Status(ctx, other); err != ErrNotRecording {
		t.Errorf("non-recording status: %v", err)
	}
	if _, err := s.Queue(ctx, other, true); err != ErrNotRecording {
		t.Errorf("non-recording queue: %v", err)
	}
}

func TestDetectOutcomes(t *testing.T) {
	f := newFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_, file := f.addFile(filepath.Join(f.rec, "TV", "Show", "a.mkv"), 1_800_000)

	if st, err := f.service("", settings.CommercialsDetect).Status(ctx, file); err != nil || st.Status != StatusPending || len(st.Breaks) != 0 {
		t.Fatalf("pending: %+v %v", st, err)
	}
	if _, err := f.service(filepath.Join(f.dir, "missing"), settings.CommercialsDetect).Queue(ctx, file, false); err != ErrUnavailable {
		t.Errorf("unavailable: %v", err)
	}

	// Comskip exits 1 when it finds nothing; with nothing found there's nothing to cut.
	none := f.service(f.comskip("", 1), settings.CommercialsRemove)
	go none.Run(ctx)
	none.Queue(ctx, file, true)
	if st := waitStatus(t, none, file, StatusDetected); len(st.Breaks) != 0 {
		t.Errorf("nothing found: %+v", st)
	}

	// A crash is a failure, with Comskip's last words. (What was found is reused, so forget it.)
	f.exec(`DELETE FROM commercial_scans`)
	failing := f.service(f.comskip("", 2), settings.CommercialsDetect)
	go failing.Run(ctx)
	failing.Queue(ctx, file, false)
	st := waitStatus(t, failing, file, StatusFailed)
	if !strings.Contains(st.Error, "could not open the file") {
		t.Errorf("failure reason: %q", st.Error)
	}
}

func TestBacklogAndSettingOff(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	s := f.service(f.comskip(`600\t780\t0\n`, 0), settings.CommercialsOff)
	_, a := f.addFile(filepath.Join(f.rec, "TV", "Show", "a.mkv"), 1_800_000)
	_, b := f.addFile(filepath.Join(f.rec, "TV", "Film", "Film.mkv"), 1_800_000)
	_, done := f.addFile(filepath.Join(f.rec, "TV", "Show", "done.mkv"), 1_800_000)
	_, outside := f.addFile(filepath.Join(f.dir, "media", "film.mkv"), 1_800_000)
	f.exec(`INSERT INTO commercial_scans (file_id, mtime, status) VALUES (?, 100, 'failed')`, done) // not retried by itself

	// Off: nothing happens, nightly or when a recording finishes.
	if msg, err := s.Backlog(ctx); err != nil || msg != "Turned off" {
		t.Errorf("off: %q %v", msg, err)
	}
	s.RecordingFinished(ctx, filepath.Join(f.rec, "TV", "Show", "a.mkv"))
	s.waiting.Wait()
	if len(s.queue) != 0 {
		t.Errorf("queued while off: %d", len(s.queue))
	}

	mode := settings.CommercialsDetect
	s.Mode = func() string { return mode }
	msg, err := s.Backlog(ctx)
	if err != nil || !strings.Contains(msg, "Checked 2 recordings: found 2 commercial breaks") {
		t.Fatalf("backlog: %q %v", msg, err)
	}
	for _, id := range []int64{a, b} {
		if st, _ := s.Status(ctx, id); st.Status != StatusDetected || len(st.Breaks) != 1 {
			t.Errorf("file %d: %+v", id, st)
		}
	}
	if st, _ := s.Status(ctx, done); st.Status != StatusFailed {
		t.Errorf("earlier failure retried: %+v", st)
	}
	var n int
	f.db.QueryRow(`SELECT COUNT(*) FROM commercial_scans WHERE file_id = ?`, outside).Scan(&n)
	if n != 0 {
		t.Error("a file outside the recordings folder was looked at")
	}
	if msg, _ := s.Backlog(ctx); !strings.HasPrefix(msg, "Checked 0 recordings") {
		t.Errorf("second run: %q", msg)
	}
	// Switching to remove picks up what was found but not cut (here the cut fails: the
	// files are placeholders) without running Comskip again.
	os.Remove(f.log)
	mode = settings.CommercialsRemove
	msg, _ = s.Backlog(ctx)
	if !strings.Contains(msg, "Checked 2 recordings") || !strings.Contains(msg, "2 failed") {
		t.Errorf("remove: %q", msg)
	}
	if _, err := os.Stat(f.log); err == nil {
		t.Error("Comskip ran again for recordings already checked")
	}

	// Paced, and stopped by the end of the maintenance window.
	mode = settings.CommercialsDetect
	f.exec(`DELETE FROM commercial_scans`)
	s.Pace = time.Hour
	wctx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()
	msg, err = s.Backlog(wctx)
	if err != nil || !strings.Contains(msg, "Checked 1 recordings") || !strings.Contains(msg, "left for next time") {
		t.Errorf("paced: %q %v", msg, err)
	}
}

// ---------- cutting, with FFmpeg ----------

func tools(t *testing.T) (ffmpeg, ffprobe string) {
	t.Helper()
	ff, err1 := exec.LookPath("ffmpeg")
	fp, err2 := exec.LookPath("ffprobe")
	if err1 != nil || err2 != nil {
		t.Skip("ffmpeg and ffprobe needed")
	}
	return ff, fp
}

// makeClip writes a 95 s recording: programme 0–20 s, adverts 20–40 s, programme 40–60 s,
// adverts 60–80 s, programme 80–95 s; H.264 with a keyframe every 2 s, two audio tracks and
// a subtitle track.
func makeClip(t *testing.T, ff, path string) {
	t.Helper()
	dir := t.TempDir()
	parts := []struct {
		name, video string
		secs        int
		freq        int
	}{{"a", "testsrc2", 20, 440}, {"ad1", "smptebars", 20, 1000}, {"b", "testsrc2", 20, 440}, {"ad2", "rgbtestsrc", 20, 1200}, {"c", "testsrc2", 15, 440}}
	var list strings.Builder
	for _, p := range parts {
		out := filepath.Join(dir, p.name+".ts")
		cmd := exec.Command(ff, "-v", "error", "-y",
			"-f", "lavfi", "-i", fmt.Sprintf("%s=s=320x180:r=30:d=%d", p.video, p.secs),
			"-f", "lavfi", "-i", fmt.Sprintf("sine=f=%d:r=48000:d=%d", p.freq, p.secs),
			"-f", "lavfi", "-i", fmt.Sprintf("sine=f=%d:r=48000:d=%d", p.freq/2, p.secs),
			"-map", "0", "-map", "1", "-map", "2", "-c:v", "libx264", "-preset", "ultrafast", "-g", "60", "-keyint_min", "60",
			"-sc_threshold", "0", "-bf", "0", "-c:a", "aac", "-ac", "2", "-f", "mpegts", out)
		if b, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("clip part: %v %s", err, b)
		}
		fmt.Fprintf(&list, "file '%s'\n", out)
	}
	os.WriteFile(filepath.Join(dir, "list.txt"), []byte(list.String()), 0o644)
	srt := "1\n00:00:01,000 --> 00:00:03,000\nHello\n\n2\n00:01:25,000 --> 00:01:27,000\nGoodbye\n"
	os.WriteFile(filepath.Join(dir, "subs.srt"), []byte(srt), 0o644)
	os.MkdirAll(filepath.Dir(path), 0o755)
	cmd := exec.Command(ff, "-v", "error", "-y", "-f", "concat", "-safe", "0", "-i", filepath.Join(dir, "list.txt"),
		"-i", filepath.Join(dir, "subs.srt"), "-map", "0", "-map", "1", "-c", "copy", "-f", "matroska", path)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("clip: %v %s", err, b)
	}
}

// streamEnds returns each stream's first and last timestamps (s) and its kind.
func streamEnds(t *testing.T, fp, path string) (first, last map[int]float64, kinds map[int]string) {
	t.Helper()
	out, err := exec.Command(fp, "-v", "error", "-show_entries", "packet=stream_index,pts_time,duration_time", "-of", "csv=p=0", path).Output()
	if err != nil {
		t.Fatal(err)
	}
	first, last, kinds = map[int]float64{}, map[int]float64{}, map[int]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		f := strings.Split(line, ",")
		if len(f) < 3 {
			continue
		}
		i, _ := strconv.Atoi(f[0])
		pts, err := strconv.ParseFloat(f[1], 64)
		if err != nil {
			continue
		}
		d, _ := strconv.ParseFloat(f[2], 64)
		if v, ok := first[i]; !ok || pts < v {
			first[i] = pts
		}
		last[i] = max(last[i], pts+d)
	}
	ks, _ := exec.Command(fp, "-v", "error", "-show_entries", "stream=index,codec_type", "-of", "csv=p=0", path).Output()
	for _, line := range strings.Split(strings.TrimSpace(string(ks)), "\n") {
		if i, k, ok := strings.Cut(line, ","); ok {
			n, _ := strconv.Atoi(i)
			kinds[n] = k
		}
	}
	return first, last, kinds
}

func hashFile(t *testing.T, p string) string {
	t.Helper()
	fh, err := os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer fh.Close()
	h := sha256.New()
	io.Copy(h, fh)
	return fmt.Sprintf("%x", h.Sum(nil))
}

func TestCutSwapAndRestore(t *testing.T) {
	ff, fp := tools(t)
	f := newFixture(t)
	ctx := context.Background()
	path := filepath.Join(f.rec, "TV", "Show", "Show - S01E01.mkv")
	makeClip(t, ff, path)
	origHash := hashFile(t, path)
	item, file := f.addFile(path, 95_000)
	// Someone stopped 50 s in (10 s into the second part of the programme).
	f.exec(`INSERT INTO user_item_state (user_id, item_id, view_offset_ms, play_count) VALUES (1, ?, 50000, 2)`, item)
	f.exec(`INSERT INTO markers (file_id, kind, start_ms, end_ms, source) VALUES (?, 'intro', 0, 10000, 'detected')`, file)

	// The breaks Comskip reports end a little after a keyframe: the cut keeps the programme
	// by resuming on the keyframe at 40 s and 80 s.
	s := f.service(f.comskip(`20.00\t40.90\t0\n60.00\t80.90\t0\n`, 0), settings.CommercialsRemove)
	s.FFmpeg, s.FFprobe = ff, fp
	playing := false
	s.Playing = func(id int64) bool { return playing && id == file }

	// Not while someone's watching: it waits, file untouched.
	playing = true
	if _, err := s.process(ctx, mustFile(t, s, file), true); !errors.Is(err, ErrBusy) {
		st, _ := s.Status(ctx, file)
		t.Fatalf("while playing: %v %+v", err, st)
	}
	if st, _ := s.Status(ctx, file); st.Status != StatusWaiting || hashFile(t, path) != origHash {
		t.Fatalf("waiting: %+v", st)
	}
	// Nor while it's being recorded.
	playing = false
	rid := f.exec(`INSERT INTO dvr_recordings (user_id, channel_name, start, stop, title, status, path, created_at) VALUES (1, 'C', 'a', 'b', 'T', 'recording', ?, 'x')`, path)
	if _, err := s.process(ctx, mustFile(t, s, file), true); !errors.Is(err, ErrBusy) {
		t.Fatalf("while recording: %v", err)
	}
	f.exec(`UPDATE dvr_recordings SET status = 'completed' WHERE id = ?`, rid)

	if _, err := s.process(ctx, mustFile(t, s, file), true); err != nil {
		t.Fatalf("cut: %v", err)
	}
	st, _ := s.Status(ctx, file)
	// The clip's keyframes sit a little after each even second (40.064 s, 80.107 s): the cut
	// resumes on the one just before the reported end, not the one after (42 s) and not an
	// earlier one inside the adverts.
	if st.Status != StatusCut || len(st.Breaks) != 2 || !st.CanRestore ||
		st.Breaks[0].StartMS != 20_000 || st.Breaks[0].EndMS < 40_000 || st.Breaks[0].EndMS > 40_900 ||
		st.Breaks[1].StartMS != 60_000 || st.Breaks[1].EndMS < 80_000 || st.Breaks[1].EndMS > 80_900 ||
		st.RemovedMS != st.Breaks[0].EndMS-20_000+st.Breaks[1].EndMS-60_000 {
		t.Fatalf("cut status: %+v", st)
	}

	// The cut file: 55 s, every stream kept, audio and video in step.
	pi, err := s.probe(ctx, path)
	if err != nil || math.Abs(float64(pi.DurationMS-(95_000-st.RemovedMS))) > 1_100 || pi.Video != 1 || pi.Audio != 2 || pi.Subs != 1 {
		t.Fatalf("cut file: %+v %v", pi, err)
	}
	first, last, kinds := streamEnds(t, fp, path)
	var v, a []int
	for i, k := range kinds {
		switch k {
		case "video":
			v = append(v, i)
		case "audio":
			a = append(a, i)
		}
	}
	for _, ai := range a {
		if d := math.Abs(first[ai] - first[v[0]]); d > 0.1 {
			t.Errorf("audio %d starts %.3fs from the video", ai, d)
		}
		if d := math.Abs(last[ai] - last[v[0]]); d > 0.15 {
			t.Errorf("audio %d ends %.3fs from the video (%.3f vs %.3f)", ai, d, last[ai], last[v[0]])
		}
	}

	// Same item, same file record, watch state kept and moved with the cut; stale markers
	// gone; the library rescanned.
	var gotItem, offset, plays, size int64
	var gotPath string
	f.db.QueryRow(`SELECT v.item_id, f.path, f.size FROM media_files f JOIN media_versions v ON v.id = f.version_id WHERE f.id = ?`, file).Scan(&gotItem, &gotPath, &size)
	f.db.QueryRow(`SELECT view_offset_ms, play_count FROM user_item_state WHERE item_id = ?`, item).Scan(&offset, &plays)
	if gotItem != item || gotPath != path || size != -1 {
		t.Errorf("record: item %d path %s size %d", gotItem, gotPath, size)
	}
	if want := 50_000 - (st.Breaks[0].EndMS - 20_000); offset != want || plays != 2 {
		t.Errorf("watch state: offset %d plays %d", offset, plays)
	}
	var markers int
	f.db.QueryRow(`SELECT COUNT(*) FROM markers WHERE file_id = ?`, file).Scan(&markers)
	if markers != 0 || f.rescans.Load() != f.lib {
		t.Errorf("markers %d, rescanned %d", markers, f.rescans.Load())
	}

	// The original is in the media trash, untouched.
	var original string
	f.db.QueryRow(`SELECT original FROM commercial_scans WHERE file_id = ?`, file).Scan(&original)
	if !strings.Contains(original, library.TrashDir) || hashFile(t, original) != origHash {
		t.Fatalf("original in the trash: %q", original)
	}
	// Never cut twice.
	if _, err := s.process(ctx, mustFile(t, s, file), true); err != nil {
		t.Fatal(err)
	}
	if st, _ := s.Status(ctx, file); st.Status != StatusCut {
		t.Errorf("after a second request: %+v", st)
	}

	// Not restored while playing; then put back, the cut file going to the trash.
	playing = true
	if _, err := s.Restore(ctx, file); !errors.Is(err, ErrBusy) {
		t.Errorf("restore while playing: %v", err)
	}
	playing = false
	cutHash := hashFile(t, path)
	st, err = s.Restore(ctx, file)
	if err != nil || st.Status != StatusRestored || st.CanRestore {
		t.Fatalf("restore: %+v %v", st, err)
	}
	if hashFile(t, path) != origHash {
		t.Error("restored file isn't the original")
	}
	if _, err := os.Stat(original); !os.IsNotExist(err) {
		t.Error("original still in the trash")
	}
	foundCut := false
	filepath.Walk(filepath.Join(f.rec, "TV", library.TrashDir), func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() && hashFile(t, p) == cutHash {
			foundCut = true
		}
		return nil
	})
	if !foundCut {
		t.Error("the cut file isn't in the trash")
	}
	f.db.QueryRow(`SELECT view_offset_ms FROM user_item_state WHERE item_id = ?`, item).Scan(&offset)
	if offset != 50_000 {
		t.Errorf("watch position after restore: %d", offset)
	}
	if _, err := s.Restore(ctx, file); !errors.Is(err, ErrCantRestore) {
		t.Errorf("restoring twice: %v", err)
	}
}

func mustFile(t *testing.T, s *Service, id int64) file {
	t.Helper()
	f, err := s.file(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// A cut that doesn't check out leaves the recording exactly as it was.
func TestCutVerificationFailureKeepsOriginal(t *testing.T) {
	ff, fp := tools(t)
	f := newFixture(t)
	ctx := context.Background()
	path := filepath.Join(f.rec, "TV", "Show", "a.mkv")
	makeClip(t, ff, path)
	origHash := hashFile(t, path)
	_, file := f.addFile(path, 95_000)
	// An FFmpeg that writes a broken file.
	bad := filepath.Join(f.dir, "ffmpeg-bad")
	os.WriteFile(bad, []byte("#!/bin/sh\nfor a in \"$@\"; do last=\"$a\"; done\necho junk > \"$last\"\n"), 0o755)
	s := f.service(f.comskip(`20.00\t40.00\t0\n`, 0), settings.CommercialsRemove)
	s.FFmpeg, s.FFprobe = bad, fp
	if _, err := s.process(ctx, mustFile(t, s, file), true); err == nil {
		t.Fatal("a broken cut was accepted")
	}
	st, _ := s.Status(ctx, file)
	if st.Status != StatusFailed || !strings.Contains(st.Error, "unchanged") || len(st.Breaks) != 1 {
		t.Errorf("status: %+v", st)
	}
	if hashFile(t, path) != origHash {
		t.Error("the recording changed")
	}
	if _, err := os.Stat(filepath.Join(f.rec, "TV", library.TrashDir)); !os.IsNotExist(err) {
		t.Error("something went to the trash")
	}
	if _, err := os.Stat(filepath.Join(f.rec, ".commercials")); err == nil {
		if left, _ := os.ReadDir(filepath.Join(f.rec, ".commercials")); len(left) > 0 {
			t.Error("work files left behind")
		}
	}
}

// Stream copy can't cut where the keyframes don't allow it: the fallback re-encodes, with
// exact cuts.
func TestCutReencodeFallback(t *testing.T) {
	ff, fp := tools(t)
	f := newFixture(t)
	ctx := context.Background()
	path := filepath.Join(f.rec, "TV", "Show", "a.mkv")
	makeClip(t, ff, path)
	_, file := f.addFile(path, 95_000)
	// A break of under a GOP (20.5–21.9 s, no keyframe inside) next to a normal one.
	s := f.service(f.comskip(`20.50\t21.90\t0\n60.00\t80.00\t0\n`, 0), settings.CommercialsRemove)
	s.FFmpeg, s.FFprobe = ff, fp
	f2 := mustFile(t, s, file)
	s.saveDetected(ctx, f2, []Break{{20_500, 21_900}, {60_000, 80_000}}) // Normalize would drop the short one
	if _, err := s.process(ctx, f2, true); err != nil {
		t.Fatalf("cut: %v", err)
	}
	st, _ := s.Status(ctx, file)
	if st.Status != StatusCut || st.RemovedMS != 21_400 {
		t.Fatalf("status %+v", st)
	}
	pi, _ := s.probe(ctx, path)
	if math.Abs(float64(pi.DurationMS-73_600)) > 1_500 || pi.Audio != 2 || pi.Subs != 1 {
		t.Errorf("re-encoded: %+v", pi)
	}
}

// TestRealComskip runs the real Comskip (e.g. inside the Docker image) on a copy of the clip
// in MARQUEE_TEST_COMSKIP_CLIP and, with MARQUEE_TEST_FFMPEG and MARQUEE_TEST_FFPROBE set,
// cuts the commercials out of it.
func TestRealComskip(t *testing.T) {
	clip := os.Getenv("MARQUEE_TEST_COMSKIP_CLIP")
	if clip == "" {
		t.Skip("set MARQUEE_TEST_COMSKIP_CLIP to run Comskip on a recording")
	}
	f := newFixture(t)
	path := filepath.Join(f.rec, "TV", "Clip", "clip.mkv")
	os.MkdirAll(filepath.Dir(path), 0o755)
	if b, err := exec.Command("cp", clip, path).CombinedOutput(); err != nil {
		t.Fatalf("copy: %v %s", err, b)
	}
	s := &Service{DB: f.db, Comskip: "comskip", INI: os.Getenv("MARQUEE_COMSKIP_INI"), Dir: f.rec,
		FFmpeg: os.Getenv("MARQUEE_TEST_FFMPEG"), FFprobe: os.Getenv("MARQUEE_TEST_FFPROBE"), Trash: &mediatrash.Service{DB: f.db}}
	if !s.Available() {
		t.Fatal("comskip isn't on PATH")
	}
	cut := s.FFmpeg != ""
	var dur int64
	if cut {
		pi, err := s.probe(context.Background(), path)
		if err != nil {
			t.Fatal(err)
		}
		dur = pi.DurationMS
	}
	_, file := f.addFile(path, dur)
	start := time.Now()
	n, err := s.process(context.Background(), mustFile(t, s, file), cut)
	st, _ := s.Status(context.Background(), file)
	t.Logf("breaks=%d err=%v took=%s status=%+v", n, err, time.Since(start).Round(time.Millisecond), st)
	if err != nil {
		t.Fatal(err)
	}
	if cut {
		pi, err := s.probe(context.Background(), path)
		t.Logf("cut file: %+v %v (was %d ms)", pi, err, dur)
		if st.Status != StatusCut || err != nil || math.Abs(float64(pi.DurationMS-(dur-st.RemovedMS))) > float64(dur)/50 {
			t.Fatalf("cut: %+v", st)
		}
	}
}
