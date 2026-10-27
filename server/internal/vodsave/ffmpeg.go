package vodsave

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"marquee/internal/encode"
	"marquee/internal/playback"
	"marquee/internal/settings"
)

// mediaInfo is what ffprobe says about a file.
type mediaInfo struct {
	DurationMS    int64
	Width, Height int
	Video         int // video streams (not cover pictures)
	VideoIndex    int
	PixFmt        string
	HDR           bool
	Audio         []stream
	Subtitles     []stream
}

type stream struct {
	Index    int
	Codec    string
	Channels int
}

type probeOut struct {
	Streams []struct {
		Index       int    `json:"index"`
		CodecType   string `json:"codec_type"`
		CodecName   string `json:"codec_name"`
		Width       int    `json:"width"`
		Height      int    `json:"height"`
		Channels    int    `json:"channels"`
		PixFmt      string `json:"pix_fmt"`
		Transfer    string `json:"color_transfer"`
		Disposition struct {
			AttachedPic int `json:"attached_pic"`
		} `json:"disposition"`
	} `json:"streams"`
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
}

func (s *Service) probe(ctx context.Context, path string) (mediaInfo, error) {
	var mi mediaInfo
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, or2(s.FFprobe, "ffprobe"), "-v", "error", "-print_format", "json", "-show_format", "-show_streams", path)
	cmd.Stderr = &encode.LimitWriter{W: &stderr, N: 4096}
	out, err := cmd.Output()
	if err != nil {
		return mi, fmt.Errorf("ffprobe couldn't read it: %s", encode.LastLine(stderr.String(), err))
	}
	var p probeOut
	if err := json.Unmarshal(out, &p); err != nil {
		return mi, fmt.Errorf("ffprobe's answer wasn't understood: %v", err)
	}
	if d, err := strconv.ParseFloat(p.Format.Duration, 64); err == nil {
		mi.DurationMS = int64(d * 1000)
	}
	mi.VideoIndex = -1
	for _, st := range p.Streams {
		switch st.CodecType {
		case "video":
			if st.Disposition.AttachedPic == 1 {
				continue
			}
			mi.Video++
			if mi.VideoIndex < 0 {
				mi.VideoIndex, mi.Width, mi.Height, mi.PixFmt = st.Index, st.Width, st.Height, st.PixFmt
				mi.HDR = st.Transfer == "smpte2084" || st.Transfer == "arib-std-b67"
			}
		case "audio":
			mi.Audio = append(mi.Audio, stream{Index: st.Index, Codec: st.CodecName, Channels: st.Channels})
		case "subtitle":
			mi.Subtitles = append(mi.Subtitles, stream{Index: st.Index, Codec: st.CodecName})
		}
	}
	if mi.Video == 0 {
		return mi, errors.New("it has no video")
	}
	return mi, nil
}

// mkvSubtitles are subtitle formats Matroska holds as they are; mov_text (MP4's) is
// converted to SRT, and anything else (closed captions, teletext) is left out.
var mkvSubtitles = map[string]bool{"subrip": true, "srt": true, "ass": true, "ssa": true, "webvtt": true,
	"hdmv_pgs_subtitle": true, "dvd_subtitle": true, "dvb_subtitle": true}

// processArgs is the FFmpeg command line that remuxes (every stream copied) or transcodes a
// downloaded file into an MKV at out. Transcodes follow the save settings: the resolution
// capped (never upscaled), HEVC or H.264 on the given encoder at a constant quality or
// bitrate, HDR tone-mapped to SDR when the transcoder does that, audio copied or made AAC.
func processArgs(c settings.VODSave, mode, in, out string, mi mediaInfo, encoder, preset, qsvDevice string, toneMap, withSubs bool) []string {
	a := []string{"-hide_banner", "-v", "error", "-nostdin", "-y"}
	transcode := mode == "transcode"
	if transcode {
		a = append(a, encode.HWInit(encoder, qsvDevice)...)
	}
	a = append(a, "-i", in)
	if transcode {
		a = append(a, "-map", fmt.Sprintf("0:%d", mi.VideoIndex))
	} else {
		a = append(a, "-map", "0:V")
	}
	for _, st := range mi.Audio {
		a = append(a, "-map", fmt.Sprintf("0:%d", st.Index))
	}
	var subs []stream
	if withSubs {
		for _, st := range mi.Subtitles {
			if mkvSubtitles[st.Codec] || st.Codec == "mov_text" {
				subs = append(subs, st)
				a = append(a, "-map", fmt.Sprintf("0:%d", st.Index))
			}
		}
	}
	a = append(a, "-map_metadata", "0", "-map_chapters", "0", "-c", "copy")
	for i, st := range subs {
		if st.Codec == "mov_text" {
			a = append(a, fmt.Sprintf("-c:s:%d", i), "srt")
		}
	}
	if transcode {
		var chain []string
		if mi.HDR && toneMap {
			chain = append(chain, encode.ToneMap)
		}
		h := c.MaxHeight
		if mi.Height > 0 && mi.Height < h {
			h = mi.Height
		}
		chain = append(chain, fmt.Sprintf("scale=-2:%d", h))
		tenBit := c.VideoCodec == "hevc" && strings.Contains(mi.PixFmt, "10") && !(mi.HDR && toneMap)
		chain = append(chain, encode.PixelFormat(encoder, tenBit)...)
		a = append(a, "-vf", strings.Join(chain, ","))
		a = append(a, encode.Video{Encoder: encoder, Codec: c.VideoCodec, Preset: preset, Quality: c.VideoQuality, Kbps: c.VideoKbps}.Args()...)
		for i, st := range mi.Audio {
			switch c.Audio {
			case "aac_stereo":
				a = append(a, fmt.Sprintf("-c:a:%d", i), "aac", fmt.Sprintf("-ac:a:%d", i), "2", fmt.Sprintf("-b:a:%d", i), "192k")
			case "aac_51":
				ch := min(max(st.Channels, 1), 6)
				a = append(a, fmt.Sprintf("-c:a:%d", i), "aac", fmt.Sprintf("-ac:a:%d", i), strconv.Itoa(ch),
					fmt.Sprintf("-b:a:%d", i), fmt.Sprintf("%dk", max(ch, 2)*64))
			}
		}
	}
	return append(a, "-max_muxing_queue_size", "4096", "-progress", "pipe:1", "-nostats", "-f", "matroska", out)
}

