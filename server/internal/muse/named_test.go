package muse

import (
	"context"
	"reflect"
	"testing"

	"marquee/internal/soundprint"
)

// "songs by Radiohead and The Beatles anthems": the AI's answer for the real "U2 and
// Coldplay" prompt, which came back empty. The artist traits it added (British bands,
// active 1960–2020) describe the named artists, so they're dropped; the rest stays.
func TestReadMusicNamedArtistsDropTraits(t *testing.T) {
	llm := &fakeLLM{answer: `{"genres":["Rock"],"yearFrom":1980,"yearTo":2020,"artists":["Radiohead","The Beatles"],"likeArtists":[],
		"favourites":false,"unplayed":false,"recentlyAdded":false,"rediscover":false,"maxTrackMinutes":0,"totalMinutes":0,
		"sound":"anthemic rock","theme":"","tempoMin":0,"tempoMax":0,"energy":"high","mode":"","key":"","popularity":"",
		"origins":["British"],"artistType":"group","gender":"male","activeFrom":1960,"activeTo":2020,"albumTypes":["live"],
		"excludeAlbumTypes":[],"describe":"British rock bands","name":"Anthems"}`}
	r := ReadMusic(context.Background(), llm, detailLib, "songs by Radiohead and The Beatles anthems")
	q := r.Query
	if !r.AI || !reflect.DeepEqual(q.ArtistIDs, []int64{1, 2}) {
		t.Fatalf("reading %+v", r)
	}
	if len(q.Countries)+len(q.Areas) > 0 || q.ArtistType != "" || q.Gender != "" || q.ActiveFrom != 0 || q.ActiveTo != 0 {
		t.Errorf("artist traits kept for named artists: %+v", q)
	}
	if q.Energy != "high" || !reflect.DeepEqual(q.AlbumTypes, []string{"live"}) || q.YearFrom != 1980 {
		t.Errorf("other filters should stay (Muse relaxes them only when they find too little): %+v", q)
	}
	want := []string{"Rock", "1980–2020", "By Radiohead", "By The Beatles", "High energy", "Live albums", "Sounds anthemic rock"}
	if !reflect.DeepEqual(r.Understood, want) {
		t.Errorf("understood %q", r.Understood)
	}

	// Without named artists the traits are the request ("British bands").
	r = KeywordMusic(detailLib, "bands from manchester")
	if r.Query.ArtistType != "group" || len(r.Query.Areas) != 1 {
		t.Errorf("traits without named artists: %+v", r.Query)
	}
	// And the keyword reading drops them too: "by radiohead" settles who.
	r = KeywordMusic(detailLib, "bands from manchester by radiohead")
	if r.Query.ArtistType != "" || len(r.Query.Areas) != 0 || !reflect.DeepEqual(r.Query.ArtistIDs, []int64{1}) {
		t.Errorf("keyword traits with a named artist: %+v", r.Query)
	}
}

// The description loses the labels of the filters Muse relaxed, and keeps the rest.
func TestEased(t *testing.T) {
	asked := soundprint.MusicQuery{Genres: []string{"Rock"}, YearFrom: 1980, YearTo: 1989, ArtistIDs: []int64{1}, Energy: "high", AlbumTypes: []string{"live"}}
	understood := []string{"Rock", "1980s", "By Radiohead", "High energy", "Live albums", "Sounds anthemic rock"}
	if got := Eased(understood, asked, asked); !reflect.DeepEqual(got, understood) {
		t.Errorf("nothing relaxed: %q", got)
	}
	ran := asked
	ran.AlbumTypes, ran.Energy, ran.YearFrom, ran.YearTo = nil, "", 0, 0
	want := []string{"Rock", "By Radiohead", "Sounds anthemic rock"}
	if got := Eased(understood, asked, ran); !reflect.DeepEqual(got, want) {
		t.Errorf("relaxed: %q, want %q", got, want)
	}
}
