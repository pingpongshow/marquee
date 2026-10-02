package playback

import (
	"bytes"
	"context"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// makeGOPSample writes a 30 s H.264 file with a keyframe every 2 s (B-frames on) and AAC.
func makeGOPSample(t *testing.T, ff, dir, ext string) string {
	p := filepath.Join(dir, "gop."+ext)
	out, err := exec.Command(ff, "-hide_banner", "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=320x180:rate=24:duration=30",
		"-f", "lavfi", "-i", "sine=frequency=330:duration=30", "-c:v", "libx264", "-preset", "ultrafast", "-g", "48", "-keyint_min", "48",
		"-sc_threshold", "0", "-bf", "2", "-c:a", "aac", "-shortest", p).CombinedOutput()
	if err != nil {
		t.Skipf("can't build sample: %v %s", err, out)
	}
	return p
}

func TestKeyframeIndexes(t *testing.T) {
	ff := ffmpegPath(t)
	dir := t.TempDir()
	for _, ext := range []string{"mkv", "mp4"} {
		path := makeGOPSample(t, ff, dir, ext)
		var kf []float64
		var err error
		if ext == "mkv" {
			kf, err = matroskaKeyframes(path)
		} else {
			kf, err = mp4Keyframes(path)
		}
		if err != nil {
			t.Fatalf("%s: %v", ext, err)
		}
		if len(kf) != 15 {
			t.Fatalf("%s: %d keyframes %v", ext, len(kf), kf)
		}
		for i, k := range kf {
			if math.Abs(k-float64(2*i)) > 0.2 {
				t.Fatalf("%s: keyframe %d at %.3f", ext, i, k)
			}
		}
	}
	plan := KeyframePlan([]float64{0, 2, 4, 6, 8, 10, 12.5, 30, 31}, 31.2, 6)
	if want := []float64{0, 6, 12.5, 30}; len(plan) != len(want) || plan[2] != 12.5 {
		t.Fatalf("plan %v", plan)
	}
}

// Copied video cut along a keyframe plan: a segment made after a seek restart must be
// identical to the same segment from an uninterrupted run (AVPlayer needs this).
func TestCopySegmentsMatchAcrossRestarts(t *testing.T) {
	ff := ffmpegPath(t)
	dir := t.TempDir()
	in := makeGOPSample(t, ff, dir, "mkv")
	kf, err := matroskaKeyframes(in)
	if err != nil {
		t.Fatal(err)
	}
	m := Media{Container: "mkv", DurationMS: 30_000, BitrateKbps: 500,
		Video: &VideoStream{Index: 0, Codec: "h264", Width: 320, Height: 180, BitDepth: 8}, Audio: &AudioStream{Index: 1, Codec: "aac", Channels: 1}}
	d := Decide(m, appleTV, Limits{})
	if d.Method != DirectStream {
		t.Fatalf("expected direct stream: %+v", d)
	}
	plan := KeyframePlan(kf, 30, SegmentSeconds)
	run := func(name string, from int) map[int][]byte {
		tr := &Transcoder{FFmpeg: ff, Job: Job{Input: in, Decision: d, VideoIndex: 0, AudioIndex: 1, SubIndex: -1, Dir: filepath.Join(dir, name), Plan: plan},
			Encoders: []string{"software"}, TotalSegments: len(plan), ThrottleAhead: 50}
		defer tr.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		got := map[int][]byte{}
		for k := from; k < len(plan); k++ {
			p, err := tr.Segment(ctx, k)
			if err != nil {
				t.Fatalf("%s segment %d: %v", name, k, err)
			}
			b, _ := os.ReadFile(p)
			got[k] = b
		}
		got[-1], _ = os.ReadFile(filepath.Join(dir, name, "init.mp4"))
		return got
	}
	full := run("full", 0)
	restarted := run("restart", 3)
	if len(full) != len(plan)+1 {
		t.Fatalf("segments %d, plan %d", len(full), len(plan))
	}
	for k := 3; k < len(plan); k++ {
		if !bytes.Equal(full[k], restarted[k]) {
			t.Errorf("segment %d differs after restart (%d vs %d bytes)", k, len(full[k]), len(restarted[k]))
		}
	}
	// Each segment starts at its planned keyframe.
	for k := range plan {
		var trackID, timescale uint32 = 1, 0
		r := bytes.NewReader(full[-1])
		for {
			b, err := readBox(r)
			if err != nil {
				break
			}
			if b.typ == "moov" {
				trackID, timescale, _ = videoTrack(b.data)
			}
		}
		b, err := readBox(bytes.NewReader(full[k]))
		if err != nil || b.typ != "moof" {
			t.Fatalf("segment %d starts with %q: %v", k, b.typ, err)
		}
		at, _ := fragmentTime(b.data, trackID, timescale)
		if math.Abs(at-plan[k]) > 0.2 {
			t.Errorf("segment %d starts at %.3f, plan %.3f", k, at, plan[k])
		}
	}
}
