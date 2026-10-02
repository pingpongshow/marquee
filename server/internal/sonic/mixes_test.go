package sonic

import (
	"context"
	"math/rand/v2"
	"path/filepath"
	"testing"

	"marquee/internal/db"
)

// TestHistoryMixes builds a small library where the listener loves 1990s tracks, some of
// them last played a year ago, and recent additions exist.
func TestHistoryMixes(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	d, err := db.Open(ctx, filepath.Join(dir, "t.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	d.Exec(`INSERT INTO libraries (id, name, type) VALUES (1, 'Music', 'music')`)
	d.Exec(`INSERT INTO users (id, username, display_name) VALUES (1, 'u', 'U')`)
	r := rand.New(rand.NewPCG(1, 2))
	ix := NewIndex()
	vec := func(base float32) []float32 {
		v := make([]float32, 16)
		for i := range v {
			v[i] = base + float32(r.Float64())*0.3
		}
		return normalize(v)
	}
	for i := 1; i <= 120; i++ {
		year, added := 1990+i%10, "2020-01-01T00:00:00Z"
		if i > 100 {
			year, added = 2023, "2099-01-01T00:00:00Z" // recently added (in the future, so always "recent")
		}
		artist := int64(1000 + i%12)
		d.Exec(`INSERT INTO items (id, library_id, type, title, sort_title, added_at) VALUES (?, 1, 'artist', 'a', 'a', ?) ON CONFLICT DO NOTHING`, artist, added)
		if _, err := d.Exec(`INSERT INTO items (id, library_id, type, grandparent_id, title, sort_title, added_at) VALUES (?, 1, 'track', ?, 't', 't', ?)`, i, artist, added); err != nil {
			t.Fatal(err)
		}
		ix.Put(&Track{ItemID: int64(i), ArtistID: artist, AlbumID: int64(i % 30), LibraryID: 1, Year: year, Vec: vec(1)})
	}
	// Played: 1–40 (liked), of which 1–15 last a year ago and rated 5 stars.
	for i := 1; i <= 40; i++ {
		last := "strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '-2 days')"
		if i <= 15 {
			last = "'2024-01-01T00:00:00Z'"
		}
		d.Exec(`INSERT INTO user_item_state (user_id, item_id, play_count, rating, last_viewed_at) VALUES (1, ?, 4, 10, `+last+`)`, i)
	}
	s := &Service{DB: d, Index: ix}
	mixes := s.DailyMixes(ctx, 1, "", nil)
	byID := map[string]Mix{}
	for _, m := range mixes {
		byID[m.ID] = m
	}
	for _, id := range []string{"daily-1", "discovery", "rediscover", "new", "decade"} {
		if _, ok := byID[id]; !ok {
			t.Errorf("missing %s; got %v", id, len(mixes))
		}
	}
	for _, id := range byID["discovery"].IDs {
		if id <= 40 {
			t.Errorf("discovery has played track %d", id)
		}
	}
	for _, id := range byID["rediscover"].IDs {
		if id > 15 {
			t.Errorf("rediscover has recently played track %d", id)
		}
	}
	for _, id := range byID["new"].IDs {
		if id <= 100 {
			t.Errorf("new music has old track %d", id)
		}
	}
	if m := byID["decade"]; m.Title != "90s Mix" {
		t.Errorf("decade: %q", m.Title)
	}
}
