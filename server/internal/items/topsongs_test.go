package items

import (
	"context"
	"slices"
	"testing"
	"time"
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

// A thumbs down leaves a song out of Top Songs, radios without analysis, smart playlists and
// the year's playlist, takes back a favourite and a high rating, and comes off with either.
func TestThumbsDown(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	s := NewStore(d)
	me := mustExec(t, d, `INSERT INTO users(username, display_name) VALUES ('me', 'Me')`)
	them := mustExec(t, d, `INSERT INTO users(username, display_name) VALUES ('them', 'Them')`)
	lib := mustExec(t, d, `INSERT INTO libraries(name, type) VALUES ('Music', 'music')`)
	artist := mustExec(t, d, `INSERT INTO items(library_id, type, title, sort_title) VALUES (?, 'artist', 'A', 'A')`, lib)
	track := func(title string) int64 {
		return mustExec(t, d, `INSERT INTO items(library_id, type, title, sort_title, grandparent_id) VALUES (?, 'track', ?, ?, ?)`, lib, title, title, artist)
	}
	bad, good := track("Bad"), track("Good")
	acc := Access{UserID: me}
	if err := s.SetFavorite(ctx, me, bad, true); err != nil {
		t.Fatal(err)
	}
	rating := 9.0
	if err := s.SetRating(ctx, me, bad, &rating); err != nil {
		t.Fatal(err)
	}
	if err := s.SetFavorite(ctx, me, good, true); err != nil {
		t.Fatal(err)
	}
	if err := s.SetDisliked(ctx, me, bad, true); err != nil {
		t.Fatal(err)
	}
	sum, err := s.ByIDs(ctx, acc, []int64{bad})
	if err != nil || len(sum) != 1 || !sum[0].Disliked || sum[0].Favorite || sum[0].UserRating != 0 {
		t.Fatalf("after a thumbs down: %+v %v", sum, err)
	}
	// Someone else's list isn't touched.
	if other, err := s.DislikedSongs(ctx, them); err != nil || len(other) != 0 {
		t.Fatalf("their thumbs down: %v %v", other, err)
	}
	if list, _, err := s.TopSongs(ctx, acc, artist, 0, 10); err != nil || len(list) != 1 || list[0].ID != good {
		t.Fatalf("top songs: %+v %v", list, err)
	}
	if ids, err := s.RadioWithoutAnalysis(ctx, acc, artist, nil, 10); err != nil || slices.Contains(ids, bad) || !slices.Contains(ids, good) {
		t.Fatalf("radio: %v %v", ids, err)
	}
	if list, total, err := s.SmartItems(ctx, acc, "audio", SmartRules{Match: "all", Conditions: []Condition{{Field: "title", Op: "notContains", Value: "Nope"}}}, 0, 10); err != nil || total != 1 || list[0].ID != good {
		t.Fatalf("smart: %+v %d %v", list, total, err)
	}
	if p, err := s.SaveRecapPlaylist(ctx, acc, 2026, []int64{bad, good}); err != nil || p.ItemCount != 1 {
		t.Fatalf("year's playlist: %+v %v", p, err)
	}
	// Still in its album, and in a playlist they make.
	if p, err := s.CreatePlaylist(ctx, acc, "Mine", "audio", []int64{bad}, nil); err != nil || p.ItemCount != 1 {
		t.Fatalf("own playlist: %+v %v", p, err)
	}
	// A low rating leaves it; a high one or a favourite takes it back.
	low := 2.0
	if err := s.SetRating(ctx, me, bad, &low); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.DislikedSongs(ctx, me); !got[bad] {
		t.Fatal("a low rating took back the thumbs down")
	}
	high := 8.0
	if err := s.SetRating(ctx, me, bad, &high); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.DislikedSongs(ctx, me); got[bad] {
		t.Fatal("a high rating should take back the thumbs down")
	}
	s.SetDisliked(ctx, me, bad, true)
	if err := s.SetFavorite(ctx, me, bad, true); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.DislikedSongs(ctx, me); got[bad] {
		t.Fatal("a favourite should take back the thumbs down")
	}
	s.SetDisliked(ctx, me, bad, true)
	if err := s.SetDisliked(ctx, me, bad, false); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.DislikedSongs(ctx, me); got[bad] {
		t.Fatal("taking it back should clear it")
	}
}

// A thumbs down made offline and sent later loses to a newer heart or high rating and to a newer
// thumbs down change, and an older heart sent later doesn't undo a newer thumbs down.
func TestThumbsDownSentLater(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	s := NewStore(d)
	me := mustExec(t, d, `INSERT INTO users(username, display_name) VALUES ('me', 'Me')`)
	lib := mustExec(t, d, `INSERT INTO libraries(name, type) VALUES ('Music', 'music')`)
	track := func(title string) int64 {
		return mustExec(t, d, `INSERT INTO items(library_id, type, title, sort_title) VALUES (?, 'track', ?, ?)`, lib, title, title)
	}
	now := time.Now().UTC()
	early, late := now.Add(-2*time.Hour), now.Add(-time.Hour)
	disliked := func(id int64) bool {
		t.Helper()
		got, err := s.DislikedSongs(ctx, me)
		if err != nil {
			t.Fatal(err)
		}
		return got[id]
	}
	favorite := func(id int64, at time.Time) {
		t.Helper()
		if fresh, err := s.Fresh(ctx, me, id, "favorite", &at); err != nil {
			t.Fatal(err)
		} else if fresh {
			if err := s.SetFavorite(ctx, me, id, true); err != nil {
				t.Fatal(err)
			}
		}
	}

	// A newer heart stands.
	a := track("A")
	favorite(a, late)
	if err := s.SetDislikedAt(ctx, me, a, true, &early); err != nil {
		t.Fatal(err)
	}
	if disliked(a) {
		t.Fatal("an older thumbs down beat a newer heart")
	}
	// A newer high rating stands, a newer low one doesn't get in the way.
	b, c := track("B"), track("C")
	high, low := 9.0, 2.0
	for id, r := range map[int64]*float64{b: &high, c: &low} {
		if _, err := s.Fresh(ctx, me, id, "rating", &late); err != nil {
			t.Fatal(err)
		}
		if err := s.SetRating(ctx, me, id, r); err != nil {
			t.Fatal(err)
		}
		if err := s.SetDislikedAt(ctx, me, id, true, &early); err != nil {
			t.Fatal(err)
		}
	}
	if disliked(b) || !disliked(c) {
		t.Fatalf("after ratings: high %v, low %v", disliked(b), disliked(c))
	}
	// An older heart sent later doesn't take back a newer thumbs down.
	e := track("E")
	if err := s.SetDislikedAt(ctx, me, e, true, &late); err != nil {
		t.Fatal(err)
	}
	favorite(e, early)
	if !disliked(e) {
		t.Fatal("an older heart took back a newer thumbs down")
	}
	// Among thumbs down changes the newest stands, and a change made now always applies.
	if err := s.SetDislikedAt(ctx, me, e, false, &early); err != nil {
		t.Fatal(err)
	}
	if !disliked(e) {
		t.Fatal("an older take-back undid a newer thumbs down")
	}
	if err := s.SetDislikedAt(ctx, me, e, false, nil); err != nil {
		t.Fatal(err)
	}
	if disliked(e) {
		t.Fatal("a take-back made now should apply")
	}
	if err := s.SetDislikedAt(ctx, me, a, true, nil); err != nil {
		t.Fatal(err)
	}
	if !disliked(a) {
		t.Fatal("a thumbs down made now should apply over an earlier heart")
	}
}
