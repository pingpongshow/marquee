package playback

import "testing"

var chrome = DeviceProfile{
	Containers: []string{"mp4", "webm"}, VideoCodecs: []string{"h264", "vp9", "av1"}, AudioCodecs: []string{"aac", "mp3", "opus", "flac"},
	MaxAudioChannels: 2, HLS: true, HLSVideoCodecs: []string{"h264"}, HLSAudioCodecs: []string{"aac", "mp3"}, TextSubtitles: true,
}

var appleTV = DeviceProfile{
	Containers: []string{"mp4"}, VideoCodecs: []string{"h264", "hevc"}, AudioCodecs: []string{"aac", "ac3", "eac3", "flac", "alac"},
	MaxAudioChannels: 8, TenBit: true, HDR: []string{"hdr10", "hlg", "dolby_vision"}, HLS: true,
	HLSVideoCodecs: []string{"h264", "hevc"}, HLSAudioCodecs: []string{"aac", "ac3", "eac3", "flac", "alac"}, TextSubtitles: true,
}

// Archer S01E01 from the real library: HEVC 10-bit 1080p in MKV, DTS 5.1, PGS subtitles.
func archer() Media {
	return Media{Container: "mkv", BitrateKbps: 9000, DurationMS: 1_300_000,
		Video: &VideoStream{Codec: "hevc", Width: 1920, Height: 1080, BitDepth: 10, BitrateKbps: 8000},
		Audio: &AudioStream{Codec: "dts", Channels: 6}}
}

func TestDirectPlayMP4(t *testing.T) {
	m := Media{Container: "mp4", BitrateKbps: 5000, Video: &VideoStream{Codec: "h264", Height: 1080, BitDepth: 8}, Audio: &AudioStream{Codec: "aac", Channels: 2}}
	if d := Decide(m, chrome, Limits{}); d.Method != DirectPlay {
		t.Fatalf("got %s %v", d.Method, d.Reasons)
	}
}

func TestArcherOnChromeTranscodes(t *testing.T) {
	d := Decide(archer(), chrome, Limits{})
	if d.Method != Transcode || d.VideoCodec != "h264" || d.AudioCopy || d.AudioChannels != 2 || d.Height != 1080 {
		t.Fatalf("%+v", d)
	}
}

func TestArcherOnAppleTVRemuxesAndKeepsAudioWhenSupported(t *testing.T) {
	m := archer()
	d := Decide(m, appleTV, Limits{})
	if d.Method != DirectStream || !d.VideoCopy || d.AudioCopy || d.AudioCodec != "aac" || d.AudioChannels != 6 {
		t.Fatalf("DTS should become 5.1 AAC with video copied: %+v", d)
	}
	m.Audio = &AudioStream{Codec: "eac3", Channels: 6}
	if d := Decide(m, appleTV, Limits{}); d.Method != DirectStream || !d.AudioCopy {
		t.Fatalf("EAC3 should be copied: %+v", d)
	}
}

func TestImageSubtitlesForceBurnIn(t *testing.T) {
	m := archer()
	m.Subtitle = &SubtitleStream{Codec: "hdmv_pgs_subtitle", Index: 2}
	if d := Decide(m, appleTV, Limits{}); d.Method != Transcode || !d.BurnSubtitle {
		t.Fatalf("%+v", d)
	}
	m.Subtitle = &SubtitleStream{Codec: "subrip", Index: -1, External: "/x.srt"}
	if d := Decide(m, appleTV, Limits{}); d.Method != DirectStream || !d.SubtitleVTT {
		t.Fatalf("text subtitles shouldn't force a transcode: %+v", d)
	}
}

func TestRemoteLimitDownscales(t *testing.T) {
	d := Decide(archer(), appleTV, Limits{MaxKbps: 4000, Remote: true, PreferHEVC: true})
	if d.Method != Transcode || d.VideoCodec != "hevc" || d.VideoKbps > 4000-192 || d.Height > 1080 {
		t.Fatalf("%+v", d)
	}
	d = Decide(archer(), chrome, Limits{MaxKbps: 1500, Remote: true})
	if d.Height > 480 || d.VideoKbps > 1500 {
		t.Fatalf("1.5 Mbps should be ≤480p: %+v", d)
	}
}

func TestHDROnSDRDisplayToneMaps(t *testing.T) {
	m := archer()
	m.Video.HDR = "hdr10"
	p := appleTV
	p.HDR = nil
	if d := Decide(m, p, Limits{}); d.Method != Transcode || !d.ToneMap {
		t.Fatalf("%+v", d)
	}
}

func TestMusic(t *testing.T) {
	flac := Media{Container: "flac", BitrateKbps: 1000, Audio: &AudioStream{Codec: "flac", Channels: 2}}
	if d := Decide(flac, chrome, Limits{}); d.Method != DirectPlay {
		t.Fatalf("%+v", d)
	}
	if d := Decide(flac, chrome, Limits{MaxKbps: 320}); d.Method != Transcode || d.AudioCodec != "aac" {
		t.Fatalf("over limit should transcode: %+v", d)
	}
}

func TestHev1MP4IsRepackagedNotDirectPlayed(t *testing.T) {
	// The 'Burbs (1989): HEVC Main 10 tagged hev1 in MP4, AAC stereo.
	m := Media{Container: "mp4", BitrateKbps: 2228, Video: &VideoStream{Codec: "hevc", Tag: "hev1", Height: 1036, BitDepth: 10},
		Audio: &AudioStream{Codec: "aac", Channels: 2}}
	if d := Decide(m, appleTV, Limits{}); d.Method != DirectStream || !d.VideoCopy || !d.AudioCopy {
		t.Fatalf("hev1 should be repackaged with video and audio copied: %+v", d)
	}
	m.Video.Tag = "hvc1"
	if d := Decide(m, appleTV, Limits{}); d.Method != DirectPlay {
		t.Fatalf("hvc1 MP4 should direct play: %+v", d)
	}
}
