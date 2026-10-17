package vodsave

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"marquee/internal/settings"
)

func (e *env) addRow(t *testing.T, state, finished string, path string) {
	t.Helper()
	var fin any
	if finished != "" {
		fin = finished
	}
	if _, err := e.s.DB.Exec(`INSERT INTO vod_saves (source_id, kind, vod_id, source_key, file, title, target_path, quality, state,
		queue_pos, created_at, finished_at) VALUES ('xc', 'movie', 1, 'k', '1.mkv', ?, ?, 'original', ?, 1, ?, ?)`,
		state+" "+finished, path, state, time.Now().UTC().Format(time.RFC3339), fin); err != nil {
		t.Fatal(err)
	}
}

// Finished saves are kept 5 days and at most 10 (the newest); failed ones' partial files
// go with them; queued ones stay.
func TestHistoryPruned(t *testing.T) {
	e := newEnv(t, "", nil)
	ctx := context.Background()
	at := func(d time.Duration) string { return time.Now().UTC().Add(-d).Format(time.RFC3339) }
	for i := 0; i < 12; i++ {
		e.addRow(t, Done, at(time.Duration(i)*time.Hour), filepath.Join(e.movies, "x.mkv"))
	}
	part := filepath.Join(e.movies, "Old (2000)", "Old (2000).mkv")
	os.MkdirAll(filepath.Dir(part), 0o755)
	os.WriteFile(part+".part", []byte("x"), 0o644)
	e.addRow(t, Failed, at(6*24*time.Hour), part)
	e.addRow(t, Queued, "", filepath.Join(e.movies, "q.mkv"))

	e.s.pruneHistory(ctx)
	var finished, queued int
	e.s.DB.QueryRow(`SELECT COUNT(*) FROM vod_saves WHERE state IN ('done','failed','cancelled','skipped')`).Scan(&finished)
	e.s.DB.QueryRow(`SELECT COUNT(*) FROM vod_saves WHERE state = 'queued'`).Scan(&queued)
	if finished != HistoryMax || queued != 1 {
		t.Fatalf("finished %d (want %d), queued %d", finished, HistoryMax, queued)
	}
	var oldest string
	e.s.DB.QueryRow(`SELECT MIN(finished_at) FROM vod_saves WHERE state = 'done'`).Scan(&oldest)
	if oldest < at(10*time.Hour) {
		t.Errorf("an older save was kept over a newer one: %s", oldest)
	}
	if _, err := os.Stat(part + ".part"); !os.IsNotExist(err) {
		t.Errorf("the failed save's partial file is still there: %v", err)
	}
}

// With downloads limited to the maintenance window, queued saves wait outside it.
func TestWindowOnlyHolds(t *testing.T) {
	e := newEnv(t, "", func(v *settings.VODSave) { v.WindowOnly = true })
	ctx := context.Background()
	closed := time.Now().Add(2 * time.Hour).Format("15:04")
	e.s.Settings.Update(ctx, func(s *settings.Settings) error {
		s.Tasks.MaintenanceWindowStart, s.Tasks.MaintenanceWindowHours = closed, 1
		return nil
	})
	if !e.s.outsideWindow() {
		t.Fatal("outside the window not seen")
	}
	e.addRow(t, Queued, "", filepath.Join(e.movies, "q.mkv"))
	list, err := e.s.List(ctx)
	if err != nil || len(list) != 1 || list[0].PauseReason != "Waiting for the maintenance window" {
		t.Fatalf("list: %+v %v", list, err)
	}
	open := time.Now().Add(-10 * time.Minute).Format("15:04")
	e.s.Settings.Update(ctx, func(s *settings.Settings) error {
		s.Tasks.MaintenanceWindowStart = open
		return nil
	})
	if e.s.outsideWindow() {
		t.Error("inside the window seen as outside")
	}
	e.s.Settings.Update(ctx, func(s *settings.Settings) error {
		s.Tasks.MaintenanceWindowStart, s.VODSave.WindowOnly = closed, false
		return nil
	})
	if e.s.outsideWindow() {
		t.Error("any time: held anyway")
	}
}
