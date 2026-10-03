package scanner

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
)

// RefreshSubtitles re-reads the folder of one media file and brings its sidecar subtitle
// streams up to date, without re-probing the video. A library scan skips files whose size
// and modification time haven't changed, so a subtitle saved next to an existing video (by
// Bazarr, META-12) would otherwise only appear once the video itself changed. Streams for
// subtitles that are still there keep their ids. It returns how many were added.
func RefreshSubtitles(ctx context.Context, db *sql.DB, fileID int64) (int, error) {
	var path string
	if err := db.QueryRowContext(ctx, `SELECT path FROM media_files WHERE id = ?`, fileID).Scan(&path); err != nil {
		return 0, err
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		return 0, err
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() {
			names = append(names, e.Name())
		}
	}
	found := map[string]subtitleFile{}
	for _, sf := range findSubtitles(path, names) {
		found[sf.Path] = sf
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	// Subtitles downloaded from OpenSubtitles live outside the media folder; leave them be.
	rows, err := tx.QueryContext(ctx, `SELECT id, external_path FROM streams WHERE file_id = ? AND external_path IS NOT NULL
		AND external_path NOT IN (SELECT path FROM downloaded_subtitles WHERE file_id = ?)`, fileID, fileID)
	if err != nil {
		return 0, err
	}
	have := map[string]int64{}
	for rows.Next() {
		var id int64
		var p string
		if err := rows.Scan(&id, &p); err != nil {
			rows.Close()
			return 0, err
		}
		have[p] = id
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	for p, id := range have {
		if _, ok := found[p]; !ok {
			if _, err := tx.ExecContext(ctx, `DELETE FROM streams WHERE id = ?`, id); err != nil {
				return 0, err
			}
		}
	}
	added := 0
	for p, sf := range found {
		if _, ok := have[p]; ok {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO streams
			(file_id, kind, codec, language, title, is_default, is_forced, is_hearing_impaired, external_path)
			VALUES (?, 'subtitle', ?, ?, ?, ?, ?, ?, ?)`,
			fileID, sf.Codec, nullStr(sf.Language), nullStr(sf.Title), sf.Default, sf.Forced, sf.HearingImpaired, sf.Path); err != nil {
			return 0, err
		}
		added++
	}
	return added, tx.Commit()
}
