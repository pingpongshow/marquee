package vodsave

import (
	"context"
	"slices"
	"strings"
	"time"

	"marquee/internal/settings"
)

// Connection budget. A provider allows only so many streams at once; Marquee knows the
// number from the Maximum streams of the Live TV source on the same host (Settings → Live
// TV), whose recordings and live streams hold connections, as do films and episodes
// played from the VOD source. Downloads use what is left: as many saves (and, for a large
// file, range segments of one save) as there are free connections, at most
// maxConnections. When a viewer starts, the newest connections stop at once (they keep
// their partial data); when viewing stops, the connections are taken back after the grace
// period. Without a known limit the queue runs ConcurrentPerSource saves and pauses while
// watching, as before.

// maxConnections is the most connections a source's downloads use at once, and the most
// segments one download is split into.
const maxConnections = 4

// LiveUse is a Live TV source's connection limit and what holds its connections now.
type LiveUse struct {
	Host string // the source's host, matched with a VOD source's
	Max  int    // Maximum streams (0 = unlimited)
	// InUse counts live streams, recordings and channels being tuned; Recordings is the
	// part recordings hold.
	InUse, Recordings int
}

// Budget is a VOD source's connections: the limit and what others use of it.
type Budget struct {
	Limit      int // 0 = no limit known
	Live       int // live streams and recordings on the same host
	Recordings int // of Live, recordings
	VOD        int // films and episodes playing from the source (or its host)
}

// Used is the connections held by viewers and recordings.
func (b Budget) Used() int { return b.Live + b.VOD }

// Viewers is the connections held by people watching (live streams and VOD playback).
func (b Budget) Viewers() int { return b.Used() - b.Recordings }

// Free is what's left for downloads (negative when over the limit; meaningless without one).
func (b Budget) Free() int { return b.Limit - b.Used() }

// allowed is how many connections downloads may hold: the free ones (only recordings
// counted when viewers don't come first), 0 to maxConnections.
func (b Budget) allowed(viewersFirst bool) int {
	free := b.Limit - b.Recordings
	if viewersFirst {
		free = b.Free()
	}
	return min(max(free, 0), maxConnections)
}

// ConnectionBudget is a VOD source's budget: the limit is the Maximum streams of the Live
// TV sources on its host (summed when several share it); the use counts their live
// streams and recordings and the films and episodes playing from the source or its host.
func ConnectionBudget(sourceID, host string, live []LiveUse, playing []Playing) Budget {
	var b Budget
	if host == "" {
		return b
	}
	for _, l := range live {
		if l.Max > 0 && strings.EqualFold(l.Host, host) {
			b.Limit += l.Max
			b.Live += l.InUse
			b.Recordings += l.Recordings
		}
	}
	if b.Limit == 0 {
		return Budget{}
	}
	for _, p := range playing {
		if p.Kind == "vod" && (p.SourceID == sourceID || strings.EqualFold(p.Host, host)) {
			b.VOD++
		}
	}
	return b
}

// Budget is a VOD source's connection budget now (Limit 0 when no limit is known).
func (s *Service) Budget(ctx context.Context, sourceID string) Budget {
	var playing []Playing
	if s.Activity != nil {
		playing = s.Activity(ctx)
	}
	return s.budget(sourceID, playing)
}

func (s *Service) budget(sourceID string, playing []Playing) Budget {
	if s.Connections == nil || s.VOD == nil {
		return Budget{}
	}
	return ConnectionBudget(sourceID, s.VOD.SourceHost(sourceID), s.Connections(), playing)
}

// budgeted reports whether the free connections decide a source's downloads (rather
// than ConcurrentPerSource and pausing while watching): a limit is known, and saves
// don't pause for any playback at all (scope "any").
func budgeted(c settings.VODSave, b Budget) bool {
	return b.Limit > 0 && !(c.PauseWhileWatching && c.PauseScope == "any")
}

// capState is how many connections a source's downloads may hold.
type capState struct {
	cap       int
	lastLower time.Time // the last time viewers or recordings held some of them
}

