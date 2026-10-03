package soundprint

import (
	"math/rand/v2"
	"testing"
)

// Three "genres" far apart in a 6-d space, 20 tracks each across 4 artists and albums.
func testIndex() *Index {
	x := NewIndex()
	r := rand.New(rand.NewPCG(1, 2))
	id := int64(1)
	for g := 0; g < 3; g++ {
		for i := 0; i < 20; i++ {
			v := make([]float32, 6)
			v[g*2] = 1
			v[g*2+1] = float32(r.Float64()*0.4 - 0.2)
			for j := range v {
				v[j] += float32(r.Float64() * 0.05)
			}
			x.Put(&Track{ItemID: id, ArtistID: int64(100 + g*10 + i%4), AlbumID: int64(1000 + g*10 + i%4), LibraryID: 1, Vec: normalize(v)})
			id++
		}
	}
	return x
}

func genre(t *Track) int { return int((t.ItemID - 1) / 20) }

func TestFlowStaysInGenreWithVariety(t *testing.T) {
	x := testIndex()
	seed := x.Get(5)
	got := x.Flow(seed.Vec, seed, Options{Length: 15, Rand: rand.New(rand.NewPCG(3, 4))})
	if len(got) != 15 || got[0] != seed {
		t.Fatalf("len %d first %v", len(got), got[0].ItemID)
	}
	seen := map[int64]bool{}
	for i, tr := range got {
		if genre(tr) != genre(seed) {
			t.Errorf("track %d (%d) left the genre", i, tr.ItemID)
		}
		if seen[tr.ItemID] {
			t.Errorf("repeat %d", tr.ItemID)
		}
		seen[tr.ItemID] = true
		if i > 0 && got[i-1].ArtistID == tr.ArtistID {
			t.Errorf("same artist back to back at %d", i)
		}
	}
}

func TestJourneyTravelsBetweenGenres(t *testing.T) {
	x := testIndex()
	from, to := x.Get(1), x.Get(45) // genre 0 → genre 2
	got := x.Journey(from, to, 8, Options{})
	if len(got) != 8 || got[0] != from || got[7] != to {
		t.Fatalf("ends wrong: %d tracks", len(got))
	}
	if genre(got[1]) != 0 || genre(got[6]) != 2 {
		t.Errorf("path should start in the first genre and end in the last: %d … %d", genre(got[1]), genre(got[6]))
	}
}

func TestPickIsDiverseAndRelevant(t *testing.T) {
	x := testIndex()
	q := normalize([]float32{0, 0, 1, 0, 0, 0}) // genre 1
	got := x.Pick(q, 8, Options{})
	artists := map[int64]int{}
	for _, tr := range got {
		if genre(tr) != 1 {
			t.Errorf("%d not in the asked-for genre", tr.ItemID)
		}
		artists[tr.ArtistID]++
	}
	if len(artists) < 4 {
		t.Errorf("expected all four artists, got %v", artists)
	}
}

func TestSimilarSkipsOwnAlbumAndClustersFindGenres(t *testing.T) {
	x := testIndex()
	tr := x.Get(1)
	for _, s := range x.Similar(tr, 5, nil) {
		if s.AlbumID == tr.AlbumID || genre(s) != 0 {
			t.Errorf("similar returned %d (album %d, genre %d)", s.ItemID, s.AlbumID, genre(s))
		}
	}
	all := x.Where(func(*Track) bool { return true })
	cs := Clusters(all, 3, rand.New(rand.NewPCG(5, 6)))
	found := map[int]bool{}
	for _, c := range cs {
		best := 0
		for i, v := range c {
			if v > c[best] {
				best = i
			}
		}
		found[best/2] = true
	}
	if len(found) != 3 {
		t.Errorf("clusters should find the three genres: %v", found)
	}
}
