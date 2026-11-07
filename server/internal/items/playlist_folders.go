package items

import (
	"context"
	"database/sql"
	"strings"
)

// PlaylistFolder is a folder someone sorts their playlists into (MUSIC-28).
type PlaylistFolder struct {
	ID            int64
	Name          string
	PlaylistCount int // the playlists in it the person can still see
}

// PlaylistFolders lists a user's folders by name.
func (s *Store) PlaylistFolders(ctx context.Context, uid int64) ([]PlaylistFolder, error) {
	out := []PlaylistFolder{}
	err := eachRow(ctx, s.db, `SELECT f.id, f.name, (SELECT COUNT(*) FROM playlist_folder_entries e JOIN playlists pl ON pl.id = e.playlist_id
		WHERE e.folder_id = f.id AND pl.kind IN ('video', 'audio') AND (pl.user_id = ? OR `+acceptedShare+`))
		FROM playlist_folders f WHERE f.user_id = ? ORDER BY f.name COLLATE NOCASE, f.id`, []any{uid, uid, uid}, func(r *sql.Rows) error {
		var f PlaylistFolder
		if err := r.Scan(&f.ID, &f.Name, &f.PlaylistCount); err != nil {
			return err
		}
		out = append(out, f)
		return nil
	})
	return out, err
}

func (s *Store) playlistFolder(ctx context.Context, uid, id int64) (PlaylistFolder, error) {
	list, err := s.PlaylistFolders(ctx, uid)
	if err != nil {
		return PlaylistFolder{}, err
	}
	for _, f := range list {
		if f.ID == id {
			return f, nil
		}
	}
	return PlaylistFolder{}, ErrNotFound
}

func (s *Store) CreatePlaylistFolder(ctx context.Context, uid int64, name string) (PlaylistFolder, error) {
	res, err := s.db.ExecContext(ctx, `INSERT INTO playlist_folders (user_id, name) VALUES (?, ?)`, uid, strings.TrimSpace(name))
	if err != nil {
		return PlaylistFolder{}, err
	}
	id, _ := res.LastInsertId()
	return s.playlistFolder(ctx, uid, id)
}

func (s *Store) RenamePlaylistFolder(ctx context.Context, uid, id int64, name string) (PlaylistFolder, error) {
	if err := s.touchPlaylist(ctx, `UPDATE playlist_folders SET name = ? WHERE id = ? AND user_id = ?`, strings.TrimSpace(name), id, uid); err != nil {
		return PlaylistFolder{}, err
	}
	return s.playlistFolder(ctx, uid, id)
}

// DeletePlaylistFolder deletes a folder. Its playlists aren't deleted: they go back to the top level.
func (s *Store) DeletePlaylistFolder(ctx context.Context, uid, id int64) error {
	return s.touchPlaylist(ctx, `DELETE FROM playlist_folders WHERE id = ? AND user_id = ?`, id, uid)
}

// MoveToPlaylistFolder puts a playlist the user can see, their own or one shared with them,
// in one of their folders, taking it out of any other.
func (s *Store) MoveToPlaylistFolder(ctx context.Context, acc Access, folderID, playlistID int64) error {
	if _, err := s.playlistFolder(ctx, acc.UserID, folderID); err != nil {
		return err
	}
	if _, err := s.ReadablePlaylist(ctx, acc, playlistID); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO playlist_folder_entries (user_id, playlist_id, folder_id) VALUES (?, ?, ?)
		ON CONFLICT(user_id, playlist_id) DO UPDATE SET folder_id = excluded.folder_id`, acc.UserID, playlistID, folderID)
	return err
}

// RemoveFromPlaylistFolder takes a playlist out of a folder, back to the top level.
func (s *Store) RemoveFromPlaylistFolder(ctx context.Context, uid, folderID, playlistID int64) error {
	return s.touchPlaylist(ctx, `DELETE FROM playlist_folder_entries WHERE user_id = ? AND folder_id = ? AND playlist_id = ?`, uid, folderID, playlistID)
}

// fillPlaylistFolders sets FolderID on the playlists that are in one of the user's folders.
func (s *Store) fillPlaylistFolders(ctx context.Context, uid int64, list []Playlist) error {
	if len(list) == 0 {
		return nil
	}
	in := map[int64]int64{}
	err := eachRow(ctx, s.db, `SELECT playlist_id, folder_id FROM playlist_folder_entries WHERE user_id = ?`, []any{uid}, func(r *sql.Rows) error {
		var p, f int64
		if err := r.Scan(&p, &f); err != nil {
			return err
		}
		in[p] = f
		return nil
	})
	for i := range list {
		list[i].FolderID = in[list[i].ID]
	}
	return err
}
