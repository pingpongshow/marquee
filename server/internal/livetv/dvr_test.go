package livetv

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestDVRScheduling(t *testing.T) {
	s, _, uid := setup(t)
	ctx := context.Background()
	s.Refresh(ctx)
	r := &Recorder{Live: s, Dir: t.TempDir()}
	list, _ := s.Channels(ctx, uid, Filter{})
	news := list[0]
	weather := news.Next

	rec, _, err := r.Schedule(ctx, uid, news.ID, weather.Start, false, false)
	if err != nil || rec == nil || rec.Status != StatusScheduled || rec.Title != "Weather" || rec.Episode != "S2 E5" {
		t.Fatalf("schedule: %+v %v", rec, err)
	}
	if _, _, err := r.Schedule(ctx, uid, news.ID, weather.Start.Add(time.Minute), false, false); err != ErrProgrammeNotFound {
		t.Errorf("not in the guide: %v", err)
	}
	now := time.Now()
	rows, _ := s.Guide(ctx, uid, Filter{}, now.Add(-time.Hour), now.Add(3*time.Hour))
	if p := rows[0].Programmes[1]; p.Recording != StatusScheduled || p.Series {
		t.Errorf("guide mark: %+v", p)
	}

	// A series on any channel picks up every airing, once.
	_, rule, err := r.Schedule(ctx, uid, news.ID, news.Now.Start, true, true)
	if err != nil || rule == nil || rule.ChannelID != nil || rule.Upcoming != 1 {
		t.Fatalf("series: %+v %v", rule, err)
	}
	if _, again, _ := r.Schedule(ctx, uid, news.ID, news.Now.Start, true, true); again == nil || again.ID != rule.ID {
		t.Errorf("same series twice: %+v", again)
	}
	rows, _ = s.Guide(ctx, uid, Filter{}, now.Add(-time.Hour), now.Add(3*time.Hour))
	if p := rows[0].Programmes[0]; p.Recording != StatusScheduled || !p.Series {
		t.Errorf("series mark: %+v", p)
	}

	// Cancelling keeps the airing from being scheduled again by the series.
	recs, _ := r.List(ctx)
	if len(recs) != 2 {
		t.Fatalf("list: %+v", recs)
	}
	var morning Recording
	for _, x := range recs {
		if x.Title == "Morning News" {
			morning = x
		}
	}
	if morning.RuleID == nil || *morning.RuleID != rule.ID {
		t.Fatalf("rule link: %+v", morning)
	}
	if err := r.Cancel(ctx, morning.ID, false); err != nil {
		t.Fatal(err)
	}
	r.expandRules(ctx)
	if recs, _ = r.List(ctx); len(recs) != 1 {
		t.Errorf("cancelled airing came back: %+v", recs)
	}
	// A cancelled one-off can be scheduled again.
	r.Cancel(ctx, rec.ID, false)
	if again, _, err := r.Schedule(ctx, uid, news.ID, weather.Start, false, false); err != nil || again.Status != StatusScheduled || again.ID != rec.ID {
		t.Errorf("reschedule: %+v %v", again, err)
	}
	// Stopping a series drops its upcoming airings only.
	if err := r.DeleteRule(ctx, rule.ID); err != nil {
		t.Fatal(err)
	}
	if rules, _ := r.Rules(ctx); len(rules) != 0 {
		t.Errorf("rules: %+v", rules)
	}
	if recs, _ = r.List(ctx); len(recs) != 1 || recs[0].Title != "Weather" {
		t.Errorf("after stopping the series: %+v", recs)
	}

	// Upcoming recordings follow the guide: a renamed one-off is updated; a series airing
	// whose slot now holds another show is dropped.
	r.Schedule(ctx, uid, news.ID, news.Now.Start, true, false)
	s.DB.Exec(`UPDATE live_programmes SET title = 'Weather Extra', stop = ? WHERE channel_id = ? AND start = ?`,
		weather.Stop.Add(15*time.Minute).UTC().Format(time.RFC3339), news.ID, weather.Start.UTC().Format(time.RFC3339))
	s.DB.Exec(`UPDATE live_programmes SET title = 'Breaking News' WHERE channel_id = ? AND start = ?`, news.ID, news.Now.Start.UTC().Format(time.RFC3339))
	r.followGuide(ctx)
	if x, _ := r.Get(ctx, rec.ID); x.Title != "Weather Extra" || !x.Stop.Equal(weather.Stop.Add(15*time.Minute)) {
		t.Errorf("one-off follows the guide: %+v", x)
	}
	for _, x := range must(r.List(ctx)) {
		if x.RuleID != nil {
			t.Errorf("series airing kept after the slot changed: %+v", x)
		}
	}

	// Missed recordings fail rather than start late.
	s.DB.Exec(`UPDATE dvr_recordings SET start = ?, stop = ? WHERE id = ?`,
		now.Add(-2*time.Hour).UTC().Format(time.RFC3339), now.Add(-time.Hour).UTC().Format(time.RFC3339), rec.ID)
	r.Tick(ctx)
	if x, _ := r.Get(ctx, rec.ID); x.Status != StatusFailed || !strings.Contains(x.Error, "missed") {
		t.Errorf("missed: %+v", x)
	}
}

