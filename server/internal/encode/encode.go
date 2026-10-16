// Package encode holds the FFmpeg pieces shared by the background re-encoders (VOD saves,
// the library optimizer): the video encoder options per encoder (NVENC, Quick Sync,
// software) and quality, running FFmpeg with progress, decode checks and running at low
// priority.
package encode

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// CRF is the constant-quality level for an encoder, codec and quality (high, balanced,
// small): -crf for the software encoders, -cq for NVENC, -global_quality for Quick Sync.
func CRF(encoder, codec, quality string) int {
	i := map[string]int{"high": 0, "balanced": 1, "small": 2}[quality]
	switch {
	case encoder == "software" && codec == "h264":
		return []int{18, 21, 24}[i]
	case encoder == "software" && codec == "av1":
		return []int{26, 30, 34}[i]
	case encoder == "software":
		return []int{20, 23, 26}[i]
	}
	return []int{20, 24, 28}[i] // NVENC -cq, Quick Sync -global_quality
}

// Video is how a video stream is encoded.
type Video struct {
	Encoder string // nvenc, qsv, software
	Codec   string // hevc, h264, av1
	Preset  string // the encoder's own preset name (playback.EncoderPreset)
	Quality string // high, balanced, small (used unless Kbps is set)
	Kbps    int    // a target bitrate; 0 = constant quality
	// TenBit makes a 10-bit (Main 10) stream.
	TenBit bool
	// HDRTransfer keeps HDR: the stream is tagged BT.2020 with this transfer (smpte2084 for
	// HDR10/PQ, arib-std-b67 for HLG); "" = SDR.
	HDRTransfer string
}

// encoderNames are the FFmpeg encoders per encoder kind and codec.
var encoderNames = map[string]map[string]string{
	"nvenc":    {"hevc": "hevc_nvenc", "av1": "av1_nvenc", "h264": "h264_nvenc"},
	"qsv":      {"hevc": "hevc_qsv", "av1": "av1_qsv", "h264": "h264_qsv"},
	"software": {"hevc": "libx265", "av1": "libsvtav1", "h264": "libx264"},
}

// EncoderName is the FFmpeg encoder for an encoder kind and codec, e.g. hevc_nvenc (H.264
// for an unknown codec).
func EncoderName(encoder, codec string) string {
	m, ok := encoderNames[encoder]
	if !ok {
		m = encoderNames["software"]
	}
	if n, ok := m[codec]; ok {
		return n
	}
	return m["h264"]
}

func (v Video) codecName() string { return EncoderName(v.Encoder, v.Codec) }

// Args are the video encoding options: -c:v, preset and rate control, plus Main 10 and
// HDR10 tagging when asked.
func (v Video) Args() []string {
	var a []string
	q := CRF(v.Encoder, v.Codec, v.Quality)
	switch v.Encoder {
	case "nvenc":
		a = append(a, "-c:v", v.codecName(), "-preset", v.Preset, "-rc", "vbr", "-spatial_aq", "1")
		if v.Kbps == 0 {
			a = append(a, "-cq", strconv.Itoa(q), "-b:v", "0")
		}
		if v.TenBit && v.Codec == "hevc" {
			a = append(a, "-profile:v", "main10")
		}
	case "qsv":
		a = append(a, "-c:v", v.codecName(), "-preset", v.Preset)
		if v.Kbps == 0 {
			a = append(a, "-global_quality", strconv.Itoa(q))
		}
		if v.TenBit && v.Codec == "hevc" {
			a = append(a, "-profile:v", "main10")
		}
	default:
		switch v.Codec {
		case "hevc":
			params := "log-level=error"
			if v.HDRTransfer != "" {
				params += ":repeat-headers=1:colorprim=bt2020:colormatrix=bt2020nc:transfer=" + v.HDRTransfer
				if v.HDRTransfer == "smpte2084" {
					params += ":hdr-opt=1"
				}
			}
			a = append(a, "-c:v", "libx265", "-preset", v.Preset, "-x265-params", params)
		case "av1":
			a = append(a, "-c:v", "libsvtav1", "-preset", svtPreset(v.Preset))
		default:
			a = append(a, "-c:v", "libx264", "-preset", v.Preset)
		}
		if v.Kbps == 0 {
			a = append(a, "-crf", strconv.Itoa(q))
		}
	}
	if k := v.Kbps; k > 0 {
		a = append(a, "-b:v", fmt.Sprintf("%dk", k), "-maxrate", fmt.Sprintf("%dk", k*3/2), "-bufsize", fmt.Sprintf("%dk", k*2))
	}
	if v.HDRTransfer != "" {
		a = append(a, "-color_primaries", "bt2020", "-color_trc", v.HDRTransfer, "-colorspace", "bt2020nc")
	}
	return a
}

// svtPreset maps x264-style preset names to SVT-AV1's numbered presets.
func svtPreset(p string) string {
	switch p {
	case "ultrafast", "superfast", "veryfast":
		return "10"
	case "faster", "fast":
		return "8"
	case "slow", "slower", "veryslow":
		return "5"
	}
	return "7"
}

// HWInit are the input options an encoder needs before -i (Quick Sync's device).
func HWInit(encoder, qsvDevice string) []string {
	if encoder == "qsv" {
		return []string{"-init_hw_device", "qsv=qs:" + qsvDevice, "-filter_hw_device", "qs"}
	}
	return nil
}

