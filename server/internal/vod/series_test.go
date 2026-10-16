package vod

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"marquee/internal/metadata/tmdb"
	"marquee/internal/settings"
)

// Series as Xtream Codes servers list them: numbers as strings or numbers, "tmdb" or
// "tmdb_id", backdrops as a list or one string, empty values.
const xcSeries = `[
{"num":1,"name":"Kings of the Hill","series_id":"201","cover":"/covers/201.jpg","plot":"Source plot.","cast":"A, B",
 "director":"","genre":"Drama","releaseDate":"2011-04-17","rating":"9.2","backdrop_path":["/bd/201.jpg"],"tmdb":"1399",
 "category_id":"10","last_modified":"1700000000"},
{"num":2,"name":"Small Town","series_id":202,"cover":"","plot":"Small plot.","genre":"Comedy, Drama","release_date":"2019",
 "rating":7,"backdrop_path":"/bd/202.jpg","tmdb_id":"","category_id":11,"last_modified":1700000001},
{"num":3,"name":"Gone Show","series_id":"203","category_id":"10","tmdb":null,"rating":""},
{"num":4,"name":"","series_id":"204"}
]`

// Episodes by season ("1": [...]), with string numbers and "Show - S01E01 - " titles.
const xcInfoByMap = `{"seasons":[{"season_number":"1","name":"Season 1","cover":"/s1.jpg","episode_count":"2"},
 {"season_number":2,"name":"Season 2"},{"season_number":3,"name":"Empty season"}],
 "info":{"name":"Kings of the Hill","plot":"Info plot."},
 "episodes":{"2":[{"id":"3003","episode_num":"1","title":"Kings of the Hill - S02E01 - Return","container_extension":"mkv","season":"2",
   "info":{"duration_secs":"3000","movie_image":"/e/3003.jpg","plot":"Back."}}],
 "1":[{"id":"3001","episode_num":"1","title":"Kings of the Hill - S01E01 - Winter","container_extension":"mkv","season":1,
   "info":{"duration_secs":3600,"releasedate":"2011-04-17","rating":"8.5"}},
  {"id":3002,"episode_num":2,"title":"Kings of the Hill - S01E02","container_extension":".mp4",
   "info":{"duration":"00:55:00","plot":"Second."}}]}}`

// Episodes as one list; seasons and info empty.
const xcInfoByList = `{"seasons":[],"info":[],"episodes":[
 {"id":"4001","episode_num":"1","title":"Pilot","container_extension":"mp4","season":"1","info":[]},
 {"id":"4002","episode_num":"2","title":"Second","container_extension":"bad/ext","season":"1","info":{"duration_secs":"1500"}}]}`

func (f *fakeMatcher) TVByID(_ context.Context, id int) (*tmdb.TVShow, error) {
	f.mu.Lock()
	f.byID = append(f.byID, -id) // negative: a series
	f.mu.Unlock()
	if id != 1399 {
		return nil, errors.New("not found")
	}
	t := &tmdb.TVShow{ID: 1399, Name: "Kings", Overview: "TMDB's plot.", FirstAirDate: "2011-04-17", VoteAverage: 8.4,
		Genres: []tmdb.Genre{{Name: "Drama"}, {Name: "Fantasy"}}, PosterPath: "/k.jpg", BackdropPath: "/kb.jpg"}
	t.Credits.Cast = []tmdb.Cast{{Name: "Sean", Character: "Ned"}}
	t.ContentRatings.Results = append(t.ContentRatings.Results, struct {
		Country string `json:"iso_3166_1"`
		Rating  string `json:"rating"`
	}{"US", "TV-MA"})
	return t, nil
}

func (f *fakeMatcher) TVSeason(_ context.Context, showID, season int) (*tmdb.Season, error) {
	if showID != 1399 || season != 1 {
		return nil, errors.New("not found")
	}
	return &tmdb.Season{SeasonNumber: 1, Name: "Season 1", Overview: "The first.", Episodes: []tmdb.Episode{
		{EpisodeNumber: 1, SeasonNumber: 1, Name: "Winter Is Here", Overview: "TMDB's first.", StillPath: "/still1.jpg"},
	}}, nil
}

func seriesByTitle(t *testing.T, s *Service, uid int64) map[string]Series {
	t.Helper()
	list, _, err := s.SeriesList(context.Background(), Filter{UserID: uid})
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]Series{}
	for _, m := range list {
		out[m.Title] = m
	}
	return out
}

