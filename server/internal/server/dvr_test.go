package server

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"marquee/internal/api"
	"marquee/internal/auth"
	"marquee/internal/library"
	"marquee/internal/livetv"
	"marquee/internal/settings"
)

// A user's DVR quota: their own when set (0 = unlimited), else the server default.
func TestDVRQuotaOverride(t *testing.T) {
	var s settings.Settings
	s.Integrations.DVRDefaultQuotaGB = 50
	zero, ten := 0, 10
	for _, c := range []struct {
		own   *int
		bytes int64
		gb    int
		mine  bool
	}{{nil, 50_000_000_000, 50, false}, {&zero, 0, 0, true}, {&ten, 10_000_000_000, 10, true}} {
		u := auth.User{Restrictions: auth.Restrictions{DVRQuotaGB: c.own}}
		if b, gb, own := api.DVRQuota(u, s); b != c.bytes || gb != c.gb || own != c.mine {
			t.Errorf("%v: %d %d %v", c.own, b, gb, own)
		}
	}
	s.Integrations.DVRDefaultQuotaGB = 0
	if b, _, _ := api.DVRQuota(auth.User{}, s); b != 0 {
		t.Errorf("unlimited default: %d", b)
	}
}

type dvrWorld struct {
	h       *harness
	admin   string
	tokens  map[string]string
	ids     map[string]int64
	rec     string // the recordings folder
	lib     int64
	channel int64
	items   map[string]int64
	recs    map[string]int64
}

func newDVRWorld(t *testing.T) *dvrWorld {
	h := newHarness(t)
	ctx := context.Background()
	var res api.AuthResult
	h.do("POST", "/setup", map[string]any{"serverName": "T", "username": "admin", "password": "correct horse", "device": device}, &res)
	w := &dvrWorld{h: h, admin: res.Token, tokens: map[string]string{"admin": res.Token}, ids: map[string]int64{"admin": res.User.Id},
		items: map[string]int64{}, recs: map[string]int64{}}
	h.token = w.admin
	f := false
	for name, r := range map[string]map[string]any{
		"alex":   {},
		"kid":    {"maxContentRating": "TV-PG"},
		"noview": {"canViewDVR": f},
		"norec":  {"canRecordDVR": f},
	} {
		var u api.User
		if code := h.do("POST", "/users", map[string]any{"username": name, "displayName": strings.ToUpper(name[:1]) + name[1:], "password": "correct horse", "restrictions": r}, &u); code != 201 {
			t.Fatalf("create %s: %d", name, code)
		}
		w.ids[name] = u.Id
		var lr api.AuthResult
		h.do("POST", "/auth/login", map[string]any{"username": name, "password": "correct horse", "device": device}, &lr)
		w.tokens[name] = lr.Token
	}

	w.rec = filepath.Join(t.TempDir(), "recordings")
	live := &livetv.Service{DB: h.db, Settings: h.store}
	libs := library.NewStore(h.db)
	h.handlers.LiveTV = live
	h.handlers.DVR = &livetv.Recorder{Live: live, Dir: w.rec,
		Folder:  func() string { return h.store.Get().Integrations.DVRFolder },
		Library: libs.RecordingsLibrary,
		QuotaBytes: func(ctx context.Context, id int64) int64 {
			u, _ := h.handlers.Auth.GetUser(ctx, id)
			b, _, _ := api.DVRQuota(u, h.store.Get())
			return b
		}}
	lib, err := libs.RecordingsLibrary(ctx, false, filepath.Join(w.rec, "TV"))
	if err != nil {
		t.Fatal(err)
	}
	w.lib = lib

	r, _ := h.db.Exec(`INSERT INTO live_channels (source_id, source_key, name, stream_url) VALUES ('s', 'k', 'News', 'http://x/s')`)
	w.channel, _ = r.LastInsertId()
	now := time.Now().UTC().Truncate(time.Hour)
	for i, p := range []struct{ title, rating string }{{"Later Show", "TV-Y"}, {"Unrated Show", ""}, {"Another", "TV-G"}} {
		start := now.Add(time.Duration(i+1) * time.Hour)
		h.db.Exec(`INSERT INTO live_programmes (channel_id, start, stop, title, rating) VALUES (?, ?, ?, ?, ?)`,
			w.channel, start.Format(time.RFC3339), start.Add(time.Hour).Format(time.RFC3339), p.title, p.rating)
	}

	// Two finished recordings by alex, filed in the library: one rated TV-MA there, one with
	// no rating of its own but TV-G in the guide.
	for _, f := range []struct{ title, itemRating, guideRating string }{{"Grown-up", "TV-MA", "TV-14"}, {"Family", "", "TV-G"}} {
		p := filepath.Join(w.rec, "TV", f.title, f.title+".mkv")
		os.MkdirAll(filepath.Dir(p), 0o755)
		os.WriteFile(p, []byte("video"), 0o644)
		var rating any
		if f.itemRating != "" {
			rating = f.itemRating
		}
		r, _ := h.db.Exec(`INSERT INTO items (library_id, type, title, sort_title, content_rating) VALUES (?, 'movie', ?, ?, ?)`, lib, f.title, f.title, rating)
		item, _ := r.LastInsertId()
		r, _ = h.db.Exec(`INSERT INTO media_versions (item_id) VALUES (?)`, item)
		v, _ := r.LastInsertId()
		h.db.Exec(`INSERT INTO media_files (version_id, library_id, path, size, mtime, available) VALUES (?, ?, ?, 5, 1, 1)`, v, lib, p)
		r, _ = h.db.Exec(`INSERT INTO dvr_recordings (user_id, channel_name, start, stop, title, status, path, rating, created_at)
			VALUES (?, 'News', ?, ?, ?, 'completed', ?, ?, ?)`, w.ids["alex"], now.Add(-48*time.Hour).Format(time.RFC3339), now.Add(-47*time.Hour).Format(time.RFC3339),
			f.title, p, f.guideRating, now.Format(time.RFC3339))
		w.items[f.title], _ = item, 0
		w.recs[f.title], _ = r.LastInsertId()
	}
	return w
}

