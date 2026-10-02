package livetv

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"
)

// Idle is how long a live stream runs without anyone fetching it.
const Idle = 60 * time.Second

// SegmentSeconds is the live HLS segment length.
const SegmentSeconds = 4

var ErrUnavailable = errors.New("the channel isn't available right now")

// Sessions runs live streams: FFmpeg reads the channel and writes a sliding-window HLS
// playlist, copying H.264 when the device can play it and transcoding otherwise.
type Sessions struct {
	FFmpeg, FFprobe string
	Dir             string // <transcode dir>/live
	// Encoders returns the encoders to try, best first (nvenc, qsv, software).
	Encoders  func() []string
	QSVDevice string

	once sync.Once
	mu   sync.Mutex
	live map[string]*Session
}

// Session is one viewer's live stream.
type Session struct {
	ID         string
	ChannelID  int64
	UserID     int64
	Method     string // copy, transcode
	VideoCodec string
	AudioCodec string
	Encoder    string

	dir    string
	cancel context.CancelFunc
	done   chan struct{}
	mu     sync.Mutex
	last   time.Time
}

// Options describe the device and network for a stream.
type Options struct {
	VideoCodecs []string // codecs the device plays in HLS
	AudioCodecs []string
	MaxKbps     int  // 0 = no cap
	Remote      bool // away from home: transcode to a lighter stream
	UserAgent   string
}

type probe struct {
	Video, Audio string
	Height       int
	Interlaced   bool
}

func (m *Sessions) init() {
	m.once.Do(func() {
		m.live = map[string]*Session{}
		os.RemoveAll(m.Dir) // leftovers from a previous run
		go m.reap()
	})
}

func (m *Sessions) reap() {
	for range time.Tick(10 * time.Second) {
		m.mu.Lock()
		var idle []*Session
		for _, s := range m.live {
			s.mu.Lock()
			if time.Since(s.last) > Idle {
				idle = append(idle, s)
			}
			s.mu.Unlock()
		}
		m.mu.Unlock()
		for _, s := range idle {
			slog.Info("ending idle live stream", "channel", s.ChannelID)
			m.Stop(s.ID)
		}
	}
}

