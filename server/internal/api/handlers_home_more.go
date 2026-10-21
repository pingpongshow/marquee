package api

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"marquee/internal/items"
	"marquee/internal/vodsuggest"
)

// More Home rows (USER-12): holidays, top rated, trending, genre rows and other categories,
// each with films and series on demand mixed in for apps that ask (mixed=true), and each
// one in full for See all (GET /hubs/home/{hubId}).

const (
	homeRowSize = 20 // library titles per Home row
	genreRows   = 3
)

// decades are the ones the decade row takes turns with, a different one each day.
var decades = []int{1970, 1980, 1990, 2000, 2010}

// genreTitles are friendlier row titles for common genres.
var genreTitles = map[string]string{
	"action": "Action Movies & Shows", "adventure": "Adventures", "animation": "Animated Favourites", "comedy": "Comedies",
	"crime": "Crime Stories", "documentary": "Documentaries", "drama": "Dramas", "family": "Family Time",
	"fantasy": "Fantasy Worlds", "horror": "Horror", "mystery": "Mysteries", "romance": "Romance",
	"science fiction": "Sci-Fi", "sci-fi & fantasy": "Sci-Fi & Fantasy", "thriller": "Thrillers", "war": "War Stories",
	"western": "Westerns", "action & adventure": "Action & Adventure", "kids": "For the Kids", "music": "Music & Musicals",
	"history": "Based on History", "reality": "Reality TV",
}

func genreTitle(g string) string {
	if t, ok := genreTitles[strings.ToLower(g)]; ok {
		return t
	}
	return g
}

func genreID(g string) string { return "genre-" + strings.ReplaceAll(strings.ToLower(g), " ", "-") }

// mixedCap is how many titles a mixed row of n library titles holds in all.
func mixedCap(n int) int { return n + n/2 }

// moreRows builds the extra suggestion rows for one request, n library titles a row.
type moreRows struct {
	h    *Handlers
	ctx  context.Context
	acc  items.Access
	home []int64
	n    int
	// The on-demand recommendation rows, worked out once per request when needed.
	vodRows func() (map[string][]vodsuggest.Suggestion, error)
}

func (h *Handlers) newMoreRows(ctx context.Context, home []int64, n int) *moreRows {
	m := &moreRows{h: h, ctx: ctx, acc: access(ctx), home: append([]int64{}, home...), n: n} // none means none, not all
	m.vodRows = sync.OnceValues(func() (map[string][]vodsuggest.Suggestion, error) {
		u, _, _, allowed := h.suggestSession(ctx)
		if !allowed {
			return nil, nil
		}
		rows, err := h.VODSuggest.Rows(ctx, u, "", n)
		out := map[string][]vodsuggest.Suggestion{}
		for _, r := range rows {
			out[r.ID] = r.Items
		}
		return out, err
	})
	return m
}

// vodRow mixes in one of the on-demand recommendation rows.
func (m *moreRows) vodRow(id string) func() ([]VodSuggestion, error) {
	return func() ([]VodSuggestion, error) {
		rows, err := m.vodRows()
		if err != nil || len(rows[id]) == 0 {
			return nil, err
		}
		return m.h.toAPIVodSuggestions(m.ctx, m.acc.UserID, rows[id])
	}
}

// muse mixes in on-demand titles that fit q.
func (m *moreRows) muse(q vodsuggest.MuseQuery) func() ([]VodSuggestion, error) {
	return func() ([]VodSuggestion, error) { return m.h.vodMuse(m.ctx, q, m.n) }
}

// discover is a category row of unwatched library titles that fit q.
func (m *moreRows) discover(id, title string, q items.DiscoverQuery, vod func() ([]VodSuggestion, error)) homeRow {
	return homeRow{id: id, title: title, vodFill: vod,
		fill: func() ([]items.Summary, error) { return m.h.Items.Discover(m.ctx, m.acc, m.home, q, m.n) }}
}

func (m *moreRows) holiday(hd items.Holiday) homeRow {
	return homeRow{id: "holiday-" + hd.ID, title: hd.Title,
		fill:    func() ([]items.Summary, error) { return m.h.Items.HolidayVideos(m.ctx, m.acc, hd, m.home, m.n) },
		vodFill: m.muse(vodsuggest.MuseQuery{Keywords: hd.Keywords, Words: hd.ID})}
}

// genre is a row of one genre; all is every library genre name (for the genre's aliases on demand).
func (m *moreRows) genre(g string, all []string) homeRow {
	group := items.GenreGroup(g, all)
	if len(group) == 0 {
		group = []string{g}
	}
	return homeRow{id: genreID(g), title: genreTitle(g),
		fill:    func() ([]items.Summary, error) { return m.h.Items.GenreVideos(m.ctx, m.acc, g, m.home, m.n) },
		vodFill: m.muse(vodsuggest.MuseQuery{Genres: [][]string{group}})}
}

