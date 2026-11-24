package items

import (
	"context"
	"errors"
	"testing"
)

// A collection (USER-23) belongs to one video library and holds its titles as they were added,
// each once; the video playlists made before go to the libraries their titles are in, with a
// show that was there whole becoming the show.
func TestOwnCollections(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	s := NewStore(d)
	ann := mustExec(t, d, `INSERT INTO users(username, display_name) VALUES ('ann', 'Ann')`)
	movies := mustExec(t, d, `INSERT INTO libraries(name, type) VALUES ('Movies', 'movies')`)
	shows := mustExec(t, d, `INSERT INTO libraries(name, type) VALUES ('Shows', 'shows')`)
	music := mustExec(t, d, `INSERT INTO libraries(name, type) VALUES ('Music', 'music')`)
	item := func(lib int64, typ, title string, parent, grand any) int64 {
		return mustExec(t, d, `INSERT INTO items(library_id, type, title, sort_title, parent_id, grandparent_id, duration_ms) VALUES (?, ?, ?, ?, ?, ?, 1000)`,
			lib, typ, title, title, parent, grand)
	}
	film, film2 := item(movies, "movie", "Film", nil, nil), item(movies, "movie", "Film 2", nil, nil)
	show := item(shows, "show", "Show", nil, nil)
	season := item(shows, "season", "Season 1", show, nil)
	e1, e2 := item(shows, "episode", "E1", season, show), item(shows, "episode", "E2", season, show)
	other := item(shows, "show", "Other", nil, nil)
	oSeason := item(shows, "season", "Season 1", other, nil)
	o1 := item(shows, "episode", "O1", oSeason, other)
	item(shows, "episode", "O2", oSeason, other)
	track := item(music, "track", "Song", nil, nil)
	acc := Access{UserID: ann}
	titles := func(id int64) []string {
		t.Helper()
		list, _, err := s.PlaylistItems(ctx, acc, id, 0, 100)
		if err != nil {
			t.Fatal(err)
		}
		out := []string{}
		for _, e := range list {
			out = append(out, e.Item.Title)
		}
		return out
	}
	same := func(got []string, want ...string) {
		t.Helper()
		if len(got) != len(want) {
			t.Fatalf("got %v, want %v", got, want)
		}
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("got %v, want %v", got, want)
			}
		}
	}

	// Its own library's titles only, a show as a show, each once.
	c, err := s.CreateOwnCollection(ctx, acc, "Favourites", shows, []int64{show, film, e1, track, show})
	if err != nil || c.LibraryID != shows || c.Kind != "video" {
		t.Fatalf("create: %+v %v", c, err)
	}
	same(titles(c.ID), "Show", "E1")
	if err := s.AddToPlaylist(ctx, acc, c.ID, []int64{show}); err != nil {
		t.Fatalf("adding what's there: %v", err)
	}
	if err := s.AddToPlaylist(ctx, acc, c.ID, []int64{film}); !errors.Is(err, ErrPlaylistKind) {
		t.Fatalf("another library's film: %v", err)
	}
	same(titles(c.ID), "Show", "E1")
	// Not in a music library, and not as a plain playlist.
	if _, err := s.CreateOwnCollection(ctx, acc, "No", music, nil); !errors.Is(err, ErrCollectionLibrary) {
		t.Fatalf("music library: %v", err)
	}
	if _, err := s.CreatePlaylist(ctx, acc, "No", "video", nil, nil); !errors.Is(err, ErrCollectionLibrary) {
		t.Fatalf("video playlist: %v", err)
	}
	// Listed under its library; hidden while the library has them turned off.
	if list, _ := s.OwnCollections(ctx, acc, movies); len(list) != 0 {
		t.Fatalf("movies has %d", len(list))
	}
	if list, _ := s.OwnCollections(ctx, acc, shows); len(list) != 1 {
		t.Fatalf("shows has %d", len(list))
	}
	mustExec(t, d, `UPDATE libraries SET options = '{"personalCollections":false}' WHERE id = ?`, shows)
	if list, _ := s.Playlists(ctx, acc, ""); len(list) != 0 {
		t.Fatalf("turned off, still listed: %d", len(list))
	}
	if _, err := s.CreateOwnCollection(ctx, acc, "No", shows, nil); !errors.Is(err, ErrCollectionLibrary) {
		t.Fatalf("turned off: %v", err)
	}
	mustExec(t, d, `UPDATE libraries SET options = '{}' WHERE id = ?`, shows)
	// Collections of different libraries don't combine.
	m, _ := s.CreateOwnCollection(ctx, acc, "Films", movies, []int64{film})
	if _, err := s.MergePlaylists(ctx, acc, []int64{c.ID, m.ID}, MergeOptions{}); !errors.Is(err, ErrPlaylistMix) {
		t.Fatalf("two libraries: %v", err)
	}

	// An old video playlist: a film, a whole show's episodes, one episode of another show.
	old := mustExec(t, d, `INSERT INTO playlists(user_id, title, kind) VALUES (?, 'Weekend', 'video')`, ann)
	for i, it := range []int64{film2, e1, e2, o1, film2} {
		mustExec(t, d, `INSERT INTO playlist_items(playlist_id, item_id, ord) VALUES (?, ?, ?)`, old, it, i+1)
	}
	empty := mustExec(t, d, `INSERT INTO playlists(user_id, title, kind) VALUES (?, 'Empty', 'video')`, ann)
	if err := s.ConvertVideoPlaylists(ctx); err != nil {
		t.Fatal(err)
	}
	if err := s.ConvertVideoPlaylists(ctx); err != nil { // nothing left to do
		t.Fatal(err)
	}
	p, err := s.Playlist(ctx, ann, old)
	if err != nil || p.LibraryID != movies {
		t.Fatalf("converted: %+v %v", p, err)
	}
	same(titles(old), "Film 2")
	inShows, _ := s.OwnCollections(ctx, acc, shows)
	var weekend int64
	for _, x := range inShows {
		if x.Title == "Weekend" {
			weekend = x.ID
		}
	}
	if weekend == 0 || weekend == old {
		t.Fatalf("no Weekend in shows: %+v", inShows)
	}
	same(titles(weekend), "Show", "O1")
	if p, _ := s.Playlist(ctx, ann, empty); p.LibraryID != movies {
		t.Fatalf("empty one went to %d", p.LibraryID)
	}
}
