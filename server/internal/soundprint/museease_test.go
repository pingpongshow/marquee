package soundprint

import (
	"context"
	"testing"
)

// "U2 & Coldplay anthems" came back empty: the AI added live albums, and neither artist has
// one. The guessed filters give way one at a time; the named artists stay.
func TestMuseQueryEasesGuessedFilters(t *testing.T) {
	s, _ := museLibrary(t)
	ctx := context.Background()
	q := MusicQuery{ArtistIDs: []int64{1001}, AlbumTypes: []string{"live"}, Energy: "high", YearFrom: 1990, YearTo: 1999}
	st, err := s.MuseQuery(ctx, 1, q, 5, Options{})
	if err != nil || len(st.IDs) != 5 || !contains(st.IDs, 1, 10) {
		t.Fatalf("by the rockers, live: %v %v", st.IDs, err)
	}
	// Live albums went first, then energy (no track here has one); that was enough, so the
	// years stayed.
	if st.Query.AlbumTypes != nil || st.Query.Energy != "" || st.Query.YearFrom != 1990 || len(st.Query.ArtistIDs) != 1 {
		t.Errorf("ran %+v", st.Query)
	}

	// Explicit history filters stay: no favourites means no list.
	st, err = s.MuseQuery(ctx, 1, MusicQuery{Favourites: true, AlbumTypes: []string{"live"}}, 5, Options{})
	if err != nil || len(st.IDs) != 0 {
		t.Fatalf("favourites: %v %v", st.IDs, err)
	}

	// A filter few tracks pass still leads: the one 1960 track opens a list filled from the
	// rest, rather than a list of one.
	st, _ = s.MuseQuery(ctx, 1, MusicQuery{YearFrom: 1960, YearTo: 1960}, 5, Options{})
	found := false
	for _, id := range st.IDs {
		found = found || id == 20
	}
	if len(st.IDs) != 5 || !found || st.Query.YearFrom != 0 {
		t.Errorf("1960: %v (ran %+v)", st.IDs, st.Query)
	}

	// Filters that find enough are untouched.
	st, _ = s.MuseQuery(ctx, 1, MusicQuery{YearFrom: 1990, YearTo: 1999}, 5, Options{})
	if !contains(st.IDs, 1, 10) || st.Query.YearFrom != 1990 {
		t.Errorf("90s: %v (ran %+v)", st.IDs, st.Query)
	}
}