func (m *moreRows) decade(from int) homeRow {
	return m.discover(fmt.Sprintf("decade-%d", from), fmt.Sprintf("Best of the %02ds", from%100),
		items.DiscoverQuery{YearFrom: from, YearTo: from + 9, MinRating: 6.5},
		m.muse(vodsuggest.MuseQuery{YearFrom: from, YearTo: from + 9, MinRating: 6.5}))
}

// rows are the extra suggestion rows, in their default order after Because You Watched.
func (m *moreRows) rows() []homeRow {
	year := time.Now().Year()
	return []homeRow{
		{id: "holiday", title: "Holiday Picks", expand: func() ([]homeRow, error) {
			var out []homeRow
			for _, hd := range items.HolidaysOn(time.Now()) {
				out = append(out, m.holiday(hd))
			}
			return out, nil
		}},
		m.discover("new-releases", "New Releases", items.DiscoverQuery{YearFrom: year - 2, Newest: true},
			m.muse(vodsuggest.MuseQuery{YearFrom: year - 2})),
		m.discover("top-rated", "Top Rated", items.DiscoverQuery{MinRating: 7.5}, m.muse(vodsuggest.MuseQuery{MinRating: 7.5})),
		{id: "trending", title: "Trending Now", vodFill: m.vodRow("popular"),
			fill: func() ([]items.Summary, error) { return m.h.Items.Trending(m.ctx, m.acc, m.home, m.n) }},
		{id: "genres", title: "Your Genres", expand: func() ([]homeRow, error) {
			genres, err := m.h.Items.TasteGenres(m.ctx, m.acc, m.home, 2*genreRows)
			if err != nil || len(genres) == 0 {
				return nil, err
			}
			all, err := m.h.Items.VideoGenres(m.ctx)
			if err != nil {
				return nil, err
			}
			// A different few each day, from the genres they watch most.
			start := time.Now().YearDay() % len(genres)
			var out []homeRow
			for i := range min(genreRows, len(genres)) {
				out = append(out, m.genre(genres[(start+i)%len(genres)], all))
			}
			return out, nil
		}},
		m.discover("acclaimed", "Critically Acclaimed", items.DiscoverQuery{MinCritic: 85}, m.vodRow("acclaimed")),
		m.discover("binge-worthy", "Binge-Worthy Shows", items.DiscoverQuery{Types: []string{"show"}, MinEpisodes: 20, MinRating: 7},
			m.muse(vodsuggest.MuseQuery{Kinds: []string{vodsuggest.Series}, MinRating: 7.5})),
		m.discover("short-films", "Under 95 Minutes", items.DiscoverQuery{Types: []string{"movie"}, MaxMinutes: 95, MinRating: 6.5},
			m.muse(vodsuggest.MuseQuery{Kinds: []string{vodsuggest.Movie}, MaxMinutes: 95, MinRating: 6.5})),
		{id: "decades", title: "Best of a Decade", expand: func() ([]homeRow, error) {
			return []homeRow{m.decade(decades[time.Now().YearDay()%len(decades)])}, nil
		}},
		m.discover("classics", "Timeless Classics", items.DiscoverQuery{YearTo: 1979, MinRating: 7},
			m.muse(vodsuggest.MuseQuery{YearTo: 1979, MinRating: 7})),
		m.discover("family", "Family Movie Night", items.DiscoverQuery{Family: true},
			m.muse(vodsuggest.MuseQuery{MaxRating: "PG", Genres: [][]string{{"family", "animation", "kids"}}})),
		m.discover("international", "International Picks", items.DiscoverQuery{Foreign: true, MinRating: 6.5}, nil),
		{id: "watch-again", title: "Watch It Again",
			fill: func() ([]items.Summary, error) { return m.h.Items.WatchAgain(m.ctx, m.acc, m.home, m.n) }},
	}
}

// direct finds a row that's one of several (a holiday, genre or decade) by its hub id,
// whether or not it's on Home today (See all of yesterday's genre row still works).
func (m *moreRows) direct(id string) (homeRow, bool, error) {
	kind, rest, _ := strings.Cut(id, "-")
	switch kind {
	case "holiday":
		for _, hd := range items.Holidays() {
			if hd.ID == rest {
				return m.holiday(hd), true, nil
			}
		}
	case "genre":
		all, err := m.h.Items.VideoGenres(m.ctx)
		if err != nil {
			return homeRow{}, false, err
		}
		for _, g := range all {
			if genreID(g) == id {
				return m.genre(g, all), true, nil
			}
		}
	case "decade":
		if y, err := strconv.Atoi(rest); err == nil && y%10 == 0 && y >= 1900 && y <= time.Now().Year() {
			return m.decade(y), true, nil
		}
	}
	return homeRow{}, false, nil
}

