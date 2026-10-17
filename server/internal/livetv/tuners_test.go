package livetv

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"marquee/internal/settings"
)

// liveSource serves MPEG-TS clips at about real time, like a live channel, counting the
// requests for each path.
type liveSource struct {
	srv  *httptest.Server
	clip []byte
	secs float64
	mu   sync.Mutex
	hits map[string]int
}

func newLiveSource(t *testing.T, ff string) *liveSource {
	t.Helper()
	clip := filepath.Join(t.TempDir(), "live.ts")
	// About 2 Mbps, so a few seconds make enough to probe.
	if out, err := exec.Command(ff, "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=640x360:rate=25", "-f", "lavfi", "-i", "sine=frequency=440",
		"-t", "40", "-c:v", "libx264", "-preset", "ultrafast", "-g", "25", "-b:v", "2M", "-minrate", "2M", "-maxrate", "2M", "-bufsize", "1M",
		"-c:a", "aac", "-f", "mpegts", clip).CombinedOutput(); err != nil {
		t.Fatalf("making clip: %v %s", err, out)
	}
	b, _ := os.ReadFile(clip)
	l := &liveSource{clip: b, secs: 40, hits: map[string]int{}}
	l.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		l.mu.Lock()
		l.hits[r.URL.Path]++
		l.mu.Unlock()
		w.Header().Set("Content-Type", "video/mp2t")
		per := int(float64(len(l.clip)) / l.secs / 20) // bytes every 50 ms
		for off := 0; off < len(l.clip); off += per {
			if _, err := w.Write(l.clip[off:min(off+per, len(l.clip))]); err != nil {
				return
			}
			w.(http.Flusher).Flush()
			select {
			case <-r.Context().Done():
				return
			case <-time.After(50 * time.Millisecond):
			}
		}
	}))
	t.Cleanup(l.srv.Close)
	return l
}

func (l *liveSource) count(path string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.hits[path]
}

func tools(t *testing.T) (string, string) {
	ff, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	fp, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe not installed")
	}
	return ff, fp
}

