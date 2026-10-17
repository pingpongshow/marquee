package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"marquee/internal/api"
	"marquee/internal/auth"
	"marquee/internal/livetv"
	"marquee/internal/settings"
)

// Admins aren't subject to the default DVR quota; a quota set for one applies to them.
func TestDVRAdminQuota(t *testing.T) {
	var s settings.Settings
	s.Integrations.DVRDefaultQuotaGB = 10
	five := 5
	if b, gb, own := api.DVRQuota(auth.User{IsAdmin: true}, s); b != 0 || gb != 0 || own {
		t.Errorf("admin without a quota: %d %d %v", b, gb, own)
	}
	if b, _, own := api.DVRQuota(auth.User{IsAdmin: true, Restrictions: auth.Restrictions{DVRQuotaGB: &five}}, s); b != 5_000_000_000 || !own {
		t.Errorf("admin with their own quota: %d %v", b, own)
	}
	if b, _, _ := api.DVRQuota(auth.User{}, s); b != 10_000_000_000 {
		t.Errorf("non-admin default: %d", b)
	}

	w := newDVRWorld(t)
	// The admin's recordings take 2 GB; the default quota is 1 GB.
	p := filepath.Join(w.rec, "TV", "Big", "Big.mkv")
	os.MkdirAll(filepath.Dir(p), 0o755)
	os.WriteFile(p, nil, 0o644)
	os.Truncate(p, 2_000_000_000)
	now := time.Now().UTC()
	w.h.db.Exec(`INSERT INTO dvr_recordings (user_id, channel_name, start, stop, title, status, path, created_at) VALUES (?, 'News', ?, ?, 'Big', 'completed', ?, ?)`,
		w.ids["admin"], now.Add(-50*time.Hour).Format(time.RFC3339), now.Add(-49*time.Hour).Format(time.RFC3339), p, now.Format(time.RFC3339))
	if code := w.as("admin").do("PATCH", "/settings", map[string]any{"integrations": map[string]any{"dvrDefaultQuotaGb": 1}}, nil); code != 200 {
		t.Fatalf("default quota: %d", code)
	}
	if code, raw := w.schedule("admin", "Later Show"); code != 201 {
		t.Errorf("admin held to the default: %d %s", code, raw)
	}
	var u api.DvrUsage
	w.as("admin").do("GET", "/livetv/recordings/usage", nil, &u)
	if u.QuotaBytes != 0 || u.OverQuota || u.QuotaSource != api.DvrUsageQuotaSourceDefault {
		t.Errorf("admin usage: %+v", u)
	}
	// The admin sets a quota for themselves: it applies.
	if code := w.as("admin").do("PATCH", "/users/"+itoa(w.ids["admin"]), map[string]any{"restrictions": map[string]any{"dvrQuotaGb": 1}}, nil); code != 200 {
		t.Fatalf("own quota: %d", code)
	}
	if code, _ := w.schedule("admin", "Another"); code != 409 {
		t.Errorf("admin over their own quota: %d", code)
	}
	var all []api.DvrUsage
	w.as("admin").do("GET", "/admin/dvr/usage", nil, &all)
	for _, x := range all {
		if x.UserId == w.ids["admin"] && (x.QuotaBytes != 1_000_000_000 || x.QuotaSource != api.DvrUsageQuotaSourceUser || !x.OverQuota) {
			t.Errorf("admin in the usage list: %+v", x)
		}
	}
	if me := userByID(w, w.ids["admin"]); me.Restrictions.DvrQuotaGb == nil || *me.Restrictions.DvrQuotaGb != 1 {
		t.Errorf("admin's quota not kept: %+v", me.Restrictions)
	}
}

// Channel groups no longer limit anyone: a liveTvGroups sent is ignored, isn't returned,
// and every channel, group and guide row is listed.
func TestLiveGroupsIgnored(t *testing.T) {
	w := newDVRWorld(t)
	w.h.db.Exec(`UPDATE live_channels SET group_name = 'News'`)
	w.h.db.Exec(`INSERT INTO live_channels (source_id, source_key, name, group_name, stream_url) VALUES ('s', 'k2', 'Cartoons', 'Kids', 'http://x/k')`)
	var u api.User
	if code := w.as("admin").do("POST", "/users", map[string]any{"username": "grouped", "password": "correct horse",
		"restrictions": map[string]any{"liveTvGroups": []string{"Kids"}}}, &u); code != 201 {
		t.Fatalf("create: %d", code)
	}
	if u.Restrictions.LiveTvGroups != nil {
		t.Errorf("liveTvGroups returned: %v", *u.Restrictions.LiveTvGroups)
	}
	var stored string
	w.h.db.QueryRow(`SELECT restrictions FROM users WHERE id = ?`, u.Id).Scan(&stored)
	if strings.Contains(stored, "liveTvGroups") {
		t.Errorf("stored: %s", stored)
	}
	// One stored before (the migration clears these; the server ignores any left).
	w.h.db.Exec(`UPDATE users SET restrictions = '{"liveTvGroups":["Kids"]}' WHERE id = ?`, w.ids["alex"])
	var chans []api.LiveChannel
	if code := w.as("alex").do("GET", "/livetv/channels", nil, &chans); code != 200 || len(chans) != 2 {
		t.Errorf("channels: %d %d", code, len(chans))
	}
	var groups []map[string]any
	if w.as("alex").do("GET", "/livetv/groups", nil, &groups); len(groups) != 2 {
		t.Errorf("groups: %v", groups)
	}
	if got := userByID(w, w.ids["alex"]); got.Restrictions.LiveTvGroups != nil {
		t.Errorf("stored groups returned: %v", *got.Restrictions.LiveTvGroups)
	}
}

