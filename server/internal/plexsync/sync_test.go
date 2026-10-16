package plexsync

import (
	"bytes"
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"marquee/internal/db"
	"marquee/internal/notify"
	"marquee/internal/settings"
)

type alertLog struct{ got []notify.Alert }

func (a *alertLog) Raise(_ context.Context, al notify.Alert) bool {
	a.got = append(a.got, al)
	return true
}

type world struct {
	t      *testing.T
	ctx    context.Context
	db     *sql.DB
	store  *settings.Store
	plex   *fakePlex
	svc    *Service
	alerts *alertLog
}

// Marquee items: movies A–E (10–14), a track (20). Plex keys: 101–105, 201.
func newWorld(t *testing.T) *world {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	d, err := db.Open(ctx, filepath.Join(dir, "t.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := d.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	exec(`INSERT INTO users (id, username, display_name, is_admin) VALUES (1, 'admin', 'Admin', 1), (2, 'kid', 'Kid', 0), (3, 'friend', 'Friend', 0)`)
	exec(`INSERT INTO libraries (id, name, type) VALUES (1, 'Movies', 'movies'), (2, 'Music', 'music')`)
	f := newFakePlex()
	t.Cleanup(f.srv.Close)
	for i, name := range []string{"A", "B", "C", "D", "E"} {
		id := 10 + i
		exec(`INSERT INTO items (id, library_id, type, title, sort_title, duration_ms) VALUES (?, 1, 'movie', ?, ?, 7200000)`, id, name, name)
		exec(`INSERT INTO media_versions (id, item_id) VALUES (?, ?)`, id, id)
		exec(`INSERT INTO media_files (version_id, library_id, path, size, mtime) VALUES (?, 1, ?, 1, 1)`, id, "/media/movies/"+name+"/"+name+".mkv")
		f.addItem(fakeItem{key: "10" + string(rune('1'+i)), section: "1", typ: "movie", title: name, file: "/data/movies/" + name + "/" + name + ".mkv", duration: 7200000})
	}
	exec(`INSERT INTO items (id, library_id, type, title, sort_title) VALUES (19, 2, 'artist', 'Band', 'Band')`)
	exec(`INSERT INTO items (id, library_id, type, title, sort_title, parent_id, duration_ms) VALUES (20, 2, 'track', 'Song', 'Song', 19, 200000)`)
	exec(`INSERT INTO media_versions (id, item_id) VALUES (20, 20)`)
	exec(`INSERT INTO media_files (version_id, library_id, path, size, mtime) VALUES (20, 2, '/media/music/Band/Song.flac', 1, 1)`)
	f.addItem(fakeItem{key: "201", section: "2", typ: "track", title: "Song", grandparent: "Band", file: "/data/music/Band/Song.flac", duration: 200000})
	// A Plex movie Marquee doesn't have.
	f.addItem(fakeItem{key: "199", section: "1", typ: "movie", title: "Elsewhere", file: "/data/movies/Elsewhere.mkv", duration: 1})

	store, err := settings.Open(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Update(ctx, func(s *settings.Settings) error {
		s.PlexSync.URL, s.PlexSync.Token = f.srv.URL, ownerTok
		s.PlexSync.PathMappings = []settings.PlexPathMapping{{Plex: "/data/movies", Marquee: "/media/movies"}, {Plex: "/data/music", Marquee: "/media/music"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// As the Plex import leaves it: owner → admin, Kid → kid, Friend → friend.
	exec(`INSERT INTO plex_sync_users (plex_account_id, name, user_id) VALUES (1, 'Owner', 1), (2, 'Kid', 2), (3, 'Friend', 3)`)
	al := &alertLog{}
	svc := &Service{DB: d, Settings: store, Alerts: al, PlexTV: f.srv.URL + "/tv", RateEvery: time.Microsecond, Backoff: time.Millisecond, PageSize: 2}
	return &world{t: t, ctx: ctx, db: d, store: store, plex: f, svc: svc, alerts: al}
}

func (w *world) exec(q string, args ...any) {
	w.t.Helper()
	if _, err := w.db.Exec(q, args...); err != nil {
		w.t.Fatalf("%s: %v", q, err)
	}
}

func (w *world) int(q string, args ...any) (n int64) {
	w.t.Helper()
	if err := w.db.QueryRow(q, args...).Scan(&n); err != nil {
		w.t.Fatalf("%s: %v", q, err)
	}
	return
}

func (w *world) run(dry bool) *Report {
	w.t.Helper()
	rep, err := w.svc.Run(w.ctx, dry)
	if err != nil {
		w.t.Fatal(err)
	}
	return rep
}

func f64(v float64) *float64 { return &v }

func hasCall(calls []string, c string) bool { return slices.Contains(calls, c) }

func TestSyncBothWays(t *testing.T) {
	w := newWorld(t)
	f := w.plex
	t0 := time.Now().Add(-48 * time.Hour).Unix()
	// Plex, owner: A played twice, B half way, the song rated 8, two plays in history.
	*f.st(1, "101") = fakeState{viewCount: 2, lastViewedAt: t0}
	*f.st(1, "102") = fakeState{viewOffset: 600000, lastViewedAt: t0}
	*f.st(1, "201") = fakeState{rating: f64(8), lastRatedAt: t0}
	f.history = []fakePlay{{1, "101", t0 - 7200}, {1, "101", t0}}
	// Marquee: the admin watched C, rated A 6 and is part way into E; the kid watched A.
	w.exec(`INSERT INTO user_item_state (user_id, item_id, play_count, last_viewed_at) VALUES (1, 12, 1, '2026-10-01T10:00:00.000Z')`)
	w.exec(`INSERT INTO user_item_state (user_id, item_id, rating) VALUES (1, 10, 6)`)
	w.exec(`INSERT INTO user_item_state (user_id, item_id, view_offset_ms, last_viewed_at) VALUES (1, 14, 300000, '2026-10-01T10:00:00.000Z')`)
	w.exec(`INSERT INTO user_item_state (user_id, item_id, play_count, last_viewed_at) VALUES (2, 10, 1, '2026-10-01T10:00:00.000Z')`)

	// ---- Preview: lists both ways, changes nothing ----
	pv := w.run(true)
	if !pv.DryRun || pv.ToPlex != 4 || pv.ToMarquee != 3 || pv.HistoryPulled != 2 || pv.NotInMarquee != 1 {
		t.Fatalf("preview: %s %+v", pv.Summary(), pv.Changes)
	}
	if len(f.callLog()) != 0 {
		t.Fatalf("preview wrote to Plex: %v", f.callLog())
	}
	if n := w.int(`SELECT COUNT(*) FROM plex_sync_ledger`) + w.int(`SELECT COUNT(*) FROM play_history`) +
		w.int(`SELECT COALESCE(SUM(play_count), 0) FROM user_item_state WHERE user_id = 1 AND item_id = 10`); n != 0 {
		t.Fatalf("preview changed Marquee (%d)", n)
	}
	if w.svc.LastReport(w.ctx) != nil {
		t.Error("a preview was saved as the last run")
	}
	users, _ := w.svc.Users(w.ctx)
	for _, u := range users {
		switch u.PlexAccountID {
		case 1:
			if u.Kind != KindOwner || !u.CanSync {
				t.Errorf("owner: %+v", u)
			}
		case 2:
			if u.Kind != KindManaged || !u.CanSync {
				t.Errorf("managed user: %+v", u)
			}
		case 3:
			if u.Kind != KindShared || u.CanSync || !strings.Contains(u.Reason, "shared user") {
				t.Errorf("friend: %+v", u)
			}
		}
	}

	// ---- Sync ----
	rep := w.run(false)
	if rep.ToPlex != 4 || rep.ToMarquee != 3 || rep.HistoryPulled != 2 || rep.Failed != 0 || rep.UsersSynced != 2 {
		t.Fatalf("sync: %s %+v %v", rep.Summary(), rep.Changes, rep.Errors)
	}
	calls := f.callLog()
	for _, c := range []string{"scrobble acct=1 key=103", "rate acct=1 key=101 rating=6", "timeline acct=1 key=105 time=300000 duration=7200000",
		"scrobble acct=2 key=101"} { // the managed user, with the token from plex.tv's user switch
		if !hasCall(calls, c) {
			t.Errorf("missing Plex call %q in %v", c, calls)
		}
	}
	var count, offset int64
	var last string
	w.db.QueryRow(`SELECT play_count, last_viewed_at FROM user_item_state WHERE user_id = 1 AND item_id = 10`).Scan(&count, &last)
	if count != 2 || last != time.Unix(t0, 0).UTC().Format(isoMS) {
		t.Errorf("A pulled: %d plays, last %s", count, last)
	}
	w.db.QueryRow(`SELECT view_offset_ms FROM user_item_state WHERE user_id = 1 AND item_id = 11`).Scan(&offset)
	if offset != 600000 {
		t.Errorf("B position pulled: %d", offset)
	}
	var rating float64
	w.db.QueryRow(`SELECT rating FROM user_item_state WHERE user_id = 1 AND item_id = 20`).Scan(&rating)
	if rating != 8 {
		t.Errorf("song rating pulled (Plex 0–10 = Marquee 0–10): %v", rating)
	}
	// Plex's history: two old plays pulled; the plays pushed just now (C, the kid's A) skipped.
	if n := w.int(`SELECT COUNT(*) FROM play_history WHERE source = 'plex' AND user_id = 1 AND item_id = 10`); n != 2 || w.int(`SELECT COUNT(*) FROM play_history`) != 2 {
		t.Errorf("history: %d for A, %d in all", n, w.int(`SELECT COUNT(*) FROM play_history`))
	}
	if last := w.svc.LastReport(w.ctx); last == nil || last.ToPlex != 4 {
		t.Errorf("last report: %+v", last)
	}

	// ---- Again: nothing changed, nothing echoes ----
	before := len(f.callLog())
	rep = w.run(false)
	if rep.ToPlex != 0 || rep.ToMarquee != 0 || rep.HistoryPulled != 0 || rep.Conflicts != 0 || len(f.callLog()) != before {
		t.Fatalf("second run should be quiet: %s %+v, calls %v", rep.Summary(), rep.Changes, f.callLog()[before:])
	}
	if rep.Unchanged == 0 {
		t.Error("unchanged items not counted")
	}

	// ---- Newest wins, both ways ----
	f.mu.Lock()
	now := time.Now().Unix()
	f.st(1, "101").viewCount = 0                                       // A marked unwatched in Plex
	*f.st(1, "102") = fakeState{viewOffset: 900000, lastViewedAt: now} // B moved on in Plex just now…
	f.mu.Unlock()
	// …and in Marquee an hour ago: Plex's is newer.
	w.exec(`UPDATE user_item_state SET view_offset_ms = 1200000, view_offset_changed_at = ? WHERE user_id = 1 AND item_id = 11`,
		time.Now().Add(-time.Hour).UTC().Format(isoMS))
	w.exec(`UPDATE user_item_state SET rating = 9 WHERE user_id = 1 AND item_id = 20`) // re-rated in Marquee (trigger dates it)
	before = len(f.callLog())
	rep = w.run(false)
	calls = f.callLog()[before:]
	if !slices.Equal(calls, []string{"rate acct=1 key=201 rating=9"}) {
		t.Errorf("pushes: %v", calls)
	}
	w.db.QueryRow(`SELECT play_count FROM user_item_state WHERE user_id = 1 AND item_id = 10`).Scan(&count)
	w.db.QueryRow(`SELECT view_offset_ms FROM user_item_state WHERE user_id = 1 AND item_id = 11`).Scan(&offset)
	if count != 0 || offset != 900000 || rep.Conflicts != 1 {
		t.Errorf("pulled unwatched A (%d plays) and B's newer Plex position (%d); conflicts %d", count, offset, rep.Conflicts)
	}

	// ---- Both sides changed: the newer wins; a tie goes to Marquee ----
	f.mu.Lock()
	*f.st(1, "104") = fakeState{rating: f64(4), lastRatedAt: now - 60}
	f.mu.Unlock()
	w.exec(`INSERT INTO user_item_state (user_id, item_id, rating, rating_changed_at) VALUES (1, 13, 7, ?)`, time.Unix(now-60, 0).UTC().Format(isoMS))
	w.run(false) // first sync of D's rating: both rated, equal times → Marquee
	if f.st(1, "104").rating == nil || *f.st(1, "104").rating != 7 {
		t.Errorf("tie should go to Marquee: Plex has %v", f.st(1, "104").rating)
	}
	f.mu.Lock()
	*f.st(1, "104") = fakeState{rating: f64(3), lastRatedAt: now + 5}
	f.mu.Unlock()
	w.exec(`UPDATE user_item_state SET rating = 5, rating_changed_at = ? WHERE user_id = 1 AND item_id = 13`, time.Unix(now, 0).UTC().Format(isoMS))
	rep = w.run(false)
	w.db.QueryRow(`SELECT rating FROM user_item_state WHERE user_id = 1 AND item_id = 13`).Scan(&rating)
	if rating != 3 || rep.Conflicts != 1 {
		t.Errorf("newer Plex rating should win: %v, conflicts %d", rating, rep.Conflicts)
	}

	// A play in Marquee after an unwatch in Plex: Marquee's play is newer.
	f.mu.Lock()
	f.st(1, "103").viewCount = 0
	f.mu.Unlock()
	w.exec(`UPDATE user_item_state SET play_count = play_count + 1 WHERE user_id = 1 AND item_id = 12`)
	before = len(f.callLog())
	w.run(false)
	if c := f.callLog()[before:]; !slices.Equal(c, []string{"scrobble acct=1 key=103", "scrobble acct=1 key=103"}) {
		t.Errorf("Marquee's newer plays should go back to Plex: %v", c)
	}
}

func TestUnreachableWaits(t *testing.T) {
	w := newWorld(t)
	w.exec(`INSERT INTO user_item_state (user_id, item_id, play_count) VALUES (1, 12, 1)`)
	w.plex.srv.Close()
	for i := 0; i < 3; i++ {
		rep, err := w.svc.Run(w.ctx, false)
		if err != nil || !rep.Waiting || rep.Failed != 0 || rep.ToPlex != 0 {
			t.Fatalf("run %d: %v %+v", i, err, rep)
		}
	}
	if w.int(`SELECT COUNT(*) FROM plex_sync_ledger`) != 0 {
		t.Error("ledger moved while Plex was away")
	}
	if len(w.alerts.got) != 1 || w.alerts.got[0].Type != settings.AlertPlexSync {
		t.Errorf("alert after three unreachable runs: %+v", w.alerts.got)
	}
	if r := w.svc.Reachable(w.ctx); r == nil || *r {
		t.Errorf("reachable: %v", r)
	}
}

func TestTokenRedaction(t *testing.T) {
	w := newWorld(t)
	var logs bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	defer slog.SetDefault(old)

	w.exec(`INSERT INTO user_item_state (user_id, item_id, play_count) VALUES (1, 12, 1)`)
	w.plex.fail["/:/scrobble"] = 500
	rep := w.run(false)
	if rep.Failed != 1 || len(rep.Errors) != 1 || rep.ToPlex != 1 {
		t.Fatalf("a failing item should be counted and the run carry on: %+v", rep)
	}
	// The item isn't marked synced, so it's tried again.
	if w.int(`SELECT watch_synced FROM plex_sync_ledger WHERE user_id = 1 AND rating_key = '103'`) != 0 {
		t.Error("failed push recorded as synced")
	}
	delete(w.plex.fail, "/:/scrobble")
	if rep := w.run(false); rep.ToPlex != 1 || rep.Failed != 0 {
		t.Errorf("retry: %+v", rep)
	}

	// A rejected token fails the run, without the token in the message.
	w.store.Update(w.ctx, func(s *settings.Settings) error { s.PlexSync.Token = "wrong-secret-token"; return nil })
	_, err := w.svc.Run(w.ctx, false)
	if err == nil || !strings.Contains(err.Error(), "rejected") || strings.Contains(err.Error(), "wrong-secret-token") {
		t.Errorf("bad token: %v", err)
	}
	if w.svc.LastError(w.ctx) == "" || len(w.alerts.got) == 0 {
		t.Error("a failing sync should be recorded and alerted")
	}
	for _, s := range append(rep.Errors, logs.String(), w.svc.LastError(w.ctx)) {
		if strings.Contains(s, ownerTok) || strings.Contains(s, kidSrvTok) || strings.Contains(s, "wrong-secret-token") {
			t.Errorf("token leaked: %s", s)
		}
	}
	sec := &secrets{}
	sec.add("abc123secret")
	if got := sec.redact(`GET http://plex:32400/x?X-Plex-Token=zzz&a=1: abc123secret {"authToken":"qqq"}`); strings.Contains(got, "zzz") ||
		strings.Contains(got, "abc123secret") || strings.Contains(got, "qqq") {
		t.Errorf("redact: %s", got)
	}
}

func TestAutomaticSyncOffByDefault(t *testing.T) {
	if settings.Defaults().PlexSync.Enabled || settings.Defaults().PlexSync.IntervalMinutes != 30 {
		t.Fatalf("defaults: %+v", settings.Defaults().PlexSync)
	}
	w := newWorld(t)
	task := w.svc.Task()
	if task.EveryFunc() != 0 {
		t.Error("automatic sync should be off by default")
	}
	w.store.Update(w.ctx, func(s *settings.Settings) error {
		s.PlexSync.Enabled, s.PlexSync.IntervalMinutes = true, 45
		return nil
	})
	if task.EveryFunc() != 45*time.Minute {
		t.Errorf("interval: %v", task.EveryFunc())
	}
	if _, err := w.store.Update(w.ctx, func(s *settings.Settings) error { s.PlexSync.IntervalMinutes = 2; return nil }); err == nil {
		t.Error("an interval under 5 minutes should be refused")
	}
	// Manual runs work with automatic sync off.
	w.store.Update(w.ctx, func(s *settings.Settings) error { s.PlexSync.Enabled = false; return nil })
	if msg, err := task.Run(w.ctx); err != nil || !strings.HasPrefix(msg, "Pushed") {
		t.Errorf("manual run: %q %v", msg, err)
	}
}

func TestPinSignIn(t *testing.T) {
	w := newWorld(t)
	p, err := w.svc.StartPin(w.ctx)
	if err != nil || p.ID != 77 || !strings.Contains(p.AuthURL, "code=ABCD") {
		t.Fatalf("pin: %+v %v", p, err)
	}
	ok, err := w.svc.CheckPin(w.ctx, p.ID)
	if !ok || err != nil || w.store.Get().PlexSync.Token != "pin-token-xyz" {
		t.Errorf("check: %v %v", ok, err)
	}
}

func TestDecide(t *testing.T) {
	cfg := settings.Defaults().PlexSync
	now := time.Now().UTC()
	lastRun := now.Add(-30 * time.Minute)
	movie := Metadata{Type: "movie", Duration: 6_000_000}

	// Rating scale: Plex 0–10 is Marquee's 0–10; Plex has no 0.
	for _, c := range []struct {
		in   *float64
		want *float64
	}{{nil, nil}, {f64(0), nil}, {f64(7.5), f64(7.5)}, {f64(10), f64(10)}, {f64(12), f64(10)}} {
		if got := plexRating(c.in); !sameRating(got, c.want) {
			t.Errorf("plexRating(%v) = %v", c.in, got)
		}
	}

	// Unwatched in Marquee after Plex's last play wins over that play.
	synced := ledgerRow{watchSynced: true, mCount: 2, pCount: 2}
	p := movie
	p.ViewCount, p.LastViewedAt = 3, now.Add(-2*time.Hour).Unix()
	pl := decide(cfg, mstate{count: 0, countAt: now.Add(-time.Hour)}, p, synced, lastRun, now)
	if !pl.unscrobble || pl.setCount != nil || pl.conflicts != 1 {
		t.Errorf("newer unwatch should win: %+v", pl)
	}
	// …but an older unwatch loses to a newer play: count = max.
	pl = decide(cfg, mstate{count: 0, countAt: now.Add(-3 * time.Hour)}, p, synced, lastRun, now)
	if pl.unscrobble || pl.setCount == nil || *pl.setCount != 3 {
		t.Errorf("newer play should win: %+v", pl)
	}
	// Only Marquee changed: more plays go to Plex as scrobbles.
	p.ViewCount = 2
	pl = decide(cfg, mstate{count: 4, countAt: now}, p, synced, lastRun, now)
	if pl.scrobbles != 2 || pl.setCount != nil {
		t.Errorf("plays to Plex: %+v", pl)
	}
	// Positions near the end aren't sent (Plex would mark it played).
	pl = decide(cfg, mstate{offset: 5_900_000, offsetAt: now}, movie, ledgerRow{offsetSynced: true}, lastRun, now)
	if pl.pushOffset != nil || pl.skipped != 1 {
		t.Errorf("near the end: %+v", pl)
	}
	// Music has no resume position.
	pl = decide(cfg, mstate{offset: 60_000, offsetAt: now}, Metadata{Type: "track", Duration: 200_000}, ledgerRow{}, lastRun, now)
	if pl.pushOffset != nil || pl.offsetKind {
		t.Errorf("track position: %+v", pl)
	}
	// Turned off kinds aren't touched.
	off := cfg
	off.Ratings = false
	if pl := decide(off, mstate{rating: f64(5), ratingAt: now}, movie, ledgerRow{}, lastRun, now); pl.pushRating || pl.ratingKind {
		t.Errorf("ratings off: %+v", pl)
	}
}

func TestRateLimitAndBackoff(t *testing.T) {
	var hits []time.Time
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits = append(hits, time.Now())
		if len(hits) <= 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Write([]byte(`{"MediaContainer":{"machineIdentifier":"m"}}`))
	}))
	defer srv.Close()
	c := &client{base: srv.URL, http: srv.Client(), lim: &limiter{every: 20 * time.Millisecond}, backoff: 5 * time.Millisecond, retries: 3, secrets: &secrets{}}
	id, err := c.identity(context.Background(), "tok")
	if err != nil || id != "m" || len(hits) != 3 {
		t.Fatalf("retry after 429: %q %v (%d requests)", id, err, len(hits))
	}
	for i := 1; i < len(hits); i++ {
		if gap := hits[i].Sub(hits[i-1]); gap < 15*time.Millisecond {
			t.Errorf("requests %v apart; the limit is one per 20 ms", gap)
		}
	}
}