func (w *dvrWorld) as(name string) *harness { w.h.token = w.tokens[name]; return w.h }

func (w *dvrWorld) schedule(name, title string) (int, json.RawMessage) {
	var start string
	w.h.db.QueryRow(`SELECT start FROM live_programmes WHERE title = ?`, title).Scan(&start)
	var raw json.RawMessage
	code := w.as(name).do("POST", "/livetv/recordings", map[string]any{"channelId": w.channel, "start": start}, &raw)
	return code, raw
}

func recordingTitles(list []api.Recording) []string {
	var out []string
	for _, x := range list {
		out = append(out, x.Title)
	}
	sort.Strings(out)
	return out
}

func TestDVRSharingAndSwitches(t *testing.T) {
	w := newDVRWorld(t)

	// alex schedules two programmes (one with no guide rating); norec may not.
	for _, title := range []string{"Later Show", "Unrated Show"} {
		if code, raw := w.schedule("alex", title); code != 201 {
			t.Fatalf("schedule %s: %d %s", title, code, raw)
		}
	}
	if code, _ := w.schedule("norec", "Another"); code != 403 {
		t.Errorf("norec scheduled: %d", code)
	}

	// Everyone who may view DVR sees everyone's recordings, within their rating limit.
	var list []api.Recording
	if code := w.as("norec").do("GET", "/livetv/recordings", nil, &list); code != 200 ||
		!slices.Equal(recordingTitles(list), []string{"Family", "Grown-up", "Later Show", "Unrated Show"}) {
		t.Fatalf("norec list: %d %v", code, recordingTitles(list))
	}
	for _, x := range list {
		if *x.OwnerName != "Alex" || *x.CanDelete {
			t.Errorf("norec sees %s: owner %v, canDelete %v", x.Title, *x.OwnerName, *x.CanDelete)
		}
	}
	w.as("alex").do("GET", "/livetv/recordings", nil, &list)
	for _, x := range list {
		if !*x.CanDelete {
			t.Errorf("alex can't delete his %s", x.Title)
		}
	}
	if code := w.as("kid").do("GET", "/livetv/recordings", nil, &list); code != 200 ||
		!slices.Equal(recordingTitles(list), []string{"Family", "Later Show"}) {
		t.Errorf("kid list: %d %v", code, recordingTitles(list))
	}
	if code := w.as("noview").do("GET", "/livetv/recordings", nil, nil); code != 403 {
		t.Errorf("noview list: %d", code)
	}
	// noview may still record: they see their own usage and series, nobody else's.
	if code := w.as("noview").do("GET", "/livetv/recordings/usage", nil, nil); code != 200 {
		t.Errorf("noview usage: %d", code)
	}
	w.tokens["neither"] = w.tokens["noview"]
	w.as("admin").do("PATCH", "/users/"+itoa(w.ids["noview"]), map[string]any{"restrictions": map[string]any{"canViewDVR": false, "canRecordDVR": false}}, nil)
	for _, path := range []string{"/livetv/recording-rules", "/livetv/recordings/usage", "/livetv/recordings"} {
		if code := w.as("neither").do("GET", path, nil, nil); code != 403 {
			t.Errorf("neither %s: %d", path, code)
		}
	}
	w.as("admin").do("PATCH", "/users/"+itoa(w.ids["noview"]), map[string]any{"restrictions": map[string]any{"canViewDVR": false}}, nil)

	// The library follows the same rules: browse, search, item and playback.
	var libs []api.Library
	w.as("noview").do("GET", "/libraries", nil, &libs)
	for _, l := range libs {
		if l.Id == w.lib {
			t.Error("noview sees the recordings library")
		}
	}
	if code := w.as("noview").do("GET", "/libraries/"+itoa(w.lib)+"/items", nil, nil); code != 404 && code != 403 {
		t.Errorf("noview browses recordings: %d", code)
	}
	var found api.SearchResults
	w.as("noview").do("GET", "/search?q=Family", nil, &found)
	if len(found.Groups) != 0 {
		t.Errorf("noview finds recordings: %+v", found.Groups)
	}
	w.as("alex").do("GET", "/search?q=Family", nil, &found)
	if len(found.Groups) != 1 {
		t.Errorf("alex can't find recordings: %+v", found.Groups)
	}
	for _, c := range []struct {
		who, title string
		want       int
	}{{"noview", "Family", 404}, {"kid", "Grown-up", 404}, {"kid", "Family", 200}, {"alex", "Grown-up", 200}} {
		if code := w.as(c.who).do("GET", "/items/"+itoa(w.items[c.title]), nil, nil); code != c.want {
			t.Errorf("%s gets %s: %d, want %d", c.who, c.title, code, c.want)
		}
		if c.want == 404 {
			if code := w.as(c.who).do("POST", "/playback", map[string]any{"itemId": w.items[c.title], "device": map[string]any{"clientId": "c", "name": "n", "platform": "web"}}, nil); code != 404 {
				t.Errorf("%s plays %s: %d", c.who, c.title, code)
			}
		}
	}
	var kidItems struct {
		Items []api.ItemSummary `json:"items"`
	}
	if code := w.as("kid").do("GET", "/libraries/"+itoa(w.lib)+"/items?type=movie", nil, &kidItems); code != 200 {
		t.Errorf("kid browse: %d", code)
	}
	if len(kidItems.Items) != 1 || kidItems.Items[0].Title != "Family" {
		t.Errorf("kid browses recordings: %+v", kidItems.Items)
	}

	// Live TV status: record buttons and the Recordings tab.
	var st api.LiveTvStatus
	w.as("norec").do("GET", "/livetv/status", nil, &st)
	if *st.CanRecord || !*st.CanViewRecordings {
		t.Errorf("norec status: %v %v", *st.CanRecord, *st.CanViewRecordings)
	}
	w.as("noview").do("GET", "/livetv/status", nil, &st)
	if !*st.CanRecord || *st.CanViewRecordings {
		t.Errorf("noview status: %v %v", *st.CanRecord, *st.CanViewRecordings)
	}

	// Only whoever scheduled it, or an admin, may cancel or delete.
	unrated := int64(0)
	for _, x := range list {
		_ = x
	}
	w.h.db.QueryRow(`SELECT id FROM dvr_recordings WHERE title = 'Unrated Show'`).Scan(&unrated)
	if code := w.as("norec").do("DELETE", "/livetv/recordings/"+itoa(unrated), nil, nil); code != 403 {
		t.Errorf("someone else cancels: %d", code)
	}
	if code := w.as("norec").do("DELETE", "/livetv/recordings/"+itoa(w.recs["Family"])+"?deleteFile=true", nil, nil); code != 403 {
		t.Errorf("someone else deletes: %d", code)
	}
	if code := w.as("alex").do("DELETE", "/livetv/recordings/"+itoa(unrated), nil, nil); code != 204 {
		t.Errorf("owner cancels: %d", code)
	}
	if code := w.as("admin").do("DELETE", "/livetv/recordings/"+itoa(w.recs["Grown-up"]), nil, nil); code != 204 {
		t.Errorf("admin removes: %d", code)
	}

	// Series: owner and paused state; someone else can't stop it.
	var start string
	w.h.db.QueryRow(`SELECT start FROM live_programmes WHERE title = 'Another'`).Scan(&start)
	var sched struct {
		Rule *api.RecordingRule `json:"rule"`
	}
	if code := w.as("alex").do("POST", "/livetv/recordings", map[string]any{"channelId": w.channel, "start": start, "series": true}, &sched); code != 201 || sched.Rule == nil {
		t.Fatalf("series: %d", code)
	}
	if code := w.as("norec").do("DELETE", "/livetv/recording-rules/"+itoa(sched.Rule.Id), nil, nil); code != 403 {
		t.Errorf("someone else stops a series: %d", code)
	}
	var rules []api.RecordingRule
	w.as("admin").do("GET", "/livetv/recording-rules", nil, &rules)
	if len(rules) != 1 || *rules[0].OwnerName != "Alex" || !*rules[0].CanDelete || *rules[0].Paused {
		t.Errorf("rules: %+v", rules)
	}
	if w.as("noview").do("GET", "/livetv/recording-rules", nil, &rules); len(rules) != 0 {
		t.Errorf("noview sees others' series: %+v", rules)
	}
	// alex loses recording: his series is paused (kept), shown to admins.
	w.as("admin").do("PATCH", "/users/"+itoa(w.ids["alex"]), map[string]any{"restrictions": map[string]any{"canRecordDVR": false}}, nil)
	h := w.h.handlers
	h.DVR.CanRecord = func(ctx context.Context, id int64) bool {
		u, err := h.Auth.GetUser(ctx, id)
		return err == nil && (u.IsAdmin || u.Restrictions.DVRRecordAllowed())
	}
	if w.as("admin").do("GET", "/livetv/recording-rules", nil, &rules); len(rules) != 1 || !*rules[0].Paused {
		t.Errorf("paused: %+v", rules)
	}
	if code, _ := w.schedule("alex", "Another"); code != 403 {
		t.Errorf("alex schedules without recording: %d", code)
	}
	w.as("admin").do("PATCH", "/users/"+itoa(w.ids["alex"]), map[string]any{"restrictions": map[string]any{}}, nil)

	// Restrictions round-trip; the old canRecord name still works when the new one isn't sent.
	var u api.User
	w.as("admin").do("PATCH", "/users/"+itoa(w.ids["alex"]), map[string]any{"restrictions": map[string]any{"canRecord": false}}, &u)
	if r := u.Restrictions; *r.CanRecordDVR || *r.CanRecord || !*r.CanViewDVR || r.DvrQuotaGb != nil {
		t.Errorf("legacy canRecord: %+v", r)
	}
	w.as("admin").do("PATCH", "/users/"+itoa(w.ids["alex"]), map[string]any{"restrictions": map[string]any{"canRecordDVR": true, "canRecord": false, "dvrQuotaGb": 0}}, &u)
	if r := u.Restrictions; !*r.CanRecordDVR || r.DvrQuotaGb == nil || *r.DvrQuotaGb != 0 {
		t.Errorf("canRecordDVR wins, quota 0: %+v", r)
	}
	if code := w.as("admin").do("PATCH", "/users/"+itoa(w.ids["alex"]), map[string]any{"restrictions": map[string]any{"dvrQuotaGb": -1}}, nil); code != 400 {
		t.Errorf("negative quota: %d", code)
	}
}

