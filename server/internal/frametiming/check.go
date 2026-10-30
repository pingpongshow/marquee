// Package frametiming finds and repairs video files whose frames are timed in decode order
// instead of display order (Library Health → Frame timing errors). Modern video stores some
// frames ahead of the order they're shown in (B-frames), so each frame needs its own display
// time; some MP4 muxers left that table out, giving every frame its decode time. Players that
// take the file as it is (direct play, or Marquee repackaging it for HLS) then show many frames
// out of order: the picture stutters while the sound is fine. Converting it while playing
// hides it (the decoder puts the frames back in order), so flagged files are converted until
// they're repaired. A repair works out the real order from the video itself and writes the
// missing table: nothing is re-encoded.
package frametiming

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"marquee/internal/encode"
)

// Repairable reports whether a file in this container can be repaired (Rewrite handles
// MP4 and QuickTime files).
func Repairable(container, path string) bool {
	switch strings.ToLower(container) {
	case "mp4", "mov", "m4v", "mov,mp4,m4a,3gp,3g2,mj2":
		return true
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".mp4", ".m4v", ".mov":
		return true
	}
	return false
}

// Checked reports whether a file in this container is checked at all: AVI has no display
// times to get wrong (players work them out), so its frames always come out of the decoder
// "out of order".
func Checked(container, path string) bool {
	return !strings.EqualFold(container, "avi") && !strings.EqualFold(filepath.Ext(path), ".avi")
}

// checkSeconds is how much of the start Check decodes.
const checkSeconds = 8

// Check reports what's wrong with the file's frame timing ("" = nothing): the frames of its
// first seconds, decoded and so in display order, must carry rising times. It runs FFprobe
// at low priority (lookPath finds nice and ionice; nil = exec.LookPath) and returns an error
// when the file couldn't be read.
func Check(ctx context.Context, lookPath func(string) (string, error), ffprobe, path string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	name, args := encode.LowPriority(lookPath, or(ffprobe, "ffprobe"), "-v", "error", "-threads", "0",
		"-select_streams", "v:0", "-read_intervals", "%+"+strconv.Itoa(checkSeconds),
		"-show_entries", "frame=pts", "-of", "csv=p=0", path)
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stderr = &encode.LimitWriter{W: &stderr, N: 4096}
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", errors.New(encode.LastLine(stderr.String(), err))
	}
	return Verdict(OutOfOrder(times(out)))
}

// Verdict is Check's finding for back of n frames timed before the frame shown ahead of
// them. A few can be a glitch; a tenth or more is the decode-order table, which puts close
// to half the frames out of order.
func Verdict(back, n int) (string, error) {
	if n < 24 || back*10 < n {
		return "", nil
	}
	return fmt.Sprintf("Frames are timed in decode order (%d of the first %d out of order)", back, n), nil
}

// OutOfOrder counts the times (in display order) earlier than the one before them, of n.
func OutOfOrder(ts []int64) (back, n int) {
	for i := 1; i < len(ts); i++ {
		if ts[i] < ts[i-1] {
			back++
		}
	}
	return back, len(ts)
}

// times reads FFprobe's csv of one time per line, skipping frames without one.
func times(out []byte) []int64 {
	var ts []int64
	for _, line := range strings.Split(string(out), "\n") {
		if v, err := strconv.ParseInt(strings.TrimSpace(strings.TrimSuffix(line, ",")), 10, 64); err == nil {
			ts = append(ts, v)
		}
	}
	return ts
}

// Order works out the order the video track's frames are shown in: order[k] is the
// decode-order index (the sample's place in the file) of the k-th frame shown. It decodes
// the whole track at low priority, with the edit list ignored so every sample comes out;
// progress hears the fraction done.
func Order(ctx context.Context, lookPath func(string) (string, error), ffprobe, path string, progress func(float64)) ([]int, error) {
	ffprobe = or(ffprobe, "ffprobe")
	// The samples in decode order, by time (which, decode order being all they carry, are
	// unique and rising).
	name, args := encode.LowPriority(lookPath, ffprobe, "-v", "error", "-ignore_editlist", "1",
		"-select_streams", "v:0", "-show_entries", "packet=pts", "-of", "csv=p=0", path)
	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stderr = &encode.LimitWriter{W: &stderr, N: 4096}
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New(encode.LastLine(stderr.String(), err))
	}
	packets := times(out)
	index := make(map[int64]int, len(packets))
	for i, t := range packets {
		if _, dup := index[t]; dup {
			return nil, errors.New("two frames have the same time, so their order can't be worked out")
		}
		index[t] = i
	}
	if len(packets) == 0 {
		return nil, errors.New("no video frames")
	}

	// The frames as the decoder puts them out: in display order, each with its sample's time.
	// showall keeps damaged frames, which still have a place; skipping the loop filter only
	// spares work.
	name, args = encode.LowPriority(lookPath, ffprobe, "-v", "error", "-ignore_editlist", "1", "-threads", "0",
		"-skip_loop_filter", "all", "-flags2", "+showall",
		"-select_streams", "v:0", "-show_entries", "frame=pts", "-of", "csv=p=0", path)
	stderr.Reset()
	cmd = exec.CommandContext(ctx, name, args...)
	cmd.Stderr = &encode.LimitWriter{W: &stderr, N: 4096}
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	order := make([]int, 0, len(packets))
	seen := make([]bool, len(packets))
	var bad error
	sc := bufio.NewScanner(pipe)
	for sc.Scan() {
		v, err := strconv.ParseInt(strings.TrimSpace(strings.TrimSuffix(sc.Text(), ",")), 10, 64)
		if err != nil {
			continue
		}
		i, ok := index[v]
		if bad == nil && (!ok || seen[i]) {
			bad = errors.New("the decoded frames don't match the file's samples")
		}
		if ok && !seen[i] {
			seen[i] = true
			order = append(order, i)
		}
		if progress != nil && len(order)%2000 == 0 {
			progress(float64(len(order)) / float64(len(packets)))
		}
	}
	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New(encode.LastLine(stderr.String(), err))
	}
	if bad != nil {
		return nil, bad
	}
	if len(order) != len(packets) {
		return nil, fmt.Errorf("%d of %d frames decoded, so their order can't be worked out", len(order), len(packets))
	}
	return order, nil
}

func or(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
