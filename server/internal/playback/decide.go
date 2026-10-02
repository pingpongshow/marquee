// Package playback decides how a file reaches a client (PLAY-1), manages playback sessions,
// and runs FFmpeg to produce HLS when the file can't be played as-is.
package playback

import (
	"fmt"
	"slices"
	"strings"
)

// DeviceProfile describes what a client can play. Clients send it with every session request.
type DeviceProfile struct {
	// Containers the client can play directly from a file URL (e.g. mp4, webm).
	Containers []string
	// VideoCodecs playable in those containers and in HLS fMP4.
	VideoCodecs []string
	// AudioCodecs playable in those containers and in HLS fMP4.
	AudioCodecs      []string
	MaxAudioChannels int
	MaxHeight        int  // 0 = no limit
	TenBit           bool // can decode 10-bit video
	HDR              []string
	// HLS is true when the client plays HLS (natively or with hls.js).
	HLS bool
	// HLSVideoCodecs/HLSAudioCodecs may differ from the direct-play lists (e.g. Safari).
	HLSVideoCodecs []string
	HLSAudioCodecs []string
	// TextSubtitles: the client can show WebVTT sidecar subtitles.
	TextSubtitles bool
	// ASSSubtitles: the client renders styled ASS/SSA itself.
	ASSSubtitles bool
	// HLSSubtitles: text subtitles must be WebVTT renditions inside HLS (AVPlayer).
	HLSSubtitles bool
}

type Method string

const (
	DirectPlay   Method = "direct_play"
	DirectStream Method = "direct_stream"
	Transcode    Method = "transcode"
)

// Media is the part of a probed file the decision needs.
type Media struct {
	Container   string
	BitrateKbps int
	DurationMS  int64
	Video       *VideoStream
	Audio       *AudioStream
	Subtitle    *SubtitleStream // selected subtitle, nil = none
}

type VideoStream struct {
	Index                   int
	Codec                   string
	Tag                     string // container codec tag, e.g. hvc1 / hev1 / avc1
	Width, Height, BitDepth int
	HDR                     string // "", hdr10, hlg, dolby_vision
	BitrateKbps             int
	FrameRate               float64
}

type AudioStream struct {
	Index       int
	Codec       string
	Channels    int
	BitrateKbps int
	Language    string
}

type SubtitleStream struct {
	ID       int64
	Index    int // container stream index; -1 for external files
	Codec    string
	External string // sidecar path
}

// IsImage reports bitmap subtitles, which can only be shown by burning them into the video.
func (s *SubtitleStream) IsImage() bool {
	switch s.Codec {
	case "hdmv_pgs_subtitle", "dvd_subtitle", "dvb_subtitle", "xsub":
		return true
	}
	return false
}

// Limits are the quality limits for this session (see quality.go).
type Limits struct {
	MaxKbps    int // 0 = no limit
	MaxHeight  int // 0 = no limit
	Remote     bool
	PreferHEVC bool
	// NoVideoCopy rules out direct stream: the file has no usable keyframe index (D48).
	NoVideoCopy bool
}

// Decision is the plan for a session.
type Decision struct {
	Method        Method
	VideoCopy     bool
	VideoCodec    string // output codec when transcoding: h264, hevc
	Height        int    // output height when transcoding
	VideoKbps     int
	AudioCopy     bool
	AudioCodec    string // output audio codec when not copying: aac
	AudioChannels int
	BurnSubtitle  bool // burn the selected (image) subtitle into the video
	SubtitleVTT   bool // deliver the selected text subtitle as a WebVTT sidecar
	SubtitleASS   bool // deliver it as the original ASS (client renders styling and fonts)
	SubtitleHLS   bool // deliver it as a WebVTT rendition in the HLS master playlist
	ToneMap       bool
	Reasons       []string
	retag         bool // video is fine apart from its codec tag; direct stream fixes it
}

func has(list []string, v string) bool { return slices.Contains(list, strings.ToLower(v)) }

