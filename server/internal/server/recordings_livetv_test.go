package server

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"marquee/internal/api"
	"marquee/internal/items"
	"marquee/internal/library"
	"marquee/internal/settings"
)

// Recorded Live TV is only under Live TV → Recordings: the recordings libraries aren't
// listed (to admins either, except Settings → Libraries), browsed, searched, or shown on
// Home (Continue Watching included) or in Large files; the recordings still open and play
// by id for who may view them, with their resume point in the Recordings list.
func TestRecordingsOnlyUnderLiveTV(t *testing.T) {
	w := newDVRWorld(t)
	h := w.h
	ctx := context.Background()
	family, grown := w.items["Family"], w.items["Grown-up"]

	// An ordinary library with a film alex is part-way through, as is the Family recording.
	movies, err := library.NewStore(h.db).Create(ctx, "Movies", library.Movies, []string{filepath.Join(h.media, "video", "Movies")}, library.Options{})
	if err != nil {
		t.Fatal(err)
	}
	r, _ := h.db.Exec(`INSERT INTO items (library_id, type, title, sort_title, duration_ms) VALUES (?, 'movie', 'Family Film', 'Family Film', 3600000)`, movies.ID)
	film, _ := r.LastInsertId()
	r, _ = h.db.Exec(`INSERT INTO media_versions (item_id) VALUES (?)`, film)
	v, _ := r.LastInsertId()
	h.db.Exec(`INSERT INTO media_files (version_id, library_id, path, size, mtime, available, video_codec) VALUES (?, ?, ?, 50000000000, 1, 1, 'h264')`,
		v, movies.ID, filepath.Join(h.media, "video", "Movies", "Family Film.mkv"))
	h.db.Exec(`UPDATE media_files SET size = 60000000000, video_codec = 'h264' WHERE library_id = ?`, w.lib)
	h.db.Exec(`UPDATE items SET duration_ms = 3600000 WHERE library_id = ?`, w.lib)
	for _, id := range []int64{film, family} {
		h.db.Exec(`INSERT INTO user_item_state (user_id, item_id, view_offset_ms, last_viewed_at) VALUES (?, ?, 60000, '2026-10-01T10:00:00.000Z')`, w.ids["alex"], id)
	}

	// The library list, for everyone; admins managing libraries see it, flagged.
	for _, who := range []string{"alex", "admin"} {
		var libs []api.Library
		w.as(who).do("GET", "/libraries", nil, &libs)
		ids := []int64{}
		for _, l := range libs {
			ids = append(ids, l.Id)
		}
		if slices.Contains(ids, w.lib) || !slices.Contains(ids, movies.ID) {
			t.Errorf("%s's libraries: %v", who, ids)
		}
		if code := w.as(who).do("GET", "/libraries/"+itoa(w.lib)+"/items", nil, nil); code != 404 {
			t.Errorf("%s browses recordings: %d", who, code)
		}
		var found api.SearchResults
		w.as(who).do("GET", "/search?q=Family", nil, &found)
		for _, g := range found.Groups {
			for _, it := range g.Items {
				if it.Id == family {
					t.Errorf("%s finds the recording in search", who)
				}
			}
		}
	}
	var libs []api.Library
	w.as("admin").do("GET", "/libraries?manage=true", nil, &libs)
	var rec *api.Library
	for i := range libs {
		if libs[i].Id == w.lib {
			rec = &libs[i]
		}
	}
	if rec == nil || rec.Recordings == nil || !*rec.Recordings {
		t.Errorf("admin's Settings → Libraries: %+v", libs)
	}
	if w.as("alex").do("GET", "/libraries?manage=true", nil, &libs); len(libs) != 1 {
		t.Errorf("manage=true for someone not an admin: %d libraries", len(libs))
	}
	if code := w.as("alex").do("GET", "/libraries/"+itoa(w.lib), nil, nil); code != 404 {
		t.Errorf("alex gets the recordings library: %d", code)
	}
	if code := w.as("admin").do("GET", "/libraries/"+itoa(w.lib), nil, nil); code != 200 {
		t.Errorf("admin gets the recordings library to manage it: %d", code)
	}

	// Home: no row lists a recording; Continue Watching has the film, not the recording.
	var hubs []api.Hub
	if code := w.as("alex").do("GET", "/hubs/home", nil, &hubs); code != 200 {
		t.Fatalf("home: %d", code)
	}
	sawFilm := false
	for _, hub := range hubs {
		for _, it := range hub.Items {
			if it.LibraryId == w.lib || it.Id == family || it.Id == grown {
				t.Errorf("home row %s lists the recording %s", hub.Id, it.Title)
			}
			sawFilm = sawFilm || (hub.Id == "continue-watching" && it.Id == film)
		}
		if hub.Entries != nil {
			for _, e := range *hub.Entries {
				if e.Item != nil && e.Item.Id == family {
					t.Errorf("home row %s has the recording", hub.Id)
				}
			}
		}
	}
	if !sawFilm {
		t.Errorf("Continue Watching lacks the film: %+v", hubs)
	}
	var cw api.ContinueWatchingPage
	w.as("alex").do("GET", "/hubs/continue-watching", nil, &cw)
	if len(cw.Entries) != 1 || cw.Entries[0].Item == nil || cw.Entries[0].Item.Id != film {
		t.Errorf("continue watching: %+v", cw.Entries)
	}

	// Large files: never recordings.
	var page api.LargeFilePage
	w.as("admin").do("GET", "/optimize/files", nil, &page)
	if len(page.Items) != 1 || page.Items[0].ItemId != film {
		t.Errorf("large files: %+v", page.Items)
	}
	var fileID int64
	h.db.QueryRow(`SELECT f.id FROM media_files f WHERE f.library_id = ?`, w.lib).Scan(&fileID)
	var qr api.OptimizeQueueResult
	if code := w.as("admin").do("POST", "/optimize/jobs", map[string]any{"presetId": "1080p-hevc-balanced", "fileIds": []int64{fileID}}, &qr); code != 202 || qr.Queued != 0 {
		t.Errorf("queue a recording: %d %+v", code, qr)
	}

	// Still opened and played from the Recordings list, with the resume point there.
	if code := w.as("alex").do("GET", "/items/"+itoa(family), nil, nil); code != 200 {
		t.Errorf("alex opens the recording: %d", code)
	}
	if code := w.as("admin").do("GET", "/items/"+itoa(family), nil, nil); code != 200 {
		t.Errorf("admin opens the recording: %d", code)
	}
	var list []api.Recording
	w.as("alex").do("GET", "/livetv/recordings", nil, &list)
	var got *api.Recording
	for i := range list {
		if list[i].ItemId != nil && *list[i].ItemId == family {
			got = &list[i]
		}
	}
	if got == nil || got.ViewOffsetMs == nil || *got.ViewOffsetMs != 60000 || got.DurationMs == nil || *got.DurationMs != 3600000 || got.Watched == nil || *got.Watched {
		t.Errorf("Family in Recordings: %+v", got)
	}
	// Watch state by id works (as playback's progress does); Recordings shows it.
	if code := w.as("alex").do("POST", "/items/"+itoa(family)+"/watched", nil, nil); code/100 != 2 {
		t.Errorf("alex marks the recording watched: %d", code)
	}
	w.as("alex").do("GET", "/livetv/recordings", nil, &list)
	for _, x := range list {
		if x.ItemId != nil && *x.ItemId == family && (x.Watched == nil || !*x.Watched || *x.ViewOffsetMs != 0) {
			t.Errorf("Family after watching: %+v", x)
		}
	}
	// Playback by id gets past the visibility check (the harness has no player, so stop
	// there): denied callers get 404 before it (TestDVRSharingAndSwitches).
	if err := h.handlers.Items.Visible(ctx, items.Access{UserID: w.ids["alex"], AllowRecordings: true}, family); err != nil {
		t.Errorf("visible by id: %v", err)
	}
	if err := h.handlers.Items.Visible(ctx, items.Access{UserID: w.ids["alex"], HideRecordings: true}, family); err == nil {
		t.Error("visible in listings")
	}

	// A profile limited to some libraries may still open recordings (DVR viewing decides),
	// though they aren't listed for it either.
	var u api.User
	if code := w.as("admin").do("POST", "/users", map[string]any{"username": "limited", "password": "correct horse",
		"restrictions": map[string]any{"libraryIds": []int64{movies.ID}}}, &u); code != 201 {
		t.Fatalf("create limited: %d", code)
	}
	var lr api.AuthResult
	h.do("POST", "/auth/login", map[string]any{"username": "limited", "password": "correct horse", "device": device}, &lr)
	w.tokens["limited"] = lr.Token
	if code := w.as("limited").do("GET", "/items/"+itoa(family), nil, nil); code != 200 {
		t.Errorf("limited opens the recording: %d", code)
	}
	if w.as("limited").do("GET", "/livetv/recordings", nil, &list); !slices.Contains(recordingTitles(list), "Family") {
		t.Errorf("limited's recordings: %v", recordingTitles(list))
	}
}

