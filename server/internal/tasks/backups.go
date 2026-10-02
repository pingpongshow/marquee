package tasks

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"marquee/internal/db"
)

var (
	ErrBackupNotFound = errors.New("backup not found")
	validBackupName   = regexp.MustCompile(`^[A-Za-z0-9._-]+\.db$`)
)

type Backup struct {
	Name      string
	Size      int64
	CreatedAt time.Time
	Kind      string // scheduled, manual, pre-migration, pre-restore
}

// Backups manages database backups in Dir (ADM-3).
type Backups struct {
	DB        *sql.DB
	Dir       string
	ConfigDir string
	Retention func() int
}

func backupKind(name string) string {
	for _, k := range []string{"pre-migration", "pre-restore", "manual"} {
		if strings.HasPrefix(name, k) {
			return k
		}
	}
	return "scheduled"
}

func (b *Backups) List() ([]Backup, error) {
	entries, err := os.ReadDir(b.Dir)
	if errors.Is(err, os.ErrNotExist) {
		return []Backup{}, nil
	}
	if err != nil {
		return nil, err
	}
	out := []Backup{}
	for _, e := range entries {
		if e.IsDir() || !validBackupName.MatchString(e.Name()) {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, Backup{Name: e.Name(), Size: info.Size(), CreatedAt: info.ModTime(), Kind: backupKind(e.Name())})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out, nil
}

// Path returns the file of a named backup.
func (b *Backups) Path(name string) (string, error) {
	if !validBackupName.MatchString(name) {
		return "", ErrBackupNotFound
	}
	p := filepath.Join(b.Dir, name)
	if _, err := os.Stat(p); err != nil {
		return "", ErrBackupNotFound
	}
	return p, nil
}

// Create writes a backup. kind is "manual" or "scheduled".
func (b *Backups) Create(ctx context.Context, kind string) (Backup, error) {
	prefix := "marquee"
	if kind == "manual" {
		prefix = "manual"
	}
	name := fmt.Sprintf("%s-%s.db", prefix, time.Now().UTC().Format("20060102-150405"))
	p := filepath.Join(b.Dir, name)
	if err := db.Backup(ctx, b.DB, p); err != nil {
		return Backup{}, err
	}
	info, err := os.Stat(p)
	if err != nil {
		return Backup{}, err
	}
	return Backup{Name: name, Size: info.Size(), CreatedAt: info.ModTime(), Kind: backupKind(name)}, nil
}

func (b *Backups) Delete(name string) error {
	p, err := b.Path(name)
	if err != nil {
		return err
	}
	return os.Remove(p)
}

// Prune keeps the newest Retention scheduled backups, and the five newest of every other kind.
func (b *Backups) Prune() (int, error) {
	list, err := b.List()
	if err != nil {
		return 0, err
	}
	seen := map[string]int{}
	removed := 0
	for _, bk := range list {
		keep := 5
		if bk.Kind == "scheduled" {
			keep = b.Retention()
		}
		seen[bk.Kind]++
		if seen[bk.Kind] > keep {
			if os.Remove(filepath.Join(b.Dir, bk.Name)) == nil {
				removed++
			}
		}
	}
	return removed, nil
}

// StageRestore copies a backup to where startup picks it up (db.ApplyPendingRestore).
func (b *Backups) StageRestore(name string) error {
	src, err := b.Path(name)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	tmp := db.PendingRestorePath(b.ConfigDir) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, db.PendingRestorePath(b.ConfigDir))
}

// Task is the daily backup-and-prune maintenance task.
func (b *Backups) Task() Task {
	return Task{ID: "backup", Name: "Back up database", Window: true,
		Description: "Saves a copy of the database and removes old copies beyond the number to keep.",
		Run: func(ctx context.Context) (string, error) {
			bk, err := b.Create(ctx, "scheduled")
			if err != nil {
				return "", err
			}
			n, err := b.Prune()
			if err != nil {
				return "", err
			}
			return fmt.Sprintf("Saved %s (%.1f MB); removed %d old backups", bk.Name, float64(bk.Size)/1e6, n), nil
		}}
}
