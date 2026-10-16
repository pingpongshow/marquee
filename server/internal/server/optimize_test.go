package server

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"marquee/internal/api"
	"marquee/internal/library"
)

// The library optimizer over the API: settings and custom presets, the Large files list
// with filters and estimates, queueing by selection and by filter, the queue and its
// actions; admins only.
func TestOptimizeAPI(t *testing.T) {
	h := newHarness(t)
	var res api.AuthResult
	h.do("POST", "/setup", map[string]any{"serverName": "T", "username": "admin", "password": "correct horse", "device": device}, &res)
	h.token = res.Token
	ctx := context.Background()

	// Settings: defaults, every preset; a custom preset saved; bad values refused.
	var cfg api.OptimizeConfig
	if code := h.do("GET", "/optimize/settings", nil, &cfg); code != 200 || cfg.Settings.Concurrency != 1 || cfg.Settings.MinSavingsPercent != 20 ||
		!cfg.Settings.PauseWhileWatching || len(cfg.Presets) != 4 || !*cfg.Presets[0].BuiltIn {
		t.Fatalf("settings: %d %+v", code, cfg)
	}
	custom := map[string]any{"id": "c1", "name": "Small 720p", "maxHeight": 720, "codec": "hevc", "quality": "small", "audio": "aac_stereo",
		"dropCommentary": true, "imageSubtitles": "drop", "hdr": "tonemap"}
	body := map[string]any{"presets": []any{custom}, "defaultPreset": "c1", "concurrency": 2, "minSavingsPercent": 30,
		"pauseWhileWatching": true, "windowOnly": false, "paused": true}
	if code := h.do("PUT", "/optimize/settings", body, &cfg); code != 200 || len(cfg.Presets) != 5 || cfg.Settings.DefaultPreset != "c1" ||
		!cfg.Settings.Paused || len(cfg.Settings.Presets) != 1 {
		t.Fatalf("update: %d %+v", code, cfg)
	}
	if h.store.Get().Optimize.Concurrency != 2 {
		t.Error("not stored")
	}
	bad := map[string]any{"presets": []any{}, "defaultPreset": "1080p-hevc-balanced", "concurrency": 9, "minSavingsPercent": 20,
		"pauseWhileWatching": true, "windowOnly": false, "paused": false}
	if code := h.do("PUT", "/optimize/settings", bad, nil); code != 400 {
		t.Errorf("concurrency 9: %d", code)
	}
	custom["codec"] = "av1"
	body["presets"] = []any{custom}
	if code := h.do("PUT", "/optimize/settings", body, nil); code != 400 {
		t.Errorf("AV1 without an encoder: %d", code)
	}

	lib, err := library.NewStore(h.db).Create(ctx, "Movies", library.Movies, []string{filepath.Join(h.media, "video", "Movies")}, library.Options{})
	if err != nil {
		t.Fatal(err)
	}
	add := func(title string, size int64, w, ht int, codec string) int64 {
		r, _ := h.db.Exec(`INSERT INTO items (library_id, type, title, sort_title) VALUES (?, 'movie', ?, ?)`, lib.ID, title, title)
		item, _ := r.LastInsertId()
		r, _ = h.db.Exec(`INSERT INTO media_versions (item_id) VALUES (?)`, item)
		v, _ := r.LastInsertId()
		r, err := h.db.Exec(`INSERT INTO media_files (version_id, library_id, path, size, mtime, duration_ms, width, height, video_codec, bitrate_kbps, available)
			VALUES (?, ?, ?, ?, 1, 7200000, ?, ?, ?, ?, 1)`, v, lib.ID, filepath.Join(h.media, "video", "Movies", title+".mkv"), size, w, ht, codec, size*8/7200/1000)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := r.LastInsertId()
		return id
	}
	uhd := add("UHD", 60e9, 3840, 2160, "hevc")
	vc1 := add("VC1", 25e9, 1920, 1080, "vc1")
	add("Small", 2e9, 1280, 720, "h264")

	var page api.LargeFilePage
	if code := h.do("GET", "/optimize/files?minSizeGb=10&presetId=1080p-hevc-balanced", nil, &page); code != 200 || page.Total != 2 ||
		page.Items[0].FileId != uhd || page.Items[1].FileId != vc1 || page.TotalSize != 85e9 || page.EstimatedSize >= page.TotalSize {
		t.Fatalf("list: %d %+v", code, page)
	}
	if page.Items[0].EstimatedSize >= page.Items[0].Size || page.Items[0].Width != 3840 {
		t.Errorf("row: %+v", page.Items[0])
	}
	if code := h.do("GET", "/optimize/files?codecs=vc1,mpeg2video&resolution=1080", nil, &page); code != 200 || page.Total != 1 || page.Items[0].FileId != vc1 {
		t.Errorf("codec filter: %d %+v", code, page)
	}
	if code := h.do("GET", "/optimize/files?presetId=nope", nil, nil); code != 400 {
		t.Errorf("unknown preset: %d", code)
	}

	// Queue one by selection, then everything over 1 GB by filter (the first is skipped).
	var qr api.OptimizeQueueResult
	if code := h.do("POST", "/optimize/jobs", map[string]any{"presetId": "c1", "fileIds": []int64{vc1}}, &qr); code != 202 || qr.Queued != 1 {
		t.Fatalf("queue: %d %+v", code, qr)
	}
	if code := h.do("POST", "/optimize/jobs", map[string]any{"presetId": "1080p-hevc-balanced", "filter": map[string]any{"minSizeGb": 1}}, &qr); code != 202 ||
		qr.Queued != 2 || qr.Skipped != 1 {
		t.Fatalf("queue by filter: %d %+v", code, qr)
	}
	if code := h.do("POST", "/optimize/jobs", map[string]any{"presetId": "nope", "fileIds": []int64{vc1}}, nil); code != 400 {
		t.Errorf("unknown preset: %d", code)
	}
	var jobs api.OptimizeJobPage
	if code := h.do("GET", "/optimize/jobs", nil, &jobs); code != 200 || jobs.Total != 3 || jobs.Items[0].State != api.OptimizeJobStateQueued ||
		jobs.Items[0].PresetName != "Small 720p" {
		t.Fatalf("jobs: %d %+v", code, jobs)
	}
	if code := h.do("GET", "/optimize/files?minSizeGb=10", nil, &page); code != 200 || page.Items[0].JobState == nil || *page.Items[0].JobState != api.OptimizeJobStateQueued {
		t.Errorf("job state in the list: %+v", page.Items[0])
	}
	first := jobs.Items[0].Id
	var raw json.RawMessage
	if code := h.do("POST", "/optimize/jobs/"+itoa(first)+"/restore", nil, &raw); code != 409 {
		t.Errorf("restore a queued job: %d %s", code, raw)
	}
	if code := h.do("POST", "/optimize/jobs/"+itoa(first)+"/cancel", nil, nil); code != 204 {
		t.Errorf("cancel: %d", code)
	}
	if code := h.do("POST", "/optimize/jobs/"+itoa(first)+"/cancel", nil, &raw); code != 409 || !strings.Contains(string(raw), "bad_state") {
		t.Errorf("cancel twice: %d %s", code, raw)
	}
	if code := h.do("POST", "/optimize/jobs/"+itoa(first)+"/retry", nil, nil); code != 204 {
		t.Errorf("retry: %d", code)
	}
	if code := h.do("POST", "/optimize/jobs/999/cancel", nil, nil); code != 404 {
		t.Errorf("no such job: %d", code)
	}
	var cleared struct{ Cleared int }
	if code := h.do("POST", "/optimize/jobs/clear", nil, &cleared); code != 200 || cleared.Cleared != 0 {
		t.Errorf("clear: %d %+v", code, cleared)
	}
	if code := h.do("GET", "/optimize/jobs?view=history", nil, &jobs); code != 200 || jobs.Total != 0 {
		t.Errorf("history: %d %+v", code, jobs)
	}

	// Others can't see or change anything.
	h.do("POST", "/users", map[string]any{"username": "viewer", "password": "correct horse"}, nil)
	var vres api.AuthResult
	h.do("POST", "/auth/login", map[string]any{"username": "viewer", "password": "correct horse", "device": device}, &vres)
	h.token = vres.Token
	for _, c := range []struct{ method, path string }{{"GET", "/optimize/files"}, {"GET", "/optimize/jobs"}, {"POST", "/optimize/jobs"},
		{"POST", "/optimize/jobs/clear"}, {"POST", "/optimize/jobs/" + itoa(first) + "/cancel"}, {"GET", "/optimize/settings"}, {"PUT", "/optimize/settings"}} {
		if code := h.do(c.method, c.path, map[string]any{"presetId": "c1", "fileIds": []int64{uhd}, "presets": []any{}, "defaultPreset": "c1",
			"concurrency": 1, "minSavingsPercent": 20}, nil); code != 403 {
			t.Errorf("viewer %s %s: %d", c.method, c.path, code)
		}
	}
	h.token = ""
	if code := h.do("GET", "/optimize/jobs", nil, nil); code != 401 {
		t.Errorf("signed out: %d", code)
	}
}
