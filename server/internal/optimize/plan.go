package optimize

import (
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"marquee/internal/encode"
	"marquee/internal/settings"
)

// Source is what ffprobe says about the file being optimized.
type Source struct {
	Path          string
	DurationMS    int64
	Width, Height int
	VideoIndex    int
	VideoCodec    string
	PixFmt        string
	Transfer      string // color_transfer: smpte2084 (PQ), arib-std-b67 (HLG), …
	DVProfile     int    // Dolby Vision profile, 0 = none
	Audio         []Stream
	Subtitles     []Stream
	Attachments   int
}

// HDR reports whether the video is HDR (PQ or HLG).
func (s Source) HDR() bool { return s.Transfer == "smpte2084" || s.Transfer == "arib-std-b67" }

// TenBit reports whether the video has more than 8 bits per sample.
func (s Source) TenBit() bool {
	return strings.Contains(s.PixFmt, "10") || strings.Contains(s.PixFmt, "12")
}

// Stream is an audio or subtitle stream.
type Stream struct {
	Index    int
	Codec    string
	Channels int
	Title    string
	Default  bool
	Comment  bool // commentary (disposition or title)
}

// textSubtitles are subtitle formats kept as they are in MKV; mov_text (MP4's) becomes SRT.
var textSubtitles = map[string]bool{"subrip": true, "srt": true, "ass": true, "ssa": true, "webvtt": true, "mov_text": true, "text": true}

// imageSubtitles are picture-based subtitle formats (kept or dropped by the preset).
var imageSubtitles = map[string]bool{"hdmv_pgs_subtitle": true, "dvd_subtitle": true, "dvb_subtitle": true}

// mp4Audio are audio codecs an MP4 holds as they are; others are converted to AAC there.
var mp4Audio = map[string]bool{"aac": true, "ac3": true, "eac3": true, "mp3": true, "alac": true, "opus": true, "flac": true}

// ErrUnsupported is returned for files the optimizer can't re-encode faithfully.
var ErrUnsupported = errors.New("unsupported")

// AudioOut is one audio stream of the result.
type AudioOut struct {
	Stream
	Codec    string // "copy" or "aac"
	Channels int    // for AAC
	Kbps     int    // for AAC
}

// SubOut is one subtitle stream of the result.
type SubOut struct {
	Stream
	Codec string // "copy", "srt" or "mov_text"
}

// Plan is how one file is re-encoded with a preset.
type Plan struct {
	Format string // matroska or mp4
	// Final is the path the result goes to: the original's, or with .mkv when the
	// original's container can't hold the result.
	Final        string
	ScaleW       int // 0 = no scaling
	ScaleH       int
	ToneMap      bool
	TenBit       bool
	HDRTransfer  string // kept HDR's transfer, "" = SDR
	Codec        string
	Quality      string
	VideoKbps    int
	Audio        []AudioOut
	Subtitles    []SubOut
	Attachments  bool
	Preset       settings.OptimizePreset
	SourceVideoW int
}

// boxes are the frame each resolution cap fits the picture in (so a letterboxed 3840×1600
// film capped at 1080p becomes 1920×800, not 2592×1080).
var boxes = map[int][2]int{2160: {3840, 2160}, 1080: {1920, 1080}, 720: {1280, 720}, 480: {854, 480}}

