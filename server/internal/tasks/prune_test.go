package tasks

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Nightly backups are kept for Retention days, and the newest is never deleted.
func TestPruneByDays(t *testing.T) {
	dir := t.TempDir()
	mk := func(name string, age time.Duration) {
		p := filepath.Join(dir, name)
		os.WriteFile(p, []byte("db"), 0o644)
		ts := time.Now().Add(-age)
		os.Chtimes(p, ts, ts)
	}
	day := 24 * time.Hour
	mk("marquee-2026-10-01.db", 1*day)
	mk("marquee-2026-09-25.db", 7*day)
	mk("marquee-2026-09-01.db", 31*day)
	mk("manual-2026-08-01.db", 60*day) // other kinds: the five newest stay
	b := &Backups{Dir: dir, Retention: func() int { return 14 }}
	if n, err := b.Prune(); err != nil || n != 1 {
		t.Fatalf("pruned %d %v", n, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "marquee-2026-09-01.db")); !os.IsNotExist(err) {
		t.Error("31-day-old backup kept")
	}
	// Only old backups left: the newest still stays.
	os.Remove(filepath.Join(dir, "marquee-2026-10-01.db"))
	os.Remove(filepath.Join(dir, "marquee-2026-09-25.db"))
	mk("marquee-2026-06-01.db", 120*day)
	mk("marquee-2026-05-01.db", 150*day)
	if n, _ := b.Prune(); n != 1 {
		t.Errorf("pruned %d, want 1 (keep the newest)", n)
	}
}
