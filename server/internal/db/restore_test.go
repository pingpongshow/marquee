package db

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestApplyPendingRestore(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dbPath, backups := filepath.Join(dir, "marquee.db"), filepath.Join(dir, "backups")
	d, err := Open(ctx, dbPath, backups)
	if err != nil {
		t.Fatal(err)
	}
	d.Exec(`INSERT INTO settings(key, value) VALUES ('marker', '"before"')`)
	if err := Backup(ctx, d, PendingRestorePath(dir)); err != nil {
		t.Fatal(err)
	}
	d.Exec(`UPDATE settings SET value = '"after"' WHERE key = 'marker'`)
	d.Close()

	if err := ApplyPendingRestore(dbPath, dir, backups); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(PendingRestorePath(dir)); !os.IsNotExist(err) {
		t.Fatal("pending restore file should be consumed")
	}
	d, err = Open(ctx, dbPath, backups)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	var v string
	d.QueryRow(`SELECT value FROM settings WHERE key = 'marker'`).Scan(&v)
	if v != `"before"` {
		t.Fatalf("restored value %s", v)
	}
	kept, _ := filepath.Glob(filepath.Join(backups, "pre-restore-*.db"))
	if len(kept) != 1 {
		t.Fatalf("pre-restore copy: %v", kept)
	}
}
