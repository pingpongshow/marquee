// Package health finds library problems an admin can fix (ADM-11): duplicates, unmatched
// titles, missing or unreadable files, titles worth upgrading, playback failures, missing
// posters and missing subtitles. Each check is one query, so the overview stays fast on
// large libraries (joins are CROSS JOINs to fix their order, so stale planner statistics
// can't turn a lookup into a scan). Extras are left out everywhere.
package health

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"marquee/internal/bazarr"
)

var (
	ErrUnknownCheck = errors.New("unknown health check")
	ErrNotFound     = errors.New("item not found")
)

// Check is one health check with how many items it found.
type Check struct {
	ID, Title, Description string
	Severity               string // info, warning, error
	Count                  int
	Available              bool
}

// Issue is one thing a check found.
type Issue struct {
	ItemID, FileID int64 // FileID 0 = the item as a whole
	Path, Detail   string
	Related        []int64 // duplicates: the other copies
}

// Service runs the checks.
type Service struct {
	DB *sql.DB
	// Bazarr feeds missingSubtitles; nil or not set up makes that check unavailable.
	Bazarr *bazarr.Service
}

type check struct {
	id, title, description, severity string
	// query yields item_id, file_id, path, detail, rank (higher first, then by title).
	query string
}

// Resolution classes by width or height, so letterboxed films (1920×800) count as 1080p.
const resolution = `CASE WHEN COALESCE(f.width, 0) >= 3200 OR COALESCE(f.height, 0) >= 2000 THEN '4K'
	WHEN COALESCE(f.width, 0) >= 1700 OR COALESCE(f.height, 0) >= 1000 THEN '1080p'
	WHEN COALESCE(f.width, 0) >= 1200 OR COALESCE(f.height, 0) >= 700 THEN '720p'
	ELSE 'SD' END`

// Upgrade thresholds: a title's best file is worth replacing when it is SD or 720p, or when
// its overall bitrate is low for its resolution: under 10 Mbps for 4K and 3 Mbps for 1080p
// in H.264 (and other older codecs), half that for HEVC, AV1 and VP9, which need about half
// the bitrate for the same picture.
const lowBitrate = `kbps > 0 AND kbps < (CASE res WHEN '4K' THEN 10000 ELSE 3000 END) /
	(CASE WHEN codec IN ('hevc', 'h265', 'av1', 'vp9') THEN 2 ELSE 1 END)`

