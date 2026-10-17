package vodsave

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"sync"
	"time"
)

// Segmented downloads. A large file (SegmentMin or more, its size known, from a server
// that answers Range requests) downloaded while its source has free connections is
// fetched over several at once: the .part file is made full size and each connection
// writes its range at its offset. Ranges split as connections are added (the busy range
// with the most left gives up its second half) and stop when connections are taken back
// (the newest first); a stopped range is picked up again later from where it got to. The
// ranges and how far each got are kept next to the .part file (<part>.segs), so a stopped
// or restarted save continues each one. The SHA-256 is computed once the file is
// complete. Any trouble with a range (no Range support, an error) falls back to one
// connection, continuing from the part of the file that is complete from its start.

func (s *Service) segmentMin() int64 {
	if s.SegmentMin > 0 {
		return s.SegmentMin
	}
	return 200 << 20
}

func (s *Service) splitMin() int64 {
	if s.SplitMin > 0 {
		return s.SplitMin
	}
	return 32 << 20
}

// errSegFallback asks for the download to go on over one connection.
var errSegFallback = errors.New("segmented download unavailable")

type segment struct {
	Start int64 `json:"start"`
	End   int64 `json:"end"`  // exclusive
	Done  int64 `json:"done"` // bytes written from Start

	busy     bool
	stopping bool
	seq      int
	cancel   context.CancelFunc
}

func (g *segment) next() int64 { return g.Start + g.Done }
func (g *segment) left() int64 { return g.End - g.next() }

// segFile is a segmented download's state, kept in <part>.segs.
type segFile struct {
	Total    int64      `json:"total"`
	Segments []*segment `json:"segments"`
}

func segsPath(part string) string { return part + ".segs" }

func loadSegs(part string) (*segFile, bool) {
	b, err := os.ReadFile(segsPath(part))
	if err != nil {
		return nil, false
	}
	var st segFile
	if json.Unmarshal(b, &st) != nil || st.Total <= 0 || len(st.Segments) == 0 {
		os.Remove(segsPath(part))
		return nil, false
	}
	for _, g := range st.Segments {
		if g.Start < 0 || g.End > st.Total || g.Start > g.End || g.Done < 0 || g.Done > g.End-g.Start {
			os.Remove(segsPath(part))
			return nil, false
		}
	}
	return &st, true
}

// saveSegs writes the state (after the data it describes is on disk).
func saveSegs(part string, st *segFile) error {
	b, err := json.Marshal(st)
	if err != nil {
		return err
	}
	tmp := segsPath(part) + ".tmp"
	if err := os.WriteFile(tmp, b, 0o664); err != nil {
		return err
	}
	return os.Rename(tmp, segsPath(part))
}

// prefix is how much of the file is complete from its start.
func (st *segFile) prefix() int64 {
	segs := slices.Clone(st.Segments)
	slices.SortFunc(segs, func(a, b *segment) int { return compare(a.Start, b.Start) })
	at := int64(0)
	for _, g := range segs {
		if g.Start > at {
			break
		}
		at = max(at, g.next())
		if g.left() > 0 {
			break
		}
	}
	return at
}

func (st *segFile) done() int64 {
	var n int64
	for _, g := range st.Segments {
		n += g.Done
	}
	return n
}

func (st *segFile) complete() bool {
	for _, g := range st.Segments {
		if g.left() > 0 {
			return false
		}
	}
	return true
}

