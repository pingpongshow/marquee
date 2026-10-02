package naming

import (
	"reflect"
	"testing"
)

// Cases are taken from the production library (docs/07-deployment-environment.md).
func TestParseMovie(t *testing.T) {
	tests := []struct {
		folder, file string
		want         Movie
	}{
		{"A Quiet Place (2018)", "A Quiet Place (2018) Bluray-1080p.mkv", Movie{Title: "A Quiet Place", Year: 2018}},
		{"(500) Days of Summer (2009)", "(500) Days of Summer (2009).mkv", Movie{Title: "(500) Days of Summer", Year: 2009}},
		{"#Alive (2020)", "#Alive (2020) WEBDL-1080p.mkv", Movie{Title: "#Alive", Year: 2020}},
		{"-batteries not included (1987)", "x.mkv", Movie{Title: "-batteries not included", Year: 1987}},
		{"Ace Ventura", "Ace Ventura.mkv", Movie{Title: "Ace Ventura"}},
		{"A Quiet Place Part III ()", "A Quiet Place Part III.mkv", Movie{Title: "A Quiet Place Part III"}},
		{"Big Trouble In Little China 1986", "Big Trouble In Little China 1986.mkv", Movie{Title: "Big Trouble In Little China", Year: 1986}},
		{"1955.Godzilla.Gigantis.The.Fire.Monster.DVDRip.Xvid-P2P", "godzilla.avi", Movie{Title: "Godzilla Gigantis The Fire Monster", Year: 1955}},
		{"War.Of.The.Worlds.Revival.[2025].1080p.WEBRip.5.1-LAMA", "x.mp4", Movie{Title: "War Of The Worlds Revival", Year: 2025}},
		{"Blade Runner 2049 (2017)", "Blade Runner 2049 (2017).mkv", Movie{Title: "Blade Runner 2049", Year: 2017}},
		{"1917 (2019)", "1917 (2019).mkv", Movie{Title: "1917", Year: 2019}},
		{"The Matrix (1999) {tmdb-603}", "The Matrix (1999) {edition-Director's Cut}.mkv", Movie{Title: "The Matrix", Year: 1999, Edition: "Director's Cut", IDs: IDs{TMDB: "603"}}},
		{"", "Heat.1995.1080p.BluRay.x264.mkv", Movie{Title: "Heat", Year: 1995}},
		{"Studio Ghibli Film Collection", "[AnimeRG] Tales from Earthsea (2006) Ged's War Chronicles [MULTI-AUDIO] [1080p] [x265] [pseudo].mkv", Movie{Title: "Tales from Earthsea", Year: 2006}},
		{"Untitled Predator Film ()", "Predator - Killer of Killers (2025) WEBDL-1080p.mkv", Movie{Title: "Predator - Killer of Killers", Year: 2025}},
		{"Homeward Bound Kaleidoscope RG", "Homeward.Bound.The.Incredible.Journey.1993.1080p.WEBRip.x265.mp4", Movie{Title: "Homeward Bound The Incredible Journey", Year: 1993}},
	}
	for _, tt := range tests {
		got := ParseMovie(tt.folder, tt.file)
		if !reflect.DeepEqual(got, tt.want) {
			t.Errorf("ParseMovie(%q, %q) = %+v, want %+v", tt.folder, tt.file, got, tt.want)
		}
	}
}

func TestParseEpisode(t *testing.T) {
	tests := []struct {
		file string
		want Episode
	}{
		{"Archer (2009) - S01E01 - Mole Hunt Bluray-1080p Proper.mkv", Episode{Season: 1, Episodes: []int{1}, Title: "Mole Hunt"}},
		{"Archer (2009) - S00E05 - Heart of Archness (2) Bluray-1080p.mkv", Episode{Season: 0, Episodes: []int{5}, Title: "Heart of Archness (2)"}},
		{"Pachinko.S01E01.720p.WEB.h264-KOGi.mkv", Episode{Season: 1, Episodes: []int{1}}},
		{"MythBusters - S2014E03 - Hollywood Car Crash Cliches WEBRip-720p.mkv", Episode{Season: 2014, Episodes: []int{3}, Title: "Hollywood Car Crash Cliches"}},
		{"Dog of Flanders S01E27.mkv", Episode{Season: 1, Episodes: []int{27}}},
		{"Show - S02E01-E03 - Triple.mkv", Episode{Season: 2, Episodes: []int{1, 2, 3}, Title: "Triple"}},
		{"Twisted Metal_S01E01_WLUDRV_WEB-DL.720p.x264.EAC3.NTb.mkv", Episode{Season: 1, Episodes: []int{1}, Title: "WLUDRV"}},
		{"Show S01E01E02.mkv", Episode{Season: 1, Episodes: []int{1, 2}}},
		{"show.3x07.title.mkv", Episode{Season: 3, Episodes: []int{7}, Title: "title"}},
		{"The Daily Show 2024.03.14 Guest.mkv", Episode{Season: -1, AirDate: "2024-03-14"}},
		{"[Erai-raws] Kaijuu 8 Gou 2nd Season - 10 [720p CR WEB-DL AVC AAC][MultiSub][AD68A084].mkv", Episode{Season: -1, Episodes: []int{10}, Absolute: 10}},
		{"[Erai-raws] Kaijuu 8 Gou 2nd Season-10 [720p CR WEB-DL AVC AAC][MultiSub][AD68A084].mkv", Episode{Season: -1, Episodes: []int{10}, Absolute: 10}},
		{"[SubsPlease] Frieren - 28 (1080p) [ABCD1234].mkv", Episode{Season: -1, Episodes: []int{28}, Absolute: 28}},
		{"One Piece Episode 1071.mkv", Episode{Season: -1, Episodes: []int{1071}, Absolute: 1071}},
	}
	for _, tt := range tests {
		got, ok := ParseEpisode(tt.file)
		if !ok || !reflect.DeepEqual(got, tt.want) {
			t.Errorf("ParseEpisode(%q) = %+v %v, want %+v", tt.file, got, ok, tt.want)
		}
	}
	if _, ok := ParseEpisode("Some Random Featurette.mkv"); ok {
		t.Error("expected no match for featurette")
	}
}