// NewPlan works out how src is re-encoded with p.
func NewPlan(src Source, p settings.OptimizePreset) (Plan, error) {
	pl := Plan{Codec: p.Codec, Quality: p.Quality, VideoKbps: p.VideoKbps, Preset: p, SourceVideoW: src.Width}
	if src.DVProfile == 5 {
		return pl, fmt.Errorf("%w: Dolby Vision profile 5 has no HDR10 picture to keep, so it can't be re-encoded faithfully", ErrUnsupported)
	}
	ext := strings.ToLower(filepath.Ext(src.Path))
	switch ext {
	case ".mp4", ".m4v", ".mov":
		pl.Format, pl.Final = "mp4", src.Path
	case ".mkv":
		pl.Format, pl.Final = "matroska", src.Path
	default:
		pl.Format, pl.Final = "matroska", strings.TrimSuffix(src.Path, filepath.Ext(src.Path))+".mkv"
	}
	// Every subtitle is kept: MP4 can't hold picture subtitles, so such a file becomes MKV.
	if pl.Format == "mp4" {
		for _, st := range src.Subtitles {
			if imageSubtitles[st.Codec] {
				pl.Format, pl.Final = "matroska", strings.TrimSuffix(src.Path, filepath.Ext(src.Path))+".mkv"
				break
			}
		}
	}
	if b, ok := boxes[p.MaxHeight]; ok && (src.Width > b[0] || src.Height > b[1]) {
		pl.ScaleW, pl.ScaleH = b[0], b[1]
	}
	if src.HDR() {
		// HDR is kept only in HEVC or AV1; H.264 (or asking for SDR) tone-maps.
		if p.HDR == "keep" && (p.Codec == "hevc" || p.Codec == "av1") {
			pl.HDRTransfer = src.Transfer
		} else {
			pl.ToneMap = true
		}
	}
	pl.TenBit = (p.Codec == "hevc" || p.Codec == "av1") && !pl.ToneMap && (src.TenBit() || pl.HDRTransfer != "")

	// Audio: every track (all languages, commentary), copied or converted to AAC.
	for _, a := range src.Audio {
		out := AudioOut{Stream: a, Codec: "copy"}
		want := 0
		switch p.Audio {
		case "aac_stereo":
			want = 2
		case "aac_51":
			want = min(max(a.Channels, 1), 6)
		}
		switch {
		case want > 0 && !(a.Codec == "aac" && a.Channels <= want):
			out.Codec, out.Channels = "aac", want
		case pl.Format == "mp4" && !mp4Audio[a.Codec]:
			out.Codec, out.Channels = "aac", min(max(a.Channels, 2), 6)
		}
		if out.Codec == "aac" {
			out.Kbps = map[bool]int{true: 192, false: max(out.Channels, 2) * 64}[out.Channels == 2]
		}
		pl.Audio = append(pl.Audio, out)
	}

	// Subtitles: every one is kept. Text as SRT in MKV (mov_text in MP4), pictures as they
	// are (the file is MKV then), closed-caption tracks as SRT; teletext can't be carried.
	for _, st := range src.Subtitles {
		switch {
		case textSubtitles[st.Codec] && pl.Format == "mp4":
			pl.Subtitles = append(pl.Subtitles, SubOut{Stream: st, Codec: "mov_text"})
		case st.Codec == "eia_608" && pl.Format == "mp4":
			pl.Subtitles = append(pl.Subtitles, SubOut{Stream: st, Codec: "mov_text"})
		case st.Codec == "mov_text" || st.Codec == "text" || st.Codec == "eia_608":
			pl.Subtitles = append(pl.Subtitles, SubOut{Stream: st, Codec: "srt"})
		case textSubtitles[st.Codec]:
			pl.Subtitles = append(pl.Subtitles, SubOut{Stream: st, Codec: "copy"})
		case imageSubtitles[st.Codec]:
			pl.Subtitles = append(pl.Subtitles, SubOut{Stream: st, Codec: "copy"})
		}
	}
	pl.Attachments = pl.Format == "matroska" && src.Attachments > 0 && ext == ".mkv"
	return pl, nil
}

