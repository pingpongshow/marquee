package playback

import (
	"context"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"marquee/internal/db"
	"marquee/internal/settings"
)

var testLadder = []settings.QualityRung{
	{MaxHeight: 1080, VideoKbps: 12000}, {MaxHeight: 1080, VideoKbps: 8000}, {MaxHeight: 720, VideoKbps: 4000},
	{MaxHeight: 480, VideoKbps: 1500}, {MaxHeight: 360, VideoKbps: 750},
}

func TestLadderFor(t *testing.T) {
	top := Decision{Method: Transcode, Height: 1080, VideoKbps: 7800}
	r := ladderFor(top, 1080, testLadder)
	if len(r) != 2 || r[0].VideoKbps != 4000 || r[0].Height != 720 || r[1].VideoKbps != 1500 || r[1].Height != 480 {
		t.Fatalf("ladder %+v", r)
	}
	// A low top rung leaves room for only the smallest step.
	if r := ladderFor(Decision{Height: 480, VideoKbps: 1400}, 1080, testLadder); len(r) != 1 || r[0].VideoKbps != 750 {
		t.Fatalf("small ladder %+v", r)
	}
}

// A remote transcode lists its rungs in the master playlist, and a lower rung produces
// segments at its own resolution when fetched.
func TestLowerRungTranscodesOnDemand(t *testing.T) {
	ff := ffmpegPath(t)
	dir := t.TempDir()
	in := makeSample(t, ff, dir)
	m := Media{Container: "mkv", DurationMS: 40_000, BitrateKbps: 800,
		Video: &VideoStream{Index: 0, Codec: "hevc", Width: 640, Height: 360, BitDepth: 8}, Audio: &AudioStream{Index: 1, Codec: "ac3", Channels: 2}}
	d := Decide(m, chrome, Limits{})
	d.Height, d.VideoKbps = 360, 3000
	base := &Transcoder{FFmpeg: ff, Job: Job{Input: in, Decision: d, VideoIndex: 0, AudioIndex: 1, SubIndex: -1, Dir: filepath.Join(dir, "s"), Preset: "speed"},
		Encoders: []string{"software"}, TotalSegments: SegmentCount(m.DurationMS), ThrottleAhead: 20}
	defer base.Stop()
	s := &Session{Media: m, Decision: d, transcoder: base, Plan: FixedPlan(m.DurationMS),
		rungs: []Rung{{Height: 240, VideoKbps: 400}}}
	database, err := db.Open(context.Background(), filepath.Join(dir, "t.db"), filepath.Join(dir, "b"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	store, err := settings.Open(context.Background(), database)
	if err != nil {
		t.Fatal(err)
	}
	mgr := &Manager{Settings: store}
	defer s.stopRungs()

	rec := httptest.NewRecorder()
	mgr.serveMaster(rec, s)
	if !strings.Contains(rec.Body.String(), "r1/index.m3u8") || !strings.Contains(rec.Body.String(), "RESOLUTION=426x240") {
		t.Fatalf("master:\n%s", rec.Body.String())
	}

	tr, err := mgr.RungTranscoder(s, 1)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	p, err := tr.Segment(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(p, "/r1/") {
		t.Fatalf("rung segment path %s", p)
	}
	out, _ := exec.Command("ffprobe", "-v", "error", "-select_streams", "v", "-show_entries", "stream=height", "-of", "csv=p=0",
		filepath.Join(filepath.Dir(p), "init.mp4")).Output()
	if strings.TrimSpace(string(out)) != "240" {
		t.Fatalf("rung height %q", out)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatal(err)
	}
}
