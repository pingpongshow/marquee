package mediatrash

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"marquee/internal/db"
	"marquee/internal/library"
)

type env struct {
	t    *testing.T
	db   *sql.DB
	root string
	lib  int64
	svc  *Service
}

var today = time.Date(2026, 10, 2, 12, 0, 0, 0, time.Local)

func newEnv(t *testing.T, typ library.Type) *env {
	dir := t.TempDir()
	d, err := db.Open(context.Background(), filepath.Join(dir, "t.db"), filepath.Join(dir, "b"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	root := filepath.Join(dir, "media")
	os.MkdirAll(root, 0o755)
	l, err := library.NewStore(d).Create(context.Background(), "Lib", typ, []string{root}, library.Options{})
	if err != nil {
		t.Fatal(err)
	}
	return &env{t: t, db: d, root: root, lib: l.ID, svc: &Service{DB: d, Now: func() time.Time { return today }}}
}

func (e *env) exec(q string, args ...any) int64 {
	e.t.Helper()
	r, err := e.db.Exec(q, args...)
	if err != nil {
		e.t.Fatalf("%s: %v", q, err)
	}
	id, _ := r.LastInsertId()
	return id
}

func (e *env) touch(rel string) string {
	e.t.Helper()
	p := filepath.Join(e.root, rel)
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(rel), 0o644); err != nil {
		e.t.Fatal(err)
	}
	return p
}

func (e *env) item(typ, title string, parent, grandparent any) int64 {
	return e.exec(`INSERT INTO items(library_id, type, title, sort_title, parent_id, grandparent_id) VALUES (?, ?, ?, ?, ?, ?)`,
		e.lib, typ, title, title, parent, grandparent)
}

// file adds a version with one file on disk at rel, with a video stream.
func (e *env) file(item int64, rel string) int64 {
	p := e.touch(rel)
	v := e.exec(`INSERT INTO media_versions(item_id) VALUES (?)`, item)
	f := e.exec(`INSERT INTO media_files(version_id, library_id, path, size, mtime) VALUES (?, ?, ?, 1, 1)`, v, e.lib, p)
	e.exec(`INSERT INTO streams(file_id, kind, codec) VALUES (?, 'video', 'h264')`, f)
	return f
}

func (e *env) count(q string, args ...any) int {
	e.t.Helper()
	var n int
	if err := e.db.QueryRow(q, args...).Scan(&n); err != nil {
		e.t.Fatal(err)
	}
	return n
}

func exists(p string) bool { _, err := os.Lstat(p); return err == nil }

func TestDeleteMovesFileAndOwnSidecarsToTrash(t *testing.T) {
	e := newEnv(t, library.Movies)
	movie := e.item("movie", "Heat", nil, nil)
	keep := e.file(movie, "Heat (1995)/Heat (1995) 4K.mkv")
	gone := e.file(movie, "Heat (1995)/Heat (1995).mkv")
	for _, s := range []string{"Heat (1995).en.srt", "Heat (1995).nfo", "Heat (1995)-poster.jpg", "Heat (1995) 4K.en.srt", "poster.jpg"} {
		e.touch("Heat (1995)/" + s)
	}

	if err := e.svc.Delete(context.Background(), gone, "admin"); err != nil {
		t.Fatal(err)
	}
	day := filepath.Join(e.root, library.TrashDir, "2026-10-02", "Heat (1995)")
	var trashed []string
	entries, _ := os.ReadDir(day)
	for _, en := range entries {
		trashed = append(trashed, en.Name())
	}
	sort.Strings(trashed)
	want := []string{"Heat (1995)-poster.jpg", "Heat (1995).en.srt", "Heat (1995).mkv", "Heat (1995).nfo"}
	if len(trashed) != len(want) {
		t.Fatalf("trash holds %v, want %v", trashed, want)
	}
	for i := range want {
		if trashed[i] != want[i] {
			t.Fatalf("trash holds %v, want %v", trashed, want)
		}
	}
	// The other version and its own subtitles, and folder artwork, stay.
	for _, s := range []string{"Heat (1995) 4K.mkv", "Heat (1995) 4K.en.srt", "poster.jpg"} {
		if !exists(filepath.Join(e.root, "Heat (1995)", s)) {
			t.Errorf("%s was moved", s)
		}
	}
	if n := e.count(`SELECT COUNT(*) FROM media_files WHERE id = ?`, gone); n != 0 {
		t.Error("file row kept")
	}
	if n := e.count(`SELECT COUNT(*) FROM streams WHERE file_id = ?`, gone); n != 0 {
		t.Error("streams kept")
	}
	if n := e.count(`SELECT COUNT(*) FROM media_files WHERE id = ?`, keep); n != 1 {
		t.Error("other file removed")
	}
	if n := e.count(`SELECT COUNT(*) FROM items WHERE id = ?`, movie); n != 1 {
		t.Error("movie removed though a file remains")
	}
}

func TestDeletingLastFileRemovesItemAndEmptyParents(t *testing.T) {
	e := newEnv(t, library.Shows)
	show := e.item("show", "Show", nil, nil)
	s1 := e.item("season", "Season 1", show, nil)
	s2 := e.item("season", "Season 2", show, nil)
	ep1 := e.item("episode", "One", s1, show)
	ep2 := e.item("episode", "Two", s2, show)
	f1 := e.file(ep1, "Show/Season 1/Show - S01E01.mkv")
	f2 := e.file(ep2, "Show/Season 2/Show - S02E01.mkv")
	ctx := context.Background()

	if err := e.svc.Delete(ctx, f1, "admin"); err != nil {
		t.Fatal(err)
	}
	if n := e.count(`SELECT COUNT(*) FROM items WHERE id IN (?, ?)`, ep1, s1); n != 0 {
		t.Errorf("episode and its empty season should be gone, %d left", n)
	}
	if n := e.count(`SELECT COUNT(*) FROM items WHERE id IN (?, ?, ?)`, show, s2, ep2); n != 3 {
		t.Errorf("the rest of the show should stay, %d left", n)
	}
	if err := e.svc.Delete(ctx, f2, "admin"); err != nil {
		t.Fatal(err)
	}
	if n := e.count(`SELECT COUNT(*) FROM items`); n != 0 {
		t.Errorf("an empty show should be gone, %d items left", n)
	}
	if err := e.svc.Delete(ctx, f2, "admin"); !errors.Is(err, ErrNotFound) {
		t.Errorf("deleting again: %v, want ErrNotFound", err)
	}
}

func TestDeleteFromReadOnlyFolderFails(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores permissions")
	}
	e := newEnv(t, library.Movies)
	movie := e.item("movie", "Heat", nil, nil)
	f := e.file(movie, "Heat/Heat.mkv")
	os.Chmod(filepath.Join(e.root, "Heat"), 0o555)
	os.Chmod(e.root, 0o555)
	t.Cleanup(func() { os.Chmod(e.root, 0o755); os.Chmod(filepath.Join(e.root, "Heat"), 0o755) })

	err := e.svc.Delete(context.Background(), f, "admin")
	var me *MoveError
	if !errors.As(err, &me) {
		t.Fatalf("got %v, want a MoveError", err)
	}
	if !exists(filepath.Join(e.root, "Heat", "Heat.mkv")) {
		t.Error("file moved")
	}
	if n := e.count(`SELECT COUNT(*) FROM media_files WHERE id = ?`, f); n != 1 {
		t.Error("file row removed though the move failed")
	}
}

func TestEmptyRemovesOnlyOldTrash(t *testing.T) {
	e := newEnv(t, library.Movies)
	trash := filepath.Join(e.root, library.TrashDir)
	e.touch(library.TrashDir + "/2026-08-01/Old/Old.mkv")       // 62 days ago
	e.touch(library.TrashDir + "/2026-09-01/Edge/Edge.mkv")     // a day over 30 days
	e.touch(library.TrashDir + "/2026-09-20/Recent/Recent.mkv") // 12 days ago
	e.touch(library.TrashDir + "/notes.txt")                    // not a date folder: left alone
	os.MkdirAll(filepath.Join(trash, "2026-09-25", "Empty"), 0o755)

	msg, err := e.svc.Empty(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if msg != "Removed 2 files deleted over 30 days ago" {
		t.Errorf("message %q", msg)
	}
	for dir, want := range map[string]bool{"2026-08-01": false, "2026-09-01": false, "2026-09-20": true, "2026-09-25": false, "notes.txt": true} {
		if got := exists(filepath.Join(trash, dir)); got != want {
			t.Errorf("%s exists = %v, want %v", dir, got, want)
		}
	}
}
