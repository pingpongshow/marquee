package optimize

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"marquee/internal/settings"
)

// Turning "Optimize large files" off in Scheduled Tasks holds the queue; back on, it runs.
func TestDisabledTaskHoldsTheQueue(t *testing.T) {
	e := newEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := filepath.Join(e.dir, "media", "D.mkv")
	clip(t, p, "4")
	fi, _ := os.Stat(p)
	_, file := e.add(fileSpec{path: p, size: fi.Size(), w: 320, h: 240, codec: "mpeg2video", available: true})
	ff, log, _ := fakeFFmpeg(t, e.dir)
	s := &Service{DB: e.db, Settings: e.settings, FFmpeg: ff, LookPath: noLook, Tick: 20 * time.Millisecond}
	e.set(func(c *settings.Settings) {
		c.Tasks.Disabled = []string{TaskID}
		c.Optimize.Presets = []settings.OptimizePreset{{ID: "tiny", Name: "Tiny", Codec: "hevc", Quality: "small", Audio: "aac_stereo", HDR: "keep"}}
	})
	if q, _, err := s.Enqueue(ctx, []int64{file}, "tiny"); err != nil || q != 1 {
		t.Fatalf("enqueue: %d %v", q, err)
	}
	go s.Run(ctx)
	time.Sleep(200 * time.Millisecond)
	if n := len(lines(log)); n != 0 {
		t.Fatalf("started while turned off: %v", lines(log))
	}
	if r := s.HoldReason(); r != HoldDisabled {
		t.Errorf("hold reason: %q", r)
	}
	if st := s.QueueState(ctx); st != HoldDisabled+" · 1 queued" {
		t.Errorf("queue state: %q", st)
	}
	e.set(func(c *settings.Settings) {
		c.Tasks.Disabled = slices.DeleteFunc(c.Tasks.Disabled, func(id string) bool { return id == TaskID })
	})
	s.Wake()
	waitFor(t, "the job to start once turned on", func() bool { return len(lines(log)) == 1 })
}
