package items

import (
	"context"
	"errors"
	"testing"
)

// Playlists stay private until shared; someone it's shared with accepts it, can play it but
// not change it, and can take it off their list, after which only the owner can bring it back.
func TestPlaylistSharing(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	s := NewStore(d)
	user := func(name string) int64 {
		return mustExec(t, d, `INSERT INTO users(username, display_name) VALUES (?, ?)`, name, name)
	}
	ann, bob, cid := user("ann"), user("bob"), user("cid")
	lib := mustExec(t, d, `INSERT INTO libraries(name, type) VALUES ('Music', 'music')`)
	other := mustExec(t, d, `INSERT INTO libraries(name, type) VALUES ('Private', 'music')`)
	track := func(library int64, title string) int64 {
		return mustExec(t, d, `INSERT INTO items(library_id, type, title, sort_title, duration_ms) VALUES (?, 'track', ?, ?, 1000)`, library, title, title)
	}
	song, hidden := track(lib, "Song"), track(other, "Hidden")
	annAcc, bobAcc := Access{UserID: ann}, Access{UserID: bob, LibraryIDs: []int64{lib}}

	pl, err := s.CreatePlaylist(ctx, annAcc, "Ann's", "audio", []int64{song, hidden}, nil)
	if err != nil {
		t.Fatal(err)
	}
	listed := func(acc Access) []Playlist {
		t.Helper()
		list, err := s.Playlists(ctx, acc, "")
		if err != nil {
			t.Fatal(err)
		}
		return list
	}
	invites := func(acc Access) int {
		t.Helper()
		list, err := s.PlaylistInvites(ctx, acc)
		if err != nil {
			t.Fatal(err)
		}
		return len(list)
	}

	// Private until shared.
	if len(listed(bobAcc)) != 0 || invites(bobAcc) != 0 {
		t.Fatal("someone else's playlist should be private")
	}
	if _, err := s.ReadablePlaylist(ctx, bobAcc, pl.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("readable before sharing: %v", err)
	}
	if err := s.SetPlaylistShares(ctx, bob, pl.ID, []int64{bob}, false); !errors.Is(err, ErrNotFound) {
		t.Fatalf("only the owner may share: %v", err)
	}
	if _, err := s.AcceptPlaylistShare(ctx, bobAcc, pl.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("accepting what wasn't offered: %v", err)
	}

	// Shared: offered, but not theirs until accepted.
	if err := s.SetPlaylistShares(ctx, ann, pl.ID, []int64{bob, ann}, false); err != nil {
		t.Fatal(err)
	}
	if invites(bobAcc) != 1 || invites(Access{UserID: cid}) != 0 || len(listed(bobAcc)) != 0 {
		t.Fatal("should be offered to bob alone")
	}
	if _, shares, err := s.PlaylistShares(ctx, ann, pl.ID); err != nil || len(shares) != 1 || shares[0].Status != "pending" || shares[0].Name != "bob" {
		t.Fatalf("shares: %+v %v", shares, err)
	}
	got, err := s.AcceptPlaylistShare(ctx, bobAcc, pl.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.ReadOnly || got.OwnerName != "ann" {
		t.Fatalf("accepted: %+v", got)
	}
	if got.ItemCount != 1 {
		t.Fatalf("should count only what bob may see: %d", got.ItemCount)
	}
	if mine := listed(annAcc); len(mine) != 1 || mine[0].ReadOnly || mine[0].ItemCount != 2 || mine[0].ShareCount != 1 {
		t.Fatalf("the owner's view: %+v", mine)
	}
	entries, total, err := s.PlaylistItems(ctx, bobAcc, pl.ID, 0, 10)
	if err != nil || total != 1 || entries[0].Item.ID != song {
		t.Fatalf("entries: %+v %d %v", entries, total, err)
	}

	// Theirs to play, not to change.
	if err := s.RenamePlaylist(ctx, bob, pl.ID, "Mine now"); !errors.Is(err, ErrNotFound) {
		t.Errorf("rename: %v", err)
	}
	if err := s.DeletePlaylist(ctx, bob, pl.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("delete: %v", err)
	}
	if err := s.AddToPlaylist(ctx, bobAcc, pl.ID, []int64{song}); !errors.Is(err, ErrNotFound) {
		t.Errorf("add: %v", err)
	}
	if err := s.RemovePlaylistItem(ctx, bob, pl.ID, entries[0].EntryID); !errors.Is(err, ErrNotFound) {
		t.Errorf("remove: %v", err)
	}
	if err := s.MovePlaylistItem(ctx, bob, pl.ID, entries[0].EntryID, 0); !errors.Is(err, ErrNotFound) {
		t.Errorf("move: %v", err)
	}

	// Removing it leaves the owner's alone, and accepting again takes the owner.
	if err := s.RemovePlaylistShare(ctx, bob, pl.ID); err != nil {
		t.Fatal(err)
	}
	if len(listed(bobAcc)) != 0 || invites(bobAcc) != 0 || len(listed(annAcc)) != 1 {
		t.Fatal("removing should only take it off bob's list")
	}
	if _, err := s.AcceptPlaylistShare(ctx, bobAcc, pl.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("got it back alone: %v", err)
	}
	if err := s.RemovePlaylistShare(ctx, ann, pl.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("the owner has no share to remove: %v", err)
	}
	if err := s.SetPlaylistShares(ctx, ann, pl.ID, []int64{bob}, false); err != nil {
		t.Fatal(err)
	}
	if invites(bobAcc) != 1 {
		t.Fatal("sharing again should offer it again")
	}

	// Leaving someone out takes it away.
	if _, err := s.AcceptPlaylistShare(ctx, bobAcc, pl.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPlaylistShares(ctx, ann, pl.ID, nil, false); err != nil {
		t.Fatal(err)
	}
	if len(listed(bobAcc)) != 0 {
		t.Fatal("unsharing should take it away")
	}

	// Everyone, including people who arrive later; removing it sticks until shared again.
	if err := s.SetPlaylistShares(ctx, ann, pl.ID, nil, true); err != nil {
		t.Fatal(err)
	}
	dee := Access{UserID: user("dee")}
	if invites(Access{UserID: cid}) != 1 || invites(dee) != 1 || invites(annAcc) != 0 {
		t.Fatal("should be offered to everyone but the owner")
	}
	if _, err := s.AcceptPlaylistShare(ctx, dee, pl.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.RemovePlaylistShare(ctx, cid, pl.ID); err != nil {
		t.Fatal(err)
	}
	if invites(Access{UserID: cid}) != 0 {
		t.Fatal("declining should stick")
	}
	if err := s.SetPlaylistShares(ctx, ann, pl.ID, nil, true); err != nil {
		t.Fatal(err)
	}
	if invites(Access{UserID: cid}) != 0 {
		t.Fatal("saving with everyone still on shouldn't offer it again")
	}
	if len(listed(dee)) != 1 {
		t.Fatal("saving with everyone still on shouldn't take it from those who accepted")
	}
	if err := s.SetPlaylistShares(ctx, ann, pl.ID, []int64{cid}, true); err != nil {
		t.Fatal(err)
	}
	if invites(Access{UserID: cid}) != 1 {
		t.Fatal("naming them should offer it again")
	}
	if err := s.RemovePlaylistShare(ctx, cid, pl.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPlaylistShares(ctx, ann, pl.ID, nil, false); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPlaylistShares(ctx, ann, pl.ID, nil, true); err != nil {
		t.Fatal(err)
	}
	if invites(Access{UserID: cid}) != 1 {
		t.Fatal("sharing with everyone anew should offer it again")
	}

	// Deleting the playlist ends every share.
	if err := s.DeletePlaylist(ctx, ann, pl.ID); err != nil {
		t.Fatal(err)
	}
	if len(listed(dee)) != 0 || invites(Access{UserID: cid}) != 0 {
		t.Fatal("a deleted playlist should go from everyone's")
	}
}