var checks = []check{
	{id: "unplayable", title: "Unplayable files", severity: "error",
		description: "Files that couldn't be read: no audio or video, no duration, or ffprobe failed.",
		query: `SELECT i.id AS item_id, f.id AS file_id, f.path AS path, CASE
			WHEN pf.error IS NOT NULL THEN 'Couldn''t be read: ' || pf.error
			WHEN NOT EXISTS (SELECT 1 FROM streams s WHERE s.file_id = f.id AND s.kind IN ('video', 'audio')) THEN 'No audio or video streams'
			ELSE 'Zero duration' END AS detail, 0 AS rank
		FROM media_files f CROSS JOIN media_versions v ON v.id = f.version_id CROSS JOIN items i ON i.id = v.item_id
		LEFT JOIN probe_failures pf ON pf.path = f.path
		WHERE f.available = 1 AND i.extra_type IS NULL AND i.type NOT IN ('photo')
			AND (pf.path IS NOT NULL OR COALESCE(f.duration_ms, 0) <= 0
				OR NOT EXISTS (SELECT 1 FROM streams s WHERE s.file_id = f.id AND s.kind IN ('video', 'audio')))`},
	{id: "unavailable", title: "Missing files", severity: "error",
		description: "Titles whose files are no longer on disk (deleted, moved, or on a drive that's offline).",
		query: `SELECT i.id AS item_id, f.id AS file_id, f.path AS path, 'File not found' AS detail, 0 AS rank
		FROM items i CROSS JOIN media_versions v ON v.item_id = i.id CROSS JOIN media_files f ON f.version_id = v.id AND f.part_index = 0
		WHERE i.available = 0 AND i.type IN ('movie', 'episode', 'track', 'video') AND i.extra_type IS NULL
		GROUP BY i.id`},
	{id: "playbackErrors", title: "Playback errors", severity: "error",
		description: "Titles that failed to play in the last 30 days.",
		query: `SELECT pe.item_id AS item_id, pe.file_id AS file_id, f.path AS path,
			CASE WHEN pe.n > 1 THEN pe.n || ' errors · ' ELSE '' END || CASE WHEN pe.message = '' THEN 'Playback failed' ELSE pe.message END AS detail,
			julianday(pe.at) AS rank
		FROM (SELECT item_id, file_id, message, at, COUNT(*) OVER (PARTITION BY item_id) AS n,
				ROW_NUMBER() OVER (PARTITION BY item_id ORDER BY at DESC, id DESC) AS rn
			FROM playback_errors WHERE at >= strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '-30 days')) pe
		CROSS JOIN items i ON i.id = pe.item_id LEFT JOIN media_files f ON f.id = pe.file_id
		WHERE pe.rn = 1 AND i.extra_type IS NULL`},
	{id: "duplicates", title: "Duplicates", severity: "warning",
		description: "The same film or show more than once in a library, or a title with two files of the same resolution and edition.",
		query: `SELECT item_id, MAX(file_id) AS file_id, MAX(path) AS path, MAX(detail) AS detail, 0 AS rank FROM (
			SELECT i.id AS item_id, NULL AS file_id, NULL AS path, (COUNT(DISTINCT j.id) + 1) || ' copies in this library' AS detail
			FROM (SELECT provider, value FROM external_ids WHERE provider IN ('tmdb', 'imdb')
				GROUP BY provider, value HAVING COUNT(*) > 1) d
			CROSS JOIN external_ids e ON e.provider = d.provider AND e.value = d.value
			CROSS JOIN items i ON i.id = e.item_id
			CROSS JOIN external_ids e2 ON e2.provider = d.provider AND e2.value = d.value AND e2.item_id <> i.id
			CROSS JOIN items j ON j.id = e2.item_id
			WHERE i.type IN ('movie', 'show') AND i.extra_type IS NULL
				AND j.library_id = i.library_id AND j.type = i.type AND j.extra_type IS NULL
			GROUP BY i.id
			UNION ALL
			SELECT i.id, f2.id, f2.path, '2 files at ' || ` + resolution + ` ||
				CASE WHEN v.label <> '' THEN ' (' || v.label || ')' ELSE '' END
			FROM (SELECT item_id FROM media_versions GROUP BY item_id HAVING COUNT(*) > 1) m
			CROSS JOIN items i ON i.id = m.item_id
			CROSS JOIN media_versions v ON v.item_id = m.item_id
			CROSS JOIN media_versions v2 ON v2.item_id = m.item_id AND v2.id > v.id AND v2.label = v.label
			CROSS JOIN media_files f ON f.version_id = v.id
			CROSS JOIN media_files f2 ON f2.version_id = v2.id
			WHERE i.type IN ('movie', 'episode') AND i.extra_type IS NULL
				AND f.part_index = 0 AND f.available = 1 AND f2.part_index = 0 AND f2.available = 1
				AND ` + resolution + ` = ` + strings.ReplaceAll(resolution, "f.", "f2.") + `
		) GROUP BY item_id`},
	{id: "unmatched", title: "Unmatched", severity: "warning",
		description: "Movies, shows, artists and albums without online metadata. Fix the match from the title's menu.",
		query: `SELECT i.id AS item_id, NULL AS file_id, NULL AS path,
			CASE i.match_state WHEN 'failed' THEN 'Matching failed' ELSE 'Not matched' END AS detail, 0 AS rank
		FROM items i WHERE i.type IN ('movie', 'show', 'artist', 'album') AND i.match_state IN ('unmatched', 'failed') AND i.extra_type IS NULL`},
	{id: "missingSubtitles", title: "Missing subtitles", severity: "warning",
		description: "Movies and episodes still missing subtitles their Bazarr language profile asks for."},
	{id: "upgrades", title: "Upgrade candidates", severity: "info",
		description: "Movies and episodes whose best file is SD or 720p, or has a low bitrate for its resolution.",
		query: `SELECT item_id, file_id, path, res || CASE WHEN kbps > 0 THEN ' · ' || printf('%.1f', kbps / 1000.0) || ' Mbps' ELSE '' END ||
			CASE WHEN res IN ('4K', '1080p') THEN ' (low bitrate)' ELSE '' END AS detail, 0 AS rank
		FROM (SELECT i.id AS item_id, f.id AS file_id, f.path, COALESCE(f.bitrate_kbps, 0) AS kbps, LOWER(COALESCE(f.video_codec, '')) AS codec,
				CASE WHEN ` + resolution + ` = 'SD' THEN COALESCE(f.height, 0) || 'p' ELSE ` + resolution + ` END AS res,
				ROW_NUMBER() OVER (PARTITION BY i.id ORDER BY COALESCE(f.width, 0) * COALESCE(f.height, 0) DESC, COALESCE(f.bitrate_kbps, 0) DESC) AS rn
			FROM items i CROSS JOIN media_versions v ON v.item_id = i.id CROSS JOIN media_files f ON f.version_id = v.id AND f.part_index = 0
			WHERE i.type IN ('movie', 'episode') AND i.extra_type IS NULL AND f.available = 1 AND COALESCE(f.width, 0) > 0)
		WHERE rn = 1 AND (res NOT IN ('4K', '1080p') OR (` + lowBitrate + `))`},
	{id: "missingArtwork", title: "Missing artwork", severity: "info",
		description: "Movies, shows and albums without a poster.",
		query: `SELECT i.id AS item_id, NULL AS file_id, NULL AS path, 'No poster' AS detail, 0 AS rank FROM items i
		WHERE i.type IN ('movie', 'show', 'album') AND i.extra_type IS NULL
			AND NOT EXISTS (SELECT 1 FROM artwork a WHERE a.item_id = i.id AND a.kind = 'poster' AND a.selected = 1)`},
}

