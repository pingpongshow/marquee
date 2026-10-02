// Package tasks runs background work: library scans now; metadata refresh, backups and
// other maintenance as later milestones add them.
package tasks

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"marquee/internal/library"
	"marquee/internal/scanner"
	"marquee/internal/settings"
)

type ScanState string

const (
	Idle     ScanState = "idle"
	Queued   ScanState = "queued"
	Scanning ScanState = "scanning"
)

type ScanStatus struct {
	State      ScanState
	Progress   *scanner.Progress
	LastResult string
	LastError  string
}

// Scans runs library scans one at a time (they are disk-bound; parallel scans of the same
// array only slow each other down) and keeps per-library status for the UI.
type Scans struct {
	DB        *sql.DB
	Scanner   *scanner.Scanner
	Libraries *library.Store
	Settings  *settings.Store
	// AfterScan, if set, runs after each successful scan as part of the same task, e.g.
	// metadata matching. Its progress is reported through report.
	AfterScan func(ctx context.Context, lib library.Library, report func(scanner.Progress)) error

	mu     sync.Mutex
	status map[int64]*ScanStatus
	queue  []int64
	cancel map[int64]context.CancelFunc
	wake   chan struct{}
}

func (s *Scans) init() {
	if s.status == nil {
		s.status = map[int64]*ScanStatus{}
		s.cancel = map[int64]context.CancelFunc{}
		s.wake = make(chan struct{}, 1)
	}
}

// Run processes the queue until ctx is cancelled, and queues scheduled rescans.
func (s *Scans) Run(ctx context.Context) {
	s.mu.Lock()
	s.init()
	s.mu.Unlock()

	// Runs left "running" by a restart are recorded as interrupted.
	s.DB.ExecContext(ctx, `UPDATE task_runs SET status = 'failed', message = 'interrupted by server restart',
		finished_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE status = 'running' AND task = 'scan'`)

	if s.Settings.Get().Library.ScanOnStartup {
		s.QueueAll(ctx)
	} else if libs, err := s.Libraries.List(ctx); err == nil {
		// Libraries that have never completed a scan (e.g. the server restarted mid-scan).
		for _, l := range libs {
			if l.LastScannedAt == nil {
				s.Queue(l.ID)
			}
		}
	}
	tick := time.NewTicker(10 * time.Minute)
	defer tick.Stop()
	for {
		if id, ok := s.next(); ok {
			s.runOne(ctx, id)
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-s.wake:
		case <-tick.C:
			s.queueDue(ctx)
		}
	}
}

// Queue adds a library to the scan queue. It is a no-op if already queued or running.
func (s *Scans) Queue(libID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.init()
	st := s.status[libID]
	if st == nil {
		st = &ScanStatus{State: Idle}
		s.status[libID] = st
	}
	if st.State != Idle {
		return
	}
	st.State = Queued
	s.queue = append(s.queue, libID)
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *Scans) QueueAll(ctx context.Context) {
	libs, err := s.Libraries.List(ctx)
	if err != nil {
		slog.Error("queue all scans", "err", err)
		return
	}
	for _, l := range libs {
		s.Queue(l.ID)
	}
}

// Cancel removes a queued scan or stops a running one.
func (s *Scans) Cancel(libID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.init()
	if c := s.cancel[libID]; c != nil {
		c()
	}
	for i, id := range s.queue {
		if id == libID {
			s.queue = append(s.queue[:i], s.queue[i+1:]...)
			s.status[libID].State = Idle
			break
		}
	}
}

// Status returns a copy of a library's scan status.
func (s *Scans) Status(libID int64) ScanStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.init()
	st := s.status[libID]
	if st == nil {
		return ScanStatus{State: Idle}
	}
	out := *st
	if st.Progress != nil {
		p := *st.Progress
		out.Progress = &p
	}
	return out
}

// Active returns the status of every queued or running scan, keyed by library id.
func (s *Scans) Active() map[int64]ScanStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[int64]ScanStatus{}
	for id, st := range s.status {
		if st.State == Idle {
			continue
		}
		c := *st
		if st.Progress != nil {
			p := *st.Progress
			c.Progress = &p
		}
		out[id] = c
	}
	return out
}

func (s *Scans) next() (int64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.queue) == 0 {
		return 0, false
	}
	id := s.queue[0]
	s.queue = s.queue[1:]
	return id, true
}

func (s *Scans) runOne(parent context.Context, libID int64) {
	lib, err := s.Libraries.Get(parent, libID)
	if err != nil {
		s.finish(libID, "", err)
		return
	}
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	s.mu.Lock()
	s.cancel[libID] = cancel
	s.status[libID].State = Scanning
	s.status[libID].Progress = &scanner.Progress{Phase: "walking"}
	s.mu.Unlock()

	runID := s.recordStart(parent, libID)
	slog.Info("scan started", "library", lib.Name)
	st, err := s.Scanner.Scan(ctx, lib, s.Settings.Get().Library.IgnorePatterns, func(p scanner.Progress) {
		s.mu.Lock()
		s.status[libID].Progress = &p
		s.mu.Unlock()
	})
	result := st.String()
	if err != nil {
		slog.Warn("scan failed", "library", lib.Name, "err", err, "partial", result)
	} else {
		slog.Info("scan finished", "library", lib.Name, "result", result)
		if s.AfterScan != nil {
			if aerr := s.AfterScan(ctx, lib, func(p scanner.Progress) {
				s.mu.Lock()
				s.status[libID].Progress = &p
				s.mu.Unlock()
			}); aerr != nil {
				err = fmt.Errorf("metadata: %w", aerr)
			}
		}
	}
	s.recordFinish(parent, runID, result, err)
	s.finish(libID, result, err)
}

func (s *Scans) finish(libID int64, result string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := s.status[libID]
	if st == nil {
		st = &ScanStatus{}
		s.status[libID] = st
	}
	st.State = Idle
	st.Progress = nil
	st.LastResult = result
	st.LastError = ""
	if err != nil {
		st.LastError = err.Error()
		if errors.Is(err, context.Canceled) {
			st.LastError = "cancelled"
		}
	}
	delete(s.cancel, libID)
}

// queueDue queues libraries whose scan interval has elapsed.
func (s *Scans) queueDue(ctx context.Context) {
	libs, err := s.Libraries.List(ctx)
	if err != nil {
		return
	}
	for _, l := range libs {
		hours := 24
		if l.Options.ScanIntervalHours != nil {
			hours = *l.Options.ScanIntervalHours
		}
		if hours <= 0 {
			continue
		}
		if l.LastScannedAt == nil || time.Since(*l.LastScannedAt) > time.Duration(hours)*time.Hour {
			s.Queue(l.ID)
		}
	}
}

func (s *Scans) recordStart(ctx context.Context, libID int64) int64 {
	r, err := s.DB.ExecContext(ctx, `INSERT INTO task_runs(task, library_id, status, started_at)
		VALUES ('scan', ?, 'running', strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))`, libID)
	if err != nil {
		return 0
	}
	id, _ := r.LastInsertId()
	return id
}

func (s *Scans) recordFinish(ctx context.Context, runID int64, msg string, err error) {
	status := "succeeded"
	switch {
	case errors.Is(err, context.Canceled):
		status = "cancelled"
	case err != nil:
		status = "failed"
		msg = err.Error() + " (" + msg + ")"
	}
	s.DB.ExecContext(ctx, `UPDATE task_runs SET status = ?, message = ?, progress = 1,
		finished_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = ?`, status, msg, runID)
}
