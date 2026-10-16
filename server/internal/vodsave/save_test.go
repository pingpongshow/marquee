package vodsave

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"marquee/internal/settings"
)

// A film is downloaded, verified and moved into the movies folder under the library's
// naming; the library holding it is scanned; the checksum is stored; nothing leaks the
// source's password.
func TestSaveFilmOriginal(t *testing.T) {
	data := clip(t, "mkv")
	e := newEnv(t, oneFilm, nil)
	e.src.files["movie/101.mkv"] = data
	ctx := context.Background()
	res, err := e.s.SaveMovie(ctx, e.movieID("EN - Clip Film (2020)"), 1, false)
	if err != nil || res.Queued != 1 || len(res.Saves) != 1 {
		t.Fatalf("save: %+v %v", res, err)
	}
	if v := res.Saves[0]; v.Title != "Clip Film (2020)" || v.TargetPath != filepath.Join(e.movies, "Clip Film (2020)", "Clip Film (2020).mkv") {
		t.Fatalf("queued: %+v", v)
	}
	// Asking again doesn't queue it twice.
	if again, _ := e.s.SaveMovie(ctx, e.movieID("EN - Clip Film (2020)"), 1, false); again.Queued != 0 || again.Already != 1 {
		t.Fatalf("again: %+v", again)
	}
	e.start()
	v := e.wait(res.Saves[0].ID, Done, Failed)
	want := filepath.Join(e.movies, "Clip Film (2020)", "Clip Film (2020) WEBDL-720p.mkv")
	if v.State != Done || v.TargetPath != want || !v.Verified || v.SHA256 != sha(data) || v.BytesDone != int64(len(data)) {
		t.Fatalf("done: %+v", v)
	}
	if !strings.Contains(v.VerifyNote, "720p") {
		t.Errorf("note: %q", v.VerifyNote)
	}
	got, err := os.ReadFile(want)
	if err != nil || sha(got) != sha(data) {
		t.Fatalf("file: %v", err)
	}
	if st, _ := os.Stat(want); st.Mode().Perm() != 0o664 {
		t.Errorf("file mode %v", st.Mode().Perm())
	}
	if st, _ := os.Stat(filepath.Dir(want)); st.Mode().Perm() != 0o775 {
		t.Errorf("folder mode %v", st.Mode().Perm())
	}
	if _, err := os.Stat(want + ".part"); !os.IsNotExist(err) {
		t.Error("the .part file is left")
	}
	e.waitFor("the scan", func() bool { e.scanMu.Lock(); defer e.scanMu.Unlock(); return len(e.scans) == 1 })
	libs, _ := e.s.Libraries(ctx)
	if l, _ := libraryFor(libs, want); e.scans[0] != l.ID || l.Name != "Movies" {
		t.Errorf("scanned %v, want the Movies library", e.scans)
	}
	e.waitFor("the summary", func() bool { return len(e.drained) == 1 })
	if e.drained[0] != "1 0" {
		t.Errorf("drained: %v", e.drained)
	}
	st, _ := e.s.MovieState(ctx, e.movieID("EN - Clip Film (2020)"))
	if st.State != Done || st.Progress != 1 || !st.Verified {
		t.Errorf("state: %+v", st)
	}
	// Saving it again now skips it: the file is there.
	e.s.ClearFinished(ctx)
	if r, _ := e.s.SaveMovie(ctx, e.movieID("EN - Clip Film (2020)"), 1, false); r.Skipped != 1 || r.Saves[0].State != Skipped {
		t.Errorf("existing: %+v", r)
	}
	e.noSecrets()
}