func TestDVRNames(t *testing.T) {
	dir := t.TempDir()
	r := &Recorder{Dir: dir}
	start := time.Date(2026, 10, 2, 20, 0, 0, 0, time.Local)
	for _, c := range []struct {
		x     Recording
		want  string
		movie bool
	}{
		{Recording{Title: "Weather", Episode: "S2 E5", Subtitle: "Rain: Again?"}, "TV/Weather/Season 2/Weather - S02E05 - Rain - Again.mkv", false},
		{Recording{Title: "News/Tonight", Start: start}, "TV/News-Tonight/News-Tonight - 2026-10-02.mkv", false},
		{Recording{Title: "The Film", Category: "Movie"}, "Movies/The Film/The Film.mkv", true},
		{Recording{Title: "Drama", Category: "Movie", Episode: "S1 E1"}, "TV/Drama/Season 1/Drama - S01E01.mkv", false},
	} {
		got, movie := r.destination(c.x)
		if rel, _ := filepath.Rel(dir, got); rel != c.want || movie != c.movie {
			t.Errorf("%q: got %q %v, want %q", c.x.Title, rel, movie, c.want)
		}
	}
	// Never overwrites.
	p, _ := r.destination(Recording{Title: "The Film", Category: "Movie"})
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, []byte("x"), 0o644)
	if p2, _ := r.destination(Recording{Title: "The Film", Category: "Movie"}); !strings.HasSuffix(p2, "The Film (2).mkv") {
		t.Errorf("second copy: %s", p2)
	}
}

// TestDVRRecord records a short programme from an HTTP stream that keeps ending, so the
// recording reconnects and joins its parts (skipped without FFmpeg).
func TestDVRRecord(t *testing.T) {
	ff, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	s, _, uid := setup(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := t.TempDir()
	clip := filepath.Join(dir, "in.ts")
	if out, err := exec.Command(ff, "-v", "error", "-f", "lavfi", "-i", "testsrc=size=320x240:rate=25", "-f", "lavfi", "-i", "sine=frequency=440",
		"-t", "2", "-c:v", "libx264", "-c:a", "aac", "-f", "mpegts", clip).CombinedOutput(); err != nil {
		t.Fatalf("making clip: %v %s", err, out)
	}
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.ServeFile(w, r, clip)
	}))
	defer srv.Close()
	res, _ := s.DB.Exec(`INSERT INTO live_channels (source_id, source_key, name, stream_url) VALUES ('t', 'k', 'Test', ?)`, srv.URL+"/in.ts")
	ch, _ := res.LastInsertId()
	now := time.Now().UTC()
	res, _ = s.DB.Exec(`INSERT INTO dvr_recordings (user_id, channel_id, channel_name, start, stop, title, episode, created_at) VALUES (?, ?, 'Test', ?, ?, 'Short Show', 'S1 E2', ?)`,
		uid, ch, now.Format(time.RFC3339), now.Add(5*time.Second).Format(time.RFC3339), now.Format(time.RFC3339))
	id, _ := res.LastInsertId()

	var libDir string
	var scanned atomic.Int64
	rec := &Recorder{Live: s, FFmpeg: ff, Dir: filepath.Join(dir, "rec"),
		Library: func(_ context.Context, movies bool, d string) (int64, error) { libDir = d; return 7, nil },
		Scan:    func(id int64) { scanned.Store(id) }}
	if !rec.Available() {
		t.Fatal("folder not writable")
	}
	rec.Tick(ctx)
	if x, _ := rec.Get(ctx, id); x.Status != StatusRecording {
		t.Fatalf("not started: %+v", x)
	}
	deadline := time.Now().Add(30 * time.Second)
	var x Recording
	for time.Now().Before(deadline) {
		if x, _ = rec.Get(ctx, id); x.Status == StatusCompleted || x.Status == StatusFailed {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if x.Status != StatusCompleted || x.Size == 0 {
		t.Fatalf("recording: %+v", x)
	}
	if want := filepath.Join(rec.Dir, "TV", "Short Show", "Season 1", "Short Show - S01E02.mkv"); x.Path != want {
		t.Errorf("path %s, want %s", x.Path, want)
	}
	if hits.Load() < 2 {
		t.Errorf("expected a reconnect, got %d requests", hits.Load())
	}
	if libDir != filepath.Join(rec.Dir, "TV") || scanned.Load() != 7 {
		t.Errorf("library %q scanned %d", libDir, scanned.Load())
	}
	if _, err := os.Stat(filepath.Join(rec.Dir, ".recording", "1")); !os.IsNotExist(err) {
		t.Error("work folder left behind")
	}
	// Deleting it removes the file.
	if err := rec.Cancel(ctx, id, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(x.Path); !os.IsNotExist(err) {
		t.Error("file kept")
	}
	if _, err := rec.Get(ctx, id); err != ErrRecordingNotFound {
		t.Errorf("row kept: %v", err)
	}
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
