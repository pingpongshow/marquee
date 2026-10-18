package scanner

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"marquee/internal/library"
)

// clock is a settable scan clock.
type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func (e *env) missingSince(rel string) sql.NullString {
	var s sql.NullString
	if err := e.db.QueryRow(`SELECT missing_since FROM media_files WHERE path = ?`, filepath.Join(e.root, rel)).Scan(&s); err != nil {
		e.t.Fatal(err)
	}
	return s
}

// A file found gone notes when; finding it again clears it.
func TestMissingSince(t *testing.T) {
	e := newEnv(t)
	c := &clock{time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)}
	e.sc.Now = c.now
	e.touch("tv/Show/Show - S01E01.mkv")
	e.touch("tv/Show/Show - S01E02.mkv")
	l := e.lib("TV", library.Shows, "tv")
	e.scan(l)
	const ep = "tv/Show/Show - S01E02.mkv"
	if s := e.missingSince(ep); s.Valid {
		t.Fatalf("missing_since set on a file that's there: %v", s.String)
	}
	os.Remove(filepath.Join(e.root, ep))
	e.scan(l)
	if s := e.missingSince(ep); s.String != "2026-10-01T12:00:00.000Z" {
		t.Fatalf("missing_since = %q", s.String)
	}
	// Still missing on a later scan: the first time is kept.
	c.t = c.t.Add(48 * time.Hour)
	e.scan(l)
	if s := e.missingSince(ep); s.String != "2026-10-01T12:00:00.000Z" {
		t.Fatalf("missing_since moved to %q", s.String)
	}
	e.touch(ep)
	if st := e.scan(l); st.Updated+st.Restored == 0 {
		t.Fatalf("not restored: %s", st)
	}
	if s := e.missingSince(ep); s.Valid {
		t.Errorf("missing_since kept after the file came back: %v", s.String)
	}
}

// Scans remove titles missing longer than the setting, with watch state and the show left
// empty; not before, and never with the setting at 0.
func TestScanRemovesMissingAfterGrace(t *testing.T) {
	e := newEnv(t)
	c := &clock{time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)}
	days := 7
	e.sc.Now, e.sc.RemoveMissingAfter = c.now, func() int { return days }
	e.touch("tv/Kept/Kept - S01E01.mkv")
	e.touch("tv/Gone/Gone - S01E01.mkv")
	l := e.lib("TV", library.Shows, "tv")
	e.scan(l)
	var epID int64
	if err := e.db.QueryRow(`SELECT id FROM items WHERE type = 'episode' AND grandparent_id = (SELECT id FROM items WHERE title = 'Gone')`).Scan(&epID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Exec(`INSERT INTO users(id, username, display_name) VALUES (1, 'u', 'u')`); err != nil {
		t.Fatal(err)
	}
	if _, err := e.db.Exec(`INSERT INTO user_item_state(user_id, item_id, play_count) VALUES (1, ?, 1)`, epID); err != nil {
		t.Fatal(err)
	}

	// The Gone folder moves to another library.
	os.RemoveAll(filepath.Join(e.root, "tv/Gone"))
	e.scan(l)
	c.t = c.t.Add(6 * 24 * time.Hour)
	if st := e.scan(l); st.Removed != 0 {
		t.Fatalf("removed before the grace period: %s", st)
	}
	if n := e.count(`SELECT COUNT(*) FROM items WHERE type = 'show' AND title = 'Gone'`); n != 1 {
		t.Fatal("show removed before the grace period")
	}
	// Turned off: kept however long.
	days = 0
	c.t = c.t.Add(30 * 24 * time.Hour)
	if st := e.scan(l); st.Removed != 0 {
		t.Fatalf("removed with the setting off: %s", st)
	}
	days = 7
	st := e.scan(l)
	if st.Removed != 1 {
		t.Fatalf("after the grace period: %s", st)
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
	if n := e.count(`SELECT COUNT(*) FROM items WHERE type = 'episode'`); n != 1 {
		t.Errorf("episodes = %d, want the kept one", n)
	}
}

// Files in a library folder that's offline, or empty like an unmounted disk's mount point,
// are never removed.
func TestScanNeverRemovesFromOfflineFolder(t *testing.T) {
	e := newEnv(t)
	c := &clock{time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)}
	e.sc.Now, e.sc.RemoveMissingAfter = c.now, func() int { return 1 }
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
	os.RemoveAll(filepath.Join(cc, "Ran (1985)")) // folder left empty
	for range 3 {
		c.t = c.t.Add(10 * 24 * time.Hour)
		if st := e.scan(l); st.Removed != 0 {
			t.Fatalf("removed from an offline folder: %s", st)
		}
	}
	if n := e.count(`SELECT COUNT(*) FROM items WHERE type = 'movie'`); n != 3 {
		t.Fatalf("movies = %d, want 3", n)
	}
	if n := e.count(`SELECT available FROM media_files WHERE path LIKE ?`, b+"%"); n != 1 {
		t.Error("offline drive's file marked missing")
	}
	// Removing by hand skips them too.
	if n, err := RemoveMissing(context.Background(), e.db, l); err != nil || n != 0 {
		t.Fatalf("remove missing = %d, %v", n, err)
	}
}

// Remove missing titles now: counts and removes the gone titles, refuses when no folder
// can be reached.
func TestRemoveMissingNow(t *testing.T) {
	e := newEnv(t)
	e.touch("movies/Heat (1995)/Heat (1995).mkv")
	e.touch("movies/Alien (1979)/Alien (1979).mkv")
	e.touch("movies/Ran (1985)/Ran (1985).mkv")
	e.touch("movies/Ran (1985)/Ran (1985) {edition-Director's Cut}.mkv")
	l := e.lib("Movies", library.Movies, "movies")
	e.scan(l)
	os.RemoveAll(filepath.Join(e.root, "movies/Alien (1979)"))
	os.Remove(filepath.Join(e.root, "movies/Ran (1985)/Ran (1985) {edition-Director's Cut}.mkv")) // one of two versions
	e.scan(l)
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
