package items

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
)

// SetWatched marks an item (and, for shows/seasons/artists/albums, everything in it) watched
// or unwatched for a user.
func (s *Store) SetWatched(ctx context.Context, uid, itemID int64, watched bool) error {
	var typ string
	if err := s.db.QueryRowContext(ctx, `SELECT type FROM items WHERE id = ?`, itemID).Scan(&typ); errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	} else if err != nil {
		return err
	}
	ids := []any{itemID}
	if typ == "show" || typ == "season" || typ == "artist" || typ == "album" {
		rows, err := s.db.QueryContext(ctx, `SELECT id FROM items WHERE (parent_id = ? OR grandparent_id = ?) AND type IN ('episode', 'track')`, itemID, itemID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id int64
			rows.Scan(&id)
			ids = append(ids, id)
		}
		rows.Close()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, id := range ids {
		if watched {
			_, err = tx.ExecContext(ctx, `INSERT INTO user_item_state(user_id, item_id, play_count, view_offset_ms, last_viewed_at)
				VALUES (?, ?, 1, 0, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
				ON CONFLICT(user_id, item_id) DO UPDATE SET play_count = MAX(play_count, 1), view_offset_ms = 0,
				last_viewed_at = excluded.last_viewed_at, updated_at = excluded.last_viewed_at`, uid, id)
		} else {
			_, err = tx.ExecContext(ctx, `UPDATE user_item_state SET play_count = 0, view_offset_ms = 0,
				updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE user_id = ? AND item_id = ?`, uid, id)
		}
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Next returns the episode after an episode (crossing into the next season), or ErrNotFound.
func (s *Store) Next(ctx context.Context, acc Access, episodeID int64) (Summary, error) {
	var show int64
	var season, ep int
	err := s.db.QueryRowContext(ctx, `SELECT e.grandparent_id, s.idx, e.idx FROM items e JOIN items s ON s.id = e.parent_id
		WHERE e.id = ? AND e.type = 'episode'`, episodeID).Scan(&show, &season, &ep)
	if errors.Is(err, sql.ErrNoRows) {
		return Summary{}, ErrNotFound
	}
	if err != nil {
		return Summary{}, err
	}
	return s.firstEpisode(ctx, acc, show, season, ep, false)
}

// firstEpisode returns the first episode of a show after (season, ep). Specials (season 0)
// are skipped. unwatchedOnly skips episodes the user has watched.
func (s *Store) firstEpisode(ctx context.Context, acc Access, show int64, season, ep int, unwatchedOnly bool) (Summary, error) {
	ac, aargs := acc.clause()
	q := `SELECT ` + cols(acc.UserID) + ` FROM items i JOIN items p ON p.id = i.parent_id LEFT JOIN items g ON g.id = i.grandparent_id
		WHERE i.grandparent_id = ? AND i.type = 'episode' AND p.idx > 0 AND i.available = 1
		AND (p.idx > ? OR (p.idx = ? AND i.idx > ?)) AND ` + ac
	if unwatchedOnly {
		q += ` AND NOT EXISTS (SELECT 1 FROM user_item_state x WHERE x.item_id = i.id AND x.user_id = ` +
			strconv.FormatInt(acc.UserID, 10) + ` AND x.play_count > 0)`
	}
	q += ` ORDER BY p.idx, i.idx LIMIT 1`
	sum, err := scanSummary(s.db.QueryRowContext(ctx, q, append([]any{show, season, season, ep}, aargs...)...))
	if errors.Is(err, sql.ErrNoRows) {
		return Summary{}, ErrNotFound
	}
	return sum, err
}

// Hub is a Home screen row.
type Hub struct {
	ID, Title string
	LibraryID int64
	Items     []Summary
}

// ContinueWatching returns in-progress movies/episodes/videos plus the next episode of shows
// the user has been watching (Plex's "On Deck"), most recent first.
func (s *Store) ContinueWatching(ctx context.Context, acc Access, libIDs []int64, limit int) ([]Summary, error) {
	acc.LibraryIDs = intersect(acc.LibraryIDs, libIDs)
	ac, aargs := acc.clause()
	uid := strconv.FormatInt(acc.UserID, 10)
	rows, err := s.db.QueryContext(ctx, `SELECT `+cols(acc.UserID)+summaryFrom+`
		JOIN user_item_state us ON us.item_id = i.id AND us.user_id = `+uid+`
		WHERE us.view_offset_ms > 0 AND i.type IN ('movie', 'episode', 'video') AND i.available = 1 AND `+ac+`
		ORDER BY us.last_viewed_at DESC LIMIT ?`, append(aargs, limit)...)
	if err != nil {
		return nil, err
	}
	inProgress, err := collect(rows)
	if err != nil {
		return nil, err
	}
	busyShows := map[int64]bool{}
	for _, it := range inProgress {
		if it.Type == "episode" {
			busyShows[it.GrandparentID] = true
		}
	}

	// On Deck: for shows watched in the last 60 days, the episode after the latest watched one.
	type watched struct {
		show       int64
		season, ep int
		when       string
	}
	r, err := s.db.QueryContext(ctx, `SELECT e.grandparent_id, s.idx, e.idx, MAX(us.last_viewed_at) FROM user_item_state us
		JOIN items e ON e.id = us.item_id AND e.type = 'episode' JOIN items s ON s.id = e.parent_id
		WHERE us.user_id = `+uid+` AND us.play_count > 0 AND us.last_viewed_at > strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '-60 days')
		GROUP BY e.grandparent_id ORDER BY MAX(us.last_viewed_at) DESC LIMIT 30`)
	if err != nil {
		return nil, err
	}
	var recent []watched
	for r.Next() {
		var w watched
		r.Scan(&w.show, &w.season, &w.ep, &w.when)
		recent = append(recent, w)
	}
	r.Close()
	type entry struct {
		item Summary
		when string
	}
	var merged []entry
	for _, it := range inProgress {
		merged = append(merged, entry{it, it.LastViewedAt})
	}
	for _, w := range recent {
		if busyShows[w.show] {
			continue
		}
		next, err := s.firstEpisode(ctx, acc, w.show, w.season, w.ep, true)
		if err != nil {
			continue
		}
		merged = append(merged, entry{next, w.when})
	}
	// Newest activity first.
	for i := 1; i < len(merged); i++ {
		for j := i; j > 0 && merged[j].when > merged[j-1].when; j-- {
			merged[j], merged[j-1] = merged[j-1], merged[j]
		}
	}
	out := []Summary{}
	for _, e := range merged {
		if len(out) == limit {
			break
		}
		out = append(out, e.item)
	}
	return out, nil
}

// RecentlyAdded returns the newest top-level items of a library. For shows, a show counts
// as new when an episode was added; for music, albums are shown.
func (s *Store) RecentlyAdded(ctx context.Context, acc Access, libID int64, libType string, limit int) ([]Summary, error) {
	acc.LibraryIDs = intersect(acc.LibraryIDs, []int64{libID})
	ac, aargs := acc.clause()
	var q string
	switch libType {
	case "shows", "anime":
		q = `SELECT ` + cols(acc.UserID) + summaryFrom + `
			JOIN (SELECT grandparent_id AS sid, MAX(added_at) AS latest FROM items WHERE library_id = ? AND type = 'episode'
			      GROUP BY grandparent_id) n ON n.sid = i.id
			WHERE ` + ac + ` ORDER BY n.latest DESC LIMIT ?`
	case "music":
		q = `SELECT ` + cols(acc.UserID) + summaryFrom + ` WHERE i.library_id = ? AND i.type = 'album' AND ` + ac + `
			ORDER BY i.added_at DESC, i.id DESC LIMIT ?`
	default:
		q = `SELECT ` + cols(acc.UserID) + summaryFrom + ` WHERE i.library_id = ? AND i.type IN ('movie', 'video') AND i.extra_type IS NULL AND ` + ac + `
			ORDER BY i.added_at DESC, i.id DESC LIMIT ?`
	}
	rows, err := s.db.QueryContext(ctx, q, append(append([]any{libID}, aargs...), limit)...)
	if err != nil {
		return nil, err
	}
	return collect(rows)
}

// intersect narrows an access list (nil = all) to ids.
func intersect(allowed, ids []int64) []int64 {
	if allowed == nil {
		return append([]int64{}, ids...)
	}
	set := map[int64]bool{}
	for _, a := range allowed {
		set[a] = true
	}
	out := []int64{}
	for _, id := range ids {
		if set[id] {
			out = append(out, id)
		}
	}
	return out
}

// RecentlyPlayedAlbums returns the albums the user played tracks from most recently.
func (s *Store) RecentlyPlayedAlbums(ctx context.Context, acc Access, libID int64, limit int) ([]Summary, error) {
	acc.LibraryIDs = intersect(acc.LibraryIDs, []int64{libID})
	ac, aargs := acc.clause()
	rows, err := s.db.QueryContext(ctx, `SELECT `+cols(acc.UserID)+summaryFrom+`
		JOIN (SELECT t.parent_id AS album, MAX(us.last_viewed_at) AS played FROM user_item_state us
		      JOIN items t ON t.id = us.item_id AND t.type = 'track' AND t.library_id = ?
		      WHERE us.user_id = ? AND us.last_viewed_at IS NOT NULL GROUP BY t.parent_id) r ON r.album = i.id
		WHERE `+ac+` ORDER BY r.played DESC LIMIT ?`, append(append([]any{libID, acc.UserID}, aargs...), limit)...)
	if err != nil {
		return nil, err
	}
	return collect(rows)
}
