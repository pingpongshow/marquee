// Package mediatrash deletes media files from Library Health, and titles from their pages
// (ADM-11). A deleted file,
// with the subtitles and artwork beside it that belong only to it, moves into a
// .marquee-trash folder in its library folder (<root>/.marquee-trash/<YYYY-MM-DD>/<path
// relative to the root>) and is removed for good by the nightly "Empty media trash" task. Moves are renames, so the trash stays on the same filesystem.
package mediatrash

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"marquee/internal/library"
	"marquee/internal/scanner"
)

const dateLayout = "2006-01-02"

var (
	ErrNotFound = errors.New("file not found")
	ErrNoItem   = errors.New("item not found")
)

// MoveError says why a file couldn't be moved to the trash.
type MoveError struct{ Msg string }

func (e *MoveError) Error() string { return e.Msg }

// Service deletes files to the trash and empties it.
type Service struct {
	DB  *sql.DB
	Now func() time.Time
	// Playing, when set, reports whether a file is being played now (Empty skips it).
	Playing func(path string) bool
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

// Delete moves a media file (and its own sidecars) to the trash and removes it from the
// library. When it was its item's last file the item goes too, with a season or show (or
// album or artist) left empty. by names who asked, for the log.
func (s *Service) Delete(ctx context.Context, fileID int64, by string) error {
	m, err := s.move(ctx, fileID)
	if err != nil {
		return err
	}
	if err := s.forget(ctx, m); err != nil {
		m.undo()
		return err
	}
	slog.InfoContext(ctx, "media file deleted to the trash", "by", by, "file", m.path, "trash", m.moved[0][1], "sidecars", len(m.moved)-1)
	return nil
}

// DeleteItem moves every media file of an item and of everything under it (a show's or
// season's episodes, an artist's or album's tracks, a title's extras), each with its own
// sidecars, to the trash and removes them from the library, so the item goes too, with a
// season or show (or album or artist) left empty. Nothing moves unless everything can. It
// returns how many media files moved; by names who asked, for the log.
func (s *Service) DeleteItem(ctx context.Context, itemID int64, by string) (int, error) {
	var typ, title string
	err := s.DB.QueryRowContext(ctx, `SELECT type, title FROM items WHERE id = ?`, itemID).Scan(&typ, &title)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNoItem
	}
	if err != nil {
		return 0, err
	}
	if typ == "collection" {
		return 0, &MoveError{"A collection has no files of its own: delete the collection instead, and its titles stay in the library"}
	}
	ids, err := s.itemFiles(ctx, itemID)
	if err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, &MoveError{"It has no media files to delete"}
	}
	var done []moved
	undo := func() {
		for i := len(done) - 1; i >= 0; i-- {
			done[i].undo()
		}
	}
	for _, id := range ids {
		m, err := s.move(ctx, id)
		if err != nil {
			undo()
			return 0, err
		}
		done = append(done, m)
	}
	if err := s.forget(ctx, done...); err != nil {
		undo()
		return 0, err
	}
	for _, m := range done {
		slog.InfoContext(ctx, "media file deleted to the trash", "by", by, "item", title, "file", m.path, "trash", m.moved[0][1], "sidecars", len(m.moved)-1)
	}
	return len(done), nil
}

