package scanner

import (
	"context"
	"path/filepath"
	"strings"
)

// linkExtras attaches extras without a parent to the movie or show in the folder they
// belong to (the folder holding them, or its parent when they sit in an Extras folder).
func (s *Scanner) linkExtras(ctx context.Context, libID int64) error {
	rows, err := s.DB.QueryContext(ctx, `SELECT i.id, f.path FROM items i
		JOIN media_versions v ON v.item_id = i.id JOIN media_files f ON f.version_id = v.id
		WHERE i.library_id = ? AND i.extra_type IS NOT NULL AND i.parent_id IS NULL GROUP BY i.id`, libID)
	if err != nil {
		return err
	}
	type extra struct {
		id   int64
		path string
	}
	var list []extra
	for rows.Next() {
		var e extra
		if rows.Scan(&e.id, &e.path) == nil {
			list = append(list, e)
		}
	}
	rows.Close()
	for _, e := range list {
		dir := filepath.Dir(e.path)
		if extrasDirs[strings.ToLower(filepath.Base(dir))] != "" {
			dir = filepath.Dir(dir)
		}
		like := strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(dir) + "/%"
		var owner int64
		err := s.DB.QueryRowContext(ctx, `SELECT CASE WHEN i.type = 'episode' THEN i.grandparent_id ELSE i.id END
			FROM media_files f JOIN media_versions v ON v.id = f.version_id JOIN items i ON i.id = v.item_id
			WHERE f.library_id = ? AND i.extra_type IS NULL AND i.type IN ('movie', 'episode') AND f.path LIKE ? ESCAPE '\'
			ORDER BY length(f.path) LIMIT 1`, libID, like).Scan(&owner)
		if err != nil || owner == 0 {
			continue // the main title isn't scanned yet; try again next scan
		}
		if _, err := s.DB.ExecContext(ctx, `UPDATE items SET parent_id = ? WHERE id = ?`, owner, e.id); err != nil {
			return err
		}
	}
	return nil
}
