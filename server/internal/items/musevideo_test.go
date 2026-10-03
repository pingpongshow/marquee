package items

import (
	"fmt"
	"testing"
)

var libraryGenres = []string{"Action", "Adventure", "Animation", "Comedy", "Crime", "Documentary", "Drama", "Family", "Fantasy",
	"History", "Horror", "Music", "Mystery", "Romance", "Science Fiction", "Thriller", "War", "Western",
	"Action & Adventure", "Sci-Fi & Fantasy", "Kids", "Reality"}

func TestParseVideoPrompt(t *testing.T) {
	cases := []struct {
		prompt string
		want   VideoQuery
		under  string
	}{
		{"90s sci-fi with time travel", VideoQuery{YearFrom: 1990, YearTo: 1999,
			Genres: [][]string{{"Science Fiction", "Sci-Fi & Fantasy"}}, Rest: "time travel"},
			"1990s · Science Fiction · like 'time travel'"},
		{"1980s horror movies", VideoQuery{Types: []string{"movie"}, YearFrom: 1980, YearTo: 1989, Genres: [][]string{{"Horror"}}},
			"Movies · 1980s · Horror"},
		{"a rom-com from the nineties", VideoQuery{YearFrom: 1990, YearTo: 1999, Genres: [][]string{{"Romance"}, {"Comedy"}}},
			"1990s · Romance · Comedy"},
		{"80's action films", VideoQuery{Types: []string{"movie"}, YearFrom: 1980, YearTo: 1989, Genres: [][]string{{"Action", "Action & Adventure"}}}, ""},
		{"’70s westerns", VideoQuery{YearFrom: 1970, YearTo: 1979, Genres: [][]string{{"Western"}}}, ""},
		{"thrillers from 2010", VideoQuery{YearFrom: 2010, Genres: [][]string{{"Thriller"}}}, "2010 or later · Thriller"},
		{"comedies before 2000", VideoQuery{YearTo: 1999, Genres: [][]string{{"Comedy"}}}, "1999 or earlier · Comedy"},
		{"dramas after 2015", VideoQuery{YearFrom: 2016, Genres: [][]string{{"Drama"}}}, ""},
		{"films between 1995 and 2005 about heists", VideoQuery{Types: []string{"movie"}, YearFrom: 1995, YearTo: 2005, Rest: "heists"}, ""},
		{"something from the 2000s", VideoQuery{YearFrom: 2000, YearTo: 2009}, "2000s"},
		{"a tv series about chefs", VideoQuery{Types: []string{"show"}, Rest: "chefs"}, "Shows · like 'chefs'"},
		{"a show i haven't seen yet", VideoQuery{Types: []string{"show"}, Unwatched: true, Rest: "yet"}, ""},
		{"unwatched documentaries", VideoQuery{Unwatched: true, Genres: [][]string{{"Documentary"}}}, "Documentary · Unwatched"},
		{"something new to me", VideoQuery{Unwatched: true}, "Unwatched"},
		{"a short funny movie", VideoQuery{Types: []string{"movie"}, Genres: [][]string{{"Comedy"}}, MaxMinutes: 100}, "Movies · Comedy · Under 100 min"},
		{"thriller under 90 minutes", VideoQuery{Genres: [][]string{{"Thriller"}}, MaxMinutes: 90}, ""},
		{"a film less than 2 hours", VideoQuery{Types: []string{"movie"}, MaxMinutes: 120}, ""},
		{"a long epic war film", VideoQuery{Types: []string{"movie"}, Genres: [][]string{{"War", "War & Politics"}}, MinMinutes: 140}, ""},
		{"over 2.5 hours", VideoQuery{MinMinutes: 150}, ""},
		{"highly rated mysteries", VideoQuery{Genres: [][]string{{"Mystery"}}, MinRating: 7.5}, "Mystery · Highly rated"},
		{"the best crime films", VideoQuery{Types: []string{"movie"}, Genres: [][]string{{"Crime"}}, MinRating: 7.5}, ""},
		{"family movie night with dinosaurs", VideoQuery{Types: []string{"movie"}, Family: true, Rest: "night dinosaurs"}, ""},
		{"animated films for kids", VideoQuery{Types: []string{"movie"}, Family: true, Genres: [][]string{{"Animation"}}}, "Movies · Animation · Family-friendly"},
		{"cozy mystery set in a small english village", VideoQuery{Genres: [][]string{{"Mystery"}}, Rest: "cozy small english village"}, ""},
		{"Show me 90s sci-fi", VideoQuery{YearFrom: 1990, YearTo: 1999, Genres: [][]string{{"Science Fiction", "Sci-Fi & Fantasy"}}}, ""},
		{"movies from 1999", VideoQuery{Types: []string{"movie"}, YearFrom: 1999}, ""},
		{"a 1999 movie", VideoQuery{Types: []string{"movie"}, YearFrom: 1999, YearTo: 1999}, "Movies · 1999"},
		{"star wars", VideoQuery{Rest: "star wars"}, "like 'star wars'"},
		{"movies and shows about space", VideoQuery{Rest: "space"}, ""},
		{"lonely robot", VideoQuery{Rest: "lonely robot"}, "like 'lonely robot'"},
		{"best", VideoQuery{MinRating: 7.5}, "Highly rated"},
	}
	for _, c := range cases {
		got := ParseVideoPrompt(c.prompt, libraryGenres)
		lbl := got.labels
		got.labels = nil
		if fmt.Sprintf("%+v", got) != fmt.Sprintf("%+v", c.want) {
			t.Errorf("%q:\n got %+v\nwant %+v", c.prompt, got, c.want)
		}
		got.labels = lbl
		if c.under != "" && got.Understood() != c.under {
			t.Errorf("%q: understood %q, want %q", c.prompt, got.Understood(), c.under)
		}
	}
	if u := ParseVideoPrompt("a movie", nil).Understood(); u != "Movies" {
		t.Errorf("understood %q", u)
	}
	if u := ParseVideoPrompt("something", nil).Understood(); u != "Top rated" {
		t.Errorf("understood %q", u)
	}
}
