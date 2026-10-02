package loudness

import (
	"context"
	"math"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestFromTags(t *testing.T) {
	g := FromTags(`{"format":{"tags":{"REPLAYGAIN_TRACK_GAIN":"-7.32 dB","replaygain_album_gain":"-6.10 dB","REPLAYGAIN_TRACK_PEAK":"0.988"}}}`)
	if g.Track == nil || *g.Track != -7.32 || g.Album == nil || *g.Album != -6.10 || g.Peak == nil || *g.Peak != 0.988 {
		t.Fatalf("%+v", g)
	}
	g = FromTags(`{"format":{"tags":{}},"streams":[{"tags":{"R128_TRACK_GAIN":"-1280"}}]}`)
	if g.Track == nil || *g.Track != 0 {
		t.Fatalf("R128 -1280/256 = -5 dB vs -23 LUFS = 0 dB vs -18: %+v", g.Track)
	}
}

func TestMeasure(t *testing.T) {
	ff, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("no ffmpeg")
	}
	p := filepath.Join(t.TempDir(), "tone.flac")
	if out, err := exec.Command(ff, "-v", "error", "-f", "lavfi", "-i", "sine=f=1000:duration=5", "-af", "volume=-12dB", p).CombinedOutput(); err != nil {
		t.Skipf("%v %s", err, out)
	}
	lufs, peak, err := Measure(context.Background(), ff, p)
	if err != nil {
		t.Fatal(err)
	}
	// A 1 kHz sine at -12 dB relative to FFmpeg's default amplitude (1/8 ≈ -18 dBFS) is about -33 LUFS.
	if lufs > -25 || lufs < -40 || peak <= 0 || peak > 1 || math.IsNaN(lufs) {
		t.Fatalf("lufs %.1f peak %.3f", lufs, peak)
	}
}
