package tasks

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"marquee/internal/settings"
)

var (
	ErrUnknownTask = errors.New("no such task")
	ErrTaskRunning = errors.New("the task is already running")
)

// Task is a maintenance job (ADM-2). Window tasks run once a day inside the maintenance
// window; interval tasks run every Every.
type Task struct {
	ID, Name, Description string
	Window                bool
	// Bounded window tasks are stopped (their context ends) when the window closes and
	// pick up where they left off next time. Run Now ignores the window.
	Bounded bool
	Every   time.Duration
	Run     func(ctx context.Context) (string, error)
	// Progress, when set, reports a running task's progress for the activity indicator.
	Progress func() (done, total int)
}

func (t Task) Schedule() string {
	switch {
	case t.Window:
		return "Daily in the maintenance window"
	case t.Every >= 24*time.Hour:
		return fmt.Sprintf("Every %d days", int(t.Every/(24*time.Hour)))
	case t.Every > 0:
		return fmt.Sprintf("Every %d hours", int(t.Every/time.Hour))
	default:
		return "Manual"
	}
}

type LastRun struct {
	Status, Message       string
	StartedAt, FinishedAt *time.Time
}

type TaskInfo struct {
	Task
	Running bool
	Last    *LastRun
}

// Scheduler runs maintenance tasks on their schedules and records each run in task_runs.
type Scheduler struct {
	DB       *sql.DB
	Settings *settings.Store
	Now      func() time.Time

	mu      sync.Mutex
	tasks   []Task
	running map[string]bool
}

func (s *Scheduler) Register(t Task) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks = append(s.tasks, t)
}

func (s *Scheduler) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Scheduler) find(id string) (Task, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.tasks {
		if t.ID == id {
			return t, true
		}
	}
	return Task{}, false
}

// List returns every task with its last run.
func (s *Scheduler) List(ctx context.Context) ([]TaskInfo, error) {
	s.mu.Lock()
	tasks := append([]Task(nil), s.tasks...)
	s.mu.Unlock()
	out := make([]TaskInfo, len(tasks))
	for i, t := range tasks {
		out[i] = TaskInfo{Task: t, Running: s.isRunning(t.ID)}
		var status string
		var msg, started, finished sql.NullString
		err := s.DB.QueryRowContext(ctx, `SELECT status, message, started_at, finished_at FROM task_runs WHERE task = ? ORDER BY id DESC LIMIT 1`, t.ID).
			Scan(&status, &msg, &started, &finished)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		lr := &LastRun{Status: status, Message: msg.String}
		if ts, err := time.Parse(time.RFC3339Nano, started.String); err == nil {
			lr.StartedAt = &ts
		}
		if ts, err := time.Parse(time.RFC3339Nano, finished.String); err == nil {
			lr.FinishedAt = &ts
		}
		out[i].Last = lr
	}
	return out, nil
}

// Running lists the tasks running now.
func (s *Scheduler) Running() []Task {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Task
	for _, t := range s.tasks {
		if s.running[t.ID] {
			out = append(out, t)
		}
	}
	return out
}

func (s *Scheduler) isRunning(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running[id]
}

// RunNow starts a task in the background.
func (s *Scheduler) RunNow(ctx context.Context, id string) error {
	return s.start(ctx, id, time.Time{})
}

// start runs a task in the background, stopping it at deadline when that is set.
func (s *Scheduler) start(ctx context.Context, id string, deadline time.Time) error {
	t, ok := s.find(id)
	if !ok {
		return ErrUnknownTask
	}
	s.mu.Lock()
	if s.running == nil {
		s.running = map[string]bool{}
	}
	if s.running[id] {
		s.mu.Unlock()
		return ErrTaskRunning
	}
	s.running[id] = true
	s.mu.Unlock()
	run := context.WithoutCancel(ctx)
	var cancel context.CancelFunc = func() {}
	if !deadline.IsZero() {
		run, cancel = context.WithDeadline(run, deadline)
	}
	go func() {
		defer cancel()
		s.execute(run, t)
	}()
	return nil
}

