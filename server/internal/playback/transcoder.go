package playback

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"syscall"
	"time"
)

var ErrSegmentTimeout = errors.New("transcoder didn't produce the segment in time")

// Transcoder runs FFmpeg for one session, restarting it on seeks, pausing it when it runs too
// far ahead of the viewer, and falling back to the next encoder if one fails.
type Transcoder struct {
	FFmpeg        string
	Job           Job      // template; StartSegment and Encoder are set per run
	Encoders      []string // fallback order, e.g. nvenc, qsv, software
	TotalSegments int
	ThrottleAhead int // segments allowed ahead of the last request before pausing

	mu          sync.Mutex
	encIdx      int
	cmd         *exec.Cmd
	exited      chan struct{}
	exitErr     error
	stderr      *tailBuffer
	startSeg    int
	produced    int // highest complete segment of the current run, startSeg-1 if none
	lastRequest int
	paused      bool
	stopped     bool
	stopOnce    sync.Once
	quit        chan struct{}
}

func (t *Transcoder) Encoder() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.Encoders[t.encIdx]
}

func (t *Transcoder) segPath(k int) string { return filepath.Join(t.Job.Dir, strconv.Itoa(k)+".m4s") }

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// start launches FFmpeg at segment k. Caller holds t.mu.
func (t *Transcoder) start(k int) error {
	t.killLocked()
	if err := os.MkdirAll(t.Job.Dir, 0o755); err != nil {
		return err
	}
	job := t.Job
	job.StartSegment = k
	job.Encoder = t.Encoders[t.encIdx]
	cmd := exec.Command(t.FFmpeg, job.Args()...)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	t.stderr = &tailBuffer{max: 4096}
	cmd.Stderr = t.stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	slog.Debug("transcode started", "dir", job.Dir, "segment", k, "encoder", job.Encoder)
	t.cmd, t.startSeg, t.produced, t.paused = cmd, k, k-1, false
	t.exited, t.exitErr = make(chan struct{}), nil
	exited := t.exited
	go func() {
		err := cmd.Wait()
		t.mu.Lock()
		if t.cmd == cmd {
			t.exitErr = err
		}
		t.mu.Unlock()
		close(exited)
	}()
	if t.quit == nil {
		t.quit = make(chan struct{})
		go t.throttleLoop()
	}
	return nil
}

func (t *Transcoder) killLocked() {
	if t.cmd == nil || t.cmd.Process == nil {
		return
	}
	pgid := -t.cmd.Process.Pid
	syscall.Kill(pgid, syscall.SIGCONT) // a paused process can't handle SIGKILL promptly otherwise
	syscall.Kill(pgid, syscall.SIGKILL)
	exited := t.exited
	t.cmd = nil
	t.mu.Unlock()
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
	}
	t.mu.Lock()
}

// refreshProduced advances the count of finished segments. With -hls_flags temp_file a
// segment file only appears once it is complete. Caller holds t.mu.
func (t *Transcoder) refreshProduced() {
	for exists(t.segPath(t.produced + 1)) {
		t.produced++
	}
}

// Init returns the fMP4 initialization segment, starting FFmpeg if needed.
func (t *Transcoder) Init(ctx context.Context) (string, error) {
	p := filepath.Join(t.Job.Dir, "init.mp4")
	t.mu.Lock()
	if t.cmd == nil && !exists(p) {
		if err := t.start(t.lastRequest); err != nil {
			t.mu.Unlock()
			return "", err
		}
	}
	start := t.startSeg
	t.mu.Unlock()
	// FFmpeg creates init.mp4 before filling it; it is complete once the first segment exists.
	if err := t.waitFor(ctx, t.segPath(start), start); err != nil {
		return "", err
	}
	return p, nil
}

// Segment returns the path of segment k once it exists.
func (t *Transcoder) Segment(ctx context.Context, k int) (string, error) {
	if k < 0 || k >= t.TotalSegments {
		return "", fmt.Errorf("segment %d out of range", k)
	}
	p := t.segPath(k)
	t.mu.Lock()
	if t.stopped {
		t.mu.Unlock()
		return "", errors.New("session stopped")
	}
	t.lastRequest = k
	if t.paused && t.cmd != nil {
		syscall.Kill(-t.cmd.Process.Pid, syscall.SIGCONT)
		t.paused = false
	}
	if exists(p) {
		t.mu.Unlock()
		return p, nil
	}
	t.refreshProduced()
	running := t.cmd != nil && t.exitErr == nil && !isClosed(t.exited)
	// Restart when the viewer jumped behind this run or well ahead of what it has produced.
	if !running || k < t.startSeg || k > t.produced+3 {
		if err := t.start(k); err != nil {
			t.mu.Unlock()
			return "", err
		}
	}
	t.mu.Unlock()
	return p, t.waitFor(ctx, p, k)
}

