package tasks

import (
	"errors"
	"testing"
)

// A library queued while it is being scanned (the file watcher, the optimizer swapping a
// file in, a VOD save) is scanned again once the running scan ends, since that scan may
// already have walked past the change; a cancelled scan doesn't come back.
func TestQueueDuringScanRescansAfter(t *testing.T) {
	s := &Scans{}
	s.Queue(1)
	if id, ok := s.next(); !ok || id != 1 {
		t.Fatalf("next: %d %v", id, ok)
	}
	s.mu.Lock()
	s.status[1].State = Scanning
	s.mu.Unlock()

	s.Queue(1) // files changed mid-scan
	s.Queue(1) // more than once: one rescan
	s.finish(1, "done", nil)
	if st := s.Status(1).State; st != Queued {
		t.Fatalf("after the scan: %s, want queued", st)
	}
	if id, ok := s.next(); !ok || id != 1 {
		t.Fatalf("rescan not queued: %d %v", id, ok)
	}
	if _, ok := s.next(); ok {
		t.Fatal("queued twice")
	}

	// Cancelled while a rescan was pending: it stays idle.
	s.mu.Lock()
	s.status[1].State = Scanning
	s.mu.Unlock()
	s.Queue(1)
	s.Cancel(1)
	s.finish(1, "", errors.New("context canceled"))
	if st := s.Status(1).State; st != Idle {
		t.Fatalf("after cancel: %s, want idle", st)
	}
	if _, ok := s.next(); ok {
		t.Fatal("cancelled scan was queued again")
	}
}
