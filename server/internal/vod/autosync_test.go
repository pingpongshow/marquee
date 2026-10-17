package vod

import (
	"context"
	"sync"
	"testing"
	"time"

	"marquee/internal/settings"
)

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *fakeClock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func refreshedAt(t *testing.T, s *Service) string {
	t.Helper()
	var at *string
	s.DB.QueryRow(`SELECT refreshed_at FROM vod_sources WHERE id = 'p'`).Scan(&at)
	if at == nil {
		return ""
	}
	return *at
}

func setSync(t *testing.T, s *Service, auto bool, hours int) {
	t.Helper()
	if _, err := s.Settings.Update(context.Background(), func(st *settings.Settings) error {
		st.Integrations.VODAutoRefresh, st.Integrations.VODRefreshHours = auto, hours
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

// Automatic sync off: a never-loaded source loads once at start, then lists reload only
// when asked. On: a refresh is due the interval after the last successful one, and a
// changed interval applies without a restart.
func TestAutoSync(t *testing.T) {
	s, _, _, _ := setup(t)
	clk := &fakeClock{t: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)}
	s.Now = clk.now
	ctx := context.Background()
	stamp := func() string { return clk.now().UTC().Format(time.RFC3339) }

	setSync(t, s, false, 6)
	if !s.due(ctx, true) {
		t.Fatal("a never-loaded source should load at start even with automatic sync off")
	}
	if s.due(ctx, false) {
		t.Fatal("off: no automatic refresh after start")
	}
	s.Refresh(ctx, false)
	settle(t, s)
	first := refreshedAt(t, s)
	if first != stamp() {
		t.Fatalf("refreshed_at = %q", first)
	}
	clk.add(7 * time.Hour)
	if s.due(ctx, true) || s.due(ctx, false) {
		t.Error("off: a loaded source refreshed on its own")
	}
	// Run with automatic sync off does nothing on its own.
	runCtx, stop := context.WithCancel(ctx)
	s.Tick = 5 * time.Millisecond
	done := make(chan struct{})
	go func() { s.Run(runCtx, nil); close(done) }()
	time.Sleep(60 * time.Millisecond)
	stop()
	<-done
	if got := refreshedAt(t, s); got != first {
		t.Errorf("off: refreshed automatically (%s)", got)
	}
	// Manual refresh works with automatic sync off.
	s.Refresh(ctx, true)
	settle(t, s)
	manual := refreshedAt(t, s)
	if manual != stamp() {
		t.Errorf("manual refresh with sync off: %q", manual)
	}

	// On, every 6 hours: not due 5 hours after the last refresh, due at 6.
	setSync(t, s, true, 6)
	clk.add(5 * time.Hour)
	if s.due(ctx, false) {
		t.Error("on: due before the interval")
	}
	// A shorter interval applies at once.
	setSync(t, s, true, 4)
	if !s.due(ctx, false) {
		t.Error("on: a shorter interval didn't apply")
	}
	setSync(t, s, true, 6)
	clk.add(time.Hour)
	if !s.due(ctx, false) {
		t.Fatal("on: not due after the interval")
	}
	runCtx, stop = context.WithCancel(ctx)
	done = make(chan struct{})
	go func() { s.Run(runCtx, nil); close(done) }()
	deadline := time.Now().Add(5 * time.Second)
	for refreshedAt(t, s) != stamp() && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	stop()
	<-done
	settle(t, s)
	if got := refreshedAt(t, s); got != stamp() {
		t.Errorf("on: not refreshed after the interval (%s)", got)
	}
	if s.due(ctx, false) {
		t.Error("on: still due right after refreshing")
	}
	// Manual refresh works with automatic sync on too.
	clk.add(time.Minute)
	s.Refresh(ctx, true)
	settle(t, s)
	if got := refreshedAt(t, s); got != stamp() {
		t.Errorf("manual refresh with sync on: %q", got)
	}
}

// A source that fails is tried again after the interval, not every minute.
func TestAutoSyncFailingSourceWaits(t *testing.T) {
	s, srv, _, _ := setup(t)
	clk := &fakeClock{t: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)}
	s.Now = clk.now
	ctx := context.Background()
	s.Settings.Update(ctx, func(st *settings.Settings) error {
		st.Integrations.VODSources[0].URL = srv.URL + "/missing.m3u"
		return nil
	})
	if !s.due(ctx, false) {
		t.Fatal("a never-loaded source should be due with automatic sync on")
	}
	s.Refresh(ctx, false)
	if refreshedAt(t, s) != "" {
		t.Fatal("a failed first load shouldn't count as loaded")
	}
	clk.add(time.Minute)
	if s.due(ctx, false) {
		t.Error("a failing source retried a minute later")
	}
	clk.add(6 * time.Hour)
	if !s.due(ctx, false) {
		t.Error("a failing source not retried after the interval")
	}
}
