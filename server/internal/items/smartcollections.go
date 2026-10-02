package items

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"marquee/internal/scanner/naming"
)

// Smart collections (META-7, D86): a collection whose members are a saved library filter,
// so it stays current as the library changes (e.g. "Unwatched 90s action").

type SmartCollection struct {
	ID, LibraryID int64
	ItemType      string // movie or show
	Filter        Filter
	Sort          string
	Max           int // 0 = all
}

// Smart returns the collection's rules when it is a smart collection.
func (s *Store) Smart(ctx context.Context, id int64) (SmartCollection, bool) {
	var sc SmartCollection
	var f string
	err := s.db.QueryRowContext(ctx, `SELECT i.id, i.library_id, sc.item_type, sc.filter, sc.sort, sc.max_items
		FROM smart_collections sc JOIN items i ON i.id = sc.item_id WHERE sc.item_id = ?`, id).
		Scan(&sc.ID, &sc.LibraryID, &sc.ItemType, &f, &sc.Sort, &sc.Max)
	if err != nil {
		return sc, false
	}
	json.Unmarshal([]byte(f), &sc.Filter)
	return sc, true
}

var ErrInvalidSmart = errors.New("a smart collection lists movies or shows, with a known sort")

// SaveSmart creates (id == 0) or updates a smart collection in a library.
func (s *Store) SaveSmart(ctx context.Context, sc SmartCollection, title string) (int64, error) {
	if sc.ItemType != "movie" && sc.ItemType != "show" {
		return 0, ErrInvalidSmart
	}
	if sc.Sort == "" {
		sc.Sort = "title"
	}
	if _, ok := sorts[sc.Sort]; !ok {
		return 0, ErrInvalidSmart
	}
	if sc.Max < 0 {
		sc.Max = 0
	}
	sc.Filter.Letter = ""
	f, _ := json.Marshal(sc.Filter)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	id := sc.ID
	title = strings.TrimSpace(title)
	if id == 0 {
		if title == "" {
			return 0, errors.New("a collection needs a name")
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO items (library_id, type, title, sort_title, match_state) VALUES (?, 'collection', ?, ?, 'local')`,
			sc.LibraryID, title, naming.SortTitle(title))
		if err != nil {
			return 0, err
		}
		id, _ = res.LastInsertId()
	} else if title != "" {
		if _, err := tx.ExecContext(ctx, `UPDATE items SET title = ?, sort_title = ? WHERE id = ? AND type = 'collection'`, title, naming.SortTitle(title), id); err != nil {
			return 0, err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO smart_collections (item_id, item_type, filter, sort, max_items) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT (item_id) DO UPDATE SET item_type = excluded.item_type, filter = excluded.filter, sort = excluded.sort, max_items = excluded.max_items`,
		id, sc.ItemType, string(f), sc.Sort, sc.Max); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	// The count shown on its card.
	if _, total, err := s.List(ctx, Access{}, sc.LibraryID, sc.ItemType, sc.Sort, sc.Filter, 0, 1); err == nil {
		if sc.Max > 0 && total > sc.Max {
			total = sc.Max
		}
		s.db.ExecContext(ctx, `UPDATE items SET child_count = ?, leaf_count = ? WHERE id = ?`, total, total, id)
	}
	return id, nil
}

