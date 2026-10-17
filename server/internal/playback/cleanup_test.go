package playback

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// The startup sweep of the transcode directory removes stale session folders only: the
// offline downloads kept there for a week (and other users of the directory) survive.
func TestRunKeepsNonSessionDirs(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, newID())
	keep := filepath.Join(dir, "downloads", "abc.mp4")
	for _, p := range []string{filepath.Join(stale, "0.m4s"), keep, filepath.Join(dir, "live", "x.ts")} {
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	(&Manager{TranscodeDir: dir}).Run(ctx)
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Errorf("stale session folder kept: %v", err)
	}
	for _, p := range []string{keep, filepath.Join(dir, "live", "x.ts")} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s removed: %v", p, err)
		}
	}
}
