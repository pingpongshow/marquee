package soundprint

import (
	"context"
	"path/filepath"
	"testing"

	"marquee/internal/db"
)

// Favourite songs (the heart) outrank 5-star songs: they lead the listener's taste, move up
// in radios and Muse picks, and are never avoided for skips or low ratings. A favourite
// album or artist lifts its songs a little.
func TestFavouritesWeighting(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	d, err := db.Open(ctx, filepath.Join(dir, "t.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	d.Exec(`INSERT INTO libraries (id, name, type) VALUES (1, 'Music', 'music')`)
	d.Exec(`INSERT INTO users (id, username, display_name) VALUES (1, 'u', 'U')`)
	ix := NewIndex()
	// Track 1: the favourite. Track 2: rated 5 stars. Both equally close to the query; track
	// 3 (on a favourite album) a little further; track 4 further still, by another artist.
	vecs := map[int64][]float32{1: {1, 0.30, 0}, 2: {1, 0.30, 0.0001}, 3: {1, 0.36, 0}, 4: {1, 0.34, 0}}
	for id, v := range vecs {
		album, artist := int64(50), int64(60+id)
		if id == 3 {
			album = 51
		}
		d.Exec(`INSERT INTO items (id, library_id, type, title, sort_title) VALUES (?, 1, 'artist', 'a', 'a') ON CONFLICT DO NOTHING`, artist)
		d.Exec(`INSERT INTO items (id, library_id, type, title, sort_title) VALUES (?, 1, 'album', 'b', 'b') ON CONFLICT DO NOTHING`, album)
		if _, err := d.Exec(`INSERT INTO items (id, library_id, type, parent_id, grandparent_id, title, sort_title) VALUES (?, 1, 'track', ?, ?, 't', 't')`, id, album, artist); err != nil {
			t.Fatal(err)
		}
		ix.Put(&Track{ItemID: id, AlbumID: album, ArtistID: artist, LibraryID: 1, Vec: normalize(v)})
	}
	d.Exec(`INSERT INTO user_item_state (user_id, item_id, rating, play_count, last_viewed_at) VALUES (1, 2, 10, 5, strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '-10 days'))`)
	// The favourite was quickly skipped and rated 1 star at some point: still never avoided.
	d.Exec(`INSERT INTO user_item_state (user_id, item_id, rating, favorite_at) VALUES (1, 1, 2, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))`)
	d.Exec(`INSERT INTO play_history (user_id, item_id, started_at, stopped_at, position_ms) VALUES (1, 1, strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '-1 day'), strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '-1 day'), 2000)`)
	d.Exec(`INSERT INTO user_item_state (user_id, item_id, favorite_at) VALUES (1, 51, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))`)
	s := &Service{DB: d, Index: ix}

	liked := s.Liked(ctx, 1)
	if len(liked) != 2 || liked[0].ItemID != 1 {
		t.Fatalf("liked: favourite should come first: %v", TrackIDs(liked))
	}
	for _, learn := range []bool{false, true} {
		s.Learn = func() bool { return learn }
		if w := s.Avoid(ctx, 1)[1]; w != 0 {
			t.Errorf("learn=%v: favourite avoided (%v)", learn, w)
		}
	}
	favour := s.Favour(ctx, 1)
	if favour[1] != favourTrack || favour[3] != favourGroup || favour[2] != 0 || favour[4] != 0 {
		t.Fatalf("favour: %v", favour)
	}
	q := normalize([]float32{1, 0.3, 0})
	o := Options{Avoid: s.Avoid(ctx, 1), Favour: favour}
	picked := ix.Pick(q, 4, o)
	if len(picked) < 3 || picked[0].ItemID != 1 {
		t.Fatalf("pick: favourite should lead: %v", TrackIDs(picked))
	}
	pos := map[int64]int{}
	for i, tr := range ix.Flow(q, nil, Options{Length: 4, Favour: favour}) {
		pos[tr.ItemID] = i
	}
	if pos[1] != 0 || pos[3] > pos[4] {
		t.Errorf("flow: favourite first, favourite album's song before the other: %v", pos)
	}
	// The taste leans to the favourite.
	other := []*Track{ix.Get(4)}
	if taste := s.Taste(ctx, 1, other); dot(taste, ix.Get(1).Vec) <= dot(Mean(other), ix.Get(1).Vec) {
		t.Errorf("taste should be nearer the favourite")
	}
}
