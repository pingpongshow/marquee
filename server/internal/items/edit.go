package items

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"marquee/internal/scanner/naming"
)

// Editable fields: API name → column.
var editable = map[string]string{
	"title": "title", "sortTitle": "sort_title", "originalTitle": "original_title", "year": "year",
	"originallyAvailableAt": "originally_available_at", "summary": "summary", "tagline": "tagline",
	"contentRating": "content_rating", "studio": "studio", "index": "idx", "artistCredit": "artist_credit",
}

// TagLocks are the tag lists an administrator can edit (META-13): API name → tag kind. Each is
// locked by its API name, so metadata agents leave an edited list alone.
var TagLocks = map[string]string{"genres": "genre", "styles": "style", "moods": "mood", "countries": "country"}

// TagLock is the lock name of a tag kind ("" for kinds that can't be edited).
func TagLock(kind string) string {
	for lock, k := range TagLocks {
		if k == kind {
			return lock
		}
	}
	return ""
}

// LockedAPIFields maps stored (column and tag-list) locks back to API names.
func LockedAPIFields(columns []string) []string {
	rev := map[string]string{}
	for api, col := range editable {
		rev[col] = api
	}
	for lock := range TagLocks {
		rev[lock] = lock
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

// Edit sets fields (API names) and tag lists (TagLocks names) and locks them, then unlocks the
// listed fields. Changing the title also updates and locks the sort title unless one is given.
func (s *Store) Edit(ctx context.Context, id int64, set map[string]any, tags map[string][]string, unlock []string) error {
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
	for lock, names := range tags {
		kind, ok := TagLocks[lock]
		if !ok {
			continue
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM item_tags WHERE item_id = ? AND tag_id IN (SELECT id FROM tags WHERE kind = ?)`, id, kind); err != nil {
			return err
		}
		seen := map[string]bool{}
		for _, n := range names {
			n = strings.TrimSpace(n)
			if n == "" || seen[strings.ToLower(n)] {
				continue
			}
			seen[strings.ToLower(n)] = true
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO tags(kind, name) VALUES (?, ?)`, kind, n); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO item_tags(item_id, tag_id) SELECT ?, id FROM tags WHERE kind = ? AND name = ?`, id, kind, n); err != nil {
				return err
			}
		}
		lockSet[lock] = true
	}
	for _, field := range unlock {
		if _, isTag := TagLocks[field]; isTag {
			delete(lockSet, field)
			continue
		}
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
