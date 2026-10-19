package vodsave

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"marquee/internal/settings"
)

// followEnv is a series of n season-1 episodes (ids 3001…) on the source, following turned
// on, and a second (non-admin) user who follows.
type followEnv struct {
	*env
	seriesID int64
	user     int64
	alerts   []string
	usage    map[int64][2]int64 // DVR usage and quota by user
}

func episodesJSON(n int) string {
	var eps []string
	for i := 1; i <= n; i++ {
		eps = append(eps, fmt.Sprintf(`{"id":"%d","episode_num":"%d","title":"Ep %d","container_extension":"mkv","season":"1"}`, 3000+i, i, i))
	}
	return `{"seasons":[],"episodes":{"1":[` + strings.Join(eps, ",") + `]}}`
}

func newFollowEnv(t *testing.T, episodes int, mutate func(*settings.VODSave)) *followEnv {
	t.Helper()
	e := newEnv(t, "[]", func(v *settings.VODSave) {
		v.FollowEnabled = true
		if mutate != nil {
			mutate(v)
		}
	})
	e.src.series = `[{"name":"Kings (2011)","series_id":"201","category_id":"1"}]`
	e.src.info["201"] = episodesJSON(episodes)
	ctx := context.Background()
	e.vod.Refresh(ctx, false)
	f := &followEnv{env: e, usage: map[int64][2]int64{}}
	e.s.DB.QueryRow("SELECT id FROM vod_series").Scan(&f.seriesID)
	if err := e.vod.LoadEpisodes(ctx, f.seriesID, false); err != nil {
		t.Fatal(err)
	}
	r, err := e.s.DB.Exec(`INSERT INTO users (username, display_name, is_admin) VALUES ('kim', 'Kim', 0)`)
	if err != nil {
		t.Fatal(err)
	}
	f.user, _ = r.LastInsertId()
	e.s.DVRUsage = func(_ context.Context, id int64) (int64, int64) { u := f.usage[id]; return u[0], u[1] }
	e.s.OnFollowQuota = func(_ int64, owner, title, msg string) { f.alerts = append(f.alerts, owner+": "+msg) }
	return f
}

// newEpisodes puts n episodes on the source and has the series' episodes fetched again
// when next looked at (as a catalogue refresh does when the series changed).
func (f *followEnv) newEpisodes(n int) {
	f.src.mu.Lock()
	f.src.info["201"] = episodesJSON(n)
	f.src.mu.Unlock()
	f.s.DB.Exec("UPDATE vod_series SET episodes_at=NULL WHERE id=?", f.seriesID)
}

// queued lists the saves the follower has waiting, by the source's episode id.
func (f *followEnv) queued() []string {
	f.t.Helper()
	rows, err := f.s.DB.Query(`SELECT source_key FROM vod_saves WHERE requested_by=? AND state=? ORDER BY source_key`, f.user, Queued)
	if err != nil {
		f.t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var k string
		rows.Scan(&k)
		out = append(out, k)
	}
	return out
}

// finishQueued marks the follower's waiting saves done with files of size bytes, as the
// worker does.
func (f *followEnv) finishQueued(size int) []string {
	f.t.Helper()
	ctx := context.Background()
	rows, _ := f.s.DB.Query(`SELECT id, target_path FROM vod_saves WHERE requested_by=? AND state=?`, f.user, Queued)
	type save struct {
		id   int64
		path string
	}
	var list []save
	for rows.Next() {
		var s save
		rows.Scan(&s.id, &s.path)
		list = append(list, s)
	}
	rows.Close()
	var paths []string
	for _, s := range list {
		os.MkdirAll(filepath.Dir(s.path), 0o755)
		os.WriteFile(s.path, make([]byte, size), 0o644)
		f.s.DB.Exec(`UPDATE vod_saves SET state=? WHERE id=?`, Done, s.id)
		f.s.followSaved(ctx, s.id, s.path, int64(size))
		paths = append(paths, s.path)
	}
	return paths
}

func (f *followEnv) follow(o FollowOptions) Follow {
	f.t.Helper()
	fl, err := f.s.FollowSeries(context.Background(), f.user, f.seriesID, o)
	if err != nil {
		f.t.Fatal(err)
	}
	return fl
}

