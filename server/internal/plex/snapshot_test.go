package plex

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

// The copy of Plex's database is deleted when the import or preview is done with it.
func TestSnapshotRemovedOnClose(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, dbRel)
	os.MkdirAll(filepath.Dir(p), 0o755)
	plexDB, err := sql.Open("sqlite", "file:"+p)
	if err != nil {
		t.Fatal(err)
	}
	for _, tbl := range []string{"library_sections", "section_locations", "accounts", "metadata_items", "media_items",
		"media_parts", "metadata_item_settings", "metadata_item_views", "taggings", "tags", "play_queue_generators"} {
		if _, err := plexDB.Exec(`CREATE TABLE ` + tbl + ` (id INTEGER PRIMARY KEY)`); err != nil {
			t.Fatal(err)
		}
	}
	plexDB.Close()
	work := t.TempDir()
	src, err := Snapshot(context.Background(), root, work)
	if err != nil {
		t.Fatal(err)
	}
	copyPath := filepath.Join(work, "plex-snapshot.db")
	if _, err := os.Stat(copyPath); err != nil {
		t.Fatalf("snapshot not taken: %v", err)
	}
	if err := src.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(copyPath); !os.IsNotExist(err) {
		t.Fatalf("snapshot left behind: %v", err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("Plex's own database must stay: %v", err)
	}
}
