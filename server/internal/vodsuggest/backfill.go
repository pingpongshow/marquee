package vodsuggest

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"

	"marquee/internal/settings"
	"marquee/internal/tasks"
	"marquee/internal/vod"
)

// The "Fetch VOD suggestion data" task: TMDB's recommendations, keywords and vote counts for
// the films and series on demand matched before they were kept, so suggestions have them.
// It runs in the maintenance window (stopping when it closes and carrying on the next
// night, best-rated titles first) at four requests a second, and after a catalogue refresh
// matched new titles, a small batch at any time of day.

// BackfillTaskID is the task's id (for its on/off switch and Run now).
const BackfillTaskID = "vod-suggest-backfill"

// afterRefreshBatch is how many titles a run after a catalogue refresh fetches outside the
// window by default.
const afterRefreshBatch = 200

// Backfill is the task.
type Backfill struct {
	VOD       *vod.Service
	Suggest   *Service // told when data was filled in, so suggestions are worked out again; may be nil
	Scheduler *tasks.Scheduler
	Settings  *settings.Store
	// Every is the pace (default vod.BackfillEvery, 4 a second); Now the clock (tests).
	Every time.Duration
	Now   func() time.Time
	// Batch is how many titles a run after a refresh fetches outside the window (default 200).
	Batch int

	afterRefresh atomic.Bool
}

func (b *Backfill) now() time.Time {
	if b.Now != nil {
		return b.Now()
	}
	return time.Now()
}

// Task is the scheduled task.
func (b *Backfill) Task() tasks.Task {
	return tasks.Task{ID: BackfillTaskID, Name: "Fetch VOD suggestion data", Window: true, Bounded: true,
		Description: "Fetches TMDB's recommendations, keywords and vote counts for films and series on demand matched before Marquee kept them, four a second, for suggestions from VOD. Runs in the maintenance window and carries on the next night where it stopped; after a VOD refresh matches new titles it also fetches up to 200 of them straight away.",
		Run:         b.Run, Progress: b.VOD.BackfillProgress}
}

// Run fetches: in the maintenance window until it closes; after a catalogue refresh, outside
// the window, up to afterRefreshBatch titles; on Run now, everything.
func (b *Backfill) Run(ctx context.Context) (string, error) {
	small := b.afterRefresh.Swap(false)
	batch := b.Batch
	if batch <= 0 {
		batch = afterRefreshBatch
	}
	limit := 0
	if b.Settings != nil {
		if end, in := tasks.WindowEnd(b.now(), b.Settings.Get().Tasks); in {
			var cancel context.CancelFunc
			ctx, cancel = context.WithDeadline(ctx, end)
			defer cancel()
		} else if small {
			limit = batch
		}
	} else if small {
		limit = batch
	}
	n, err := b.VOD.BackfillExtras(ctx, limit, b.Every)
	if n > 0 && b.Suggest != nil {
		b.Suggest.CatalogChanged()
	}
	if err != nil {
		return "", err
	}
	left, err := b.VOD.MissingExtras(context.WithoutCancel(ctx))
	if err != nil {
		return "", err
	}
	if left == 0 {
		return fmt.Sprintf("Fetched %d; every VOD title has its suggestion data", n), nil
	}
	return fmt.Sprintf("Fetched %d; %d left for the next run", n, left), nil
}

// AfterRefresh starts a small run when a catalogue refresh left matched titles without
// their data (not when the task is turned off or already running).
func (b *Backfill) AfterRefresh(ctx context.Context) {
	if n, err := b.VOD.MissingExtras(ctx); err != nil || n == 0 || b.Scheduler == nil {
		return
	}
	b.afterRefresh.Store(true)
	if err := b.Scheduler.Trigger(ctx, BackfillTaskID); err != nil {
		b.afterRefresh.Store(false)
	}
}