func (f *followEnv) state() Follow {
	f.t.Helper()
	fl, err := f.s.FollowOf(context.Background(), f.user, f.seriesID)
	if err != nil {
		f.t.Fatal(err)
	}
	return fl
}

// Following is refused while it's off or there's no shows folder; unfollowing an unfollowed
// series says so; following twice changes the follow.
func TestFollowOffAndUnfollow(t *testing.T) {
	f := newFollowEnv(t, 2, func(v *settings.VODSave) { v.FollowEnabled = false })
	ctx := context.Background()
	if _, err := f.s.FollowSeries(ctx, f.user, f.seriesID, FollowOptions{}); !errors.Is(err, ErrFollowOff) {
		t.Fatalf("off: %v", err)
	}
	f.s.Settings.Update(ctx, func(s *settings.Settings) error {
		s.VODSave.FollowEnabled, s.VODSave.ShowsFolder = true, ""
		return nil
	})
	if _, err := f.s.FollowSeries(ctx, f.user, f.seriesID, FollowOptions{}); !errors.Is(err, ErrNoFolder) {
		t.Fatalf("no folder: %v", err)
	}
	f.s.Settings.Update(ctx, func(s *settings.Settings) error {
		s.VODSave.ShowsFolder, s.VODSave.FollowDefaultMaxGB = f.shows, 2.5
		return nil
	})
	if _, err := f.s.FollowSeries(ctx, f.user, f.seriesID+99, FollowOptions{}); !errors.Is(err, ErrNotFound) {
		t.Errorf("unknown series: %v", err)
	}
	if _, err := f.s.FollowSeries(ctx, f.user, f.seriesID, FollowOptions{Destination: ptrTo("Nope")}); !errors.Is(err, ErrDestination) {
		t.Errorf("unknown destination: %v", err)
	}
	fl := f.follow(FollowOptions{})
	if fl.Status != FollowFollowing || fl.MaxBytes != 2_500_000_000 || fl.UserName != "Kim" || fl.Title != "Kings (2011)" || fl.SeriesID != f.seriesID {
		t.Fatalf("follow: %+v", fl)
	}
	limit := int64(5_000_000_000)
	if fl = f.follow(FollowOptions{MaxBytes: &limit}); fl.MaxBytes != limit {
		t.Errorf("changed: %+v", fl)
	}
	if list, _ := f.s.Follows(ctx, 0); len(list) != 1 {
		t.Errorf("everyone's: %+v", list)
	}
	if list, _ := f.s.Follows(ctx, 1); len(list) != 0 {
		t.Errorf("the admin's: %+v", list)
	}
	if m := f.s.Followed(ctx, f.user, []int64{f.seriesID, 999}); !m[f.seriesID] || len(m) != 1 {
		t.Errorf("followed: %v", m)
	}
	if err := f.s.Unfollow(ctx, f.user, f.seriesID); err != nil {
		t.Fatal(err)
	}
	if err := f.s.Unfollow(ctx, f.user, f.seriesID); !errors.Is(err, ErrNotFollowed) {
		t.Errorf("again: %v", err)
	}
}

func ptrTo[T any](v T) *T { return &v }