func (f *fakeXtream) count(action string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if c == action {
			n++
		}
	}
	return n
}

func TestSeriesRefreshAndEpisodes(t *testing.T) {
	s, f, srv, m, uid := xtreamSetup(t)
	ctx := context.Background()
	s.Refresh(ctx, false)
	settle(t, s)

	st, _ := s.Status(ctx)
	if st.Series != 3 || len(st.Sources) != 1 || st.Sources[0].Series != 3 || st.Sources[0].Movies != 3 || st.Sources[0].Error != "" {
		t.Fatalf("status: %+v", st)
	}
	// Refreshing never asks for a series' episodes.
	if f.count("get_series_info") != 0 {
		t.Errorf("requests: %v", f.calls)
	}
	by := seriesByTitle(t, s, uid)
	kings, small := by["Kings of the Hill"], by["Small Town"]
	// The series named by TMDB id gets TMDB's details; its title stays the source's.
	if !kings.Matched || kings.Overview != "TMDB's plot." || kings.ContentRating != "TV-MA" || kings.Rating != 8.4 ||
		kings.Year != 2011 || len(kings.Cast) != 1 || kings.Category != "Drama Series" || !kings.HasPoster || !kings.HasBackdrop {
		t.Errorf("kings: %+v", kings)
	}
	if small.Matched || small.Overview != "Small plot." || small.Year != 2019 || small.Rating != 7 || len(small.Genres) != 2 ||
		small.Category != "Comedy Series" || small.HasPoster || !small.HasBackdrop || small.Episodes != 0 {
		t.Errorf("small: %+v", small)
	}
	if n := len(m.byID); n != 2 { // 603 (the film) and the one series with an id
		t.Errorf("TMDB lookups: %v", m.byID)
	}
	cats, _ := s.SeriesCategories(ctx, "")
	if len(cats) != 2 || cats[0].Name != "Comedy Series" || cats[1].Count != 2 {
		t.Errorf("categories: %+v", cats)
	}
	if list, total, _ := s.SeriesList(ctx, Filter{UserID: uid, Query: "town", Sort: "rating"}); total != 1 || list[0].ID != small.ID {
		t.Errorf("search: %d", total)
	}
	// A TV-14 limit hides TV-MA and unrated series, as for films.
	if _, total, _ := s.SeriesList(ctx, Filter{UserID: uid, MaxRating: "TV-14"}); total != 0 {
		t.Errorf("TV-14 listing: %d", total)
	}

	// Episodes come when the series is opened: both shapes, numbers as strings.
	if err := s.LoadEpisodes(ctx, kings.ID, false); err != nil {
		t.Fatal(err)
	}
	eps, _ := s.Episodes(ctx, kings.ID, uid)
	if len(eps) != 3 || eps[0].Season != 1 || eps[0].Episode != 1 || eps[2].Season != 2 {
		t.Fatalf("episodes: %+v", eps)
	}
	if eps[0].StreamURL != "3001.mkv" || eps[1].StreamURL != "3002.mp4" || eps[1].RuntimeMS != 55*60000 || eps[0].RuntimeMS != 3_600_000 ||
		eps[0].Rating != 8.5 || eps[0].AirDate != "2011-04-17" || eps[2].Title != "Return" || eps[1].Title != "Episode 2" || !eps[2].HasStill {
		t.Errorf("episodes: %+v", eps)
	}
	seasons, _ := s.Seasons(ctx, kings.ID, uid)
	if len(seasons) != 2 || seasons[0].Name != "Season 1" || seasons[0].Episodes != 2 || seasons[1].Number != 2 {
		t.Errorf("seasons: %+v", seasons)
	}
	// TMDB's names, overviews and stills follow in the background for a matched series.
	deadline := time.Now().Add(3 * time.Second)
	for {
		e, _ := s.OneEpisode(ctx, eps[0].ID, uid)
		if e.Title == "Winter Is Here" && e.Overview == "TMDB's first." {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("TMDB episode: %+v", e)
		}
		time.Sleep(20 * time.Millisecond)
	}
	// Kept for a while: opening again doesn't ask; refresh=true does, and TMDB's names stay.
	s.LoadEpisodes(ctx, kings.ID, false)
	if n := f.count("get_series_info"); n != 1 {
		t.Errorf("episode fetches = %d", n)
	}
	s.LoadEpisodes(ctx, kings.ID, true)
	if n := f.count("get_series_info"); n != 2 {
		t.Errorf("episode fetches after a forced refresh = %d", n)
	}
	if e, _ := s.OneEpisode(ctx, eps[0].ID, uid); e.Title != "Winter Is Here" || e.ID != eps[0].ID {
		t.Errorf("after refetch: %+v", e)
	}

	if err := s.LoadEpisodes(ctx, small.ID, false); err != nil {
		t.Fatal(err)
	}
	seps, _ := s.Episodes(ctx, small.ID, uid)
	if len(seps) != 2 || seps[0].Title != "Pilot" || seps[1].StreamURL != "4002.mp4" || seps[1].RuntimeMS != 1_500_000 {
		t.Errorf("list-shaped episodes: %+v", seps)
	}
	if z, _ := s.Seasons(ctx, small.ID, uid); len(z) != 1 || z[0].Name != "Season 1" {
		t.Errorf("derived seasons: %+v", z)
	}
	// A series the server has no episodes for reports why (nothing kept).
	if err := s.LoadEpisodes(ctx, by["Gone Show"].ID, false); err == nil {
		t.Error("no error for a series without episodes")
	}

	// The episode's link holds the password only when it's built to play.
	link, _, ok := s.EpisodePlayLink(eps[0])
	if !ok || link != srv.URL+"/series/marquee/"+url.PathEscape(xcPass)+"/3001.mkv" {
		t.Errorf("link %q", link)
	}
	if got := s.Redact("xc", "ffmpeg: "+link+": refused"); strings.Contains(got, "w0rd") {
		t.Errorf("redact: %q", got)
	}
	var stored string
	s.DB.QueryRow(`SELECT group_concat(stream_url || still_url, ' ') FROM vod_episodes`).Scan(&stored)
	var storedSeries string
	s.DB.QueryRow(`SELECT group_concat(logo_url || backdrop_url || poster_url, ' ') FROM vod_series`).Scan(&storedSeries)
	if strings.Contains(stored+storedSeries, "w0rd") || !strings.Contains(storedSeries, srv.URL+"/covers/201.jpg") {
		t.Errorf("stored: %q %q", stored, storedSeries)
	}
}