func TestFansubShow(t *testing.T) {
	title, season, ok := FansubShow("[Erai-raws] Kaijuu 8 Gou 2nd Season-10 [720p CR WEB-DL AVC AAC][MultiSub][AD68A084].mkv")
	if !ok || title != "Kaijuu 8 Gou" || season != 2 {
		t.Errorf("got %q %d %v", title, season, ok)
	}
	title, season, ok = FansubShow("[SubsPlease] Frieren - 28 (1080p) [ABCD1234].mkv")
	if !ok || title != "Frieren" || season != 1 {
		t.Errorf("got %q %d %v", title, season, ok)
	}
}

func TestShowAndSeasonFolders(t *testing.T) {
	if s := ParseShowFolder("Archer (2009)"); s.Title != "Archer" || s.Year != 2009 {
		t.Errorf("show: %+v", s)
	}
	if s := ParseShowFolder("Alien - Earth"); s.Title != "Alien - Earth" || s.Year != 0 {
		t.Errorf("show: %+v", s)
	}
	for in, want := range map[string]int{"Season 01": 1, "Season 2014": 2014, "S2": 2, "Specials": 0, "season.3": 3} {
		if got, ok := ParseSeasonFolder(in); !ok || got != want {
			t.Errorf("season %q = %d %v", in, got, ok)
		}
	}
	if _, ok := ParseSeasonFolder("Extras"); ok {
		t.Error("Extras is not a season")
	}
}

func TestShowTitleFromFile(t *testing.T) {
	for file, want := range map[string]string{
		"Pachinko.S01E01.720p.WEB.h264-KOGi.mkv":                     "Pachinko",
		"WeCrashed - S01E01 - This Is Where It Begins HDTV-720p.mkv": "WeCrashed",
		"[Erai-raws] Kaijuu 8 Gou 2nd Season-10 [720p].mkv":          "Kaijuu 8 Gou",
		"Twisted Metal_S01E01_WLUDRV_WEB-DL.720p.x264.EAC3.NTb.mkv":  "Twisted Metal",
	} {
		if got := ShowTitleFromFile(file).Title; got != want {
			t.Errorf("ShowTitleFromFile(%q) = %q, want %q", file, got, want)
		}
	}
	if PartIndex("Movie (1999) cd2.avi") != 2 || PartIndex("Movie - Part 1.mkv") != 1 || PartIndex("Part of Me (2012).mkv") != 0 {
		t.Error("PartIndex")
	}
}

func TestMatchScore(t *testing.T) {
	cases := []struct {
		cand  string
		cy    int
		want  string
		wy    int
		score int
	}{
		{"Antiporno", 2016, "Antiporno", 2017, 4},
		{"Making of Antiporno", 2016, "Antiporno", 2017, 3},
		{"The Cell", 2000, "Gauche the Cellist", 1982, 0},
		{"The Treehouse", 2014, "Treehouse", 2014, 5},
		{"Disclosure Day", 2026, "The Dish", 2026, 2},
	}
	for _, c := range cases {
		if got := MatchScore(c.cand, c.cy, c.want, c.wy); got != c.score {
			t.Errorf("MatchScore(%q, %d, %q, %d) = %d, want %d", c.cand, c.cy, c.want, c.wy, got, c.score)
		}
	}
	if !SharesWord("Godzilla Gigantis The Fire Monster", "Godzilla Raids Again") || SharesWord("The Dish", "Death Before Dishonour") ||
		!SharesWord("F1 The Movie", "F1") || !SharesWord("71", "'71") {
		t.Error("SharesWord")
	}
}
