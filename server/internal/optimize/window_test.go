package optimize

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"marquee/internal/settings"
)

// "Only during the maintenance window": queued files wait outside the window and start
// once it opens (03:00–05:00 here, with the clock moved from noon into the window).
func TestWindowOnlyRunsInsideTheWindow(t *testing.T) {
	e := newEnv(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := filepath.Join(e.dir, "media", "W.mkv")
	clip(t, p, "4")
	fi, _ := os.Stat(p)
	_, file := e.add(fileSpec{path: p, size: fi.Size(), w: 320, h: 240, codec: "mpeg2video", available: true})
	ff, log, _ := fakeFFmpeg(t, e.dir)

	noon := time.Date(2026, 10, 16, 12, 0, 0, 0, time.Local)
	var clock atomic.Int64
	clock.Store(noon.UnixNano())
	s := &Service{DB: e.db, Settings: e.settings, FFmpeg: ff, LookPath: noLook,
		Now: func() time.Time { return time.Unix(0, clock.Load()) }, Tick: 20 * time.Millisecond}
	e.set(func(c *settings.Settings) {
		c.Tasks.MaintenanceWindowStart, c.Tasks.MaintenanceWindowHours = "03:00", 2
		c.Optimize.WindowOnly = true
		c.Optimize.Presets = []settings.OptimizePreset{{ID: "tiny", Name: "Tiny", Codec: "hevc", Quality: "small", Audio: "aac_stereo", HDR: "keep"}}
	})
	if q, _, err := s.Enqueue(ctx, []int64{file}, "tiny"); err != nil || q != 1 {
		t.Fatalf("enqueue: %d %v", q, err)
	}
	go s.Run(ctx)
	time.Sleep(200 * time.Millisecond)
	if n := len(lines(log)); n != 0 {
		t.Fatalf("started outside the window: %v", lines(log))
	}
	if r := s.HoldReason(); !strings.Contains(r, "maintenance window") {
		t.Errorf("hold reason: %q", r)
	}
	// 03:30 the next day: inside the window, the queued file starts.
	clock.Store(time.Date(2026, 10, 17, 3, 30, 0, 0, time.Local).UnixNano())
	waitFor(t, "the job to start in the window", func() bool { return len(lines(log)) == 1 })
}
