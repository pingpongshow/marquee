package items

import (
	"context"
	"database/sql"
	"errors"
	"strings"

	"marquee/internal/scanner/naming"
)

// ---------- Watchlist (USER-8) ----------

// SetWatchlist adds an item to a user's watchlist or takes it off.
func (s *Store) SetWatchlist(ctx context.Context, uid, itemID int64, on bool) error {
	if on {
		_, err := s.db.ExecContext(ctx, `INSERT INTO user_item_state (user_id, item_id, watchlisted_at)
			VALUES (?, ?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
			ON CONFLICT(user_id, item_id) DO UPDATE SET watchlisted_at = COALESCE(watchlisted_at, excluded.watchlisted_at)`, uid, itemID)
		return err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE user_item_state SET watchlisted_at = NULL WHERE user_id = ? AND item_id = ?`, uid, itemID)
	return err
}

// Watchlist returns what a user has saved to watch, most recently added first.
func (s *Store) Watchlist(ctx context.Context, acc Access, limit int) ([]Summary, error) {
	ac, aargs := acc.clause()
	rows, err := s.db.QueryContext(ctx, `SELECT `+cols(acc.UserID)+summaryFrom+`
		JOIN user_item_state w ON w.item_id = i.id AND w.user_id = ? AND w.watchlisted_at IS NOT NULL
		WHERE `+ac+` ORDER BY w.watchlisted_at DESC LIMIT ?`, append(append([]any{acc.UserID}, aargs...), limit)...)
	if err != nil {
		return nil, err
	}
	return collect(rows)
}

// ---------- Collections (META-7) ----------

// ErrNotCollection is returned when an id isn't a collection.
var ErrNotCollection = errors.New("not a collection")

// CreateCollection makes an empty manual collection in a library.
func (s *Store) CreateCollection(ctx context.Context, libID int64, title string) (int64, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return 0, errors.New("a collection needs a name")
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO items (library_id, type, title, sort_title, match_state) VALUES (?, 'collection', ?, ?, 'local')`,
		libID, title, naming.SortTitle(title))
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) collectionLibrary(ctx context.Context, id int64) (int64, error) {
	var lib int64
	var typ string
	err := s.db.QueryRowContext(ctx, `SELECT library_id, type FROM items WHERE id = ?`, id).Scan(&lib, &typ)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err == nil && typ != "collection" {
		err = ErrNotCollection
	}
	return lib, err
}

// AddToCollection adds items (from the collection's library) to it.
func (s *Store) AddToCollection(ctx context.Context, id int64, itemIDs []int64) error {
	lib, err := s.collectionLibrary(ctx, id)
	if err != nil {
		return err
	}
	for _, it := range itemIDs {
		if _, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO collection_items (collection_id, item_id, ord)
			SELECT ?, i.id, COALESCE(i.year, 0) FROM items i WHERE i.id = ? AND i.library_id = ? AND i.type != 'collection'`, id, it, lib); err != nil {
			return err
		}
	}
	return RecountCollection(ctx, s.db, id)
}

// RemoveFromCollection takes an item out of a collection.
func (s *Store) RemoveFromCollection(ctx context.Context, id, itemID int64) error {
	if _, err := s.collectionLibrary(ctx, id); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM collection_items WHERE collection_id = ? AND item_id = ?`, id, itemID); err != nil {
		return err
	}
	return RecountCollection(ctx, s.db, id)
}

// DeleteCollection removes a collection (its members stay in the library).
func (s *Store) DeleteCollection(ctx context.Context, id int64) error {
	if _, err := s.collectionLibrary(ctx, id); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM items WHERE id = ? AND type = 'collection'`, id)
	return err
}

// CollectionsOf lists the collections an item is in.
func (s *Store) CollectionsOf(ctx context.Context, acc Access, itemID int64) ([]Summary, error) {
	ac, aargs := acc.clause()
	rows, err := s.db.QueryContext(ctx, `SELECT `+cols(acc.UserID)+summaryFrom+`
		WHERE i.id IN (SELECT collection_id FROM collection_items WHERE item_id = ?) AND `+ac+` ORDER BY i.sort_title`,
		append([]any{itemID}, aargs...)...)
	if err != nil {
		return nil, err
	}
	return collect(rows)
}

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// RecountCollection refreshes a collection's member count.
func RecountCollection(ctx context.Context, db execer, id int64) error {
	_, err := db.ExecContext(ctx, `UPDATE items SET child_count = (SELECT COUNT(*) FROM collection_items WHERE collection_id = ?),
		leaf_count = (SELECT COUNT(*) FROM collection_items WHERE collection_id = ?) WHERE id = ?`, id, id, id)
	return err
}
