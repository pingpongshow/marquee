package metadata

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strconv"

	"marquee/internal/items"
	"marquee/internal/metadata/tmdb"
	"marquee/internal/scanner/naming"
)

// setCollection puts a movie in its TMDB collection (META-7), creating the collection in
// the movie's library the first time. Manual collections are left alone.
func setCollection(ctx context.Context, tx *sql.Tx, itemID int64, c *tmdb.CollectionRef) error {
	rows, err := tx.QueryContext(ctx, `SELECT collection_id FROM collection_items WHERE item_id = ?
		AND collection_id IN (SELECT item_id FROM external_ids WHERE provider = 'tmdb_collection')`, itemID)
	if err != nil {
		return err
	}
	var old []int64
	for rows.Next() {
		var id int64
		rows.Scan(&id)
		old = append(old, id)
	}
	rows.Close()
	var cid int64
	if c != nil && c.ID != 0 {
		var lib int64
		var year sql.NullInt64
		if err := tx.QueryRowContext(ctx, `SELECT library_id, year FROM items WHERE id = ?`, itemID).Scan(&lib, &year); err != nil {
			return err
		}
		err := tx.QueryRowContext(ctx, `SELECT c.id FROM items c JOIN external_ids x ON x.item_id = c.id AND x.provider = 'tmdb_collection'
			WHERE x.value = ? AND c.library_id = ? AND c.type = 'collection'`, strconv.Itoa(c.ID), lib).Scan(&cid)
		if errors.Is(err, sql.ErrNoRows) {
			res, err := tx.ExecContext(ctx, `INSERT INTO items (library_id, type, title, sort_title, match_state) VALUES (?, 'collection', ?, ?, 'matched')`,
				lib, c.Name, naming.SortTitle(c.Name))
			if err != nil {
				return err
			}
			cid, _ = res.LastInsertId()
			if err := finishMatch(ctx, tx, cid, map[string]string{"tmdb_collection": strconv.Itoa(c.ID)}); err != nil {
				return err
			}
			if err := setArtwork(ctx, tx, cid, tmdb.Images{}, c.PosterPath, c.BackdropPath); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO collection_items (collection_id, item_id, ord) VALUES (?, ?, ?)`,
			cid, itemID, year.Int64); err != nil {
			return err
		}
	}
	for _, o := range old {
		if o == cid {
			continue
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM collection_items WHERE collection_id = ? AND item_id = ?`, o, itemID); err != nil {
			return err
		}
		if err := items.RecountCollection(ctx, tx, o); err != nil {
			return err
		}
	}
	if cid != 0 {
		return items.RecountCollection(ctx, tx, cid)
	}
	return nil
}

// SyncCollections puts every matched movie in its TMDB collection and removes automatic
// collections left empty. It runs weekly, so collections appear for movies matched before
// collections existed and follow TMDB's changes.
func (s *Service) SyncCollections(ctx context.Context) (string, error) {
	c, err := s.tmdb("")
	if err != nil {
		return "TMDB isn't set up", nil
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT i.id, x.value FROM items i
		JOIN external_ids x ON x.item_id = i.id AND x.provider = 'tmdb'
		WHERE i.type = 'movie' AND i.match_state = 'matched'`)
	if err != nil {
		return "", err
	}
	type movie struct {
		id   int64
		tmdb int
	}
	var movies []movie
	for rows.Next() {
		var m movie
		var v string
		if rows.Scan(&m.id, &v) == nil {
			m.tmdb, _ = strconv.Atoi(v)
			movies = append(movies, m)
		}
	}
	rows.Close()
	failed := 0
	for _, m := range movies {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		info, err := c.MovieBasic(ctx, m.tmdb)
		if err != nil {
			failed++
			continue
		}
		tx, err := s.DB.BeginTx(ctx, nil)
		if err != nil {
			return "", err
		}
		if err := setCollection(ctx, tx, m.id, info.Collection); err != nil {
			tx.Rollback()
			return "", err
		}
		if err := tx.Commit(); err != nil {
			return "", err
		}
	}
	if _, err := s.DB.ExecContext(ctx, `DELETE FROM items WHERE type = 'collection'
		AND id IN (SELECT item_id FROM external_ids WHERE provider = 'tmdb_collection')
		AND NOT EXISTS (SELECT 1 FROM collection_items WHERE collection_id = items.id)`); err != nil {
		return "", err
	}
	var n int
	s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM items WHERE type = 'collection' AND child_count >= 2`).Scan(&n)
	if failed > 0 {
		slog.Warn("collections: TMDB lookups failed", "count", failed)
	}
	return fmt.Sprintf("Checked %d movies; %d collections", len(movies), n), nil
}
