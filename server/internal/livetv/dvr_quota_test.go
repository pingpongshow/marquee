package livetv

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// quotaFixture is a recorder with a guide, hooks the tests steer, and helpers to add
// recordings.
type quotaFixture struct {
	t       *testing.T
	s       *Service
	r       *Recorder
	uid     int64
	quota   int64
	may     map[int64]bool
	action  string
	playing map[string]bool
	trashed []string
	alerts  []string
	folder  string
}

func newQuotaFixture(t *testing.T) *quotaFixture {
	s, _, uid := setup(t)
	s.Refresh(context.Background())
	f := &quotaFixture{t: t, s: s, uid: uid, may: map[int64]bool{}, action: "block", playing: map[string]bool{}}
	f.r = &Recorder{Live: s, Dir: t.TempDir(),
		Folder:      func() string { return f.folder },
		CanRecord:   func(_ context.Context, id int64) bool { v, ok := f.may[id]; return !ok || v },
		QuotaBytes:  func(context.Context, int64) int64 { return f.quota },
		QuotaAction: func() string { return f.action },
		Playing:     func(p string) bool { return f.playing[p] },
		Trash: func(_ context.Context, p string) error {
			f.trashed = append(f.trashed, p)
			return os.Remove(p)
		},
		OnQuota: func(_ int64, owner, title, msg string) { f.alerts = append(f.alerts, msg) },
	}
	return f
}

// finished adds a completed recording of size bytes at dir/name, started daysAgo days ago;
// watched marks it played by its owner (it then has a library item).
func (f *quotaFixture) finished(dir, name string, size int, daysAgo int, watched bool) Recording {
	f.t.Helper()
	p := filepath.Join(dir, "TV", name+".mkv")
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, make([]byte, size), 0o644)
	start := time.Now().Add(-time.Duration(daysAgo) * 24 * time.Hour).UTC().Format(time.RFC3339)
	res, err := f.s.DB.Exec(`INSERT INTO dvr_recordings (user_id, channel_name, start, stop, title, status, path, size, created_at)
		VALUES (?, 'News One', ?, ?, ?, 'completed', ?, 1, ?)`, f.uid, start, start, name, p, start)
	if err != nil {
		f.t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	r, _ := f.s.DB.Exec(`INSERT OR IGNORE INTO libraries (id, name, type) VALUES (1, 'Recorded TV', 'shows')`)
	_ = r
	r, _ = f.s.DB.Exec(`INSERT INTO items (library_id, type, title, sort_title) VALUES (1, 'movie', ?, ?)`, name, name)
	item, _ := r.LastInsertId()
	r, _ = f.s.DB.Exec(`INSERT INTO media_versions (item_id) VALUES (?)`, item)
	v, _ := r.LastInsertId()
	f.s.DB.Exec(`INSERT INTO media_files (version_id, library_id, path, size, mtime, available) VALUES (?, 1, ?, ?, 1, 1)`, v, p, size)
	if watched {
		f.s.DB.Exec(`INSERT INTO user_item_state (user_id, item_id, play_count) VALUES (?, ?, 1)`, f.uid, item)
	}
	x, _ := f.r.Get(context.Background(), id)
	return x
}

