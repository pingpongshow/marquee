package optimize

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"marquee/internal/library"
)

// Originals of optimized files kept in the media trash (keepOriginals on, or from before it
// existed). Only the originals recorded on finished optimizer jobs are ever touched: nothing
// else in the media trash (Library Health's removed files, commercials originals, VOD
// upgrade replacements).

// keptOriginal is a finished job whose original is still in the media trash.
type keptOriginal struct {
	id   int64
	path string
	size int64
}

// inTrash reports whether p is inside a media trash folder (a safety check before deleting).
func inTrash(p string) bool {
	return filepath.IsAbs(p) && strings.Contains(filepath.Clean(p), string(filepath.Separator)+library.TrashDir+string(filepath.Separator))
}

func (s *Service) keptOriginals(ctx context.Context) ([]keptOriginal, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, original FROM optimize_jobs WHERE state = 'done' AND original <> '' ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []keptOriginal
	for rows.Next() {
		var k keptOriginal
		if err := rows.Scan(&k.id, &k.path); err != nil {
			return nil, err
		}
		if fi, err := os.Stat(k.path); err == nil && fi.Mode().IsRegular() {
			k.size = fi.Size()
		} else if err == nil {
			continue // not a file: leave it alone
		} else {
			k.size = -1 // gone already
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// KeptOriginals counts the originals still kept in the media trash and their size.
func (s *Service) KeptOriginals(ctx context.Context) (n int, bytes int64, err error) {
	list, err := s.keptOriginals(ctx)
	for _, k := range list {
		if k.size >= 0 {
			n++
			bytes += k.size
		}
	}
	return n, bytes, err
}

// DeleteKeptOriginals deletes the originals of finished jobs still in the media trash and
// marks them deleted (so they can't be restored); originals already gone are just marked.
func (s *Service) DeleteKeptOriginals(ctx context.Context, why string) (deleted int, freed int64, err error) {
	list, err := s.keptOriginals(ctx)
	if err != nil {
		return 0, 0, err
	}
	for _, k := range list {
		if k.size >= 0 {
			if !inTrash(k.path) {
				slog.Warn("optimize: a kept original isn't in the media trash; left alone", "job", k.id, "path", k.path)
				continue
			}
			if err := os.Remove(k.path); err != nil && !os.IsNotExist(err) {
				slog.Warn("optimize: couldn't delete a kept original", "path", k.path, "err", err)
				continue
			}
			deleted++
			freed += k.size
		}
		if _, err := s.DB.ExecContext(ctx, `UPDATE optimize_jobs SET original = '', original_deleted = 1 WHERE id = ? AND original = ?`, k.id, k.path); err != nil {
			return deleted, freed, err
		}
	}
	if deleted > 0 {
		slog.Info("optimize: deleted originals kept in the media trash", "why", why, "files", deleted, "freed", Bytes(freed))
	}
	return deleted, freed, nil
}

// PurgeOriginals deletes kept originals when keepOriginals is off (at start-up and when
// the setting is saved off); with it on it does nothing.
func (s *Service) PurgeOriginals(ctx context.Context, why string) {
	if s.cfg().KeepOriginals {
		return
	}
	if _, _, err := s.DeleteKeptOriginals(ctx, why); err != nil {
		slog.Warn("optimize: couldn't delete kept originals", "err", err)
	}
}
