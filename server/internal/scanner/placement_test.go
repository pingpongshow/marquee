package scanner

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"marquee/internal/library"
	"marquee/internal/probe"
)

func (e *env) id(q string, args ...any) int64 {
	var n int64
	if err := e.db.QueryRow(q, args...).Scan(&n); err != nil {
		e.t.Fatalf("%s: %v", q, err)
	}
	return n
}

func TestPlaceTrack(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.touch("music/Weezer/Blue/Weezer|Blue|1|My Name Is Jonas.flac")
	e.touch("music/Weezer/Blue/Weezer|Blue|2|No One Else.flac")
	e.touch("music/Weezer/Odd/Weezr|Odd|3|Buddy Holly.flac")
	e.touch("music/Rivers/Solo/Rivers|Solo|1|Wanting.flac")
	l := e.lib("Music", library.Music, "music")
	e.scan(l)

	blue := e.id(`SELECT id FROM items WHERE type = 'album' AND title = 'Blue'`)
	buddy := e.id(`SELECT id FROM items WHERE type = 'track' AND title = 'Buddy Holly'`)
	mustExec := func(q string, args ...any) {
		if _, err := e.db.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	mustExec(`INSERT INTO users (id, username, display_name) VALUES (1, 'u', 'U')`)
	mustExec(`INSERT INTO user_item_state (user_id, item_id, play_count, rating) VALUES (1, ?, 7, 10)`, buddy)

	// To an album in the library: the misspelt artist and its album go, the plays stay.
	if err := PlaceTrack(ctx, e.db, buddy, Placement{AlbumID: blue}); err != nil {
		t.Fatal(err)
	}
	if n := e.id(`SELECT parent_id FROM items WHERE id = ?`, buddy); n != blue {
		t.Errorf("parent = %d, want %d", n, blue)
	}
	if n := e.count(`SELECT child_count FROM items WHERE id = ?`, blue); n != 3 {
		t.Errorf("Blue tracks = %d", n)
	}
	if n := e.count(`SELECT COUNT(*) FROM items WHERE title IN ('Weezr', 'Odd')`); n != 0 {
		t.Errorf("empty album and artist left: %d", n)
	}
	if n := e.count(`SELECT play_count FROM user_item_state WHERE item_id = ?`, buddy); n != 7 {
		t.Errorf("plays = %d", n)
	}
	if !HasPlacement(ctx, e.db, buddy) {
		t.Error("placement not recorded")
	}

	// It stays there through scans: unchanged, re-tagged (changed), moved and forgotten files.
	e.scan(l)
	p := filepath.Join(e.root, "music/Weezer/Odd/Weezr|Odd|3|Buddy Holly.flac")
	os.WriteFile(p, []byte("re-tagged, a little longer"), 0o644)
	e.scan(l)
	if n := e.id(`SELECT parent_id FROM items WHERE id = ?`, buddy); n != blue {
		t.Errorf("after rescans parent = %d", n)
	}
	os.MkdirAll(filepath.Join(e.root, "music/Weezer/Blue Extra"), 0o755)
	moved := filepath.Join(e.root, "music/Weezer/Blue Extra/Weezr|Odd|3|Buddy Holly.flac")
	if err := os.Rename(p, moved); err != nil {
		t.Fatal(err)
	}
	e.scan(l)
	if n := e.count(`SELECT COUNT(*) FROM track_placements WHERE path = ?`, moved); n != 1 {
		t.Error("placement didn't follow the file")
	}
	if n := e.id(`SELECT parent_id FROM items WHERE id = ?`, buddy); n != blue {
		t.Errorf("after the move parent = %d", n)
	}
	mustExec(`DELETE FROM media_files WHERE path = ?`, moved)
	e.scan(l)
	if n := e.count(`SELECT COUNT(*) FROM items i JOIN media_versions v ON v.item_id = i.id JOIN media_files f ON f.version_id = v.id
		WHERE f.path = ? AND i.parent_id = ?`, moved, blue); n != 1 {
		t.Error("a file found again isn't placed as chosen")
	}
	buddy = e.id(`SELECT v.item_id FROM media_versions v JOIN media_files f ON f.version_id = v.id WHERE f.path = ?`, moved)
	if n := e.count(`SELECT COUNT(*) FROM items WHERE title IN ('Weezr', 'Odd')`); n != 0 {
		t.Errorf("tags placed it: %d", n)
	}

	// To a new album by name, with a new album artist and track artist.
	wanting := e.id(`SELECT id FROM items WHERE type = 'track' AND title = 'Wanting'`)
	if err := PlaceTrack(ctx, e.db, wanting, Placement{AlbumArtist: "Rivers Cuomo", Album: "Alone", Artist: "Rivers Cuomo"}); err != nil {
		t.Fatal(err)
	}
	if n := e.count(`SELECT COUNT(*) FROM items t JOIN items a ON a.id = t.parent_id JOIN items ar ON ar.id = t.grandparent_id
		WHERE t.id = ? AND a.title = 'Alone' AND ar.title = 'Rivers Cuomo' AND ar.id = a.parent_id AND t.artist_credit = 'Rivers Cuomo'`, wanting); n != 1 {
		t.Error("not moved to the new album")
	}
	if n := e.count(`SELECT COUNT(*) FROM items WHERE title IN ('Rivers', 'Solo')`); n != 0 {
		t.Errorf("old album left: %d", n)
	}
	// Back to the tags, at once.
	if err := UnplaceTrack(ctx, e.db, wanting); err != nil {
		t.Fatal(err)
	}
	if n := e.count(`SELECT COUNT(*) FROM items t JOIN items a ON a.id = t.parent_id WHERE t.id = ? AND a.title = 'Solo' AND t.artist_credit = 'Rivers'`, wanting); n != 1 {
		t.Error("not back on its tagged album")
	}
	if n := e.count(`SELECT COUNT(*) FROM items WHERE title IN ('Alone', 'Rivers Cuomo')`); n != 0 {
		t.Errorf("emptied album left: %d", n)
	}
	if HasPlacement(ctx, e.db, wanting) {
		t.Error("placement kept")
	}
	e.scan(l)
	if n := e.count(`SELECT COUNT(*) FROM items t JOIN items a ON a.id = t.parent_id WHERE t.id = ? AND a.title = 'Solo'`, wanting); n != 1 {
		t.Error("rescan moved it")
	}

	// Only tracks, and only albums in the same library.
	if err := PlaceTrack(ctx, e.db, blue, Placement{AlbumID: blue}); err != ErrNotTrack {
		t.Errorf("album moved: %v", err)
	}
	if err := PlaceTrack(ctx, e.db, wanting, Placement{AlbumID: wanting}); err != ErrAlbumNotInLibrary {
		t.Errorf("into a track: %v", err)
	}
	if err := PlaceTrack(ctx, e.db, wanting, Placement{Album: "No artist"}); err != ErrNoAlbum {
		t.Errorf("no album artist: %v", err)
	}
}

// taggedProber adds MusicBrainz ids to the fake music tags.
type taggedProber struct{ fakeProber }

func (p taggedProber) Probe(ctx context.Context, path string) (*probe.Result, error) {
	r, err := p.fakeProber.Probe(ctx, path)
	if err == nil && r.Tags != nil {
		r.Tags["musicbrainz_albumartistid"] = "tagged-artist"
		r.Tags["musicbrainz_albumid"] = "tagged-release"
	}
	return r, err
}

// An admin's MusicBrainz match (music_matches) wins over the tags, also for an artist and
// album whose files went and came back.
func TestManualMusicMatchWinsOverTags(t *testing.T) {
	e := newEnv(t)
	e.sc.Prober = taggedProber{e.sc.Prober.(fakeProber)}
	e.touch("music/Weezer/Blue/Weezer|Blue|1|My Name Is Jonas.flac")
	l := e.lib("Music", library.Music, "music")
	e.scan(l)
	artist := e.id(`SELECT id FROM items WHERE type = 'artist'`)
	album := e.id(`SELECT id FROM items WHERE type = 'album'`)
	if n := e.count(`SELECT COUNT(*) FROM external_ids WHERE value IN ('tagged-artist', 'tagged-release')`); n != 2 {
		t.Fatalf("tag ids = %d", n)
	}
	// What Fix match stores.
	if _, err := e.db.Exec(`DELETE FROM external_ids WHERE item_id IN (?, ?)`, artist, album); err != nil {
		t.Fatal(err)
	}
	e.db.Exec(`INSERT INTO external_ids (item_id, provider, value) VALUES (?, 'musicbrainz', 'manual-artist'), (?, 'musicbrainz_releasegroup', 'manual-rg')`, artist, album)
	e.db.Exec(`INSERT INTO music_matches (library_id, kind, scan_key, mbid, matched_at) VALUES (?, 'artist', 'a:weezer', 'manual-artist', 'x'), (?, 'album', 'al:weezer:blue', 'manual-rg', 'x')`, l.ID, l.ID)

	// Rescans, a re-tagged file and a new file on the album leave the match alone.
	e.touch("music/Weezer/Blue/Weezer|Blue|2|No One Else.flac")
	os.WriteFile(filepath.Join(e.root, "music/Weezer/Blue/Weezer|Blue|1|My Name Is Jonas.flac"), []byte("re-tagged"), 0o644)
	e.scan(l)
	if n := e.count(`SELECT COUNT(*) FROM external_ids WHERE value LIKE 'tagged-%' AND item_id IN (?, ?)`, artist, album); n != 0 {
		t.Errorf("tags overwrote the match: %d", n)
	}
	// The files go and come back: the new artist and album get the manual match, not the tags.
	tx, _ := e.db.Begin()
	tx.Exec(`DELETE FROM media_files`)
	if err := Tidy(context.Background(), tx, l.ID); err != nil {
		t.Fatal(err)
	}
	tx.Commit()
	if n := e.count(`SELECT COUNT(*) FROM items`); n != 0 {
		t.Fatalf("items left: %d", n)
	}
	e.scan(l)
	if v := e.count(`SELECT COUNT(*) FROM external_ids x JOIN items i ON i.id = x.item_id WHERE i.type = 'artist' AND x.provider = 'musicbrainz' AND x.value = 'manual-artist'`); v != 1 {
		t.Error("recreated artist lost the manual match")
	}
	if v := e.count(`SELECT COUNT(*) FROM external_ids x JOIN items i ON i.id = x.item_id WHERE i.type = 'album' AND x.value = 'tagged-release'`); v != 0 {
		t.Error("recreated album took the tags' release")
	}
}