// due adds a scheduled recording starting now.
func (f *quotaFixture) due(title string) int64 {
	now := time.Now().UTC()
	res, err := f.s.DB.Exec(`INSERT INTO dvr_recordings (user_id, channel_name, start, stop, title, created_at) VALUES (?, 'News One', ?, ?, ?, ?)`,
		f.uid, now.Format(time.RFC3339), now.Add(time.Hour).Format(time.RFC3339), title, now.Format(time.RFC3339))
	if err != nil {
		f.t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

func TestDVRUsageCountsInProgressAndEveryFolder(t *testing.T) {
	f := newQuotaFixture(t)
	ctx := context.Background()
	f.finished(f.r.Dir, "Old", 1000, 3, false)
	// A recording in progress counts with the parts written so far.
	id := f.due("Live")
	f.s.DB.Exec(`UPDATE dvr_recordings SET status = 'recording' WHERE id = ?`, id)
	work := f.r.workDir(ctx, id)
	os.MkdirAll(work, 0o755)
	os.WriteFile(filepath.Join(work, "part-000.ts"), make([]byte, 300), 0o644)
	os.WriteFile(filepath.Join(work, "part-001.ts"), make([]byte, 200), 0o644)
	// Someone else's recordings don't count.
	res, _ := f.s.DB.Exec(`INSERT INTO users (username, display_name) VALUES ('o', 'Other')`)
	other, _ := res.LastInsertId()
	f.s.DB.Exec(`INSERT INTO dvr_recordings (user_id, channel_name, start, stop, title, status, path, size, created_at) VALUES (?, 'x', 'a', 'b', 'Theirs', 'completed', ?, 5000, 'a')`,
		other, filepath.Join(f.r.Dir, "theirs.mkv"))

	u, err := f.r.UserUsage(ctx, f.uid)
	if err != nil || u.Bytes != 1500 || u.Recordings != 2 {
		t.Fatalf("usage: %+v %v", u, err)
	}
	// The recordings folder moves: new recordings go to the new one, the parts of the one in
	// progress stay where they are, and the old recordings still count.
	f.folder = t.TempDir()
	if f.r.NewFolder() != f.folder || !slices.Contains(f.r.Roots(ctx), f.r.Dir) {
		t.Fatalf("folders: %s %v", f.r.NewFolder(), f.r.Roots(ctx))
	}
	if got := f.r.workDir(ctx, id); got != work {
		t.Errorf("resumed work dir moved: %s", got)
	}
	if got := f.r.workDir(ctx, id+100); !strings.HasPrefix(got, f.folder) {
		t.Errorf("new work dir: %s", got)
	}
	f.finished(f.folder, "New", 700, 1, false)
	if u, _ = f.r.UserUsage(ctx, f.uid); u.Bytes != 2200 {
		t.Errorf("usage across folders: %+v", u)
	}
	// A cut (smaller) file counts at its size now, not when it was recorded.
	old := filepath.Join(f.r.Dir, "TV", "Old.mkv")
	os.WriteFile(old, make([]byte, 400), 0o644)
	if u, _ = f.r.UserUsage(ctx, f.uid); u.Bytes != 1600 {
		t.Errorf("after a cut: %+v", u)
	}
	all, _ := f.r.AllUsage(ctx)
	if all[f.uid].Bytes != 1600 || all[other].Bytes != 0 || all[other].Recordings != 1 {
		t.Errorf("all usage: %+v", all)
	}
	// The new folder's recording is deleted as a recording (it's inside a recordings folder).
	list, _ := f.r.List(ctx)
	for _, x := range list {
		if x.Title == "New" {
			if err := f.r.Cancel(ctx, x.ID, true); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Stat(x.Path); !os.IsNotExist(err) {
				t.Errorf("file in the new folder not deleted: %v", err)
			}
		}
	}
}

func TestDVRQuotaScheduleAndStart(t *testing.T) {
	f := newQuotaFixture(t)
	ctx := context.Background()
	list, _ := f.s.Channels(ctx, f.uid, Filter{})
	news := list[0]
	f.finished(f.r.Dir, "Old", 1000, 3, false)

	// Unlimited, and under quota, schedule.
	if _, _, err := f.r.Schedule(ctx, f.uid, news.ID, news.Next.Start, false, false); err != nil {
		t.Fatalf("unlimited: %v", err)
	}
	f.quota = 2000
	if rec, _, err := f.r.Schedule(ctx, f.uid, news.ID, news.Now.Start, false, false); err != nil || rec.Rating != "" {
		t.Fatalf("under quota: %+v %v", rec, err)
	}
	// The guide's rating is carried onto the recording.
	recs, _ := f.r.List(ctx)
	for _, x := range recs {
		if x.Title == "Weather" && x.Rating != "TV-PG" {
			t.Errorf("rating: %+v", x)
		}
		if x.OwnerName != "U" {
			t.Errorf("owner: %+v", x)
		}
	}
	// At quota: refused.
	f.quota = 1000
	if _, _, err := f.r.Schedule(ctx, f.uid, news.ID, news.Next.Start, true, false); err != ErrQuotaExceeded {
		t.Errorf("at quota: %v", err)
	}
	// A due recording over quota isn't made, and admins hear about it.
	id := f.due("Due")
	if msg := f.r.mayStart(ctx, id); msg != msgOverQuota {
		t.Errorf("start over quota: %q", msg)
	}
	if len(f.alerts) != 1 || !strings.Contains(f.alerts[0], "Due") {
		t.Errorf("alerts: %v", f.alerts)
	}
	// One resumed after a restart (parts already written) carries on: no cutting mid-show.
	work := f.r.workDir(ctx, id)
	os.MkdirAll(work, 0o755)
	os.WriteFile(filepath.Join(work, "part-000.ts"), []byte("ts"), 0o644)
	if msg := f.r.mayStart(ctx, id); msg != "" {
		t.Errorf("resumed: %q", msg)
	}
	// Under quota it starts.
	f.quota = 5000
	if msg := f.r.mayStart(ctx, f.due("Fine")); msg != "" {
		t.Errorf("under quota: %q", msg)
	}
	// Tick marks a refused one failed with the reason.
	f.quota = 1000
	refused := f.due("Refused")
	f.r.Tick(ctx)
	if x, _ := f.r.Get(ctx, refused); x.Status != StatusFailed || x.Error != msgOverQuota {
		t.Errorf("tick: %+v", x)
	}
}

func TestDVRQuotaDeleteOldest(t *testing.T) {
	f := newQuotaFixture(t)
	ctx := context.Background()
	f.action = "deleteOldest"
	oldest := f.finished(f.r.Dir, "Oldest", 400, 9, true)
	unwatched := f.finished(f.r.Dir, "Unwatched", 400, 8, false)
	playing := f.finished(f.r.Dir, "Playing", 400, 7, true)
	older := f.finished(f.r.Dir, "Older", 400, 6, true)
	newer := f.finished(f.r.Dir, "Newer", 400, 2, true)
	f.playing[playing.Path] = true
	// One in progress is never removed, whatever its age.
	inProgress := f.due("In progress")
	f.s.DB.Exec(`UPDATE dvr_recordings SET status = 'recording', start = ? WHERE id = ?`, time.Now().Add(-30*24*time.Hour).UTC().Format(time.RFC3339), inProgress)

	f.quota = 1500 // 2000 used: two watched, not playing, oldest first go
	// Scheduling isn't refused: removing watched recordings would make room.
	if f.r.atQuota(ctx, f.uid) {
		t.Error("refused although watched recordings can go")
	}
	id := f.due("New show")
	if msg := f.r.mayStart(ctx, id); msg != "" {
		t.Fatalf("start: %q", msg)
	}
	if want := []string{oldest.Path, older.Path}; !slices.Equal(f.trashed, want) {
		t.Errorf("trashed %v, want %v", f.trashed, want)
	}
	for _, x := range []Recording{oldest, older} {
		if _, err := f.r.Get(ctx, x.ID); err != ErrRecordingNotFound {
			t.Errorf("%s still listed: %v", x.Title, err)
		}
	}
	for _, x := range []Recording{unwatched, playing, newer} {
		if _, err := os.Stat(x.Path); err != nil {
			t.Errorf("%s removed: %v", x.Title, err)
		}
	}
	if len(f.alerts) != 1 || !strings.Contains(f.alerts[0], "media trash") {
		t.Errorf("alerts: %v", f.alerts)
	}
	// Nothing else may go: refused, and scheduling is refused too.
	f.quota = 400
	if msg := f.r.mayStart(ctx, f.due("Too much")); msg != msgOverQuota {
		t.Errorf("nothing to remove: %q", msg)
	}
	if !f.r.atQuota(ctx, f.uid) {
		t.Error("scheduling allowed with nothing to remove")
	}
	if slices.Contains(f.trashed, playing.Path) || slices.Contains(f.trashed, unwatched.Path) {
		t.Errorf("removed one being watched or unwatched: %v", f.trashed)
	}
}

func TestDVRRecordingOffPausesSeries(t *testing.T) {
	f := newQuotaFixture(t)
	ctx := context.Background()
	list, _ := f.s.Channels(ctx, f.uid, Filter{})
	news := list[0]
	_, rule, err := f.r.Schedule(ctx, f.uid, news.ID, news.Now.Start, true, false)
	if err != nil || rule.Upcoming != 1 || rule.Paused {
		t.Fatalf("series: %+v %v", rule, err)
	}
	one, _, _ := f.r.Schedule(ctx, f.uid, news.ID, news.Next.Start, false, false)

	f.may[f.uid] = false
	if err := f.r.expandRules(ctx); err != nil {
		t.Fatal(err)
	}
	rules, _ := f.r.Rules(ctx)
	if len(rules) != 1 || !rules[0].Paused || rules[0].Upcoming != 0 {
		t.Errorf("paused: %+v", rules)
	}
	if msg := f.r.mayStart(ctx, one.ID); msg != msgRecordingOff {
		t.Errorf("one-off of a user who may not record: %q", msg)
	}
	// Allowed again: the series picks up where it was.
	f.may[f.uid] = true
	f.r.expandRules(ctx)
	if rules, _ = f.r.Rules(ctx); rules[0].Paused || rules[0].Upcoming != 1 {
		t.Errorf("resumed: %+v", rules)
	}
}
