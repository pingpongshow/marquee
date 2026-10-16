package commercials

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Cutting: the kept stretches are joined into a new file next to the recordings, with every
// video, audio and subtitle stream. Stream copy first (each stretch starting on a keyframe);
// if that can't cut cleanly or the result
// doesn't check out, the stretches are re-encoded (video on the configured encoder, best
// first; audio and subtitles copied) and joined.

// probeInfo is what ffprobe says about a file.
type probeInfo struct {
	DurationMS         int64
	StartMS            int64 // the first timestamp (MPEG-TS rarely starts at 0)
	Video, Audio, Subs int
	AudioCodecs        []string
	VideoCodec, PixFmt string
}

type probeJSON struct {
	Streams []struct {
		CodecType   string `json:"codec_type"`
		CodecName   string `json:"codec_name"`
		PixFmt      string `json:"pix_fmt"`
		Disposition struct {
			AttachedPic int `json:"attached_pic"`
		} `json:"disposition"`
	} `json:"streams"`
	Format struct {
		Duration  string `json:"duration"`
		StartTime string `json:"start_time"`
	} `json:"format"`
}

func (s *Service) ffprobe() string { return or(s.FFprobe, "ffprobe") }
func (s *Service) ffmpeg() string  { return or(s.FFmpeg, "ffmpeg") }

func or(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func (s *Service) probe(ctx context.Context, path string) (probeInfo, error) {
	var pi probeInfo
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, s.ffprobe(), "-v", "error", "-print_format", "json", "-show_format", "-show_streams", path)
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return pi, fmt.Errorf("ffprobe couldn't read it%s", tail(stderr.String()+" "+err.Error()))
	}
	var p probeJSON
	if err := json.Unmarshal(out, &p); err != nil {
		return pi, err
	}
	if d, err := strconv.ParseFloat(p.Format.Duration, 64); err == nil {
		pi.DurationMS = int64(math.Round(d * 1000))
	}
	if st, err := strconv.ParseFloat(p.Format.StartTime, 64); err == nil {
		pi.StartMS = int64(math.Round(st * 1000))
	}
	for _, st := range p.Streams {
		switch st.CodecType {
		case "video":
			if st.Disposition.AttachedPic == 1 {
				continue
			}
			if pi.Video == 0 {
				pi.VideoCodec, pi.PixFmt = st.CodecName, st.PixFmt
			}
			pi.Video++
		case "audio":
			pi.Audio++
			pi.AudioCodecs = append(pi.AudioCodecs, st.CodecName)
		case "subtitle":
			pi.Subs++
		}
	}
	return pi, nil
}

// keyframes lists the first video stream's keyframes (ms from the start of the file). It
// reads the whole file's packets but decodes nothing.
func (s *Service) keyframes(ctx context.Context, path string, startMS int64, durationMS int64) ([]int64, error) {
	ctx, cancel := context.WithTimeout(ctx, max(10*time.Minute, time.Duration(durationMS)*time.Millisecond/4))
	defer cancel()
	name, args := s.lowPriority(s.ffprobe(), "-v", "error", "-select_streams", "v:0",
		"-show_entries", "packet=pts_time,flags", "-of", "csv=p=0", path)
	out, err := exec.CommandContext(ctx, name, args...).Output()
	if err != nil {
		return nil, fmt.Errorf("listing keyframes: %w", err)
	}
	var kfs []int64
	for _, line := range strings.Split(string(out), "\n") {
		pts, flags, ok := strings.Cut(strings.TrimSpace(line), ",")
		if !ok || !strings.Contains(flags, "K") {
			continue
		}
		if t, err := strconv.ParseFloat(pts, 64); err == nil {
			kfs = append(kfs, int64(math.Round(t*1000))-startMS)
		}
	}
	sort.Slice(kfs, func(i, j int) bool { return kfs[i] < kfs[j] })
	return kfs, nil
}

// lowPriority wraps a command in nice -n 19 and ionice -c3 when they exist.
func (s *Service) lowPriority(name string, args ...string) (string, []string) {
	cs := append([]string{name}, args...)
	if p, err := s.lookPath("ionice"); err == nil {
		cs = append([]string{p, "-c3"}, cs...)
	}
	if p, err := s.lookPath("nice"); err == nil {
		cs = append([]string{p, "-n", "19"}, cs...)
	}
	return cs[0], cs[1:]
}

