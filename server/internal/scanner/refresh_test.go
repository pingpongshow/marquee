package scanner

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"marquee/internal/library"
)

func TestRefreshSubtitles(t *testing.T) {
	e := newEnv(t)
	l := e.lib("Movies", library.Movies, "Movies")
	e.touch("Movies/Heat (1995)/Heat (1995).mkv")
	e.touch("Movies/Heat (1995)/Heat (1995).en.srt")
	e.scan(l)
	var fileID, keptID int64
	e.db.QueryRow(`SELECT id FROM media_files`).Scan(&fileID)
	e.db.QueryRow(`SELECT id FROM streams WHERE external_path IS NOT NULL`).Scan(&keptID)
	ext := `SELECT COUNT(*) FROM streams WHERE file_id = ? AND external_path IS NOT NULL`
	if n := e.count(ext, fileID); n != 1 {
		t.Fatalf("after scan: %d external subtitles", n)
	}

	// A rescan picks up a subtitle added next to an unchanged video (through the refresh).
	e.touch("Movies/Heat (1995)/Heat (1995).fr.forced.srt")
	e.scan(l)
	if n := e.count(ext, fileID); n != 2 {
		t.Fatalf("rescan: %d external subtitles, want 2", n)
	}
	if added, err := RefreshSubtitles(context.Background(), e.db, fileID); err != nil || added != 0 {
		t.Fatalf("refresh after the rescan: %d %v", added, err)
	}
	if n := e.count(`SELECT COUNT(*) FROM streams WHERE file_id = ? AND language = 'fre' AND is_forced = 1`, fileID); n != 1 {
		t.Fatal("forced French subtitle missing")
	}
	if n := e.count(`SELECT COUNT(*) FROM streams WHERE id = ?`, keptID); n != 1 {
		t.Fatal("existing subtitle stream lost its id")
	}

	// Removed files go; OpenSubtitles downloads (outside the folder) stay.
	os.Remove(filepath.Join(e.root, "Movies/Heat (1995)/Heat (1995).en.srt"))
	dl := filepath.Join(t.TempDir(), "1.eng.srt")
	e.db.Exec(`INSERT INTO downloaded_subtitles (file_id, path, language, title, hearing_impaired, provider_id) VALUES (?, ?, 'eng', 'OpenSubtitles', 0, '1')`, fileID, dl)
	e.db.Exec(`INSERT INTO streams (file_id, kind, codec, language, external_path) VALUES (?, 'subtitle', 'subrip', 'eng', ?)`, fileID, dl)
	if added, err := RefreshSubtitles(context.Background(), e.db, fileID); err != nil || added != 0 {
		t.Fatalf("second refresh: %d %v", added, err)
	}
	if n := e.count(ext, fileID); n != 2 {
		t.Fatalf("after removal: %d external subtitles, want the French one and the download", n)
	}
}
