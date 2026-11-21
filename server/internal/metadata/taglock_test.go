package metadata

import (
	"context"
	"path/filepath"
	"testing"

	"marquee/internal/db"
	"marquee/internal/metadata/tmdb"
)

// TMDB leaves genres and countries an administrator edited alone (META-13).
func TestAgentsKeepEditedTags(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	d, err := db.Open(ctx, filepath.Join(dir, "t.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	mustExec(t, d, `INSERT INTO libraries(id, name, type) VALUES (1, 'Movies', 'movies')`)
	mustExec(t, d, `INSERT INTO items(id, library_id, type, title, sort_title, locked_fields) VALUES (5, 1, 'movie', 'Film', 'Film', '["genres","countries"]')`)
	mustExec(t, d, `INSERT INTO items(id, library_id, type, title, sort_title) VALUES (6, 1, 'movie', 'Other', 'Other')`)
	tx, _ := d.BeginTx(ctx, nil)
	for _, id := range []int64{5, 6} {
		if err := setGenres(ctx, tx, id, []tmdb.Genre{{Name: "Comedy"}}); err != nil {
			t.Fatal(err)
		}
		if err := replaceTags(ctx, tx, id, "country", []string{"Italy"}); err != nil {
			t.Fatal(err)
		}
	}
	tx.Commit()
	count := func(id int64) int {
		var n int
		d.QueryRow(`SELECT COUNT(*) FROM item_tags WHERE item_id = ?`, id).Scan(&n)
		return n
	}
	if count(5) != 0 || count(6) != 2 {
		t.Fatalf("locked item got %d tags, other %d", count(5), count(6))
	}
}
