package items

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var ErrPlaylistKind = errors.New("none of these items fit this playlist (video playlists take movies, episodes and videos; music playlists take tracks)")

type Playlist struct {
	ID         int64
	Title      string
	Kind       string // video, audio
	ItemCount  int
	DurationMS int64
	ImageIDs   []int64
	UpdatedAt  time.Time
	Rules      *SmartRules // smart playlists only
	// Sharing (MUSIC-24). ReadOnly and OwnerName are set on someone else's playlist;
	// SharedWithAll and ShareCount on the viewer's own.
	ReadOnly      bool
	OwnerName     string
	SharedWithAll bool
	ShareCount    int
}

type PlaylistEntry struct {
	EntryID int64
	Item    Summary
}

var leafTypes = map[string]string{"video": "'movie', 'episode', 'video'", "audio": "'track'"}

const playlistCols = `pl.id, pl.title, pl.kind, pl.updated_at, COALESCE(pl.smart_rule, ''), pl.user_id,
	(SELECT COUNT(*) FROM playlist_items x WHERE x.playlist_id = pl.id),
	(SELECT COALESCE(SUM(i.duration_ms), 0) FROM playlist_items x JOIN items i ON i.id = x.item_id WHERE x.playlist_id = pl.id),
	pl.shared_with_all, (SELECT display_name FROM users u WHERE u.id = pl.user_id),
	(SELECT COUNT(*) FROM playlist_shares sh WHERE sh.playlist_id = pl.id AND sh.status IN ('pending', 'accepted'))`

