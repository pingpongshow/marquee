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

// Emptying deletes everything in the trash folders and nothing else: not the library's
// files, not a link's target, not a file being played; it reports the space freed.
func TestEmptyDeletesAllTrashAndNothingElse(t *testing.T) {
	e := newEnv(t, library.Movies)
	trash := filepath.Join(e.root, library.TrashDir)
	big := e.touch(library.TrashDir + "/2026-10-01/Old/Old.mkv")
	os.WriteFile(big, make([]byte, 1_500_000), 0o644)
	e.touch(library.TrashDir + "/2026-10-02/New/New.mkv") // today's: goes too
	e.touch(library.TrashDir + "/notes.txt")
	playing := e.touch(library.TrashDir + "/2026-10-02/Playing.mkv")
	keep := e.touch("Movie/Movie.mkv") // in the library, outside the trash
	outside := filepath.Join(t.TempDir(), "elsewhere.mkv")
	os.WriteFile(outside, []byte("x"), 0o644)
	if err := os.Symlink(outside, filepath.Join(trash, "link.mkv")); err != nil {
		t.Fatal(err)
	}
	e.svc.Playing = func(p string) bool { return p == playing }

	msg, err := e.svc.Empty(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if want := "Freed 1.5 MB (4 files); 1 file being played kept for next time"; msg != want {
		t.Errorf("message %q, want %q", msg, want)
	}
	for p, want := range map[string]bool{big: false, filepath.Join(trash, "2026-10-01"): false, filepath.Join(trash, "notes.txt"): false,
		filepath.Join(trash, "link.mkv"): false, playing: true, keep: true, outside: true} {
		if got := exists(p); got != want {
			t.Errorf("%s exists = %v, want %v", p, got, want)
		}
	}
	// Next time (nobody watching) the rest goes, the trash folder with it.
	e.svc.Playing = nil
	if msg, _ := e.svc.Empty(context.Background()); msg != "Freed 37 bytes (1 file)" {
		t.Errorf("second run: %q", msg)
	}
	if exists(trash) || !exists(keep) {
		t.Errorf("trash folder left (%v) or library file gone (%v)", exists(trash), !exists(keep))
	}
	if msg, _ := e.svc.Empty(context.Background()); msg != "The media trash was empty" {
		t.Errorf("empty trash: %q", msg)
	}
}

// Deleting a title moves the files of everything under it, each with its own sidecars, and
// takes the title and its empty containers out of the library; the rest stays.
func TestDeleteItemMovesEverythingUnderIt(t *testing.T) {
	e := newEnv(t, library.Shows)
	show := e.item("show", "Show", nil, nil)
	s1 := e.item("season", "Season 1", show, nil)
	s2 := e.item("season", "Season 2", show, nil)
	ep1 := e.item("episode", "One", s1, show)
	ep2 := e.item("episode", "Two", s1, show)
	ep3 := e.item("episode", "Three", s2, show)
	e.file(ep1, "Show/Season 1/Show - S01E01.mkv")
	e.touch("Show/Season 1/Show - S01E01.en.srt")
	e.file(ep2, "Show/Season 1/Show - S01E02.mkv")
	e.file(ep3, "Show/Season 2/Show - S02E01.mkv")
	other := e.item("show", "Other", nil, nil)
	oe := e.item("episode", "Pilot", e.item("season", "Season 1", other, nil), other)
	keep := e.file(oe, "Other/Season 1/Other - S01E01.mkv")
	ctx := context.Background()

	n, err := e.svc.DeleteItem(ctx, s1, "admin")
	if err != nil || n != 2 {
		t.Fatalf("season: %d %v", n, err)
	}
	day := filepath.Join(e.root, library.TrashDir, "2026-10-02", "Show", "Season 1")
	for _, name := range []string{"Show - S01E01.mkv", "Show - S01E01.en.srt", "Show - S01E02.mkv"} {
		if !exists(filepath.Join(day, name)) {
			t.Errorf("%s not in the trash", name)
		}
	}
	if c := e.count(`SELECT COUNT(*) FROM items WHERE id IN (?, ?, ?)`, s1, ep1, ep2); c != 0 {
		t.Errorf("the season and its episodes should be gone, %d left", c)
	}
	if c := e.count(`SELECT COUNT(*) FROM items WHERE id IN (?, ?, ?)`, show, s2, ep3); c != 3 {
		t.Errorf("the rest of the show should stay, %d left", c)
	}

	if n, err := e.svc.DeleteItem(ctx, show, "admin"); err != nil || n != 1 {
		t.Fatalf("show: %d %v", n, err)
	}
	if c := e.count(`SELECT COUNT(*) FROM items WHERE id = ? OR parent_id = ? OR grandparent_id = ?`, show, show, show); c != 0 {
		t.Errorf("the show should be gone, %d items left", c)
	}
	if c := e.count(`SELECT COUNT(*) FROM media_files WHERE id = ?`, keep); c != 1 || !exists(filepath.Join(e.root, "Other/Season 1/Other - S01E01.mkv")) {
		t.Error("another show was touched")
	}
	if _, err := e.svc.DeleteItem(ctx, show, "admin"); !errors.Is(err, ErrNoItem) {
		t.Errorf("deleting again: %v, want ErrNoItem", err)
	}
}

// Nothing moves unless everything can: a file missing from disk stops the delete and every
// other file stays where it was, still in the library.
func TestDeleteItemIsAllOrNothing(t *testing.T) {
	e := newEnv(t, library.Movies)
	movie := e.item("movie", "Heat", nil, nil)
	a := e.file(movie, "Heat (1995)/Heat (1995) 1080p.mkv")
	b := e.file(movie, "Heat (1995)/Heat (1995) 4K.mkv")
	os.Remove(filepath.Join(e.root, "Heat (1995)/Heat (1995) 4K.mkv"))

	_, err := e.svc.DeleteItem(context.Background(), movie, "admin")
	var me *MoveError
	if !errors.As(err, &me) {
		t.Fatalf("got %v, want a MoveError", err)
	}
	if !exists(filepath.Join(e.root, "Heat (1995)/Heat (1995) 1080p.mkv")) {
		t.Error("the file that could move was left in the trash")
	}
	if c := e.count(`SELECT COUNT(*) FROM media_files WHERE id IN (?, ?)`, a, b); c != 2 {
		t.Errorf("file rows removed though the delete failed: %d left", c)
	}
}

// A collection has no files of its own (its titles are its members, not its children), and
// an item without files has nothing to delete.
func TestDeleteItemRefusesCollectionsAndEmptyItems(t *testing.T) {
	e := newEnv(t, library.Movies)
	movie := e.item("movie", "Heat", nil, nil)
	e.file(movie, "Heat/Heat.mkv")
	coll := e.item("collection", "Crime", nil, nil)
	e.exec(`INSERT INTO collection_items(collection_id, item_id) VALUES (?, ?)`, coll, movie)
	ctx := context.Background()
	var me *MoveError
	if _, err := e.svc.DeleteItem(ctx, coll, "admin"); !errors.As(err, &me) {
		t.Errorf("collection: %v, want a MoveError", err)
	}
	if !exists(filepath.Join(e.root, "Heat/Heat.mkv")) {
		t.Error("a collection's member was deleted")
	}
	empty := e.item("movie", "Nothing", nil, nil)
	if _, err := e.svc.DeleteItem(ctx, empty, "admin"); !errors.As(err, &me) {
		t.Errorf("no files: %v, want a MoveError", err)
	}
	if _, err := e.svc.DeleteItem(ctx, 9999, "admin"); !errors.Is(err, ErrNoItem) {
		t.Errorf("unknown: %v, want ErrNoItem", err)
	}
}
