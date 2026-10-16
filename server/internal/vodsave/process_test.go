package vodsave

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"marquee/internal/settings"
)

func has(args []string, seq ...string) bool {
	return strings.Contains(" "+strings.Join(args, " ")+" ", " "+strings.Join(seq, " ")+" ")
}

// The FFmpeg command lines for remuxing and for each encoder's transcode.
func TestProcessArgs(t *testing.T) {
	mi := mediaInfo{DurationMS: 1000, Width: 3840, Height: 2160, Video: 1, VideoIndex: 0, PixFmt: "yuv420p10le", HDR: true,
		Audio:     []stream{{Index: 1, Codec: "eac3", Channels: 6}, {Index: 2, Codec: "aac", Channels: 2}},
		Subtitles: []stream{{Index: 3, Codec: "mov_text"}, {Index: 4, Codec: "subrip"}, {Index: 5, Codec: "eia_608"}}}
	c := settings.Defaults().VODSave

	remux := processArgs(c, "remux", "in", "out", mi, "software", "veryfast", "", true, true)
	for _, want := range [][]string{{"-map", "0:V"}, {"-map", "0:1"}, {"-map", "0:2"}, {"-map", "0:3"}, {"-map", "0:4"},
		{"-c", "copy"}, {"-c:s:0", "srt"}, {"-f", "matroska", "out"}} {
		if !has(remux, want...) {
			t.Errorf("remux lacks %v: %v", want, remux)
		}
	}
	if has(remux, "-map", "0:5") || has(remux, "-vf") || has(remux, "-c:v", "libx265") {
		t.Errorf("remux: %v", remux)
	}
	if noSubs := processArgs(c, "remux", "in", "out", mi, "software", "", "", true, false); has(noSubs, "-map", "0:3") {
		t.Errorf("without subtitles: %v", noSubs)
	}

	c.Quality, c.MaxHeight, c.VideoCodec, c.VideoQuality, c.Audio = "transcode", 1080, "hevc", "balanced", "aac_51"
	nv := processArgs(c, "transcode", "in", "out", mi, "nvenc", "p4", "", true, true)
	for _, want := range [][]string{{"-map", "0:0"}, {"-c:v", "hevc_nvenc"}, {"-preset", "p4"}, {"-cq", "24"}, {"-b:v", "0"},
		{"-c:a:0", "aac", "-ac:a:0", "6", "-b:a:0", "384k"}, {"-c:a:1", "aac", "-ac:a:1", "2"}} {
		if !has(nv, want...) {
			t.Errorf("nvenc lacks %v: %v", want, nv)
		}
	}
	vf := nv[indexOf(nv, "-vf")+1]
	if !strings.HasPrefix(vf, "tonemapx=") || !strings.Contains(vf, "scale=-2:1080") || !strings.HasSuffix(vf, "format=yuv420p") {
		t.Errorf("nvenc filters: %s", vf)
	}

	c.VideoKbps, c.Audio = 6000, "aac_stereo"
	qsv := processArgs(c, "transcode", "in", "out", mi, "qsv", "medium", "/dev/dri/renderD128", false, true)
	for _, want := range [][]string{{"-init_hw_device", "qsv=qs:/dev/dri/renderD128"}, {"-c:v", "hevc_qsv"}, {"-b:v", "6000k", "-maxrate", "9000k"},
		{"-c:a:0", "aac", "-ac:a:0", "2", "-b:a:0", "192k"}} {
		if !has(qsv, want...) {
			t.Errorf("qsv lacks %v: %v", want, qsv)
		}
	}
	if has(qsv, "-global_quality") {
		t.Error("qsv with a bitrate also has a quality")
	}
	if vf := qsv[indexOf(qsv, "-vf")+1]; strings.Contains(vf, "tonemapx") || !strings.Contains(vf, "format=p010le,hwupload=extra_hw_frames=64,format=qsv") {
		t.Errorf("qsv filters (10-bit kept without tone mapping): %s", vf)
	}

	c.VideoKbps, c.VideoCodec, c.VideoQuality, c.MaxHeight, c.Audio = 0, "h264", "high", 2160, "copy"
	sd := mediaInfo{DurationMS: 1000, Width: 720, Height: 480, Video: 1, VideoIndex: 0, PixFmt: "yuv420p", Audio: []stream{{Index: 1, Channels: 2}}}
	sw := processArgs(c, "transcode", "in", "out", sd, "software", "fast", "", true, true)
	for _, want := range [][]string{{"-c:v", "libx264"}, {"-crf", "18"}, {"-preset", "fast"}} {
		if !has(sw, want...) {
			t.Errorf("software lacks %v: %v", want, sw)
		}
	}
	if vf := sw[indexOf(sw, "-vf")+1]; vf != "scale=-2:480,format=yuv420p" {
		t.Errorf("never upscaled: %s", vf)
	}
	if has(sw, "-c:a:0") {
		t.Errorf("audio copy: %v", sw)
	}
}