// normalizeContainer maps ffprobe/extension names to profile names.
func normalizeContainer(c string) string {
	switch c {
	case "matroska", "matroska,webm":
		return "mkv"
	case "mov", "m4v":
		return "mp4"
	}
	return c
}

// Decide chooses how to play m on a device with profile p under limits l.
func Decide(m Media, p DeviceProfile, l Limits) Decision {
	var d Decision
	reason := func(f string, a ...any) { d.Reasons = append(d.Reasons, fmt.Sprintf(f, a...)) }
	container := normalizeContainer(m.Container)

	// ---- audio-only (music) ----
	if m.Video == nil {
		switch {
		case m.Audio == nil:
			reason("no audio or video stream")
			d.Method = DirectPlay
		case !has(p.AudioCodecs, m.Audio.Codec):
			reason("audio codec %s not supported", m.Audio.Codec)
		case l.MaxKbps > 0 && m.BitrateKbps > l.MaxKbps:
			reason("bitrate %d kbps over the %d kbps limit", m.BitrateKbps, l.MaxKbps)
		default:
			d.Method = DirectPlay
			return d
		}
		d.Method = Transcode
		d.AudioCodec, d.AudioChannels = "aac", min(max(m.Audio.Channels, 2), 2)
		return d
	}

	v, a := m.Video, m.Audio
	maxH := l.MaxHeight
	if p.MaxHeight > 0 && (maxH == 0 || p.MaxHeight < maxH) {
		maxH = p.MaxHeight
	}

	videoOK := true
	switch {
	case !has(p.VideoCodecs, v.Codec):
		videoOK = false
		reason("video codec %s not supported", v.Codec)
	case v.Codec == "hevc" && strings.EqualFold(v.Tag, "hev1") && container == "mp4":
		// Apple players (and Safari) only play HEVC tagged hvc1; repackaging retags it.
		videoOK = false
		d.retag = true
		reason("HEVC tagged hev1 must be repackaged as hvc1")
	case v.BitDepth > 8 && !p.TenBit:
		videoOK = false
		reason("%d-bit video not supported", v.BitDepth)
	case v.HDR != "" && !has(p.HDR, v.HDR):
		videoOK = false
		d.ToneMap = true
		reason("%s not supported by this display", strings.ReplaceAll(v.HDR, "_", " "))
	case maxH > 0 && v.Height > maxH:
		videoOK = false
		reason("%dp is above the %dp limit", v.Height, maxH)
	}
	bitrateOK := l.MaxKbps == 0 || m.BitrateKbps == 0 || m.BitrateKbps <= l.MaxKbps
	if !bitrateOK {
		reason("bitrate %d kbps over the %d kbps limit", m.BitrateKbps, l.MaxKbps)
	}
	audioOK := a == nil || (has(p.AudioCodecs, a.Codec) && (p.MaxAudioChannels == 0 || a.Channels <= p.MaxAudioChannels))
	if !audioOK {
		reason("audio %s (%d ch) not supported", a.Codec, a.Channels)
	}
	containerOK := has(p.Containers, container)
	if !containerOK {
		reason("container %s not supported", container)
	}
	if s := m.Subtitle; s != nil {
		if s.IsImage() {
			d.BurnSubtitle = true
			reason("image subtitles (%s) must be burned in", s.Codec)
		} else if (s.Codec == "ass" || s.Codec == "ssa") && p.ASSSubtitles {
			d.SubtitleASS = true
		} else if p.HLSSubtitles && p.HLS {
			d.SubtitleHLS = true
		} else if p.TextSubtitles {
			d.SubtitleVTT = true
		} else {
			d.BurnSubtitle = true
			reason("this device can't show text subtitles")
		}
	}

	if d.SubtitleHLS && videoOK && audioOK && containerOK && bitrateOK {
		reason("subtitles are delivered inside HLS")
	}
	if videoOK && audioOK && containerOK && bitrateOK && !d.BurnSubtitle && !d.SubtitleHLS {
		d.Method = DirectPlay
		d.VideoCopy, d.AudioCopy = true, true
		return d
	}

	// Keep the original video, repackaged as HLS (audio converted if needed).
	if (videoOK || d.retag) && bitrateOK && !d.BurnSubtitle && p.HLS && has(p.HLSVideoCodecs, v.Codec) && l.NoVideoCopy {
		reason("no keyframe index, so the video can't be repackaged")
	}
	if (videoOK || d.retag) && bitrateOK && !d.BurnSubtitle && p.HLS && has(p.HLSVideoCodecs, v.Codec) && !l.NoVideoCopy {
		d.Method = DirectStream
		d.VideoCopy = true
		d.AudioCopy = a == nil || (has(p.HLSAudioCodecs, a.Codec) && (p.MaxAudioChannels == 0 || a.Channels <= p.MaxAudioChannels))
		if !d.AudioCopy {
			d.AudioCodec, d.AudioChannels = audioTarget(a, p)
		}
		return d
	}

	// Transcode.
	d.Method = Transcode
	d.VideoCodec = "h264"
	if l.PreferHEVC && has(p.HLSVideoCodecs, "hevc") {
		d.VideoCodec = "hevc"
	}
	d.Height = v.Height
	if maxH > 0 && d.Height > maxH {
		d.Height = maxH
	}
	d.VideoKbps = videoBitrate(v, m.BitrateKbps, d.Height, l.MaxKbps, d.VideoCodec)
	if d.VideoKbps > 0 && l.MaxKbps > 0 {
		// Leave room for audio within the limit and pick a sensible height for the rate.
		if h := heightForKbps(d.VideoKbps, d.VideoCodec); h < d.Height {
			d.Height = h
		}
	}
	d.AudioCopy = a == nil || (has(p.HLSAudioCodecs, a.Codec) && (p.MaxAudioChannels == 0 || a.Channels <= p.MaxAudioChannels) && l.MaxKbps == 0)
	if !d.AudioCopy {
		d.AudioCodec, d.AudioChannels = audioTarget(a, p)
	}
	return d
}