// Stopping a recording: owner or admin only; 409 when it isn't recording; a recording in
// progress is in the activity monitor (admins) with a link to Live TV → Recordings.
func TestStopRecordingAndActivity(t *testing.T) {
	w := newDVRWorld(t)
	code, raw := w.schedule("alex", "Later Show")
	if code != 201 {
		t.Fatalf("schedule: %d %s", code, raw)
	}
	var sched struct{ Recording api.Recording }
	json.Unmarshal(raw, &sched)
	id := sched.Recording.Id
	stop := func(who string, rid int64, keep bool) (int, string) {
		var out json.RawMessage
		c := w.as(who).do("POST", "/livetv/recordings/"+itoa(rid)+"/stop", map[string]any{"keep": keep}, &out)
		return c, string(out)
	}
	if c, _ := stop("kid", id, true); c != 403 {
		t.Errorf("someone else: %d", c)
	}
	if c, body := stop("alex", id, true); c != 409 || !strings.Contains(body, "not_recording") {
		t.Errorf("not recording: %d %s", c, body)
	}
	if c, _ := stop("admin", 99999, true); c != 404 {
		t.Errorf("unknown: %d", c)
	}

	// A recording in progress (FFmpeg stand-in that just waits).
	ff := filepath.Join(t.TempDir(), "ffmpeg")
	os.WriteFile(ff, []byte("#!/bin/sh\nexec sleep 60\n"), 0o755)
	dvr := w.h.handlers.DVR
	dvr.FFmpeg = ff
	w.h.handlers.LiveTV.DVR = dvr
	now := time.Now().UTC()
	res, _ := w.h.db.Exec(`INSERT INTO dvr_recordings (user_id, channel_id, channel_name, start, stop, title, created_at) VALUES (?, ?, 'News', ?, ?, 'Gold Rush', ?)`,
		w.ids["alex"], w.channel, now.Add(-10*time.Minute).Format(time.RFC3339), now.Add(50*time.Minute).Format(time.RFC3339), now.Format(time.RFC3339))
	rid, _ := res.LastInsertId()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	dvr.Tick(ctx)
	if len(dvr.ActiveRecordings()) != 1 {
		t.Fatalf("not recording: %+v", dvr.ActiveRecordings())
	}
	var act api.Activity
	w.as("admin").do("GET", "/activity", nil, &act)
	var task *api.ActivityTask
	for i := range act.Tasks {
		if act.Tasks[i].Id == "recording:"+itoa(rid) {
			task = &act.Tasks[i]
		}
	}
	if task == nil {
		t.Fatalf("recording not in activity: %+v", act.Tasks)
	}
	if task.Kind != api.ActivityTaskKindTask || task.Title != "Recording Gold Rush on News" || task.Link == nil || *task.Link != "/livetv?tab=recordings" ||
		task.Progress == nil || task.Progress.Total != 3600 || task.Progress.Done < 590 || task.Progress.Done > 700 {
		t.Errorf("activity task: %+v %+v", task, task.Progress)
	}
	w.as("alex").do("GET", "/activity", nil, &act)
	if len(act.Tasks) != 0 {
		t.Errorf("non-admin activity: %+v", act.Tasks)
	}
	// The list shows it recording; someone else can't stop it; its owner stops and deletes it.
	if c, _ := stop("kid", rid, false); c != 403 {
		t.Errorf("someone else stopping: %d", c)
	}
	if c, body := stop("alex", rid, false); c != 204 {
		t.Fatalf("stop and delete: %d %s", c, body)
	}
	if _, err := dvr.Get(ctx, rid); err != livetv.ErrRecordingNotFound {
		t.Errorf("row kept: %v", err)
	}
	if len(dvr.ActiveRecordings()) != 0 {
		t.Error("still active")
	}
}

func userByID(w *dvrWorld, id int64) api.User {
	var list []api.User
	w.as("admin").do("GET", "/users", nil, &list)
	for _, u := range list {
		if u.Id == id {
			return u
		}
	}
	w.h.t.Fatalf("user %d not listed", id)
	return api.User{}
}