// PixelFormat ends a software filter chain in the pixel format the encoder takes (and
// uploads the frames to Quick Sync).
func PixelFormat(encoder string, tenBit bool) []string {
	var chain []string
	switch {
	case tenBit && encoder == "software":
		chain = append(chain, "format=yuv420p10le")
	case tenBit:
		chain = append(chain, "format=p010le")
	default:
		chain = append(chain, "format=yuv420p")
	}
	if encoder == "qsv" {
		chain = append(chain, "hwupload=extra_hw_frames=64", "format=qsv")
	}
	return chain
}

// ToneMap is the filter that tone-maps HDR to SDR (BT.709), as playback does.
const ToneMap = "tonemapx=tonemap=bt2390:desat=0:peak=100:t=bt709:m=bt709:p=bt709:format=yuv420p"

// Progress is how far an FFmpeg run has got.
type Progress struct {
	Fraction float64 // 0–1
	FPS      float64
	Speed    float64 // × real time
}

// Run runs FFmpeg (name and args, which should include -progress pipe:1 -nostats),
// reporting progress from its -progress output. cmd, when set, is told about the started
// process (to pause it).
func Run(ctx context.Context, name string, args []string, durationMS int64, progress func(Progress), started func(*exec.Cmd)) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = 5 * time.Second
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &LimitWriter{W: &stderr, N: 8192}
	if err := cmd.Start(); err != nil {
		return err
	}
	if started != nil {
		started(cmd)
	}
	var p Progress
	sc := bufio.NewScanner(stdout)
	for sc.Scan() {
		k, v, _ := strings.Cut(sc.Text(), "=")
		switch k {
		case "out_time_us":
			if us, err := strconv.ParseInt(v, 10, 64); err == nil && durationMS > 0 {
				p.Fraction = max(0, min(1, float64(us)/1000/float64(durationMS)))
			}
		case "fps":
			p.FPS, _ = strconv.ParseFloat(strings.TrimSpace(v), 64)
		case "speed":
			p.Speed, _ = strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(v), "x"), 64)
		case "progress":
			if progress != nil {
				progress(p)
			}
		}
	}
	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("FFmpeg: %s", LastLine(stderr.String(), err))
	}
	return nil
}

// DecodeCheck decodes seconds of the first video stream (and the audio, when withAudio)
// from at (seconds), failing on any error or when no frame comes out.
func DecodeCheck(ctx context.Context, ffmpeg, path string, at, seconds float64, withAudio bool) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	args := []string{"-hide_banner", "-v", "error", "-nostdin"}
	if at > 0 {
		args = append(args, "-ss", strconv.FormatFloat(at, 'f', 3, 64))
	}
	args = append(args, "-i", path, "-t", strconv.FormatFloat(seconds, 'f', 3, 64), "-map", "0:V:0")
	if withAudio {
		args = append(args, "-map", "0:a?")
	}
	args = append(args, "-f", "null", "-progress", "pipe:1", "-nostats", "-")
	cmd := exec.CommandContext(ctx, ffmpeg, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &LimitWriter{W: &stderr, N: 4096}
	out, err := cmd.Output()
	if err != nil {
		return errors.New(LastLine(stderr.String(), err))
	}
	if msg := strings.TrimSpace(stderr.String()); msg != "" {
		return errors.New(FirstLine(msg))
	}
	frames := 0
	for _, line := range strings.Split(string(out), "\n") {
		if v, ok := strings.CutPrefix(line, "frame="); ok {
			frames, _ = strconv.Atoi(strings.TrimSpace(v))
		}
	}
	if frames == 0 {
		return errors.New("no frames")
	}
	return nil
}

// LowPriority wraps a command in nice -n 19 and ionice -c3 (idle I/O) when they exist.
// lookPath finds them (nil = exec.LookPath). Both exec the command, so the process that
// runs is the command's own.
func LowPriority(lookPath func(string) (string, error), name string, args ...string) (string, []string) {
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	cs := append([]string{name}, args...)
	if p, err := lookPath("ionice"); err == nil {
		cs = append([]string{p, "-c3"}, cs...)
	}
	if p, err := lookPath("nice"); err == nil {
		cs = append([]string{p, "-n", "19"}, cs...)
	}
	return cs[0], cs[1:]
}

// LastLine is the last line of FFmpeg's error output, or err when there's none.
func LastLine(msg string, err error) string {
	msg = strings.TrimSpace(msg)
	if msg == "" {
		return err.Error()
	}
	if i := strings.LastIndex(msg, "\n"); i >= 0 {
		msg = msg[i+1:]
	}
	return msg
}

// FirstLine is a message's first line.
func FirstLine(msg string) string {
	if i := strings.Index(msg, "\n"); i >= 0 {
		return msg[:i]
	}
	return msg
}

// LimitWriter keeps the first N bytes written to it.
type LimitWriter struct {
	W io.Writer
	N int
}

func (l *LimitWriter) Write(p []byte) (int, error) {
	if l.N > 0 {
		k := min(len(p), l.N)
		l.W.Write(p[:k])
		l.N -= k
	}
	return len(p), nil
}

// Available returns a check for whether FFmpeg was built with an encoder (by name, e.g.
// libsvtav1), asking FFmpeg once, on first use. If FFmpeg can't be asked, every encoder
// counts as there (a failing one is skipped when it's used).
func Available(ffmpeg string) func(name string) bool {
	var once sync.Once
	var asked bool
	names := map[string]bool{}
	return func(name string) bool {
		once.Do(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			out, err := exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-encoders").Output()
			if err != nil {
				return
			}
			asked = true
			for _, line := range strings.Split(string(out), "\n") {
				if f := strings.Fields(line); len(f) >= 2 && len(f[0]) == 6 {
					names[f[1]] = true
				}
			}
		})
		return !asked || names[name]
	}
}
