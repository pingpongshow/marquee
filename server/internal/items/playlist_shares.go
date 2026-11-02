package items

import (
	"context"
	"database/sql"
	"errors"
)

// ---------- Sharing playlists (MUSIC-24) ----------
//
// Playlists are private to their owner until shared. The person it's shared with accepts it,
// then has it among their own to play but not to change; the store's Playlist finds only
// someone's own playlists, which is what keeps every change to the owner. Removing it leaves
// the owner's alone, and it stays gone until the owner shares it with them again.

// acceptedShare is the SQL test for pl being shared with the user in its one parameter.
const acceptedShare = `EXISTS (SELECT 1 FROM playlist_shares sh WHERE sh.playlist_id = pl.id AND sh.user_id = ? AND sh.status = 'accepted')`

// invitedShare is the SQL test for pl being offered to the user in its three parameters.
const invitedShare = `pl.user_id != ? AND (EXISTS (SELECT 1 FROM playlist_shares sh WHERE sh.playlist_id = pl.id AND sh.user_id = ? AND sh.status = 'pending')
	OR (pl.shared_with_all = 1 AND NOT EXISTS (SELECT 1 FROM playlist_shares sh WHERE sh.playlist_id = pl.id AND sh.user_id = ?)))`

// PlaylistShare is someone a playlist is shared with; Status is pending or accepted.
type PlaylistShare struct {
	UserID int64
	Name   string
	Status string
}

// ReadablePlaylist returns a playlist acc may play: their own, or one they accepted.
func (s *Store) ReadablePlaylist(ctx context.Context, acc Access, id int64) (Playlist, error) {
	list, err := s.scanPlaylists(ctx, acc, `SELECT `+playlistCols+` FROM playlists pl WHERE pl.id = ? AND (pl.user_id = ? OR `+acceptedShare+`)`, id, acc.UserID, acc.UserID)
	if err != nil {
		return Playlist{}, err
	}
	if len(list) == 0 {
		return Playlist{}, ErrNotFound
	}
	return list[0], nil
}

// PlaylistShares says who an owner's playlist is shared with.
func (s *Store) PlaylistShares(ctx context.Context, owner, id int64) (bool, []PlaylistShare, error) {
	p, err := s.Playlist(ctx, owner, id)
	if err != nil {
		return false, nil, err
	}
	out := []PlaylistShare{}
	err = eachRow(ctx, s.db, `SELECT u.id, u.display_name, sh.status FROM playlist_shares sh JOIN users u ON u.id = sh.user_id
		WHERE sh.playlist_id = ? AND sh.status IN ('pending', 'accepted') ORDER BY u.display_name COLLATE NOCASE`, []any{id}, func(r *sql.Rows) error {
		var sh PlaylistShare
		if err := r.Scan(&sh.UserID, &sh.Name, &sh.Status); err != nil {
			return err
		}
		out = append(out, sh)
		return nil
	})
	return p.SharedWithAll, out, err
}