func indexOf(a []string, s string) int {
	for i, v := range a {
		if v == s {
			return i
		}
	}
	return -1
}

// An MPEG-TS film saved as "original" gets an MKV container, every stream copied; with
// "transcode" it's converted (software, H.264 480p) and checked against the download.
func TestRemuxAndTranscode(t *testing.T) {
	ts := clip(t, "ts")
	films := `[{"name":"TS Film (2010)","stream_id":"5","container_extension":"ts"},{"name":"Small (2011)","stream_id":"6","container_extension":"mkv"}]`
	e := newEnv(t, films, nil)
	e.src.files["movie/5.ts"] = ts
	e.src.files["movie/6.mkv"] = clip(t, "mkv")
	ctx := context.Background()
	r, _ := e.s.SaveMovie(ctx, e.movieID("TS Film (2010)"), 1, false)
	if !strings.HasSuffix(r.Saves[0].TargetPath, "TS Film (2010).mkv") {
		t.Fatalf("target: %s", r.Saves[0].TargetPath)
	}
	e.s.Settings.Update(ctx, func(s *settings.Settings) error {
		s.VODSave.Quality, s.VODSave.MaxHeight, s.VODSave.VideoCodec, s.VODSave.VideoQuality, s.VODSave.Audio = "transcode", 480, "h264", "small", "aac_stereo"
		s.Transcoder.Preset = "speed"
		return nil
	})
	r2, _ := e.s.SaveMovie(ctx, e.movieID("Small (2011)"), 1, false)
	e.start()
	v := e.wait(r.Saves[0].ID, Done, Failed)
	if v.State != Done || !strings.HasSuffix(v.TargetPath, "TS Film (2010) WEBDL-720p.mkv") || !v.Verified {
		t.Fatalf("remux: %+v", v)
	}
	mi, err := e.s.probe(ctx, v.TargetPath)
	if err != nil || mi.Height != 720 || len(mi.Audio) != 1 {
		t.Fatalf("remuxed: %+v %v", mi, err)
	}
	b, _ := os.ReadFile(v.TargetPath)
	if v.SHA256 != sha(b) {
		t.Error("the checksum isn't the saved file's")
	}
	if _, err := os.Stat(strings.TrimSuffix(v.TargetPath, " WEBDL-720p.mkv") + ".mkv.source.part"); !os.IsNotExist(err) {
		t.Error("the download was left")
	}
	v2 := e.wait(r2.Saves[0].ID, Done, Failed)
	if v2.State != Done || !strings.HasSuffix(v2.TargetPath, "Small (2011) WEBDL-480p.mkv") {
		t.Fatalf("transcode: %+v", v2)
	}
	if mi, _ := e.s.probe(ctx, v2.TargetPath); mi.Height != 480 {
		t.Errorf("transcoded height %d", mi.Height)
	}
	entries, _ := os.ReadDir(filepath.Dir(v2.TargetPath))
	if len(entries) != 1 {
		t.Errorf("left in the folder: %v", entries)
	}
}

