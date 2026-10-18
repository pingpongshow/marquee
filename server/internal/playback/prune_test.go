package playback

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// segmentsLeft writes segments 0..n-1, prunes, and reports which remain.
func segmentsLeft(t *testing.T, n, lastRequest, viewer int) map[int]bool {
	t.Helper()
	dir := t.TempDir()
	for k := 0; k < n; k++ {
		if err := os.WriteFile(filepath.Join(dir, strconv.Itoa(k)+".m4s"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	tr := &Transcoder{Job: Job{Dir: dir}, lastRequest: lastRequest}
	tr.mu.Lock()
	tr.pruneLocked(viewer)
	tr.mu.Unlock()
	left := map[int]bool{}
	for k := 0; k < n; k++ {
		if exists(filepath.Join(dir, strconv.Itoa(k)+".m4s")) {
			left[k] = true
		}
	}
	return left
}

// A player that buffers far ahead (hls.js fetched a copied episode to the end) must not
// lose the segments it is about to play: pruning follows the playhead, not the last request.
func TestPruneKeepsSegmentsAheadOfPlayhead(t *testing.T) {
	left := segmentsLeft(t, 143, 142, 115)
	for k := 105; k < 143; k++ {
		if !left[k] {
			t.Errorf("segment %d removed; the viewer is at 115", k)
		}
	}
	for k := 85; k < 105; k++ {
		if left[k] {
			t.Errorf("segment %d kept; it is more than %d behind the viewer", k, pruneBehind)
		}
	}
}

// Without a playhead (or with one ahead of the requests) the last request decides, as before.
func TestPruneFallsBackToLastRequest(t *testing.T) {
	for _, viewer := range []int{-1, 60} {
		left := segmentsLeft(t, 60, 50, viewer)
		for k := 20; k < 40; k++ {
			if left[k] {
				t.Errorf("viewer %d: segment %d kept", viewer, k)
			}
		}
		for k := 40; k < 60; k++ {
			if !left[k] {
				t.Errorf("viewer %d: segment %d removed", viewer, k)
			}
		}
	}
}

// A playhead that stopped moving (a client no longer reporting progress) can't hold on to
// a whole film's segments.
func TestPruneIgnoresStalePlayhead(t *testing.T) {
	left := segmentsLeft(t, 400, 390, 5)
	ref := 390 - maxPlayheadLag
	for k := ref - pruneBehind - pruneWindow; k < ref-pruneBehind; k++ {
		if left[k] {
			t.Errorf("segment %d kept behind a stale playhead", k)
		}
	}
	if !left[ref-pruneBehind] {
		t.Errorf("segment %d removed", ref-pruneBehind)
	}
}

// The session's playhead is the plan segment at the reported position.
func TestSessionPlayhead(t *testing.T) {
	s := &Session{positionMS: 25_000}
	ph := s.playhead([]float64{0, 10.01, 20.02, 30.03})
	if got := ph(); got != 2 {
		t.Fatalf("playhead at 25 s = %d, want 2", got)
	}
	s.mu.Lock()
	s.positionMS = 31_000
	s.mu.Unlock()
	if got := ph(); got != 3 {
		t.Fatalf("playhead at 31 s = %d, want 3", got)
	}
}

// The throttle loop asks for the playhead without holding the transcoder's lock (the
// session's lock is taken before the transcoder's elsewhere), and prunes with it.
func TestThrottleLoopPrunesBehindPlayhead(t *testing.T) {
	dir := t.TempDir()
	for k := 0; k < 60; k++ {
		os.WriteFile(filepath.Join(dir, strconv.Itoa(k)+".m4s"), []byte("x"), 0o644)
	}
	tr := &Transcoder{Job: Job{Dir: dir}, lastRequest: 59}
	tr.Playhead = func() int {
		if !tr.mu.TryLock() {
			t.Error("Playhead called with t.mu held")
			return -1
		}
		tr.mu.Unlock()
		return 30
	}
	tr.quit = make(chan struct{})
	done := make(chan struct{})
	go func() { tr.throttleLoop(); close(done) }()
	deadline := time.Now().Add(5 * time.Second)
	for exists(filepath.Join(dir, "19.m4s")) && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	close(tr.quit)
	<-done
	if exists(filepath.Join(dir, "19.m4s")) {
		t.Error("segment 19 not pruned (viewer at 30)")
	}
	for k := 20; k < 60; k++ {
		if !exists(filepath.Join(dir, strconv.Itoa(k)+".m4s")) {
			t.Errorf("segment %d removed (viewer at 30)", k)
		}
	}
}
