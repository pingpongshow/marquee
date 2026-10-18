package scanner

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"marquee/internal/library"
)

// ErrNoReachableFolder is returned by RemoveMissing when none of the library's folders can
// be read: nothing can be told missing there, so nothing is removed.
var ErrNoReachableFolder = errors.New("none of the library's folders can be reached, so nothing was removed")

// timeLayout matches the timestamps SQLite writes (strftime('%Y-%m-%dT%H:%M:%fZ')), so
// they compare as strings.
const timeLayout = "2006-01-02T15:04:05.000Z"

func sqlTime(t time.Time) string { return t.UTC().Format(timeLayout) }

// removableRoots are the library folders whose missing files may be removed: the ones that
// can be read (a folder that can't be opened can't show what's gone).
func removableRoots(roots []string) []string {
	var out []string
	for _, r := range roots {
		if fi, err := os.Stat(r); err == nil && fi.IsDir() {
			out = append(out, r)
		}
	}
	return out
}

// UnreadableRoots are the library folders that can't be read right now: missing, not a
// folder, or not openable (an offline drive, permissions).
func UnreadableRoots(roots []string) []string {
	var out []string
	for _, r := range roots {
		f, err := os.Open(r)
		if err == nil {
			var fi os.FileInfo
			if fi, err = f.Stat(); err == nil && !fi.IsDir() {
				err = errors.New("not a folder")
			}
			if err == nil {
				_, err = f.Readdirnames(1)
				if errors.Is(err, io.EOF) { // empty, but readable
					err = nil
				}
			}
			f.Close()
		}
		if err != nil {
			out = append(out, r)
		}
	}
	return out
}

// underRoots is an SQL condition (with its arguments) matching col paths inside roots.
func underRoots(col string, roots []string) (string, []any) {
	var conds []string
	var args []any
	for _, r := range roots {
		prefix := strings.TrimSuffix(r, string(filepath.Separator)) + string(filepath.Separator)
		conds = append(conds, "substr("+col+", 1, ?) = ?")
		args = append(args, utf8.RuneCountInString(prefix), prefix)
	}
	if len(conds) == 0 {
		return "0", nil
	}
	return "(" + strings.Join(conds, " OR ") + ")", args
}

const leafTypes = `('movie','episode','track','video')`

// removeMissing deletes the library's unavailable files inside roots (missing since before
// cutoff, or all of them when cutoff is empty), tidies the library, and returns how many
// titles went with them. Watch state and the rest cascade with the items.
func removeMissing(ctx context.Context, tx *sql.Tx, libID int64, roots []string, cutoff string) (int, error) {
	if len(roots) == 0 {
		return 0, tidy(ctx, tx, libID)
	}
	var before, after int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM items WHERE library_id = ? AND type IN `+leafTypes, libID).Scan(&before); err != nil {
		return 0, err
	}
	cond, args := underRoots("path", roots)
	q := `DELETE FROM media_files WHERE library_id = ? AND available = 0 AND ` + cond
	args = append([]any{libID}, args...)
	if cutoff != "" {
		q += ` AND missing_since IS NOT NULL AND missing_since < ?`
		args = append(args, cutoff)
	}
	if _, err := tx.ExecContext(ctx, q, args...); err != nil {
		return 0, err
	}
	if err := tidy(ctx, tx, libID); err != nil {
		return 0, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM items WHERE library_id = ? AND type IN `+leafTypes, libID).Scan(&after); err != nil {
		return 0, err
	}
	return before - after, nil
}

// MissingCount is how many of the library's titles RemoveMissing would remove now: those
// without an available file whose files are all in folders that can be reached.
func MissingCount(ctx context.Context, db *sql.DB, lib library.Library) (int, error) {
	roots := removableRoots(lib.Paths)
	if len(roots) == 0 {
		return 0, nil
	}
	cond, args := underRoots("f.path", roots)
	var n int
	err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM items i WHERE i.library_id = ? AND i.type IN `+leafTypes+` AND i.available = 0
		AND EXISTS (SELECT 1 FROM media_versions v JOIN media_files f ON f.version_id = v.id WHERE v.item_id = i.id)
		AND NOT EXISTS (SELECT 1 FROM media_versions v JOIN media_files f ON f.version_id = v.id
			WHERE v.item_id = i.id AND NOT `+cond+`)`, append([]any{lib.ID}, args...)...).Scan(&n)
	return n, err
}

// RemoveMissing removes the library's unavailable files now, in folders that can be
// reached, then tidies the library (titles left without files go, with their watch
// history). It returns how many titles were removed.
func RemoveMissing(ctx context.Context, db *sql.DB, lib library.Library) (int, error) {
	roots := removableRoots(lib.Paths)
	if len(roots) == 0 {
		return 0, ErrNoReachableFolder
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	n, err := removeMissing(ctx, tx, lib.ID, roots, "")
	if err != nil {
		return 0, err
	}
	return n, tx.Commit()
}
