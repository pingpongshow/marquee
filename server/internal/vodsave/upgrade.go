package vodsave

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"marquee/internal/scanner"
)

// Upgrades: a film or episode on demand better than the library's copy replaces it. Once
// the new file is verified, the library's file(s) of that film or episode (with their own
// subtitles, .nfo and artwork) move to the media trash (.marquee-trash, emptied after 30
// days), the new file goes in the same folder under the library's naming, and the item's
// file record is pointed at it, so the item keeps its id, watch history and ratings; the
// library is then scanned, which reads the new file into that record.

// replace puts a verified file in place of the library's copy v replaces. It returns where
// the new file went and where the replaced one was.
func (s *Service) replace(ctx context.Context, v Save, from, to string) (final, replaced string, err error) {
	// The item's available files (the one compared, and any other versions or parts).
	rows, err := s.DB.QueryContext(ctx, `SELECT f2.id, f2.library_id FROM media_files f
		JOIN media_versions mv ON mv.id=f.version_id JOIN media_versions mv2 ON mv2.item_id=mv.item_id
		JOIN media_files f2 ON f2.version_id=mv2.id WHERE f.id=? AND f2.available=1 ORDER BY f2.id=? DESC, f2.id`,
		v.ReplaceFileID, v.ReplaceFileID)
	if err != nil {
		return "", "", err
	}
	var ids []int64
	libs := map[int64]bool{}
	for rows.Next() {
		var id, lib int64
		rows.Scan(&id, &lib)
		ids = append(ids, id)
		libs[lib] = true
	}
	rows.Close()
	if len(ids) == 0 {
		// The library's copy went meanwhile: this is a plain save now.
		final, err = place(from, to)
		return final, "", err
	}
	if s.Trash == nil {
		return "", "", permError{errors.New("replacing the library's copy isn't available"), false}
	}
	paths, undo, err := s.Trash.Trash(ctx, ids, "replaced by a better copy on demand: "+v.Title)
	if err != nil {
		return "", "", permError{fmt.Errorf("couldn't move the library's copy to the media trash: %w", err), true}
	}
	final, err = place(from, to)
	if err != nil {
		undo()
		return "", "", err
	}
	if err := s.repoint(ctx, ids, final, libs); err != nil {
		// The files are where they should be; the scan sorts the records out.
		slog.Warn("vod save: upgrade: couldn't point the library at the new file; the scan will", "path", final, "err", err)
	}
	return final, paths[0], nil
}

// repoint points the kept file record (ids[0]) at the new file, to be read again by the
// next scan, and forgets the item's other files (moved to the trash). A scan that already
// found the new file keeps its record instead.
func (s *Service) repoint(ctx context.Context, ids []int64, path string, libs map[int64]bool) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var found int64
	tx.QueryRowContext(ctx, "SELECT id FROM media_files WHERE path=?", path).Scan(&found)
	forget := ids[1:]
	if found > 0 {
		forget = ids
	} else if _, err := tx.ExecContext(ctx, `UPDATE media_files SET path=?, size=-1, mtime=0, available=1 WHERE id=?`, path, ids[0]); err != nil {
		return err
	}
	for _, id := range forget {
		if id == found {
			continue
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM media_files WHERE id=?", id); err != nil {
			return err
		}
	}
	for lib := range libs {
		if err := scanner.Tidy(ctx, tx, lib); err != nil {
			return err
		}
	}
	return tx.Commit()
}