func TestSeriesProgressAndNextEpisode(t *testing.T) {
	s, _, _, _, uid := xtreamSetup(t)
	ctx := context.Background()
	s.Refresh(ctx, false)
	settle(t, s)
	kings := seriesByTitle(t, s, uid)["Kings of the Hill"]
	s.LoadEpisodes(ctx, kings.ID, false)
	eps, _ := s.Episodes(ctx, kings.ID, uid)
	e1, e2, e3 := eps[0].ID, eps[1].ID, eps[2].ID

	next := func() int64 {
		t.Helper()
		e, err := s.NextUp(ctx, kings.ID, uid)
		if err != nil {
			t.Fatal(err)
		}
		if e == nil {
			return 0
		}
		return e.ID
	}
	if next() != e1 {
		t.Error("nothing watched: the first episode is next")
	}
	if cw, _ := s.SeriesContinueWatching(ctx, uid, ""); len(cw) != 0 {
		t.Errorf("continue before watching: %+v", cw)
	}
	s.SetEpisodeProgress(ctx, uid, e2, 600_000, 3_300_000)
	cw, _ := s.SeriesContinueWatching(ctx, uid, "")
	if len(cw) != 1 || cw[0].Episode.ID != e2 || cw[0].Episode.PositionMS != 600_000 || cw[0].Series.ID != kings.ID {
		t.Fatalf("continue: %+v", cw)
	}
	// Past the threshold it counts as watched, and the next season's first is next.
	s.SetEpisodeProgress(ctx, uid, e2, 3_200_000, 3_300_000)
	if e, _ := s.OneEpisode(ctx, e2, uid); !e.Watched || e.PositionMS != 0 {
		t.Errorf("after the end: %+v", e)
	}
	if next() != e3 {
		t.Errorf("next after S1E2 = %d", next())
	}
	// Autoplay goes in order, across seasons, and stops after the last.
	if n, _ := s.NextEpisode(ctx, e1, uid); n == nil || n.ID != e2 {
		t.Error("next after S1E1")
	}
	if n, _ := s.NextEpisode(ctx, e2, uid); n == nil || n.ID != e3 {
		t.Error("next after S1E2")
	}
	if n, _ := s.NextEpisode(ctx, e3, uid); n != nil {
		t.Errorf("next after the last: %+v", n)
	}

	// Marking a season, then the series.
	s.SetSeriesWatched(ctx, uid, kings.ID, ptrInt(2), true)
	if z, _ := s.Seasons(ctx, kings.ID, uid); z[1].WatchedEpisodes != 1 || z[0].WatchedEpisodes != 1 {
		t.Errorf("seasons: %+v", z)
	}
	if next() != e1 {
		t.Errorf("only S1E1 unwatched: next = %d", next())
	}
	s.SetSeriesWatched(ctx, uid, kings.ID, nil, true)
	if m, _ := s.OneSeries(ctx, kings.ID, uid); !m.AllWatched() || m.Watched != 3 {
		t.Errorf("series: %+v", m)
	}
	if next() != 0 {
		t.Error("everything watched: nothing next")
	}
	if cw, _ := s.SeriesContinueWatching(ctx, uid, ""); len(cw) != 0 {
		t.Errorf("finished series in continue: %+v", cw)
	}
	s.SetEpisodeWatched(ctx, uid, e3, false)
	if next() != e3 {
		t.Error("unwatched S2E1 is next")
	}
	s.SetSeriesWatched(ctx, uid, kings.ID, nil, false)
	if m, _ := s.OneSeries(ctx, kings.ID, uid); m.Watched != 0 || next() != e1 {
		t.Errorf("after unwatching: %+v", m)
	}
}

