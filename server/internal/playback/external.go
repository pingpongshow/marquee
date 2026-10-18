package playback

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"marquee/internal/probe"
)

// External sessions play a remote file that isn't a library item: a film on demand from
// an Xtream Codes server (LIVE-6). The file is read over HTTP (Range requests, so seeking
// restarts FFmpeg with -ss at the new place) and made into the same fMP4 HLS as library
// transcodes: video copied when the device plays it (cut on the file's own keyframes, read
// from its index), else transcoded with the configured encoders; audio copied or converted
// to AAC. Sessions count against the transcode limit and end like any other.

// ExternalIdle is how long an idle external session (a film left paused) is kept, without
// FFmpeg, before it ends.
const ExternalIdle = 3 * time.Hour

// ExternalRequest starts an external session.
type ExternalRequest struct {
	UserID, DeviceID     int64
	UserName, DeviceName string
	Title                string
	URL, UserAgent       string
	StartMS              int64
	// Profile is what the device plays; nil copies only H.264 video and AAC/MP3 audio.
	Profile  *DeviceProfile
	Remote   bool
	ClientIP string
	Quality  QualityInputs
	// Redact cleans messages that may name URL (it may hold a password).
	Redact func(string) string
}

// DefaultExternalProfile is assumed for a device that doesn't say what it plays: every
// client plays H.264 with AAC or MP3 in HLS.
func DefaultExternalProfile() DeviceProfile {
	return DeviceProfile{VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac", "mp3"}, HLS: true,
		HLSVideoCodecs: []string{"h264"}, HLSAudioCodecs: []string{"aac", "mp3"}}
}

// ExternalInputArgs are FFmpeg's options for reading a remote file: reconnect on drops
// (also after a pause, when the server may have closed the connection).
func ExternalInputArgs(userAgent string) []string {
	a := []string{"-reconnect", "1", "-reconnect_on_network_error", "1", "-reconnect_delay_max", "10", "-rw_timeout", "30000000"}
	if userAgent != "" {
		a = append(a, "-user_agent", userAgent)
	}
	return a
}

// ProbeRemote reads a remote file's streams and length with ffprobe.
func ProbeRemote(ctx context.Context, ffprobe, url, userAgent string, redact func(string) string) (*probe.Result, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	args := []string{"-v", "error", "-rw_timeout", "20000000"}
	if userAgent != "" {
		args = append(args, "-user_agent", userAgent)
	}
	args = append(args, "-print_format", "json", "-show_format", "-show_streams", "--", url)
	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, ffprobe, args...)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if i := strings.LastIndex(msg, "\n"); i >= 0 {
			msg = msg[i+1:]
		}
		if msg == "" {
			msg = err.Error()
		}
		if redact != nil {
			msg = redact(msg)
		}
		return nil, fmt.Errorf("%w: %s", ErrUnavailable, msg)
	}
	return probe.Parse(stdout.Bytes())
}

