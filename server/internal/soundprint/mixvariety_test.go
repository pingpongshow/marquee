package soundprint

import (
	"context"
	"fmt"
	"math/rand/v2"
	"path/filepath"
	"testing"

	"marquee/internal/db"
)

// A library where six artists with deep catalogues sit right on the listener's taste and
// sixty others a little further off: every mix still spans a dozen or more artists, with at
// most two tracks of one artist and never one artist twice in a row. Mixes used to fill up
// with the six (the tracks nearest a taste are a few artists' albums), so they read like
// album playlists.
func TestMixesSpreadAcrossArtists(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	d, err := db.Open(ctx, filepath.Join(dir, "t.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	d.Exec(`INSERT INTO libraries (id, name, type) VALUES (1, 'Music', 'music')`)
	d.Exec(`INSERT INTO users (id, username, display_name) VALUES (6, 'u', 'U')`)
	r := rand.New(rand.NewPCG(11, 12))
	taste := make([]float32, 16)
	for i := range taste {
		taste[i] = float32(r.NormFloat64())
	}
	ix := NewIndex()
	id := int64(1)
	var liked []int64
	add := func(artist int64, tracks int, spread float64, likeEvery int) {
		d.Exec(`INSERT INTO items (id, library_id, type, title, sort_title) VALUES (?, 1, 'artist', 'a', 'a')`, artist)
		for n := 0; n < tracks; n++ {
			album := artist*10 + int64(n%2)
			d.Exec(`INSERT INTO items (id, library_id, type, title, sort_title) VALUES (?, 1, 'album', 'b', 'b') ON CONFLICT DO NOTHING`, album)
			year, added := 1995, "2020-01-01T00:00:00Z"
			if n == tracks-1 {
				year, added = 2024, "2099-01-01T00:00:00Z" // always "recently added"
			}
			title := fmt.Sprintf("Song %d", id)
			d.Exec(`INSERT INTO items (id, library_id, type, grandparent_id, parent_id, title, sort_title, year, added_at)
				VALUES (?, 1, 'track', ?, ?, ?, ?, ?, ?)`, id, artist, album, title, title, year, added)
			v := make([]float32, len(taste))
			for i := range v {
				v[i] = taste[i] + float32(r.NormFloat64()*spread)
			}
			ix.Put(&Track{ItemID: id, ArtistID: artist, AlbumID: album, LibraryID: 1, Year: year,
				Vec: normalize(v), Copy: CopyKey(title, artist)})
			if likeEvery > 0 && n%likeEvery == 0 {
				liked = append(liked, id)
			}
			id++
		}
	}
	for a := int64(0); a < 6; a++ {
		add(2000+a, 30, 0.15, 4) // the deep catalogues the listener loves most
	}
	for a := int64(0); a < 60; a++ {
		add(3000+a, 5, 0.35, 5) // one loved song each
	}
	for i, item := range liked {
		last := "strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '-2 days')"
		if i%2 == 0 {
			last = "'2023-01-01T00:00:00Z'"
		}
		d.Exec(`INSERT INTO user_item_state (user_id, item_id, play_count, rating, last_viewed_at) VALUES (6, ?, 4, 10, `+last+`)`, item)
	}
	// One of the six is a favourite artist, which lifts their whole catalogue.
	d.Exec(`INSERT INTO user_item_state (user_id, item_id, favorite_at) VALUES (6, 2000, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))`)

	s := &Service{DB: d, Index: ix}
	mixes := s.DailyMixes(ctx, 6, "", nil)
	ids := map[string]bool{}
	for _, m := range mixes {
		ids[m.ID] = true
	}
	for _, want := range []string{"daily-1", "discovery", "rediscover", "decade"} {
		if !ids[want] {
			t.Errorf("missing %s", want)
		}
	}
	for _, m := range mixes {
		if len(m.IDs) < 10 {
			t.Errorf("%s has only %d tracks", m.ID, len(m.IDs))
		}
		byArtist := map[int64]int{}
		var prev int64
		for i, item := range m.IDs {
			a := ix.Get(item).ArtistID
			byArtist[a]++
			if i > 0 && a == prev {
				t.Errorf("%s plays artist %d twice in a row (at %d)", m.ID, a, i)
			}
			prev = a
		}
		for a, n := range byArtist {
			if n > mixPerArtist {
				t.Errorf("%s has %d tracks by artist %d (%d artists in %d tracks)", m.ID, n, a, len(byArtist), len(m.IDs))
			}
		}
	}

	// A station started from one of the six plays it a few times, not throughout.
	first := ix.Get(1)
	st := ix.Flow(first.Vec, first, Options{Length: 50, Rand: rand.New(rand.NewPCG(1, 2))})
	byArtist := map[int64]int{}
	for _, tr := range st {
		byArtist[tr.ArtistID]++
	}
	if len(st) != 50 || byArtist[first.ArtistID] > 5 {
		t.Errorf("radio: %d tracks, %d by the seed's artist", len(st), byArtist[first.ArtistID])
	}
}

// spreadOut keeps an artist's share to the cap while others are left, then lets more in.
func TestSpreadOut(t *testing.T) {
	var ts []*Track
	for i, a := range []int64{1, 1, 1, 2, 1, 3} {
		ts = append(ts, &Track{ItemID: int64(i + 1), ArtistID: a})
	}
	if got := TrackIDs(spreadOut(ts, 4, 2)); fmt.Sprint(got) != "[1 2 4 6]" {
		t.Errorf("spreadOut(4) = %v", got)
	}
	if got := TrackIDs(spreadOut(ts, 6, 2)); fmt.Sprint(got) != "[1 2 4 6 3 5]" {
		t.Errorf("spreadOut(6) = %v", got)
	}
}
