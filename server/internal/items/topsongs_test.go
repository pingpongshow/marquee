package items

import (
	"context"
	"slices"
	"testing"
)

// Favourites, songs rated 3.5 stars or more, and popular songs make the list, best first;
// a song rated lower with nothing else going for it doesn't.
func TestTopSongs(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	s := NewStore(d)
	me := mustExec(t, d, `INSERT INTO users(username, display_name) VALUES ('me', 'Me')`)
	them := mustExec(t, d, `INSERT INTO users(username, display_name) VALUES ('them', 'Them')`)
	lib := mustExec(t, d, `INSERT INTO libraries(name, type) VALUES ('Music', 'music')`)
	artist := mustExec(t, d, `INSERT INTO items(library_id, type, title, sort_title) VALUES (?, 'artist', 'A', 'A')`, lib)
	album := mustExec(t, d, `INSERT INTO items(library_id, type, title, sort_title, parent_id) VALUES (?, 'album', 'B', 'B', ?)`, lib, artist)
	track := func(title string) int64 {
		return mustExec(t, d, `INSERT INTO items(library_id, type, title, sort_title, parent_id, grandparent_id) VALUES (?, 'track', ?, ?, ?, ?)`, lib, title, title, album, artist)
	}
	fav, loved, justRated, belowBar, popular, played, plain := track("Fav"), track("Loved"), track("JustRated"), track("BelowBar"), track("Popular"), track("Played"), track("Plain")
	state := func(user, item int64, column string, value any) {
		mustExec(t, d, `INSERT INTO user_item_state(user_id, item_id, `+column+`) VALUES (?, ?, ?)`, user, item, value)
	}
	state(me, fav, "favorite_at", "2026-01-01T00:00:00.000Z")
	state(me, loved, "rating", 10)
	state(me, justRated, "rating", 7)
	state(me, belowBar, "rating", 6.5)
	state(them, played, "play_count", 4)
	state(them, fav, "rating", 2) // someone else's rating doesn't count
	mustExec(t, d, `INSERT INTO music_popular(artist_id, track_id, rank) VALUES (?, ?, 1)`, artist, popular)

	list, total, err := s.TopSongs(ctx, Access{UserID: me}, artist, 0, 100)
	if err != nil {
		t.Fatal(err)
	}
	var got []int64
	for _, it := range list {
		got = append(got, it.ID)
	}
	if want := []int64{fav, loved, played, popular, justRated}; !slices.Equal(got, want) || total != 5 {
		t.Fatalf("got %v of %d, want %v", got, total, want)
	}
	if slices.Contains(got, belowBar) || slices.Contains(got, plain) {
		t.Fatal("songs with nothing going for them should stay out")
	}
	page, total, err := s.TopSongs(ctx, Access{UserID: me}, artist, 3, 15)
	if err != nil || total != 5 || len(page) != 2 || page[0].ID != popular {
		t.Fatalf("second page: %+v %d %v", page, total, err)
	}
	if list, total, err := s.TopSongs(ctx, Access{UserID: me, LibraryIDs: []int64{}}, artist, 0, 15); err != nil || total != 0 || len(list) != 0 {
		t.Fatalf("no access: %+v %d %v", list, total, err)
	}
}

// Without analysis, a radio plays its artist and those similar or in the same genres, a track's
// starting with that track, and leaves out what's queued and what the listener can't see.
func TestRadioWithoutAnalysis(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	s := NewStore(d)
	me := mustExec(t, d, `INSERT INTO users(username, display_name) VALUES ('me', 'Me')`)
	lib := mustExec(t, d, `INSERT INTO libraries(name, type) VALUES ('Music', 'music')`)
	closed := mustExec(t, d, `INSERT INTO libraries(name, type) VALUES ('Closed', 'music')`)
	artist := func(library int64, name string) int64 {
		return mustExec(t, d, `INSERT INTO items(library_id, type, title, sort_title) VALUES (?, 'artist', ?, ?)`, library, name, name)
	}
	track := func(library, by int64, title string) int64 {
		return mustExec(t, d, `INSERT INTO items(library_id, type, title, sort_title, grandparent_id) VALUES (?, 'track', ?, ?, ?)`, library, title, title, by)
	}
	seedArtist, near, sameGenre, unrelated := artist(lib, "Seed"), artist(lib, "Near"), artist(lib, "Genre"), artist(lib, "Far")
	seed, seed2 := track(lib, seedArtist, "S1"), track(lib, seedArtist, "S2")
	nearSong, genreSong, _ := track(lib, near, "N1"), track(lib, sameGenre, "G1"), track(lib, unrelated, "F1")
	shut := track(closed, near, "Shut")
	mustExec(t, d, `INSERT INTO music_similar_artists(artist_id, similar_id, source, score) VALUES (?, ?, 'lastfm', 1)`, seedArtist, near)
	tag := mustExec(t, d, `INSERT INTO tags(name, kind) VALUES ('Rock', 'genre')`)
	mustExec(t, d, `INSERT INTO item_tags(item_id, tag_id) VALUES (?, ?), (?, ?)`, seedArtist, tag, sameGenre, tag)

	acc := Access{UserID: me, LibraryIDs: []int64{lib}}
	got, err := s.RadioWithoutAnalysis(ctx, acc, seed, nil, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 4 || got[0] != seed {
		t.Fatalf("a track's radio: %v", got)
	}
	for _, id := range []int64{seed2, nearSong, genreSong} {
		if !slices.Contains(got, id) {
			t.Fatalf("missing %d in %v", id, got)
		}
	}
	if slices.Contains(got, shut) {
		t.Fatal("a closed library's song was offered")
	}
	got, err = s.RadioWithoutAnalysis(ctx, acc, seedArtist, map[int64]bool{seed: true, nearSong: true}, 50)
	if err != nil || len(got) != 2 || slices.Contains(got, seed) || slices.Contains(got, nearSong) {
		t.Fatalf("continuing: %v %v", got, err)
	}
}
