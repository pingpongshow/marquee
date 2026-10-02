package playback

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// SegmentSeconds is the HLS segment length. Transcodes force keyframes on these boundaries
// so the playlist (which we generate) and FFmpeg's output always agree.
const SegmentSeconds = 6

// Encoders records which hardware encoders work on this machine.
type Encoders struct {
	NVENC     bool
	QSVDevice string // e.g. /dev/dri/renderD128, "" if Quick Sync is unavailable
}

// DetectEncoders test-encodes a fraction of a second with each hardware encoder.
func DetectEncoders(ctx context.Context, ffmpeg string) Encoders {
	var e Encoders
	run := func(args ...string) bool {
		ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		base := []string{"-hide_banner", "-v", "error"}
		return exec.CommandContext(ctx, ffmpeg, append(base, args...)...).Run() == nil
	}
	test := []string{"-f", "lavfi", "-i", "testsrc2=size=320x180:rate=10:duration=0.5"}
	e.NVENC = run(append(test, "-c:v", "h264_nvenc", "-f", "null", "-")...)
	for _, dev := range []string{"/dev/dri/renderD128", "/dev/dri/renderD129", "/dev/dri/renderD130"} {
		if _, err := os.Stat(dev); err != nil {
			continue
		}
		args := append([]string{"-init_hw_device", "qsv=qs:" + dev, "-filter_hw_device", "qs"}, test...)
		if run(append(args, "-vf", "hwupload=extra_hw_frames=16,format=qsv", "-c:v", "h264_qsv", "-f", "null", "-")...) {
			e.QSVDevice = dev
			break
		}
	}
	slog.Info("hardware encoders", "nvenc", e.NVENC, "qsv", e.QSVDevice != "")
	return e
}

// Available returns the configured encoder order filtered to what works here.
func (e Encoders) Available(order []string) []string {
	var out []string
	for _, k := range order {
		switch {
		case k == "nvenc" && e.NVENC, k == "qsv" && e.QSVDevice != "", k == "software":
			out = append(out, k)
		}
	}
	if len(out) == 0 || out[len(out)-1] != "software" {
		out = append(out, "software") // always the last resort
	}
	return out
}

// Job describes one FFmpeg run producing HLS for a session.
type Job struct {
	Input        string
	Decision     Decision
	VideoIndex   int // container stream index (-1 = none)
	AudioIndex   int
	SubIndex     int    // container stream index of a subtitle to burn (-1 = none)
	SubRelIndex  int    // index among the file's subtitle streams (for the subtitles filter)
	SubExternal  string // external subtitle file to burn
	SubImage     bool   // the subtitle to burn is a bitmap format (overlay) rather than text (libass)
	VideoCodec   string // source video codec (for stream-copy tagging)
	StartSegment int
	Dir          string
	Encoder      string // nvenc, qsv, software
	QSVDevice    string
	Preset       string // speed, balanced, quality
}

func presetFor(encoder, p string) string {
	switch encoder {
	case "nvenc":
		return map[string]string{"speed": "p2", "balanced": "p4", "quality": "p6"}[p]
	case "qsv":
		return map[string]string{"speed": "veryfast", "balanced": "medium", "quality": "slow"}[p]
	}
	return map[string]string{"speed": "superfast", "balanced": "veryfast", "quality": "fast"}[p]
}