func waitUntil(t *testing.T, what string, d time.Duration, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if ok() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// addRecording schedules a recording of channel ch from now for secs seconds.
func addRecording(t *testing.T, s *Service, uid, ch int64, title string, secs int) int64 {
	now := time.Now().UTC()
	res, err := s.DB.Exec(`INSERT INTO dvr_recordings (user_id, channel_id, channel_name, start, stop, title, created_at) VALUES (?, ?, 'Ch', ?, ?, ?, ?)`,
		uid, ch, now.Format(time.RFC3339), now.Add(time.Duration(secs)*time.Second).Format(time.RFC3339), title, now.Format(time.RFC3339))
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

type probed struct {
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
	Streams []struct {
		Type string `json:"codec_type"`
	} `json:"streams"`
}

// TestStopRecordingEarly: Stop and keep files what was recorded as a well-formed, playable
// file marked as stopped early; Stop and delete removes the partial recording and its row.
func TestStopRecordingEarly(t *testing.T) {
	ff, fp := tools(t)
	s, _, uid := setup(t)
	src := newLiveSource(t, ff)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	res, _ := s.DB.Exec(`INSERT INTO live_channels (source_id, source_key, name, stream_url) VALUES ('d', 'gold', 'Discovery', ?)`, src.srv.URL+"/gold")
	ch, _ := res.LastInsertId()
	dir := t.TempDir()
	rec := &Recorder{Live: s, FFmpeg: ff, Dir: filepath.Join(dir, "rec")}
	s.DVR = rec

	keep := addRecording(t, s, uid, ch, "Gold Rush", 600)
	rec.Tick(ctx)
	if got := rec.ActiveRecordings(); len(got) != 1 || got[0].Title != "Gold Rush" || got[0].ChannelID != ch {
		t.Fatalf("active: %+v", got)
	}
	time.Sleep(5 * time.Second)
	began := time.Now()
	if err := rec.Stop(ctx, keep, true); err != nil {
		t.Fatal(err)
	}
	t.Logf("stopped and filed in %s", time.Since(began))
	x, _ := rec.Get(ctx, keep)
	if x.Status != StatusCompleted || !x.StoppedEarly || x.Path == "" || !strings.HasSuffix(x.Path, ".mkv") {
		t.Fatalf("kept: %+v", x)
	}
	if len(rec.ActiveRecordings()) != 0 {
		t.Error("still listed as active")
	}
	out, err := exec.Command(fp, "-v", "error", "-show_entries", "format=duration:stream=codec_type", "-of", "json", x.Path).Output()
	if err != nil {
		t.Fatalf("ffprobe: %v", err)
	}
	var p probed
	json.Unmarshal(out, &p)
	dur, _ := strconv.ParseFloat(p.Format.Duration, 64)
	var video, audio bool
	for _, st := range p.Streams {
		video = video || st.Type == "video"
		audio = audio || st.Type == "audio"
	}
	// About five seconds were recorded: the whole of it, finalised with a duration.
	if dur < 2.5 || dur > 9 || !video || !audio {
		t.Errorf("stopped recording: %.1f s, video %v, audio %v (%s)", dur, video, audio, out)
	}
	// Decodes to the end without errors: not truncated.
	if msg, err := exec.Command(ff, "-v", "error", "-i", x.Path, "-f", "null", "-").CombinedOutput(); err != nil || len(strings.TrimSpace(string(msg))) > 0 {
		t.Errorf("decoding the stopped recording: %v %s", err, msg)
	}

	// Stop and delete.
	gone := addRecording(t, s, uid, ch, "Bering Sea Gold", 600)
	rec.Tick(ctx)
	waitUntil(t, "parts to be written", 20*time.Second, func() bool {
		parts, _ := filepath.Glob(filepath.Join(rec.Dir, ".recording", strconv.FormatInt(gone, 10), "part-*.ts"))
		return len(parts) > 0
	})
	time.Sleep(2 * time.Second)
	if err := rec.Stop(ctx, gone, false); err != nil {
		t.Fatal(err)
	}
	if _, err := rec.Get(ctx, gone); err != ErrRecordingNotFound {
		t.Errorf("row kept: %v", err)
	}
	if _, err := os.Stat(filepath.Join(rec.Dir, ".recording", strconv.FormatInt(gone, 10))); !os.IsNotExist(err) {
		t.Error("partial recording left behind")
	}
	if m, _ := filepath.Glob(filepath.Join(rec.Dir, "TV", "Bering Sea Gold*")); len(m) > 0 {
		t.Errorf("filed though deleted: %v", m)
	}
	if err := rec.Stop(ctx, gone, true); err != ErrNotRecording {
		t.Errorf("stopping what isn't recording: %v", err)
	}
}

// TestConnectionLimit: with a one-connection source, a recording makes the source's other
// channels unavailable (409 no_tuner when tuned), the recorded channel is watched from the
// recording's input without another request to the source, and a recording that starts
// takes the connection from a live stream on another channel.
func TestConnectionLimit(t *testing.T) {
	ff, fp := tools(t)
	s, _, uid := setup(t)
	src := newLiveSource(t, ff)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.Settings.Update(ctx, func(x *settings.Settings) error { x.Integrations.LiveTVSources[0].MaxStreams = 1; return nil })
	add := func(key, name string) Channel {
		res, _ := s.DB.Exec(`INSERT INTO live_channels (source_id, source_key, name, stream_url) VALUES ('d', ?, ?, ?)`, key, name, src.srv.URL+"/"+key)
		id, _ := res.LastInsertId()
		c, _ := s.Channel(ctx, id)
		return c
	}
	gold, news := add("gold", "Discovery"), add("news", "News One")
	dir := t.TempDir()
	s.Sessions = &Sessions{FFmpeg: ff, FFprobe: fp, Dir: filepath.Join(dir, "live")}
	rec := &Recorder{Live: s, FFmpeg: ff, Dir: filepath.Join(dir, "rec")}
	s.DVR = rec
	o := Options{VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"}}
	// VOD downloads hear of each connection about to open, already counted, before the
	// source is asked.
	var hookMu sync.Mutex
	var heard []string
	s.BeforeConnect = func(sourceID string) {
		hookMu.Lock()
		defer hookMu.Unlock()
		heard = append(heard, sourceID+" "+strconv.Itoa(s.InUse(sourceID))+" gold "+strconv.Itoa(src.count("/gold"))+" news "+strconv.Itoa(src.count("/news")))
	}

	// Nothing in use: everything is available.
	avail := s.Availability()
	if ok, _ := avail(news); !ok {
		t.Fatal("news unavailable with nothing in use")
	}

	id := addRecording(t, s, uid, gold.ID, "Gold Rush", 600)
	rec.Tick(ctx)
	waitUntil(t, "the recording to connect", 10*time.Second, func() bool { return src.count("/gold") == 1 })
	if n := s.InUse("d"); n != 1 {
		t.Errorf("in use: %d", n)
	}
	avail = s.Availability()
	if ok, why := avail(news); ok || why != "All connections are in use: recording Gold Rush" {
		t.Errorf("news: %v %q", ok, why)
	}
	if ok, _ := avail(gold); !ok {
		t.Error("the recorded channel should stay available")
	}
	if _, err := s.Tune(ctx, news, uid, o); !IsNoTuner(err) {
		t.Fatalf("tuning another channel: %v", err)
	}
	if src.count("/news") != 0 {
		t.Error("tuning an unavailable channel reached the source")
	}
	// Watching the recorded channel shares the recording's input.
	ls, err := s.Tune(ctx, gold, uid, o)
	if err != nil {
		t.Fatal(err)
	}
	if !ls.Shared {
		t.Errorf("not shared: %+v", ls)
	}
	web := httptest.NewServer(s.Sessions.Handler())
	defer web.Close()
	resp, err := http.Get(web.URL + "/api/v1/live/" + ls.ID + "/index.m3u8")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Errorf("shared playlist: %d", resp.StatusCode)
	}
	if n := src.count("/gold"); n != 1 {
		t.Errorf("watching the recorded channel made %d requests to the source, want 1 (the recording's)", n)
	}
	if n := s.InUse("d"); n != 1 {
		t.Errorf("in use with a shared stream: %d", n)
	}
	s.Sessions.Stop(ls.ID)
	if err := rec.Stop(ctx, id, false); err != nil {
		t.Fatal(err)
	}

	// A live stream on News holds the connection; a recording on Discovery takes it.
	live, err := s.Tune(ctx, news, uid, o)
	if err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.Availability()(gold); ok {
		t.Error("gold available while news holds the only connection")
	}
	id = addRecording(t, s, uid, gold.ID, "Gold Rush", 600)
	rec.Tick(ctx)
	waitUntil(t, "the live stream to be stopped", 15*time.Second, func() bool { return s.Sessions.Get(live.ID) == nil })
	resp, _ = http.Get(web.URL + "/api/v1/live/" + live.ID + "/index.m3u8")
	body := make([]byte, 200)
	n, _ := resp.Body.Read(body)
	resp.Body.Close()
	if resp.StatusCode != http.StatusGone || !strings.HasPrefix(string(body[:n]), ReasonPreempted) {
		t.Errorf("pre-empted stream: %d %q", resp.StatusCode, body[:n])
	}
	waitUntil(t, "the recording to connect", 10*time.Second, func() bool { return src.count("/gold") == 2 })
	if n := s.InUse("d"); n != 1 {
		t.Errorf("in use after pre-empting: %d", n)
	}
	rec.Stop(ctx, id, false)
	hookMu.Lock()
	defer hookMu.Unlock()
	// The first recording, the live stream on News, the second recording: each counted in
	// use, and heard of before it connected.
	if strings.Join(heard, ", ") != "d 1 gold 0 news 0, d 1 gold 1 news 0, d 1 gold 1 news "+strconv.Itoa(src.count("/news")) {
		t.Errorf("before connect: %q", heard)
	}
}