// StartExternal probes a remote file and prepares its HLS session. FFmpeg starts with the
// first request for the playlist's media.
func (m *Manager) StartExternal(ctx context.Context, r ExternalRequest) (*Session, error) {
	cfg := m.Settings.Get()
	if m.transcodeCount() >= cfg.Transcoder.MaxConcurrentTranscodes {
		return nil, ErrBusy
	}
	ffprobe := m.RemoteFFprobe
	if ffprobe == "" {
		ffprobe = m.FFprobe
	}
	res, err := ProbeRemote(ctx, ffprobe, r.URL, r.UserAgent, r.Redact)
	if err != nil {
		return nil, err
	}
	s := &Session{ID: newID(), UserID: r.UserID, UserName: r.UserName, DeviceID: r.DeviceID, DeviceName: r.DeviceName,
		ItemType: "movie", Title: r.Title, Path: r.URL, Remote: r.Remote, ClientIP: r.ClientIP, External: true,
		StartedAt: time.Now(), lastActive: time.Now(), state: "playing", redact: r.Redact}
	s.Media = Media{Container: res.Container, BitrateKbps: res.BitrateKbps, DurationMS: res.DurationMS}
	if v := res.Video(); v != nil {
		s.Media.Video = &VideoStream{Index: v.Index, Codec: v.Codec, Tag: v.Tag, Width: v.Width, Height: v.Height,
			BitDepth: v.BitDepth, HDR: v.HDRFormat, BitrateKbps: v.BitrateKbps, FrameRate: v.FrameRate}
		if s.Media.Video.BitDepth == 0 {
			s.Media.Video.BitDepth = 8
		}
	}
	if a := res.Audio(); a != nil {
		s.Media.Audio = &AudioStream{Index: a.Index, Codec: a.Codec, Profile: a.Profile, Channels: a.Channels, BitrateKbps: a.BitrateKbps, Language: a.Language}
	}
	switch {
	case s.Media.Video == nil:
		return nil, fmt.Errorf("%w: the file has no video", ErrUnavailable)
	case s.Media.DurationMS <= 0:
		return nil, fmt.Errorf("%w: the file's length is unknown", ErrUnavailable)
	}
	s.StartMS = max(r.StartMS, 0)
	if s.StartMS >= s.Media.DurationMS {
		s.StartMS = 0
	}
	s.positionMS = s.StartMS

	p := DefaultExternalProfile()
	if r.Profile != nil {
		p = *r.Profile
	}
	p.Containers = nil // always HLS: the players expect a playlist
	p.HLS = true
	q := r.Quality
	q.Remote = r.Remote
	q.OtherRemoteKbps, q.OtherRemoteCount = m.remoteUsage("")
	s.LimitKbps, s.LimitReason = MaxKbps(q, cfg.RemoteAccess)
	limits := Limits{MaxKbps: s.LimitKbps, Remote: r.Remote, PreferHEVC: r.Remote && cfg.Transcoder.PreferHEVCRemote}
	s.Decision = Decide(s.Media, p, limits)
	var plan []float64
	if s.Decision.Method == DirectStream {
		kctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		kf, err := RemoteKeyframes(kctx, &http.Client{}, r.URL, r.UserAgent, s.Media.Container)
		cancel()
		if err == nil {
			plan = KeyframePlan(kf, float64(s.Media.DurationMS)/1000, SegmentSeconds)
		} else {
			msg := err.Error()
			if r.Redact != nil {
				msg = r.Redact(msg)
			}
			slog.Warn("no keyframe index in the remote file; transcoding video", "title", r.Title, "err", msg)
			limits.NoVideoCopy = true
			s.Decision = Decide(s.Media, p, limits)
		}
	}
	if plan == nil {
		plan = FixedPlan(s.Media.DurationMS)
	}
	s.Plan = plan
	if s.Decision.ToneMap && !cfg.Transcoder.ToneMapping {
		s.Decision.ToneMap = false
	}
	if s.Decision.Method == DirectPlay { // can't happen without containers; be safe
		return nil, errors.New("external files play as HLS")
	}
	job := Job{Input: r.URL, InputArgs: ExternalInputArgs(r.UserAgent), Decision: s.Decision, VideoIndex: s.Media.Video.Index,
		VideoCodec: s.Media.Video.Codec, AudioIndex: -1, SubIndex: -1, SubRelIndex: -1, Dir: filepath.Join(m.TranscodeDir, s.ID),
		Preset: cfg.Transcoder.Preset, QSVDevice: m.Encoders.QSVDevice, Plan: plan}
	if a := s.Media.Audio; a != nil {
		job.AudioIndex, job.AudioCodec = a.Index, a.Codec
	}
	s.transcoder = &Transcoder{FFmpeg: m.FFmpeg, Job: job, Encoders: m.encodersFor(cfg.Transcoder.EncoderOrder),
		TotalSegments: len(plan), ThrottleAhead: cfg.Transcoder.ThrottleSegmentsAhead, Redact: r.Redact,
		Playhead: s.playhead(plan)}
	if s.Decision.VideoCopy {
		s.transcoder.Encoders = []string{"software"} // copying video needs no encoder
	}
	s.transcoder.lastRequest = segmentFor(plan, float64(s.StartMS)/1000)
	if s.Remote && s.Decision.Method == Transcode {
		s.rungs = ladderFor(s.Decision, s.Media.Video.Height, cfg.Transcoder.RemoteLadder)
	}

	m.mu.Lock()
	m.init()
	m.sessions[s.ID] = s
	m.mu.Unlock()
	slog.Info("playback started", "user", s.UserName, "title", s.Title, "method", s.Decision.Method, "external", true,
		"remote", s.Remote, "limitKbps", s.LimitKbps, "reasons", strings.Join(s.Decision.Reasons, "; "))
	return s, nil
}
