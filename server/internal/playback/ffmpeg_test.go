package playback

import (
	"strings"
	"testing"
)

func TestJobArgs(t *testing.T) {
	d := Decide(archer(), chrome, Limits{})
	j := Job{Input: "/m/a.mkv", Decision: d, VideoIndex: 0, AudioIndex: 4, SubIndex: -1, StartSegment: 10, Dir: "/t/s", Encoder: "nvenc", Preset: "balanced"}
	cmd := strings.Join(j.Args(), " ")
	for _, want := range []string{"-hwaccel cuda", "-ss 60.000", "-copyts", "scale_cuda=w=-2:h=1080:format=nv12", "h264_nvenc", "-no-scenecut 1",
		"gte(t,prev_forced_t+6-0.001)", "+frag_discont pipe:1", "-c:a aac -ac 2", "-map 0:4"} {
		if !strings.Contains(cmd, want) {
			t.Errorf("missing %q in\n%s", want, cmd)
		}
	}
	// Burning PGS subtitles: CPU decode + overlay, then upload to the GPU encoder.
	m := archer()
	m.Subtitle = &SubtitleStream{Codec: "hdmv_pgs_subtitle", Index: 2}
	j.Decision = Decide(m, chrome, Limits{})
	j.SubIndex, j.SubImage = 2, true
	cmd = strings.Join(j.Args(), " ")
	if strings.Contains(cmd, "-hwaccel cuda") || !strings.Contains(cmd, "[0:0][0:2]overlay=eof_action=pass,scale=-2:1080,format=yuv420p,hwupload_cuda[v]") {
		t.Errorf("burn-in graph wrong:\n%s", cmd)
	}
	// Direct stream copies video and needs no filters.
	j = Job{Input: "/m/a.mkv", Decision: Decide(archer(), appleTV, Limits{}), VideoIndex: 0, AudioIndex: 4, SubIndex: -1, Dir: "/t", Encoder: "nvenc"}
	cmd = strings.Join(j.Args(), " ")
	if !strings.Contains(cmd, "-c:v copy") || strings.Contains(cmd, "filter_complex") || strings.Contains(cmd, "-ss ") {
		t.Errorf("direct stream:\n%s", cmd)
	}
	// A copy restart seeks just past the planned keyframe.
	j.Plan, j.StartSegment = []float64{0, 5.005, 11.2}, 2
	if cmd = strings.Join(j.Args(), " "); !strings.Contains(cmd, "-ss 11.250") || strings.Contains(cmd, "force_key_frames") {
		t.Errorf("copy restart:\n%s", cmd)
	}
}