func isClosed(c chan struct{}) bool {
	if c == nil {
		return true
	}
	select {
	case <-c:
		return true
	default:
		return false
	}
}

// waitFor waits until p exists. If FFmpeg fails before producing anything, it retries with
// the next encoder.
func (t *Transcoder) waitFor(ctx context.Context, p string, k int) error {
	deadline := time.Now().Add(45 * time.Second)
	for {
		if exists(p) {
			return nil
		}
		t.mu.Lock()
		exited := isClosed(t.exited)
		failed := exited && t.exitErr != nil
		noOutput := t.produced < t.startSeg && !exists(t.segPath(t.startSeg))
		if failed && noOutput && t.encIdx+1 < len(t.Encoders) {
			slog.Warn("encoder failed; falling back", "encoder", t.Encoders[t.encIdx], "next", t.Encoders[t.encIdx+1],
				"err", t.exitErr, "ffmpeg", t.stderr.String())
			t.encIdx++
			os.RemoveAll(t.Job.Dir) // the new encoder's init segment won't match old segments
			start := k
			if start < 0 {
				start = t.lastRequest
			}
			if err := t.start(start); err != nil {
				t.mu.Unlock()
				return err
			}
			t.mu.Unlock()
			continue
		}
		if exited && !exists(p) {
			err := t.exitErr
			msg := t.stderr.String()
			t.mu.Unlock()
			if err == nil {
				return fmt.Errorf("transcoder finished without producing %s", filepath.Base(p))
			}
			return fmt.Errorf("transcoder failed: %v: %s", err, msg)
		}
		t.mu.Unlock()
		if time.Now().After(deadline) {
			return ErrSegmentTimeout
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// throttleLoop pauses FFmpeg when it is far ahead of the viewer and prunes old segments.
func (t *Transcoder) throttleLoop() {
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-t.quit:
			return
		case <-tick.C:
		}
		t.mu.Lock()
		if t.cmd != nil && !isClosed(t.exited) {
			t.refreshProduced()
			ahead := t.produced - t.lastRequest
			switch {
			case !t.paused && ahead > t.ThrottleAhead:
				syscall.Kill(-t.cmd.Process.Pid, syscall.SIGSTOP)
				t.paused = true
			case t.paused && ahead <= t.ThrottleAhead/2:
				syscall.Kill(-t.cmd.Process.Pid, syscall.SIGCONT)
				t.paused = false
			}
		}
		// Keep a minute of segments behind the viewer for small rewinds.
		for k := t.lastRequest - 10 - 20; k < t.lastRequest-10; k++ {
			if k >= 0 {
				os.Remove(t.segPath(k))
			}
		}
		t.mu.Unlock()
	}
}

// Stop kills FFmpeg and deletes the session's files.
func (t *Transcoder) Stop() {
	t.stopOnce.Do(func() {
		t.mu.Lock()
		t.stopped = true
		t.killLocked()
		if t.quit != nil {
			close(t.quit)
		}
		t.mu.Unlock()
		os.RemoveAll(t.Job.Dir)
	})
}

// Playlist renders the VOD media playlist: fixed-length segments covering the duration.
func Playlist(durationMS int64) []byte {
	var b bytes.Buffer
	b.WriteString("#EXTM3U\n#EXT-X-VERSION:7\n")
	fmt.Fprintf(&b, "#EXT-X-TARGETDURATION:%d\n", SegmentSeconds)
	b.WriteString("#EXT-X-PLAYLIST-TYPE:VOD\n#EXT-X-MEDIA-SEQUENCE:0\n#EXT-X-INDEPENDENT-SEGMENTS\n")
	b.WriteString("#EXT-X-MAP:URI=\"init.mp4\"\n")
	total := float64(durationMS) / 1000
	for i := 0; i < SegmentCount(durationMS); i++ {
		d := float64(SegmentSeconds)
		if rem := total - float64(i*SegmentSeconds); rem < d {
			d = rem
		}
		fmt.Fprintf(&b, "#EXTINF:%.3f,\n%d.m4s\n", d, i)
	}
	b.WriteString("#EXT-X-ENDLIST\n")
	return b.Bytes()
}

func SegmentCount(durationMS int64) int {
	n := int((durationMS + SegmentSeconds*1000 - 1) / (SegmentSeconds * 1000))
	return max(n, 1)
}

// tailBuffer keeps the last bytes written (FFmpeg's stderr for error messages).
type tailBuffer struct {
	mu  sync.Mutex
	buf []byte
	max int
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = append(b.buf, p...)
	if len(b.buf) > b.max {
		b.buf = b.buf[len(b.buf)-b.max:]
	}
	return len(p), nil
}

func (b *tailBuffer) String() string {
	if b == nil {
		return ""
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(bytes.TrimSpace(b.buf))
}
