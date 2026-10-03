// Package mediatrash deletes media files from Library Health (ADM-11). A deleted file,
// with the subtitles and artwork beside it that belong only to it, moves into a
// .marquee-trash folder in its library folder (<root>/.marquee-trash/<YYYY-MM-DD>/<path
// relative to the root>) and is removed for good after Retention by the "Empty media
// trash" task. Moves are renames, so the trash stays on the same filesystem.
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

// Retention is how long deleted files wait in the trash.
const Retention = 30 * 24 * time.Hour

const dateLayout = "2006-01-02"

var ErrNotFound = errors.New("file not found")

// MoveError says why a file couldn't be moved to the trash.
type MoveError struct{ Msg string }

func (e *MoveError) Error() string { return e.Msg }

// Service deletes files to the trash and empties it.
type Service struct {
	DB  *sql.DB
	Now func() time.Time
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
	var path string
	var libID int64
	err := s.DB.QueryRowContext(ctx, `SELECT path, library_id FROM media_files WHERE id = ?`, fileID).Scan(&path, &libID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	root, err := s.rootFor(ctx, libID, path)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &MoveError{"The file isn't on disk any more, so there's nothing to move to the trash"}
		}
		return moveError(err)
	}
	others, err := s.mediaInDir(ctx, path, fileID)
	if err != nil {
		return err
	}
	files := append([]string{path}, ownSidecars(path, others)...)

	day := filepath.Join(root, library.TrashDir, s.now().Format(dateLayout))
	var moved [][2]string // from, to
	undo := func() {
		for i := len(moved) - 1; i >= 0; i-- {
			os.Rename(moved[i][1], moved[i][0])
		}
	}
	for _, from := range files {
		rel, err := filepath.Rel(root, from)
		if err != nil || strings.HasPrefix(rel, "..") {
			undo()
			return &MoveError{"The file isn't inside its library folder"}
		}
		to := free(filepath.Join(day, rel))
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			undo()
			return moveError(err)
		}
		if err := os.Rename(from, to); err != nil {
			if from != path && errors.Is(err, os.ErrNotExist) {
				continue // a sidecar went in the meantime
			}
			undo()
			return moveError(err)
		}
		moved = append(moved, [2]string{from, to})
	}

	if err := s.forget(ctx, fileID, libID); err != nil {
		undo()
		return err
	}
	slog.InfoContext(ctx, "media file deleted to the trash", "by", by, "file", path, "trash", moved[0][1], "sidecars", len(moved)-1)
	return nil
}

// forget removes the file's row (streams and the rest cascade) and tidies the library the
// way a scan does, so an item without files goes, and so do containers left empty.
func (s *Service) forget(ctx context.Context, fileID, libID int64) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM media_files WHERE id = ?`, fileID); err != nil {
		return err
	}
	if err := scanner.Tidy(ctx, tx, libID); err != nil {
		return err
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

// Empty removes trash older than Retention, and empty date folders, in every library
// folder. It is the "Empty media trash" task.
func (s *Service) Empty(ctx context.Context) (string, error) {
	libs, err := library.NewStore(s.DB).List(ctx)
	if err != nil {
		return "", err
	}
	cutoff := s.now().Add(-Retention)
	removed, failed := 0, 0
	seen := map[string]bool{}
	for _, l := range libs {
		for _, root := range l.Paths {
			trash := filepath.Join(filepath.Clean(root), library.TrashDir)
			if seen[trash] {
				continue
			}
			seen[trash] = true
			entries, err := os.ReadDir(trash)
			if err != nil {
				continue // no trash here
			}
			for _, e := range entries {
				if ctx.Err() != nil {
					return "", ctx.Err()
				}
				p := filepath.Join(trash, e.Name())
				day, perr := time.ParseInLocation(dateLayout, e.Name(), s.now().Location())
				switch {
				case !e.IsDir() || perr != nil:
					continue // not ours
				case day.AddDate(0, 0, 1).Before(cutoff) || day.AddDate(0, 0, 1).Equal(cutoff):
					n := countFiles(p)
					if err := os.RemoveAll(p); err != nil {
						slog.WarnContext(ctx, "empty media trash", "folder", p, "err", err)
						failed++
						continue
					}
					removed += n
				case countFiles(p) == 0:
					os.RemoveAll(p) // only empty folders left
				}
			}
			if left, err := os.ReadDir(trash); err == nil && len(left) == 0 {
				os.Remove(trash)
			}
		}
	}
	if failed > 0 {
		return "", fmt.Errorf("removed %d files; %d trash folders couldn't be removed", removed, failed)
	}
	if removed == 0 {
		return "Nothing old enough to remove", nil
	}
	return fmt.Sprintf("Removed %d files deleted over 30 days ago", removed), nil
}

func countFiles(dir string) int {
	n := 0
	filepath.WalkDir(dir, func(_ string, d os.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			n++
		}
		return nil
	})
	return n
}
