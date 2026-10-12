package muse

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"marquee/internal/items"
	"marquee/internal/origins"
)

var vocab = items.VideoVocab{Genres: genres, Keywords: map[string]bool{"heist": true, "time loop": true, "based on a true story": true}}

// The AI's new movie fields: origins, foreign language, keywords (library tags, else meaning
// text), a content rating limit and a critics' threshold; the seed's title leaves the meaning.
func TestReadVideoDetails(t *testing.T) {
	llm := &fakeLLM{answer: `{"types":["movie"],"yearFrom":0,"yearTo":0,"genres":["thriller"],"excludeGenres":[],"unwatched":false,
		"maxMinutes":0,"minMinutes":0,"highlyRated":false,"kidFriendly":false,"people":[],"like":[{"title":"Alien","year":0}],
		"origins":["Korean","Atlantis"],"foreignLanguage":true,"keywords":["Heists","based on true story","giant robots"],
		"maxContentRating":"PG-13","minCriticScore":91,"meaning":"films like alien","title":"Seoul Heists"}`}
	r := ReadVideo(context.Background(), llm, fakeLookup{}, items.Access{}, "korean heist thrillers like alien, PG-13 max, RT over 90", vocab)
	q := r.Query
	if !r.AI {
		t.Fatalf("not the AI reading: %+v", r)
	}
	if !reflect.DeepEqual(q.Origins, []origins.Origin{{Country: "KR", Language: "ko"}}) || q.Foreign {
		t.Errorf("origins %+v foreign %v", q.Origins, q.Foreign)
	}
	if !reflect.DeepEqual(q.Keywords, []string{"heist", "based on a true story"}) {
		t.Errorf("keywords %v", q.Keywords)
	}
	if q.MaxRating != "PG-13" || q.MinCritic != 91 {
		t.Errorf("limits %q %d", q.MaxRating, q.MinCritic)
	}
	// "films like alien" without the title is filler: only the unknown origin and keyword remain.
	if q.Rest != "Atlantis giant robots" || !reflect.DeepEqual(q.Seeds, []int64{7}) {
		t.Errorf("rest %q seeds %v", q.Rest, q.Seeds)
	}
	for _, want := range []string{"From South Korea", "Themes: heist, based on a true story", "PG-13 or under", "Critics 91%+", "Like Alien"} {
		if !strings.Contains(q.Understood(), want) {
			t.Errorf("understood %q lacks %q", q.Understood(), want)
		}
	}
}

// Without AI, "like <title>" finds the title in the library, and the title isn't meaning text.
func TestKeywordVideoSeeds(t *testing.T) {
	q := KeywordVideo(context.Background(), fakeLookup{}, items.Access{}, "movies like Alien", vocab)
	if !reflect.DeepEqual(q.Seeds, []int64{7}) || q.Rest != "" || q.Understood() != "Movies · Like Alien" {
		t.Errorf("like alien: %+v (%s)", q, q.Understood())
	}
	q = KeywordVideo(context.Background(), fakeLookup{}, items.Access{}, "something like alien but funnier", vocab)
	if !reflect.DeepEqual(q.Seeds, []int64{7}) || q.Rest != "but funnier" {
		t.Errorf("like alien but funnier: %+v", q)
	}
	q = KeywordVideo(context.Background(), fakeLookup{}, items.Access{}, "films like nothing on earth", vocab)
	if len(q.Seeds) != 0 || q.Rest == "" {
		t.Errorf("unknown title: %+v", q)
	}
}

var detailLib = MusicLibrary{Genres: musicLib.Genres, Artists: musicLib.Artists, Countries: []string{"IS", "GB", "SE"}, Areas: []string{"Manchester", "Reykjavík"}}

// The AI's new music fields, validated against the library.
func TestReadMusicDetails(t *testing.T) {
	llm := &fakeLLM{answer: `{"genres":[],"yearFrom":0,"yearTo":0,"artists":[],"likeArtists":[],"favourites":false,"unplayed":false,
		"recentlyAdded":false,"rediscover":false,"maxTrackMinutes":0,"totalMinutes":0,"sound":"","theme":"",
		"tempoMin":140,"tempoMax":110,"energy":"high","mode":"minor","key":"Eb","popularity":"deep cuts",
		"origins":["Icelandic","Manchester","Narnia"],"artistType":"person","gender":"female","activeFrom":1990,"activeTo":1999,
		"albumTypes":["live","bogus"],"excludeAlbumTypes":["compilation"],"describe":"experimental singer-songwriters","name":"Northern Lights"}`}
	r := ReadMusic(context.Background(), llm, detailLib, "fast intense deep cuts by icelandic female singer-songwriters")
	q := r.Query
	if !r.AI || q.TempoMin != 110 || q.TempoMax != 140 || q.Energy != "high" || q.Mode != "minor" || q.Key != "D#" || q.Popular != -1 {
		t.Fatalf("sound fields %+v", q)
	}
	if !reflect.DeepEqual(q.Countries, []string{"IS"}) || !reflect.DeepEqual(q.Areas, []string{"Manchester"}) || q.ArtistType != "person" || q.Gender != "female" {
		t.Fatalf("artist fields %+v", q)
	}
	if q.ActiveFrom != 1990 || q.ActiveTo != 1999 || !reflect.DeepEqual(q.AlbumTypes, []string{"live"}) || !reflect.DeepEqual(q.ExcludeAlbumTypes, []string{"compilation"}) {
		t.Fatalf("era and album fields %+v", q)
	}
	if q.Describe != "experimental singer-songwriters from Narnia" {
		t.Fatalf("describe %q", q.Describe)
	}
	want := []string{"110–140 BPM", "High energy", "D# minor", "Deep cuts", "From Iceland", "From Manchester", "Female solo artists", "Active 1990s", "Live albums", "No compilations"}
	if !reflect.DeepEqual(r.Understood, want) {
		t.Fatalf("understood %q", r.Understood)
	}
}

