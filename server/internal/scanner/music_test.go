package scanner

import (
	"reflect"
	"testing"
)

func TestSplitArtists(t *testing.T) {
	tests := map[string][]string{
		"&ME,Rampa,Adam Port,Cubicolor":    {"&ME", "Rampa", "Adam Port", "Cubicolor"},
		"AC/DC":                            {"AC/DC"},
		"Simon & Garfunkel":                {"Simon & Garfunkel"},
		"Tyler, The Creator":               {"Tyler, The Creator"},
		"Tyler, The Creator,Kali Uchis":    {"Tyler, The Creator", "Kali Uchis"},
		"2Pac; Dr. Dre":                    {"2Pac", "Dr. Dre"},
		"Earth, Wind & Fire, The Emotions": {"Earth, Wind & Fire", "The Emotions"},
		"Weezer":                           {"Weezer"},
		"":                                 nil,
	}
	for in, want := range tests {
		if got := splitArtists(in); !reflect.DeepEqual(got, want) {
			t.Errorf("splitArtists(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestReadTrackTagsFallback(t *testing.T) {
	got := readTrackTags(map[string]string{}, "Weezer/Blue Album/03 - Undone.flac")
	if got.AlbumArtist != "Weezer" || got.Album != "Blue Album" || got.Title != "Undone" || got.Track != 3 {
		t.Errorf("fallback: %+v", got)
	}
	got = readTrackTags(map[string]string{"artist": "A", "album": "B", "title": "C", "track": "4/12", "disc": "2/2", "date": "1994-05-10"}, "x/y/z.flac")
	if got.AlbumArtist != "A" || got.Track != 4 || got.Disc != 2 || got.Year != 1994 {
		t.Errorf("tags: %+v", got)
	}
}

func TestFindSubtitles(t *testing.T) {
	files := []string{"Dog of Flanders S01E01.mkv", "Dog of Flanders S01E01.en.dub.srt", "Dog of Flanders S01E01.srt",
		"Dog of Flanders S01E01.es.forced.ass", "Dog of Flanders S01E010.en.srt", "Other.srt", "Movie.idx", "Movie.sub"}
	subs := findSubtitles("/m/Dog of Flanders S01E01.mkv", files)
	if len(subs) != 3 {
		t.Fatalf("got %d subs: %+v", len(subs), subs)
	}
	if subs[0].Language != "eng" || subs[0].Title != "dub" || subs[0].Codec != "subrip" {
		t.Errorf("sub0: %+v", subs[0])
	}
	if subs[2].Language != "spa" || !subs[2].Forced || subs[2].Codec != "ass" {
		t.Errorf("sub2: %+v", subs[2])
	}
	if vob := findSubtitles("/m/Movie.avi", files); len(vob) != 1 || vob[0].Codec != "dvd_subtitle" {
		t.Errorf("vobsub: %+v", vob)
	}
}
