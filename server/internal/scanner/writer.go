package scanner

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"

	"marquee/internal/library"
	"marquee/internal/probe"
)

const batchSize = 200

type outcome int

const (
	added outcome = iota
	updated
	moved
)

// writer applies scan results to the database in batched transactions. It is used from a
// single goroutine.
type writer struct {
	db       *sql.DB
	tx       *sql.Tx
	lib      library.Library
	dirs     map[string][]string
	existing map[string]existingFile
	missing  map[string]existingFile // path → file not found on disk (yet)
	pending  int
	keyCache map[string]int64 // type|scan_key → item id
}

func (w *writer) begin(ctx context.Context) error {
	if w.keyCache == nil {
		w.keyCache = map[string]int64{}
	}
	tx, err := w.db.BeginTx(ctx, nil)
	w.tx = tx
	return err
}

func (w *writer) rollback() {
	if w.tx != nil {
		w.tx.Rollback()
	}
}

func (w *writer) maybeCommit(ctx context.Context) error {
	w.pending++
	if w.pending < batchSize {
		return nil
	}
	w.pending = 0
	if err := w.tx.Commit(); err != nil {
		return err
	}
	return w.begin(ctx)
}

func (w *writer) restore(ctx context.Context, ids []int64) error {
	for _, id := range ids {
		if _, err := w.tx.ExecContext(ctx, `UPDATE media_files SET available = 1 WHERE id = ?`, id); err != nil {
			return err
		}
	}
	return nil
}

// upsert stores one probed file.
func (w *writer) upsert(ctx context.Context, c candidate, res *probe.Result) (outcome, error) {
	if e, ok := w.existing[c.Path]; ok {
		if err := w.updateFile(ctx, e.ID, c, res); err != nil {
			return updated, err
		}
		return updated, w.writeStreams(ctx, e.ID, c, res)
	}

	// A file that disappeared from one place and appeared elsewhere with the same name
	// and size was moved or its folder renamed: keep the record (and the watch history).
	if id, ok := w.findMoved(c); ok {
		if err := w.updateFile(ctx, id, c, res); err != nil {
			return moved, err
		}
		return moved, w.writeStreams(ctx, id, c, res)
	}

	pl, err := w.place(ctx, c, res)
	if err != nil {
		return added, err
	}
	versionID, err := w.version(ctx, pl, c)
	if err != nil {
		return added, err
	}
	v := res.Video()
	a := res.Audio()
	r, err := w.tx.ExecContext(ctx, `INSERT INTO media_files
		(version_id, library_id, path, size, mtime, container, duration_ms, bitrate_kbps, width, height,
		 video_codec, audio_codec, hdr_format, dv_profile, part_index, probed_at, probe_json)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), ?)`,
		versionID, w.lib.ID, c.Path, c.Size, c.MTime, res.Container, res.DurationMS, res.BitrateKbps,
		vint(v, func(s *probe.Stream) int { return s.Width }), vint(v, func(s *probe.Stream) int { return s.Height }),
		vstr(v, func(s *probe.Stream) string { return s.Codec }), vstr(a, func(s *probe.Stream) string { return s.Codec }),
		nullStr(vstr(v, func(s *probe.Stream) string { return s.HDRFormat })), nullInt(vint(v, func(s *probe.Stream) int { return s.DVProfile })),
		pl.Part, string(res.Raw))
	if err != nil {
		return added, err
	}
	fileID, _ := r.LastInsertId()
	if _, err := w.tx.ExecContext(ctx, `UPDATE items SET duration_ms = MAX(COALESCE(duration_ms, 0), ?) WHERE id = ?`, res.DurationMS, pl.ItemID); err != nil {
		return added, err
	}
	return added, w.writeStreams(ctx, fileID, c, res)
}

func (w *writer) findMoved(c candidate) (int64, bool) {
	base := strings.ToLower(filepath.Base(c.Path))
	for p, e := range w.missing {
		if e.Size == c.Size && strings.ToLower(filepath.Base(p)) == base {
			delete(w.missing, p)
			return e.ID, true
		}
	}
	return 0, false
}

