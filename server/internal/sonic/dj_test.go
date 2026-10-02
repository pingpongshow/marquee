package sonic

import (
	"math/rand/v2"
	"testing"
)

func TestDJPick(t *testing.T) {
	x := NewIndex()
	add := func(id, artist, album int64, year int, v ...float32) *Track {
		tr := &Track{ItemID: id, ArtistID: artist, AlbumID: album, Year: year, LibraryID: 1, Vec: normalize(v)}
		x.Put(tr)
		return tr
	}
	now := add(1, 10, 100, 1994, 1, 0, 0)
	add(2, 10, 100, 1994, 1, 0.1, 0)    // same album
	add(3, 10, 101, 1997, 0.9, 0.2, 0)  // same artist, other album
	add(4, 20, 200, 1995, 0.95, 0.1, 0) // someone else, same era, close
	add(5, 30, 300, 2015, 0.97, 0, 0.1) // someone else, much later, closest
	add(6, 40, 400, 1993, 0, 0, 1)      // far away
	o := Options{Rand: rand.New(rand.NewPCG(1, 1))}
	plays := func(ids []int64) map[int64]int { return map[int64]int{2: 9, 3: 0} }

	if p := x.DJPick(now, DJGroupie, o, plays); p == nil || p.ItemID != 3 {
		t.Errorf("groupie picked %+v, want track 3", p)
	}
	if p := x.DJPick(now, DJStretch, o, plays); p == nil || p.ArtistID == 10 {
		t.Errorf("stretch picked %+v, want another artist", p)
	}
	if p := x.DJPick(now, DJContempo, o, plays); p == nil || p.ItemID != 4 {
		t.Errorf("contempo picked %+v, want track 4 (same era)", p)
	}
	if p := x.DJPick(now, DJDeepCuts, o, plays); p == nil || p.ItemID != 3 {
		t.Errorf("deep cuts picked %+v, want the unplayed track 3", p)
	}
	o.Exclude = map[int64]bool{3: true}
	if p := x.DJPick(now, DJGroupie, o, plays); p != nil {
		t.Errorf("groupie should find nothing once track 3 is queued, got %+v", p)
	}
}