// Only episodes that appear after the follow are saved, as the follower, into the
// follow's folder; the saved file counts as the follow's (and the follower's) usage.
func TestFollowSavesNewEpisodes(t *testing.T) {
	data := clip(t, "mkv")
	f := newFollowEnv(t, 2, nil)
	ctx := context.Background()
	fl := f.follow(FollowOptions{})
	f.s.CheckFollows(ctx)
	if q := f.queued(); len(q) != 0 {
		t.Fatalf("existing episodes queued: %v", q)
	}
	if fl.Status != FollowFollowing {
		t.Fatalf("follow: %+v", fl)
	}
	// A third episode appears.
	f.newEpisodes(3)
	f.src.files["series/3003.mkv"] = data
	f.s.CheckFollows(ctx)
	if q := f.queued(); len(q) != 1 || q[0] != "3003" {
		t.Fatalf("queued: %v", q)
	}
	if st := f.state(); st.EpisodesQueued != 1 || st.LastNewAt == nil {
		t.Errorf("queued state: %+v", st)
	}
	// Looking again doesn't queue it twice.
	f.s.CheckFollows(ctx)
	var n int
	f.s.DB.QueryRow("SELECT COUNT(*) FROM vod_saves").Scan(&n)
	if n != 1 {
		t.Errorf("%d saves", n)
	}
	var id int64
	f.s.DB.QueryRow("SELECT id FROM vod_saves").Scan(&id)
	f.start()
	v := f.wait(id, Done, Failed)
	if v.State != Done || v.RequestedBy != f.user || !strings.HasPrefix(v.TargetPath, f.shows) {
		t.Fatalf("save: %+v", v)
	}
	st := f.state()
	if st.EpisodesSaved != 1 || st.UsedBytes != int64(len(data)) || st.EpisodesQueued != 0 {
		t.Errorf("saved state: %+v", st)
	}
	if b := f.s.FollowedBytes(ctx); b[f.user] != int64(len(data)) || len(b) != 1 {
		t.Errorf("followed bytes: %v", b)
	}
	// Unfollowing keeps the file, which no longer counts.
	if err := f.s.Unfollow(ctx, f.user, f.seriesID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(v.TargetPath); err != nil {
		t.Errorf("the saved file went: %v", err)
	}
	if b := f.s.FollowedBytes(ctx); len(b) != 0 {
		t.Errorf("followed bytes after unfollowing: %v", b)
	}
}

// includeExisting saves the episodes already there too, leaving out those in the library and
// those already saved, a few at a time.
func TestFollowIncludeExisting(t *testing.T) {
	f := newFollowEnv(t, 6, nil)
	ctx := context.Background()
	// The library has episode 1; episode 2 was saved before (and is still waiting).
	shows := f.libID("Shows")
	d := f.s.DB
	r, _ := d.Exec(`INSERT INTO items(library_id, type, title, sort_title, year) VALUES(?, 'show', 'Kings', 'Kings', 2011)`, shows)
	show, _ := r.LastInsertId()
	r, _ = d.Exec(`INSERT INTO items(library_id, type, title, sort_title, parent_id, idx) VALUES(?, 'season', 'Season 1', 'Season 1', ?, 1)`, shows, show)
	season, _ := r.LastInsertId()
	r, _ = d.Exec(`INSERT INTO items(library_id, type, title, sort_title, parent_id, grandparent_id, idx) VALUES(?, 'episode', 'E1', '1', ?, ?, 1)`, shows, season, show)
	ep, _ := r.LastInsertId()
	r, _ = d.Exec(`INSERT INTO media_versions(item_id) VALUES(?)`, ep)
	v, _ := r.LastInsertId()
	d.Exec(`INSERT INTO media_files(version_id, library_id, path, size, mtime, width, height, video_codec, bitrate_kbps, available)
		VALUES(?,?,?,1,1,1920,1080,'h264',8000,1)`, v, shows, filepath.Join(f.shows, "Kings (2011)", "Season 1", "Kings - S01E01.mkv"))
	var e2 int64
	d.QueryRow("SELECT id FROM vod_episodes WHERE source_key='3002'").Scan(&e2)
	if _, err := f.s.SaveEpisode(ctx, e2, 1, false); err != nil {
		t.Fatal(err)
	}

	f.follow(FollowOptions{IncludeExisting: true})
	f.s.CheckFollows(ctx)
	if q := f.queued(); strings.Join(q, ",") != "3003,3004,3005" {
		t.Fatalf("first batch: %v", q)
	}
	if st := f.state(); st.EpisodesQueued != 3 {
		t.Errorf("state: %+v", st)
	}
	// As they finish, the rest follow.
	f.finishQueued(100)
	f.s.CheckFollows(ctx)
	if q := f.queued(); strings.Join(q, ",") != "3006" {
		t.Fatalf("second batch: %v", q)
	}
	f.finishQueued(100)
	f.s.CheckFollows(ctx)
	if q := f.queued(); len(q) != 0 {
		t.Errorf("more: %v", q)
	}
	if st := f.state(); st.EpisodesSaved != 4 || st.UsedBytes != 400 {
		t.Errorf("saved: %+v", st)
	}
	// The admin's save of episode 2 isn't the follow's.
	var by int64
	d.QueryRow("SELECT requested_by FROM vod_saves WHERE source_key='3002'").Scan(&by)
	if by != 1 {
		t.Errorf("episode 2 requested by %d", by)
	}
}

// A follow stops at its size limit, and at the follower's DVR quota (recordings plus
// followed saves), and goes on by itself once there's room again.
func TestFollowLimitsAndResume(t *testing.T) {
	f := newFollowEnv(t, 5, nil)
	ctx := context.Background()
	limit := int64(250)
	f.follow(FollowOptions{IncludeExisting: true, MaxBytes: &limit})
	f.s.CheckFollows(ctx)
	paths := f.finishQueued(100) // 300 bytes saved: over 250
	f.s.CheckFollows(ctx)
	st := f.state()
	if st.Status != FollowFull || !strings.Contains(st.StatusText, "size limit") || len(f.queued()) != 0 {
		t.Fatalf("full: %+v %v", st, f.queued())
	}
	// An episode leaves the library: room again.
	os.Remove(paths[0])
	f.s.CheckFollows(ctx)
	if st := f.state(); st.Status != FollowFollowing || st.UsedBytes != 200 || len(f.queued()) != 2 {
		t.Fatalf("resumed: %+v %v", st, f.queued())
	}
	// The follower's DVR usage reaches their quota.
	f.usage[f.user] = [2]int64{2_000_000_000, 2_000_000_000}
	f.finishQueued(10)
	limit = 0
	f.follow(FollowOptions{MaxBytes: &limit})
	f.newEpisodes(6)
	f.s.CheckFollows(ctx)
	st = f.state()
	if st.Status != FollowQuota || !strings.Contains(st.StatusText, "recording space is full (2.0 GB of 2.0 GB)") || len(f.queued()) != 0 {
		t.Fatalf("quota: %+v %v", st, f.queued())
	}
	f.s.CheckFollows(ctx)
	if len(f.alerts) != 1 || !strings.HasPrefix(f.alerts[0], "Kim: ") {
		t.Errorf("alerts: %v", f.alerts)
	}
	f.usage[f.user] = [2]int64{1_000_000_000, 2_000_000_000}
	f.s.CheckFollows(ctx)
	if st := f.state(); st.Status != FollowFollowing || strings.Join(f.queued(), ",") != "3006" {
		t.Errorf("after the quota: %+v %v", st, f.queued())
	}
}

// A follow pauses while following is off or its follower may not record, and while its
// save folder is gone.
func TestFollowPaused(t *testing.T) {
	f := newFollowEnv(t, 2, nil)
	ctx := context.Background()
	may := true
	f.s.MayFollow = func(context.Context, int64) bool { return may }
	f.follow(FollowOptions{})
	f.newEpisodes(3)
	may = false
	f.s.CheckFollows(ctx)
	if st := f.state(); st.Status != FollowPaused || st.StatusText != "Paused: this profile may not record" || len(f.queued()) != 0 {
		t.Fatalf("may not record: %+v", st)
	}
	may = true
	f.s.Settings.Update(ctx, func(s *settings.Settings) error { s.VODSave.FollowEnabled = false; return nil })
	f.s.CheckFollows(ctx)
	if st := f.state(); st.Status != FollowPaused || !strings.Contains(st.StatusText, "turned off") {
		t.Fatalf("off: %+v", st)
	}
	f.s.Settings.Update(ctx, func(s *settings.Settings) error { s.VODSave.FollowEnabled = true; return nil })
	f.s.CheckFollows(ctx)
	if st := f.state(); st.Status != FollowFollowing || len(f.queued()) != 1 {
		t.Errorf("back on: %+v %v", st, f.queued())
	}
}

// Without the episodes known when followed (the source was down), the first list fetched
// is taken as the old episodes: nothing is saved from it.
func TestFollowBaselineLater(t *testing.T) {
	f := newFollowEnv(t, 2, nil)
	ctx := context.Background()
	f.src.down.Store(true)
	f.s.DB.Exec("DELETE FROM vod_episodes")
	f.s.DB.Exec("UPDATE vod_series SET episodes_at=NULL")
	f.follow(FollowOptions{})
	f.src.down.Store(false)
	f.s.CheckFollows(ctx)
	f.s.CheckFollows(ctx)
	if q := f.queued(); len(q) != 0 {
		t.Fatalf("old episodes queued: %v", q)
	}
	f.newEpisodes(3)
	f.s.CheckFollows(ctx)
	if q := f.queued(); strings.Join(q, ",") != "3003" {
		t.Errorf("new: %v", q)
	}
}
