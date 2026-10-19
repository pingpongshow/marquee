// Package storage is the storage overview (Dashboard → Storage): space used per library,
// the biggest titles, a daily sample of each library's size for the growth chart, and the
// free space on the volumes Marquee uses.
package storage

import (
	"context"
	"database/sql"
	"log/slog"
	"slices"
	"strings"
	"time"
)

// KeepDays is how long daily samples are kept.
const KeepDays = 366

// Library is one library's size: its available media files.
type Library struct {
	ID         int64
	Name, Type string
	Bytes      int64
	Files      int
	Recordings bool
}

// Title is a movie, show (all its episodes) or home video and the space its files take.
type Title struct {
	ItemID, LibraryID int64
	Title, Type       string
	Year              int // 0 when unknown
	Bytes             int64
	Files             int
}

// Sample is a library's size on a day.
type Sample struct {
	Date  string // YYYY-MM-DD
	Bytes int64
	Files int
}

// Folder is a folder whose volume is reported (config, transcode, recordings, a library
// folder), named for what it holds.
type Folder struct {
	Name, Path string
}

// Volume is a file system holding one or more of the folders.
type Volume struct {
	Path       string // the first folder on it
	Label      string // what it holds, e.g. "Config, Transcode, Movies"
	TotalBytes int64
	FreeBytes  int64
}

// Service reads sizes from the database and records the daily samples.
type Service struct {
	DB *sql.DB
	// Folders returns the folders to report volumes for (the disk space alert's list).
	Folders func(ctx context.Context) []Folder
	// Usage reports the file system holding path (tests); defaults to statfs.
	Usage func(path string) (total, free int64, err error)
	// Device identifies the file system holding path (tests); defaults to stat's device.
	Device func(path string) string
	// Now is the clock (tests).
	Now func() time.Time
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Libraries returns every library with the size and count of its available media files,
// in the libraries' display order.
func (s *Service) Libraries(ctx context.Context) ([]Library, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT l.id, l.name, l.type, l.recordings,
		       COALESCE(SUM(f.size), 0), COUNT(f.id)
		FROM libraries l LEFT JOIN media_files f ON f.library_id = l.id AND f.available = 1
		GROUP BY l.id ORDER BY l.sort_order, l.name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Library{}
	for rows.Next() {
		var l Library
		if err := rows.Scan(&l.ID, &l.Name, &l.Type, &l.Recordings, &l.Bytes, &l.Files); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// Largest returns the top biggest movies, shows and home videos, biggest first. A file
// counts toward the movie, show or video at the top of its item's tree, so a show sums its
// episodes, and extras count toward the title they belong to.
func (s *Service) Largest(ctx context.Context, top int) ([]Title, error) {
	if top <= 0 {
		top = 20
	}
	rows, err := s.DB.QueryContext(ctx, `
		SELECT t.id, t.library_id, t.title, t.type, COALESCE(t.year, 0), SUM(f.size), COUNT(f.id)
		FROM media_files f
		JOIN media_versions v ON v.id = f.version_id
		JOIN items i ON i.id = v.item_id
		JOIN items t ON t.id = COALESCE(i.grandparent_id, i.parent_id, i.id)
		WHERE f.available = 1 AND t.type IN ('movie', 'show', 'video') AND t.extra_type IS NULL
		GROUP BY t.id ORDER BY SUM(f.size) DESC, t.id LIMIT ?`, top)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Title{}
	for rows.Next() {
		var t Title
		if err := rows.Scan(&t.ItemID, &t.LibraryID, &t.Title, &t.Type, &t.Year, &t.Bytes, &t.Files); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// History returns each library's samples from the last days days, oldest first.
func (s *Service) History(ctx context.Context, days int) (map[int64][]Sample, error) {
	from := s.now().AddDate(0, 0, -days).Format(time.DateOnly)
	rows, err := s.DB.QueryContext(ctx, `SELECT library_id, date, bytes, files FROM storage_samples
		WHERE date > ? ORDER BY library_id, date`, from)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][]Sample{}
	for rows.Next() {
		var id int64
		var x Sample
		if err := rows.Scan(&id, &x.Date, &x.Bytes, &x.Files); err != nil {
			return nil, err
		}
		out[id] = append(out[id], x)
	}
	return out, rows.Err()
}

// Sample records today's size of every library unless today's samples exist, and drops
// samples older than KeepDays. It reports whether it recorded.
func (s *Service) Sample(ctx context.Context) (bool, error) {
	now := s.now()
	today := now.Format(time.DateOnly)
	var n int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM storage_samples WHERE date = ?`, today).Scan(&n); err != nil {
		return false, err
	}
	recorded := false
	if n == 0 {
		libs, err := s.Libraries(ctx)
		if err != nil {
			return false, err
		}
		tx, err := s.DB.BeginTx(ctx, nil)
		if err != nil {
			return false, err
		}
		for _, l := range libs {
			if _, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO storage_samples(library_id, date, bytes, files) VALUES (?, ?, ?, ?)`,
				l.ID, today, l.Bytes, l.Files); err != nil {
				tx.Rollback()
				return false, err
			}
		}
		if err := tx.Commit(); err != nil {
			return false, err
		}
		recorded = len(libs) > 0
	}
	cutoff := now.AddDate(0, 0, -KeepDays).Format(time.DateOnly)
	_, err := s.DB.ExecContext(ctx, `DELETE FROM storage_samples WHERE date < ?`, cutoff)
	return recorded, err
}

// Run records today's sample now if it's missing, then checks every hour, so there's one
// sample a day whenever the server is up (a restart or a sleeping host doesn't miss a day
// it's running on).
func (s *Service) Run(ctx context.Context) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		if _, err := s.Sample(ctx); err != nil && ctx.Err() == nil {
			slog.Warn("storage: daily sample failed", "err", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Volumes returns the file systems holding the folders, each once (folders on the same
// device share an entry, labelled with everything on it), in the folders' order. Folders
// that can't be read are left out.
func (s *Service) Volumes(ctx context.Context) []Volume {
	if s.Folders == nil {
		return []Volume{}
	}
	usage, device := s.Usage, s.Device
	if usage == nil {
		usage = diskUsage
	}
	if device == nil {
		device = deviceOf
	}
	out := []Volume{}
	byDev := map[string]int{}
	labels := map[string][]string{}
	for _, f := range s.Folders(ctx) {
		if f.Path == "" {
			continue
		}
		dev := device(f.Path)
		if i, ok := byDev[dev]; ok {
			if !slices.Contains(labels[dev], f.Name) {
				labels[dev] = append(labels[dev], f.Name)
				out[i].Label = strings.Join(labels[dev], ", ")
			}
			continue
		}
		total, free, err := usage(f.Path)
		if err != nil || total <= 0 {
			continue
		}
		byDev[dev] = len(out)
		labels[dev] = []string{f.Name}
		out = append(out, Volume{Path: f.Path, Label: f.Name, TotalBytes: total, FreeBytes: free})
	}
	return out
}