func TestDVRQuotaAPI(t *testing.T) {
	w := newDVRWorld(t)
	// alex's recordings take 2 GB (a sparse file).
	big := filepath.Join(w.rec, "TV", "Family", "Family.mkv")
	if err := os.Truncate(big, 2_000_000_000); err != nil {
		t.Fatal(err)
	}
	setQuota := func(q any) {
		r := map[string]any{}
		if q != nil {
			r["dvrQuotaGb"] = q
		}
		if code := w.as("admin").do("PATCH", "/users/"+itoa(w.ids["alex"]), map[string]any{"restrictions": r}, nil); code != 200 {
			t.Fatalf("set quota: %d", code)
		}
	}
	setDefault := func(gb int) {
		if code := w.as("admin").do("PATCH", "/settings", map[string]any{"integrations": map[string]any{"dvrDefaultQuotaGb": gb}}, nil); code != 200 {
			t.Fatalf("default quota: %d", code)
		}
	}

	setQuota(1)
	code, raw := w.schedule("alex", "Later Show")
	if code != 409 || !strings.Contains(string(raw), "quota_exceeded") {
		t.Errorf("over own quota: %d %s", code, raw)
	}
	var u api.DvrUsage
	w.as("alex").do("GET", "/livetv/recordings/usage", nil, &u)
	if u.UsedBytes < 2_000_000_000 || u.QuotaBytes != 1_000_000_000 || !u.OverQuota || u.QuotaSource != api.DvrUsageQuotaSourceUser || u.Recordings != 2 {
		t.Errorf("usage: %+v", u)
	}
	// Someone else isn't affected.
	if code, raw := w.schedule("kid", "Later Show"); code != 201 {
		t.Errorf("kid: %d %s", code, raw)
	}
	setQuota(0) // unlimited overrides the default
	setDefault(1)
	if code, raw := w.schedule("alex", "Another"); code != 201 {
		t.Errorf("unlimited: %d %s", code, raw)
	}
	setQuota(nil) // the default again
	if code, _ := w.schedule("alex", "Unrated Show"); code != 409 {
		t.Errorf("over the default: %d", code)
	}
	w.as("alex").do("GET", "/livetv/recordings/usage", nil, &u)
	if u.QuotaSource != api.DvrUsageQuotaSourceDefault || *u.QuotaGb != 1 {
		t.Errorf("default usage: %+v", u)
	}
	setDefault(0)
	if code, raw := w.schedule("alex", "Unrated Show"); code != 201 {
		t.Errorf("unlimited default: %d %s", code, raw)
	}

	// Admins see everyone.
	var all []api.DvrUsage
	if code := w.as("admin").do("GET", "/admin/dvr/usage", nil, &all); code != 200 || len(all) != 5 {
		t.Fatalf("all usage: %d %d", code, len(all))
	}
	for _, x := range all {
		if x.UserId == w.ids["alex"] && (x.UsedBytes < 2_000_000_000 || *x.DisplayName != "Alex") {
			t.Errorf("alex: %+v", x)
		}
		if x.UserId == w.ids["norec"] && (*x.CanRecord || !*x.CanView) {
			t.Errorf("norec: %+v", x)
		}
	}
	if code := w.as("alex").do("GET", "/admin/dvr/usage", nil, nil); code != 403 {
		t.Errorf("non-admin all usage: %d", code)
	}

	// The quota settings.
	var s api.ServerSettings
	w.as("admin").do("PATCH", "/settings", map[string]any{"integrations": map[string]any{"dvrQuotaAction": "deleteOldest"}}, &s)
	if *s.Integrations.DvrQuotaAction != api.IntegrationSettingsDvrQuotaActionDeleteOldest || *s.Integrations.DvrDefaultQuotaGb != 0 {
		t.Errorf("settings: %+v", s.Integrations)
	}
	if code := w.as("admin").do("PATCH", "/settings", map[string]any{"integrations": map[string]any{"dvrQuotaAction": "explode"}}, nil); code != 400 {
		t.Errorf("bad action: %d", code)
	}

	// The dvr_quota alert is its own switch, kept when the alert list is replaced.
	w.as("admin").do("PATCH", "/settings", map[string]any{"notifications": map[string]any{"dvrQuotaAlert": true}}, &s)
	if !*s.Notifications.DvrQuotaAlert || len(*s.Notifications.Alerts) != 0 {
		t.Errorf("alert on: %+v %v", *s.Notifications.DvrQuotaAlert, *s.Notifications.Alerts)
	}
	w.as("admin").do("PATCH", "/settings", map[string]any{"notifications": map[string]any{"alerts": []string{"task_failed"}}}, &s)
	if !*s.Notifications.DvrQuotaAlert || len(*s.Notifications.Alerts) != 1 || !w.h.store.Get().Notifications.AlertOn(settings.AlertDVRQuota) {
		t.Errorf("alert kept: %v %v", *s.Notifications.DvrQuotaAlert, *s.Notifications.Alerts)
	}
	w.as("admin").do("PATCH", "/settings", map[string]any{"notifications": map[string]any{"dvrQuotaAlert": false}}, &s)
	if *s.Notifications.DvrQuotaAlert || w.h.store.Get().Notifications.AlertOn(settings.AlertDVRQuota) {
		t.Error("alert off")
	}
}

