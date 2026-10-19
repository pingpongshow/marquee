package timelimit

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"marquee/internal/db"
)

func at(s string) time.Time {
	t, err := time.ParseInLocation("2006-01-02 15:04:05", s, time.Local)
	if err != nil {
		panic(err)
	}
	return t
}

func ip(v int) *int { return &v }

const min = int64(60000)

// 2026-10-14 is a Wednesday; 2026-10-17 a Saturday.
func TestDailyAndWeekend(t *testing.T) {
	l := Limits{Daily: ip(60), Weekend: ip(120)}
	st := Evaluate(l, 45*min, at("2026-10-14 15:00:00"))
	if !st.Limited || !st.AllowedNow || *st.LimitMinutes != 60 || *st.UsedMinutes != 45 || *st.MinutesLeft != 15 || st.Reason != "" {
		t.Errorf("weekday, 45 of 60: %+v", st)
	}
	st = Evaluate(l, 60*min, at("2026-10-14 15:00:00"))
	if st.AllowedNow || *st.MinutesLeft != 0 || st.Reason != "Time's up for today — back at midnight" ||
		!st.ResetsAt.Equal(at("2026-10-15 00:00:00")) {
		t.Errorf("weekday, used up: %+v", st)
	}
	st = Evaluate(l, 90*min, at("2026-10-17 15:00:00"))
	if !st.AllowedNow || *st.LimitMinutes != 120 || *st.MinutesLeft != 30 {
		t.Errorf("Saturday, 90 of 120: %+v", st)
	}
	// Weekend unset = the daily limit; weekend 0 = none at weekends.
	if st := Evaluate(Limits{Daily: ip(60)}, 61*min, at("2026-10-18 10:00:00")); st.AllowedNow {
		t.Errorf("Sunday with the daily limit: %+v", st)
	}
	st = Evaluate(Limits{Daily: ip(60), Weekend: ip(0)}, 600*min, at("2026-10-18 10:00:00"))
	if !st.AllowedNow || !st.Limited || st.MinutesLeft != nil {
		t.Errorf("Sunday without a limit: %+v", st)
	}
	// No limits at all.
	if st := Evaluate(Limits{}, 999*min, at("2026-10-14 10:00:00")); st.Limited || !st.AllowedNow {
		t.Errorf("unlimited: %+v", st)
	}
	// Daily 0 = no limit.
	if (Limits{Daily: ip(0)}).Active() {
		t.Error("daily 0 counted as a limit")
	}
}

func TestViewingHours(t *testing.T) {
	l := Limits{From: "07:00", Until: "20:00"}
	if st := Evaluate(l, 0, at("2026-10-14 12:00:00")); !st.AllowedNow || st.From != "07:00" || st.Until != "20:00" {
		t.Errorf("midday: %+v", st)
	}
	st := Evaluate(l, 0, at("2026-10-14 20:00:00"))
	if st.AllowedNow || st.Reason != "Viewing hours are 07:00–20:00" || !st.ResetsAt.Equal(at("2026-10-15 07:00:00")) {
		t.Errorf("at the end: %+v", st)
	}
	st = Evaluate(l, 0, at("2026-10-14 06:30:00"))
	if st.AllowedNow || !st.ResetsAt.Equal(at("2026-10-14 07:00:00")) {
		t.Errorf("early: %+v", st)
	}

	// Across midnight: 18:00–01:30.
	night := Limits{From: "18:00", Until: "01:30"}
	for _, c := range []struct {
		at string
		ok bool
	}{{"2026-10-14 17:59:00", false}, {"2026-10-14 18:00:00", true}, {"2026-10-14 23:59:00", true},
		{"2026-10-15 00:30:00", true}, {"2026-10-15 01:30:00", false}, {"2026-10-15 12:00:00", false}} {
		if st := Evaluate(night, 0, at(c.at)); st.AllowedNow != c.ok {
			t.Errorf("%s: allowed %v, want %v", c.at, st.AllowedNow, c.ok)
		}
	}
	if st := Evaluate(night, 0, at("2026-10-15 02:00:00")); !st.ResetsAt.Equal(at("2026-10-15 18:00:00")) {
		t.Errorf("resets: %v", st.ResetsAt)
	}

	// Both: used up during the window comes back when the window opens next day.
	both := Limits{Daily: ip(30), From: "07:00", Until: "20:00"}
	st = Evaluate(both, 30*min, at("2026-10-14 10:00:00"))
	if st.AllowedNow || st.Reason != "Time's up for today — back at 07:00" || !st.ResetsAt.Equal(at("2026-10-15 07:00:00")) {
		t.Errorf("used up with hours: %+v", st)
	}
	// Minutes left end with the viewing hours.
	if st := Evaluate(both, 0, at("2026-10-14 19:50:00")); *st.MinutesLeft != 10 {
		t.Errorf("left before the end of the hours: %+v", *st.MinutesLeft)
	}
}