// GET /livetv/recordings and /livetv/recording-rules: owner=me lists only the caller's,
// owner=all (the default) everyone's they may see.
func TestRecordingsOwnerFilter(t *testing.T) {
	w := newDVRWorld(t)
	if code, raw := w.schedule("admin", "Later Show"); code != 201 {
		t.Fatalf("schedule: %d %s", code, raw)
	}
	var start string
	w.h.db.QueryRow(`SELECT start FROM live_programmes WHERE title = 'Another'`).Scan(&start)
	for _, who := range []string{"alex", "admin"} {
		if code := w.as(who).do("POST", "/livetv/recordings", map[string]any{"channelId": w.channel, "start": start, "series": true}, nil); code != 201 && who == "alex" {
			t.Fatalf("%s series: %d", who, code)
		}
	}
	var list []api.Recording
	for _, c := range []struct {
		query string
		want  []string
	}{
		{"", []string{"Family", "Grown-up", "Later Show"}},
		{"?owner=all", []string{"Family", "Grown-up", "Later Show"}},
		{"?owner=me", []string{"Family", "Grown-up"}},
	} {
		if code := w.as("alex").do("GET", "/livetv/recordings"+c.query, nil, &list); code != 200 {
			t.Fatalf("list %s: %d", c.query, code)
		}
		got := recordingTitles(list)
		// alex's series may have scheduled "Another" too; it's his.
		got = slices.DeleteFunc(got, func(s string) bool { return s == "Another" })
		if !slices.Equal(got, c.want) {
			t.Errorf("alex %s: %v, want %v", c.query, got, c.want)
		}
	}
	w.as("admin").do("GET", "/livetv/recordings?owner=me", nil, &list)
	for _, x := range list {
		if *x.UserId != w.ids["admin"] {
			t.Errorf("admin's own list has %s by %d", x.Title, *x.UserId)
		}
	}
	var rules []api.RecordingRule
	w.as("admin").do("GET", "/livetv/recording-rules", nil, &rules)
	all := len(rules)
	w.as("admin").do("GET", "/livetv/recording-rules?owner=me", nil, &rules)
	for _, r := range rules {
		if *r.UserId != w.ids["admin"] {
			t.Errorf("admin's own series has one by %d", *r.UserId)
		}
	}
	if all < 1 || len(rules) >= all {
		t.Errorf("series: all %d, mine %d", all, len(rules))
	}
}

