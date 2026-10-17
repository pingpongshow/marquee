package tasks

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"marquee/internal/db"
	"marquee/internal/settings"
)

// A task turned off (settings tasks.disabled) doesn't run on its schedule or when
// triggered automatically; Run now still runs it. The setting round-trips through the
// store, and unknown ids are refused by SetEnabled and dropped by Known.
func TestDisabledTasks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dir := t.TempDir()
	d, err := db.Open(ctx, filepath.Join(dir, "t.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	store, err := settings.Open(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	if got := store.Get().Tasks.Disabled; len(got) != 0 {
		t.Fatalf("default: %v", got)
	}
	var onRuns, offRuns atomic.Int32
	s := &Scheduler{DB: d, Settings: store}
	s.Register(Task{ID: "on", Every: time.Millisecond, Run: func(context.Context) (string, error) { onRuns.Add(1); return "", nil }})
	s.Register(Task{ID: "off", Every: time.Millisecond, Run: func(context.Context) (string, error) { offRuns.Add(1); return "", nil }})

	if err := s.SetEnabled(ctx, "nope", false); !errors.Is(err, ErrUnknownTask) {
		t.Fatalf("unknown id: %v", err)
	}
	if err := s.SetEnabled(ctx, "off", false); err != nil {
		t.Fatal(err)
	}
	s.SetEnabled(ctx, "off", false) // twice: listed once
	if got := store.Get().Tasks.Disabled; !slices.Equal(got, []string{"off"}) {
		t.Fatalf("stored: %v", got)
	}
	reopened, _ := settings.Open(ctx, d)
	if got := reopened.Get().Tasks.Disabled; !slices.Equal(got, []string{"off"}) {
		t.Fatalf("round trip: %v", got)
	}
	if got := s.Known([]string{"off", "nope", "on", "off"}); !slices.Equal(got, []string{"off", "on"}) {
		t.Errorf("known: %v", got)
	}
	list, _ := s.List(ctx)
	for _, ti := range list {
		if ti.Enabled != (ti.ID == "on") {
			t.Errorf("%s enabled %v", ti.ID, ti.Enabled)
		}
	}

	// The scheduler runs the task that's on, not the one that's off.
	go s.Run(ctx)
	deadline := time.Now().Add(5 * time.Second)
	for onRuns.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)
	if onRuns.Load() == 0 || offRuns.Load() != 0 {
		t.Fatalf("scheduled: on %d, off %d", onRuns.Load(), offRuns.Load())
	}
	// Automatic triggers skip it.
	if err := s.Trigger(ctx, "off"); !errors.Is(err, ErrTaskDisabled) {
		t.Fatalf("trigger: %v", err)
	}
	time.Sleep(50 * time.Millisecond)
	if offRuns.Load() != 0 {
		t.Fatal("triggered while off")
	}
	// Run now still works.
	if err := s.RunNow(ctx, "off"); err != nil {
		t.Fatal(err)
	}
	waitRuns(t, &offRuns, 1)
	// Back on: triggers work again.
	if err := s.SetEnabled(ctx, "off", true); err != nil {
		t.Fatal(err)
	}
	if got := store.Get().Tasks.Disabled; len(got) != 0 {
		t.Fatalf("after turning on: %v", got)
	}
	for s.isRunning("off") {
		time.Sleep(5 * time.Millisecond)
	}
	if err := s.Trigger(ctx, "off"); err != nil {
		t.Fatal(err)
	}
	waitRuns(t, &offRuns, 2)
}

func waitRuns(t *testing.T, n *atomic.Int32, want int32) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for n.Load() < want {
		if time.Now().After(deadline) {
			t.Fatalf("runs: %d, want %d", n.Load(), want)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