// escapeFilterPath quotes a path for use inside an FFmpeg filter argument.
func escapeFilterPath(p string) string {
	r := strings.NewReplacer(`\`, `\\`, `'`, `\'`, `:`, `\:`, `,`, `\,`, `[`, `\[`, `]`, `\]`, `;`, `\;`)
	return "'" + r.Replace(p) + "'"
}

// Args builds the FFmpeg command line.
func (j Job) Args() []string {
	d := j.Decision
	start := float64(j.StartSegment * SegmentSeconds)
	var a []string
	a = append(a, "-hide_banner", "-v", "warning", "-nostdin", "-y")

	burn := d.BurnSubtitle && (j.SubIndex >= 0 || j.SubExternal != "")
	// Full-GPU decoding unless frames need CPU work (subtitle overlay, or tone mapping off NVIDIA).
	gpuDecode := !d.VideoCopy && !burn && (j.Encoder == "nvenc" || (j.Encoder == "qsv" && !d.ToneMap))
	if gpuDecode {
		switch j.Encoder {
		case "nvenc":
			a = append(a, "-hwaccel", "cuda", "-hwaccel_output_format", "cuda")
		case "qsv":
			a = append(a, "-init_hw_device", "qsv=qs:"+j.QSVDevice, "-filter_hw_device", "qs", "-hwaccel", "qsv", "-hwaccel_output_format", "qsv")
		}
	} else if j.Encoder == "qsv" && !d.VideoCopy {
		a = append(a, "-init_hw_device", "qsv=qs:"+j.QSVDevice, "-filter_hw_device", "qs")
	}
	if start > 0 {
		a = append(a, "-ss", strconv.FormatFloat(start, 'f', 3, 64))
	}
	a = append(a, "-copyts", "-i", j.Input)
	imageExternal := burn && j.SubExternal != "" && isImageFile(j.SubExternal)
	if imageExternal {
		if start > 0 {
			a = append(a, "-ss", strconv.FormatFloat(start, 'f', 3, 64))
		}
		a = append(a, "-copyts", "-i", j.SubExternal)
	}

	// ---- video ----
	if j.VideoIndex >= 0 {
		if d.VideoCopy {
			a = append(a, "-map", fmt.Sprintf("0:%d", j.VideoIndex), "-c:v", "copy")
			if j.VideoCodec == "hevc" {
				a = append(a, "-tag:v", "hvc1") // Apple players reject hev1
			}
		} else {
			h := d.Height
			in := fmt.Sprintf("[0:%d]", j.VideoIndex)
			overlay := "" // second input for a bitmap subtitle overlay
			var chain []string
			switch {
			case gpuDecode && j.Encoder == "nvenc" && d.ToneMap:
				chain = append(chain, fmt.Sprintf("scale_cuda=w=-2:h=%d:format=p010le", h),
					"tonemap_cuda=format=nv12:p=bt709:t=bt709:m=bt709:tonemap=bt2390:peak=100:desat=0")
			case gpuDecode && j.Encoder == "nvenc":
				chain = append(chain, fmt.Sprintf("scale_cuda=w=-2:h=%d:format=nv12", h))
			case gpuDecode && j.Encoder == "qsv":
				chain = append(chain, fmt.Sprintf("scale_qsv=w=-1:h=%d:format=nv12", h))
			default:
				// CPU frames: draw subtitles at source size, tone map, scale, then hand the
				// frames to the hardware encoder.
				if burn {
					switch {
					case imageExternal:
						overlay = "[1:0]"
					case j.SubExternal != "":
						chain = append(chain, "subtitles=f="+escapeFilterPath(j.SubExternal))
					case j.SubImage:
						overlay = fmt.Sprintf("[0:%d]", j.SubIndex)
					default:
						chain = append(chain, fmt.Sprintf("subtitles=f=%s:si=%d", escapeFilterPath(j.Input), j.SubRelIndex))
					}
				}
				if d.ToneMap {
					chain = append(chain, "tonemapx=tonemap=bt2390:desat=0:peak=100:t=bt709:m=bt709:p=bt709:format=yuv420p")
				}
				chain = append(chain, fmt.Sprintf("scale=-2:%d", h), "format=yuv420p")
				switch j.Encoder {
				case "nvenc":
					chain = append(chain, "hwupload_cuda")
				case "qsv":
					chain = append(chain, "hwupload=extra_hw_frames=64", "format=qsv")
				}
			}
			graph := in + strings.Join(chain, ",")
			if overlay != "" {
				graph = in + overlay + "overlay=eof_action=pass," + strings.Join(chain, ",")
			}
			a = append(a, "-filter_complex", graph+"[v]", "-map", "[v]")
			kbps := d.VideoKbps
			rate := []string{"-b:v", fmt.Sprintf("%dk", kbps), "-maxrate", fmt.Sprintf("%dk", kbps*3/2), "-bufsize", fmt.Sprintf("%dk", kbps*2)}
			preset := presetFor(j.Encoder, j.Preset)
			switch j.Encoder {
			case "nvenc":
				codec := "h264_nvenc"
				if d.VideoCodec == "hevc" {
					codec = "hevc_nvenc"
				}
				a = append(a, "-c:v", codec, "-preset", preset, "-rc", "vbr", "-forced-idr", "1", "-spatial_aq", "1")
			case "qsv":
				codec := "h264_qsv"
				if d.VideoCodec == "hevc" {
					codec = "hevc_qsv"
				}
				a = append(a, "-c:v", codec, "-preset", preset, "-look_ahead", "0")
			default:
				a = append(a, "-c:v", "libx264", "-preset", preset, "-sc_threshold", "0", "-profile:v", "high")
			}
			if d.VideoCodec == "hevc" {
				a = append(a, "-tag:v", "hvc1") // Apple players need hvc1
			}
			a = append(a, rate...)
			a = append(a, "-force_key_frames", fmt.Sprintf("expr:gte(t,n_forced*%d)", SegmentSeconds))
		}
	}

	// ---- audio ----
	if j.AudioIndex >= 0 {
		a = append(a, "-map", fmt.Sprintf("0:%d", j.AudioIndex))
		if d.AudioCopy {
			a = append(a, "-c:a", "copy")
		} else {
			a = append(a, "-c:a", "aac", "-ac", strconv.Itoa(d.AudioChannels), "-b:a", fmt.Sprintf("%dk", audioKbps(d.AudioChannels)))
		}
	}

	a = append(a, "-sn", "-dn", "-map_metadata", "-1", "-map_chapters", "-1",
		"-avoid_negative_ts", "disabled", "-max_muxing_queue_size", "4096",
		"-f", "hls", "-hls_time", strconv.Itoa(SegmentSeconds), "-hls_segment_type", "fmp4",
		"-hls_fmp4_init_filename", "init.mp4",
		"-hls_segment_filename", filepath.Join(j.Dir, "%d.m4s"),
		"-start_number", strconv.Itoa(j.StartSegment),
		"-hls_list_size", "0", "-hls_playlist_type", "event", "-hls_flags", "temp_file",
		filepath.Join(j.Dir, "ffmpeg.m3u8"))
	return a
}

func isImageFile(p string) bool {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".sup", ".idx", ".sub":
		return true
	}
	return false
}