func audioTarget(a *AudioStream, p DeviceProfile) (string, int) {
	ch := 2
	if a != nil && a.Channels > 2 && (p.MaxAudioChannels == 0 || p.MaxAudioChannels >= 6) && has(p.HLSAudioCodecs, "aac") {
		ch = min(a.Channels, 6)
	}
	return "aac", ch
}

// audioKbps is what the transcoded audio costs.
func audioKbps(channels int) int {
	if channels > 2 {
		return 384
	}
	return 192
}

// videoBitrate picks the transcode video bitrate: the limit minus audio, but never more than
// the source needs (scaled for resolution), and a sensible default with no limit.
func videoBitrate(v *VideoStream, totalKbps, outHeight, limitKbps int, codec string) int {
	src := v.BitrateKbps
	if src == 0 {
		src = totalKbps
	}
	natural := naturalKbps(outHeight, codec)
	if src > 0 && v.Height > 0 && outHeight < v.Height {
		// Scale the source rate by pixel count when downscaling.
		ratio := float64(outHeight*outHeight) / float64(v.Height*v.Height)
		src = int(float64(src) * ratio)
	}
	rate := natural
	if src > 0 && src < rate {
		rate = src
	}
	if limitKbps > 0 {
		rate = min(rate, max(limitKbps-audioKbps(2), 300))
	}
	return rate
}

// naturalKbps is a good-quality rate for a height (H.264; HEVC needs ~60%).
func naturalKbps(height int, codec string) int {
	var k int
	switch {
	case height >= 2160:
		k = 40000
	case height >= 1440:
		k = 20000
	case height >= 1080:
		k = 12000
	case height >= 720:
		k = 6000
	case height >= 480:
		k = 2500
	default:
		k = 1200
	}
	if codec == "hevc" {
		k = k * 6 / 10
	}
	return k
}

// heightForKbps is the largest standard height that looks good at a bitrate.
func heightForKbps(kbps int, codec string) int {
	for _, h := range []int{2160, 1440, 1080, 720, 480, 360} {
		if kbps >= naturalKbps(h, codec)*55/100 {
			return h
		}
	}
	return 240
}
