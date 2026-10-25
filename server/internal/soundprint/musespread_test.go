package soundprint

import (
	"context"
	"encoding/json"
	"math/rand/v2"
	"net/http"
	"net/http/httptest"
	"testing"
)

// latchIndex is the "coffeehouse" library that came back almost all U2: one artist (1) has
// a 40-track acoustic album (10) and a 10-track one (11) sitting right on the prompt's
// sound, twenty other artists (100–119) have two tracks each just behind it, and ten tracks
// (artist 200) sound nothing like it. Sound axes: 0 the prompt, 1 the spread around it, 2
// elsewhere.
func latchIndex() *Index {
	x := NewIndex()
	r := rand.New(rand.NewPCG(5, 6))
	id := int64(1)
	add := func(artist, album int64, v []float32) {
		v[3] = float32(r.Float64() * 0.02)
		x.Put(&Track{ItemID: id, ArtistID: artist, AlbumID: album, LibraryID: 1, Vec: normalize(v)})
		id++
	}
	for i := 0; i < 50; i++ {
		album := int64(10)
		if i >= 40 {
			album = 11
		}
		add(1, album, []float32{1, 0.1 + float32(r.Float64()*0.05), 0, 0})
	}
	for a := int64(100); a < 120; a++ {
		for i := 0; i < 2; i++ {
			add(a, a*10+int64(i), []float32{1, 0.3 + float32(r.Float64()*0.1), 0, 0})
		}
	}
	for i := 0; i < 10; i++ {
		add(200, 2000, []float32{0, 0, 1, 0})
	}
	return x
}

func perArtist(ts []*Track) map[int64]int {
	out := map[int64]int{}
	for _, t := range ts {
		out[t.ArtistID]++
	}
	return out
}

func TestPickWithoutCapsLatchesOntoOneArtist(t *testing.T) {
	// The old behaviour, still what Pick does without caps (mixes): the artist owning the
	// closest tracks fills the list, because every candidate sounds about as alike to what's
	// chosen as another track by the same artist does.
	got := latchIndex().Pick([]float32{1, 0, 0, 0}, 30, Options{})
	if n := perArtist(got)[1]; n < 20 {
		t.Fatalf("expected the uncapped pick to latch onto artist 1, got %d of 30", n)
	}
}

func TestPickCapsArtistsAndAlbums(t *testing.T) {
	x := latchIndex()
	q := MusicQuery{Sound: "coffeehouse"}
	a, b := q.spread(30)
	if a != 3 || b != 2 {
		t.Fatalf("spread(30) = %d, %d", a, b)
	}
	got := x.Pick([]float32{1, 0, 0, 0}, 30, Options{PerArtist: a, PerAlbum: b})
	if len(got) != 30 {
		t.Fatalf("got %d tracks", len(got))
	}
	albums := map[int64]int{}
	for i, tr := range got {
		if tr.ArtistID == 200 {
			t.Errorf("%d doesn't sound like the prompt", tr.ItemID)
		}
		albums[tr.AlbumID]++
		if i > 0 && got[i-1].ArtistID == tr.ArtistID {
			t.Errorf("artist %d twice in a row at %d", tr.ArtistID, i)
		}
	}
	artists := perArtist(got)
	if artists[1] > 3 || len(artists) < 11 {
		t.Errorf("artist 1 has %d tracks across %d artists: %v", artists[1], len(artists), artists)
	}
	for id, n := range albums {
		if n > 2 {
			t.Errorf("album %d has %d tracks", id, n)
		}
	}
	// The strongest match still plays first.
	if got[0].ArtistID != 1 {
		t.Errorf("first track is by %d, want the closest artist", got[0].ArtistID)
	}
}

func TestPickRaisesCapsToFillTheList(t *testing.T) {
	// Only artist 1 passes: the caps give way rather than cut the list short,
	// and the album cap goes first so it isn't all one album.
	x := latchIndex()
	keep := func(t *Track) bool { return t.ArtistID == 1 }
	got := x.Pick([]float32{1, 0, 0, 0}, 20, Options{Keep: keep, PerArtist: 2, PerAlbum: 1})
	if len(got) != 20 {
		t.Fatalf("got %d tracks, want 20", len(got))
	}
	albums := map[int64]int{}
	for _, tr := range got {
		albums[tr.AlbumID]++
	}
	if albums[11] < 8 { // 4 when both caps were raised together
		t.Errorf("want the short album's tracks in too: %v", albums)
	}
}

