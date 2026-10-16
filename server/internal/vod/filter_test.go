package vod

import (
	"context"
	"sort"
	"strings"
	"testing"
)

func movieTitles(t *testing.T, s *Service, f Filter) string {
	t.Helper()
	list, total, err := s.Movies(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	if total != len(list) {
		t.Errorf("total %d for %d films", total, len(list))
	}
	var out []string
	for _, m := range list {
		out = append(out, m.Title)
	}
	sort.Strings(out)
	return strings.Join(out, ",")
}

func seriesTitles(t *testing.T, s *Service, f Filter) string {
	t.Helper()
	list, total, err := s.SeriesList(context.Background(), f)
	if err != nil {
		t.Fatal(err)
	}
	if total != len(list) {
		t.Errorf("total %d for %d series", total, len(list))
	}
	var out []string
	for _, m := range list {
		out = append(out, m.Title)
	}
	sort.Strings(out)
	return strings.Join(out, ",")
}

func TestMovieFilters(t *testing.T) {
	s, _, _, uid := setup(t)
	ctx := context.Background()
	s.Refresh(ctx, false)
	settle(t, s)
	res, _ := s.DB.Exec(`INSERT INTO users (username, display_name) VALUES ('v', 'V')`)
	other, _ := res.LastInsertId()
	// Alpha: TMDB's Comedy/Romance, 2002, 6.4. Beta: Pluto's Anime, 2010. Gamma: no year, no genre.
	for _, c := range []struct {
		f    Filter
		want string
	}{
		{Filter{UserID: uid}, "Alpha,Beta,Gamma (1999)"},
		{Filter{UserID: uid, Genre: "romance"}, "Alpha"},
		{Filter{UserID: uid, Genre: "Anime"}, "Beta"},
		{Filter{UserID: uid, YearFrom: 2000}, "Alpha,Beta"},
		{Filter{UserID: uid, YearFrom: 2000, YearTo: 2009}, "Alpha"},
		{Filter{UserID: uid, YearTo: 2005}, "Alpha"}, // Gamma has no year stored: left out
		{Filter{UserID: uid, MinRating: 6}, "Alpha"},
		{Filter{UserID: uid, MinRating: 7}, ""},
		{Filter{UserID: uid, SourceID: "p"}, "Alpha,Beta,Gamma (1999)"},
		{Filter{UserID: uid, SourceID: "elsewhere"}, ""},
		{Filter{UserID: uid, YearFrom: 2000, Genre: "Comedy", Category: "'00s Movies"}, "Alpha"},
		{Filter{UserID: uid, YearFrom: 2000, Query: "bet"}, "Beta"},
	} {
		if got := movieTitles(t, s, c.f); got != c.want {
			t.Errorf("%+v: %q, want %q", c.f, got, c.want)
		}
	}

	list, _, _ := s.Movies(ctx, Filter{UserID: uid})
	ids := map[string]int64{}
	for _, m := range list {
		ids[m.Title] = m.ID
	}
	s.SetProgress(ctx, uid, ids["Alpha"], 600_000, 6_000_000)
	s.SetWatched(ctx, uid, ids["Beta"], true)
	for _, c := range []struct {
		f    Filter
		want string
	}{
		{Filter{UserID: uid, Watched: "unwatched"}, "Gamma (1999)"},
		{Filter{UserID: uid, Watched: "inProgress"}, "Alpha"},
		{Filter{UserID: uid, Watched: "watched"}, "Beta"},
		{Filter{UserID: uid, Watched: "watched", Genre: "Romance"}, ""},
		// Another person's state is their own.
		{Filter{UserID: other, Watched: "unwatched"}, "Alpha,Beta,Gamma (1999)"},
		{Filter{UserID: other, Watched: "watched"}, ""},
	} {
		if got := movieTitles(t, s, c.f); got != c.want {
			t.Errorf("%+v: %q, want %q", c.f, got, c.want)
		}
	}
	// Marked unwatched again: back to never started.
	s.SetWatched(ctx, uid, ids["Beta"], false)
	if got := movieTitles(t, s, Filter{UserID: uid, Watched: "unwatched"}); got != "Beta,Gamma (1999)" {
		t.Errorf("after unwatching: %q", got)
	}

	gs, err := s.Genres(ctx, "", false)
	if err != nil || len(gs) != 3 || gs[0].Name != "Anime" || gs[1].Name != "Comedy" || gs[2].Count != 1 {
		t.Errorf("genres: %+v %v", gs, err)
	}
	// The rating limit applies to the genre list too.
	if gs, _ := s.Genres(ctx, "PG", false); len(gs) != 1 || gs[0].Name != "Anime" {
		t.Errorf("PG genres: %+v", gs)
	}
}

func TestSeriesFilters(t *testing.T) {
	s, _, _, _, uid := xtreamSetup(t)
	ctx := context.Background()
	s.Refresh(ctx, false)
	settle(t, s)
	res, _ := s.DB.Exec(`INSERT INTO users (username, display_name) VALUES ('v', 'V')`)
	other, _ := res.LastInsertId()
	// Kings: TMDB's Drama/Fantasy, 2011, 8.4. Small Town: Comedy/Drama, 2019, 7. Gone Show: nothing.
	for _, c := range []struct {
		f    Filter
		want string
	}{
		{Filter{UserID: uid, Genre: "drama"}, "Kings of the Hill,Small Town"},
		{Filter{UserID: uid, Genre: "Fantasy"}, "Kings of the Hill"},
		{Filter{UserID: uid, YearFrom: 2010, YearTo: 2019}, "Kings of the Hill,Small Town"},
		{Filter{UserID: uid, YearFrom: 2015}, "Small Town"},
		{Filter{UserID: uid, MinRating: 8}, "Kings of the Hill"},
		{Filter{UserID: uid, SourceID: "xc", Category: "Drama Series"}, "Gone Show,Kings of the Hill"},
		{Filter{UserID: uid, Genre: "Drama", MinRating: 7, YearTo: 2015}, "Kings of the Hill"},
		{Filter{UserID: uid, Watched: "unwatched"}, "Gone Show,Kings of the Hill,Small Town"},
	} {
		if got := seriesTitles(t, s, c.f); got != c.want {
			t.Errorf("%+v: %q, want %q", c.f, got, c.want)
		}
	}

	by := seriesByTitle(t, s, uid)
	kings, small := by["Kings of the Hill"], by["Small Town"]
	s.LoadEpisodes(ctx, kings.ID, false)
	s.LoadEpisodes(ctx, small.ID, false)
	eps, _ := s.Episodes(ctx, kings.ID, uid)
	s.SetEpisodeProgress(ctx, uid, eps[0].ID, 600_000, 3_600_000) // Kings started
	s.SetSeriesWatched(ctx, uid, small.ID, nil, true)             // Small Town finished
	for _, c := range []struct {
		f    Filter
		want string
	}{
		{Filter{UserID: uid, Watched: "unwatched"}, "Gone Show,Kings of the Hill"},
		{Filter{UserID: uid, Watched: "inProgress"}, "Kings of the Hill"},
		{Filter{UserID: uid, Watched: "watched"}, "Small Town"},
		{Filter{UserID: uid, Watched: "watched", Genre: "Fantasy"}, ""},
		{Filter{UserID: other, Watched: "inProgress"}, ""},
		{Filter{UserID: other, Watched: "unwatched"}, "Gone Show,Kings of the Hill,Small Town"},
	} {
		if got := seriesTitles(t, s, c.f); got != c.want {
			t.Errorf("%+v: %q, want %q", c.f, got, c.want)
		}
	}
	// Finishing the last episodes makes Kings watched, not in progress.
	s.SetSeriesWatched(ctx, uid, kings.ID, nil, true)
	if got := seriesTitles(t, s, Filter{UserID: uid, Watched: "inProgress"}); got != "" {
		t.Errorf("in progress after finishing: %q", got)
	}

	gs, _ := s.Genres(ctx, "", true)
	if len(gs) != 3 || gs[0].Name != "Comedy" || gs[1].Name != "Drama" || gs[1].Count != 2 {
		t.Errorf("series genres: %+v", gs)
	}
}