// process converts the download. A transcode tries the encoders best first, falling back
// to the CPU; a remux that fails on the subtitles is made again without them.
func (s *Service) process(ctx context.Context, j *job, c settings.VODSave, mode, in, out string, mi mediaInfo) error {
	encoders := []string{"software"}
	if mode == "transcode" && s.Encoders != nil {
		if e := s.Encoders(); len(e) > 0 {
			encoders = e
		}
	}
	tc := s.Settings.Get().Transcoder
	var err error
	for _, subs := range []bool{true, false} {
		for _, enc := range encoders {
			args := processArgs(c, mode, in, out, mi, enc, playback.EncoderPreset(enc, tc.Preset), s.QSVDevice, tc.ToneMapping, subs)
			if err = s.ffmpeg(ctx, args, mi.DurationMS, j.setProgress); err == nil || ctx.Err() != nil {
				return err
			}
		}
		if len(mi.Subtitles) == 0 {
			break
		}
	}
	return err
}

// ffmpeg runs FFmpeg, reporting progress (0–1) from -progress output.
func (s *Service) ffmpeg(ctx context.Context, args []string, durationMS int64, progress func(float64)) error {
	return encode.Run(ctx, or2(s.FFmpeg, "ffmpeg"), args, durationMS, func(p encode.Progress) {
		if progress != nil && durationMS > 0 {
			progress(p.Fraction)
		}
	}, nil)
}

// verify checks a finished file before it goes into the library: it has video, its length
// is what was expected (within 2%; a minute's slack for a runtime that isn't the file's own,
// unless strict), and its first and last seconds of picture decode without errors.
func (s *Service) verify(ctx context.Context, path string, mi mediaInfo, expectedMS int64, strict bool) (string, error) {
	if mi.Video == 0 {
		return "", errors.New("it has no video")
	}
	if mi.DurationMS <= 0 {
		return "", errors.New("its length is unknown")
	}
	if expectedMS > 0 {
		tol := expectedMS / 50
		if !strict {
			tol = max(tol, 60_000)
		} else {
			tol = max(tol, 2_000)
		}
		if d := mi.DurationMS - expectedMS; d > tol || -d > tol {
			return "", fmt.Errorf("it is %s long; expected %s", clock(mi.DurationMS), clock(expectedMS))
		}
	}
	// The picture can end before the file does, with the audio running on (seen: a film whose
	// picture stops 1:41 before its soundtrack), so the end is checked where the picture
	// stops. That's looked for in the file's last five minutes only: a picture that stops
	// earlier is found nowhere there, and the file's own end is checked (and fails) as before.
	secs := float64(mi.DurationMS) / 1000
	if ms, ok := encode.VideoEnd(ctx, or2(s.FFprobe, "ffprobe"), path, mi.VideoIndex, mi.DurationMS); ok && ms < mi.DurationMS {
		secs = float64(ms) / 1000
	}
	end := max(0, secs-min(30, secs/3)) // the last 30 s (the last third of a short file)
	for _, at := range []float64{0, end} {
		if err := s.decodeCheck(ctx, path, at); err != nil {
			where := "the start"
			if at > 0 {
				where = "the end"
			}
			return "", fmt.Errorf("%s doesn't decode: %v", where, err)
		}
	}
	note := fmt.Sprintf("%s, %dp, %d audio", clock(mi.DurationMS), Resolution(mi.Width, mi.Height), len(mi.Audio))
	if n := len(mi.Subtitles); n > 0 {
		note += fmt.Sprintf(", %d subtitles", n)
	}
	return note + "; start and end decode cleanly", nil
}

// decodeCheck decodes 5 seconds of video from at (seconds), failing on any error or when
// no frame comes out.
func (s *Service) decodeCheck(ctx context.Context, path string, at float64) error {
	return encode.DecodeCheck(ctx, or2(s.FFmpeg, "ffmpeg"), path, at, 5, false)
}

func clock(ms int64) string {
	d := time.Duration(ms) * time.Millisecond
	return fmt.Sprintf("%d:%02d:%02d", int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60)
}

func or2(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
