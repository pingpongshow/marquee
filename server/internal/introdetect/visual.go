package introdetect

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
)

const (
	// blackAmount is the share of near-black pixels (percent) that makes a frame "credits":
	// white text on black is 90–100%.
	blackAmount = 80
	// creditsGap is how many non-black seconds a credits roll may contain (title cards,
	// logos) and still count as one.
	creditsGap = 4
	minCredits = 20
)

var blackLine = regexp.MustCompile(`frame:(\d+) pblack:(\d+)`)

// parseBlack reads FFmpeg blackframe output (one frame per second) into the seconds that
// were mostly black.
func parseBlack(out []byte) map[int]bool {
	black := map[int]bool{}
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		if m := blackLine.FindSubmatch(sc.Bytes()); m != nil {
			n, _ := strconv.Atoi(string(m[1]))
			p, _ := strconv.Atoi(string(m[2]))
			if p >= blackAmount {
				black[n] = true
			}
		}
	}
	return black
}

// creditsRun picks the credits from per-second blackness over a window of length seconds:
// the earliest long dark run that reaches (nearly) the end, or failing that the longest
// run of at least 40 s. Times are seconds from the window's start.
func creditsRun(black map[int]bool, length int) (Range, bool) {
	type run struct{ start, end int }
	var runs []run
	cur := run{-1, -1}
	for s := 0; s < length; s++ {
		if !black[s] {
			continue
		}
		if cur.start < 0 || s-cur.end > creditsGap+1 {
			if cur.start >= 0 {
				runs = append(runs, cur)
			}
			cur = run{s, s}
		}
		cur.end = s
	}
	if cur.start >= 0 {
		runs = append(runs, cur)
	}
	for _, r := range runs {
		if r.end-r.start+1 >= minCredits && length-1-r.end <= int(endTolerance) {
			return Range{float64(r.start), float64(length)}, true
		}
	}
	var best run
	for _, r := range runs {
		if r.end-r.start > best.end-best.start {
			best = r
		}
	}
	if best.end-best.start+1 >= 40 {
		return Range{float64(best.start), float64(best.end + 1)}, true
	}
	return Range{}, false
}

// visualCredits finds credits in the last length seconds of a file by looking for the
// mostly-black frames of a credits roll.
func (s *Service) visualCredits(ctx context.Context, path string, start, length float64, cuda bool) (*Range, error) {
	args := []string{"-hide_banner", "-ss", fmt.Sprintf("%.3f", start), "-t", fmt.Sprintf("%.3f", length)}
	if cuda {
		args = append(args, "-hwaccel", "cuda")
	}
	args = append(args, "-i", path, "-an", "-sn", "-dn", "-map", "0:v:0",
		"-vf", fmt.Sprintf("fps=1,scale=320:-2,blackframe=amount=%d:threshold=32", blackAmount), "-f", "null", "-")
	cmd := exec.CommandContext(ctx, s.FFmpeg, args...)
	if nice, err := exec.LookPath("nice"); err == nil {
		cmd = exec.CommandContext(ctx, nice, append([]string{"-n", "15", s.FFmpeg}, args...)...)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if cuda && ctx.Err() == nil {
			return s.visualCredits(ctx, path, start, length, false)
		}
		return nil, fmt.Errorf("blackframe: %w", err)
	}
	r, ok := creditsRun(parseBlack(stderr.Bytes()), int(length))
	if !ok {
		return nil, nil
	}
	r.Start += start
	r.End += start
	return &r, nil
}
