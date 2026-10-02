package items

import (
	"context"
	"database/sql"
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
}

type PlaylistEntry struct {
	EntryID int64
	Item    Summary
}

var leafTypes = map[string]string{"video": "'movie', 'episode', 'video'", "audio": "'track'"}

const playlistCols = `pl.id, pl.title, pl.kind, pl.updated_at,
	(SELECT COUNT(*) FROM playlist_items x WHERE x.playlist_id = pl.id),
	(SELECT COALESCE(SUM(i.duration_ms), 0) FROM playlist_items x JOIN items i ON i.id = x.item_id WHERE x.playlist_id = pl.id)`

func (s *Store) scanPlaylists(ctx context.Context, q string, args ...any) ([]Playlist, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	out := []Playlist{}
	for rows.Next() {
		var p Playlist
		var updated string
		if err := rows.Scan(&p.ID, &p.Title, &p.Kind, &updated, &p.ItemCount, &p.DurationMS); err != nil {
			rows.Close()
			return nil, err
		}
		p.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
		out = append(out, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		out[i].ImageIDs = []int64{}
		err := eachRow(ctx, s.db, `SELECT DISTINCT `+art("poster", "i", "p", "g")+` AS a FROM playlist_items x JOIN items i ON i.id = x.item_id
			LEFT JOIN items p ON p.id = i.parent_id LEFT JOIN items g ON g.id = i.grandparent_id
			WHERE x.playlist_id = ? AND a IS NOT NULL ORDER BY x.ord LIMIT 4`, []any{out[i].ID}, func(r *sql.Rows) error {
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

// Playlists lists a user's playlists (kind "" = all), most recently changed first.
func (s *Store) Playlists(ctx context.Context, uid int64, kind string) ([]Playlist, error) {
	q := `SELECT ` + playlistCols + ` FROM playlists pl WHERE pl.user_id = ? AND pl.kind IN ('video', 'audio')`
	args := []any{uid}
	if kind != "" {
		q += ` AND pl.kind = ?`
		args = append(args, kind)
	}
	return s.scanPlaylists(ctx, q+` ORDER BY pl.updated_at DESC`, args...)
}

// Playlist returns one of the user's playlists.
func (s *Store) Playlist(ctx context.Context, uid, id int64) (Playlist, error) {
	list, err := s.scanPlaylists(ctx, `SELECT `+playlistCols+` FROM playlists pl WHERE pl.id = ? AND pl.user_id = ?`, id, uid)
	if err != nil {
		return Playlist{}, err
	}
	if len(list) == 0 {
		return Playlist{}, ErrNotFound
	}
	return list[0], nil
}

func (s *Store) CreatePlaylist(ctx context.Context, acc Access, title, kind string, itemIDs []int64) (Playlist, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO playlists (user_id, title, kind) VALUES (?, ?, ?)`, acc.UserID, strings.TrimSpace(title), kind)
	if err != nil {
		return Playlist{}, err
	}
	id, _ := res.LastInsertId()
	if len(itemIDs) > 0 {
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
	if _, err := s.Playlist(ctx, acc.UserID, id); err != nil {
		return nil, 0, err
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