// connCap is the connections a source's downloads may hold now: raw (the free ones)
// straight away when it shrinks, but when it grows only once nothing has held the
// connections back for the grace period (channel flipping doesn't thrash). full is what
// they'd have with no viewers. s.mu is held.
func (s *Service) connCap(src string, raw, full int) int {
	st := s.caps[src]
	t := time.Now()
	if st == nil {
		st = &capState{cap: raw}
		if raw < full {
			st.lastLower = t
		}
		s.caps[src] = st
		return raw
	}
	switch {
	case raw < st.cap:
		st.cap, st.lastLower = raw, t
	case raw > st.cap:
		if t.Sub(st.lastLower) >= s.grace() {
			st.cap = raw
		}
	default:
		if raw < full {
			st.lastLower = t
		}
	}
	return st.cap
}

// sourceJobs are a source's running saves, oldest first. s.mu is held.
func (s *Service) sourceJobs(src string) []*job {
	var out []*job
	for _, j := range s.jobs {
		if j.sourceID == src {
			out = append(out, j)
		}
	}
	slices.SortFunc(out, func(a, b *job) int { return int(a.seq - b.seq) })
	return out
}

// shrink brings a source's downloads within cap connections: the newest saves' extra
// segments stop first, then the newest saves (which go back to the queue, keeping what
// they downloaded). It returns the connections still spare and how many saves run. s.mu
// is held.
func (s *Service) shrink(src string, cap int) (spare, running int) {
	jobs := s.sourceJobs(src)
	held := 0
	for _, j := range jobs {
		held += j.held()
	}
	excess := held - cap
	for i := len(jobs) - 1; i >= 0 && excess > 0; i-- {
		if w := jobs[i].held(); w > 1 {
			d := min(w-1, excess)
			jobs[i].setWant(w - d)
			excess -= d
		}
	}
	for i := len(jobs) - 1; i >= 0 && excess > 0; i-- {
		if jobs[i].held() > 0 {
			jobs[i].yield()
			excess--
		}
	}
	for _, j := range jobs {
		if !j.isYielding() {
			running++
		}
	}
	return max(cap-held, 0), running
}

// grow gives spare connections to the source's saves that can split, oldest first. s.mu
// is held.
func (s *Service) grow(src string, spare int) {
	for _, j := range s.sourceJobs(src) {
		for spare > 0 && j.splittable() {
			w := j.held()
			if w < 1 || w >= maxConnections {
				break
			}
			j.setWant(w + 1)
			spare--
		}
	}
}

// heldBy is the connections a source's downloads have open now. s.mu is held.
func (s *Service) heldBy(src string) int {
	n := 0
	for _, j := range s.sourceJobs(src) {
		n += j.open()
	}
	return n
}

// Yield makes the downloads give back connections for a stream about to start on host
// (extra connections not counted yet: a VOD playback that is starting) or one already
// counted (a live stream tuning or a recording starting: extra 0), then waits a moment
// for them to close. Downloads from sources without a known limit aren't affected (they
// pause while watching).
func (s *Service) Yield(ctx context.Context, host string, extra int) {
	if s.Connections == nil || s.VOD == nil {
		return
	}
	s.init()
	c := s.cfg()
	s.mu.Lock()
	srcs := map[string]bool{}
	for _, j := range s.jobs {
		srcs[j.sourceID] = true
	}
	s.mu.Unlock()
	if len(srcs) == 0 {
		return
	}
	var playing []Playing
	if c.PauseWhileWatching && s.Activity != nil {
		playing = s.Activity(ctx)
	}
	live := s.Connections()
	targets := map[string]int{}
	for src := range srcs {
		h := s.VOD.SourceHost(src)
		b := ConnectionBudget(src, h, live, playing)
		if !budgeted(c, b) {
			continue
		}
		if extra > 0 && host != "" && strings.EqualFold(h, host) {
			b.VOD += extra
		}
		full := b.allowed(false)
		raw := b.allowed(c.PauseWhileWatching)
		s.mu.Lock()
		cap := s.connCap(src, raw, full)
		s.shrink(src, cap)
		s.mu.Unlock()
		targets[src] = cap
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && ctx.Err() == nil {
		over := false
		s.mu.Lock()
		for src, cap := range targets {
			if s.heldBy(src) > cap {
				over = true
			}
		}
		s.mu.Unlock()
		if !over {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
}
