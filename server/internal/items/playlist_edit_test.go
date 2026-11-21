package items

import (
	"context"
	"errors"
	"testing"
)

// The playlist editor saves a new order and removals in one go, leaving entries the person
// can't see where they were; playlists combine into a new one or the end of another, without
// duplicates when asked, and the person's own sources can go afterwards (MUSIC-29).
func TestPlaylistEditAndMerge(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	s := NewStore(d)
	ann := mustExec(t, d, `INSERT INTO users(username, display_name) VALUES ('ann', 'Ann')`)
	bob := mustExec(t, d, `INSERT INTO users(username, display_name) VALUES ('bob', 'Bob')`)
	lib := mustExec(t, d, `INSERT INTO libraries(name, type) VALUES ('Music', 'music')`)
	other := mustExec(t, d, `INSERT INTO libraries(name, type) VALUES ('Private', 'music')`)
	movies := mustExec(t, d, `INSERT INTO libraries(name, type) VALUES ('Movies', 'movies')`)
	track := func(library int64, title string) int64 {
		return mustExec(t, d, `INSERT INTO items(library_id, type, title, sort_title, duration_ms) VALUES (?, 'track', ?, ?, 1000)`, library, title, title)
	}
	a, b, c, hidden := track(lib, "A"), track(lib, "B"), track(lib, "C"), track(other, "Hidden")
	film := mustExec(t, d, `INSERT INTO items(library_id, type, title, sort_title, duration_ms) VALUES (?, 'movie', 'Film', 'Film', 1000)`, movies)
	full := Access{UserID: ann}
	limited := Access{UserID: ann, LibraryIDs: []int64{lib}}

	titles := func(id int64) []string {
		t.Helper()
		list, _, err := s.PlaylistItems(ctx, full, id, 0, 100)
		if err != nil {
			t.Fatal(err)
		}
		out := []string{}
		for _, e := range list {
			out = append(out, e.Item.Title)
		}
		return out
	}
	entries := func(acc Access, id int64) map[string]int64 {
		t.Helper()
		list, _, err := s.PlaylistItems(ctx, acc, id, 0, 100)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]int64{}
		for _, e := range list {
			out[e.Item.Title] = e.EntryID
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

	pl, err := s.CreatePlaylist(ctx, full, "Mine", "audio", []int64{a, hidden, b, c}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// Reorder and remove at once, seeing only the music library: Hidden stays, after them.
	e := entries(limited, pl.ID)
	if err := s.SetPlaylistOrder(ctx, limited, pl.ID, []int64{e["C"], e["A"], e["C"], 999999}); err != nil {
		t.Fatal(err)
	}
	same(titles(pl.ID), "C", "A", "Hidden")
	// Only the owner's playlists can be edited.
	if err := s.SetPlaylistOrder(ctx, Access{UserID: bob}, pl.ID, nil); !errors.Is(err, ErrNotFound) {
		t.Fatalf("someone else's playlist: %v", err)
	}

	two, err := s.CreatePlaylist(ctx, full, "Two", "audio", []int64{b, a}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// A new playlist from both, without duplicates; the sources stay.
	merged, err := s.MergePlaylists(ctx, full, []int64{pl.ID, two.ID}, MergeOptions{Title: "Both", SkipDuplicates: true})
	if err != nil {
		t.Fatal(err)
	}
	if merged.Title != "Both" {
		t.Fatalf("title %q", merged.Title)
	}
	same(titles(merged.ID), "C", "A", "Hidden", "B")
	// Into an existing one, duplicates kept, sources deleted.
	three, _ := s.CreatePlaylist(ctx, full, "Three", "audio", []int64{c}, nil)
	if _, err := s.MergePlaylists(ctx, full, []int64{two.ID, three.ID}, MergeOptions{IntoID: merged.ID, DeleteSources: true}); err != nil {
		t.Fatal(err)
	}
	same(titles(merged.ID), "C", "A", "Hidden", "B", "B", "A", "C")
	if _, err := s.Playlist(ctx, ann, two.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("source kept: %v", err)
	}
	// Music and videos don't mix.
	vids, _ := s.CreatePlaylist(ctx, full, "Films", "video", []int64{film}, nil)
	if _, err := s.MergePlaylists(ctx, full, []int64{pl.ID, vids.ID}, MergeOptions{}); !errors.Is(err, ErrPlaylistMix) {
		t.Fatalf("mixed kinds: %v", err)
	}
	// Someone else's playlist that wasn't shared can't be a source.
	if _, err := s.MergePlaylists(ctx, Access{UserID: bob}, []int64{pl.ID}, MergeOptions{}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unshared source: %v", err)
	}
}

// A thumbs up and a thumbs down take each other back; a thumbs down on a film keeps it out of
// suggestions (Not interested), and liking it again brings it back (USER-21).
func TestThumbsUpAndDown(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	s := NewStore(d)
	ann := mustExec(t, d, `INSERT INTO users(username, display_name) VALUES ('ann', 'Ann')`)
	lib := mustExec(t, d, `INSERT INTO libraries(name, type) VALUES ('Movies', 'movies')`)
	film := mustExec(t, d, `INSERT INTO items(library_id, type, title, sort_title, duration_ms) VALUES (?, 'movie', 'Film', 'Film', 1000)`, lib)
	state := func() (liked, disliked bool) {
		t.Helper()
		list, err := s.ByIDs(ctx, Access{UserID: ann}, []int64{film})
		if err != nil || len(list) != 1 {
			t.Fatal(err)
		}
		return list[0].Liked, list[0].Disliked
	}
	if err := s.SetLikedAt(ctx, ann, film, true, nil); err != nil {
		t.Fatal(err)
	}
	if l, dl := state(); !l || dl {
		t.Fatalf("liked %v disliked %v", l, dl)
	}
	if liked, _ := s.likedVideos(ctx, ann, 10); len(liked) != 1 || liked[0] != film {
		t.Fatalf("liked videos %v", liked)
	}
	if err := s.SetDislikedAt(ctx, ann, film, true, nil); err != nil {
		t.Fatal(err)
	}
	if l, dl := state(); l || !dl {
		t.Fatalf("liked %v disliked %v", l, dl)
	}
	if !s.NotInterested(ctx, ann, film) {
		t.Fatal("a thumbs down should keep the film out of suggestions")
	}
	if err := s.SetLikedAt(ctx, ann, film, true, nil); err != nil {
		t.Fatal(err)
	}
	if l, dl := state(); !l || dl {
		t.Fatalf("liked %v disliked %v", l, dl)
	}
	if s.NotInterested(ctx, ann, film) {
		t.Fatal("liking it again should bring it back to suggestions")
	}
	// A Not interested of its own stays when a thumbs up comes.
	if err := s.SetNotInterested(ctx, ann, film, true); err != nil {
		t.Fatal(err)
	}
	s.SetLiked(ctx, ann, film, false)
	s.SetLiked(ctx, ann, film, true)
	if !s.NotInterested(ctx, ann, film) {
		t.Fatal("Not interested made on its own was taken back")
	}
}
