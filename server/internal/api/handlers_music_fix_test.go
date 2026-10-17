package api

import (
	"context"
	"path/filepath"
	"testing"

	"marquee/internal/auth"
	"marquee/internal/db"
	"marquee/internal/items"
	"marquee/internal/metadata"
)

// Fix match for music and moving tracks are for admins, and only for what they apply to.
func TestMusicFixesAdminOnly(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	d, err := db.Open(ctx, filepath.Join(dir, "t.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	for _, q := range []string{
		`INSERT INTO libraries(id, name, type) VALUES (1, 'Music', 'music'), (2, 'Movies', 'movies')`,
		`INSERT INTO items(id, library_id, type, title, sort_title, scan_key) VALUES (1, 1, 'artist', 'A', 'A', 'a:a'), (2, 1, 'album', 'B', 'B', 'al:a:b')`,
		`INSERT INTO items(id, library_id, type, title, sort_title, parent_id, grandparent_id) VALUES (3, 1, 'track', 'C', '1', 2, 1)`,
		`INSERT INTO items(id, library_id, type, title, sort_title) VALUES (4, 2, 'movie', 'Heat', 'Heat')`,
	} {
		if _, err := d.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	h := &Handlers{DB: d, Items: items.NewStore(d), Metadata: &metadata.Service{DB: d}}
	user := auth.WithSession(ctx, auth.Session{User: auth.User{ID: 2}})
	adm := auth.WithSession(ctx, auth.Session{User: auth.User{ID: 1, IsAdmin: true}})
	mbid := "aaaaaaaa-0000-0000-0000-000000000001"

	if r, _ := h.SearchMusicMatches(user, SearchMusicMatchesRequestObject{ItemId: 1}); r == nil {
		t.Error("no response")
	} else if _, ok := r.(SearchMusicMatches403JSONResponse); !ok {
		t.Errorf("search as a user: %#v", r)
	}
	if r, _ := h.ApplyMusicMatch(user, ApplyMusicMatchRequestObject{ItemId: 1, Body: &MusicMatchRequest{Mbid: mbid}}); r == nil {
		t.Error("no response")
	} else if _, ok := r.(ApplyMusicMatch403JSONResponse); !ok {
		t.Errorf("match as a user: %#v", r)
	}
	if r, _ := h.SetTrackPlacement(user, SetTrackPlacementRequestObject{ItemId: 3, Body: &TrackPlacementRequest{AlbumId: ptr(int64(2))}}); r == nil {
		t.Error("no response")
	} else if _, ok := r.(SetTrackPlacement403JSONResponse); !ok {
		t.Errorf("move as a user: %#v", r)
	}
	if r, _ := h.ClearTrackPlacement(user, ClearTrackPlacementRequestObject{ItemId: 3}); r == nil {
		t.Error("no response")
	} else if _, ok := r.(ClearTrackPlacement403JSONResponse); !ok {
		t.Errorf("undo as a user: %#v", r)
	}

	// Movies and tracks aren't matched to MusicBrainz (nothing is asked of it).
	for _, id := range []int64{3, 4} {
		if r, err := h.SearchMusicMatches(adm, SearchMusicMatchesRequestObject{ItemId: id}); err != nil {
			t.Fatal(err)
		} else if _, ok := r.(SearchMusicMatches400JSONResponse); !ok {
			t.Errorf("search %d: %#v", id, r)
		}
		if r, err := h.ApplyMusicMatch(adm, ApplyMusicMatchRequestObject{ItemId: id, Body: &MusicMatchRequest{Mbid: mbid}}); err != nil {
			t.Fatal(err)
		} else if _, ok := r.(ApplyMusicMatch400JSONResponse); !ok {
			t.Errorf("match %d: %#v", id, r)
		}
	}
	if r, _ := h.ApplyMusicMatch(adm, ApplyMusicMatchRequestObject{ItemId: 99, Body: &MusicMatchRequest{Mbid: mbid}}); r == nil {
		t.Error("no response")
	} else if _, ok := r.(ApplyMusicMatch404JSONResponse); !ok {
		t.Errorf("missing item: %#v", r)
	}
	if r, _ := h.ApplyMusicMatch(adm, ApplyMusicMatchRequestObject{ItemId: 1, Body: &MusicMatchRequest{Mbid: "not an id"}}); r == nil {
		t.Error("no response")
	} else if _, ok := r.(ApplyMusicMatch400JSONResponse); !ok {
		t.Errorf("bad id: %#v", r)
	}
	// Only tracks move.
	for _, id := range []int64{2, 4} {
		if r, err := h.SetTrackPlacement(adm, SetTrackPlacementRequestObject{ItemId: id, Body: &TrackPlacementRequest{AlbumId: ptr(int64(2))}}); err != nil {
			t.Fatal(err)
		} else if _, ok := r.(SetTrackPlacement400JSONResponse); !ok {
			t.Errorf("move %d: %#v", id, r)
		}
		if r, err := h.ClearTrackPlacement(adm, ClearTrackPlacementRequestObject{ItemId: id}); err != nil {
			t.Fatal(err)
		} else if _, ok := r.(ClearTrackPlacement400JSONResponse); !ok {
			t.Errorf("undo %d: %#v", id, r)
		}
	}
	if r, _ := h.SetTrackPlacement(adm, SetTrackPlacementRequestObject{ItemId: 99, Body: &TrackPlacementRequest{AlbumId: ptr(int64(2))}}); r == nil {
		t.Error("no response")
	} else if _, ok := r.(SetTrackPlacement404JSONResponse); !ok {
		t.Errorf("missing track: %#v", r)
	}
}
