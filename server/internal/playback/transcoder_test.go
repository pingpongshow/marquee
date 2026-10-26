package playback

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
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
	tr := &Transcoder{FFmpeg: ff, Job: Job{Input: in, Decision: d, VideoIndex: 0, AudioIndex: 1, AudioCodec: "eac3", SubIndex: -1, Dir: filepath.Join(dir, "s"), Preset: "speed"},
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

// delay_moov only for copied AC3/EAC3: elsewhere its edit list shifted timestamps differently
// after each restart and copied HEVC jumped at segment boundaries.
func TestDelayMoovOnlyForDolbyCopy(t *testing.T) {
	for _, c := range []struct {
		codec string
		copy  bool
		want  bool
	}{{"eac3", true, true}, {"ac3", true, true}, {"aac", true, false}, {"eac3", false, false}} {
		j := Job{Input: "in.mkv", Decision: Decision{Method: DirectStream, VideoCopy: true, AudioCopy: c.copy, AudioCodec: "aac", AudioChannels: 2},
			VideoIndex: 0, AudioIndex: 1, AudioCodec: c.codec, VideoCodec: "hevc", SubIndex: -1, Encoder: "software"}
		got := strings.Contains(strings.Join(j.Args(), " "), "delay_moov")
		if got != c.want {
			t.Errorf("%s copy=%v: delay_moov %v, want %v", c.codec, c.copy, got, c.want)
		}
	}
}

// TestNightModeRealFFmpeg runs night mode's command lines on short surround and stereo
// clips: the filter chain must be accepted and give stereo AAC with the video copied.
func TestNightModeRealFFmpeg(t *testing.T) {
	ff := ffmpegPath(t)
	dir := t.TempDir()
	for _, tc := range []struct {
		layout string
		ch     int
		offset int
	}{{"5.1(side)", 6, 0}, {"7.1", 8, 0}, {"5.1", 6, 250}, {"stereo", 2, 0}, {"mono", 1, 0}} {
		in := filepath.Join(dir, "in"+strconv.Itoa(tc.ch)+".mkv")
		out, err := exec.Command(ff, "-hide_banner", "-v", "error", "-y", "-f", "lavfi", "-i", "testsrc2=size=320x180:rate=24:duration=3",
			"-f", "lavfi", "-i", "sine=frequency=440:duration=3,aformat=channel_layouts=mono,pan="+tc.layout+"|c0=c0",
			"-c:v", "libx264", "-preset", "ultrafast", "-g", "24", "-c:a", "ac3", in).CombinedOutput()
		if err != nil {
			t.Skipf("can't build sample: %v %s", err, out)
		}
		m := Media{Container: "mkv", DurationMS: 3000, BitrateKbps: 500,
			Video: &VideoStream{Index: 0, Codec: "h264", Width: 320, Height: 180, BitDepth: 8}, Audio: &AudioStream{Index: 1, Codec: "ac3", Channels: tc.ch}}
		d := Decide(m, appleTV, Limits{NightMode: true, Resync: tc.offset != 0})
		j := Job{Input: in, Decision: d, VideoIndex: 0, AudioIndex: 1, AudioChannels: tc.ch, SubIndex: -1, Dir: dir, Encoder: "software", AudioOffsetMS: tc.offset}
		dst := filepath.Join(dir, "out.mp4")
		f, _ := os.Create(dst)
		cmd := exec.Command(ff, j.Args()...)
		var msg strings.Builder
		cmd.Stdout, cmd.Stderr = f, &msg
		err = cmd.Run()
		f.Close()
		if err != nil {
			t.Fatalf("%s: ffmpeg failed: %v %s\n%s", tc.layout, err, msg.String(), strings.Join(j.Args(), " "))
		}
		if msg.Len() > 0 {
			t.Logf("%s: %s", tc.layout, msg.String())
		}
		probe, _ := exec.Command("ffprobe", "-v", "error", "-show_entries", "stream=codec_name,channels", "-of", "csv=p=0", dst).Output()
		if got := strings.Fields(string(probe)); !slices.Equal(got, []string{"h264", "aac,2"}) {
			t.Errorf("%s: output streams %q", tc.layout, probe)
		}
	}
}

// A file a few seconds shorter than its stated length (a remote one whose source overstates
// it): the playlist's last segment is past where FFmpeg ends, and comes back as ErrPastEnd
// (served empty, so the player ends there) rather than a failure. Far short, it's still one.
func TestTranscoderPastTheEnd(t *testing.T) {
	ff := ffmpegPath(t)
	dir := t.TempDir()
	in := makeSample(t, ff, dir) // 40 s
	m := Media{Container: "mkv", DurationMS: 40_000, BitrateKbps: 800,
		Video: &VideoStream{Index: 0, Codec: "hevc", Width: 640, Height: 360, BitDepth: 8}, Audio: &AudioStream{Index: 1, Codec: "ac3", Channels: 2}}
	d := Decide(m, chrome, Limits{})
	run := func(name string, statedMS int64, k int) error {
		t.Helper()
		tr := &Transcoder{FFmpeg: ff, Job: Job{Input: in, Decision: d, VideoIndex: 0, AudioIndex: 1, SubIndex: -1, Dir: filepath.Join(dir, name), Preset: "speed"},
			Encoders: []string{"software"}, TotalSegments: SegmentCount(statedMS), ThrottleAhead: 20}
		defer tr.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if _, err := tr.Init(ctx); err != nil {
			t.Fatal(err)
		}
		if _, err := tr.Segment(ctx, 6); err != nil { // 36-40 s: the file's last
			t.Fatalf("%s: segment 6: %v", name, err)
		}
		_, err := tr.Segment(ctx, k)
		return err
	}
	// Stated 46 s: segments 0-7, the file has 0-6.
	if err := run("short", 46_000, 7); !errors.Is(err, ErrPastEnd) {
		t.Errorf("46 s stated: segment 7: %v, want ErrPastEnd", err)
	}
	// Stated 140 s: well past the slack, so the missing segments are a failure.
	if err := run("cut", 140_000, 7); err == nil || errors.Is(err, ErrPastEnd) {
		t.Errorf("140 s stated: segment 7: %v, want a failure", err)
	}
}
