package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

// TestSoundprintMigration checks 00026 renames the sonic table, the stored setting (keeping
// its value) and the task id, and that it rolls back.
func TestSoundprintMigration(t *testing.T) {
	ctx := context.Background()
	d, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "t.db")+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	goose.SetBaseFS(migrations)
	goose.SetLogger(goose.NopLogger())
	goose.SetDialect("sqlite3")
	if err := goose.UpToContext(ctx, d, "migrations", 25); err != nil {
		t.Fatal(err)
	}
	mustExec := func(q string, args ...any) {
		t.Helper()
		if _, err := d.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	mustExec(`INSERT INTO settings(key, value) VALUES ('server_settings', '{"music":{"sonicAnalysis":false,"onlineLyrics":true}}')`)
	mustExec(`INSERT INTO task_runs(task, status) VALUES ('sonic', 'succeeded'), ('optimize', 'succeeded')`)
	mustExec(`INSERT INTO libraries(name, type) VALUES ('Music', 'music')`)
	mustExec(`INSERT INTO items(id, library_id, type, title, sort_title) VALUES (1, 1, 'track', 'T', 'T')`)
	mustExec(`INSERT INTO media_versions(id, item_id) VALUES (1, 1)`)
	mustExec(`INSERT INTO media_files(id, version_id, library_id, path, size, mtime) VALUES (1, 1, 1, '/m/t.flac', 1, 1)`)
	mustExec(`INSERT INTO sonic(item_id, file_id, model, bpm) VALUES (1, 1, 'clap', 120)`)

	if err := goose.UpToContext(ctx, d, "migrations", 26); err != nil {
		t.Fatal(err)
	}
	var settings, task string
	var bpm float64
	d.QueryRow(`SELECT value FROM settings WHERE key = 'server_settings'`).Scan(&settings)
	d.QueryRow(`SELECT task FROM task_runs WHERE id = 1`).Scan(&task)
	if err := d.QueryRow(`SELECT bpm FROM soundprint WHERE item_id = 1`).Scan(&bpm); err != nil || bpm != 120 {
		t.Errorf("soundprint row: %v %v", bpm, err)
	}
	if settings != `{"music":{"onlineLyrics":true,"soundprintAnalysis":false}}` || task != "soundprint" {
		t.Errorf("after up: %s %s", settings, task)
	}

	if err := goose.DownToContext(ctx, d, "migrations", 25); err != nil {
		t.Fatal(err)
	}
	d.QueryRow(`SELECT value FROM settings WHERE key = 'server_settings'`).Scan(&settings)
	d.QueryRow(`SELECT task FROM task_runs WHERE id = 1`).Scan(&task)
	if settings != `{"music":{"onlineLyrics":true,"sonicAnalysis":false}}` || task != "sonic" {
		t.Errorf("after down: %s %s", settings, task)
	}
	if err := d.QueryRow(`SELECT COUNT(*) FROM sonic`).Scan(new(int)); err != nil {
		t.Errorf("sonic table back: %v", err)
	}
}