// SetPlaylistShares chooses who an owner's playlist is shared with. Someone already on it
// keeps their answer; someone who removed it is offered it again by being listed, or by
// sharing it with everyone when it wasn't.
func (s *Store) SetPlaylistShares(ctx context.Context, owner, id int64, userIDs []int64, all bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var wasAll bool
	err = tx.QueryRowContext(ctx, `SELECT shared_with_all FROM playlists WHERE id = ? AND user_id = ?`, id, owner).Scan(&wasAll)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE playlists SET shared_with_all = ? WHERE id = ?`, all, id); err != nil {
		return err
	}
	if all && !wasAll {
		if _, err := tx.ExecContext(ctx, `DELETE FROM playlist_shares WHERE playlist_id = ? AND status = 'removed'`, id); err != nil {
			return err
		}
	}
	wanted := map[int64]bool{}
	for _, u := range userIDs {
		if u != owner {
			wanted[u] = true
		}
	}
	if !all {
		rows, err := tx.QueryContext(ctx, `SELECT user_id FROM playlist_shares WHERE playlist_id = ?`, id)
		if err != nil {
			return err
		}
		var gone []int64
		for rows.Next() {
			var u int64
			if err := rows.Scan(&u); err != nil {
				rows.Close()
				return err
			}
			if !wanted[u] {
				gone = append(gone, u)
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		for _, u := range gone {
			if _, err := tx.ExecContext(ctx, `DELETE FROM playlist_shares WHERE playlist_id = ? AND user_id = ?`, id, u); err != nil {
				return err
			}
		}
	}
	for u := range wanted {
		// Unknown ids are skipped: someone can be deleted while the owner is choosing.
		if _, err := tx.ExecContext(ctx, `INSERT INTO playlist_shares (playlist_id, user_id, status) SELECT ?, id, 'pending' FROM users WHERE id = ?
			ON CONFLICT(playlist_id, user_id) DO UPDATE SET status = 'pending' WHERE status = 'removed'`, id, u); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// PlaylistShareTargets lists everyone a user could share a playlist with.
func (s *Store) PlaylistShareTargets(ctx context.Context, uid int64) ([]PlaylistShare, error) {
	out := []PlaylistShare{}
	err := eachRow(ctx, s.db, `SELECT id, display_name FROM users WHERE id != ? ORDER BY display_name COLLATE NOCASE`, []any{uid}, func(r *sql.Rows) error {
		var t PlaylistShare
		if err := r.Scan(&t.UserID, &t.Name); err != nil {
			return err
		}
		out = append(out, t)
		return nil
	})
	return out, err
}

// PlaylistInvites lists the playlists offered to someone that they haven't answered.
func (s *Store) PlaylistInvites(ctx context.Context, acc Access) ([]Playlist, error) {
	return s.scanPlaylists(ctx, acc, `SELECT `+playlistCols+` FROM playlists pl WHERE pl.kind IN ('video', 'audio') AND `+invitedShare+` ORDER BY pl.updated_at DESC`,
		acc.UserID, acc.UserID, acc.UserID)
}

// AcceptPlaylistShare adds a playlist offered to someone to their playlists.
func (s *Store) AcceptPlaylistShare(ctx context.Context, acc Access, id int64) (Playlist, error) {
	if p, err := s.ReadablePlaylist(ctx, acc, id); err == nil && p.ReadOnly {
		return p, nil // accepted already
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO playlist_shares (playlist_id, user_id, status)
		SELECT pl.id, ?, 'accepted' FROM playlists pl WHERE pl.id = ? AND `+invitedShare+`
		ON CONFLICT(playlist_id, user_id) DO UPDATE SET status = 'accepted'`, acc.UserID, id, acc.UserID, acc.UserID, acc.UserID)
	if err != nil {
		return Playlist{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return Playlist{}, ErrNotFound
	}
	return s.ReadablePlaylist(ctx, acc, id)
}

// RemovePlaylistShare declines a playlist offered to someone, or takes it out of their
// playlists. On one shared with everyone a mark keeps it from being offered again.
func (s *Store) RemovePlaylistShare(ctx context.Context, uid, id int64) error {
	var all bool
	err := s.db.QueryRowContext(ctx, `SELECT pl.shared_with_all FROM playlists pl WHERE pl.id = ? AND pl.user_id != ?
		AND (pl.shared_with_all = 1 OR EXISTS (SELECT 1 FROM playlist_shares sh WHERE sh.playlist_id = pl.id AND sh.user_id = ? AND sh.status != 'removed'))`,
		id, uid, uid).Scan(&all)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if all {
		_, err = s.db.ExecContext(ctx, `INSERT INTO playlist_shares (playlist_id, user_id, status) VALUES (?, ?, 'removed')
			ON CONFLICT(playlist_id, user_id) DO UPDATE SET status = 'removed'`, id, uid)
		return err
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM playlist_shares WHERE playlist_id = ? AND user_id = ?`, id, uid)
	return err
}
