package vodsave

import (
	"fmt"
	"strings"
)

// Comparing a film's or episode's file on demand with the library's copy, to tell whether
// saving it would add something, upgrade the library's copy, or neither.

// Verdicts.
const (
	VerdictNone   = "none"   // not in the library
	VerdictBetter = "better" // the copy on demand is better: saving it upgrades the library's
	VerdictSame   = "same"   // about the same
	VerdictWorse  = "worse"  // the library's copy is better
)

// Quality is what a video file is, for comparing.
type Quality struct {
	Width, Height int
	Codec         string // ffprobe's name: h264, hevc, av1…
	HDR           string // "" = SDR; any other value is HDR (HDR10, hdr10plus, HLG, DV…)
	BitrateKbps   int    // the whole file's; 0 = unknown
}

// Known reports whether the video's size is known.
func (q Quality) Known() bool { return q.Width > 0 || q.Height > 0 }

// Tier is the standard resolution: 2160, 1080, 720, 576 or 480 (0 = unknown).
func (q Quality) Tier() int { return Resolution(q.Width, q.Height) }

// codecFactor is how much picture a codec gets from a bit, H.264 being 1: HEVC and AV1 need
// about two-thirds of H.264's bitrate for the same quality.
func codecFactor(codec string) float64 {
	switch strings.ToLower(codec) {
	case "hevc", "h265", "av1":
		return 1.5
	case "vp9":
		return 1.4
	case "mpeg4", "msmpeg4v3", "xvid", "divx":
		return 0.75
	case "mpeg2video", "mpeg1video":
		return 0.5
	}
	return 1
}

// effective is the bitrate in H.264 terms (0 = unknown).
func (q Quality) effective() float64 { return float64(q.BitrateKbps) * codecFactor(q.Codec) }

// minBitrateRatio is how much higher one copy's codec-adjusted bitrate must be, at the same
// resolution and dynamic range, to count as better.
const minBitrateRatio = 1.5

func codecName(c string) string {
	switch strings.ToLower(c) {
	case "h264", "avc":
		return "H.264"
	case "hevc", "h265":
		return "HEVC"
	case "av1":
		return "AV1"
	case "vp9":
		return "VP9"
	case "mpeg2video":
		return "MPEG-2"
	case "mpeg4":
		return "MPEG-4"
	case "":
		return ""
	}
	return strings.ToUpper(c)
}

func hdrName(h string) string {
	switch strings.ToLower(h) {
	case "":
		return ""
	case "hdr10":
		return "HDR10"
	case "hdr10plus", "hdr10+":
		return "HDR10+"
	case "hlg":
		return "HLG"
	case "dolby_vision", "dv", "dovi":
		return "Dolby Vision"
	}
	return "HDR"
}

// Summary is e.g. "1080p HEVC HDR10 8 Mb/s" ("" when nothing is known).
func (q Quality) Summary() string {
	var parts []string
	if t := q.Tier(); t > 0 {
		parts = append(parts, fmt.Sprintf("%dp", t))
	}
	if c := codecName(q.Codec); c != "" {
		parts = append(parts, c)
	}
	if h := hdrName(q.HDR); h != "" {
		parts = append(parts, h)
	}
	if q.BitrateKbps > 0 {
		parts = append(parts, mbps(q.BitrateKbps))
	}
	return strings.Join(parts, " ")
}

func mbps(kbps int) string {
	if kbps >= 10_000 {
		return fmt.Sprintf("%d Mb/s", (kbps+500)/1000)
	}
	return fmt.Sprintf("%.1f Mb/s", float64(kbps)/1000)
}

// Compare says how the copy on demand (vod) compares with the library's (lib): resolution
// first, then HDR over SDR at the same resolution, then a codec-adjusted bitrate at least
// minBitrateRatio higher; else the same. The reason explains it.
func Compare(vod, lib Quality) (verdict, reason string) {
	if !lib.Known() {
		return VerdictSame, "The library's copy's quality isn't known"
	}
	if !vod.Known() {
		return VerdictSame, "The quality of the VOD copy isn't known"
	}
	vt, lt := vod.Tier(), lib.Tier()
	switch {
	case vt > lt:
		return VerdictBetter, fmt.Sprintf("Higher resolution (%dp; the library's is %dp)", vt, lt)
	case vt < lt:
		return VerdictWorse, fmt.Sprintf("The library's copy has a higher resolution (%dp; this is %dp)", lt, vt)
	}
	vh, lh := vod.HDR != "", lib.HDR != ""
	switch {
	case vh && !lh:
		return VerdictBetter, fmt.Sprintf("%s (the library's copy is SDR)", hdrName(vod.HDR))
	case lh && !vh:
		return VerdictWorse, fmt.Sprintf("The library's copy is %s; this is SDR", hdrName(lib.HDR))
	}
	ve, le := vod.effective(), lib.effective()
	if ve <= 0 || le <= 0 {
		return VerdictSame, fmt.Sprintf("Both are %dp", vt)
	}
	switch {
	case ve >= le*minBitrateRatio:
		return VerdictBetter, fmt.Sprintf("Same resolution with a much higher bitrate for its codec (%s vs %s)",
			mbps(vod.BitrateKbps)+" "+codecName(vod.Codec), mbps(lib.BitrateKbps)+" "+codecName(lib.Codec))
	case le >= ve*minBitrateRatio:
		return VerdictWorse, fmt.Sprintf("The library's copy has a much higher bitrate for its codec (%s vs %s)",
			mbps(lib.BitrateKbps)+" "+codecName(lib.Codec), mbps(vod.BitrateKbps)+" "+codecName(vod.Codec))
	}
	return VerdictSame, fmt.Sprintf("Both are %dp with a similar bitrate for their codecs", vt)
}

// better reports whether a is a better file than b (picking a library item's best file).
func better(a, b Quality) bool {
	v, _ := Compare(a, b)
	if v == VerdictSame && a.Known() && b.Known() && a.Tier() == b.Tier() {
		return a.effective() > b.effective()
	}
	return v == VerdictBetter || (!b.Known() && a.Known())
}