// itemFiles lists the media files of an item and of the items under it, in path order.
func (s *Service) itemFiles(ctx context.Context, itemID int64) ([]int64, error) {
	rows, err := s.DB.QueryContext(ctx, `
		WITH RECURSIVE under(id) AS (SELECT ? UNION SELECT i.id FROM items i JOIN under u ON i.parent_id = u.id)
		SELECT f.id FROM media_files f JOIN media_versions v ON v.id = f.version_id
		WHERE v.item_id IN (SELECT id FROM under) ORDER BY f.path`, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// Trash moves media files (each with its own sidecars) to the trash and leaves the
// library's records alone: a file replaced by a better copy keeps its item, whose record
// the caller points at the new file. undo puts every file back. It returns the files'
// paths as they were.
func (s *Service) Trash(ctx context.Context, fileIDs []int64, why string) (paths []string, undo func(), err error) {
	var done []moved
	undo = func() {
		for i := len(done) - 1; i >= 0; i-- {
			done[i].undo()
		}
	}
	for _, id := range fileIDs {
		m, err := s.move(ctx, id)
		if err != nil {
			undo()
			return nil, func() {}, err
		}
		done = append(done, m)
		paths = append(paths, m.path)
		slog.InfoContext(ctx, "media file moved to the trash", "why", why, "file", m.path, "trash", m.moved[0][1], "sidecars", len(m.moved)-1)
	}
	return paths, undo, nil
}

// Stash moves just a media file to the trash (its sidecars stay: they belong to the item)
// and leaves the library's record alone, for a caller that puts another file in its place.
// It returns where the file went, so it can be put back; undo puts it back now.
func (s *Service) Stash(ctx context.Context, fileID int64, why string) (to string, undo func(), err error) {
	var path string
	var libID int64
	err = s.DB.QueryRowContext(ctx, `SELECT path, library_id FROM media_files WHERE id = ?`, fileID).Scan(&path, &libID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", func() {}, ErrNotFound
	}
	if err != nil {
		return "", func() {}, err
	}
	root, err := s.rootFor(ctx, libID, path)
	if err != nil {
		return "", func() {}, err
	}
	rel, err := filepath.Rel(root, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", func() {}, &MoveError{"The file isn't inside its library folder"}
	}
	to = free(filepath.Join(root, library.TrashDir, s.now().Format(dateLayout), rel))
	if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
		return "", func() {}, moveError(err)
	}
	if err := os.Rename(path, to); err != nil {
		return "", func() {}, moveError(err)
	}
	slog.InfoContext(ctx, "media file moved to the trash", "why", why, "file", path, "trash", to)
	return to, func() { os.Rename(to, path) }, nil
}

// moved is a file (and its sidecars) moved to the trash.
type moved struct {
	id    int64
	path  string
	libID int64
	moved [][2]string // from, to
}

func (m moved) undo() {
	for i := len(m.moved) - 1; i >= 0; i-- {
		os.Rename(m.moved[i][1], m.moved[i][0])
	}
}

// move moves a media file and its own sidecars into its library folder's trash.
func (s *Service) move(ctx context.Context, fileID int64) (moved, error) {
	var path string
	var libID int64
	err := s.DB.QueryRowContext(ctx, `SELECT path, library_id FROM media_files WHERE id = ?`, fileID).Scan(&path, &libID)
	if errors.Is(err, sql.ErrNoRows) {
		return moved{}, ErrNotFound
	}
	if err != nil {
		return moved{}, err
	}
	m := moved{id: fileID, path: path, libID: libID}
	root, err := s.rootFor(ctx, libID, path)
	if err != nil {
		return m, err
	}
	if _, err := os.Lstat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return m, &MoveError{"The file isn't on disk any more, so there's nothing to move to the trash"}
		}
		return m, moveError(err)
	}
	others, err := s.mediaInDir(ctx, path, fileID)
	if err != nil {
		return m, err
	}
	files := append([]string{path}, ownSidecars(path, others)...)

	day := filepath.Join(root, library.TrashDir, s.now().Format(dateLayout))
	for _, from := range files {
		rel, err := filepath.Rel(root, from)
		if err != nil || strings.HasPrefix(rel, "..") {
			m.undo()
			return m, &MoveError{"The file isn't inside its library folder"}
		}
		to := free(filepath.Join(day, rel))
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			m.undo()
			return m, moveError(err)
		}
		if err := os.Rename(from, to); err != nil {
			if from != path && errors.Is(err, os.ErrNotExist) {
				continue // a sidecar went in the meantime
			}
			m.undo()
			return m, moveError(err)
		}
		m.moved = append(m.moved, [2]string{from, to})
	}
	return m, nil
}