// Without AI: tempo and energy words, hits, stored countries and places, artist traits, album types.
func TestKeywordMusicDetails(t *testing.T) {
	cases := []struct {
		prompt string
		check  func(r MusicReading) bool
	}{
		{"fast songs", func(r MusicReading) bool { return r.Query.TempoMin == 120 && r.Query.TempoMax == 0 }},
		{"slow jazz", func(r MusicReading) bool { return r.Query.TempoMax == 95 }},
		{"around 120 bpm", func(r MusicReading) bool { return r.Query.TempoMin == 112 && r.Query.TempoMax == 128 }},
		{"100-110 bpm workout", func(r MusicReading) bool {
			return r.Query.TempoMin == 100 && r.Query.TempoMax == 110 && r.Query.Energy == "high"
		}},
		{"calm evening music", func(r MusicReading) bool { return r.Query.Energy == "low" }},
		{"happy songs", func(r MusicReading) bool { return r.Query.Mode == "major" && r.Query.Key == "" }},
		{"something in f sharp minor", func(r MusicReading) bool { return r.Query.Mode == "minor" && r.Query.Key == "F#" }},
		{"the beatles greatest hits", func(r MusicReading) bool { return r.Query.Popular == 1 }},
		{"radiohead deep cuts", func(r MusicReading) bool { return r.Query.Popular == -1 }},
		{"icelandic music", func(r MusicReading) bool { return reflect.DeepEqual(r.Query.Countries, []string{"IS"}) }},
		{"french pop", func(r MusicReading) bool { return len(r.Query.Countries) == 0 }}, // no French artists stored
		{"bands from manchester", func(r MusicReading) bool {
			return reflect.DeepEqual(r.Query.Areas, []string{"Manchester"}) && r.Query.ArtistType == "group"
		}},
		{"female singer-songwriters", func(r MusicReading) bool { return r.Query.Gender == "female" && r.Query.ArtistType == "person" }},
		{"live albums no compilations", func(r MusicReading) bool {
			return reflect.DeepEqual(r.Query.AlbumTypes, []string{"live"}) && reflect.DeepEqual(r.Query.ExcludeAlbumTypes, []string{"compilation"})
		}},
		{"90s rock", func(r MusicReading) bool {
			return r.Query.TempoMin == 0 && r.Query.Energy == "" && r.Query.Popular == 0
		}},
	}
	for _, c := range cases {
		if r := KeywordMusic(detailLib, c.prompt); !c.check(r) {
			t.Errorf("%q: %+v %q", c.prompt, r.Query, r.Understood)
		}
	}
}

func TestArtistAndAlbumDocs(t *testing.T) {
	a := ArtistDoc(ArtistFacts{Name: "Björk", Type: "person", Gender: "female", Country: "IS", Area: "Iceland", BeginArea: "Reykjavík", BeginYear: 1977,
		Genres: []string{"Electronic"}, Styles: []string{"electronic", "art pop", "experimental"}, Summary: "Björk is an Icelandic singer."})
	if a != "Björk. A female solo artist, from Reykjavík, Iceland, active since 1977. Genres: Electronic. Styles: art pop, experimental. Björk is an Icelandic singer." {
		t.Errorf("artist doc: %s", a)
	}
	if a := ArtistDoc(ArtistFacts{Name: "The Smiths", Type: "group", Country: "GB", BeginYear: 1982, EndYear: 1987}); a != "The Smiths. A band, from the United Kingdom, active 1982–1987." {
		t.Errorf("band doc: %s", a)
	}
	if a := ArtistDoc(ArtistFacts{Name: "Someone"}); a != "Someone." {
		t.Errorf("bare doc: %s", a)
	}
	al := AlbumDoc(AlbumFacts{Title: "Homogenic", Artist: "Björk", Year: 1997, Type: "album", Styles: []string{"trip hop"}, Label: "One Little Indian"})
	if al != "Homogenic by Björk (1997). A studio album. Styles: trip hop. Released on One Little Indian." {
		t.Errorf("album doc: %s", al)
	}
}
