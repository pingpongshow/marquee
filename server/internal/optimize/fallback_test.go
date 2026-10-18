package optimize

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// When the all-GPU route fails on a file, NVENC is tried again with the filters on the CPU
// before a slower encoder gets the job.
func TestFullGPUFallsBackToCPUFilters(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "calls")
	ffmpeg := filepath.Join(dir, "ffmpeg")
	// Fails when frames stay on the GPU; otherwise "converts" (writes the output, the last argument).
	script := "#!/bin/sh\necho \"$*\" >> '" + log + "'\ncase \"$*\" in *hwaccel_output_format*) echo 'Function not implemented' >&2; exit 1;; esac\n" +
		"for a; do last=\"$a\"; done\necho x > \"$last\"\n"
	if err := os.WriteFile(ffmpeg, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	e := newEnv(t)
	s := &Service{DB: e.db, Settings: e.settings, FFmpeg: ffmpeg, LookPath: noLook, Encoders: func() []string { return []string{"nvenc", "software"} }}
	src := Source{Path: "/m/Film.mkv", DurationMS: 60_000, Width: 1920, Height: 1080, VideoIndex: 0, VideoCodec: "h264", PixFmt: "yuv420p"}
	plan, err := NewPlan(src, preset("1080p-hevc-balanced"))
	if err != nil {
		t.Fatal(err)
	}
	if !plan.FullGPU(src, "nvenc") {
		t.Fatal("expected the all-GPU route")
	}
	out := filepath.Join(dir, "out.part")
	if err := s.encode(context.Background(), &job{id: 1}, src, plan, out); err != nil {
		t.Fatalf("encode: %v", err)
	}
	b, _ := os.ReadFile(log)
	calls := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(calls) != 2 {
		t.Fatalf("calls: %q", calls)
	}
	if !strings.Contains(calls[0], "hwaccel_output_format") || strings.Contains(calls[1], "hwaccel_output_format") ||
		!strings.Contains(calls[1], "hevc_nvenc") || !strings.Contains(calls[1], "-hwaccel cuda") {
		t.Errorf("second try should be NVENC with CPU filters: %q", calls)
	}
}
