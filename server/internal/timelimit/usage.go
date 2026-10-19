package timelimit

import (
	"context"
	"database/sql"
	"sync"
	"time"
)

// Activity is one video stream as a sweep sees it.
type Activity struct {
	Key    string // unique per stream, e.g. "pb:<session id>"
	UserID int64
	// Playing: the stream is playing (not paused, still fetched or reported).
	Playing bool
	// PositionMS is the reported position, or -1 when the stream has none (Live TV). A
	// stream whose position hasn't moved for StallAfter isn't counted (a paused player
	// that never said so).
	PositionMS int64
}

// StallAfter is how long a playing stream's position may stand still before it stops
// counting (players report about every 10 s).
const StallAfter = 35 * time.Second

// Tracker counts the minutes of video each person watches per day. Sweep is called
// regularly with the streams playing; each stream seen playing again is credited the real
// time since the last sweep (capped), so seeking adds nothing.
type Tracker struct {
	DB *sql.DB
	// MaxStep caps what one sweep credits a stream (a missed sweep doesn't count double).
	MaxStep time.Duration
	// Now is the clock (tests); time.Now when nil.
	Now func() time.Time

	mu   sync.Mutex
	seen map[string]*seenStream
	// stopped remembers streams ended by a time limit, so their players hear why.
	stopped map[string]stopped
}

type stopped struct {
	reason string
	at     time.Time
}

// Clock is the tracker's current time.
func (t *Tracker) Clock() time.Time {
	if t.Now != nil {
		return t.Now()
	}
	return time.Now()
}

// MarkStopped records that the stream key was ended by a time limit, with the reason, and
// counts its playing time since the last sweep (it won't be seen again).
func (t *Tracker) MarkStopped(ctx context.Context, key, reason string) {
	t.mu.Lock()
	if t.stopped == nil {
		t.stopped = map[string]stopped{}
	}
	now := time.Now()
	for k, v := range t.stopped {
		if now.Sub(v.at) > 30*time.Minute {
			delete(t.stopped, k)
		}
	}
	t.stopped[key] = stopped{reason, now}
	s := t.seen[key]
	delete(t.seen, key)
	t.mu.Unlock()
	if s != nil && s.playing {
		clock := t.Clock()
		if clock.After(s.at) {
			from := s.at
			if clock.Sub(from) > t.maxStep() {
				from = clock.Add(-t.maxStep())
			}
			t.add(ctx, s.userID, from, clock)
		}
	}
}

// StoppedReason is why a time limit ended the stream key ("" when it didn't).
func (t *Tracker) StoppedReason(key string) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.stopped[key].reason
}

type seenStream struct {
	userID   int64
	at       time.Time // last sweep that saw it
	playing  bool
	pos      int64
	posSince time.Time // when pos last changed
}

func (t *Tracker) maxStep() time.Duration {
	if t.MaxStep > 0 {
		return t.MaxStep
	}
	return 30 * time.Second
}

// Day is the server-local date key for t.
func Day(t time.Time) string { return t.Format("2006-01-02") }

// Sweep credits the streams that played since the last sweep and forgets the ones gone.
func (t *Tracker) Sweep(ctx context.Context, acts []Activity, now time.Time) {
	t.mu.Lock()
	if t.seen == nil {
		t.seen = map[string]*seenStream{}
	}
	type credit struct {
		user     int64
		from, to time.Time
	}
	var credits []credit
	present := map[string]bool{}
	for _, a := range acts {
		present[a.Key] = true
		s := t.seen[a.Key]
		if s == nil {
			t.seen[a.Key] = &seenStream{userID: a.UserID, at: now, playing: a.Playing, pos: a.PositionMS, posSince: now}
			continue
		}
		if a.PositionMS != s.pos {
			s.pos, s.posSince = a.PositionMS, now
		}
		stalled := a.PositionMS >= 0 && now.Sub(s.posSince) > StallAfter
		if a.Playing && !stalled && now.After(s.at) {
			from := s.at
			if now.Sub(from) > t.maxStep() {
				from = now.Add(-t.maxStep())
			}
			credits = append(credits, credit{a.UserID, from, now})
		}
		s.at, s.playing, s.userID = now, a.Playing && !stalled, a.UserID
	}
	for k := range t.seen {
		if !present[k] {
			delete(t.seen, k)
		}
	}
	t.mu.Unlock()
	for _, c := range credits {
		t.add(ctx, c.user, c.from, c.to)
	}
}

// add records watching from..to, split at midnight.
func (t *Tracker) add(ctx context.Context, userID int64, from, to time.Time) {
	for from.Before(to) {
		end := to
		if nm := nextMidnight(from); nm.Before(end) {
			end = nm
		}
		if ms := end.Sub(from).Milliseconds(); ms > 0 && t.DB != nil {
			t.DB.ExecContext(ctx, `INSERT INTO watch_time(user_id, day, ms) VALUES (?, ?, ?)
				ON CONFLICT(user_id, day) DO UPDATE SET ms = ms + excluded.ms`, userID, Day(from), ms)
		}
		from = end
	}
}

// UsedMS is how much video userID watched on now's day, including streams playing now
// (up to the last sweep's cap).
func (t *Tracker) UsedMS(ctx context.Context, userID int64, now time.Time) int64 {
	var ms int64
	if t.DB != nil {
		t.DB.QueryRowContext(ctx, `SELECT ms FROM watch_time WHERE user_id = ? AND day = ?`, userID, Day(now)).Scan(&ms)
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	for _, s := range t.seen {
		if s.userID != userID || !s.playing || !now.After(s.at) {
			continue
		}
		from := s.at
		if now.Sub(from) > t.maxStep() {
			from = now.Add(-t.maxStep())
		}
		if m := midnight(now); from.Before(m) {
			from = m
		}
		ms += now.Sub(from).Milliseconds()
	}
	return ms
}

// Prune drops days older than keep.
func (t *Tracker) Prune(ctx context.Context, now time.Time, keep time.Duration) {
	if t.DB != nil {
		t.DB.ExecContext(ctx, `DELETE FROM watch_time WHERE day < ?`, Day(now.Add(-keep)))
	}
}