func TestMuseQuerySpreadsArtistsUnlessNamed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Texts []string }
		json.NewDecoder(r.Body).Decode(&in)
		out := make([][]float32, len(in.Texts))
		for i := range out {
			out[i] = []float32{1, 0, 0, 0}
		}
		json.NewEncoder(w).Encode(map[string]any{"model": "clap", "embeddings": out})
	}))
	defer srv.Close()
	s := &Service{Index: latchIndex(), Client: &Client{BaseURL: srv.URL}}
	ctx := context.Background()

	// "coffeehouse": no more than three by one artist.
	st, err := s.MuseQuery(ctx, 1, MusicQuery{Sound: "coffeehouse"}, 30, Options{})
	if err != nil || len(st.IDs) != 30 {
		t.Fatalf("coffeehouse: %v %v", st.IDs, err)
	}
	var ts []*Track
	for _, id := range st.IDs {
		ts = append(ts, s.Index.Get(id))
	}
	if artists := perArtist(ts); artists[1] > 3 || len(artists) < 11 {
		t.Errorf("coffeehouse: %v", artists)
	}

	// "coffeehouse songs by artist 1": all theirs, from both albums.
	st, err = s.MuseQuery(ctx, 1, MusicQuery{Sound: "coffeehouse", ArtistIDs: []int64{1}}, 30, Options{})
	if err != nil || len(st.IDs) != 30 {
		t.Fatalf("by artist 1: %v %v", st.IDs, err)
	}
	albums := map[int64]bool{}
	for _, id := range st.IDs {
		tr := s.Index.Get(id)
		if tr.ArtistID != 1 {
			t.Fatalf("by artist 1 has a track by %d", tr.ArtistID)
		}
		albums[tr.AlbumID] = true
	}
	if !albums[10] || !albums[11] {
		t.Errorf("by artist 1 should spread across both albums: %v", albums)
	}
}

func TestMuseSpread(t *testing.T) {
	for _, c := range []struct {
		q                   MusicQuery
		n, artist, perAlbum int
	}{
		{MusicQuery{}, 5, 2, 1},
		{MusicQuery{}, 30, 3, 2},
		{MusicQuery{}, 100, 10, 5},
		{MusicQuery{ArtistIDs: []int64{1}}, 30, 30, 4},    // one named artist: no artist cap
		{MusicQuery{ArtistIDs: []int64{1, 2}}, 30, 15, 4}, // two share the list
		{MusicQuery{LikeArtistIDs: []int64{1}}, 30, 3, 2}, // "like" an artist still spreads
	} {
		if a, b := c.q.spread(c.n); a != c.artist || b != c.perAlbum {
			t.Errorf("%+v n=%d: got %d, %d want %d, %d", c.q, c.n, a, b, c.artist, c.perAlbum)
		}
	}
}

func TestOrderApartKeepsArtistsApart(t *testing.T) {
	// Five tracks by artist 1 (alike) and four by others: the artist alternates instead of
	// playing five in a row, and the closest track still opens.
	var ts []*Track
	for i := 0; i < 5; i++ {
		ts = append(ts, &Track{ItemID: int64(i + 1), ArtistID: 1, Vec: normalize([]float32{1, 0.01 * float32(i), 0})})
	}
	for i := 0; i < 4; i++ {
		ts = append(ts, &Track{ItemID: int64(i + 10), ArtistID: int64(i + 2), Vec: normalize([]float32{1, 0.5, 0.1 * float32(i)})})
	}
	plain := order(ts, []float32{1, 0, 0})
	if plain[1].ArtistID != 1 {
		t.Fatalf("plain order should chain artist 1's alike tracks: %v", TrackIDs(plain))
	}
	got := orderApart(ts, []float32{1, 0, 0})
	if len(got) != 9 || got[0].ItemID != 1 {
		t.Fatalf("order %v", TrackIDs(got))
	}
	for i := 1; i < len(got); i++ {
		if got[i].ArtistID == got[i-1].ArtistID {
			t.Errorf("artist %d twice in a row at %d: %v", got[i].ArtistID, i, TrackIDs(got))
		}
	}
}