func (s *Service) run(ctx context.Context, name string, args ...string) error {
	name, args = s.lowPriority(name, args...)
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = 5 * time.Second
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("FFmpeg failed%s", tail(stderr.String()+" "+err.Error()))
	}
	return nil
}

func secs(ms int64) string { return strconv.FormatFloat(float64(ms)/1000, 'f', 3, 64) }

// cutCopy joins the kept stretches with stream copy: each is copied into a file of its own
// from the keyframe it starts on, and the files are joined. (The concat demuxer's in and out
// points on the recording itself leave clashing timestamps at the joins.)
func (s *Service) cutCopy(ctx context.Context, src, out, work string, pi probeInfo, plan Plan) error {
	ext := filepath.Ext(out)
	var b strings.Builder
	for i, k := range plan.Keep {
		seg := filepath.Join(work, fmt.Sprintf("copy-%03d%s", i, ext))
		args := []string{"-hide_banner", "-v", "error", "-nostdin", "-y"}
		if k.StartMS > 0 {
			// A millisecond past the keyframe, so rounding can't land on the one before.
			args = append(args, "-ss", secs(k.StartMS+1))
		}
		args = append(args, "-i", src)
		if k.EndMS < pi.DurationMS {
			args = append(args, "-t", secs(k.EndMS-k.StartMS))
		}
		args = append(args, "-map", "0:v?", "-map", "0:a?", "-map", "0:s?", "-c", "copy", "-avoid_negative_ts", "make_zero",
			"-max_muxing_queue_size", "4096", seg)
		if err := s.run(ctx, s.ffmpeg(), args...); err != nil {
			return err
		}
		fmt.Fprintf(&b, "file '%s'\n", strings.ReplaceAll(seg, "'", `'\''`))
	}
	return s.join(ctx, work, b.String(), out)
}

// join concatenates segment files (listed for the concat demuxer) with stream copy.
func (s *Service) join(ctx context.Context, work, list, out string) error {
	p := filepath.Join(work, "segments.txt")
	if err := os.WriteFile(p, []byte(list), 0o644); err != nil {
		return err
	}
	return s.run(ctx, s.ffmpeg(), "-hide_banner", "-v", "error", "-nostdin", "-y", "-f", "concat", "-safe", "0", "-i", p,
		"-map", "0", "-c", "copy", "-max_muxing_queue_size", "4096", out)
}

// encoderArgs are the video encoding options for an encoder (nvenc, qsv, software).
func (s *Service) encoderArgs(encoder string, pi probeInfo) (pre, enc []string) {
	preset := "balanced"
	if s.Preset != nil {
		preset = or(s.Preset(), preset)
	}
	hevc := pi.VideoCodec == "hevc"
	switch encoder {
	case "nvenc":
		codec := map[bool]string{true: "hevc_nvenc", false: "h264_nvenc"}[hevc]
		p := map[string]string{"speed": "p2", "balanced": "p4", "quality": "p6"}[preset]
		return nil, []string{"-c:v", codec, "-preset", or(p, "p4"), "-rc", "vbr", "-cq", "19", "-b:v", "0"}
	case "qsv":
		codec := map[bool]string{true: "hevc_qsv", false: "h264_qsv"}[hevc]
		p := map[string]string{"speed": "veryfast", "balanced": "medium", "quality": "slow"}[preset]
		return []string{"-init_hw_device", "qsv=qs:" + s.QSVDevice, "-filter_hw_device", "qs"},
			[]string{"-vf", "format=nv12,hwupload=extra_hw_frames=64,format=qsv", "-c:v", codec, "-preset", or(p, "medium"), "-global_quality", "20"}
	}
	p := map[string]string{"speed": "superfast", "balanced": "veryfast", "quality": "fast"}[preset]
	if hevc {
		return nil, []string{"-c:v", "libx265", "-preset", or(p, "veryfast"), "-crf", "20", "-x265-params", "log-level=error"}
	}
	return nil, []string{"-c:v", "libx264", "-preset", or(p, "veryfast"), "-crf", "18"}
}

