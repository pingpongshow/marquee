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

func TestJobOffsets(t *testing.T) {
	// Audio offset: the audio comes from a second, shifted read of the file.
	j := Job{Input: "/m/a.mkv", Decision: Decide(archer(), appleTV, Limits{}), VideoIndex: 0, AudioIndex: 4, SubIndex: -1, Dir: "/t", Encoder: "nvenc",
		Plan: []float64{0, 6, 12}, StartSegment: 1, AudioOffsetMS: -250}
	cmd := strings.Join(j.Args(), " ")
	if !strings.Contains(cmd, "-ss 6.050 -itsoffset -0.250 -copyts -i /m/a.mkv") || !strings.Contains(cmd, "-map 1:4") {
		t.Errorf("audio offset:\n%s", cmd)
	}
	// Burned-in text subtitles drawn against shifted timestamps.
	m := archer()
	m.Subtitle = &SubtitleStream{Codec: "subrip", Index: 3}
	j = Job{Input: "/m/a.mkv", Decision: Decide(m, chrome, Limits{}), VideoIndex: 0, AudioIndex: 4, SubIndex: 3, SubRelIndex: 0, Dir: "/t", Encoder: "software", SubOffsetMS: 1500}
	j.Decision.BurnSubtitle = true
	cmd = strings.Join(j.Args(), " ")
	if !strings.Contains(cmd, "setpts=PTS-1.500/TB,subtitles=f='/m/a.mkv':si=0,setpts=PTS+1.500/TB") {
		t.Errorf("subtitle offset:\n%s", cmd)
	}
	// An audio offset rules out direct play.
	if d := Decide(archer(), appleTV, Limits{Resync: true}); d.Method == DirectPlay {
		t.Errorf("resync direct played: %+v", d)
	}
}

func TestJobNightMode(t *testing.T) {
	m := archer()
	m.Audio = &AudioStream{Codec: "eac3", Channels: 6}
	surround := "-filter:a aformat=channel_layouts=5.1,pan=stereo|FL=0.5*FC+0.35*FL+0.25*BL|FR=0.5*FC+0.35*FR+0.25*BR," +
		"acompressor=threshold=-24dB:ratio=4:attack=10:release=250:makeup=6dB,alimiter=limit=0.95 -c:a aac -ac 2 -b:a 192k"
	// 5.1 source, direct stream: video copied, audio downmixed with the dialogue lifted.
	j := Job{Input: "/m/a.mkv", Decision: Decide(m, appleTV, Limits{NightMode: true}), VideoIndex: 0, AudioIndex: 1, AudioChannels: 6,
		SubIndex: -1, Dir: "/t", Encoder: "software"}
	cmd := strings.Join(j.Args(), " ")
	if !strings.Contains(cmd, "-c:v copy") || !strings.Contains(cmd, surround) || strings.Contains(cmd, "-c:a copy") {
		t.Errorf("5.1 night mode:\n%s", cmd)
	}
	// Stereo source: only compression and the limiter.
	m.Audio = &AudioStream{Codec: "aac", Channels: 2}
	j.Decision, j.AudioChannels = Decide(m, appleTV, Limits{NightMode: true}), 2
	cmd = strings.Join(j.Args(), " ")
	if !strings.Contains(cmd, "-filter:a acompressor=threshold=-24dB:ratio=4:attack=10:release=250:makeup=6dB,alimiter=limit=0.95 -c:a aac -ac 2") ||
		strings.Contains(cmd, "pan=") {
		t.Errorf("stereo night mode:\n%s", cmd)
	}
	// Alongside a video transcode and an audio offset (PLAY-17): the shifted second input is filtered.
	j = Job{Input: "/m/a.mkv", Decision: Decide(archer(), chrome, Limits{NightMode: true, Resync: true}), VideoIndex: 0, AudioIndex: 1, AudioChannels: 6,
		SubIndex: -1, Dir: "/t", Encoder: "software", AudioOffsetMS: 300}
	cmd = strings.Join(j.Args(), " ")
	if !strings.Contains(cmd, "-itsoffset 0.300") || !strings.Contains(cmd, "-map 1:1 "+surround) || !strings.Contains(cmd, "filter_complex") {
		t.Errorf("transcode + offset night mode:\n%s", cmd)
	}
	// Off: no audio filter.
	j.Decision = Decide(archer(), chrome, Limits{})
	if cmd = strings.Join(j.Args(), " "); strings.Contains(cmd, "-filter:a") {
		t.Errorf("filter without night mode:\n%s", cmd)
	}
}