// scanPlaylists reads playlists as acc sees them: on someone else's playlist, only what acc
// may see counts, and a smart one fills in from acc's own library and history.
func (s *Store) scanPlaylists(ctx context.Context, acc Access, q string, args ...any) ([]Playlist, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	out := []Playlist{}
	var owners []int64
	for rows.Next() {
		var p Playlist
		var updated, rules string
		var owner int64
		if err := rows.Scan(&p.ID, &p.Title, &p.Kind, &updated, &rules, &owner, &p.ItemCount, &p.DurationMS, &p.SharedWithAll, &p.OwnerName, &p.ShareCount); err != nil {
			rows.Close()
			return nil, err
		}
		p.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
		p.Rules = parseRules(rules)
		p.ReadOnly = owner != acc.UserID
		if p.ReadOnly {
			p.SharedWithAll, p.ShareCount = false, 0
		} else {
			p.OwnerName = ""
		}
		owners = append(owners, owner)
		out = append(out, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		if r := out[i].Rules; r != nil {
			// Smart: work out the contents now.
			viewer := Access{UserID: owners[i]}
			if out[i].ReadOnly {
				viewer = acc
			}
			list, total, err := s.SmartItems(ctx, viewer, out[i].Kind, *r, 0, 2000)
			if err != nil {
				return nil, err
			}
			out[i].ItemCount, out[i].DurationMS, out[i].ImageIDs = total, 0, []int64{}
			seen := map[int64]bool{}
			for _, it := range list {
				out[i].DurationMS += it.DurationMS
				if it.Poster != 0 && !seen[it.Poster] && len(out[i].ImageIDs) < 4 {
					seen[it.Poster] = true
					out[i].ImageIDs = append(out[i].ImageIDs, it.Poster)
				}
			}
			continue
		}
		out[i].ImageIDs = []int64{}
		visible, vargs := "1", []any(nil)
		if out[i].ReadOnly {
			visible, vargs = acc.clause()
			if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(i.duration_ms), 0)`+summaryFrom+` JOIN playlist_items x ON x.item_id = i.id
				WHERE x.playlist_id = ? AND `+visible, append([]any{out[i].ID}, vargs...)...).Scan(&out[i].ItemCount, &out[i].DurationMS); err != nil {
				return nil, err
			}
		}
		err := eachRow(ctx, s.db, `SELECT DISTINCT `+art("poster", "i", "p", "g")+` AS a FROM playlist_items x JOIN items i ON i.id = x.item_id
			LEFT JOIN items p ON p.id = i.parent_id LEFT JOIN items g ON g.id = i.grandparent_id
			WHERE x.playlist_id = ? AND a IS NOT NULL AND `+visible+` ORDER BY x.ord LIMIT 4`, append([]any{out[i].ID}, vargs...), func(r *sql.Rows) error {
			var id int64
			if err := r.Scan(&id); err != nil {
				return err
			}
			out[i].ImageIDs = append(out[i].ImageIDs, id)
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// Playlists lists a user's playlists and those shared with them that they accepted (kind "" =
// all), most recently changed first.
func (s *Store) Playlists(ctx context.Context, acc Access, kind string) ([]Playlist, error) {
	q := `SELECT ` + playlistCols + ` FROM playlists pl WHERE (pl.user_id = ? OR ` + acceptedShare + `) AND pl.kind IN ('video', 'audio')`
	args := []any{acc.UserID, acc.UserID}
	if kind != "" {
		q += ` AND pl.kind = ?`
		args = append(args, kind)
	}
	return s.scanPlaylists(ctx, acc, q+` ORDER BY pl.updated_at DESC`, args...)
}

// Playlist returns one of the user's own playlists: what may change it goes through here, so
// a playlist shared with them is not found.
func (s *Store) Playlist(ctx context.Context, uid, id int64) (Playlist, error) {
	list, err := s.scanPlaylists(ctx, Access{UserID: uid}, `SELECT `+playlistCols+` FROM playlists pl WHERE pl.id = ? AND pl.user_id = ?`, id, uid)
	if err != nil {
		return Playlist{}, err
	}
	if len(list) == 0 {
		return Playlist{}, ErrNotFound
	}
	return list[0], nil
}

func (s *Store) CreatePlaylist(ctx context.Context, acc Access, title, kind string, itemIDs []int64, rules *SmartRules) (Playlist, error) {
	var raw any
	if rules != nil {
		if err := rules.Validate(); err != nil {
			return Playlist{}, err
		}
		b, _ := json.Marshal(rules)
		raw = string(b)
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO playlists (user_id, title, kind, smart_rule) VALUES (?, ?, ?, ?)`, acc.UserID, strings.TrimSpace(title), kind, raw)
	if err != nil {
		return Playlist{}, err
	}
	id, _ := res.LastInsertId()
	if len(itemIDs) > 0 && rules == nil {
		if err := s.AddToPlaylist(ctx, acc, id, itemIDs); err != nil && !errors.Is(err, ErrPlaylistKind) {
			return Playlist{}, err
		}
	}
	return s.Playlist(ctx, acc.UserID, id)
}

func (s *Store) RenamePlaylist(ctx context.Context, uid, id int64, title string) error {
	return s.touchPlaylist(ctx, `UPDATE playlists SET title = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = ? AND user_id = ?`,
		strings.TrimSpace(title), id, uid)
}

func (s *Store) DeletePlaylist(ctx context.Context, uid, id int64) error {
	return s.touchPlaylist(ctx, `DELETE FROM playlists WHERE id = ? AND user_id = ?`, id, uid)
}

func (s *Store) touchPlaylist(ctx context.Context, q string, args ...any) error {
	res, err := s.db.ExecContext(ctx, q, args...)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// AddToPlaylist appends items, expanding shows, seasons, artists and albums into their
// episodes or tracks. Items that don't fit the playlist's kind are skipped.
func (s *Store) AddToPlaylist(ctx context.Context, acc Access, id int64, itemIDs []int64) error {
	pl, err := s.Playlist(ctx, acc.UserID, id)
	if err != nil {
		return err
	}
	if pl.Rules != nil {
		return ErrSmartPlaylist
	}
	var add []int64
	for _, itemID := range itemIDs {
		leaves, err := s.Leaves(ctx, acc, itemID, false)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		for _, l := range leaves {
			if strings.Contains(leafTypes[pl.Kind], "'"+l.Type+"'") {
				add = append(add, l.ID)
			}
		}
	}
	if len(add) == 0 {
		return ErrPlaylistKind
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var maxOrd float64
	tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(ord), 0) FROM playlist_items WHERE playlist_id = ?`, id).Scan(&maxOrd)
	for i, itemID := range add {
		if _, err := tx.ExecContext(ctx, `INSERT INTO playlist_items (playlist_id, item_id, ord) VALUES (?, ?, ?)`, id, itemID, maxOrd+float64(i+1)); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE playlists SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// PlaylistItems returns a page of a playlist's entries that acc can see.
func (s *Store) PlaylistItems(ctx context.Context, acc Access, id int64, offset, limit int) ([]PlaylistEntry, int, error) {
	pl, err := s.ReadablePlaylist(ctx, acc, id)
	if err != nil {
		return nil, 0, err
	}
	if pl.Rules != nil {
		list, total, err := s.SmartItems(ctx, acc, pl.Kind, *pl.Rules, offset, limit)
		if err != nil {
			return nil, 0, err
		}
		out := make([]PlaylistEntry, len(list))
		for i, it := range list {
			out[i] = PlaylistEntry{EntryID: it.ID, Item: it} // smart entries can't be moved or removed
		}
		return out, total, nil
	}
	ac, aargs := acc.clause()
	from := summaryFrom + ` JOIN playlist_items x ON x.item_id = i.id WHERE x.playlist_id = ? AND ` + ac
	args := append([]any{id}, aargs...)
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*)`+from, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+cols(acc.UserID)+`, x.id`+from+` ORDER BY x.ord LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []PlaylistEntry{}
	for rows.Next() {
		var e PlaylistEntry
		sum, err := scanSummary(rows, &e.EntryID)
		if err != nil {
			return nil, 0, err
		}
		e.Item = sum
		out = append(out, e)
	}
	return out, total, rows.Err()
}

func (s *Store) RemovePlaylistItem(ctx context.Context, uid, id, entryID int64) error {
	if _, err := s.Playlist(ctx, uid, id); err != nil {
		return err
	}
	return s.touchPlaylist(ctx, `DELETE FROM playlist_items WHERE id = ? AND playlist_id = ?`, entryID, id)
}

// MovePlaylistItem places entryID just after afterID (0 = at the top).
func (s *Store) MovePlaylistItem(ctx context.Context, uid, id, entryID, afterID int64) error {
	if _, err := s.Playlist(ctx, uid, id); err != nil {
		return err
	}
	var ord float64
	if afterID == 0 {
		if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(MIN(ord), 1) - 1 FROM playlist_items WHERE playlist_id = ?`, id).Scan(&ord); err != nil {
			return err
		}
	} else {
		var after float64
		if err := s.db.QueryRowContext(ctx, `SELECT ord FROM playlist_items WHERE id = ? AND playlist_id = ?`, afterID, id).Scan(&after); errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return err
		}
		var next sql.NullFloat64
		s.db.QueryRowContext(ctx, `SELECT MIN(ord) FROM playlist_items WHERE playlist_id = ? AND ord > ? AND id != ?`, id, after, entryID).Scan(&next)
		ord = after + 1
		if next.Valid {
			ord = (after + next.Float64) / 2
		}
	}
	return s.touchPlaylist(ctx, `UPDATE playlist_items SET ord = ? WHERE id = ? AND playlist_id = ?`, ord, entryID, id)
}

// SetRules replaces a smart playlist's rules.
func (s *Store) SetRules(ctx context.Context, uid, id int64, rules SmartRules) error {
	if err := rules.Validate(); err != nil {
		return err
	}
	b, _ := json.Marshal(rules)
	return s.touchPlaylist(ctx, `UPDATE playlists SET smart_rule = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
		WHERE id = ? AND user_id = ? AND smart_rule IS NOT NULL`, string(b), id, uid)
}