// Args is the FFmpeg command line that re-encodes src into out with an encoder (nvenc,
// qsv, software) at its preset name.
func (pl Plan) Args(src Source, encoder, encPreset, qsvDevice, out string) []string {
	a := []string{"-hide_banner", "-v", "error", "-nostdin", "-y"}
	if encoder == "nvenc" {
		a = append(a, "-hwaccel", "cuda") // decode on the GPU where it can (frames come back for the filters)
	}
	a = append(a, encode.HWInit(encoder, qsvDevice)...)
	a = append(a, "-i", src.Path, "-map", fmt.Sprintf("0:%d", src.VideoIndex))
	for _, st := range pl.Audio {
		a = append(a, "-map", fmt.Sprintf("0:%d", st.Index))
	}
	for _, st := range pl.Subtitles {
		a = append(a, "-map", fmt.Sprintf("0:%d", st.Index))
	}
	if pl.Attachments {
		a = append(a, "-map", "0:t?")
	}
	a = append(a, "-map_metadata", "0", "-map_chapters", "0", "-c", "copy")

	var chain []string
	if pl.ToneMap {
		chain = append(chain, encode.ToneMap)
	}
	if pl.ScaleW > 0 {
		chain = append(chain, fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease:force_divisible_by=2", pl.ScaleW, pl.ScaleH))
	}
	chain = append(chain, encode.PixelFormat(encoder, pl.TenBit)...)
	a = append(a, "-vf", strings.Join(chain, ","))
	a = append(a, encode.Video{Encoder: encoder, Codec: pl.Codec, Preset: encPreset, Quality: pl.Quality, Kbps: pl.VideoKbps,
		TenBit: pl.TenBit, HDRTransfer: pl.HDRTransfer}.Args()...)
	if pl.Format == "mp4" && pl.Codec == "hevc" {
		a = append(a, "-tag:v", "hvc1") // so Apple devices play it
	}
	for i, st := range pl.Audio {
		if st.Codec == "aac" {
			a = append(a, fmt.Sprintf("-c:a:%d", i), "aac", fmt.Sprintf("-ac:a:%d", i), strconv.Itoa(st.Channels),
				fmt.Sprintf("-b:a:%d", i), fmt.Sprintf("%dk", st.Kbps))
		}
	}
	for i, st := range pl.Subtitles {
		if st.Codec != "copy" {
			a = append(a, fmt.Sprintf("-c:s:%d", i), st.Codec)
		}
	}
	a = append(a, "-max_muxing_queue_size", "4096", "-progress", "pipe:1", "-nostats")
	if pl.Format == "mp4" {
		a = append(a, "-movflags", "+faststart")
	}
	return append(a, "-f", pl.Format, out)
}

// ---------- size estimates (from what the library knows; nothing is probed) ----------

// FileInfo is what the library knows about a file, for estimates.
type FileInfo struct {
	Size, DurationMS int64
	Width, Height    int
	HDR              bool
	Audio            []AudioInfo
}

// AudioInfo is an audio stream as the library knows it.
type AudioInfo struct {
	Codec      string
	Channels   int
	Kbps       int // 0 = unknown
	Default    bool
	Commentary bool
}

// Class is a resolution class by width or height (letterboxed films count by width):
// 2160, 1080, 720 or 480 (SD).
func Class(w, h int) int {
	switch {
	case w >= 3200 || h >= 2000:
		return 2160
	case w >= 1700 || h >= 1000:
		return 1080
	case w >= 1200 || h >= 700:
		return 720
	}
	return 480
}

// videoKbps are typical HEVC bitrates at the balanced quality per resolution class.
var videoKbps = map[int]float64{2160: 14000, 1080: 5000, 720: 2500, 480: 1200}

// audioKbps guesses a track's bitrate when the library doesn't know it.
func audioKbps(a AudioInfo) int {
	if a.Kbps > 0 {
		return a.Kbps
	}
	ch := max(a.Channels, 2)
	switch {
	case a.Codec == "truehd":
		return 3500
	case a.Codec == "dts":
		return 1536
	case a.Codec == "eac3":
		return 640
	case a.Codec == "ac3":
		return 448
	case a.Codec == "flac":
		return 450 * ch
	case strings.HasPrefix(a.Codec, "pcm"):
		return 768 * ch
	}
	return 64 * ch
}

// Estimate is about how big a file will be after a preset: the preset's typical video
// bitrate for the resulting resolution (never more than the file's own), plus the audio
// kept, never more than the file is now.
func Estimate(f FileInfo, p settings.OptimizePreset) int64 {
	if f.DurationMS <= 0 || f.Size <= 0 {
		return f.Size
	}
	secs := float64(f.DurationMS) / 1000
	class := Class(f.Width, f.Height)
	if p.MaxHeight > 0 && p.MaxHeight < class {
		class = p.MaxHeight
	}
	v := videoKbps[class] * map[string]float64{"high": 1.4, "balanced": 1, "small": 0.65}[p.Quality]
	switch p.Codec {
	case "h264":
		v *= 1.7
	case "av1":
		v *= 0.8
	}
	if f.HDR && p.HDR == "keep" && p.Codec != "h264" {
		v *= 1.15
	}
	if p.VideoKbps > 0 {
		v = float64(p.VideoKbps)
	}
	// Audio: every track is kept.
	tracks := f.Audio
	var srcAudio, outAudio float64
	for _, a := range f.Audio {
		srcAudio += float64(audioKbps(a))
	}
	for _, a := range tracks {
		switch p.Audio {
		case "aac_stereo":
			outAudio += 192
		case "aac_51":
			outAudio += float64(64 * min(max(a.Channels, 2), 6))
		default:
			outAudio += float64(audioKbps(a))
		}
	}
	// The file's own video bitrate caps the estimate: re-encoding never adds bits.
	srcVideo := float64(f.Size)*8/1000/secs - srcAudio
	if srcVideo > 0 && v > srcVideo {
		v = srcVideo
	}
	est := int64((v + outAudio) * 1000 / 8 * secs * 1.01) // ~1% container overhead
	return min(est, f.Size)
}
