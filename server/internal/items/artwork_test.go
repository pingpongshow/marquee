package items

import (
	"context"
	"errors"
	"testing"
)

// Editing tag lists replaces and locks them; artwork can be chosen from the item's own, from a
// link or an upload, survives an agent replacing its own images, and goes back to automatic
// (META-13).
func TestEditTagsAndChooseArtwork(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	s := NewStore(d)
	lib := mustExec(t, d, `INSERT INTO libraries(name, type) VALUES ('Movies', 'movies')`)
	film := mustExec(t, d, `INSERT INTO items(library_id, type, title, sort_title) VALUES (?, 'movie', 'Film', 'Film')`, lib)
	mustExec(t, d, `INSERT INTO tags(kind, name) VALUES ('genre', 'Drama')`)
	mustExec(t, d, `INSERT INTO item_tags(item_id, tag_id) SELECT ?, id FROM tags WHERE name = 'Drama'`, film)

	if err := s.Edit(ctx, film, map[string]any{"index": 3}, map[string][]string{"genres": {"Thriller", " Crime ", "thriller", ""}, "countries": {"France"}}, nil); err != nil {
		t.Fatal(err)
	}
	det, err := s.Get(ctx, Access{UserID: 0}, film, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(det.Genres) != 2 || det.Genres[0] != "Crime" || det.Genres[1] != "Thriller" || len(det.Countries) != 1 {
		t.Fatalf("genres %v countries %v", det.Genres, det.Countries)
	}
	has := func(list []string, v string) bool {
		for _, x := range list {
			if x == v {
				return true
			}
		}
		return false
	}
	if !has(det.LockedFields, "genres") || !has(det.LockedFields, "countries") || !has(det.LockedFields, "index") {
		t.Fatalf("locks %v", det.LockedFields)
	}
	s.Edit(ctx, film, nil, nil, []string{"genres"})
	det, _ = s.Get(ctx, Access{UserID: 0}, film, false)
	if has(det.LockedFields, "genres") || !has(det.LockedFields, "countries") {
		t.Fatalf("after unlocking genres: %v", det.LockedFields)
	}

	// Artwork: an agent's poster (selected), and a local one.
	agent := mustExec(t, d, `INSERT INTO artwork(item_id, kind, source, remote_url, selected) VALUES (?, 'poster', 'tmdb', 'https://image.tmdb.org/a.jpg', 1)`, film)
	local := mustExec(t, d, `INSERT INTO artwork(item_id, kind, source, local_path, selected) VALUES (?, 'poster', 'local', '/m/poster.jpg', 0)`, film)
	selected := func() (int64, string) {
		var id int64
		var src string
		d.QueryRow(`SELECT id, source FROM artwork WHERE item_id = ? AND kind = 'poster' AND selected = 1`, film).Scan(&id, &src)
		return id, src
	}
	// Choosing the agent's keeps a copy, which the agent's next refresh doesn't remove.
	if err := s.ChooseArtwork(ctx, film, "poster", agent); err != nil {
		t.Fatal(err)
	}
	if _, src := selected(); src != "chosen" {
		t.Fatalf("chosen agent poster is %s", src)
	}
	mustExec(t, d, `DELETE FROM artwork WHERE item_id = ? AND source = 'tmdb'`, film)
	mustExec(t, d, `INSERT INTO artwork(item_id, kind, source, remote_url, selected)
		SELECT ?, 'poster', 'tmdb', 'https://image.tmdb.org/b.jpg', NOT EXISTS (SELECT 1 FROM artwork WHERE item_id = ? AND kind = 'poster' AND selected = 1)`, film, film)
	if _, src := selected(); src != "chosen" {
		t.Fatalf("a refresh replaced the chosen poster: %s", src)
	}
	// A link, twice: one row. An upload.
	s.ChooseArtworkURL(ctx, film, "poster", "https://example.com/p.jpg", 0, 0)
	s.ChooseArtworkURL(ctx, film, "poster", "https://example.com/p.jpg", 0, 0)
	var links int
	d.QueryRow(`SELECT COUNT(*) FROM artwork WHERE item_id = ? AND remote_url = 'https://example.com/p.jpg'`, film).Scan(&links)
	if links != 1 {
		t.Fatalf("%d copies of a link", links)
	}
	if err := s.AddUploadedArtwork(ctx, film, "poster", "uploads/1-poster-ab.jpg"); err != nil {
		t.Fatal(err)
	}
	if _, src := selected(); src != "upload" {
		t.Fatalf("upload not selected: %s", src)
	}
	// Back to automatic: chosen and uploaded go, the local file's poster wins.
	uploads, err := s.ResetArtwork(ctx, film, "poster")
	if err != nil || len(uploads) != 1 || uploads[0] != "uploads/1-poster-ab.jpg" {
		t.Fatalf("reset: %v %v", uploads, err)
	}
	if id, _ := selected(); id != local {
		t.Fatalf("after reset %d is selected, want the local poster %d", id, local)
	}
	if err := s.ChooseArtwork(ctx, film, "banner", local); !errors.Is(err, ErrArtworkKind) {
		t.Fatalf("banner: %v", err)
	}
	if err := s.ChooseArtwork(ctx, film, "poster", 999999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown artwork: %v", err)
	}
}
