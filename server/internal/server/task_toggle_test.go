package server

import (
	"context"
	"slices"
	"testing"

	"marquee/internal/api"
	"marquee/internal/tasks"
)

// Tasks are turned on and off through PATCH /tasks/{id} or the settings (tasks.disabled,
// unknown ids dropped); the list reports enabled. The optimizer's keepOriginals is off by
// default, saved when sent and unchanged when left out.
func TestTaskToggleAndKeepOriginalsAPI(t *testing.T) {
	h := newHarness(t)
	var res api.AuthResult
	h.do("POST", "/setup", map[string]any{"serverName": "T", "username": "admin", "password": "correct horse", "device": device}, &res)
	h.token = res.Token
	noop := func(context.Context) (string, error) { return "", nil }
	h.handlers.Tasks.Register(tasks.Task{ID: "a", Name: "A", Run: noop})
	h.handlers.Tasks.Register(tasks.Task{ID: "b", Name: "B", Run: noop})

	enabled := func() map[string]bool {
		var list []api.TaskInfo
		if code := h.do("GET", "/tasks", nil, &list); code != 200 {
			t.Fatalf("tasks: %d", code)
		}
		out := map[string]bool{}
		for _, ti := range list {
			out[ti.Id] = ti.Enabled != nil && *ti.Enabled
		}
		return out
	}
	if e := enabled(); !e["a"] || !e["b"] {
		t.Fatalf("default: %v", e)
	}
	var ti api.TaskInfo
	if code := h.do("PATCH", "/tasks/a", map[string]any{"enabled": false}, &ti); code != 200 || ti.Enabled == nil || *ti.Enabled {
		t.Fatalf("turn off: %d %+v", code, ti)
	}
	if code := h.do("PATCH", "/tasks/nope", map[string]any{"enabled": false}, nil); code != 404 {
		t.Errorf("unknown task: %d", code)
	}
	var s api.ServerSettings
	h.do("GET", "/settings", nil, &s)
	if s.Tasks.Disabled == nil || !slices.Equal(*s.Tasks.Disabled, []string{"a"}) {
		t.Fatalf("settings: %v", s.Tasks.Disabled)
	}
	if code := h.do("PATCH", "/settings", map[string]any{"tasks": map[string]any{"disabled": []string{"b", "nope", "b"}}}, nil); code != 200 {
		t.Fatalf("settings update: %d", code)
	}
	if got := h.store.Get().Tasks.Disabled; !slices.Equal(got, []string{"b"}) {
		t.Fatalf("stored: %v", got)
	}
	if e := enabled(); !e["a"] || e["b"] {
		t.Fatalf("after settings: %v", e)
	}
	// Run now still works for a task that's off.
	if code := h.do("POST", "/tasks/b/run", nil, nil); code != 202 {
		t.Errorf("run now while off: %d", code)
	}

	var cfg api.OptimizeConfig
	h.do("GET", "/optimize/settings", nil, &cfg)
	if cfg.Settings.KeepOriginals == nil || *cfg.Settings.KeepOriginals {
		t.Fatalf("keepOriginals default: %v", cfg.Settings.KeepOriginals)
	}
	body := map[string]any{"presets": []any{}, "defaultPreset": "1080p-hevc-balanced", "concurrency": 1, "minSavingsPercent": 20,
		"pauseWhileWatching": true, "windowOnly": false, "paused": false, "keepOriginals": true}
	if code := h.do("PUT", "/optimize/settings", body, &cfg); code != 200 || !*cfg.Settings.KeepOriginals || !h.store.Get().Optimize.KeepOriginals {
		t.Fatalf("keepOriginals on: %d %v", code, cfg.Settings.KeepOriginals)
	}
	delete(body, "keepOriginals")
	if code := h.do("PUT", "/optimize/settings", body, &cfg); code != 200 || !h.store.Get().Optimize.KeepOriginals {
		t.Fatalf("keepOriginals left out: %d %v", code, h.store.Get().Optimize.KeepOriginals)
	}
}