// forget removes the moved files' rows (streams and the rest cascade) and tidies their
// libraries the way a scan does, so an item without files goes, and so do containers left
// empty. It's one transaction: all the rows go, or none.
func (s *Service) forget(ctx context.Context, files ...moved) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	libs := map[int64]bool{}
	for _, m := range files {
		if _, err := tx.ExecContext(ctx, `DELETE FROM media_files WHERE id = ?`, m.id); err != nil {
			return err
		}
		libs[m.libID] = true
	}
	for lib := range libs {
		if err := scanner.Tidy(ctx, tx, lib); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// rootFor is the library folder holding path (the deepest, if folders nest).
func (s *Service) rootFor(ctx context.Context, libID int64, path string) (string, error) {
	lib, err := library.NewStore(s.DB).Get(ctx, libID)
	if err != nil {
		return "", err
	}
	best := ""
	for _, r := range lib.Paths {
		r = filepath.Clean(r)
		if strings.HasPrefix(path, r+string(filepath.Separator)) && len(r) > len(best) {
			best = r
		}
	}
	if best == "" {
		return "", &MoveError{"The file isn't inside one of its library's folders"}
	}
	return best, nil
}

// mediaInDir lists the base names (without extension) of the other media in path's folder:
// files the library knows plus media files on disk.
func (s *Service) mediaInDir(ctx context.Context, path string, except int64) ([]string, error) {
	var out []string
	dir := filepath.Dir(path)
	prefix := dir + string(filepath.Separator)
	rows, err := s.DB.QueryContext(ctx, `SELECT path FROM media_files WHERE id <> ? AND substr(path, 1, ?) = ?`, except, len(prefix), prefix)
	if err != nil {
		return nil, err
	}
	known := map[string]bool{}
	for rows.Next() {
		var p string
		if rows.Scan(&p) == nil && filepath.Dir(p) == dir {
			known[filepath.Base(p)] = true
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if !e.IsDir() && e.Name() != filepath.Base(path) && scanner.IsMedia(e.Name()) {
			known[e.Name()] = true
		}
	}
	for name := range known {
		out = append(out, stem(name))
	}
	return out, nil
}

// ownSidecars finds the files next to path named after it ("Movie.en.srt", "Movie.nfo",
// "Movie-poster.jpg") that belong to no other media file in the folder. A sidecar belongs
// to the media file whose name is its longest prefix; a tie means it's shared and stays.
func ownSidecars(path string, others []string) []string {
	dir, base := filepath.Dir(path), filepath.Base(path)
	mine := stem(base)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || name == base || scanner.IsMedia(name) || !sidecarOf(name, mine) {
			continue
		}
		owned := true
		for _, o := range others {
			if sidecarOf(name, o) && len(o) >= len(mine) {
				owned = false
				break
			}
		}
		if owned {
			out = append(out, filepath.Join(dir, name))
		}
	}
	sort.Strings(out)
	return out
}

func sidecarOf(name, stem string) bool {
	if len(name) <= len(stem) || !strings.EqualFold(name[:len(stem)], stem) {
		return false
	}
	switch name[len(stem)] {
	case '.', '-', '_':
		return true
	}
	return false
}

func stem(name string) string { return strings.TrimSuffix(name, filepath.Ext(name)) }

// free returns p, or p with a counter before the extension when p is taken (the same file
// name deleted twice in a day).
func free(p string) string {
	if _, err := os.Lstat(p); errors.Is(err, os.ErrNotExist) {
		return p
	}
	ext := filepath.Ext(p)
	for i := 2; ; i++ {
		q := fmt.Sprintf("%s (%d)%s", strings.TrimSuffix(p, ext), i, ext)
		if _, err := os.Lstat(q); errors.Is(err, os.ErrNotExist) {
			return q
		}
	}
}

// moveError explains a failed move in terms an admin can act on.
func moveError(err error) error {
	switch {
	case errors.Is(err, syscall.EROFS):
		return &MoveError{"Marquee can't write to this folder; the media mount is read-only"}
	case errors.Is(err, syscall.EACCES), errors.Is(err, syscall.EPERM):
		return &MoveError{"Marquee can't write to this folder; check the media mount isn't read-only and the folder's permissions"}
	case errors.Is(err, syscall.EXDEV):
		return &MoveError{"Marquee can't move the file to the trash: the folder is on a different filesystem from its library folder"}
	}
	return &MoveError{"Marquee couldn't move the file to the trash: " + err.Error()}
}

// Empty permanently deletes everything in the media trash folders (.marquee-trash in each
// library folder), the "Empty media trash" task that runs every night. Nothing outside a
// trash folder is touched: symbolic links are removed, never followed, and a file being
// played is skipped. It reports what it freed ("Freed 112 GB (37 files)").
func (s *Service) Empty(ctx context.Context) (string, error) {
	libs, err := library.NewStore(s.DB).List(ctx)
	if err != nil {
		return "", err
	}
	var freed int64
	removed, failed, skipped := 0, 0, 0
	seen := map[string]bool{}
	for _, l := range libs {
		for _, root := range l.Paths {
			trash := filepath.Join(filepath.Clean(root), library.TrashDir)
			if seen[trash] {
				continue
			}
			seen[trash] = true
			if fi, err := os.Lstat(trash); err != nil || !fi.IsDir() {
				continue // no trash here (or something that isn't ours)
			}
			var dirs []string
			err := filepath.WalkDir(trash, func(p string, d os.DirEntry, err error) error {
				if err != nil {
					return nil
				}
				if ctx.Err() != nil {
					return ctx.Err()
				}
				if d.IsDir() {
					if p != trash {
						dirs = append(dirs, p)
					}
					return nil
				}
				if !inside(trash, p) {
					return nil
				}
				if s.Playing != nil && s.Playing(p) {
					skipped++
					return nil
				}
				var size int64
				if fi, err := d.Info(); err == nil && fi.Mode().IsRegular() {
					size = fi.Size()
				}
				if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
					slog.WarnContext(ctx, "empty media trash", "file", p, "err", err)
					failed++
					return nil
				}
				removed++
				freed += size
				return nil
			})
			if err != nil {
				return "", err
			}
			// Folders left empty go, deepest first, then the trash folder itself.
			for i := len(dirs) - 1; i >= 0; i-- {
				os.Remove(dirs[i]) // only when empty
			}
			os.Remove(trash)
		}
	}
	msg := fmt.Sprintf("Freed %s (%d file%s)", humanBytes(freed), removed, plural(removed))
	if removed == 0 {
		msg = "The media trash was empty"
	}
	if skipped > 0 {
		msg += fmt.Sprintf("; %d file%s being played kept for next time", skipped, plural(skipped))
	}
	slog.InfoContext(ctx, "media trash emptied", "files", removed, "bytes", freed, "failed", failed, "skipped", skipped)
	if failed > 0 {
		return "", fmt.Errorf("%s; %d file%s couldn't be deleted", msg, failed, plural(failed))
	}
	return msg, nil
}

// inside reports whether p is within dir (and not dir itself).
func inside(dir, p string) bool {
	rel, err := filepath.Rel(dir, p)
	return err == nil && rel != "." && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel)
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// humanBytes writes a size in decimal units, as disks are sold: 112 GB, 3.4 MB.
func humanBytes(b int64) string {
	const k = 1000
	switch {
	case b >= k*k*k*k:
		return fmt.Sprintf("%.1f TB", float64(b)/(k*k*k*k))
	case b >= 100*k*k*k:
		return fmt.Sprintf("%.0f GB", float64(b)/(k*k*k))
	case b >= k*k*k:
		return fmt.Sprintf("%.1f GB", float64(b)/(k*k*k))
	case b >= k*k:
		return fmt.Sprintf("%.1f MB", float64(b)/(k*k))
	case b >= k:
		return fmt.Sprintf("%.0f KB", float64(b)/k)
	}
	return fmt.Sprintf("%d bytes", b)
}
