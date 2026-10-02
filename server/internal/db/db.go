// Package db opens the SQLite database and applies migrations.
package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrations embed.FS

// Open opens (creating if needed) the database at path, backs it up if migrations are
// pending, and migrates it to the latest schema.
func Open(ctx context.Context, path, backupDir string) (*sql.DB, error) {
	// WAL for concurrent readers during writes; busy_timeout so writers queue instead of failing.
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(10000)" +
		"&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("open database: %w", err)
	}
	if err := migrate(ctx, db, backupDir); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func migrate(ctx context.Context, db *sql.DB, backupDir string) error {
	goose.SetBaseFS(migrations)
	goose.SetLogger(goose.NopLogger())
	if err := goose.SetDialect("sqlite3"); err != nil {
		return err
	}
	current, err := goose.GetDBVersionContext(ctx, db)
	if err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	all, err := goose.CollectMigrations("migrations", 0, goose.MaxVersion)
	if err != nil {
		return err
	}
	latest, err := all.Last()
	if err != nil {
		return err
	}
	if current >= latest.Version {
		return nil
	}
	if current > 0 {
		name := fmt.Sprintf("pre-migration-v%d-%s.db", current, time.Now().UTC().Format("20060102-150405"))
		if err := Backup(ctx, db, filepath.Join(backupDir, name)); err != nil {
			return fmt.Errorf("pre-migration backup: %w", err)
		}
	}
	slog.Info("migrating database", "from", current, "to", latest.Version)
	if err := goose.UpContext(ctx, db, "migrations"); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	return nil
}

// Backup writes a consistent snapshot of the live database to dest.
func Backup(ctx context.Context, db *sql.DB, dest string) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, "VACUUM INTO ?", dest)
	return err
}

// PendingRestorePath is where a backup chosen for restore waits for the next start.
func PendingRestorePath(configDir string) string {
	return filepath.Join(configDir, "restore-pending.db")
}

// ApplyPendingRestore swaps in a staged backup before the database is opened. The
// current database is kept in backupDir as a pre-restore backup.
func ApplyPendingRestore(dbPath, configDir, backupDir string) error {
	pending := PendingRestorePath(configDir)
	if _, err := os.Stat(pending); err != nil {
		return nil
	}
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return err
	}
	if _, err := os.Stat(dbPath); err == nil {
		// Fold the WAL into the old database before moving it aside.
		if old, err := sql.Open("sqlite", "file:"+dbPath); err == nil {
			old.Exec("PRAGMA wal_checkpoint(TRUNCATE)")
			old.Close()
		}
		keep := filepath.Join(backupDir, "pre-restore-"+time.Now().UTC().Format("20060102-150405")+".db")
		if err := os.Rename(dbPath, keep); err != nil {
			return err
		}
	}
	os.Remove(dbPath + "-wal")
	os.Remove(dbPath + "-shm")
	if err := os.Rename(pending, dbPath); err != nil {
		return err
	}
	slog.Info("restored database from backup")
	return nil
}