func TestValidate(t *testing.T) {
	for _, l := range []Limits{{Daily: ip(-1)}, {Weekend: ip(1441)}, {From: "07:00"}, {From: "7:00", Until: "20:00"},
		{From: "07:00", Until: "24:00"}, {From: "07:00", Until: "07:00"}} {
		if l.Validate() == nil {
			t.Errorf("accepted %+v", l)
		}
	}
	for _, l := range []Limits{{}, {Daily: ip(90), Weekend: ip(0)}, {From: "20:00", Until: "07:00"}} {
		if err := l.Validate(); err != nil {
			t.Errorf("refused %+v: %v", l, err)
		}
	}
}

func testTracker(t *testing.T) *Tracker {
	t.Helper()
	dir := t.TempDir()
	d, err := db.Open(context.Background(), filepath.Join(dir, "t.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	if _, err := d.Exec(`INSERT INTO users(id, username, display_name) VALUES (2, 'kid', 'Kid'), (3, 'other', 'Other')`); err != nil {
		t.Fatal(err)
	}
	return &Tracker{DB: d}
}

// Real time playing counts; seeking, pausing, a stalled player and a missed sweep don't add.
func TestUsageCounting(t *testing.T) {
	ctx := context.Background()
	tr := testTracker(t)
	t0 := at("2026-10-14 15:00:00")
	sweep := func(sec int, acts ...Activity) { tr.Sweep(ctx, acts, t0.Add(time.Duration(sec)*time.Second)) }
	a := func(pos int64, playing bool) Activity {
		return Activity{Key: "pb:1", UserID: 2, Playing: playing, PositionMS: pos}
	}
	sweep(0, a(0, true))          // first seen: nothing yet
	sweep(10, a(10_000, true))    // +10 s
	sweep(20, a(1_800_000, true)) // seeked 30 minutes ahead: still only +10 s
	sweep(30, a(1_810_000, false))
	sweep(40, a(1_810_000, false)) // paused: nothing
	if got := tr.UsedMS(ctx, 2, t0.Add(40*time.Second)); got != 20_000 {
		t.Fatalf("used %d ms, want 20000", got)
	}
	// A player that says it plays but whose position stands still stops counting.
	for s := 50; s <= 120; s += 10 {
		sweep(s, a(1_820_000, true))
	}
	got := tr.UsedMS(ctx, 2, t0.Add(120*time.Second))
	if got < 40_000 || got > 60_000 {
		t.Fatalf("stalled player: used %d ms", got)
	}
	// A missed sweep is capped (MaxStep 30 s), and live streams (no position) count by time.
	before := tr.UsedMS(ctx, 3, t0)
	live := Activity{Key: "live:x:3", UserID: 3, Playing: true, PositionMS: -1}
	sweep(0, live)
	sweep(300, live)
	if got := tr.UsedMS(ctx, 3, t0.Add(300*time.Second)) - before; got != 30_000 {
		t.Fatalf("capped step: %d", got)
	}
	// Gone streams are forgotten (a stream seen again starts fresh).
	sweep(310)
	sweep(320, live)
	if got := tr.UsedMS(ctx, 3, t0.Add(320*time.Second)) - before; got != 30_000 {
		t.Fatalf("after coming back: %d", got)
	}
}

// Playing across midnight counts on each day; the in-flight part since the last sweep is
// included.
func TestUsageMidnightAndActive(t *testing.T) {
	ctx := context.Background()
	tr := testTracker(t)
	live := Activity{Key: "live:y:2", UserID: 2, Playing: true, PositionMS: -1}
	tr.Sweep(ctx, []Activity{live}, at("2026-10-14 23:59:50"))
	tr.Sweep(ctx, []Activity{live}, at("2026-10-15 00:00:10"))
	var d1, d2 int64
	tr.DB.QueryRow(`SELECT ms FROM watch_time WHERE user_id = 2 AND day = '2026-10-14'`).Scan(&d1)
	tr.DB.QueryRow(`SELECT ms FROM watch_time WHERE user_id = 2 AND day = '2026-10-15'`).Scan(&d2)
	if d1 != 10_000 || d2 != 10_000 {
		t.Fatalf("split: %d / %d", d1, d2)
	}
	if got := tr.UsedMS(ctx, 2, at("2026-10-15 00:00:15")); got != 15_000 {
		t.Fatalf("with the playing stream: %d", got)
	}
	tr.Prune(ctx, at("2026-10-15 12:00:00"), 12*time.Hour)
	tr.DB.QueryRow(`SELECT COUNT(*) FROM watch_time`).Scan(&d1)
	if d1 != 1 {
		t.Fatalf("pruned to %d days", d1)
	}
}

// A stream ended by a time limit keeps the time it played since the last sweep.
func TestMarkStoppedCountsTheRest(t *testing.T) {
	ctx := context.Background()
	tr := testTracker(t)
	now := at("2026-10-14 15:00:00")
	tr.Now = func() time.Time { return now }
	a := Activity{Key: "pb:z", UserID: 2, Playing: true, PositionMS: 0}
	tr.Sweep(ctx, []Activity{a}, now)
	now = now.Add(7 * time.Second)
	tr.MarkStopped(ctx, "pb:z", "Time's up")
	if got := tr.UsedMS(ctx, 2, now); got != 7000 {
		t.Fatalf("used %d", got)
	}
	if tr.StoppedReason("pb:z") != "Time's up" || tr.StoppedReason("pb:y") != "" {
		t.Fatal("stopped reasons")
	}
}