// Saves run in queue order, "move to top" first, at most concurrentPerSource at a time.
func TestQueueOrderAndConcurrency(t *testing.T) {
	data := clip(t, "mkv")
	films := `[{"name":"A (2001)","stream_id":"1","container_extension":"mkv"},{"name":"B (2002)","stream_id":"2","container_extension":"mkv"},
		{"name":"C (2003)","stream_id":"3","container_extension":"mkv"},{"name":"D (2004)","stream_id":"4","container_extension":"mkv"}]`
	e := newEnv(t, films, nil)
	for _, id := range []string{"1", "2", "3", "4"} {
		e.src.files["movie/"+id+".mkv"] = data
	}
	e.src.delay = 5 * time.Millisecond
	ctx := context.Background()
	e.s.SetQueuePaused(ctx, true)
	var ids []int64
	for _, title := range []string{"A (2001)", "B (2002)", "C (2003)", "D (2004)"} {
		r, err := e.s.SaveMovie(ctx, e.movieID(title), 1, false)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, r.Saves[0].ID)
	}
	if err := e.s.MoveToTop(ctx, ids[2]); err != nil {
		t.Fatal(err)
	}
	list, _ := e.s.List(ctx)
	if list[0].ID != ids[2] || list[0].PauseReason != "Queue paused" {
		t.Fatalf("list: %+v", list[0])
	}
	e.start()
	time.Sleep(100 * time.Millisecond)
	if len(e.src.order) != 0 {
		t.Fatal("a paused queue downloaded")
	}
	e.s.SetQueuePaused(ctx, false)
	for _, id := range ids {
		e.wait(id, Done)
	}
	if got := strings.Join(e.src.order, ","); got != "3.mkv,1.mkv,2.mkv,4.mkv" {
		t.Errorf("order: %s", got)
	}
	if e.src.maxIn != 1 {
		t.Errorf("%d at once with concurrentPerSource 1", e.src.maxIn)
	}
}

func TestConcurrencyTwo(t *testing.T) {
	data := clip(t, "mkv")
	films := `[{"name":"A (2001)","stream_id":"1","container_extension":"mkv"},{"name":"B (2002)","stream_id":"2","container_extension":"mkv"},
		{"name":"C (2003)","stream_id":"3","container_extension":"mkv"}]`
	e := newEnv(t, films, func(v *settings.VODSave) { v.ConcurrentPerSource = 2 })
	for _, id := range []string{"1", "2", "3"} {
		e.src.files["movie/"+id+".mkv"] = data
	}
	e.src.delay = 20 * time.Millisecond
	ctx := context.Background()
	var ids []int64
	for _, title := range []string{"A (2001)", "B (2002)", "C (2003)"} {
		r, _ := e.s.SaveMovie(ctx, e.movieID(title), 1, false)
		ids = append(ids, r.Saves[0].ID)
	}
	e.start()
	for _, id := range ids {
		e.wait(id, Done)
	}
	if e.src.maxIn != 2 {
		t.Errorf("max at once %d, want 2", e.src.maxIn)
	}
}

// A dropped connection is retried with a Range request from where it stopped; the
// checksum still covers the whole file.
func TestResumeWithRange(t *testing.T) {
	data := clip(t, "mkv")
	e := newEnv(t, oneFilm, nil)
	e.src.files["movie/101.mkv"] = data
	e.src.dropAfter = int64(len(data) / 2)
	r, _ := e.s.SaveMovie(context.Background(), e.movieID("EN - Clip Film (2020)"), 1, false)
	e.start()
	v := e.wait(r.Saves[0].ID, Done, Failed)
	if v.State != Done || v.SHA256 != sha(data) {
		t.Fatalf("save: %+v", v)
	}
	if len(e.src.ranges) != 2 || e.src.ranges[0] != "" || !strings.HasPrefix(e.src.ranges[1], "bytes=") || e.src.ranges[1] == "bytes=0-" {
		t.Errorf("ranges: %q", e.src.ranges)
	}
	if v.Attempts != 1 {
		t.Errorf("attempts %d", v.Attempts)
	}
}

// A server that ignores Range sends the whole file: the download starts over cleanly.
func TestIgnoredRangeRestarts(t *testing.T) {
	data := clip(t, "mkv")
	e := newEnv(t, oneFilm, nil)
	e.src.files["movie/101.mkv"] = data
	e.src.dropAfter = int64(len(data) / 3)
	e.src.ignoreRange = true
	r, _ := e.s.SaveMovie(context.Background(), e.movieID("EN - Clip Film (2020)"), 1, false)
	e.start()
	v := e.wait(r.Saves[0].ID, Done, Failed)
	if v.State != Done || v.SHA256 != sha(data) {
		t.Fatalf("save: %+v", v)
	}
	b, _ := os.ReadFile(v.TargetPath)
	if sha(b) != sha(data) {
		t.Error("the file isn't the source's")
	}
}

