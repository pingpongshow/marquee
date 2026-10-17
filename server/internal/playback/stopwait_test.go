package playback

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A segment request still waiting when its session ends (the player switched tracks or
// quality, which starts a new session) gets errStopped, not a transcoder failure: failures
// raise a "Playback failed" alert to admins.
func TestSegmentWaitEndsWithStop(t *testing.T) {
	dir := t.TempDir()
	ff := filepath.Join(dir, "ffmpeg")
	os.WriteFile(ff, []byte("#!/bin/sh\nexec sleep 30\n"), 0o755) // never produces anything
	tr := &Transcoder{FFmpeg: ff, Job: Job{Input: "in.mkv", Decision: Decision{Method: Transcode, VideoCodec: "h264"}, VideoIndex: 0,
		AudioIndex: -1, SubIndex: -1, Dir: filepath.Join(dir, "s")}, Encoders: []string{"software"}, TotalSegments: 10, ThrottleAhead: 20}
	done := make(chan error, 1)
	go func() {
		_, err := tr.Segment(context.Background(), 0)
		done <- err
	}()
	time.Sleep(300 * time.Millisecond)
	tr.Stop()
	select {
	case err := <-done:
		if !errors.Is(err, errStopped) {
			t.Fatalf("got %v, want errStopped", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("still waiting after stop")
	}
}
