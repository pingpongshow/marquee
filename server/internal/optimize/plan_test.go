package optimize

import (
	"strings"
	"testing"

	"marquee/internal/settings"
)

func has(args []string, seq ...string) bool {
	return strings.Contains(" "+strings.Join(args, " ")+" ", " "+strings.Join(seq, " ")+" ")
}

func vf(args []string) string {
	for i, a := range args {
		if a == "-vf" && i+1 < len(args) {
			return args[i+1]
		}
	}
	return ""
}

func preset(id string) settings.OptimizePreset {
	p, _ := settings.Defaults().Optimize.Preset(id)
	return p
}

var hdr4K = Source{Path: "/m/Film (2020)/Film.mkv", DurationMS: 7_200_000, Width: 3840, Height: 1600, VideoIndex: 0, VideoCodec: "hevc",
	PixFmt: "yuv420p10le", Transfer: "smpte2084",
	Audio: []Stream{{Index: 1, Codec: "truehd", Channels: 8, Default: true}, {Index: 2, Codec: "ac3", Channels: 2, Title: "Director's Commentary", Comment: true},
		{Index: 3, Codec: "aac", Channels: 2}},
	Subtitles:   []Stream{{Index: 4, Codec: "subrip"}, {Index: 5, Codec: "hdmv_pgs_subtitle"}, {Index: 6, Codec: "eia_608"}, {Index: 7, Codec: "mov_text"}},
	Attachments: 1}

// HDR kept as 10-bit HEVC on each encoder, letterboxed films scaled into the 1080p box,
// every audio track copied, text and picture subtitles kept.
func TestArgsHDRKeep(t *testing.T) {
	pl, err := NewPlan(hdr4K, preset("1080p-hevc-balanced"))
	if err != nil {
		t.Fatal(err)
	}
	if pl.Final != hdr4K.Path || pl.Format != "matroska" || pl.ToneMap || !pl.TenBit || pl.HDRTransfer != "smpte2084" {
		t.Fatalf("plan: %+v", pl)
	}
	nv := pl.Args(hdr4K, "nvenc", "p6", "", "/m/out.part")
	for _, want := range [][]string{{"-hwaccel", "cuda"}, {"-map", "0:0"}, {"-map", "0:1"}, {"-map", "0:2"}, {"-map", "0:3"}, {"-map", "0:4"}, {"-map", "0:5"},
		{"-map", "0:7"}, {"-map", "0:t?"}, {"-c", "copy"}, {"-c:v", "hevc_nvenc"}, {"-preset", "p6"}, {"-cq", "24"}, {"-profile:v", "main10"},
		{"-color_trc", "smpte2084"}, {"-c:s:2", "srt"}, {"-f", "matroska", "/m/out.part"}} {
		if !has(nv, want...) {
			t.Errorf("nvenc lacks %v: %v", want, nv)
		}
	}
	if !has(nv, "-map", "0:6") || has(nv, "-c:a:0") {
		t.Errorf("captions dropped or audio converted: %v", nv)
	}
	if f := vf(nv); f != "scale=1920:1080:force_original_aspect_ratio=decrease:force_divisible_by=2,format=p010le" {
		t.Errorf("nvenc filters: %s", f)
	}
	qsv := pl.Args(hdr4K, "qsv", "slow", "/dev/dri/renderD128", "/m/out.part")
	for _, want := range [][]string{{"-init_hw_device", "qsv=qs:/dev/dri/renderD128"}, {"-c:v", "hevc_qsv"}, {"-global_quality", "24"}} {
		if !has(qsv, want...) {
			t.Errorf("qsv lacks %v: %v", want, qsv)
		}
	}
	if f := vf(qsv); !strings.HasSuffix(f, "format=p010le,hwupload=extra_hw_frames=64,format=qsv") {
		t.Errorf("qsv filters: %s", f)
	}
	sw := pl.Args(hdr4K, "software", "fast", "", "/m/out.part")
	for _, want := range [][]string{{"-c:v", "libx265"}, {"-crf", "23"}} {
		if !has(sw, want...) {
			t.Errorf("software lacks %v: %v", want, sw)
		}
	}
	if i := strings.Index(strings.Join(sw, " "), "hdr-opt=1"); i < 0 || !strings.Contains(vf(sw), "format=yuv420p10le") {
		t.Errorf("software HDR10: %v", sw)
	}
}