// Pausing a save keeps what it downloaded; resuming continues with Range.
func TestManualPauseResume(t *testing.T) {
	data := clip(t, "mkv")
	e := newEnv(t, oneFilm, nil)
	e.src.files["movie/101.mkv"] = data
	e.src.delay = 30 * time.Millisecond
	ctx := context.Background()
	r, _ := e.s.SaveMovie(ctx, e.movieID("EN - Clip Film (2020)"), 1, false)
	id := r.Saves[0].ID
	e.start()
	e.waitFor("some bytes", func() bool {
		st, _ := os.Stat(r.Saves[0].TargetPath + ".part")
		return st != nil && st.Size() > 200<<10
	})
	if err := e.s.Pause(ctx, id); err != nil {
		t.Fatal(err)
	}
	v := e.wait(id, Paused)
	time.Sleep(100 * time.Millisecond)
	st, err := os.Stat(v.TargetPath + ".part")
	if err != nil || st.Size() == 0 {
		t.Fatalf("the partial file went: %v", err)
	}
	if v = e.save(id); v.BytesDone == 0 || v.State != Paused {
		t.Fatalf("paused: %+v", v)
	}
	e.src.mu.Lock()
	e.src.delay = 0
	e.src.mu.Unlock()
	if err := e.s.Resume(ctx, id); err != nil {
		t.Fatal(err)
	}
	v = e.wait(id, Done, Failed)
	if v.State != Done || v.SHA256 != sha(data) {
		t.Fatalf("done: %+v", v)
	}
	if last := e.src.ranges[len(e.src.ranges)-1]; !strings.HasPrefix(last, "bytes=") {
		t.Errorf("resumed without Range: %q", e.src.ranges)
	}
	if err := e.s.Resume(ctx, id); !errors.Is(err, ErrWrongState) {
		t.Errorf("resume done: %v", err)
	}
}

// Cancelling removes the partial file.
func TestCancelCleansUp(t *testing.T) {
	data := clip(t, "mkv")
	e := newEnv(t, oneFilm, nil)
	e.src.files["movie/101.mkv"] = data
	e.src.delay = 30 * time.Millisecond
	ctx := context.Background()
	r, _ := e.s.SaveMovie(ctx, e.movieID("EN - Clip Film (2020)"), 1, false)
	e.start()
	part := r.Saves[0].TargetPath + ".part"
	e.waitFor("some bytes", func() bool { st, _ := os.Stat(part); return st != nil && st.Size() > 0 })
	if err := e.s.Cancel(ctx, r.Saves[0].ID); err != nil {
		t.Fatal(err)
	}
	e.wait(r.Saves[0].ID, Cancelled)
	e.waitFor("the partial file to go", func() bool { _, err := os.Stat(part); return os.IsNotExist(err) })
}

// Network errors are retried with backoff, then the save fails, keeping its partial file;
// Retry continues from it.
func TestRetriesThenFailThenRetry(t *testing.T) {
	data := clip(t, "mkv")
	e := newEnv(t, oneFilm, nil)
	e.src.files["movie/101.mkv"] = data
	e.src.dropAfter = int64(len(data) / 2)
	ctx := context.Background()
	r, _ := e.s.SaveMovie(ctx, e.movieID("EN - Clip Film (2020)"), 1, false)
	id := r.Saves[0].ID
	// After the first half, the file answers 502 while the account still signs in.
	e.src.statusAfterDrop = 502
	e.start()
	v := e.wait(id, Failed)
	if v.Attempts != 3 || !strings.Contains(v.Error, "after 3 tries") {
		t.Fatalf("failed: %+v", v)
	}
	if st, err := os.Stat(v.TargetPath + ".part"); err != nil || st.Size() == 0 {
		t.Fatal("a failed save lost its partial file")
	}
	if len(e.failed) != 1 || e.failed[0].ID != id {
		t.Errorf("failure alert: %+v", e.failed)
	}
	e.src.mu.Lock()
	e.src.fileStatus = 0
	n := len(e.src.ranges)
	e.src.mu.Unlock()
	if err := e.s.Retry(ctx, id); err != nil {
		t.Fatal(err)
	}
	v = e.wait(id, Done, Failed)
	if v.State != Done || v.SHA256 != sha(data) {
		t.Fatalf("retried: %+v", v)
	}
	if !strings.HasPrefix(e.src.ranges[n], "bytes=") {
		t.Errorf("retry started over: %q", e.src.ranges[n:])
	}
	e.noSecrets()
}

