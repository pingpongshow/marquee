package items

import (
	"context"
	"database/sql"
	"errors"
	"strings"
)

// PersonCredit is one item a person appears in.
type PersonCredit struct {
	Item            Summary
	Role, Character string
}

type Person struct {
	ID       int64
	Name     string
	HasPhoto bool
	Credits  []PersonCredit
}

// Person returns a person and the visible items they are credited on, newest first.
// Episode credits are folded into their show.
func (s *Store) Person(ctx context.Context, acc Access, id int64) (Person, error) {
	var p Person
	var photo sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT id, name, photo_path FROM people WHERE id = ?`, id).Scan(&p.ID, &p.Name, &photo)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	if err != nil {
		return p, err
	}
	p.HasPhoto = photo.String != ""
	ac, aargs := acc.clause()
	rows, err := s.db.QueryContext(ctx, `SELECT `+cols(acc.UserID)+`, c.role, COALESCE(c.character, '')`+summaryFrom+`
		JOIN (SELECT CASE WHEN e.type = 'episode' THEN e.grandparent_id ELSE e.id END AS item_id, MIN(c.role) AS role, MAX(c.character) AS character
		      FROM credits c JOIN items e ON e.id = c.item_id WHERE c.person_id = ? GROUP BY 1) c ON c.item_id = i.id
		WHERE i.extra_type IS NULL AND `+ac+` ORDER BY COALESCE(i.year, 0) DESC, i.sort_title COLLATE NOCASE`, append([]any{id}, aargs...)...)
	if err != nil {
		return p, err
	}
	defer rows.Close()
	p.Credits = []PersonCredit{}
	for rows.Next() {
		var c PersonCredit
		sum, err := scanSummary(rows, &c.Role, &c.Character)
		if err != nil {
			return p, err
		}
		c.Item = sum
		p.Credits = append(p.Credits, c)
	}
	return p, rows.Err()
}

// Related returns up to limit items of the same type and library ranked by shared genres
// and cast/crew ("More like this"). Plain ? parameters after ?3 continue the numbering,
// so the access-clause and LIMIT arguments follow the three numbered ones.
func (s *Store) Related(ctx context.Context, acc Access, id int64, limit int) ([]Summary, error) {
	var lib int64
	var typ string
	if err := s.db.QueryRowContext(ctx, `SELECT library_id, type FROM items WHERE id = ?`, id).Scan(&lib, &typ); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	if typ != "movie" && typ != "show" && typ != "artist" && typ != "album" {
		return []Summary{}, nil
	}
	ac, aargs := acc.clause()
	rows, err := s.db.QueryContext(ctx, `SELECT `+cols(acc.UserID)+summaryFrom+`
		JOIN (SELECT o.id AS rid, 2.0 * COUNT(*) AS score FROM item_tags mine
		        JOIN tags t ON t.id = mine.tag_id AND t.kind = 'genre'
		        JOIN item_tags ot ON ot.tag_id = mine.tag_id JOIN items o ON o.id = ot.item_id
		      WHERE mine.item_id = ?1 AND o.id != ?1 AND o.library_id = ?2 AND o.type = ?3 GROUP BY o.id
		      UNION ALL
		      SELECT o.id, 3.0 * COUNT(*) FROM credits mine JOIN credits oc ON oc.person_id = mine.person_id AND mine.ord < 8 AND oc.ord < 8
		        JOIN items o ON o.id = oc.item_id
		      WHERE mine.item_id = ?1 AND o.id != ?1 AND o.library_id = ?2 AND o.type = ?3 GROUP BY o.id) r ON r.rid = i.id
		WHERE `+ac+` GROUP BY i.id ORDER BY SUM(r.score) DESC, COALESCE(i.audience_rating, 0) DESC LIMIT ?`,
		append(append([]any{id, lib, typ}, aargs...), limit)...)
	if err != nil {
		return nil, err
	}
	return collect(rows)
}

// Leaves returns the playable items under a container in play order (episodes by season
// and number, tracks by album, disc and number), up to 2,000.
func (s *Store) Leaves(ctx context.Context, acc Access, id int64, unwatchedFirst bool) ([]Summary, error) {
	var typ string
	if err := s.db.QueryRowContext(ctx, `SELECT type FROM items WHERE id = ?`, id).Scan(&typ); errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	} else if err != nil {
		return nil, err
	}
	ac, aargs := acc.clause()
	var where, order string
	switch typ {
	case "show":
		where, order = `i.grandparent_id = ? AND i.type = 'episode'`, `p.idx = 0, p.idx, i.idx`
	case "season", "album":
		where, order = `i.parent_id = ? AND i.type IN ('episode', 'track')`, `COALESCE(i.disc, 0), i.idx`
	case "artist":
		where, order = `i.grandparent_id = ? AND i.type = 'track'`, `COALESCE(p.year, 9999), p.sort_title, COALESCE(i.disc, 0), i.idx`
	case "collection":
		where, order = `i.id IN (SELECT item_id FROM collection_items WHERE collection_id = ?)`, `i.year, i.sort_title`
	default:
		where, order = `i.id = ?`, `i.id`
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+cols(acc.UserID)+summaryFrom+` WHERE `+where+` AND i.extra_type IS NULL AND `+ac+
		` ORDER BY `+order+` LIMIT 2000`, append([]any{id}, aargs...)...)
	if err != nil {
		return nil, err
	}
	out, err := collect(rows)
	if err != nil || !unwatchedFirst {
		return out, err
	}
	// Continue from the first unwatched episode, like Plex's Play on a show.
	for i, it := range out {
		if it.ViewCount == 0 {
			return out[i:], nil
		}
	}
	return out, nil
}

// ByIDs returns the visible items among ids, in the given order.
func (s *Store) ByIDs(ctx context.Context, acc Access, ids []int64) ([]Summary, error) {
	if len(ids) == 0 {
		return []Summary{}, nil
	}
	ac, aargs := acc.clause()
	args := make([]any, 0, len(ids)+len(aargs))
	ph := make([]string, len(ids))
	for i, id := range ids {
		ph[i] = "?"
		args = append(args, id)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+cols(acc.UserID)+summaryFrom+` WHERE i.id IN (`+strings.Join(ph, ",")+`) AND `+ac,
		append(args, aargs...)...)
	if err != nil {
		return nil, err
	}
	list, err := collect(rows)
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]Summary, len(list))
	for _, it := range list {
		byID[it.ID] = it
	}
	out := make([]Summary, 0, len(ids))
	for _, id := range ids {
		if it, ok := byID[id]; ok {
			out = append(out, it)
		}
	}
	return out, nil
}

// SetRating stores (or clears, with nil) the user's 0–10 rating.
func (s *Store) SetRating(ctx context.Context, uid, itemID int64, rating *float64) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO user_item_state(user_id, item_id, rating) VALUES (?, ?, ?)
		ON CONFLICT(user_id, item_id) DO UPDATE SET rating = excluded.rating, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')`, uid, itemID, rating)
	return err
}

// TrackIDsWhere returns music track ids matching a genre (on the track or its album/artist)
// or a decade, in a library the user can see.
func (s *Store) TrackIDsByGenre(ctx context.Context, acc Access, genre string) ([]int64, error) {
	ac, aargs := acc.clause()
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT i.id`+summaryFrom+`
		JOIN item_tags it ON it.item_id IN (i.id, i.parent_id, i.grandparent_id) JOIN tags t ON t.id = it.tag_id AND t.kind = 'genre'
		WHERE i.type = 'track' AND t.name = ? COLLATE NOCASE AND `+ac, append([]any{genre}, aargs...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		rows.Scan(&id)
		out = append(out, id)
	}
	return out, rows.Err()
}
