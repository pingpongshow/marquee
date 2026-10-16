package playback

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"marquee/internal/db"
	"marquee/internal/settings"
)

const testSecret = "s3cret/pa ss"

// remoteFilm serves file at /movie/user/<secret>/1.<ext> with Range support, as an Xtream
// Codes server does, and counts the requests that asked for a range.
func remoteFilm(t *testing.T, file string) (string, *atomic.Int32) {
	t.Helper()
	var ranges atomic.Int32
	want := "/movie/user/" + testSecret + "/1" + filepath.Ext(file)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != want {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Range") != "" {
			ranges.Add(1)
		}
		http.ServeFile(w, r, file)
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/movie/user/" + strings.ReplaceAll(testSecret, " ", "%20") + "/1" + filepath.Ext(file), &ranges
}

func redactSecret(s string) string {
	for _, p := range []string{testSecret, strings.ReplaceAll(testSecret, " ", "%20")} {
		s = strings.ReplaceAll(s, p, "***")
	}
	return s
}

func externalManager(t *testing.T, ff string) *Manager {
	t.Helper()
	ffprobe, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe not installed")
	}
	dir := t.TempDir()
	database, err := db.Open(context.Background(), filepath.Join(dir, "t.db"), filepath.Join(dir, "b"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	store, err := settings.Open(context.Background(), database)
	if err != nil {
		t.Fatal(err)
	}
	store.Update(context.Background(), func(s *settings.Settings) error {
		s.Transcoder.Preset = "speed"
		s.Transcoder.EncoderOrder = []string{"software"}
		return nil
	})
	return &Manager{DB: database, Settings: store, FFmpeg: ff, RemoteFFprobe: ffprobe, TranscodeDir: filepath.Join(dir, "transcode")}
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

// startTime is the first video timestamp of an fMP4 segment, read with its init segment.
func startTime(t *testing.T, init, seg []byte) float64 {
	t.Helper()
	p := filepath.Join(t.TempDir(), "seg.mp4")
	os.WriteFile(p, append(append([]byte{}, init...), seg...), 0o644)
	out, err := exec.Command("ffprobe", "-v", "error", "-select_streams", "v:0", "-show_entries", "packet=pts_time",
		"-read_intervals", "%+#1", "-of", "csv=p=0", p).Output()
	if err != nil {
		t.Fatalf("ffprobe segment: %v", err)
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(strings.Split(string(out), "\n")[0]), 64)
	if err != nil {
		t.Fatalf("segment time %q", out)
	}
	return v
}

// An H.264 film with AC3 audio, as a remote MKV: the video is copied, cut on the keyframes
// its Cues list (read by Range requests), the audio converted to AAC, and seeking far into
// it restarts FFmpeg there.
func TestExternalCopiesVideoAndSeeks(t *testing.T) {
	ff := ffmpegPath(t)
	dir := t.TempDir()
	in := filepath.Join(dir, "film.mkv")
	if out, err := exec.Command(ff, "-hide_banner", "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=320x180:rate=24:duration=60",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=60", "-c:v", "libx264", "-preset", "ultrafast", "-g", "48",
		"-c:a", "ac3", "-ac", "2", in).CombinedOutput(); err != nil {
		t.Skipf("can't build sample: %v %s", err, out)
	}
	link, ranges := remoteFilm(t, in)
	m := externalManager(t, ff)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	s, err := m.StartExternal(ctx, ExternalRequest{UserID: 1, Title: "Film", URL: link, StartMS: 30_000, Redact: redactSecret})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop(context.Background(), s.ID)
	if !s.Decision.VideoCopy || s.Decision.AudioCopy || s.Decision.AudioCodec != "aac" {
		t.Fatalf("decision %+v", s.Decision)
	}
	if ranges.Load() == 0 || len(s.Plan) < 8 || s.Media.DurationMS < 59_000 {
		t.Fatalf("plan from the remote index: ranges=%d plan=%v duration=%d", ranges.Load(), s.Plan, s.Media.DurationMS)
	}
	if s.StartMS != 30_000 || m.transcodeCount() != 1 {
		t.Fatalf("start %d, transcodes %d", s.StartMS, m.transcodeCount())
	}

	h := m.StreamHandler(t.TempDir())
	base := StreamPrefix + s.ID + "/"
	if rec := get(t, h, base+"master.m3u8"); !strings.Contains(rec.Body.String(), "index.m3u8") ||
		!strings.Contains(rec.Body.String(), "avc1") || !strings.Contains(rec.Body.String(), "mp4a.40.2") {
		t.Fatalf("master:\n%s", rec.Body.String())
	}
	index := get(t, h, base+"index.m3u8").Body.String()
	if !strings.Contains(index, "#EXT-X-PLAYLIST-TYPE:VOD") || !strings.Contains(index, "#EXT-X-ENDLIST") ||
		strings.Count(index, ".m4s") != len(s.Plan) {
		t.Fatalf("index:\n%s", index)
	}
	// The player starts at the resume point.
	initRec := get(t, h, base+"init.mp4")
	if initRec.Code != http.StatusOK || initRec.Body.Len() == 0 {
		t.Fatalf("init: %d", initRec.Code)
	}
	k := segmentFor(s.Plan, 30)
	seg := get(t, h, base+strconv.Itoa(k)+".m4s")
	if seg.Code != http.StatusOK {
		t.Fatalf("segment %d: %d %s", k, seg.Code, seg.Body.String())
	}
	if got := startTime(t, initRec.Body.Bytes(), seg.Body.Bytes()); got < s.Plan[k]-0.1 || got > s.Plan[k]+0.1 {
		t.Fatalf("segment %d starts at %.3f, plan %.3f", k, got, s.Plan[k])
	}
	// Seeking back to the start restarts FFmpeg there.
	seg0 := get(t, h, base+"0.m4s")
	if seg0.Code != http.StatusOK {
		t.Fatalf("segment 0: %d", seg0.Code)
	}
	if got := startTime(t, initRec.Body.Bytes(), seg0.Body.Bytes()); got > 0.1 {
		t.Fatalf("segment 0 starts at %.3f", got)
	}

	// Left paused: FFmpeg ends and the session stops counting as a transcode, but it stays.
	s.mu.Lock()
	s.lastActive = time.Now().Add(-10 * time.Minute)
	s.mu.Unlock()
	m.Reap(ctx, 3*time.Minute)
	if _, ok := m.sessions[s.ID]; !ok || !s.isSuspended() || s.transcoder.running() || m.transcodeCount() != 0 {
		t.Fatalf("suspended=%v running=%v count=%d", s.isSuspended(), s.transcoder.running(), m.transcodeCount())
	}
	// …and picks up where the viewer is when they come back.
	if rec := get(t, h, base+strconv.Itoa(k+1)+".m4s"); rec.Code != http.StatusOK {
		t.Fatalf("after pause: %d", rec.Code)
	}
	if s.isSuspended() || m.transcodeCount() != 1 {
		t.Fatal("a resumed session counts again")
	}

	// The limit counts films too.
	m.Settings.Update(ctx, func(st *settings.Settings) error { st.Transcoder.MaxConcurrentTranscodes = 1; return nil })
	if _, err := m.StartExternal(ctx, ExternalRequest{URL: link}); !errors.Is(err, ErrBusy) {
		t.Fatalf("over the limit: %v", err)
	}

	dirPath := s.transcoder.Job.Dir
	m.Stop(context.Background(), s.ID)
	if _, err := os.Stat(dirPath); !os.IsNotExist(err) {
		t.Fatal("stopping removes the segments")
	}
	// Long gone: the session ends.
	s2, err := m.StartExternal(ctx, ExternalRequest{URL: link})
	if err != nil {
		t.Fatal(err)
	}
	s2.mu.Lock()
	s2.lastActive = time.Now().Add(-ExternalIdle - time.Minute)
	s2.mu.Unlock()
	m.Reap(ctx, 3*time.Minute)
	if m.Exists(s2.ID) {
		t.Fatal("an external session idle for hours ends")
	}
}

// HEVC isn't assumed: without a profile it is transcoded to H.264; a device that plays
// HEVC gets it copied.
func TestExternalTranscodesUnsupportedVideo(t *testing.T) {
	ff := ffmpegPath(t)
	dir := t.TempDir()
	in := makeSample(t, ff, dir) // HEVC + AC3 MKV
	link, _ := remoteFilm(t, in)
	m := externalManager(t, ff)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	s, err := m.StartExternal(ctx, ExternalRequest{URL: link, Redact: redactSecret})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop(context.Background(), s.ID)
	if s.Decision.Method != Transcode || s.Decision.VideoCodec != "h264" || s.Decision.AudioCodec != "aac" {
		t.Fatalf("decision %+v", s.Decision)
	}
	h := m.StreamHandler(t.TempDir())
	base := StreamPrefix + s.ID + "/"
	initRec := get(t, h, base+"init.mp4")
	seg := get(t, h, base+"3.m4s")
	if initRec.Code != http.StatusOK || seg.Code != http.StatusOK {
		t.Fatalf("init %d, segment %d", initRec.Code, seg.Code)
	}
	if got := startTime(t, initRec.Body.Bytes(), seg.Body.Bytes()); got < 3*SegmentSeconds-0.1 || got > 3*SegmentSeconds+0.1 {
		t.Fatalf("segment 3 starts at %.3f", got)
	}

	p := appleTV
	s2, err := m.StartExternal(ctx, ExternalRequest{URL: link, Profile: &p})
	if err != nil {
		t.Fatal(err)
	}
	defer m.Stop(context.Background(), s2.ID)
	if !s2.Decision.VideoCopy || !s2.Decision.AudioCopy {
		t.Fatalf("Apple TV decision %+v", s2.Decision)
	}
}

// A film that doesn't load says why without the link's password.
func TestExternalErrorsAreRedacted(t *testing.T) {
	ff := ffmpegPath(t)
	m := externalManager(t, ff)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, "no")
	}))
	defer srv.Close()
	link := srv.URL + "/movie/user/" + strings.ReplaceAll(testSecret, " ", "%20") + "/9.mkv"
	_, err := m.StartExternal(context.Background(), ExternalRequest{URL: link, Redact: redactSecret})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("err %v", err)
	}
	if strings.Contains(err.Error(), "s3cret") {
		t.Fatalf("password in %q", err)
	}
}
