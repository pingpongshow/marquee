package items

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

var ErrPlaylistKind = errors.New("none of these items fit (a collection takes films, shows, seasons, episodes and videos from its own library; a playlist takes tracks)")

// ErrCollectionLibrary is returned when a collection (a video playlist, USER-23) is made
// without a video library that allows them.
var ErrCollectionLibrary = errors.New("a collection belongs to a video library that allows collections")

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
	// FolderID is the viewer's folder the playlist is in (MUSIC-28), 0 if none.
	FolderID int64
	// LibraryID is the video library a collection (kind video) belongs to (USER-23).
	LibraryID int64
	// Cover is the picture its owner gave it (MUSIC-30): a path inside the image cache, "" for
	// the mosaic of its items.
	Cover string
}

type PlaylistEntry struct {
	EntryID int64
	Item    Summary
}

var leafTypes = map[string]string{"video": "'movie', 'episode', 'video'", "audio": "'track'"}

// collectionTypes are what a collection holds, as added: a show stays a show.
var collectionTypes = map[string]bool{"movie": true, "show": true, "season": true, "episode": true, "video": true}

// collectionsOn is the SQL test for playlist pl being a music playlist, or a collection whose
// library still allows them (the library's personalCollections option, on unless turned off).
const collectionsOn = `(pl.kind = 'audio' OR EXISTS (SELECT 1 FROM libraries l WHERE l.id = pl.library_id
	AND COALESCE(json_extract(l.options, '$.personalCollections'), 1) != 0))`

const playlistCols = `pl.id, pl.title, pl.kind, pl.updated_at, COALESCE(pl.smart_rule, ''), pl.user_id,
	(SELECT COUNT(*) FROM playlist_items x WHERE x.playlist_id = pl.id),
	(SELECT COALESCE(SUM(i.duration_ms), 0) FROM playlist_items x JOIN items i ON i.id = x.item_id WHERE x.playlist_id = pl.id),
	pl.shared_with_all, (SELECT display_name FROM users u WHERE u.id = pl.user_id),
	(SELECT COUNT(*) FROM playlist_shares sh WHERE sh.playlist_id = pl.id AND sh.status IN ('pending', 'accepted')),
	COALESCE(pl.library_id, 0), COALESCE(pl.cover_path, '')`

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
		if err := rows.Scan(&p.ID, &p.Title, &p.Kind, &updated, &rules, &owner, &p.ItemCount, &p.DurationMS, &p.SharedWithAll, &p.OwnerName, &p.ShareCount, &p.LibraryID, &p.Cover); err != nil {
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
	if err := s.fillPlaylistFolders(ctx, acc.UserID, out); err != nil {
		return nil, err
	}
	return out, nil
}

// Playlists lists a user's playlists and collections and those shared with them that they
// accepted (kind "" = all), most recently changed first.
func (s *Store) Playlists(ctx context.Context, acc Access, kind string) ([]Playlist, error) {
	return s.playlistsIn(ctx, acc, kind, 0)
}

// OwnCollections lists the collections (USER-23) of one video library: the user's own and those
// shared with them that they accepted.
func (s *Store) OwnCollections(ctx context.Context, acc Access, libraryID int64) ([]Playlist, error) {
	return s.playlistsIn(ctx, acc, "video", libraryID)
}