func (s *Scheduler) execute(ctx context.Context, t Task) {
	defer func() {
		s.mu.Lock()
		delete(s.running, t.ID)
		s.mu.Unlock()
	}()
	res, err := s.DB.ExecContext(ctx, `INSERT INTO task_runs (task, status, started_at) VALUES (?, 'running', strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))`, t.ID)
	if err != nil {
		slog.Warn("task", "task", t.ID, "err", err)
		return
	}
	runID, _ := res.LastInsertId()
	msg, err := func() (msg string, err error) {
		defer func() {
			if r := recover(); r != nil {
				err = fmt.Errorf("panic: %v", r)
			}
		}()
		return t.Run(ctx)
	}()
	status := "succeeded"
	if err != nil {
		status, msg = "failed", err.Error()
		slog.Warn("task failed", "task", t.ID, "err", err)
	} else {
		slog.Info("task finished", "task", t.ID, "result", msg)
	}
	// The run's context may have hit its deadline; recording the result must still work.
	s.DB.ExecContext(context.WithoutCancel(ctx), `UPDATE task_runs SET status = ?, message = ?, progress = 1, finished_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = ?`,
		status, msg, runID)
}

const interrupted = "interrupted by server restart"

// lastStart is when the task last started (zero if never).
func (s *Scheduler) lastStart(ctx context.Context, id string) time.Time {
	var started sql.NullString
	// Runs cut short by a restart don't count, so they start again (within the window).
	s.DB.QueryRowContext(ctx, `SELECT MAX(started_at) FROM task_runs WHERE task = ? AND status IN ('running', 'succeeded', 'failed')
		AND COALESCE(message, '') != ?`, id, interrupted).Scan(&started)
	t, _ := time.Parse(time.RFC3339Nano, started.String)
	return t
}

// inWindow reports whether now is inside the maintenance window, and when the current
// window started.
func inWindow(now time.Time, start string, hours int) (bool, time.Time) {
	var h, m int
	if _, err := fmt.Sscanf(start, "%d:%d", &h, &m); err != nil {
		h, m = 3, 0
	}
	ws := time.Date(now.Year(), now.Month(), now.Day(), h, m, 0, 0, now.Location())
	if ws.After(now) {
		ws = ws.AddDate(0, 0, -1)
	}
	return now.Before(ws.Add(time.Duration(hours) * time.Hour)), ws
}

// due reports whether t should run now.
func (s *Scheduler) due(ctx context.Context, t Task, now time.Time) bool {
	last := s.lastStart(ctx, t.ID)
	if t.Window {
		cfg := s.Settings.Get().Tasks
		in, ws := inWindow(now, cfg.MaintenanceWindowStart, cfg.MaintenanceWindowHours)
		// Run once per window; if the server was down for a whole window, catch up within a day.
		return (in && last.Before(ws)) || now.Sub(last) > 36*time.Hour
	}
	return t.Every > 0 && now.Sub(last) >= t.Every
}

// Run checks every minute for due tasks until ctx is cancelled.
func (s *Scheduler) Run(ctx context.Context) {
	s.DB.ExecContext(ctx, `UPDATE task_runs SET status = 'failed', message = '`+interrupted+`',
		finished_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE status = 'running' AND task != 'scan'`)
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		s.mu.Lock()
		tasks := append([]Task(nil), s.tasks...)
		s.mu.Unlock()
		now := s.now()
		for _, t := range tasks {
			if !s.isRunning(t.ID) && s.due(ctx, t, now) {
				var deadline time.Time
				if t.Window && t.Bounded {
					cfg := s.Settings.Get().Tasks
					if in, ws := inWindow(now, cfg.MaintenanceWindowStart, cfg.MaintenanceWindowHours); in {
						deadline = ws.Add(time.Duration(cfg.MaintenanceWindowHours) * time.Hour)
					}
				}
				s.start(ctx, t.ID, deadline)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
