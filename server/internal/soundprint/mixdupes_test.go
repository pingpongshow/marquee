package soundprint

import (
	"context"
	"fmt"
	"math/rand/v2"
	"path/filepath"
	"testing"

	"marquee/internal/db"
)

// A library where every song sits in it twice (the album and a compilation): no mix of the
// day offers the same song twice, and no two mixes of the day offer the same track
// (MUSIC-17). Mixes used to repeat a song whenever a second file of it existed, since they
// only kept item ids apart, and only the four Daily Mixes avoided each other's tracks.
func TestMixesNeverRepeatATrack(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	d, err := db.Open(ctx, filepath.Join(dir, "t.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	d.Exec(`INSERT INTO libraries (id, name, type) VALUES (1, 'Music', 'music')`)
	d.Exec(`INSERT INTO users (id, username, display_name) VALUES (5, 'u', 'U')`)
	r := rand.New(rand.NewPCG(9, 10))
	// The two albums every song has a copy on.
	for c := 0; c < 2; c++ {
		d.Exec(`INSERT INTO items (id, library_id, type, title, sort_title) VALUES (?, 1, 'album', ?, ?)`,
			500000+c, fmt.Sprintf("Album %d", c), fmt.Sprintf("album %d", c))
	}

	// 500 songs, each as two tracks: the same title and artist, different albums, all but
	// identical in sound. Ids 2n-1 and 2n are the two copies of song n.
	const songs = 500
	ix := NewIndex()
	song := map[int64]int{}
	for n := 1; n <= songs; n++ {
		title, artist := fmt.Sprintf("Song %d", n), int64(1000+n%14)
		base := make([]float32, 16)
		for i := range base {
			base[i] = float32(r.NormFloat64())
		}
		year, added := 1995, "2020-01-01T00:00:00Z"
		if n > 400 {
			year, added = 2024, "2099-01-01T00:00:00Z" // always "recently added"
		}
		d.Exec(`INSERT INTO items (id, library_id, type, title, sort_title) VALUES (?, 1, 'artist', 'a', 'a') ON CONFLICT DO NOTHING`, artist)
		for c := 0; c < 2; c++ {
			id := int64(2*n - 1 + c)
			d.Exec(`INSERT INTO items (id, library_id, type, grandparent_id, parent_id, title, sort_title, year, added_at)
				VALUES (?, 1, 'track', ?, ?, ?, ?, ?, ?)`, id, artist, 500000+c, title, title, year, added)
			v := make([]float32, len(base))
			for i := range v {
				v[i] = base[i] + float32(r.NormFloat64())*0.01
			}
			ix.Put(&Track{ItemID: id, ArtistID: artist, AlbumID: int64(500000 + c), LibraryID: 1, Year: year,
				Vec: normalize(v), Copy: CopyKey(title, artist)})
			song[id] = n
		}
	}
	// Loved: the first copy of songs 1–300, half of them last played long enough ago to
	// rediscover.
	for n := 1; n <= 300; n++ {
		last := "strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '-2 days')"
		if n%2 == 1 {
			last = "'2023-01-01T00:00:00Z'"
		}
		d.Exec(`INSERT INTO user_item_state (user_id, item_id, play_count, rating, last_viewed_at) VALUES (5, ?, 4, 10, `+last+`)`, int64(2*n-1))
	}

	s := &Service{DB: d, Index: ix}
	mixes := s.DailyMixes(ctx, 5, "", nil)
	if len(mixes) < 4 {
		t.Fatalf("expected the daily and history mixes, got %d", len(mixes))
	}
	seenTrack := map[int64]string{}
	seenSong := map[int]string{}
	for _, m := range mixes {
		if len(m.IDs) == 0 {
			t.Errorf("%s is empty", m.ID)
		}
		inMix := map[int]bool{}
		for _, id := range m.IDs {
			if inMix[song[id]] {
				t.Errorf("%s has song %d twice (track %d)", m.ID, song[id], id)
			}
			inMix[song[id]] = true
			if other, ok := seenTrack[id]; ok {
				t.Errorf("track %d is in both %s and %s", id, other, m.ID)
			}
			if other, ok := seenSong[song[id]]; ok && other != m.ID {
				t.Errorf("song %d is in both %s and %s", song[id], other, m.ID)
			}
			seenTrack[id], seenSong[song[id]] = m.ID, m.ID
		}
	}
}

// CopyKey tells the copies of one recording from other songs.
func TestCopyKey(t *testing.T) {
	same := CopyKey("Paranoid Android", 7)
	for _, title := range []string{"paranoid android", "Paranoid  Android!", "Paranoid-Android"} {
		if got := CopyKey(title, 7); got != same {
			t.Errorf("CopyKey(%q) = %q, want %q", title, got, same)
		}
	}
	for _, c := range []struct {
		title  string
		artist int64
	}{{"Paranoid Android (Live)", 7}, {"Paranoid Android", 8}, {"Android", 7}} {
		if got := CopyKey(c.title, c.artist); got == same {
			t.Errorf("CopyKey(%q, %d) counts as the same recording", c.title, c.artist)
		}
	}
	if CopyKey("", 7) != "" || CopyKey("Song", 0) != "" {
		t.Error("a missing title or artist has no key")
	}
}