// Tone-mapped to SDR (asked for, or H.264), audio converted; every audio track
// (commentary too) and every subtitle is kept, closed captions as SRT.
func TestArgsToneMapAudioSubs(t *testing.T) {
	p := preset("1080p-hevc-small") // aac_51
	p.HDR = "tonemap"
	pl, _ := NewPlan(hdr4K, p)
	if !pl.ToneMap || pl.TenBit || pl.HDRTransfer != "" {
		t.Fatalf("plan: %+v", pl)
	}
	a := pl.Args(hdr4K, "software", "fast", "", "out")
	if f := vf(a); !strings.HasPrefix(f, "tonemapx=") || !strings.HasSuffix(f, "format=yuv420p") {
		t.Errorf("filters: %s", f)
	}
	for _, m := range []string{"0:1", "0:2", "0:3", "0:4", "0:5", "0:6", "0:7"} {
		if !has(a, "-map", m) {
			t.Errorf("stream %s dropped: %v", m, a)
		}
	}
	// TrueHD 7.1 becomes AAC 5.1, the AC-3 commentary AAC stereo; the AAC stereo track is
	// copied as it is.
	if !has(a, "-c:a:0", "aac", "-ac:a:0", "6", "-b:a:0", "384k") || !has(a, "-c:a:1", "aac") || has(a, "-c:a:2") {
		t.Errorf("audio: %v", a)
	}
	// Closed captions (stream 6, the third subtitle) become SRT.
	if !has(a, "-c:s:2", "srt") {
		t.Errorf("captions: %v", a)
	}
	if has(a, "-color_trc") {
		t.Errorf("SDR tagged as HDR: %v", a)
	}

	h264 := preset("1080p-hevc-balanced")
	h264.Codec = "h264"
	if pl, _ := NewPlan(hdr4K, h264); !pl.ToneMap {
		t.Error("HDR kept in H.264")
	}

	// Every track copied as it is.
	p = preset("same-hevc")
	pl, _ = NewPlan(hdr4K, p)
	if len(pl.Audio) != 3 || pl.Audio[1].Codec != "copy" || len(pl.Subtitles) != 4 || pl.ScaleW != 0 {
		t.Errorf("copy all: %+v", pl)
	}
	p.Audio = "aac_stereo"
	pl, _ = NewPlan(hdr4K, p)
	a = pl.Args(hdr4K, "nvenc", "p6", "", "out")
	if !has(a, "-c:a:0", "aac", "-ac:a:0", "2", "-b:a:0", "192k") || !has(a, "-c:a:1", "aac") || has(a, "-c:a:2") {
		t.Errorf("stereo: %v", a)
	}
	// A target bitrate instead of a quality.
	p.VideoKbps = 6000
	pl, _ = NewPlan(hdr4K, p)
	if a := pl.Args(hdr4K, "nvenc", "p6", "", "out"); !has(a, "-b:v", "6000k", "-maxrate", "9000k") || has(a, "-cq") {
		t.Errorf("bitrate: %v", a)
	}
}

// MP4s stay MP4 (text subtitles as mov_text, DTS made AAC, HEVC tagged hvc1) unless
// they carry picture subtitles, which only MKV holds; other containers become MKV;
// Dolby Vision profile 5 is refused.
func TestContainers(t *testing.T) {
	src := Source{Path: "/m/a.mp4", DurationMS: 1000, Width: 1920, Height: 1080, VideoCodec: "h264", PixFmt: "yuv420p",
		Audio:     []Stream{{Index: 1, Codec: "dts", Channels: 6}},
		Subtitles: []Stream{{Index: 2, Codec: "subrip"}}}
	pl, _ := NewPlan(src, preset("same-hevc"))
	a := pl.Args(src, "software", "fast", "", "out")
	if pl.Final != src.Path || !has(a, "-f", "mp4", "out") || !has(a, "-tag:v", "hvc1") || !has(a, "-c:s:0", "mov_text") ||
		!has(a, "-c:a:0", "aac") || !has(a, "-movflags", "+faststart") {
		t.Errorf("mp4: %+v %v", pl, a)
	}
	src.Subtitles = append(src.Subtitles, Stream{Index: 3, Codec: "dvd_subtitle"})
	pl, _ = NewPlan(src, preset("same-hevc"))
	a = pl.Args(src, "software", "fast", "", "out")
	if pl.Final != "/m/a.mkv" || pl.Format != "matroska" || !has(a, "-map", "0:3") || !has(a, "-map", "0:2") {
		t.Errorf("mp4 with picture subtitles: %+v %v", pl, a)
	}
	src.Path = "/m/b.avi"
	if pl, _ := NewPlan(src, preset("same-hevc")); pl.Final != "/m/b.mkv" || pl.Format != "matroska" {
		t.Errorf("avi: %+v", pl)
	}
	src.DVProfile = 5
	if _, err := NewPlan(src, preset("same-hevc")); err == nil {
		t.Error("Dolby Vision profile 5 accepted")
	}
}

// Estimates: the preset's typical bitrate for the resulting resolution plus the audio it
// keeps, never more than the file's own.
func TestEstimate(t *testing.T) {
	// A 2-hour 4K remux: 60 GB, TrueHD 7.1 (3.5 Mb/s).
	remux := FileInfo{Size: 60e9, DurationMS: 7_200_000, Width: 3840, Height: 2160, HDR: true,
		Audio: []AudioInfo{{Codec: "truehd", Channels: 8, Default: true}, {Codec: "ac3", Channels: 2, Kbps: 192, Commentary: true}}}
	est := Estimate(remux, preset("1080p-hevc-balanced"))
	// (5000 × 1.15 + 3500 + 192) kb/s × 7200 s ≈ 8.4 GB
	if est < 8.0e9 || est > 8.8e9 {
		t.Errorf("1080p balanced: %d", est)
	}
	small := Estimate(remux, preset("1080p-hevc-small")) // 0.65 × 5000 × 1.15 + AAC 5.1 (384) + the commentary as AAC stereo
	if small >= est || small < 3.6e9 || small > 4.1e9 {
		t.Errorf("1080p small: %d", small)
	}
	same := Estimate(remux, preset("same-hevc"))
	if same <= est {
		t.Errorf("same resolution should be bigger than 1080p: %d vs %d", same, est)
	}
	// An already lean file isn't estimated to grow.
	lean := FileInfo{Size: 1e9, DurationMS: 7_200_000, Width: 1920, Height: 1080, Audio: []AudioInfo{{Codec: "aac", Channels: 2, Kbps: 128}}}
	if e := Estimate(lean, preset("same-hevc")); e > lean.Size {
		t.Errorf("lean grew: %d", e)
	}
	p := preset("same-hevc")
	p.VideoKbps = 2000
	if e := Estimate(remux, p); e < 4.9e9 || e > 5.2e9 { // (2000 + 3692) × 7200 / 8
		t.Errorf("target bitrate: %d", e)
	}
	if Class(1920, 800) != 1080 || Class(3840, 1600) != 2160 || Class(1280, 536) != 720 || Class(720, 480) != 480 {
		t.Error("classes")
	}
}
