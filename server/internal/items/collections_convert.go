package items

import (
	"context"
	"database/sql"
	"log/slog"
)

// ConvertVideoPlaylists turns the video playlists made before collections (USER-23) into
// collections: each goes to the library its titles are in, and one with titles from several
// libraries becomes one collection per library, with the same name, sharing and folder. A
// show whose every episode was in the playlist (adding a show used to add its episodes)
// becomes the show; a smart one keeps what its rules found now. It runs when the server
// starts and after a Plex import, and does nothing once they're all converted.
func (s *Store) ConvertVideoPlaylists(ctx context.Context) error {
	type old struct {
		id, owner int64
		rules     string
	}
	var todo []old
	err := eachRow(ctx, s.db, `SELECT id, user_id, COALESCE(smart_rule, '') FROM playlists WHERE kind = 'video' AND library_id IS NULL ORDER BY id`, nil, func(r *sql.Rows) error {
		var o old
		if err := r.Scan(&o.id, &o.owner, &o.rules); err != nil {
			return err
		}
		todo = append(todo, o)
		return nil
	})
	if err != nil || len(todo) == 0 {
		return err
	}
	// One with nothing in it goes to the first video library.
	var firstLibrary int64
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE((SELECT id FROM libraries WHERE type IN ('movies', 'shows', 'anime', 'videos') ORDER BY sort_order, id LIMIT 1), 0)`).Scan(&firstLibrary); err != nil {
		return err
	}
	made := 0
	for _, o := range todo {
		var smart []int64
		if r := parseRules(o.rules); r != nil {
			list, _, err := s.SmartItems(ctx, Access{UserID: o.owner}, "video", *r, 0, 5000)
			if err != nil {
				return err
			}
			for _, it := range list {
				smart = append(smart, it.ID)
			}
		}
		n, err := s.convertVideoPlaylist(ctx, o.id, o.rules != "", smart, firstLibrary)
		if err != nil {
			return err
		}
		made += n
	}
	slog.Info("video playlists are now collections in their libraries", "playlists", len(todo), "collections", made)
	return nil
}

func (s *Store) convertVideoPlaylist(ctx context.Context, id int64, wasSmart bool, smart []int64, firstLibrary int64) (int, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if wasSmart {
		if _, err := tx.ExecContext(ctx, `DELETE FROM playlist_items WHERE playlist_id = ?`, id); err != nil {
			return 0, err
		}
		for i, item := range smart {
			if _, err := tx.ExecContext(ctx, `INSERT INTO playlist_items (playlist_id, item_id, ord) VALUES (?, ?, ?)`, id, item, float64(i+1)); err != nil {
				return 0, err
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE playlists SET smart_rule = NULL WHERE id = ?`, id); err != nil {
			return 0, err
		}
	}
	type entry struct{ item, library, show int64 }
	var entries []entry
	inShow := map[int64]int{}
	rows, err := tx.QueryContext(ctx, `SELECT i.id, i.library_id, CASE WHEN i.type = 'episode' THEN COALESCE(i.grandparent_id, 0) ELSE 0 END
		FROM playlist_items x JOIN items i ON i.id = x.item_id WHERE x.playlist_id = ? ORDER BY x.ord, x.id`, id)
	if err != nil {
		return 0, err
	}
	seen := map[int64]bool{}
	for rows.Next() {
		var e entry
		if err := rows.Scan(&e.item, &e.library, &e.show); err != nil {
			rows.Close()
			return 0, err
		}
		if seen[e.item] {
			continue
		}
		seen[e.item] = true
		entries = append(entries, e)
		if e.show != 0 {
			inShow[e.show]++
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	// A show that's here whole is the show.
	whole := map[int64]bool{}
	for show, n := range inShow {
		var total int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM items WHERE grandparent_id = ? AND type = 'episode'`, show).Scan(&total); err != nil {
			return 0, err
		}
		whole[show] = n >= total && total > 1
	}
	var libraries []int64
	byLibrary := map[int64][]int64{}
	for _, e := range entries {
		item := e.item
		if whole[e.show] {
			if seen[e.show] {
				continue
			}
			seen[e.show] = true
			item = e.show
		}
		if _, ok := byLibrary[e.library]; !ok {
			libraries = append(libraries, e.library)
		}
		byLibrary[e.library] = append(byLibrary[e.library], item)
	}
	if len(libraries) == 0 {
		if firstLibrary == 0 {
			return 0, nil // no video library to put it in yet
		}
		libraries = []int64{firstLibrary}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM playlist_items WHERE playlist_id = ?`, id); err != nil {
		return 0, err
	}
	for n, library := range libraries {
		target := id
		if n == 0 {
			if _, err := tx.ExecContext(ctx, `UPDATE playlists SET library_id = ? WHERE id = ?`, library, id); err != nil {
				return 0, err
			}
		} else {
			res, err := tx.ExecContext(ctx, `INSERT INTO playlists (user_id, title, kind, created_at, updated_at, shared_with_all, cover_path, library_id)
				SELECT user_id, title, kind, created_at, updated_at, shared_with_all, cover_path, ? FROM playlists WHERE id = ?`, library, id)
			if err != nil {
				return 0, err
			}
			target, _ = res.LastInsertId()
			if _, err := tx.ExecContext(ctx, `INSERT INTO playlist_shares (playlist_id, user_id, status, created_at)
				SELECT ?, user_id, status, created_at FROM playlist_shares WHERE playlist_id = ?`, target, id); err != nil {
				return 0, err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO playlist_folder_entries (user_id, playlist_id, folder_id)
				SELECT user_id, ?, folder_id FROM playlist_folder_entries WHERE playlist_id = ?`, target, id); err != nil {
				return 0, err
			}
		}
		for i, item := range byLibrary[library] {
			if _, err := tx.ExecContext(ctx, `INSERT INTO playlist_items (playlist_id, item_id, ord) VALUES (?, ?, ?)`, target, item, float64(i+1)); err != nil {
				return 0, err
			}
		}
	}
	return len(libraries), tx.Commit()
}