func (m *Sessions) probe(ctx context.Context, url, ua string) (probe, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	// A short look is enough for codecs and height, and keeps tuning quick.
	args := []string{"-v", "error", "-rw_timeout", "15000000", "-analyzeduration", "2000000", "-probesize", "2000000"}
	if ua != "" {
		args = append(args, "-user_agent", ua)
	}
	args = append(args, "-show_entries", "stream=codec_type,codec_name,height,field_order", "-of", "json", url)
	out, err := exec.CommandContext(ctx, m.FFprobe, args...).Output()
	if err != nil {
		return probe{}, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	var r struct {
		Streams []struct {
			Type       string `json:"codec_type"`
			Codec      string `json:"codec_name"`
			FieldOrder string `json:"field_order"`
			Height     int    `json:"height"`
		} `json:"streams"`
	}
	if json.Unmarshal(out, &r) != nil {
		return probe{}, ErrUnavailable
	}
	var p probe
	for _, s := range r.Streams {
		switch {
		case s.Type == "video" && p.Video == "":
			p.Video, p.Height = s.Codec, s.Height
			p.Interlaced = s.FieldOrder != "" && s.FieldOrder != "progressive" && s.FieldOrder != "unknown"
		case s.Type == "audio" && p.Audio == "":
			p.Audio = s.Codec
		}
	}
	if p.Video == "" && p.Audio == "" {
		return p, ErrUnavailable
	}
	return p, nil
}

func newID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// Start begins streaming a channel and returns once the first segments are ready.
func (m *Sessions) Start(ctx context.Context, channelID, userID int64, url string, o Options) (*Session, error) {
	m.init()
	p, err := m.probe(ctx, url, o.UserAgent)
	if err != nil {
		return nil, err
	}
	copyVideo := p.Video == "h264" && slices.Contains(o.VideoCodecs, "h264") && !p.Interlaced && o.MaxKbps == 0 && !o.Remote
	copyAudio := p.Audio == "aac" && slices.Contains(o.AudioCodecs, "aac")
	encoders := []string{"software"}
	if !copyVideo && m.Encoders != nil {
		encoders = m.Encoders()
	}
	var last error
	for _, enc := range encoders {
		s := &Session{ID: newID(), ChannelID: channelID, UserID: userID, Encoder: enc, last: time.Now(), done: make(chan struct{})}
		s.Method, s.VideoCodec, s.AudioCodec = "transcode", "h264", "aac"
		if copyVideo {
			s.Method = "copy"
		}
		if copyAudio {
			s.AudioCodec = p.Audio
		}
		s.dir = filepath.Join(m.Dir, s.ID)
		if err := os.MkdirAll(s.dir, 0o755); err != nil {
			return nil, err
		}
		args := m.args(url, s.dir, p, o, copyVideo, copyAudio, enc)
		runCtx, cancel := context.WithCancel(context.Background())
		s.cancel = cancel
		cmd := exec.CommandContext(runCtx, m.FFmpeg, args...)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Start(); err != nil {
			cancel()
			return nil, err
		}
		go func() {
			cmd.Wait()
			close(s.done)
		}()
		if err := waitReady(ctx, s); err != nil {
			cancel()
			<-s.done
			os.RemoveAll(s.dir)
			if ctx.Err() != nil {
				return nil, ctx.Err() // the viewer moved on (e.g. flicking through channels): not a failure
			}
			msg := strings.TrimSpace(stderr.String())
			if i := strings.LastIndex(msg, "\n"); i >= 0 {
				msg = msg[i+1:]
			}
			if msg == "" {
				msg = err.Error() // FFmpeg said nothing: say why we gave up (timed out, exited)
			}
			last = fmt.Errorf("%w (%s: %s)", ErrUnavailable, enc, msg)
			slog.Warn("live stream failed to start", "channel", channelID, "encoder", enc, "err", msg)
			if copyVideo {
				break // copying doesn't depend on an encoder
			}
			continue
		}
		m.mu.Lock()
		m.live[s.ID] = s
		m.mu.Unlock()
		slog.Info("live stream started", "channel", channelID, "method", s.Method, "encoder", enc, "video", p.Video, "audio", p.Audio)
		return s, nil
	}
	return nil, last
}

// waitReady waits for the playlist to list its first segment, or for FFmpeg to give up.
func waitReady(ctx context.Context, s *Session) error {
	ready := func() bool {
		b, err := os.ReadFile(filepath.Join(s.dir, "index.m3u8"))
		return err == nil && bytes.Contains(b, []byte("#EXTINF"))
	}
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if ready() {
			return nil
		}
		select {
		case <-s.done:
			if ready() { // a short source can finish before the first check
				return nil
			}
			return errors.New("ffmpeg exited")
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
	return errors.New("timed out")
}

func (m *Sessions) args(url, dir string, p probe, o Options, copyVideo, copyAudio bool, enc string) []string {
	a := []string{"-hide_banner", "-v", "error", "-nostdin", "-y", "-fflags", "+genpts+discardcorrupt"}
	if strings.HasPrefix(url, "http") {
		a = append(a, "-reconnect", "1", "-reconnect_streamed", "1", "-reconnect_delay_max", "5", "-rw_timeout", "15000000")
	}
	if o.UserAgent != "" {
		a = append(a, "-user_agent", o.UserAgent)
	}
	if enc == "qsv" && !copyVideo {
		a = append(a, "-init_hw_device", "qsv=qs:"+m.QSVDevice, "-filter_hw_device", "qs")
	}
	a = append(a, "-analyzeduration", "2000000", "-probesize", "2000000", "-i", url)
	if p.Video != "" {
		a = append(a, "-map", "0:v:0")
		if copyVideo {
			a = append(a, "-c:v", "copy")
		} else {
			// Away from home: 720p at 3 Mbps. At home: up to 1080p at 8 Mbps. A cap wins.
			h, kbps := 1080, 8000
			if o.Remote {
				h, kbps = 720, 3000
			}
			if o.MaxKbps > 0 && o.MaxKbps < kbps {
				kbps = max(o.MaxKbps-160, 500)
				if kbps < 2500 {
					h = 720
				}
				if kbps < 1200 {
					h = 480
				}
			}
			if p.Height > 0 && p.Height < h {
				h = p.Height
			}
			chain := []string{}
			if p.Interlaced {
				chain = append(chain, "yadif=mode=send_frame:deint=interlaced")
			}
			chain = append(chain, fmt.Sprintf("scale=-2:%d", h), "format=yuv420p")
			switch enc {
			case "nvenc":
				chain = append(chain, "hwupload_cuda")
			case "qsv":
				chain = append(chain, "hwupload=extra_hw_frames=64", "format=qsv")
			}
			a = append(a, "-vf", strings.Join(chain, ","))
			switch enc {
			case "nvenc":
				a = append(a, "-c:v", "h264_nvenc", "-preset", "p4", "-tune", "ll", "-rc", "vbr", "-forced-idr", "1")
			case "qsv":
				a = append(a, "-c:v", "h264_qsv", "-preset", "veryfast", "-look_ahead", "0", "-forced_idr", "1")
			default:
				a = append(a, "-c:v", "libx264", "-preset", "veryfast", "-tune", "zerolatency", "-profile:v", "high")
			}
			a = append(a, "-b:v", fmt.Sprintf("%dk", kbps), "-maxrate", fmt.Sprintf("%dk", kbps*3/2), "-bufsize", fmt.Sprintf("%dk", kbps*2),
				"-force_key_frames", "expr:gte(t,n_forced*2)") // every 2 s: quick starts, 4 s segments
		}
	}
	if p.Audio != "" {
		a = append(a, "-map", "0:a:0")
		if copyAudio {
			a = append(a, "-c:a", "copy")
		} else {
			a = append(a, "-c:a", "aac", "-ac", "2", "-b:a", "160k")
		}
	}
	return append(a, "-sn", "-dn", "-max_muxing_queue_size", "4096",
		// Short first segments so a channel starts within a couple of seconds.
		"-f", "hls", "-hls_init_time", "1", "-hls_time", fmt.Sprint(SegmentSeconds), "-hls_list_size", "8",
		"-hls_flags", "delete_segments+independent_segments+omit_endlist+temp_file",
		"-hls_segment_type", "mpegts", "-hls_segment_filename", filepath.Join(dir, "seg%06d.ts"),
		filepath.Join(dir, "index.m3u8"))
}

// Stop ends a stream.
func (m *Sessions) Stop(id string) bool {
	m.init()
	m.mu.Lock()
	s := m.live[id]
	delete(m.live, id)
	m.mu.Unlock()
	if s == nil {
		return false
	}
	s.cancel()
	<-s.done
	os.RemoveAll(s.dir)
	return true
}

// Get returns a running stream.
func (m *Sessions) Get(id string) *Session {
	m.init()
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.live[id]
}

// Count is the number of running streams.
func (m *Sessions) Count() int {
	m.init()
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.live)
}

var fileRe = regexp.MustCompile(`^(index\.m3u8|seg\d{6}\.ts)$`)

// Handler serves /api/v1/live/{session}/{file}; the session id is the credential, like
// video playback URLs (D38).
func (m *Sessions) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, "/api/v1/live/")
		id, file, ok := strings.Cut(rest, "/")
		if !ok || !fileRe.MatchString(file) {
			http.NotFound(w, r)
			return
		}
		s := m.Get(id)
		if s == nil {
			http.Error(w, "this stream has ended", http.StatusGone)
			return
		}
		s.mu.Lock()
		s.last = time.Now()
		s.mu.Unlock()
		path := filepath.Join(s.dir, file)
		if file == "index.m3u8" {
			w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
			w.Header().Set("Cache-Control", "no-cache")
		} else {
			w.Header().Set("Content-Type", "video/mp2t")
		}
		http.ServeFile(w, r, path)
	})
}