// Large files → History: the kept originals' count and size, and the button deleting them
// (only optimizer originals); saving keepOriginals off deletes them too.
func TestDeleteKeptOriginalsAPI(t *testing.T) {
	h := newHarness(t)
	var res api.AuthResult
	h.do("POST", "/setup", map[string]any{"serverName": "T", "username": "admin", "password": "correct horse", "device": device}, &res)
	h.token = res.Token
	trash := filepath.Join(h.media, "video", "Movies", library.TrashDir, "2026-10-01")
	os.MkdirAll(trash, 0o755)
	put := func(name string, n int) string {
		p := filepath.Join(trash, name)
		os.WriteFile(p, make([]byte, n), 0o644)
		return p
	}
	a, b, other := put("A.mkv", 1000), put("B.mkv", 500), put("Removed.mkv", 10)
	for _, p := range []string{a, b} {
		h.db.Exec(`INSERT INTO optimize_jobs (title, path, preset_id, preset_name, preset, state, original, finished_at)
			VALUES ('X', '/m/x.mkv', 'p', 'P', '{}', 'done', ?, '2026-10-02T00:00:00Z')`, p)
	}
	h.store.Update(context.Background(), func(s *settings.Settings) error { s.Optimize.KeepOriginals = true; return nil })

	var jobs api.OptimizeJobPage
	if code := h.do("GET", "/optimize/jobs?view=history", nil, &jobs); code != 200 || jobs.KeptOriginals == nil || *jobs.KeptOriginals != 2 ||
		*jobs.KeptOriginalsBytes != 1500 || !jobs.Items[0].CanRestore {
		t.Fatalf("history: %d %+v", code, jobs)
	}
	var out struct {
		Deleted    int   `json:"deleted"`
		FreedBytes int64 `json:"freedBytes"`
	}
	if code := h.do("POST", "/optimize/originals/delete", nil, &out); code != 200 || out.Deleted != 2 || out.FreedBytes != 1500 {
		t.Fatalf("delete: %d %+v", code, out)
	}
	for _, p := range []string{a, b} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s still there", p)
		}
	}
	if _, err := os.Stat(other); err != nil {
		t.Error("another file in the trash was deleted")
	}
	h.do("GET", "/optimize/jobs?view=history", nil, &jobs)
	if *jobs.KeptOriginals != 0 || jobs.Items[0].CanRestore || !*jobs.Items[0].OriginalDeleted {
		t.Errorf("after delete: %+v", jobs)
	}

	// Saving keepOriginals off deletes originals kept since.
	c := put("C.mkv", 100)
	h.db.Exec(`INSERT INTO optimize_jobs (title, path, preset_id, preset_name, preset, state, original) VALUES ('C', '/m/c.mkv', 'p', 'P', '{}', 'done', ?)`, c)
	off := false
	body := map[string]any{"presets": []any{}, "defaultPreset": "1080p-hevc-balanced", "concurrency": 1, "minSavingsPercent": 20,
		"pauseWhileWatching": true, "windowOnly": false, "paused": false, "keepOriginals": off}
	if code := h.do("PUT", "/optimize/settings", body, nil); code != 200 {
		t.Fatalf("settings: %d", code)
	}
	if _, err := os.Stat(c); !os.IsNotExist(err) {
		t.Error("saving keepOriginals off kept the original")
	}

	// Admins only.
	var u api.User
	h.do("POST", "/users", map[string]any{"username": "pat", "password": "correct horse"}, &u)
	var lr api.AuthResult
	h.do("POST", "/auth/login", map[string]any{"username": "pat", "password": "correct horse", "device": device}, &lr)
	h.token = lr.Token
	if code := h.do("POST", "/optimize/originals/delete", nil, nil); code != 403 {
		t.Errorf("not an admin: %d", code)
	}
}
