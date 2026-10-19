package api

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"marquee/internal/auth"
	"marquee/internal/db"
	"marquee/internal/items"
	"marquee/internal/playback"
	"marquee/internal/settings"
	"marquee/internal/timelimit"
)

type timeLimitFixture struct {
	h          *Handlers
	kid, admin auth.User
	kidCtx     context.Context
	adminCtx   context.Context
	now        time.Time
}

// A kid with 30 minutes a day who watched 30 today (a Wednesday), and an admin whose
// restrictions say the same (ignored: admins are never limited).
func newTimeLimitFixture(t *testing.T) *timeLimitFixture {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	d, err := db.Open(ctx, filepath.Join(dir, "t.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	store, err := settings.Open(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`INSERT INTO libraries(id, name, type, sort_order) VALUES (1, 'Movies', 'movies', 0), (2, 'Music', 'music', 1)`,
		`INSERT INTO items(id, library_id, type, title, sort_title) VALUES (1, 1, 'movie', 'Heat', 'Heat'), (2, 2, 'track', 'Song', 'Song')`,
		`INSERT INTO items(id, library_id, type, title, sort_title, parent_id, extra_type) VALUES (3, 1, 'video', 'Heat trailer', 'Heat trailer', 1, 'trailer')`,
	} {
		if _, err := d.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	a := auth.NewService(d)
	limit := auth.Restrictions{DailyLimitMinutes: ptr(30)}
	kid, err := a.Create(ctx, auth.NewUser{Username: "kid", IsManaged: true, Restrictions: limit})
	if err != nil {
		t.Fatal(err)
	}
	admin, err := a.Create(ctx, auth.NewUser{Username: "admin", Password: "correct horse", IsAdmin: true, Restrictions: limit})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 14, 15, 0, 0, 0, time.Local)
	for _, u := range []int64{kid.ID, admin.ID} {
		d.Exec(`INSERT INTO watch_time(user_id, day, ms) VALUES (?, ?, ?)`, u, timelimit.Day(now), 30*60000)
	}
	h := &Handlers{DB: d, Auth: a, Settings: store, Items: items.NewStore(d),
		Playback:   &playback.Manager{DB: d, Settings: store, TranscodeDir: dir},
		TimeLimits: &timelimit.Tracker{DB: d, Now: func() time.Time { return now }}}
	return &timeLimitFixture{h: h, kid: kid, admin: admin, now: now,
		kidCtx:   auth.WithSession(ctx, auth.Session{User: kid}),
		adminCtx: auth.WithSession(ctx, auth.Session{User: admin})}
}

func TestMyTimeLimit(t *testing.T) {
	f := newTimeLimitFixture(t)
	r, _ := f.h.MyTimeLimit(f.kidCtx, MyTimeLimitRequestObject{})
	st, ok := r.(MyTimeLimit200JSONResponse)
	if !ok {
		t.Fatalf("%#v", r)
	}
	if !st.Limited || st.AllowedNow || *st.LimitMinutes != 30 || *st.UsedMinutes != 30 || *st.MinutesLeft != 0 ||
		*st.Reason != "Time's up for today — back at midnight" || !st.ResetsAt.Equal(time.Date(2026, 10, 15, 0, 0, 0, 0, time.Local)) {
		t.Errorf("kid: %+v", st)
	}
	r, _ = f.h.MyTimeLimit(f.adminCtx, MyTimeLimitRequestObject{})
	if st := r.(MyTimeLimit200JSONResponse); st.Limited || !st.AllowedNow || st.MinutesLeft != nil {
		t.Errorf("admin: %+v", st)
	}
	// Some time left.
	f.h.DB.Exec(`UPDATE watch_time SET ms = 10*60000 WHERE user_id = ?`, f.kid.ID)
	r, _ = f.h.MyTimeLimit(f.kidCtx, MyTimeLimitRequestObject{})
	if st := r.(MyTimeLimit200JSONResponse); !st.AllowedNow || *st.MinutesLeft != 20 || st.Reason != nil || st.ResetsAt != nil {
		t.Errorf("kid with time left: %+v", st)
	}
	if r, _ := f.h.MyTimeLimit(context.Background(), MyTimeLimitRequestObject{}); r == nil {
		t.Error("no answer signed out")
	} else if _, ok := r.(MyTimeLimit401JSONResponse); !ok {
		t.Errorf("signed out: %#v", r)
	}
}

// Video is refused past the limit; music and trailers aren't, and admins never are.
func TestTimeLimitStartRefused(t *testing.T) {
	f := newTimeLimitFixture(t)
	start := func(ctx context.Context, item int64) StartPlaybackResponseObject {
		r, err := f.h.StartPlayback(ctx, StartPlaybackRequestObject{Body: &StartPlaybackJSONRequestBody{ItemId: item}})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	r := start(f.kidCtx, 1)
	if e, ok := r.(StartPlayback403JSONResponse); !ok || e.Code != "time_limit" || e.Message != "Time's up for today — back at midnight" {
		t.Errorf("kid's film: %#v", r)
	}
	// No file in these tests: getting as far as the media lookup (404) means not refused.
	for _, item := range []int64{2, 3} {
		if r := start(f.kidCtx, item); r == nil {
			t.Error("no answer")
		} else if _, ok := r.(StartPlayback404JSONResponse); !ok {
			t.Errorf("kid's item %d: %#v", item, r)
		}
	}
	if _, ok := start(f.adminCtx, 1).(StartPlayback404JSONResponse); !ok {
		t.Error("admin was refused")
	}
	d, _ := f.h.CreateDownload(f.kidCtx, CreateDownloadRequestObject{Body: &CreateDownloadJSONRequestBody{ItemId: 1, Quality: CreateDownloadJSONBodyQualityOriginal}})
	if e, ok := d.(CreateDownload403JSONResponse); !ok || e.Code != "time_limit" {
		t.Errorf("kid's download: %#v", d)
	}
}

// A stream playing when the time runs out is ended, and its progress report says why.
func TestTimeLimitRunningSessionStopped(t *testing.T) {
	f := newTimeLimitFixture(t)
	var stopped []string
	stream := func(key string, user int64) watchStream {
		return watchStream{Activity: timelimit.Activity{Key: key, UserID: user, Playing: true, PositionMS: -1},
			stop: func(reason string) { stopped = append(stopped, key+" "+reason) }}
	}
	kidID := "0123456789abcdef0123456789abcdef"
	streams := []watchStream{stream(playbackKey(kidID), f.kid.ID), stream(playbackKey("admin"), f.admin.ID)}
	f.h.DB.Exec(`UPDATE watch_time SET ms = 29*60000 + 50000 WHERE user_id = ?`, f.kid.ID)
	f.h.sweepTimeLimits(context.Background(), f.h.TimeLimits, streams)
	if len(stopped) != 0 {
		t.Fatalf("stopped with time left: %v", stopped)
	}
	// 10 s later the last seconds are used up.
	f.now = f.now.Add(10 * time.Second)
	f.h.TimeLimits.Now = func() time.Time { return f.now }
	f.h.sweepTimeLimits(context.Background(), f.h.TimeLimits, streams)
	if len(stopped) != 1 || stopped[0] != playbackKey(kidID)+" Time's up for today — back at midnight" {
		t.Fatalf("stopped: %v", stopped)
	}
	r, err := f.h.ReportPlayback(f.kidCtx, ReportPlaybackRequestObject{SessionId: kidID,
		Body: &ReportPlaybackJSONRequestBody{PositionMs: 1000, State: PlaybackProgressStatePlaying}})
	if err != nil {
		t.Fatal(err)
	}
	if e, ok := r.(timeLimitResponse); !ok || e.Code != "time_limit" {
		t.Errorf("report: %#v", r)
	}
	// Another session that just ended normally is still a plain 404.
	r, _ = f.h.ReportPlayback(f.kidCtx, ReportPlaybackRequestObject{SessionId: "ffffffffffffffffffffffffffffffff",
		Body: &ReportPlaybackJSONRequestBody{PositionMs: 1000, State: PlaybackProgressStatePlaying}})
	if _, ok := r.(ReportPlayback404JSONResponse); !ok {
		t.Errorf("unknown session: %#v", r)
	}
}

func TestTimeLimitRestrictionsRoundTrip(t *testing.T) {
	in := &UserRestrictions{DailyLimitMinutes: ptr(90), WeekendLimitMinutes: ptr(0), AllowedFrom: ptr("20:00"), AllowedUntil: ptr("07:30")}
	r, err := fromAPIRestrictions(in)
	if err != nil {
		t.Fatal(err)
	}
	out := toAPIRestrictions(r)
	if *out.DailyLimitMinutes != 90 || *out.WeekendLimitMinutes != 0 || *out.AllowedFrom != "20:00" || *out.AllowedUntil != "07:30" {
		t.Errorf("round trip: %+v", out)
	}
	// Off by default; 0 a day is off too.
	if out := toAPIRestrictions(auth.Restrictions{}); out.DailyLimitMinutes != nil || out.AllowedFrom != nil {
		t.Errorf("default: %+v", out)
	}
	if r, _ := fromAPIRestrictions(&UserRestrictions{DailyLimitMinutes: ptr(0)}); r.DailyLimitMinutes != nil {
		t.Errorf("0 kept: %v", *r.DailyLimitMinutes)
	}
	for _, bad := range []*UserRestrictions{{DailyLimitMinutes: ptr(2000)}, {AllowedFrom: ptr("07:00")}, {AllowedFrom: ptr("7am"), AllowedUntil: ptr("20:00")}} {
		if _, err := fromAPIRestrictions(bad); err == nil {
			t.Errorf("accepted %+v", bad)
		}
	}
}