// audioEncoders re-encode audio in the codec it came in (for the frame-accurate fallback,
// where copied audio wouldn't start where the video does).
var audioEncoders = map[string]string{"aac": "aac", "ac3": "ac3", "eac3": "eac3", "mp2": "mp2", "mp3": "libmp3lame",
	"opus": "libopus", "flac": "flac", "vorbis": "libvorbis"}

// cutReencode re-encodes each kept stretch (frame-accurate) and joins them.
func (s *Service) cutReencode(ctx context.Context, src, out, work string, pi probeInfo, plan Plan) error {
	encoders := []string{"software"}
	if s.Encoders != nil {
		if e := s.Encoders(); len(e) > 0 {
			encoders = e
		}
	}
	ext := filepath.Ext(out)
	var err error
	for _, encoder := range encoders {
		err = func() error {
			var b strings.Builder
			for i, k := range plan.Keep {
				seg := filepath.Join(work, fmt.Sprintf("seg-%03d%s", i, ext))
				pre, enc := s.encoderArgs(encoder, pi)
				args := append([]string{"-hide_banner", "-v", "error", "-nostdin", "-y"}, pre...)
				args = append(args, "-ss", secs(k.StartMS), "-i", src, "-t", secs(k.EndMS-k.StartMS),
					"-map", "0:v:0", "-map", "0:a?", "-map", "0:s?", "-c", "copy")
				args = append(args, enc...)
				for i, c := range pi.AudioCodecs {
					args = append(args, fmt.Sprintf("-c:a:%d", i), or(audioEncoders[c], "aac"))
				}
				if pi.PixFmt != "" && encoder == "software" && !strings.Contains(pi.PixFmt, "10") {
					args = append(args, "-pix_fmt", "yuv420p")
				}
				args = append(args, "-avoid_negative_ts", "make_zero", "-max_muxing_queue_size", "4096", seg)
				if err := s.run(ctx, s.ffmpeg(), args...); err != nil {
					return err
				}
				fmt.Fprintf(&b, "file '%s'\n", strings.ReplaceAll(seg, "'", `'\''`))
			}
			return s.join(ctx, work, b.String(), out)
		}()
		if err == nil || ctx.Err() != nil {
			return err
		}
	}
	return err
}

// verify checks a cut file before it replaces the recording: FFprobe reads it, it has the
// same streams, its length is the kept time (within 2%), and a few seconds either side of
// every cut decode without errors.
func (s *Service) verify(ctx context.Context, path string, orig probeInfo, plan Plan) error {
	pi, err := s.probe(ctx, path)
	if err != nil {
		return err
	}
	if pi.Video != orig.Video || pi.Audio != orig.Audio || pi.Subs != orig.Subs {
		return fmt.Errorf("it has %d video, %d audio and %d subtitle streams; the recording has %d, %d and %d",
			pi.Video, pi.Audio, pi.Subs, orig.Video, orig.Audio, orig.Subs)
	}
	want := plan.KeptMS()
	if d := pi.DurationMS - want; pi.DurationMS <= 0 || d > want/50 || -d > want/50 {
		return fmt.Errorf("it is %s long; expected %s", clock(pi.DurationMS), clock(want))
	}
	at := int64(0)
	for i, k := range plan.Keep {
		if i > 0 {
			if err := s.decodeCheck(ctx, path, max(0, at-3000)); err != nil {
				return fmt.Errorf("the cut at %s doesn't play cleanly: %v", clock(at), err)
			}
		}
		at += k.EndMS - k.StartMS
	}
	return nil
}

// decodeCheck decodes 6 s of every stream from at, failing on any error.
func (s *Service) decodeCheck(ctx context.Context, path string, atMS int64) error {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, s.ffmpeg(), "-hide_banner", "-v", "error", "-nostdin", "-ss", secs(atMS), "-i", path,
		"-t", "6", "-map", "0:v:0", "-map", "0:a?", "-f", "null", "-")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return errors.New(strings.TrimPrefix(tail(stderr.String()+" "+err.Error()), ": "))
	}
	if msg := strings.TrimSpace(stderr.String()); msg != "" {
		return errors.New(strings.TrimPrefix(tail(msg), ": "))
	}
	return nil
}

// clock formats ms as h:mm:ss or m:ss.
func clock(ms int64) string {
	t := ms / 1000
	if t >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", t/3600, t/60%60, t%60)
	}
	return fmt.Sprintf("%d:%02d", t/60, t%60)
}
