package items

import (
	"context"
	"errors"
	"testing"
)

// Folders are the person's own: they hold their playlists and those shared with them, a
// playlist is in one folder at most, and deleting a folder leaves its playlists at the top level.
func TestPlaylistFolders(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	s := NewStore(d)
	ann := mustExec(t, d, `INSERT INTO users(username, display_name) VALUES ('ann', 'Ann')`)
	bob := mustExec(t, d, `INSERT INTO users(username, display_name) VALUES ('bob', 'Bob')`)
	mine, err := s.CreatePlaylist(ctx, Access{UserID: ann}, "Techno", "audio", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	theirs, _ := s.CreatePlaylist(ctx, Access{UserID: bob}, "House", "audio", nil, nil)
	mustExec(t, d, `INSERT INTO playlist_shares(playlist_id, user_id, status) VALUES (?, ?, 'accepted')`, theirs.ID, ann)
	private, _ := s.CreatePlaylist(ctx, Access{UserID: bob}, "Bob's", "audio", nil, nil)

	el, err := s.CreatePlaylistFolder(ctx, ann, " Electronic ")
	if err != nil || el.Name != "Electronic" {
		t.Fatalf("create: %+v %v", el, err)
	}
	other, _ := s.CreatePlaylistFolder(ctx, ann, "Ambient")
	acc := Access{UserID: ann}
	for _, p := range []int64{mine.ID, theirs.ID} {
		if err := s.MoveToPlaylistFolder(ctx, acc, el.ID, p); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.MoveToPlaylistFolder(ctx, acc, el.ID, private.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("someone else's unshared playlist: %v", err)
	}
	if _, err := s.RenamePlaylistFolder(ctx, bob, el.ID, "Mine"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Bob renamed Ann's folder: %v", err)
	}
	folders, _ := s.PlaylistFolders(ctx, ann)
	if len(folders) != 2 || folders[0].Name != "Ambient" || folders[1].PlaylistCount != 2 {
		t.Fatalf("folders: %+v", folders)
	}
	// Moving to another folder takes it out of the first.
	if err := s.MoveToPlaylistFolder(ctx, acc, other.ID, mine.ID); err != nil {
		t.Fatal(err)
	}
	inFolder := func() map[string]int64 {
		list, _ := s.Playlists(ctx, acc, "")
		out := map[string]int64{}
		for _, p := range list {
			out[p.Title] = p.FolderID
		}
		return out
	}
	if got := inFolder(); got["Techno"] != other.ID || got["House"] != el.ID {
		t.Fatalf("after moving: %v", got)
	}
	if bobs, _ := s.Playlists(ctx, Access{UserID: bob}, ""); bobs[0].FolderID != 0 || bobs[1].FolderID != 0 {
		t.Fatalf("Ann's folders showed for Bob: %+v", bobs)
	}
	if err := s.RemoveFromPlaylistFolder(ctx, ann, el.ID, theirs.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveFromPlaylistFolder(ctx, ann, el.ID, theirs.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("removing twice: %v", err)
	}
	if f, err := s.RenamePlaylistFolder(ctx, ann, other.ID, "Chill"); err != nil || f.Name != "Chill" || f.PlaylistCount != 1 {
		t.Fatalf("rename: %+v %v", f, err)
	}
	if err := s.DeletePlaylistFolder(ctx, ann, other.ID); err != nil {
		t.Fatal(err)
	}
	if got := inFolder(); got["Techno"] != 0 || len(got) != 2 {
		t.Fatalf("deleting the folder should keep its playlists: %v", got)
	}
}
