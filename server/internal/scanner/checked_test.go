package scanner

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"marquee/internal/library"
)

// New files wait for their playback check (checked_at NULL). A file replaced with
// different content is checked again (its old result cleared); an unchanged file, and one
// only moved, keep theirs.
func TestRescanResetsFileCheck(t *testing.T) {
	e := newEnv(t)
	e.touch("movies/Alien (1979)/Alien (1979).mkv")
	e.touch("movies/Heat (1995)/Heat (1995).mkv")
	e.touch("movies/Ran (1985)/Ran (1985).mkv")
	l := e.lib("Movies", library.Movies, "movies")
	e.scan(l)
	if n := e.count(`SELECT COUNT(*) FROM media_files WHERE checked_at IS NULL`); n != 3 {
		t.Fatalf("new files waiting for their check = %d", n)
	}
	if _, err := e.db.Exec(`UPDATE media_files SET checked_at = '2026-01-01T00:00:00.000Z',
		check_error = CASE WHEN path LIKE '%Alien%' THEN 'The end doesn''t decode: no frames' END`); err != nil {
		t.Fatal(err)
	}

	// Alien is replaced (new content), Ran moves to another folder, Heat is untouched.
	alien := filepath.Join(e.root, "movies/Alien (1979)/Alien (1979).mkv")
	if err := os.WriteFile(alien, []byte("a new, longer copy of the film"), 0o644); err != nil {
		t.Fatal(err)
	}
	os.MkdirAll(filepath.Join(e.root, "movies/Kurosawa"), 0o755)
	if err := os.Rename(filepath.Join(e.root, "movies/Ran (1985)/Ran (1985).mkv"), filepath.Join(e.root, "movies/Kurosawa/Ran (1985).mkv")); err != nil {
		t.Fatal(err)
	}
	e.scan(l)

	state := func(like string) (checked, reason sql.NullString) {
		t.Helper()
		if err := e.db.QueryRow(`SELECT checked_at, check_error FROM media_files WHERE path LIKE ?`, like).Scan(&checked, &reason); err != nil {
			t.Fatal(err)
		}
		return
	}
	if c, r := state("%Alien%"); c.Valid || r.Valid {
		t.Errorf("replaced file: checked %v, error %v; want both cleared", c, r)
	}
	if c, _ := state("%Heat%"); !c.Valid {
		t.Error("unchanged file lost its check")
	}
	if c, _ := state("%Kurosawa%Ran%"); !c.Valid {
		t.Error("moved file lost its check")
	}
}
