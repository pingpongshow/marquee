package items

import (
	"context"
	"database/sql"
	"errors"
)

// ---------- Choosing artwork (META-13) ----------
//
// An administrator can choose an item's artwork from what it has, from TMDB's alternatives or
// any image link ("chosen"), or upload one ("upload"). Chosen and uploaded artwork is kept
// through metadata refreshes (agents replace only their own and select theirs only when
// nothing is selected) until the item is set back to automatic.

// ErrArtworkKind is returned for an artwork kind that can't be chosen.
var ErrArtworkKind = errors.New("artwork kind must be poster, backdrop, logo or thumb")

var choosableKinds = map[string]bool{"poster": true, "backdrop": true, "logo": true, "thumb": true}

// Artwork is one of an item's images.
type Artwork struct {
	ID            int64
	Source        string
	RemoteURL     string
	Width, Height int
	Language      string
	Selected      bool
}

// ItemArtwork lists an item's artwork of a kind, the selected first.
func (s *Store) ItemArtwork(ctx context.Context, itemID int64, kind string) ([]Artwork, error) {
	if !choosableKinds[kind] {
		return nil, ErrArtworkKind
	}
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM items WHERE id = ?`, itemID).Scan(&n); err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, ErrNotFound
	}
	out := []Artwork{}
	err := eachRow(ctx, s.db, `SELECT id, source, COALESCE(remote_url, ''), COALESCE(width, 0), COALESCE(height, 0), COALESCE(language, ''), selected
		FROM artwork WHERE item_id = ? AND kind = ? ORDER BY selected DESC, id`, []any{itemID, kind}, func(r *sql.Rows) error {
		var a Artwork
		if err := r.Scan(&a.ID, &a.Source, &a.RemoteURL, &a.Width, &a.Height, &a.Language, &a.Selected); err != nil {
			return err
		}
		out = append(out, a)
		return nil
	})
	return out, err
}

// ChooseArtwork selects one of the item's artwork. A metadata agent's is copied as "chosen"
// first, so a refresh (which replaces the agent's own) keeps it.
func (s *Store) ChooseArtwork(ctx context.Context, itemID int64, kind string, artworkID int64) error {
	if !choosableKinds[kind] {
		return ErrArtworkKind
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var source, remote string
	var local sql.NullString
	var w, h sql.NullInt64
	var lang sql.NullString
	err = tx.QueryRowContext(ctx, `SELECT source, COALESCE(remote_url, ''), local_path, width, height, language FROM artwork WHERE id = ? AND item_id = ? AND kind = ?`,
		artworkID, itemID, kind).Scan(&source, &remote, &local, &w, &h, &lang)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if remote != "" && source != "chosen" && source != "upload" && source != "local" && source != "embedded" {
		// An agent's image: kept as chosen.
		res, err := tx.ExecContext(ctx, `INSERT INTO artwork(item_id, kind, source, remote_url, width, height, language, selected) VALUES (?, ?, 'chosen', ?, ?, ?, ?, 0)`,
			itemID, kind, remote, w, h, lang)
		if err != nil {
			return err
		}
		artworkID, _ = res.LastInsertId()
	}
	if err := selectOnly(ctx, tx, itemID, kind, artworkID); err != nil {
		return err
	}
	return tx.Commit()
}

// ChooseArtworkURL uses an image from the web (a TMDB alternative or any https link).
func (s *Store) ChooseArtworkURL(ctx context.Context, itemID int64, kind, url string, width, height int) error {
	return s.addSelected(ctx, itemID, kind, "chosen", url, "", width, height)
}

// AddUploadedArtwork uses an uploaded image, stored at path inside the image cache.
func (s *Store) AddUploadedArtwork(ctx context.Context, itemID int64, kind, path string) error {
	return s.addSelected(ctx, itemID, kind, "upload", "", path, 0, 0)
}

func (s *Store) addSelected(ctx context.Context, itemID int64, kind, source, url, path string, width, height int) error {
	if !choosableKinds[kind] {
		return ErrArtworkKind
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM items WHERE id = ?`, itemID).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	// Reuse the same image chosen before rather than piling up copies.
	var id int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM artwork WHERE item_id = ? AND kind = ? AND source = ? AND COALESCE(remote_url, '') = ? AND COALESCE(local_path, '') = ?`,
		itemID, kind, source, url, path).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		res, err := tx.ExecContext(ctx, `INSERT INTO artwork(item_id, kind, source, remote_url, local_path, width, height, selected) VALUES (?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), NULLIF(?, 0), NULLIF(?, 0), 0)`,
			itemID, kind, source, url, path, width, height)
		if err != nil {
			return err
		}
		id, _ = res.LastInsertId()
	} else if err != nil {
		return err
	}
	if err := selectOnly(ctx, tx, itemID, kind, id); err != nil {
		return err
	}
	return tx.Commit()
}

func selectOnly(ctx context.Context, tx *sql.Tx, itemID int64, kind string, artworkID int64) error {
	if _, err := tx.ExecContext(ctx, `UPDATE artwork SET selected = (id = ?) WHERE item_id = ? AND kind = ?`, artworkID, itemID, kind); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `UPDATE items SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = ?`, itemID)
	return err
}

// ResetArtwork sets an item's artwork of a kind back to automatic: chosen and uploaded images
// go (their paths are returned, for the uploads' files to be removed), and the item's own
// file's, else an agent's, is selected.
func (s *Store) ResetArtwork(ctx context.Context, itemID int64, kind string) ([]string, error) {
	if !choosableKinds[kind] {
		return nil, ErrArtworkKind
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM items WHERE id = ?`, itemID).Scan(&n); err != nil {
		return nil, err
	}
	if n == 0 {
		return nil, ErrNotFound
	}
	var uploads []string
	rows, err := tx.QueryContext(ctx, `SELECT local_path FROM artwork WHERE item_id = ? AND kind = ? AND source = 'upload' AND local_path IS NOT NULL`, itemID, kind)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var p string
		rows.Scan(&p)
		uploads = append(uploads, p)
	}
	rows.Close()
	if _, err := tx.ExecContext(ctx, `DELETE FROM artwork WHERE item_id = ? AND kind = ? AND source IN ('chosen', 'upload')`, itemID, kind); err != nil {
		return nil, err
	}
	var best sql.NullInt64
	tx.QueryRowContext(ctx, `SELECT id FROM artwork WHERE item_id = ? AND kind = ?
		ORDER BY CASE source WHEN 'local' THEN 0 WHEN 'embedded' THEN 1 WHEN 'plex' THEN 3 WHEN 'generated' THEN 9 WHEN 'frame' THEN 9 ELSE 2 END, id LIMIT 1`,
		itemID, kind).Scan(&best)
	if best.Valid {
		if err := selectOnly(ctx, tx, itemID, kind, best.Int64); err != nil {
			return nil, err
		}
	}
	return uploads, tx.Commit()
}
