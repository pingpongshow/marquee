package playback

import (
	"fmt"
	"path/filepath"
	"time"

	"marquee/internal/settings"
)

// Adaptive bitrate for remote transcodes (PLAY-15). The session's own transcode is the
// top rung; lower rungs come from the remote quality ladder. Every rung uses the same
// fixed 6 s plan with forced keyframes, so players can switch between them at any segment.
// A rung's FFmpeg only runs while that rung is being fetched; idle rungs are stopped.

// Rung is one quality variant of a session.
type Rung struct {
	Height    int
	VideoKbps int
	lastUsed  time.Time
	t         *Transcoder
}

const (
	maxExtraRungs = 2
	rungIdle      = 90 * time.Second
)

// ladderFor picks up to two lower rungs below the top one, roughly halving the bitrate each step.
func ladderFor(top Decision, sourceHeight int, ladder []settings.QualityRung) []Rung {
	var out []Rung
	last := top.VideoKbps
	for _, q := range ladder {
		if len(out) == maxExtraRungs {
			break
		}
		if q.VideoKbps <= 0 || q.VideoKbps > last*6/10 {
			continue // too close to the rung above to be worth switching to
		}
		h := q.MaxHeight
		if h <= 0 || h > top.Height {
			h = top.Height
		}
		if sourceHeight > 0 && h > sourceHeight {
			h = sourceHeight
		}
		out = append(out, Rung{Height: h, VideoKbps: q.VideoKbps})
		last = q.VideoKbps
	}
	return out
}

// Rungs lists the session's variants after the top one (none unless it's a remote transcode).
func (s *Session) Rungs() []Rung {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Rung(nil), s.rungs...)
}

// RungTranscoder returns the transcoder for variant n (0 = the session's own), starting
// one for a lower rung on first use.
func (m *Manager) RungTranscoder(s *Session, n int) (*Transcoder, error) {
	if n == 0 {
		return s.transcoder, nil
	}
	enc := m.encodersFor(m.Settings.Get().Transcoder.EncoderOrder) // before locking s: it reads every session
	s.mu.Lock()
	defer s.mu.Unlock()
	if n < 1 || n > len(s.rungs) || s.transcoder == nil {
		return nil, fmt.Errorf("no variant %d", n)
	}
	r := &s.rungs[n-1]
	r.lastUsed = time.Now()
	if r.t == nil {
		base := s.transcoder
		job := base.Job
		job.Decision.Height = r.Height
		job.Decision.VideoKbps = r.VideoKbps
		job.Dir = filepath.Join(base.Job.Dir, fmt.Sprintf("r%d", n))
		r.t = &Transcoder{FFmpeg: base.FFmpeg, Job: job, Encoders: enc,
			TotalSegments: base.TotalSegments, ThrottleAhead: base.ThrottleAhead}
		r.t.lastRequest = base.lastRequestSnapshot()
	}
	return r.t, nil
}

// reapRungs stops lower rungs nobody has fetched for a while (the player moved on).
func (s *Session) reapRungs(now time.Time) {
	s.mu.Lock()
	var idle []*Transcoder
	for i := range s.rungs {
		r := &s.rungs[i]
		if r.t != nil && now.Sub(r.lastUsed) > rungIdle {
			idle = append(idle, r.t)
			r.t = nil
		}
	}
	s.mu.Unlock()
	for _, t := range idle {
		t.Stop()
	}
}

func (s *Session) stopRungs() {
	s.mu.Lock()
	var all []*Transcoder
	for i := range s.rungs {
		if s.rungs[i].t != nil {
			all = append(all, s.rungs[i].t)
			s.rungs[i].t = nil
		}
	}
	s.mu.Unlock()
	for _, t := range all {
		t.Stop()
	}
}

func (t *Transcoder) lastRequestSnapshot() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.lastRequest
}

// ---------- encoder capacity (PLAY-16) ----------

// nvencJobs counts running NVENC encodes across sessions and their rungs.
func (m *Manager) nvencJobs() int {
	n := 0
	for _, s := range m.List() {
		var ts []*Transcoder
		if s.transcoder != nil {
			ts = append(ts, s.transcoder)
		}
		s.mu.Lock()
		for _, r := range s.rungs {
			if r.t != nil {
				ts = append(ts, r.t)
			}
		}
		s.mu.Unlock()
		for _, t := range ts {
			if !t.Job.Decision.VideoCopy && t.Encoder() == "nvenc" && t.running() {
				n++
			}
		}
	}
	return n
}

// encodersFor is the configured encoder order, skipping NVENC while its concurrent
// session limit is reached (consumer NVIDIA drivers allow a fixed number of encodes);
// new jobs then go to Quick Sync and finally the CPU instead of failing.
// EncoderChoices is the encoders to try for a new transcode, best first.
func (m *Manager) EncoderChoices() []string {
	return m.encodersFor(m.Settings.Get().Transcoder.EncoderOrder)
}

func (m *Manager) encodersFor(order []string) []string {
	avail := m.Encoders.Available(order)
	limit := m.Settings.Get().Transcoder.NVENCSessions
	if limit <= 0 || m.nvencJobs() < limit {
		return avail
	}
	out := avail[:0:0]
	for _, e := range avail {
		if e != "nvenc" {
			out = append(out, e)
		}
	}
	return out
}
