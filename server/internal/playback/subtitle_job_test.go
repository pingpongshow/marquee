package playback

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A subtitle conversion runs once however many requests ask at the same time, and finishes
// even when nobody waits for it any more (a player that gave up), so the next request finds
// the track ready.
func TestSubtitleConversionSharedAndDetached(t *testing.T) {
	dir := t.TempDir()
	calls := filepath.Join(dir, "calls")
	ffmpeg := filepath.Join(dir, "ffmpeg")
	script := "#!/bin/sh\necho run >> '" + calls + "'\nsleep 0.3\nfor a; do last=\"$a\"; done\necho WEBVTT > \"$last\"\n"
	if err := os.WriteFile(ffmpeg, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	m := &Manager{FFmpeg: ffmpeg}
	out := filepath.Join(dir, "cache", "1-2.vtt")
	args := []string{"-i", "in.mkv", "-map", "0:3", "-c:s", "webvtt", "-f", "webvtt"}
	first := m.convertSubtitle(out, filepath.Dir(out), args)
	second := m.convertSubtitle(out, filepath.Dir(out), args)
	_ = first // the first asker gives up and never reads its result
	select {
	case err := <-second:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("conversion didn't finish")
	}
	if b, _ := os.ReadFile(out); !strings.HasPrefix(string(b), "WEBVTT") {
		t.Errorf("output: %q", b)
	}
	if b, _ := os.ReadFile(calls); strings.Count(string(b), "run") != 1 {
		t.Errorf("FFmpeg ran %d times", strings.Count(string(b), "run"))
	}
	if len(m.subJobs) != 0 {
		t.Errorf("finished job still listed")
	}
}