func (w *writer) updateFile(ctx context.Context, id int64, c candidate, res *probe.Result) error {
	v, a := res.Video(), res.Audio()
	_, err := w.tx.ExecContext(ctx, `UPDATE media_files SET path = ?, size = ?, mtime = ?, container = ?, duration_ms = ?,
		bitrate_kbps = ?, width = ?, height = ?, video_codec = ?, audio_codec = ?, hdr_format = ?, dv_profile = ?,
		available = 1, probed_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), probe_json = ? WHERE id = ?`,
		c.Path, c.Size, c.MTime, res.Container, res.DurationMS, res.BitrateKbps,
		vint(v, func(s *probe.Stream) int { return s.Width }), vint(v, func(s *probe.Stream) int { return s.Height }),
		vstr(v, func(s *probe.Stream) string { return s.Codec }), vstr(a, func(s *probe.Stream) string { return s.Codec }),
		nullStr(vstr(v, func(s *probe.Stream) string { return s.HDRFormat })), nullInt(vint(v, func(s *probe.Stream) int { return s.DVProfile })),
		string(res.Raw), id)
	return err
}

// version returns the media version for a newly placed file: multi-part files share a
// version; any other additional file of the same item becomes its own version.
func (w *writer) version(ctx context.Context, pl placement, c candidate) (int64, error) {
	if pl.Part > 0 {
		var id int64
		err := w.tx.QueryRowContext(ctx, `SELECT v.id FROM media_versions v JOIN media_files f ON f.version_id = v.id
			WHERE v.item_id = ? AND v.label = ? AND f.part_index > 0 AND f.path LIKE ? ESCAPE '\' LIMIT 1`,
			pl.ItemID, pl.Label, likePrefix(filepath.Dir(c.Path))+"%").Scan(&id)
		if err == nil {
			return id, nil
		}
		if err != sql.ErrNoRows {
			return 0, err
		}
	}
	r, err := w.tx.ExecContext(ctx, `INSERT INTO media_versions(item_id, label) VALUES (?, ?)`, pl.ItemID, pl.Label)
	if err != nil {
		return 0, err
	}
	return r.LastInsertId()
}

