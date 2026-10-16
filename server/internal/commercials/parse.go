// Package commercials finds commercial breaks in Live TV recordings with Comskip, run as a
// separate program, so players can skip them. Recordings are never cut: only the break
// times are stored, per media file.
package commercials

import (
	"bufio"
	"errors"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
)

// Break is one commercial break, in milliseconds from the start of the file.
type Break struct {
	StartMS, EndMS int64
}

const (
	// mergeGap joins breaks closer together than this (a few seconds of show between two
	// detected breaks is usually a bumper, not the programme).
	mergeGap = 10_000
	// minBreak drops breaks shorter than this.
	minBreak = 15_000
	// edge keeps the first and last seconds of a recording unmarked, so playback never
	// starts or ends with a skip.
	edge = 5_000
)

// ParseEDL reads a MPlayer/Kodi edit decision list: "start end type" per line, in seconds.
// Cut (0) and commercial break (3) entries are breaks; mute (1) and scene markers (2) aren't.
// Malformed lines are skipped.
func ParseEDL(r io.Reader) ([]Break, error) {
	var out []Break
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 2 {
			continue
		}
		start, err1 := strconv.ParseFloat(f[0], 64)
		end, err2 := strconv.ParseFloat(f[1], 64)
		if err1 != nil || err2 != nil || !finite(start) || !finite(end) {
			continue
		}
		if len(f) >= 3 {
			if t, err := strconv.Atoi(f[2]); err == nil && t != 0 && t != 3 {
				continue
			}
		}
		out = append(out, Break{StartMS: secMS(start), EndMS: secMS(end)})
	}
	return out, sc.Err()
}

// ErrNoHeader is returned for a Comskip .txt without its "FILE PROCESSING COMPLETE" line.
var ErrNoHeader = errors.New("not a Comskip cut list")

// ParseTXT reads Comskip's default output: a header giving the frame rate
// ("FILE PROCESSING COMPLETE 53999 FRAMES AT 2997", i.e. fps × 100), a dashed line, then
// "startFrame endFrame" per break.
func ParseTXT(r io.Reader) ([]Break, error) {
	sc := bufio.NewScanner(r)
	fps := 0.0
	var out []Break
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if fps == 0 {
			if i := strings.Index(line, "FRAMES AT"); i >= 0 && strings.Contains(line, "FILE PROCESSING COMPLETE") {
				v, err := strconv.ParseFloat(strings.TrimSpace(line[i+len("FRAMES AT"):]), 64)
				if err != nil || v <= 0 {
					return nil, ErrNoHeader
				}
				fps = v / 100
			}
			continue
		}
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		a, err1 := strconv.ParseFloat(f[0], 64)
		b, err2 := strconv.ParseFloat(f[1], 64)
		if err1 != nil || err2 != nil || !finite(a) || !finite(b) {
			continue
		}
		out = append(out, Break{StartMS: secMS(a / fps), EndMS: secMS(b / fps)})
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	if fps == 0 {
		return nil, ErrNoHeader
	}
	return out, nil
}

// Normalize tidies detected breaks for a file durationMS long (0 = unknown): breaks are
// kept inside the file and out of its first and last 5 s, sorted, those less than 10 s
// apart are merged, and any shorter than 15 s are dropped. A break covering more than half
// the recording means detection went wrong (e.g. a programme too short for Comskip to tell
// show from adverts), so it's dropped rather than skipping the programme.
func Normalize(in []Break, durationMS int64) []Break {
	var bs []Break
	for _, b := range in {
		b.StartMS = max(b.StartMS, edge)
		if durationMS > 0 {
			b.EndMS = min(b.EndMS, durationMS-edge)
		}
		if b.EndMS > b.StartMS {
			bs = append(bs, b)
		}
	}
	sort.Slice(bs, func(i, j int) bool { return bs[i].StartMS < bs[j].StartMS })
	var merged []Break
	for _, b := range bs {
		if n := len(merged); n > 0 && b.StartMS-merged[n-1].EndMS < mergeGap {
			merged[n-1].EndMS = max(merged[n-1].EndMS, b.EndMS)
			continue
		}
		merged = append(merged, b)
	}
	out := merged[:0]
	for _, b := range merged {
		if l := b.EndMS - b.StartMS; l >= minBreak && (durationMS <= 0 || l <= durationMS/2) {
			out = append(out, b)
		}
	}
	return out
}

func secMS(s float64) int64 { return int64(math.Round(s * 1000)) }

func finite(f float64) bool { return !math.IsNaN(f) && !math.IsInf(f, 0) && f >= 0 }
