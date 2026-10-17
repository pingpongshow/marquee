package optimize

import (
	"context"
	"strconv"
	"testing"
)

// A maximum size per hour caps the video bitrate at what fits after the audio and subtitles: copied
// lossless audio (its real bitrate, or a guess for DTS-HD MA) leaves less for the video,
// AAC leaves more; a target bitrate above the cap comes down to it; a file whose audio
// fills the size keeps a floor.
func TestMaxSizeCapsVideo(t *testing.T) {
	src := Source{Path: "/m/Film.mkv", DurationMS: 7_200_000, Width: 1920, Height: 1080, VideoIndex: 0, VideoCodec: "h264", PixFmt: "yuv420p",
		Audio:     []Stream{{Index: 1, Codec: "dts", Profile: "DTS-HD MA", Channels: 6, Kbps: 4000}},
		Subtitles: []Stream{{Index: 2, Codec: "hdmv_pgs_subtitle"}, {Index: 3, Codec: "subrip"}}}
	p := preset("1080p-hevc-balanced") // audio copied
	p.MaxGBPerHour = 2.5 // a 2-hour film: at most 5 GB
	pl, err := NewPlan(src, p)
	if err != nil {
		t.Fatal(err)
	}
	// 2.5 GB an hour = 5556 kb/s; 97% = 5388; minus 4000 audio and 41 subtitles = 1347.
	if pl.MaxKbps != 1347 {
		t.Errorf("copied audio: cap %d", pl.MaxKbps)
	}
	args := pl.Args(src, "nvenc", "p6", "", "/m/out.part")
	if !has(args, "-cq", "24", "-b:v", "0") || !has(args, "-maxrate", "1347k", "-bufsize", "5388k") {
		t.Errorf("nvenc capped CQ: %v", args)
	}
	if sw := pl.Args(src, "software", "medium", "", "/m/out.part"); !has(sw, "-crf", "23") || !has(sw, "-maxrate", "1347k") {
		t.Errorf("software capped CRF: %v", sw)
	}
	if q := pl.Args(src, "qsv", "slow", "/dev/dri/renderD128", "/m/out.part"); has(q, "-global_quality", "24") || !has(q, "-b:v", "1347k") {
		t.Errorf("qsv uses the cap as its target: %v", q)
	}

	// Unknown bitrate: DTS-HD MA 5.1 is guessed at 3600 kb/s.
	src.Audio[0].Kbps = 0
	pl, _ = NewPlan(src, p)
	if pl.MaxKbps != 5388-3600-41 {
		t.Errorf("guessed audio: cap %d", pl.MaxKbps)
	}

	// AAC 5.1 (384 kb/s) leaves most of the size to the video.
	p.Audio = "aac_51"
	pl, _ = NewPlan(src, p)
	if pl.MaxKbps != 5388-384-41 {
		t.Errorf("aac: cap %d", pl.MaxKbps)
	}
	// A target bitrate above the cap is lowered to it; one below is kept.
	p.VideoKbps = 8000
	if pl, _ = NewPlan(src, p); pl.VideoKbps != pl.MaxKbps {
		t.Errorf("target over the cap: %d (cap %d)", pl.VideoKbps, pl.MaxKbps)
	}
	p.VideoKbps = 3000
	if pl, _ = NewPlan(src, p); pl.VideoKbps != 3000 {
		t.Errorf("target under the cap: %d", pl.VideoKbps)
	}
	if args := pl.Args(src, "nvenc", "p6", "", "/m/out.part"); !has(args, "-b:v", "3000k", "-maxrate", "4500k") {
		t.Errorf("target bitrate args: %v", args)
	}

	// A tiny size: the floor.
	p.VideoKbps, p.MaxGBPerHour = 0, 0.25
	if pl, _ = NewPlan(src, p); pl.MaxKbps != minCapKbps {
		t.Errorf("floor: %d", pl.MaxKbps)
	}
	// Per hour, the cap doesn't depend on the running time: a 1-hour episode gets the same
	// bitrate (so at most half the size).
	p.MaxGBPerHour = 2.5
	short := src
	short.DurationMS = 3_600_000
	a, _ := NewPlan(src, p)
	b, _ := NewPlan(short, p)
	if a.MaxKbps != b.MaxKbps {
		t.Errorf("per hour: %d vs %d", a.MaxKbps, b.MaxKbps)
	}
	// No maximum: no cap.
	p.MaxGBPerHour = 0
	if pl, _ = NewPlan(src, p); pl.MaxKbps != 0 || has(pl.Args(src, "nvenc", "p6", "", "/m/o.part"), "-maxrate") {
		t.Errorf("no maximum: %+v", pl)
	}
}

// The Large files estimate respects the maximum size per hour.
func TestEstimateMaxSize(t *testing.T) {
	const gb = 1_000_000_000
	f := FileInfo{Size: 30 * gb, DurationMS: 3 * 3_600_000, Width: 1920, Height: 1080,
		Audio: []AudioInfo{{Codec: "dts", Profile: "DTS-HD MA 5.1", Channels: 6}}}
	p := preset("1080p-hevc-balanced")
	before := Estimate(f, p)
	p.MaxGBPerHour = 2 // 3 hours: at most 6 GB
	p.Audio = "aac_51"
	after := Estimate(f, p)
	if after > 6*gb || after >= before {
		t.Errorf("estimate %d (uncapped %d)", after, before)
	}
}

// Files already optimized can be listed on their own (to optimize them again) or left out,
// and queued again.
func TestReoptimize(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	const gb = 1_000_000_000
	_, done := e.add(fileSpec{path: "/m/done.mkv", size: 10 * gb, w: 1920, h: 1080, codec: "hevc", available: true})
	_, fresh := e.add(fileSpec{path: "/m/fresh.mkv", size: 20 * gb, w: 1920, h: 1080, available: true})
	_, restored := e.add(fileSpec{path: "/m/restored.mkv", size: 15 * gb, w: 1920, h: 1080, available: true})
	for _, j := range []struct {
		file  int64
		state string
	}{{done, "done"}, {restored, "restored"}} {
		if _, err := e.db.Exec(`INSERT INTO optimize_jobs (file_id, library_id, title, path, preset_id, preset_name, preset, state)
			VALUES (?, ?, 't', 'p', 'x', 'x', '{}', ?)`, j.file, e.lib, j.state); err != nil {
			t.Fatal(err)
		}
	}
	s := &Service{DB: e.db, Settings: e.settings}
	ids := func(f Filter) string {
		got, err := s.Matching(ctx, f)
		if err != nil {
			t.Fatal(err)
		}
		out := ""
		for _, id := range got {
			out += strconv.FormatInt(id, 10) + " "
		}
		return out
	}
	if got, want := ids(Filter{Optimized: ptrBool(true)}), strconv.FormatInt(done, 10)+" "; got != want {
		t.Errorf("optimized: %q, want %q", got, want)
	}
	if got, want := ids(Filter{Optimized: ptrBool(false)}), strconv.FormatInt(fresh, 10)+" "+strconv.FormatInt(restored, 10)+" "; got != want {
		t.Errorf("not optimized: %q, want %q", got, want)
	}
	queued, skipped, err := s.Enqueue(ctx, []int64{done}, "1080p-hevc-small")
	if err != nil || queued != 1 || skipped != 0 {
		t.Fatalf("re-queue: %d %d %v", queued, skipped, err)
	}
	// Queued once is enough.
	if queued, skipped, _ = s.Enqueue(ctx, []int64{done}, "1080p-hevc-small"); queued != 0 || skipped != 1 {
		t.Errorf("queued twice: %d %d", queued, skipped)
	}
}
