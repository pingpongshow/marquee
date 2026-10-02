package items

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"

	"marquee/internal/scanner/naming"
)

// Editable fields: API name → column.
var editable = map[string]string{
	"title": "title", "sortTitle": "sort_title", "originalTitle": "original_title", "year": "year",
	"originallyAvailableAt": "originally_available_at", "summary": "summary", "tagline": "tagline",
	"contentRating": "content_rating", "studio": "studio",
}

// LockedAPIFields maps stored (column) locks back to API names.
func LockedAPIFields(columns []string) []string {
	rev := map[string]string{}
	for api, col := range editable {
		rev[col] = api
	}
	out := []string{}
	for _, c := range columns {
		if a, ok := rev[c]; ok {
			out = append(out, a)
		}
	}
	sort.Strings(out)
	return out
}

// Edit sets fields (API names) and locks them, then unlocks the listed fields.
// Changing the title also updates and locks the sort title unless one is given.
func (s *Store) Edit(ctx context.Context, id int64, set map[string]any, unlock []string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var lockedJSON string
	err = tx.QueryRowContext(ctx, `SELECT locked_fields FROM items WHERE id = ?`, id).Scan(&lockedJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	var locked []string
	json.Unmarshal([]byte(lockedJSON), &locked)
	lockSet := map[string]bool{}
	for _, l := range locked {
		lockSet[l] = true
	}
	if t, ok := set["title"].(string); ok {
		if _, given := set["sortTitle"]; !given {
			set["sortTitle"] = naming.SortTitle(t)
		}
	}
	for field, v := range set {
		col, ok := editable[field]
		if !ok {
			continue
		}
		if s, isStr := v.(string); isStr && s == "" && col != "title" {
			v = nil // clearing a field
		}
		if _, err := tx.ExecContext(ctx, `UPDATE items SET `+col+` = ? WHERE id = ?`, v, id); err != nil {
			return err
		}
		lockSet[col] = true
	}
	for _, field := range unlock {
		delete(lockSet, editable[field])
	}
	locked = locked[:0]
	for c := range lockSet {
		locked = append(locked, c)
	}
	sort.Strings(locked)
	raw, _ := json.Marshal(locked)
	if _, err := tx.ExecContext(ctx, `UPDATE items SET locked_fields = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = ?`, string(raw), id); err != nil {
		return err
	}
	return tx.Commit()
}

// IsNotFound reports whether err means the item doesn't exist (from this package or a
// raw sql.ErrNoRows from another package's lookup).
func IsNotFound(err error) bool { return errors.Is(err, ErrNotFound) || errors.Is(err, sql.ErrNoRows) }