// A source that's down pauses its queue instead of failing the saves, and they go on when
// it's back.
func TestSourceDownPauses(t *testing.T) {
	data := clip(t, "mkv")
	e := newEnv(t, oneFilm, nil)
	e.src.files["movie/101.mkv"] = data
	e.src.down.Store(true)
	ctx := context.Background()
	r, _ := e.s.SaveMovie(ctx, e.movieID("EN - Clip Film (2020)"), 1, false)
	id := r.Saves[0].ID
	e.start()
	e.waitFor("the source to count as down", func() bool {
		list, _ := e.s.List(ctx)
		return len(list) == 1 && strings.HasPrefix(list[0].PauseReason, "Source unreachable")
	})
	time.Sleep(400 * time.Millisecond) // several waits
	if v := e.save(id); v.State != Queued || v.Attempts != 0 {
		t.Fatalf("while down: %+v", v)
	}
	e.src.down.Store(false)
	if v := e.wait(id, Done, Failed); v.State != Done {
		t.Fatalf("after: %+v", v)
	}
	// Connection refused (the server gone) counts the same, and leaks nothing.
	e.src.srv.Close()
	os.RemoveAll(filepath.Dir(e.save(id).TargetPath))
	e.s.ClearFinished(ctx)
	r, _ = e.s.SaveMovie(ctx, e.movieID("EN - Clip Film (2020)"), 1, false)
	e.waitFor("refused", func() bool {
		v := e.save(r.Saves[0].ID)
		return strings.Contains(v.Error, "Source unreachable")
	})
	if v := e.save(r.Saves[0].ID); v.State == Failed {
		t.Fatalf("failed instead of waiting: %+v", v)
	}
	e.noSecrets()
}

// Saves pause while something from the same source (or the same host's Live TV) plays and
// resume after the grace period.
func TestAutoPauseWhileWatching(t *testing.T) {
	data := clip(t, "mkv")
	e := newEnv(t, oneFilm, nil)
	e.src.files["movie/101.mkv"] = data
	e.src.delay = 30 * time.Millisecond
	ctx := context.Background()
	r, _ := e.s.SaveMovie(ctx, e.movieID("EN - Clip Film (2020)"), 1, false)
	id := r.Saves[0].ID
	e.start()
	part := r.Saves[0].TargetPath + ".part"
	e.waitFor("some bytes", func() bool { st, _ := os.Stat(part); return st != nil && st.Size() > 0 })

	// Library playback doesn't pause saves with scope "source"; another host's Live TV neither.
	e.playing.Store([]Playing{{Kind: "library"}, {Kind: "live", Host: "elsewhere.test"}})
	time.Sleep(100 * time.Millisecond)
	if v := e.save(id); v.State != Downloading {
		t.Fatalf("paused for unrelated playback: %+v", v)
	}
	e.playing.Store([]Playing{{Kind: "live", Host: "127.0.0.1"}})
	e.waitFor("the auto-pause", func() bool {
		list, _ := e.s.List(ctx)
		return list[0].State == Queued && list[0].PauseReason == "Paused while Live TV is playing"
	})
	before := e.save(id).BytesDone
	if before == 0 {
		t.Error("progress was lost")
	}
	stopped := time.Now()
	e.playing.Store([]Playing(nil))
	e.waitFor("the resume", func() bool { return e.save(id).State == Downloading })
	if d := time.Since(stopped); d < 250*time.Millisecond {
		t.Errorf("resumed after %v, before the grace period", d)
	}
	e.src.mu.Lock()
	e.src.delay = 0
	e.src.mu.Unlock()
	v := e.wait(id, Done, Failed)
	if v.State != Done || v.SHA256 != sha(data) {
		t.Fatalf("done: %+v", v)
	}
	if !strings.HasPrefix(e.src.ranges[len(e.src.ranges)-1], "bytes=") {
		t.Errorf("didn't resume with Range: %q", e.src.ranges)
	}
}