// writeStreams replaces the file's streams, chapters and sidecars.
func (w *writer) writeStreams(ctx context.Context, fileID int64, c candidate, res *probe.Result) error {
	for _, q := range []string{
		`DELETE FROM streams WHERE file_id = ?`,
		`DELETE FROM markers WHERE file_id = ? AND kind = 'chapter' AND source = 'file'`,
		`DELETE FROM sidecars WHERE file_id = ?`,
	} {
		if _, err := w.tx.ExecContext(ctx, q, fileID); err != nil {
			return err
		}
	}
	for _, s := range res.Streams {
		if s.AttachedPic {
			continue
		}
		if _, err := w.tx.ExecContext(ctx, `INSERT INTO streams
			(file_id, stream_index, kind, codec, profile, level, language, title, is_default, is_forced, is_hearing_impaired,
			 channels, channel_layout, sample_rate, bitrate_kbps, width, height, frame_rate, bit_depth, color_transfer, color_primaries)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			fileID, s.Index, s.Kind, s.Codec, nullStr(s.Profile), nullInt(s.Level), nullStr(s.Language), nullStr(s.Title),
			s.Default, s.Forced, s.HearingImpaired, nullInt(s.Channels), nullStr(s.ChannelLayout), nullInt(s.SampleRate),
			nullInt(s.BitrateKbps), nullInt(s.Width), nullInt(s.Height), nullFloat(s.FrameRate), nullInt(s.BitDepth),
			nullStr(s.ColorTransfer), nullStr(s.ColorPrimaries)); err != nil {
			return err
		}
	}
	dirFiles := w.dirs[filepath.Dir(c.Path)]
	if w.lib.Type != library.Music {
		for _, sf := range findSubtitles(c.Path, dirFiles) {
			if _, err := w.tx.ExecContext(ctx, `INSERT INTO streams
				(file_id, kind, codec, language, title, is_default, is_forced, is_hearing_impaired, external_path)
				VALUES (?, 'subtitle', ?, ?, ?, ?, ?, ?, ?)`,
				fileID, sf.Codec, nullStr(sf.Language), nullStr(sf.Title), sf.Default, sf.Forced, sf.HearingImpaired, sf.Path); err != nil {
				return err
			}
		}
		// Subtitles downloaded from OpenSubtitles live outside the (read-only) media folder.
		if _, err := w.tx.ExecContext(ctx, `INSERT INTO streams (file_id, kind, codec, language, title, is_hearing_impaired, external_path)
			SELECT file_id, 'subtitle', 'subrip', language, title, hearing_impaired, path FROM downloaded_subtitles WHERE file_id = ?`, fileID); err != nil {
			return err
		}
	} else if lrc := findSidecar(c.Path, dirFiles, "lrc"); lrc != "" {
		if _, err := w.tx.ExecContext(ctx, `INSERT OR REPLACE INTO sidecars(file_id, kind, path) VALUES (?, 'lyrics', ?)`, fileID, lrc); err != nil {
			return err
		}
	}
	for _, ch := range res.Chapters {
		if _, err := w.tx.ExecContext(ctx, `INSERT INTO markers(file_id, kind, title, start_ms, end_ms, source) VALUES (?, 'chapter', ?, ?, ?, 'file')`,
			fileID, nullStr(ch.Title), ch.StartMS, ch.EndMS); err != nil {
			return err
		}
	}
	return nil
}

// finish marks missing files unavailable, removes items left without files, recomputes
// availability and child counts, and commits.
func (w *writer) finish(ctx context.Context) error {
	for _, e := range w.missing {
		if _, err := w.tx.ExecContext(ctx, `UPDATE media_files SET available = 0 WHERE id = ?`, e.ID); err != nil {
			return err
		}
	}
	lib := w.lib.ID
	steps := []string{
		// Versions and leaf items whose files are all gone from the database.
		`DELETE FROM media_versions WHERE item_id IN (SELECT id FROM items WHERE library_id = ?1)
			AND NOT EXISTS (SELECT 1 FROM media_files f WHERE f.version_id = media_versions.id)`,
		`DELETE FROM items WHERE library_id = ?1 AND type IN ('movie','episode','track','video')
			AND NOT EXISTS (SELECT 1 FROM media_versions v WHERE v.item_id = items.id)`,
		// Containers left empty.
		`DELETE FROM items WHERE library_id = ?1 AND type IN ('season','album')
			AND NOT EXISTS (SELECT 1 FROM items c WHERE c.parent_id = items.id)`,
		`DELETE FROM items WHERE library_id = ?1 AND type IN ('show','artist')
			AND NOT EXISTS (SELECT 1 FROM items c WHERE c.parent_id = items.id AND c.extra_type IS NULL)`,
		// Availability bubbles up from files.
		`UPDATE items SET available = EXISTS (SELECT 1 FROM media_versions v JOIN media_files f ON f.version_id = v.id
			WHERE v.item_id = items.id AND f.available = 1)
			WHERE library_id = ?1 AND type IN ('movie','episode','track','video')`,
		`UPDATE items SET available = EXISTS (SELECT 1 FROM items c WHERE c.parent_id = items.id AND c.available = 1)
			WHERE library_id = ?1 AND type IN ('season','album')`,
		`UPDATE items SET available = EXISTS (SELECT 1 FROM items c WHERE c.parent_id = items.id AND c.available = 1 AND c.extra_type IS NULL)
			WHERE library_id = ?1 AND type IN ('show','artist')`,
		// Counts.
		`UPDATE items SET
			child_count = (SELECT COUNT(*) FROM items c WHERE c.parent_id = items.id),
			leaf_count  = (SELECT COUNT(*) FROM items c WHERE c.parent_id = items.id),
			duration_ms = (SELECT SUM(duration_ms) FROM items c WHERE c.parent_id = items.id)
			WHERE library_id = ?1 AND type IN ('season','album')`,
		`UPDATE items SET
			child_count = (SELECT COUNT(*) FROM items c WHERE c.parent_id = items.id AND c.extra_type IS NULL),
			leaf_count  = (SELECT COUNT(*) FROM items c WHERE c.grandparent_id = items.id)
			WHERE library_id = ?1 AND type = 'show'`,
		`UPDATE items SET
			child_count = (SELECT COUNT(*) FROM items c WHERE c.parent_id = items.id),
			leaf_count  = (SELECT COUNT(*) FROM items t JOIN items a ON t.parent_id = a.id WHERE a.parent_id = items.id)
			WHERE library_id = ?1 AND type = 'artist'`,
		`UPDATE libraries SET last_scanned_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = ?1`,
	}
	for _, q := range steps {
		if _, err := w.tx.ExecContext(ctx, q, lib); err != nil {
			return err
		}
	}
	err := w.tx.Commit()
	w.tx = nil
	return err
}

// ---- helpers ----

func vint(s *probe.Stream, f func(*probe.Stream) int) int {
	if s == nil {
		return 0
	}
	return f(s)
}

func vstr(s *probe.Stream, f func(*probe.Stream) string) string {
	if s == nil {
		return ""
	}
	return f(s)
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullInt(n int) any {
	if n == 0 {
		return nil
	}
	return n
}

func nullFloat(f float64) any {
	if f == 0 {
		return nil
	}
	return f
}

func likePrefix(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s) + string(filepath.Separator)
}
