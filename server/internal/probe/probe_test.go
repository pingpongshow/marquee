package probe

import (
	"os"
	"testing"
)

func load(t *testing.T, name string) *Result {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	r, err := Parse(data)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestParseEpisode(t *testing.T) {
	r := load(t, "episode.json")
	if r.Container != "mkv" || r.DurationMS < 60_000 || r.BitrateKbps == 0 {
		t.Fatalf("format: %+v", r)
	}
	v := r.Video()
	if v == nil || v.Codec != "hevc" || v.Height != 1080 || v.FrameRate < 23 || v.BitDepth == 0 {
		t.Fatalf("video: %+v", v)
	}
	a := r.Audio()
	if a == nil || a.Codec != "dts" || a.Language != "eng" || a.Channels == 0 {
		t.Fatalf("audio: %+v", a)
	}
	subs := 0
	for _, s := range r.Streams {
		if s.Kind == "subtitle" {
			subs++
			if s.Codec != "hdmv_pgs_subtitle" || s.Language == "" {
				t.Errorf("subtitle: %+v", s)
			}
		}
	}
	if subs != 3 || len(r.Chapters) != 5 {
		t.Fatalf("subs=%d chapters=%d", subs, len(r.Chapters))
	}
}

func TestParseTrack(t *testing.T) {
	r := load(t, "track.json")
	if r.Container != "flac" || r.Tags["album"] == "" || r.Tags["artist"] == "" || r.Tags["title"] == "" {
		t.Fatalf("track: container=%s tags=%v", r.Container, r.Tags)
	}
	if a := r.Audio(); a == nil || a.Codec != "flac" || a.SampleRate == 0 {
		t.Fatalf("audio: %+v", a)
	}
}
