package vodsuggest

import (
	"context"
	"strings"
	"testing"
	"time"

	"marquee/internal/metadata/tmdb"
	"marquee/internal/settings"
	"marquee/internal/tasks"
	"marquee/internal/vod"
)

type fakeTMDB struct{}

func (fakeTMDB) MatchMovie(context.Context, string, int) (*tmdb.Movie, error) { return nil, nil }
func (fakeTMDB) MovieByID(context.Context, int) (*tmdb.Movie, error)          { return nil, tmdb.ErrNotFound }
func (fakeTMDB) MovieExtras(_ context.Context, id int) (*tmdb.Movie, error) {
	m := &tmdb.Movie{ID: id, VoteCount: 100}
	m.Recommendations.Results = []tmdb.SearchResult{{ID: 1}}
	return m, nil
}
func (fakeTMDB) TVExtras(_ context.Context, id int) (*tmdb.TVShow, error) {
	return &tmdb.TVShow{ID: id}, nil
}

func missing(t *testing.T, e *env) int {
	t.Helper()
	var n int
	e.db.QueryRow(`SELECT COUNT(*) FROM vod_movies WHERE tmdb_recs IS NULL`).Scan(&n)
	return n
}

// The task: after a catalogue refresh, outside the maintenance window, a small batch (only
// when the task is on); in the window, everything; filled data has suggestions worked out
// again.
func TestBackfillTask(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	store, err := settings.Open(ctx, e.db)
	if err != nil {
		t.Fatal(err)
	}
	// The window is open now (the run's deadline is real time) and closed in three hours.
	night := time.Now()
	store.Update(ctx, func(s *settings.Settings) error {
		s.Tasks.MaintenanceWindowStart, s.Tasks.MaintenanceWindowHours = night.Add(-time.Hour).Format("15:04"), 2
		return nil
	})
	noon := night.Add(3 * time.Hour)
	now := noon
	for i := range 6 {
		e.vodMovie("Film "+itoa(int64(i)), 100+i, "", "")
	}
	v := &vod.Service{DB: e.db, Matcher: fakeTMDB{}}
	sched := &tasks.Scheduler{DB: e.db, Settings: store}
	b := &Backfill{VOD: v, Suggest: e.svc, Scheduler: sched, Settings: store, Every: time.Millisecond, Batch: 2, Now: func() time.Time { return now }}
	sched.Register(b.Task())
	wait := func() {
		t.Helper()
		for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
			if len(sched.Running()) == 0 {
				return
			}
		}
		t.Fatal("the task didn't finish")
	}

	// Turned off: a refresh doesn't start it.
	if err := sched.SetEnabled(ctx, BackfillTaskID, false); err != nil {
		t.Fatal(err)
	}
	b.AfterRefresh(ctx)
	wait()
	if n := missing(t, e); n != 6 {
		t.Fatalf("ran while turned off: %d missing", n)
	}
	sched.SetEnabled(ctx, BackfillTaskID, true)

	// Kept suggestions are marked for working out again once data is filled in.
	e.svc.For(ctx, e.user(), "", 5)
	b.AfterRefresh(ctx)
	wait()
	if n := missing(t, e); n != 4 {
		t.Fatalf("after a refresh at noon: %d missing, want 4", n)
	}
	e.svc.mu.Lock()
	for _, en := range e.svc.entries {
		if !en.stale {
			t.Error("suggestions not marked for rebuilding")
		}
	}
	e.svc.mu.Unlock()

	// In the window: the rest.
	now = night
	msg, err := b.Run(ctx)
	if err != nil || missing(t, e) != 0 || !strings.Contains(msg, "every VOD title") {
		t.Fatalf("in the window: %q %v, %d missing", msg, err, missing(t, e))
	}
	if b := b.Task(); !b.Window || !b.Bounded || b.Progress == nil {
		t.Errorf("task: %+v", b)
	}
}
