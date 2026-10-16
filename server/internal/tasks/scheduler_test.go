package tasks

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"marquee/internal/db"
)

func TestInWindow(t *testing.T) {
	loc := time.UTC
	at := func(h, m int) time.Time { return time.Date(2026, 10, 2, h, m, 0, 0, loc) }
	cases := []struct {
		now   time.Time
		in    bool
		start time.Time
	}{
		{at(3, 30), true, at(3, 0)},
		{at(2, 59), false, at(3, 0).AddDate(0, 0, -1)},
		{at(7, 0), false, at(3, 0)},
		{at(6, 59), true, at(3, 0)},
	}
	for _, c := range cases {
		in, start := inWindow(c.now, "03:00", 4)
		if in != c.in || !start.Equal(c.start) {
			t.Errorf("%v: got %v %v, want %v %v", c.now, in, start, c.in, c.start)
		}
	}
	// A window that crosses midnight.
	if in, _ := inWindow(at(1, 0), "23:00", 4); !in {
		t.Error("01:00 should be inside a 23:00 + 4 h window")
	}
}

func TestEveryFunc(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	d, err := db.Open(ctx, filepath.Join(dir, "t.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	every := time.Duration(0)
	task := Task{ID: "x", EveryFunc: func() time.Duration { return every }}
	s := &Scheduler{DB: d}
	now := time.Now().UTC()
	if s.due(ctx, task, now) || task.Schedule() != "Manual" {
		t.Errorf("off: due %v, schedule %q", s.due(ctx, task, now), task.Schedule())
	}
	every = 30 * time.Minute
	if !s.due(ctx, task, now) || task.Schedule() != "Every 30 minutes" {
		t.Errorf("never run: due %v, schedule %q", s.due(ctx, task, now), task.Schedule())
	}
	d.Exec(`INSERT INTO task_runs (task, status, started_at) VALUES ('x', 'succeeded', ?)`, now.Add(-10*time.Minute).Format(time.RFC3339Nano))
	if s.due(ctx, task, now) {
		t.Error("due 10 minutes after the last run with a 30-minute interval")
	}
	if !s.due(ctx, task, now.Add(21*time.Minute)) {
		t.Error("not due 31 minutes after the last run")
	}
}
