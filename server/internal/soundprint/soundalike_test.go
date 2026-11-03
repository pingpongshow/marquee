package soundprint

import "testing"

func TestSoundAlikeKeepsCloseArtistsOnly(t *testing.T) {
	x := NewIndex()
	put := func(id, artist, lib int64, v ...float32) {
		x.Put(&Track{ItemID: id, ArtistID: artist, LibraryID: lib, Vec: normalize(v)})
	}
	put(1, 10, 1, 1, 0, 0)       // liked
	put(2, 20, 1, 0.95, 0.05, 0) // sounds like it
	put(3, 30, 1, 0, 1, 0)       // doesn't
	put(4, 40, 2, 0.95, 0.05, 0) // does, in another library
	s := &Service{Index: x}
	got := s.SoundAlike([]int64{10}, func(t *Track) bool { return t.LibraryID == 1 })
	if len(got) != 1 || got[20] != 10 {
		t.Errorf("got %v", got)
	}
}