func find(id string) (check, bool) {
	for _, c := range checks {
		if c.id == id {
			return c, true
		}
	}
	return check{}, false
}

// Valid reports whether id names a check.
func Valid(id string) bool { _, ok := find(id); return ok }

const notIgnored = ` WHERE NOT EXISTS (SELECT 1 FROM health_ignored h WHERE h.check_id = ? AND h.item_id = x.item_id)`

// Checks runs every check's count.
func (s *Service) Checks(ctx context.Context) ([]Check, error) {
	out := make([]Check, 0, len(checks))
	for _, c := range checks {
		ck := Check{ID: c.id, Title: c.title, Description: c.description, Severity: c.severity, Available: true}
		if c.query == "" {
			list, err := s.wanted(ctx, 20*time.Second)
			ck.Count, ck.Available = len(list), err == nil
		} else if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM (`+c.query+`) x`+notIgnored, c.id).Scan(&ck.Count); err != nil {
			return nil, err
		}
		out = append(out, ck)
	}
	return out, nil
}

// Issues pages through what one check found, ignored items left out.
func (s *Service) Issues(ctx context.Context, id string, offset, limit int) ([]Issue, int, error) {
	c, ok := find(id)
	if !ok {
		return nil, 0, ErrUnknownCheck
	}
	if c.query == "" {
		list, err := s.wanted(ctx, bazarr.ListTimeout)
		if err != nil {
			return nil, 0, err
		}
		return page(list, offset, limit), len(list), nil
	}
	var total int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM (`+c.query+`) x`+notIgnored, c.id).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT x.item_id, COALESCE(x.file_id, 0), COALESCE(x.path, ''), x.detail
		FROM (`+c.query+`) x CROSS JOIN items i ON i.id = x.item_id`+
		strings.Replace(notIgnored, " WHERE", " AND", 1)+`
		ORDER BY x.rank DESC, i.sort_title COLLATE NOCASE, i.id, x.file_id LIMIT ? OFFSET ?`, c.id, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []Issue
	for rows.Next() {
		var is Issue
		if err := rows.Scan(&is.ItemID, &is.FileID, &is.Path, &is.Detail); err != nil {
			return nil, 0, err
		}
		is.Detail = clip(is.Detail, 300)
		out = append(out, is)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	if id == "duplicates" {
		err = s.related(ctx, out)
	}
	return out, total, err
}

// related fills in the other copies of duplicated titles.
func (s *Service) related(ctx context.Context, issues []Issue) error {
	for k := range issues {
		rows, err := s.DB.QueryContext(ctx, `SELECT DISTINCT j.id FROM items i
			JOIN external_ids e ON e.item_id = i.id AND e.provider IN ('tmdb', 'imdb')
			JOIN external_ids e2 ON e2.provider = e.provider AND e2.value = e.value AND e2.item_id <> i.id
			JOIN items j ON j.id = e2.item_id AND j.library_id = i.library_id AND j.type = i.type AND j.extra_type IS NULL
			WHERE i.id = ? ORDER BY j.id`, issues[k].ItemID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id int64
			if rows.Scan(&id) == nil {
				issues[k].Related = append(issues[k].Related, id)
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
	}
	return nil
}

// wanted is Bazarr's list of items missing subtitles, ignored ones left out.
func (s *Service) wanted(ctx context.Context, timeout time.Duration) ([]Issue, error) {
	if s.Bazarr == nil || !s.Bazarr.Configured() {
		return nil, bazarr.ErrNotConfigured
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	list, err := s.Bazarr.Wanted(ctx)
	if err != nil {
		return nil, err
	}
	ignored := map[int64]bool{}
	rows, err := s.DB.QueryContext(ctx, `SELECT item_id FROM health_ignored WHERE check_id = 'missingSubtitles'`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ignored[id] = true
		}
	}
	rows.Close()
	out := make([]Issue, 0, len(list))
	for _, w := range list {
		if !ignored[w.ItemID] {
			out = append(out, Issue{ItemID: w.ItemID, FileID: w.FileID, Path: w.Path, Detail: "Missing " + bazarr.Describe(w.Missing)})
		}
	}
	return out, rows.Err()
}

func page(list []Issue, offset, limit int) []Issue {
	if offset >= len(list) {
		return nil
	}
	return list[offset:min(len(list), offset+limit)]
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n] + "…"
}

// Ignore stops reporting an item under a check.
func (s *Service) Ignore(ctx context.Context, checkID string, itemID int64) error {
	if !Valid(checkID) {
		return ErrUnknownCheck
	}
	var one int
	if err := s.DB.QueryRowContext(ctx, `SELECT 1 FROM items WHERE id = ?`, itemID).Scan(&one); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	_, err := s.DB.ExecContext(ctx, `INSERT OR IGNORE INTO health_ignored(check_id, item_id) VALUES (?, ?)`, checkID, itemID)
	return err
}

// Unignore reports an item again.
func (s *Service) Unignore(ctx context.Context, checkID string, itemID int64) error {
	if !Valid(checkID) {
		return ErrUnknownCheck
	}
	_, err := s.DB.ExecContext(ctx, `DELETE FROM health_ignored WHERE check_id = ? AND item_id = ?`, checkID, itemID)
	return err
}

// RecordPlaybackError stores a failure an app reported (the same one repeated within a
// minute counts once), and forgets ones over 30 days old.
func RecordPlaybackError(ctx context.Context, db *sql.DB, itemID, fileID int64, message string) error {
	var file any
	if fileID > 0 {
		file = fileID
	}
	message = clip(strings.TrimSpace(message), 500)
	if _, err := db.ExecContext(ctx, `INSERT INTO playback_errors(item_id, file_id, message)
		SELECT id, ?, ? FROM items WHERE id = ? AND NOT EXISTS (SELECT 1 FROM playback_errors
			WHERE item_id = ? AND message = ? AND at > strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '-1 minute'))`,
		file, message, itemID, itemID, message); err != nil {
		return err
	}
	_, err := db.ExecContext(ctx, `DELETE FROM playback_errors WHERE at < strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '-30 days')`)
	return err
}