func TestDVRFolderSetting(t *testing.T) {
	w := newDVRWorld(t)
	h := w.as("admin")
	var s api.ServerSettings
	h.do("GET", "/settings", nil, &s)
	if *s.Integrations.DvrFolder != "" || *s.Integrations.DvrFolderDefault != w.rec || !*s.Integrations.DvrFolderInLibrary {
		t.Fatalf("defaults: %q %q %v", *s.Integrations.DvrFolder, *s.Integrations.DvrFolderDefault, *s.Integrations.DvrFolderInLibrary)
	}
	for _, bad := range []string{"recordings", filepath.Join(t.TempDir(), "missing")} {
		if code := h.do("PATCH", "/settings", map[string]any{"integrations": map[string]any{"dvrFolder": bad}}, nil); code != 400 {
			t.Errorf("%q accepted: %d", bad, code)
		}
	}
	// A new folder: the Recorded TV library takes it too, new recordings go there, and the
	// old recordings stay where they are and still count.
	folder := t.TempDir()
	if code := h.do("PATCH", "/settings", map[string]any{"integrations": map[string]any{"dvrFolder": folder + "/"}}, &s); code != 200 {
		t.Fatalf("folder: %d", code)
	}
	if *s.Integrations.DvrFolder != folder || !*s.Integrations.DvrFolderInLibrary {
		t.Errorf("after: %q %v", *s.Integrations.DvrFolder, *s.Integrations.DvrFolderInLibrary)
	}
	l, _ := library.NewStore(w.h.db).Get(context.Background(), w.lib)
	if !slices.Contains(l.Paths, filepath.Join(folder, "TV")) || !slices.Contains(l.Paths, filepath.Join(w.rec, "TV")) {
		t.Errorf("library paths: %v", l.Paths)
	}
	libs, _ := library.NewStore(w.h.db).List(context.Background())
	if len(libs) != 1 {
		t.Errorf("a second recordings library was made: %d", len(libs))
	}
	if got := w.h.handlers.DVR.NewFolder(); got != folder {
		t.Errorf("new recordings go to %q", got)
	}
	if _, err := os.Stat(filepath.Join(w.rec, "TV", "Family", "Family.mkv")); err != nil {
		t.Errorf("old recording moved: %v", err)
	}
	var u api.DvrUsage
	w.as("alex").do("GET", "/livetv/recordings/usage", nil, &u)
	if u.Recordings != 2 || u.UsedBytes != 10 {
		t.Errorf("usage after the move: %+v", u)
	}
	// A folder no library covers is reported.
	if _, err := library.NewStore(w.h.db).Update(context.Background(), w.lib, nil, &[]string{filepath.Join(w.rec, "TV")}, nil); err != nil {
		t.Fatal(err)
	}
	s = api.ServerSettings{}
	w.as("admin").do("GET", "/settings", nil, &s)
	if s.Integrations == nil || *s.Integrations.DvrFolderInLibrary {
		t.Error("uncovered folder reported as in the library")
	}
}
