package playback

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strconv"
	"strings"
)

// OfflineQuality is a preset for converting a video to download (offline playback on
// phones and tablets). HEVC keeps files about half the size of H.264 at the same quality.
type OfflineQuality struct {
	Label     string
	Height    int
	VideoKbps int
}

var OfflineQualities = map[string]OfflineQuality{
	"high":   {"1080p", 1080, 4500},
	"medium": {"720p", 720, 2200},
	"low":    {"480p", 480, 900},
}

// ErrNotVideo is returned when converting something that isn't a video.
var ErrNotVideo = errors.New("only videos can be converted")

// OfflineJob prepares an FFmpeg job that converts an item to a fast-start MP4 at out:
// HEVC at the preset's size (never upscaled), tone-mapped to SDR when the source is HDR,
// with the user's preferred audio track as stereo AAC and an image subtitle burned in when
// the user's preferences pick one. It returns the job and the item's duration.
func (m *Manager) OfflineJob(ctx context.Context, r Request, quality, out string) (Job, int64, error) {
	q, ok := OfflineQualities[quality]
	if !ok {
		return Job{}, 0, fmt.Errorf("unknown quality %q", quality)
	}
	s := &Session{UserID: r.UserID, ItemID: r.ItemID}
	if err := m.load(ctx, s, r); err != nil {
		return Job{}, 0, err
	}
	v := s.Media.Video
	if v == nil {
		return Job{}, 0, ErrNotVideo
	}
	cfg := m.Settings.Get()
	h := q.Height
	if v.Height > 0 && v.Height < h {
		h = v.Height
	}
	kbps := q.VideoKbps
	if src := s.Media.BitrateKbps; src > 0 && src < kbps {
		kbps = max(src, 300) // never bigger than the original
	}
	d := Decision{Method: Transcode, VideoCodec: "hevc", Height: h, VideoKbps: kbps, AudioCodec: "aac", AudioChannels: 2,
		ToneMap: v.HDR != "" && cfg.Transcoder.ToneMapping}
	job := Job{Input: s.Path, Decision: d, VideoIndex: v.Index, AudioIndex: -1, SubIndex: -1, SubRelIndex: -1, VideoCodec: v.Codec,
		Preset: cfg.Transcoder.Preset, QSVDevice: m.Encoders.QSVDevice, OutputFile: out}
	if a := s.Media.Audio; a != nil {
		job.AudioIndex = a.Index
	}
	if sub := s.Media.Subtitle; sub != nil && sub.IsImage() {
		job.Decision.BurnSubtitle = true
		job.SubIndex, job.SubExternal, job.SubImage = sub.Index, sub.External, true
		job.SubRelIndex = m.subtitleRelIndex(ctx, s.FileID, sub.Index)
	}
	job.Encoder = m.encodersFor(cfg.Transcoder.EncoderOrder)[0]
	return job, s.Media.DurationMS, nil
}

// RunOffline runs a conversion, reporting progress (0–1). If the hardware encoder fails it
// tries the CPU once.
func (m *Manager) RunOffline(ctx context.Context, job Job, durationMS int64, progress func(float64)) error {
	err := m.runOffline(ctx, job, durationMS, progress)
	if err != nil && ctx.Err() == nil && job.Encoder != "software" {
		job.Encoder = "software"
		err = m.runOffline(ctx, job, durationMS, progress)
	}
	return err
}

func (m *Manager) runOffline(ctx context.Context, job Job, durationMS int64, progress func(float64)) error {
	cmd := exec.CommandContext(ctx, m.FFmpeg, job.Args()...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr strings.Builder
	cmd.Stderr = &limitWriter{w: &stderr, n: 8192}
	if err := cmd.Start(); err != nil {
		return err
	}
	sc := bufio.NewScanner(stdout)
	for sc.Scan() {
		// -progress prints key=value lines; out_time_us is how far it has got.
		if v, ok := strings.CutPrefix(sc.Text(), "out_time_us="); ok && durationMS > 0 {
			if us, err := strconv.ParseInt(v, 10, 64); err == nil {
				progress(min(1, float64(us)/1000/float64(durationMS)))
			}
		}
	}
	if err := cmd.Wait(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if i := strings.LastIndex(msg, "\n"); i >= 0 {
			msg = msg[i+1:]
		}
		return fmt.Errorf("convert (%s): %w: %s", job.Encoder, err, msg)
	}
	return nil
}

type limitWriter struct {
	w io.Writer
	n int
}

func (l *limitWriter) Write(p []byte) (int, error) {
	if l.n > 0 {
		k := min(len(p), l.n)
		l.w.Write(p[:k])
		l.n -= k
	}
	return len(p), nil
}
