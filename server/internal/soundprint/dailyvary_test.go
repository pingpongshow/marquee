package soundprint

import (
	"context"
	"math/rand/v2"
	"path/filepath"
	"testing"
	"time"

	"marquee/internal/db"
)

// Daily mixes change from day to day (they used to come out nearly identical, since only
// the clustering used the day's seed) while each still keeps to one taste.
func TestDailyMixesChangeEachDay(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	d, err := db.Open(ctx, filepath.Join(dir, "t.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	d.Exec(`INSERT INTO libraries (id, name, type) VALUES (1, 'Music', 'music')`)
	d.Exec(`INSERT INTO users (id, username, display_name) VALUES (77, 'u', 'U')`)
	r := rand.New(rand.NewPCG(3, 4))
	const dims, genres, perGenre = 24, 4, 150
	bases := make([][]float32, genres)
	for g := range bases {
		bases[g] = make([]float32, dims)
		for i := range bases[g] {
			bases[g][i] = float32(r.NormFloat64())
		}
	}
	genre := map[int64]int{}
	ix := NewIndex()
	id := int64(0)
	for g := 0; g < genres; g++ {
		for n := 0; n < perGenre; n++ {
			id++
			v := make([]float32, dims)
			for i := range v {
				v[i] = bases[g][i] + float32(r.NormFloat64())*0.45
			}
			artist := 10000 + id%60
			d.Exec(`INSERT INTO items (id, library_id, type, title, sort_title) VALUES (?, 1, 'artist', 'a', 'a') ON CONFLICT DO NOTHING`, artist)
			d.Exec(`INSERT INTO items (id, library_id, type, grandparent_id, title, sort_title, added_at) VALUES (?, 1, 'track', ?, 't', 't', '2020-01-01T00:00:00Z')`, id, artist)
			ix.Put(&Track{ItemID: id, ArtistID: artist, AlbumID: id % 90, LibraryID: 1, Year: 2000, Vec: normalize(v)})
			genre[id] = g
		}
	}
	// Loved: 30 tracks each from genres 0 and 1, played long ago.
	for g := 0; g < 2; g++ {
		for n := 1; n <= 30; n++ {
			d.Exec(`INSERT INTO user_item_state (user_id, item_id, play_count, rating, last_viewed_at) VALUES (77, ?, 5, 10, '2024-01-01T00:00:00Z')`, int64(g*perGenre+n))
		}
	}
	s := &Service{DB: d, Index: ix}
	daily := func(day time.Time) map[string][]int64 {
		out := map[string][]int64{}
		for _, m := range s.dailyMixes(ctx, 77, "vary-test", nil, day) {
			if len(m.ID) > 6 && m.ID[:6] == "daily-" {
				out[m.ID] = m.IDs
			}
		}
		return out
	}
	day1 := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	a, again, b := daily(day1), daily(day1), daily(day1.AddDate(0, 0, 1))
	if len(a) == 0 || len(b) == 0 {
		t.Fatalf("no daily mixes: %d %d", len(a), len(b))
	}
	all := func(m map[string][]int64) map[int64]bool {
		set := map[int64]bool{}
		for _, ids := range m {
			for _, id := range ids {
				set[id] = true
			}
		}
		return set
	}
	if len(all(a)) != len(all(again)) {
		t.Errorf("mixes changed within the day")
	}
	sa, sb := all(a), all(b)
	shared := 0
	for id := range sb {
		if sa[id] {
			shared++
		}
	}
	t.Logf("overlap %d of %d", shared, len(sb))
	if overlap := float64(shared) / float64(len(sb)); overlap > 0.4 {
		t.Errorf("next day's mixes repeat %.0f%% of today's tracks (%d of %d)", overlap*100, shared, len(sb))
	}
	// Each mix keeps to one taste: most tracks from a loved genre, mostly a single one.
	for name, ids := range b {
		count := map[int]int{}
		for _, id := range ids {
			count[genre[id]]++
		}
		top := 0
		for g, c := range count {
			if c > top && g < 2 {
				top = c
			}
		}
		if float64(top) < 0.8*float64(len(ids)) {
			t.Errorf("%s strays from its taste: %v", name, count)
		}
	}
}