func (s *Store) playlistsIn(ctx context.Context, acc Access, kind string, libraryID int64) ([]Playlist, error) {
	q := `SELECT ` + playlistCols + ` FROM playlists pl WHERE (pl.user_id = ? OR ` + acceptedShare + `) AND pl.kind IN ('video', 'audio') AND ` + collectionsOn
	args := []any{acc.UserID, acc.UserID}
	if kind != "" {
		q += ` AND pl.kind = ?`
		args = append(args, kind)
	}
	if libraryID != 0 {
		q += ` AND pl.library_id = ?`
		args = append(args, libraryID)
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

// CreateOwnCollection makes a collection of the user's own (USER-23) in a video library that allows them.
func (s *Store) CreateOwnCollection(ctx context.Context, acc Access, title string, libraryID int64, itemIDs []int64) (Playlist, error) {
	var ok bool
	err := s.db.QueryRowContext(ctx, `SELECT type IN ('movies', 'shows', 'anime', 'videos') AND COALESCE(json_extract(options, '$.personalCollections'), 1) != 0
		FROM libraries WHERE id = ?`, libraryID).Scan(&ok)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !ok) {
		return Playlist{}, ErrCollectionLibrary
	}
	if err != nil {
		return Playlist{}, err
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO playlists (user_id, title, kind, library_id) VALUES (?, ?, 'video', ?)`, acc.UserID, strings.TrimSpace(title), libraryID)
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

// CreatePlaylist makes a music playlist; a collection (kind video) is made with
// CreateOwnCollection, which gives it its library.
func (s *Store) CreatePlaylist(ctx context.Context, acc Access, title, kind string, itemIDs []int64, rules *SmartRules) (Playlist, error) {
	if kind == "video" {
		return Playlist{}, ErrCollectionLibrary
	}
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

// SetPlaylistCover gives uid's playlist or collection a picture of its own (MUSIC-30), or takes
// it away (path ""). It returns the picture's file it replaced when nothing else uses it, for
// the caller to remove.
func (s *Store) SetPlaylistCover(ctx context.Context, uid, id int64, path string) (string, error) {
	p, err := s.Playlist(ctx, uid, id)
	if err != nil {
		return "", err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE playlists SET cover_path = NULLIF(?, ''), updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = ?`, path, id); err != nil {
		return "", err
	}
	if p.Cover == "" || p.Cover == path {
		return "", nil
	}
	var others int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM playlists WHERE cover_path = ?`, p.Cover).Scan(&others); err != nil || others > 0 {
		return "", err
	}
	return p.Cover, nil
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

// AddToPlaylist appends items. A playlist takes tracks, with artists and albums expanded into
// theirs; a collection takes titles of its own library as they are (a show stays a show), each
// once. Items that don't fit are skipped.
func (s *Store) AddToPlaylist(ctx context.Context, acc Access, id int64, itemIDs []int64) error {
	pl, err := s.Playlist(ctx, acc.UserID, id)
	if err != nil {
		return err
	}
	if pl.Rules != nil {
		return ErrSmartPlaylist
	}
	var add []int64
	if pl.Kind == "video" {
		add, err = s.collectionAdds(ctx, acc, pl, itemIDs)
		if err != nil || add == nil {
			return err
		}
		itemIDs = nil
	}
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

// collectionAdds picks what of itemIDs goes into collection pl: titles acc can see, of its
// library, not already in it. Nil with no error means they're all in it already.
func (s *Store) collectionAdds(ctx context.Context, acc Access, pl Playlist, itemIDs []int64) ([]int64, error) {
	have := map[int64]bool{}
	err := eachRow(ctx, s.db, `SELECT item_id FROM playlist_items WHERE playlist_id = ?`, []any{pl.ID}, func(r *sql.Rows) error {
		var item int64
		if err := r.Scan(&item); err != nil {
			return err
		}
		have[item] = true
		return nil
	})
	if err != nil {
		return nil, err
	}
	ac, aargs := acc.clause()
	var add []int64
	fits := false
	for _, itemID := range itemIDs {
		var typ string
		var lib int64
		err := s.db.QueryRowContext(ctx, `SELECT i.type, i.library_id`+summaryFrom+` WHERE i.id = ? AND `+ac, append([]any{itemID}, aargs...)...).Scan(&typ, &lib)
		if errors.Is(err, sql.ErrNoRows) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if !collectionTypes[typ] || lib != pl.LibraryID {
			continue
		}
		fits = true
		if !have[itemID] {
			have[itemID] = true
			add = append(add, itemID)
		}
	}
	if !fits {
		return nil, ErrPlaylistKind
	}
	return add, nil
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
