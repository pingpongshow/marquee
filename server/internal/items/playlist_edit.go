package items

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// ---------- Editing and combining playlists (MUSIC-29) ----------

// ErrPlaylistMix is returned when combining playlists of different kinds (songs and videos).
var ErrPlaylistMix = errors.New("music and video playlists can't be combined")

// SetPlaylistOrder puts uid's playlist's entries in the order given and removes the ones left
// out, in one go: the playlist editor's sort, shuffle, drag and bulk remove. Entries the
// person can't see (a library taken away from them) aren't theirs to remove: they keep their
// places after the ones listed. Unknown entry ids are ignored.
func (s *Store) SetPlaylistOrder(ctx context.Context, acc Access, id int64, entryIDs []int64) error {
	pl, err := s.Playlist(ctx, acc.UserID, id)
	if err != nil {
		return err
	}
	if pl.Rules != nil {
		return ErrSmartPlaylist
	}
	ac, aargs := acc.clause()
	visible := map[int64]bool{}
	err = eachRow(ctx, s.db, `SELECT x.id`+summaryFrom+` JOIN playlist_items x ON x.item_id = i.id WHERE x.playlist_id = ? AND `+ac,
		append([]any{id}, aargs...), func(r *sql.Rows) error {
			var e int64
			if err := r.Scan(&e); err != nil {
				return err
			}
			visible[e] = true
			return nil
		})
	if err != nil {
		return err
	}
	var hidden []int64
	err = eachRow(ctx, s.db, `SELECT id FROM playlist_items WHERE playlist_id = ? ORDER BY ord`, []any{id}, func(r *sql.Rows) error {
		var e int64
		if err := r.Scan(&e); err != nil {
			return err
		}
		if !visible[e] {
			hidden = append(hidden, e)
		}
		return nil
	})
	if err != nil {
		return err
	}
	keep := make([]int64, 0, len(entryIDs)+len(hidden))
	seen := map[int64]bool{}
	for _, e := range entryIDs {
		if visible[e] && !seen[e] {
			seen[e] = true
			keep = append(keep, e)
		}
	}
	keep = append(keep, hidden...)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for e := range visible {
		if !seen[e] {
			if _, err := tx.ExecContext(ctx, `DELETE FROM playlist_items WHERE id = ? AND playlist_id = ?`, e, id); err != nil {
				return err
			}
		}
	}
	for i, e := range keep {
		if _, err := tx.ExecContext(ctx, `UPDATE playlist_items SET ord = ? WHERE id = ? AND playlist_id = ?`, float64(i+1), e, id); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE playlists SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// MergeOptions says how playlists are combined.
type MergeOptions struct {
	// IntoID adds the sources to the end of this playlist of uid's; 0 makes a new one titled
	// Title.
	IntoID int64
	Title  string
	// SkipDuplicates leaves out anything already in the playlist (or added from an earlier
	// source), so each song or video is in it once.
	SkipDuplicates bool
	// DeleteSources deletes the sources afterwards (only uid's own; shared ones stay).
	DeleteSources bool
}

// MergePlaylists combines playlists, in the order given, into a new playlist or the end of an
// existing one of acc's: theirs, ones shared with them and smart ones (as they are now). All
// must be of one kind. Returns the playlist they went into.
func (s *Store) MergePlaylists(ctx context.Context, acc Access, sourceIDs []int64, opt MergeOptions) (Playlist, error) {
	var kind string
	var sources []Playlist
	seenSource := map[int64]bool{}
	for _, sid := range sourceIDs {
		if seenSource[sid] || sid == opt.IntoID {
			continue
		}
		seenSource[sid] = true
		p, err := s.ReadablePlaylist(ctx, acc, sid)
		if err != nil {
			return Playlist{}, err
		}
		if kind != "" && p.Kind != kind {
			return Playlist{}, ErrPlaylistMix
		}
		kind = p.Kind
		sources = append(sources, p)
	}
	var into Playlist
	if opt.IntoID != 0 {
		p, err := s.Playlist(ctx, acc.UserID, opt.IntoID)
		if err != nil {
			return Playlist{}, err
		}
		if p.Rules != nil {
			return Playlist{}, ErrSmartPlaylist
		}
		if kind != "" && p.Kind != kind {
			return Playlist{}, ErrPlaylistMix
		}
		into = p
	} else {
		if len(sources) == 0 {
			return Playlist{}, ErrNotFound
		}
		title := strings.TrimSpace(opt.Title)
		if title == "" {
			title = sources[0].Title
		}
		p, err := s.CreatePlaylist(ctx, acc, title, kind, nil, nil)
		if err != nil {
			return Playlist{}, err
		}
		into = p
	}

	have := map[int64]bool{}
	if opt.SkipDuplicates && opt.IntoID != 0 {
		err := eachRow(ctx, s.db, `SELECT item_id FROM playlist_items WHERE playlist_id = ?`, []any{into.ID}, func(r *sql.Rows) error {
			var item int64
			if err := r.Scan(&item); err != nil {
				return err
			}
			have[item] = true
			return nil
		})
		if err != nil {
			return Playlist{}, err
		}
	}
	var add []int64
	for _, src := range sources {
		for offset := 0; ; offset += 2000 {
			page, total, err := s.PlaylistItems(ctx, acc, src.ID, offset, 2000)
			if err != nil {
				return Playlist{}, err
			}
			for _, e := range page {
				if opt.SkipDuplicates && have[e.Item.ID] {
					continue
				}
				have[e.Item.ID] = true
				add = append(add, e.Item.ID)
			}
			if len(page) == 0 || offset+len(page) >= total {
				break
			}
		}
	}
	if len(add) > 0 {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return Playlist{}, err
		}
		defer tx.Rollback()
		var maxOrd float64
		tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(ord), 0) FROM playlist_items WHERE playlist_id = ?`, into.ID).Scan(&maxOrd)
		for i, item := range add {
			if _, err := tx.ExecContext(ctx, `INSERT INTO playlist_items (playlist_id, item_id, ord) VALUES (?, ?, ?)`, into.ID, item, maxOrd+float64(i+1)); err != nil {
				return Playlist{}, err
			}
		}
		if _, err := tx.ExecContext(ctx, `UPDATE playlists SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = ?`, into.ID); err != nil {
			return Playlist{}, err
		}
		if err := tx.Commit(); err != nil {
			return Playlist{}, err
		}
	}
	if opt.DeleteSources {
		for _, src := range sources {
			if src.ReadOnly || src.ID == into.ID {
				continue
			}
			if err := s.DeletePlaylist(ctx, acc.UserID, src.ID); err != nil && !errors.Is(err, ErrNotFound) {
				return Playlist{}, err
			}
		}
	}
	return s.Playlist(ctx, acc.UserID, into.ID)
}
