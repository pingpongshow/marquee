package playback

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestSubtitleForceStyle(t *testing.T) {
	if s := (SubtitleStyle{}).ForceStyle(); s != "" {
		t.Errorf("no style set should keep FFmpeg's defaults, got %q", s)
	}
	cases := []struct {
		st   SubtitleStyle
		want []string
		not  []string
	}{
		{SubtitleStyle{Size: "small"}, []string{"FontSize=13", "PrimaryColour=&H00FFFFFF", "BorderStyle=1", "Outline=1.5", "MarginV=10"}, nil},
		{SubtitleStyle{Size: "medium"}, []string{"FontSize=16"}, nil},
		{SubtitleStyle{Size: "large"}, []string{"FontSize=20"}, nil},
		{SubtitleStyle{Size: "huge"}, []string{"FontSize=25"}, nil},
		{SubtitleStyle{Color: "#ffcc00"}, []string{"FontSize=16", "PrimaryColour=&H0000CCFF"}, nil},
		{SubtitleStyle{Color: "#12AB9f"}, []string{"PrimaryColour=&H009FAB12"}, nil},
		{SubtitleStyle{Background: "none"}, []string{"Outline=0", "Shadow=1"}, []string{"BorderStyle=3"}},
		{SubtitleStyle{Background: "outline"}, []string{"BorderStyle=1", "Outline=1.5", "Shadow=0"}, nil},
		{SubtitleStyle{Background: "translucent"}, []string{"BorderStyle=3", "OutlineColour=&H80000000", "BackColour=&H80000000"}, nil},
		{SubtitleStyle{Background: "opaque"}, []string{"BorderStyle=3", "OutlineColour=&H00000000", "BackColour=&H00000000"}, nil},
		{SubtitleStyle{Position: "raised"}, []string{"MarginV=50"}, []string{"MarginV=10"}},
		{SubtitleStyle{Position: "bottom"}, []string{"MarginV=10"}, nil},
	}
	for _, c := range cases {
		got := c.st.ForceStyle()
		for _, w := range c.want {
			if !strings.Contains(got+",", w+",") {
				t.Errorf("%+v: missing %s in %s", c.st, w, got)
			}
		}
		for _, n := range c.not {
			if strings.Contains(got+",", n+",") {
				t.Errorf("%+v: unexpected %s in %s", c.st, n, got)
			}
		}
	}
}

func TestBurnStyleOnlyPlainText(t *testing.T) {
	st := SubtitleStyle{Size: "large"}
	for _, c := range []struct {
		sub  *SubtitleStream
		want bool
	}{
		{&SubtitleStream{Codec: "subrip", Index: 3}, true},
		{&SubtitleStream{Codec: "mov_text", Index: 3}, true},
		{&SubtitleStream{Codec: "subrip", Index: -1, External: "/m/a.en.srt"}, true},
		{&SubtitleStream{Codec: "webvtt", Index: -1, External: "/m/a.en.vtt"}, true},
		{&SubtitleStream{Codec: "ass", Index: 3}, false},
		{&SubtitleStream{Codec: "ssa", Index: 3}, false},
		{&SubtitleStream{Index: -1, External: "/m/a.en.ASS"}, false},
		{&SubtitleStream{Codec: "hdmv_pgs_subtitle", Index: 3}, false},
		{&SubtitleStream{Index: -1, External: "/m/a.sup"}, false},
		{nil, false},
	} {
		if got := burnStyle(c.sub, st) != ""; got != c.want {
			t.Errorf("%+v: styled=%v, want %v", c.sub, got, c.want)
		}
	}
}

func TestJobArgsSubtitleStyle(t *testing.T) {
	m := archer()
	m.Subtitle = &SubtitleStream{Codec: "subrip", Index: 3}
	style := SubtitleStyle{Size: "large", Background: "translucent"}.ForceStyle()
	// Embedded track: the style follows the stream index, quoted so its commas don't split the graph.
	j := Job{Input: "/m/a.mkv", Decision: Decide(m, chrome, Limits{}), VideoIndex: 0, AudioIndex: 4, SubIndex: 3, SubRelIndex: 0,
		Dir: "/t", Encoder: "software", SubForceStyle: style}
	j.Decision.BurnSubtitle = true
	cmd := strings.Join(j.Args(), " ")
	want := `subtitles=f='/m/a.mkv':si=0:force_style='FontSize=20,PrimaryColour=&H00FFFFFF,BorderStyle=3,Outline=1,Shadow=0,OutlineColour=&H80000000,BackColour=&H80000000,MarginV=10'`
	if !strings.Contains(cmd, want) {
		t.Errorf("embedded:\n%s\nwant %s", cmd, want)
	}
	// External file.
	j.SubIndex, j.SubExternal = -1, "/m/a.en.srt"
	cmd = strings.Join(j.Args(), " ")
	if !strings.Contains(cmd, `subtitles=f='/m/a.en.srt':force_style='FontSize=20,`) {
		t.Errorf("external:\n%s", cmd)
	}
	// No style: unchanged.
	j.SubForceStyle = ""
	if cmd = strings.Join(j.Args(), " "); strings.Contains(cmd, "force_style") {
		t.Errorf("unstyled:\n%s", cmd)
	}
	// With a subtitle offset the style sits inside the shifted section.
	j.SubForceStyle, j.SubOffsetMS = style, 500
	if cmd = strings.Join(j.Args(), " "); !strings.Contains(cmd, `setpts=PTS-0.500/TB,subtitles=f='/m/a.en.srt':force_style='FontSize=20,`) ||
		!strings.Contains(cmd, `MarginV=10',setpts=PTS+0.500/TB`) {
		t.Errorf("offset:\n%s", cmd)
	}
	// Bitmap subtitles are overlaid, never styled.
	j = Job{Input: "/m/a.mkv", Decision: Decide(m, chrome, Limits{}), VideoIndex: 0, AudioIndex: 4, SubIndex: 2, SubImage: true,
		Dir: "/t", Encoder: "software", SubForceStyle: style}
	j.Decision.BurnSubtitle = true
	if cmd = strings.Join(j.Args(), " "); strings.Contains(cmd, "force_style") {
		t.Errorf("bitmap:\n%s", cmd)
	}
}