func ptrInt(n int) *int { return &n }

func TestSeriesRemoval(t *testing.T) {
	s, f, _, _, uid := xtreamSetup(t)
	ctx := context.Background()
	s.Refresh(ctx, false)
	settle(t, s)
	by := seriesByTitle(t, s, uid)
	small := by["Small Town"]
	s.LoadEpisodes(ctx, small.ID, false)
	eps, _ := s.Episodes(ctx, small.ID, uid)
	s.SetEpisodeProgress(ctx, uid, eps[0].ID, 60_000, 0)

	// Gone from the server: the series goes with its episodes and progress; the rest stay.
	f.mu.Lock()
	f.series = strings.Replace(xcSeries, `"name":"Small Town"`, `"name":""`, 1)
	f.mu.Unlock()
	s.Refresh(ctx, false)
	settle(t, s)
	if _, total, _ := s.SeriesList(ctx, Filter{}); total != 2 {
		t.Errorf("series after removal = %d", total)
	}
	var n int
	s.DB.QueryRow("SELECT (SELECT COUNT(*) FROM vod_episodes) + (SELECT COUNT(*) FROM vod_episode_progress)").Scan(&n)
	if n != 0 {
		t.Errorf("episodes and progress left: %d", n)
	}
	// A series list that fails keeps what was there (films still load).
	f.mu.Lock()
	f.series = `not json`
	f.mu.Unlock()
	s.Refresh(ctx, false)
	st, _ := s.Status(ctx)
	if st.Series != 2 || st.Sources[0].Series != 2 || st.Sources[0].Movies != 3 || !strings.HasPrefix(st.Sources[0].Error, "series: ") {
		t.Errorf("after a failed series list: %+v", st)
	}
	// Removing the source removes its series.
	s.Settings.Update(ctx, func(st *settings.Settings) error { st.Integrations.VODSources = nil; return nil })
	s.Refresh(ctx, false)
	if _, total, _ := s.SeriesList(ctx, Filter{}); total != 0 {
		t.Errorf("series after removing the source = %d", total)
	}
}

// Dispatcharr answers 404 for an unknown username and 401 for a wrong (or unset) XC
// password; both get a message that says so, without the password.
func TestXtreamDispatcharrErrors(t *testing.T) {
	s, _, _, _, _ := xtreamSetup(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("username") != "Marquee" {
			w.WriteHeader(http.StatusNotFound)
			io.WriteString(w, `{"detail":"Not found."}`)
			return
		}
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"error":"Unauthorized"}`)
	}))
	defer srv.Close()
	ctx := context.Background()
	src := settings.VODSource{Kind: settings.VODKindXtream, URL: srv.URL, Username: "marquee", Password: xcPass}
	_, err := s.Test(ctx, src)
	if err == nil || !strings.Contains(err.Error(), "doesn't know that username") || !strings.Contains(err.Error(), "case-sensitive") {
		t.Errorf("unknown user: %v", err)
	}
	src.Username = "Marquee"
	_, err = s.Test(ctx, src)
	if err == nil || !strings.Contains(err.Error(), "wrong password, or the user has no XC password") {
		t.Errorf("wrong password: %v", err)
	}
	if err != nil && strings.Contains(err.Error(), "w0rd") {
		t.Errorf("password in %q", err)
	}
}