// vodMuse are up to n on-demand titles for q, best first (none when on demand isn't the caller's).
func (h *Handlers) vodMuse(ctx context.Context, q vodsuggest.MuseQuery, n int) ([]VodSuggestion, error) {
	u, s, _, allowed := h.suggestSession(ctx)
	if !allowed {
		return nil, nil
	}
	list, err := h.VODSuggest.Muse(ctx, u, q, n)
	if err != nil || len(list) == 0 {
		return nil, err
	}
	return h.toAPIVodSuggestions(ctx, s.User.ID, list)
}

// mixEntries puts library items and on-demand titles in one row: two from the library to
// each one on demand, then whichever is left, up to max.
func mixEntries(lib []items.Summary, vod []VodSuggestion, max int) []HomeEntry {
	out := make([]HomeEntry, 0, min(len(lib)+len(vod), max))
	i, j := 0, 0
	for len(out) < max && (i < len(lib) || j < len(vod)) {
		if i < len(lib) && (j >= len(vod) || len(out)%3 != 2) {
			it := toAPISummary(lib[i])
			out = append(out, HomeEntry{Kind: HomeEntryKindItem, Key: "item:" + strconv.FormatInt(lib[i].ID, 10), Item: &it})
			i++
			continue
		}
		v := vod[j]
		out = append(out, HomeEntry{Kind: HomeEntryKindVod, Key: "vod:" + v.Key, Vod: &v})
		j++
	}
	return out
}

// fillHub loads a row (not continue-watching or the VOD row) as a hub; ok is false when it
// has nothing to show.
func (h *Handlers) fillHub(r homeRow, mixed bool, n int) (Hub, bool, error) {
	list, err := r.fill()
	if err != nil {
		return Hub{}, false, err
	}
	var vod []VodSuggestion
	if mixed && r.vodFill != nil {
		if vod, err = r.vodFill(); err != nil {
			return Hub{}, false, err
		}
	}
	if len(list) == 0 && len(vod) == 0 {
		return Hub{}, false, nil
	}
	hub := Hub{Id: r.id, Title: r.title, LibraryId: nz(r.lib), Items: make([]ItemSummary, len(list))}
	for i, it := range list {
		hub.Items[i] = toAPISummary(it)
	}
	if mixed && r.vodFill != nil {
		hub.MixedEntries = ptr(mixEntries(list, vod, mixedCap(n)))
	} else if len(list) == 0 {
		return Hub{}, false, nil // nothing older apps can show
	}
	return hub, true, nil
}

// HomeHub is one Home row in full, for See all.
func (h *Handlers) HomeHub(ctx context.Context, req HomeHubRequestObject) (HomeHubResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return HomeHub401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	n := 100
	set(&n, req.Params.Limit)
	n = min(max(n, 1), 200)
	mixed := req.Params.Mixed != nil && *req.Params.Mixed
	notFound := HomeHub404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "no such Home row"))}
	home, err := h.homeLibraries(ctx)
	if err != nil {
		return nil, internal(ctx, "homeHub", err)
	}
	r, found, err := h.newMoreRows(ctx, home, n).direct(req.HubId)
	if err != nil {
		return nil, internal(ctx, "homeHub", err)
	}
	if !found {
		rows, err := h.homeRows(ctx, s.User.ID, []savedRow{{ID: req.HubId}}, n)
		if err != nil {
			return nil, internal(ctx, "homeHub", err)
		}
	search:
		for _, row := range rows {
			if row.id == req.HubId && row.expand == nil {
				r, found = row, true
				break
			}
			if row.expand != nil {
				subs, err := row.expand()
				if err != nil {
					return nil, internal(ctx, "homeHub", err)
				}
				for _, sub := range subs {
					if sub.id == req.HubId {
						r, found = sub, true
						break search
					}
				}
			}
		}
	}
	if !found {
		return notFound, nil
	}
	var hub Hub
	switch {
	case r.vod:
		hub, ok, err = h.vodSuggestHub(ctx, r, n)
	case r.id == "continue-watching":
		hub, ok, err = h.fillHub(r, false, n)
	default:
		hub, ok, err = h.fillHub(r, mixed, n)
	}
	if err != nil {
		return nil, internal(ctx, "homeHub", err)
	}
	if !ok {
		// An empty row: shown as empty, not missing.
		hub = Hub{Id: r.id, Title: r.title, LibraryId: nz(r.lib), Items: []ItemSummary{}}
		if mixed && r.vodFill != nil {
			hub.MixedEntries = &[]HomeEntry{}
		}
	}
	return HomeHub200JSONResponse(hub), nil
}