func TestPauseReasons(t *testing.T) {
	vodHere := []Playing{{Kind: "vod", SourceID: "xc", Host: "dispatcharr.lan"}}
	if r := pauseReasonFor("source", "xc", "dispatcharr.lan", vodHere); r == "" {
		t.Error("same source")
	}
	if r := pauseReasonFor("source", "other", "other.lan", vodHere); r != "" {
		t.Errorf("other source: %q", r)
	}
	if r := pauseReasonFor("source", "xc2", "dispatcharr.lan", vodHere); r == "" {
		t.Error("another source on the same Dispatcharr")
	}
	if r := pauseReasonFor("any", "other", "other.lan", []Playing{{Kind: "library"}}); r != "Paused while something is playing" {
		t.Errorf("any: %q", r)
	}
	if r := pauseReasonFor("source", "xc", "dispatcharr.lan", nil); r != "" {
		t.Errorf("nothing playing: %q", r)
	}
}

// Verification catches a truncated file (the source's own, so the size matches) and a
// corrupt one; each is downloaded once more, then fails with the reason.
func TestVerificationFailures(t *testing.T) {
	data := clip(t, "mkv")
	for _, tc := range []struct {
		name string
		file []byte
		want string
	}{
		{"truncated", data[:len(data)*55/100], "the end doesn't decode"},
		{"corrupt", append(append([]byte{}, make([]byte, 8192)...), data[8192:]...), "ffprobe couldn't read it"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, oneFilm, nil)
			e.src.files["movie/101.mkv"] = tc.file
			r, _ := e.s.SaveMovie(context.Background(), e.movieID("EN - Clip Film (2020)"), 1, false)
			e.start()
			v := e.wait(r.Saves[0].ID, Failed, Done)
			if v.State != Failed || !strings.Contains(v.Error, "Verification failed") || !strings.Contains(v.Error, tc.want) {
				t.Fatalf("save: %+v", v)
			}
			if len(e.src.order) != 2 {
				t.Errorf("downloads: %d, want 2 (one more after the first failed)", len(e.src.order))
			}
			if _, err := os.Stat(filepath.Dir(v.TargetPath)); err == nil {
				entries, _ := os.ReadDir(filepath.Dir(v.TargetPath))
				if len(entries) != 0 {
					t.Errorf("left behind: %v", entries)
				}
			}
		})
	}
}

// The film's runtime from the source is checked.
func TestVerificationDuration(t *testing.T) {
	data := clip(t, "mkv")
	e := newEnv(t, oneFilm, nil)
	e.src.files["movie/101.mkv"] = data
	id := e.movieID("EN - Clip Film (2020)")
	e.s.DB.Exec("UPDATE vod_movies SET runtime_ms=? WHERE id=?", 600_000, id) // the source says 10 minutes
	r, _ := e.s.SaveMovie(context.Background(), id, 1, false)
	e.start()
	v := e.wait(r.Saves[0].ID, Failed, Done)
	if v.State != Failed || !strings.Contains(v.Error, "expected 0:10:00") {
		t.Fatalf("save: %+v", v)
	}
}

func TestDiskSpace(t *testing.T) {
	data := clip(t, "mkv")
	e := newEnv(t, oneFilm, nil)
	e.src.files["movie/101.mkv"] = data
	e.s.FreeSpace = func(string) (uint64, error) { return 1 << 20, nil }
	r, _ := e.s.SaveMovie(context.Background(), e.movieID("EN - Clip Film (2020)"), 1, false)
	e.start()
	v := e.wait(r.Saves[0].ID, Failed, Done)
	if v.State != Failed || !strings.Contains(v.Error, "not enough disk space") {
		t.Fatalf("save: %+v", v)
	}
}

func TestNoFolderAndNotSavable(t *testing.T) {
	e := newEnv(t, oneFilm, func(v *settings.VODSave) { v.MoviesFolder = "" })
	if _, err := e.s.SaveMovie(context.Background(), e.movieID("EN - Clip Film (2020)"), 1, false); !errors.Is(err, ErrNoFolder) {
		t.Errorf("no folder: %v", err)
	}
	if _, err := e.s.SaveMovie(context.Background(), 9999, 1, false); !errors.Is(err, ErrNoFolder) && !errors.Is(err, ErrNotFound) {
		t.Errorf("missing: %v", err)
	}
}
