package scanner

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"marquee/internal/library"
)

// A scan that finds files gone removes them at once, with the titles left without files,
// their watch state and the show left empty; the rest of the library stays.
func TestScanRemovesMissingAtOnce(t *testing.T) {
	e := newEnv(t)
	e.touch("tv/Kept/Kept - S01E01.mkv")
	e.touch("tv/Gone/Gone - S01E01.mkv")
	e.touch("tv/Gone/Gone - S01E02.mkv")
	l := e.lib("TV", library.Shows, "tv")
	e.scan(l)
	var epID int64
	if err := e.db.QueryRow(`SELECT id FROM items WHERE type = 'episode' AND grandparent_id = (SELECT id FROM items WHERE title = 'Gone') LIMIT 1`).Scan(&epID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Exec(`INSERT INTO users(id, username, display_name) VALUES (1, 'u', 'u')`); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Exec(`INSERT INTO user_item_state(user_id, item_id, play_count) VALUES (1, ?, 1)`, epID); err != nil {
		t.Fatal(err)
	}

	// The Gone folder moves to another library: the next scan removes it.
	os.RemoveAll(filepath.Join(e.root, "tv/Gone"))
	st := e.scan(l)
	if st.Removed != 2 {
		t.Fatalf("removed: %s", st)
	}
	if n := e.count(`SELECT COUNT(*) FROM items WHERE title = 'Gone'`); n != 0 {
		t.Errorf("show left behind: %d", n)
	}
	if n := e.count(`SELECT COUNT(*) FROM items i JOIN items s ON s.id = i.grandparent_id WHERE s.title = 'Gone'`); n != 0 {
		t.Errorf("episodes left behind: %d", n)
	}
	if n := e.count(`SELECT COUNT(*) FROM user_item_state WHERE item_id = ?`, epID); n != 0 {
		t.Error("watch state left behind")
	}
	if n := e.count(`SELECT COUNT(*) FROM media_files`); n != 1 {
		t.Errorf("files = %d, want the kept one", n)
	}
}

// A library folder that can't be read (a drive that's gone) isn't scanned, so nothing in it
// counts as missing; a folder that can be read but is empty has lost its files.
func TestScanOfflineFolder(t *testing.T) {
	e := newEnv(t)
	e.touch("a/Heat (1995)/Heat (1995).mkv")
	e.touch("b/Alien (1979)/Alien (1979).mkv")
	e.touch("c/Ran (1985)/Ran (1985).mkv")
	a, b, cc := filepath.Join(e.root, "a"), filepath.Join(e.root, "b"), filepath.Join(e.root, "c")
	l, err := library.NewStore(e.db).Create(context.Background(), "Movies", library.Movies, []string{a, b, cc}, library.Options{})
	if err != nil {
		t.Fatal(err)
	}
	e.scan(l)
	os.Rename(b, b+"-offline")                    // drive gone
	os.RemoveAll(filepath.Join(cc, "Ran (1985)")) // folder emptied
	st := e.scan(l)
	if st.Removed != 1 {
		t.Fatalf("removed: %s", st)
	}
	if len(st.Unreadable) != 1 || st.Unreadable[0] != b {
		t.Errorf("unreadable folders reported: %v", st.Unreadable)
	}
	if u := UnreadableRoots([]string{a, b, cc}); len(u) != 1 || u[0] != b {
		t.Errorf("UnreadableRoots: %v", u)
	}
	if n := e.count(`SELECT COUNT(*) FROM items WHERE type = 'movie'`); n != 2 {
		t.Fatalf("movies = %d, want Heat and Alien", n)
	}
	if n := e.count(`SELECT available FROM media_files WHERE path LIKE ?`, b+"%"); n != 1 {
		t.Error("offline drive's file marked missing")
	}
}

// Remove missing titles now (for files an older version left greyed out): counts and removes
// the gone titles, refuses when no folder can be reached.
func TestRemoveMissingNow(t *testing.T) {
	e := newEnv(t)
	e.touch("movies/Heat (1995)/Heat (1995).mkv")
	e.touch("movies/Alien (1979)/Alien (1979).mkv")
	e.touch("movies/Ran (1985)/Ran (1985).mkv")
	e.touch("movies/Ran (1985)/Ran (1985) {edition-Director's Cut}.mkv")
	l := e.lib("Movies", library.Movies, "movies")
	e.scan(l)
	// Files an older version left greyed out (unavailable) instead of removing.
	os.RemoveAll(filepath.Join(e.root, "movies/Alien (1979)"))
	os.Remove(filepath.Join(e.root, "movies/Ran (1985)/Ran (1985) {edition-Director's Cut}.mkv")) // one of two versions
	if _, err := e.db.Exec(`UPDATE media_files SET available = 0 WHERE path LIKE '%Alien%' OR path LIKE '%Director%'`); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Exec(`UPDATE items SET available = 0 WHERE title LIKE 'Alien%'`); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if n, err := MissingCount(ctx, e.db, l); err != nil || n != 1 {
		t.Fatalf("missing count = %d, %v", n, err)
	}
	n, err := RemoveMissing(ctx, e.db, l)
	if err != nil || n != 1 {
		t.Fatalf("removed = %d, %v", n, err)
	}
	if n := e.count(`SELECT COUNT(*) FROM items WHERE type = 'movie'`); n != 2 {
		t.Errorf("movies = %d", n)
	}
	if n := e.count(`SELECT COUNT(*) FROM media_files`); n != 2 {
		t.Errorf("files = %d (the missing version should go too)", n)
	}
	if n, _ := MissingCount(ctx, e.db, l); n != 0 {
		t.Errorf("missing count after = %d", n)
	}
	os.Rename(filepath.Join(e.root, "movies"), filepath.Join(e.root, "movies-offline"))
	if _, err := RemoveMissing(ctx, e.db, l); !errors.Is(err, ErrNoReachableFolder) {
		t.Errorf("offline library: %v", err)
	}
}
