package intown

import (
	"context"
	"sync"
	"time"
)

const (
	// DefaultDailyBudget is what the background lookups may use in a day: a free key allows
	// 5,000 calls, so most of the rest stays for what people do.
	DefaultDailyBudget = 2000
	// hardDailyCap stops every call, people's too, short of the default quota.
	hardDailyCap = 4500
	// backgroundReserve is the quota the source reported that background lookups leave
	// alone, in case the key is also used elsewhere.
	backgroundReserve = 500
	// callSpacing is two calls a second: the docs say 5 and the FAQ 2, so the lower.
	callSpacing = 500 * time.Millisecond
	// When the source gives no reset, waits double from firstBackoff up to maxBackoff.
	firstBackoff = 15 * time.Minute
	maxBackoff   = 6 * time.Hour
	// unknownResetWait is how long to believe a reported quota when the source doesn't say when
	// it resets, so a count of nothing left can't hold calls for good.
	unknownResetWait = time.Hour
)

type backgroundKey struct{}

// background marks calls as the background job's, which have the smaller budget.
func background(ctx context.Context) context.Context {
	return context.WithValue(ctx, backgroundKey{}, true)
}

func isBackground(ctx context.Context) bool {
	b, _ := ctx.Value(backgroundKey{}).(bool)
	return b
}

// budgetState is what's remembered between runs, in the cache file.
type budgetState struct {
	Day        string    `json:"day"` // UTC date the counts are for
	Used       int       `json:"used"`
	Background int       `json:"background"`
	Limit      int       `json:"limit"`     // the key's quota as reported, -1 unknown
	Available  int       `json:"available"` // what's left as reported, -1 unknown
	Reset      time.Time `json:"reset"`
	Backoff    time.Time `json:"backoff"`
	Strikes    int       `json:"strikes"` // 429s in a row with no reset given
	// BadKey: the source refused the key, so nothing is sent until the key changes.
	BadKey bool `json:"badKey,omitempty"`
}

// budget keeps calls to the pace and inside the day's allowance, across the background job
// and people's requests. It is the Limiter for the source.
type budget struct {
	// Daily is the background allowance.
	Daily func() int
	Now   func() time.Time
	// Sleep waits for a slot; replaced in tests.
	Sleep func(ctx context.Context, d time.Duration) error
	// Saved hears each change, to remember it.
	Saved func(budgetState)

	mu    sync.Mutex
	state budgetState
	next  time.Time
}

func newBudget(daily func() int, now func() time.Time) *budget {
	return &budget{Daily: daily, Now: now, Sleep: sleepCtx, state: budgetState{Limit: -1, Available: -1}}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// roll starts a new day's counts. mu held.
func (b *budget) roll(now time.Time) {
	if day := now.UTC().Format(time.DateOnly); b.state.Day != day {
		b.state.Day, b.state.Used, b.state.Background = day, 0, 0
	}
	if !b.state.Reset.IsZero() && now.After(b.state.Reset) {
		b.state.Available, b.state.Reset = -1, time.Time{}
	}
}

func (b *budget) Before(ctx context.Context) error {
	b.mu.Lock()
	now := b.Now()
	b.roll(now)
	s := &b.state
	if s.BadKey {
		b.mu.Unlock()
		return ErrBadKey
	}
	over := now.Before(s.Backoff) || s.Used >= hardDailyCap || s.Available == 0
	bg := isBackground(ctx)
	if bg {
		over = over || s.Background >= b.Daily() || (s.Available >= 0 && s.Available <= backgroundReserve)
	}
	if over {
		b.mu.Unlock()
		return ErrOverBudget
	}
	slot := b.next
	if slot.Before(now) {
		slot = now
	}
	b.next = slot.Add(callSpacing)
	s.Used++
	if bg {
		s.Background++
	}
	b.saveLocked()
	sleep := b.Sleep
	b.mu.Unlock()
	if wait := slot.Sub(now); wait > 0 {
		return sleep(ctx, wait)
	}
	return nil
}

func (b *budget) After(q Quota) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if q.Limit >= 0 {
		b.state.Limit = q.Limit
	}
	if q.Available >= 0 {
		b.state.Available = q.Available
		b.state.Strikes = 0
	}
	if !q.Reset.IsZero() {
		b.state.Reset = q.Reset
	} else if q.Available >= 0 && b.state.Reset.IsZero() {
		b.state.Reset = b.Now().Add(unknownResetWait)
	}
	b.saveLocked()
}

// Limited holds every call until until, or for a growing wait when the source didn't say.
func (b *budget) Limited(until time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := b.Now()
	if !until.After(now) {
		b.state.Strikes++
		wait := firstBackoff
		for i := 1; i < b.state.Strikes && wait < maxBackoff; i++ {
			wait *= 2
		}
		until = now.Add(min(wait, maxBackoff))
	}
	b.state.Backoff = until
	b.saveLocked()
}

// remaining is what the background job may still use today.
func (b *budget) remaining() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.roll(b.Now())
	return max(0, b.Daily()-b.state.Background)
}

func (b *budget) snapshot() budgetState {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.roll(b.Now())
	return b.state
}

func (b *budget) restore(st budgetState) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.state = st
}

// Rejected holds every call until the key changes.
func (b *budget) Rejected() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.state.BadKey = true
	b.saveLocked()
}

// badKey reports the key having been refused.
func (b *budget) badKey() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state.BadKey
}

// newKey starts afresh: a new key has its own quota, and nothing said about the old one counts.
func (b *budget) newKey() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.state = budgetState{Day: b.state.Day, Limit: -1, Available: -1}
	b.saveLocked()
}

// clearBadKey gives the same key another try, when In Town is turned off and on again; it may
// have been reinstated. The day's counts stand, as it's the same quota.
func (b *budget) clearBadKey() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.state.BadKey = false
	b.saveLocked()
}

func (b *budget) saveLocked() {
	if b.Saved != nil {
		b.Saved(b.state)
	}
}
