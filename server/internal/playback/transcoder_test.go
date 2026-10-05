package playback

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

func ffmpegPath(t *testing.T) string {
	p, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	return p
}

// makeSample writes a 40 s HEVC MKV with stereo AC3 audio (needs transcoding for browsers).
func makeSample(t *testing.T, ff, dir string) string {
	p := filepath.Join(dir, "sample.mkv")
	out, err := exec.Command(ff, "-hide_banner", "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=640x360:rate=24:duration=40",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=40", "-c:v", "libx265", "-preset", "ultrafast", "-x265-params", "log-level=none",
		"-c:a", "ac3", "-ac", "2", p).CombinedOutput()
	if err != nil {
		t.Skipf("can't build sample: %v %s", err, out)
	}
	return p
}

func TestTranscoderSegmentsSeekAndStop(t *testing.T) {
	ff := ffmpegPath(t)
	dir := t.TempDir()
	in := makeSample(t, ff, dir)
	m := Media{Container: "mkv", DurationMS: 40_000, BitrateKbps: 800,
		Video: &VideoStream{Index: 0, Codec: "hevc", Width: 640, Height: 360, BitDepth: 8}, Audio: &AudioStream{Index: 1, Codec: "ac3", Channels: 2}}
	d := Decide(m, chrome, Limits{})
	if d.Method != Transcode {
		t.Fatalf("expected transcode: %+v", d)
	}
	tr := &Transcoder{FFmpeg: ff, Job: Job{Input: in, Decision: d, VideoIndex: 0, AudioIndex: 1, SubIndex: -1, Dir: filepath.Join(dir, "s"), Preset: "speed"},
		Encoders:      []string{"nvenc", "software"}, // nvenc fails on this machine → exercises the fallback
		TotalSegments: SegmentCount(m.DurationMS), ThrottleAhead: 20}
	defer tr.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	initPath, err := tr.Init(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(initPath); st == nil || st.Size() == 0 {
		t.Fatal("init segment must be complete when returned")
	}
	if _, err := tr.Segment(ctx, 0); err != nil {
		t.Fatal(err)
	}
	if got := tr.Encoder(); got != "software" && got != "nvenc" {
		t.Fatalf("encoder %s", got)
	}
	// Jump to the last segment: FFmpeg restarts there, timestamps continue from 36 s.
	p, err := tr.Segment(ctx, 6)
	if err != nil {
		t.Fatal(err)
	}
	out, _ := exec.Command("ffprobe", "-v", "error", "-show_entries", "packet=pts_time", "-of", "csv=p=0", "-read_intervals", "%+#1",
		"-i", "concat:"+filepath.Join(tr.Job.Dir, "init.mp4")+"|"+p).Output()
	first, _ := strconv.ParseFloat(strings.TrimSpace(strings.Split(string(out), "\n")[0]), 64)
	if first < 35.5 || first > 36.5 {
		t.Errorf("segment 6 should start at 36 s, starts at %.3f (%s)", first, out)
	}
	if !strings.Contains(string(Playlist(m.DurationMS)), "6.m4s") || SegmentCount(m.DurationMS) != 7 {
		t.Error("playlist")
	}
	tr.Stop()
	if _, err := os.Stat(tr.Job.Dir); !os.IsNotExist(err) {
		t.Error("stop should delete the session's files")
	}
}

// An Apple TV playing an MKV whose image subtitle must be burned in: the video is
// transcoded and the EAC3 audio copied into fMP4, which FFmpeg only accepts with delay_moov.
func TestTranscoderCopiesEAC3(t *testing.T) {
	ff := ffmpegPath(t)
	dir := t.TempDir()
	in := filepath.Join(dir, "eac3.mkv")
	if out, err := exec.Command(ff, "-hide_banner", "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=640x360:rate=24:duration=8",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=8", "-c:v", "libx265", "-preset", "ultrafast", "-x265-params", "log-level=none",
		"-c:a", "eac3", "-ac", "6", in).CombinedOutput(); err != nil {
		t.Skipf("can't build sample: %v %s", err, out)
	}
	d := Decision{Method: Transcode, VideoCodec: "h264", Height: 360, VideoKbps: 2000, AudioCopy: true}
	tr := &Transcoder{FFmpeg: ff, Job: Job{Input: in, Decision: d, VideoIndex: 0, AudioIndex: 1, SubIndex: -1, Dir: filepath.Join(dir, "s"), Preset: "speed"},
		Encoders: []string{"software"}, TotalSegments: SegmentCount(8_000), ThrottleAhead: 20}
	defer tr.Stop()
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if _, err := tr.Init(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := tr.Segment(ctx, 0); err != nil {
		t.Fatal(err)
	}
}