func compare(a, b int64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

// collapse turns a segmented download back into a plain one: the file keeps what is
// complete from its start, and the state goes.
func collapse(f *os.File, part string, st *segFile) error {
	if err := f.Truncate(st.prefix()); err != nil {
		return err
	}
	return os.Remove(segsPath(part))
}

// segRun is a segmented download in progress.
type segRun struct {
	s         *Service
	j         *job
	v         Save
	link, ua  string
	f         *os.File
	part      string
	mu        sync.Mutex
	st        *segFile
	seq       int
	err       error // the first range that failed
	ended     chan struct{}
	lastFlush time.Time
}

// segmented downloads the rest of a segmented file. first, when set, is an answer already
// open for the range starting at the first unfinished segment's next byte.
func (s *Service) segmented(ctx context.Context, j *job, v Save, link, ua string, f *os.File, part string, st *segFile,
	first *http.Response, processed bool) error {
	need := st.Total - st.done() + spaceMargin
	if processed {
		need += st.Total
	}
	if free, err := s.freeSpace(filepath.Dir(part)); err == nil && free < uint64(need) {
		if first != nil {
			first.Body.Close()
		}
		return permError{fmt.Errorf("not enough disk space: %s needed, %s free", sizeText(need), sizeText(int64(free))), true}
	}
	if fi, err := f.Stat(); err == nil && fi.Size() != st.Total {
		if err := f.Truncate(st.Total); err != nil {
			if first != nil {
				first.Body.Close()
			}
			return permError{fmt.Errorf("couldn't write the file: %w", err), true}
		}
	}
	if err := saveSegs(part, st); err != nil {
		if first != nil {
			first.Body.Close()
		}
		return permError{fmt.Errorf("couldn't write the file: %w", err), true}
	}
	s.DB.ExecContext(ctx, "UPDATE vod_saves SET bytes_total=?, bytes_done=? WHERE id=?", st.Total, st.done(), v.ID)
	j.setBytes(st.done(), st.Total)
	j.setSplit(true)
	defer j.setSplit(false)

	r := &segRun{s: s, j: j, v: v, link: link, ua: ua, f: f, part: part, st: st, ended: make(chan struct{}, 2*maxConnections),
		lastFlush: time.Now()}
	if first != nil {
		r.mu.Lock()
		if g := r.pick(); g != nil { // the first unfinished range, which first answers
			r.launch(ctx, g, first)
		} else {
			first.Body.Close()
		}
		r.mu.Unlock()
	}
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	for {
		r.mu.Lock()
		if r.err == nil && ctx.Err() == nil && !st.complete() {
			want := max(j.wantConns(), 1)
			running := r.running()
			// Connections taken back: the newest ranges stop (picked up again later).
			for running > want {
				if g := r.newest(); g != nil {
					g.stopping = true
					g.cancel()
				}
				running--
			}
			for running < want {
				g := r.pick()
				if g == nil {
					break
				}
				r.launch(ctx, g, nil)
				running++
			}
		}
		busy := r.busy()
		finished := st.complete() && busy == 0
		failed := (r.err != nil || ctx.Err() != nil) && busy == 0
		if r.err != nil || ctx.Err() != nil {
			for _, g := range st.Segments {
				if g.busy && g.cancel != nil {
					g.cancel()
				}
			}
		}
		r.mu.Unlock()
		if finished || failed {
			break
		}
		select {
		case <-r.ended:
		case <-j.wantCh:
		case <-ctx.Done():
		case <-tick.C:
			r.flush(ctx)
		}
	}
	syncErr := f.Sync()
	r.mu.Lock()
	err := r.err
	complete := st.complete()
	r.mu.Unlock()
	r.flush(context.WithoutCancel(ctx))
	switch {
	case complete && syncErr == nil:
		os.Remove(segsPath(part))
		s.DB.ExecContext(ctx, "UPDATE vod_saves SET bytes_done=? WHERE id=?", st.Total, v.ID)
		return nil
	case ctx.Err() != nil:
		return ctx.Err()
	case syncErr != nil:
		return permError{fmt.Errorf("couldn't write the file: %w", syncErr), true}
	}
	var pe permError
	if errors.As(err, &pe) {
		return err
	}
	return errSegFallback
}

// flush writes the state down (the data first).
func (r *segRun) flush(ctx context.Context) {
	r.f.Sync()
	r.mu.Lock()
	b, _ := json.Marshal(r.st)
	done := r.st.done()
	r.mu.Unlock()
	tmp := segsPath(r.part) + ".tmp"
	if os.WriteFile(tmp, b, 0o664) == nil {
		os.Rename(tmp, segsPath(r.part))
	}
	r.s.DB.ExecContext(ctx, "UPDATE vod_saves SET bytes_done=? WHERE id=?", done, r.v.ID)
}

// running is the ranges downloading and not being stopped. r.mu is held.
func (r *segRun) running() int {
	n := 0
	for _, g := range r.st.Segments {
		if g.busy && !g.stopping {
			n++
		}
	}
	return n
}

func (r *segRun) busy() int {
	n := 0
	for _, g := range r.st.Segments {
		if g.busy {
			n++
		}
	}
	return n
}

// newest is the range started last that isn't being stopped. r.mu is held.
func (r *segRun) newest() *segment {
	var out *segment
	for _, g := range r.st.Segments {
		if g.busy && !g.stopping && (out == nil || g.seq > out.seq) {
			out = g
		}
	}
	return out
}

// pick is the next range to download: an idle unfinished one, else the second half of
// the busy one with the most left. r.mu is held.
func (r *segRun) pick() *segment {
	var idle, big *segment
	for _, g := range r.st.Segments {
		switch {
		case g.left() <= 0:
		case !g.busy:
			if idle == nil || g.Start < idle.Start {
				idle = g
			}
		case !g.stopping && (big == nil || g.left() > big.left()):
			big = g
		}
	}
	if idle != nil {
		return idle
	}
	if big == nil || big.left() < 2*r.s.splitMin() {
		return nil
	}
	mid := big.next() + big.left()/2
	n := &segment{Start: mid, End: big.End}
	big.End = mid
	r.st.Segments = append(r.st.Segments, n)
	return n
}

// launch starts downloading a range. r.mu is held.
func (r *segRun) launch(ctx context.Context, g *segment, resp *http.Response) {
	gctx, cancel := context.WithCancel(ctx)
	r.seq++
	g.busy, g.stopping, g.seq, g.cancel = true, false, r.seq, cancel
	r.j.addConns(1)
	go func() {
		err := r.fetch(gctx, g, resp)
		stopped := gctx.Err() != nil // stopped on purpose, not failed
		cancel()
		r.mu.Lock()
		g.busy, g.stopping, g.cancel = false, false, nil
		if err != nil && !stopped && r.err == nil {
			r.err = err
		}
		r.mu.Unlock()
		r.j.addConns(-1)
		select {
		case r.ended <- struct{}{}:
		default: // the loop is due to look anyway
		}
	}()
}

// fetch downloads a range (resp: an answer already open for it) until it is done.
func (r *segRun) fetch(ctx context.Context, g *segment, resp *http.Response) error {
	if resp == nil {
		r.mu.Lock()
		from, to := g.next(), g.End-1
		r.mu.Unlock()
		var err error
		resp, err = r.s.getRange(ctx, r.link, r.ua, from, to)
		if err != nil {
			return err
		}
		m := contentRange.FindStringSubmatch(resp.Header.Get("Content-Range"))
		if resp.StatusCode != http.StatusPartialContent || m == nil {
			resp.Body.Close()
			return fmt.Errorf("the source answered %s to a range", resp.Status)
		}
		start, _ := strconv.ParseInt(m[1], 10, 64)
		total, _ := strconv.ParseInt(m[3], 10, 64)
		if start != from || (m[3] != "*" && total != r.st.Total) {
			resp.Body.Close()
			return errors.New("the source answered another range")
		}
	}
	defer resp.Body.Close()
	// Stopping the range ends its connection (an answer opened before it was a range too).
	defer context.AfterFunc(ctx, func() { resp.Body.Close() })()
	r.s.sourceUp(r.v.SourceID)
	stall := time.AfterFunc(time.Minute, func() { resp.Body.Close() })
	defer stall.Stop()
	buf := make([]byte, 256<<10)
	for {
		r.mu.Lock()
		at, left := g.next(), g.left()
		r.mu.Unlock()
		if left <= 0 {
			return nil // done (or the rest went to another range)
		}
		n, rerr := resp.Body.Read(buf[:min(int64(len(buf)), left)])
		if n > 0 {
			stall.Reset(time.Minute)
			if _, err := r.f.WriteAt(buf[:n], at); err != nil {
				return permError{fmt.Errorf("couldn't write the file: %w", err), true}
			}
			r.mu.Lock()
			// The range may have been split meanwhile: what went past its new end is the
			// same bytes the next range writes.
			add := min(int64(n), g.End-g.next())
			g.Done += max(add, 0)
			r.mu.Unlock()
			r.j.add(max(add, 0))
		}
		if rerr == io.EOF {
			r.mu.Lock()
			left = g.left()
			r.mu.Unlock()
			if left > 0 {
				return netError{errors.New("the connection ended early")}
			}
			return nil
		}
		if rerr != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return netError{rerr}
		}
	}
}