// TestBurnStyleWithFFmpeg runs real burn-ins when MARQUEE_TEST_FFMPEG names an FFmpeg built
// with libass. MARQUEE_TEST_BURN_OUT keeps the outputs (and a frame of each) for a look.
func TestBurnStyleWithFFmpeg(t *testing.T) {
	ff := os.Getenv("MARQUEE_TEST_FFMPEG")
	if ff == "" {
		t.Skip("set MARQUEE_TEST_FFMPEG to an FFmpeg with libass")
	}
	dir := os.Getenv("MARQUEE_TEST_BURN_OUT")
	if dir == "" {
		dir = t.TempDir()
	}
	run := func(args ...string) {
		t.Helper()
		if out, err := exec.Command(ff, args...).CombinedOutput(); err != nil {
			t.Fatalf("ffmpeg %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	// A folder name with every character the filter escaping has to handle.
	sub := filepath.Join(dir, `a,b's [x];y:z`, "clip.en.srt")
	os.MkdirAll(filepath.Dir(sub), 0o755)
	os.WriteFile(sub, []byte("1\n00:00:00,000 --> 00:00:03,000\nSubtitle style check, line one\nand a second line: \"quoted\"\n"), 0o644)
	clip := filepath.Join(dir, "clip.mp4")
	run("-hide_banner", "-v", "error", "-y", "-f", "lavfi", "-i", "testsrc2=size=1280x720:rate=24:duration=3",
		"-f", "lavfi", "-i", "sine=f=440:duration=3", "-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-shortest", clip)
	mkv := filepath.Join(dir, "clip.mkv")
	run("-hide_banner", "-v", "error", "-y", "-i", clip, "-i", sub, "-map", "0", "-map", "1", "-c", "copy", "-c:s", "srt", mkv)

	d := Decision{Method: Transcode, VideoCodec: "h264", Height: 720, VideoKbps: 2000, AudioCodec: "aac", AudioChannels: 2, BurnSubtitle: true}
	styles := map[string]SubtitleStyle{
		"default":     {},
		"small":       {Size: "small"},
		"huge-yellow": {Size: "huge", Color: "#FFFF00"},
		"none":        {Background: "none"},
		"translucent": {Background: "translucent", Position: "raised"},
		"opaque":      {Background: "opaque", Size: "large"},
	}
	for name, st := range styles {
		for _, external := range []bool{false, true} {
			j := Job{Input: mkv, Decision: d, VideoIndex: 0, AudioIndex: 1, SubIndex: 2, SubRelIndex: 0, Encoder: "software", Preset: "speed",
				SubForceStyle: st.ForceStyle(), OutputFile: filepath.Join(dir, name+map[bool]string{true: "-ext", false: "-emb"}[external]+".mp4")}
			if external {
				j.SubIndex, j.SubExternal = -1, sub
			}
			run(j.Args()...)
			run("-hide_banner", "-v", "error", "-y", "-ss", "1", "-i", j.OutputFile, "-frames:v", "1", strings.TrimSuffix(j.OutputFile, ".mp4")+".png")
		}
	}
}

func TestEscapeFilterPath(t *testing.T) {
	for in, want := range map[string]string{
		"/m/a.mkv":                         `'/m/a.mkv'`,
		"/m/Ocean's Eleven/a.srt":          `'/m/Ocean\'\''s Eleven/a.srt'`,
		"/m/a, b [x];c:d/a.srt":            `'/m/a, b [x];c\:d/a.srt'`,
		`/m/back\slash.srt`:                `'/m/back\\slash.srt'`,
		"FontSize=20,PrimaryColour=&H00FF": `'FontSize=20,PrimaryColour=&H00FF'`,
	} {
		if got := escapeFilterPath(in); got != want {
			t.Errorf("%s: got %s, want %s", in, got, want)
		}
	}
}