// A season of a series: episodes fetched from the source first, named for the library,
// saved into the shows folder.
func TestSaveSeason(t *testing.T) {
	data := clip(t, "mkv")
	e := newEnv(t, "[]", nil)
	e.src.series = `[{"name":"EN - Kings: The Hill (2011)","series_id":"201","category_id":"1"}]`
	e.src.info["201"] = `{"seasons":[],"episodes":{"1":[
		{"id":"3001","episode_num":"1","title":"Kings: The Hill - S01E01 - Winter","container_extension":"mkv","season":"1"},
		{"id":"3002","episode_num":"2","title":"Kings: The Hill - S01E02","container_extension":"mkv","season":"1"}],
		"2":[{"id":"3003","episode_num":"1","title":"Return","container_extension":"mkv","season":"2"}]}}`
	e.vod.Refresh(context.Background(), false)
	e.src.files["series/3001.mkv"] = data
	e.src.files["series/3002.mkv"] = data
	var seriesID int64
	e.s.DB.QueryRow("SELECT id FROM vod_series").Scan(&seriesID)
	ctx := context.Background()
	one := 1
	r, err := e.s.SaveSeries(ctx, seriesID, &one, 1, false)
	if err != nil || r.Queued != 2 {
		t.Fatalf("season: %+v %v", r, err)
	}
	e.start()
	show := filepath.Join(e.shows, "Kings - The Hill (2011)", "Season 1")
	want := []string{filepath.Join(show, "Kings - The Hill - S01E01 - Winter WEBDL-720p.mkv"), filepath.Join(show, "Kings - The Hill - S01E02 WEBDL-720p.mkv")}
	for i, s := range r.Saves {
		if v := e.wait(s.ID, Done, Failed); v.TargetPath != want[i] {
			t.Errorf("episode %d: %+v", i, v)
		}
	}
	states, _ := e.s.EpisodeStates(ctx, seriesID)
	if len(states) != 2 {
		t.Errorf("states: %+v", states)
	}
	// Saving the whole series now adds only season 2's episode.
	if r, _ := e.s.SaveSeries(ctx, seriesID, nil, 1, false); r.Queued != 1 || r.Already != 2 {
		t.Errorf("series: %+v", r)
	}
	e.waitFor("the shows scan", func() bool { e.scanMu.Lock(); defer e.scanMu.Unlock(); return len(e.scans) >= 1 })
}

// Saves interrupted by a restart continue; failed saves are queued again in the background.
func TestRestartAndBackgroundRetry(t *testing.T) {
	data := clip(t, "mkv")
	e := newEnv(t, oneFilm, nil)
	e.src.files["movie/101.mkv"] = data
	ctx := context.Background()
	r, _ := e.s.SaveMovie(ctx, e.movieID("EN - Clip Film (2020)"), 1, false)
	id := r.Saves[0].ID
	// As if the server stopped mid-download: half the file there, the row "downloading".
	os.MkdirAll(filepath.Dir(r.Saves[0].TargetPath), 0o775)
	os.WriteFile(r.Saves[0].TargetPath+".part", data[:len(data)/2], 0o664)
	e.s.DB.Exec("UPDATE vod_saves SET state='downloading', bytes_done=?, bytes_total=? WHERE id=?", len(data)/2, len(data), id)
	e.start()
	v := e.wait(id, Done, Failed)
	if v.State != Done || v.SHA256 != sha(data) || e.src.ranges[0] != "bytes="+strconv.Itoa(len(data)/2)+"-" {
		t.Fatalf("after restart: %+v %q", v, e.src.ranges)
	}
	// A failed one comes back by itself.
	e.s.DB.Exec("UPDATE vod_saves SET state='failed', retryable=1, error='x' WHERE id=?", id)
	e.s.ClearFinished(ctx)
	r, _ = e.s.SaveMovie(ctx, e.movieID("EN - Clip Film (2020)"), 1, false)
	e.s.DB.Exec("UPDATE vod_saves SET state='failed', retryable=1 WHERE id=?", r.Saves[0].ID)
	e.s.mu.Lock()
	e.s.RetryFailedEvery = 1
	e.s.mu.Unlock()
	if v := e.wait(r.Saves[0].ID, Skipped, Done); v.State != Skipped {
		t.Fatalf("retried: %+v", v) // the file is there now
	}
}
