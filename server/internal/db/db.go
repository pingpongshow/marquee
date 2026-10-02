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
