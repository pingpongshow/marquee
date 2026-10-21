package api

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"

	"marquee/internal/items"
	"marquee/internal/vodsuggest"
)

// More Home rows (USER-12): holidays, top rated, trending and genre rows, each with films
// and series on demand mixed in for apps that ask (mixed=true).

const (
	moreRowSize  = 20 // library titles per row
	mixedRowSize = 30 // library and on-demand titles together
	genreRows    = 3
)

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

// moreHomeRows are the extra suggestion rows, in their default order after Because You Watched.
func (h *Handlers) moreHomeRows(ctx context.Context, home []int64) []homeRow {
	acc := access(ctx)
	home = append([]int64{}, home...) // none means none, not all
	// The on-demand recommendation rows are worked out once per request, when needed.
	vodRows := sync.OnceValues(func() (map[string][]vodsuggest.Suggestion, error) {
		u, _, _, allowed := h.suggestSession(ctx)
		if !allowed {
			return nil, nil
		}
		rows, err := h.VODSuggest.Rows(ctx, u, "", moreRowSize)
		out := map[string][]vodsuggest.Suggestion{}
		for _, r := range rows {
			out[r.ID] = r.Items
		}
		return out, err
	})
	vodRow := func(id string) func() ([]VodSuggestion, error) {
		return func() ([]VodSuggestion, error) {
			rows, err := vodRows()
			if err != nil || len(rows[id]) == 0 {
				return nil, err
			}
			return h.toAPIVodSuggestions(ctx, access(ctx).UserID, rows[id])
		}
	}
	return []homeRow{
		{id: "holiday", title: "Holiday Picks", expand: func() ([]homeRow, error) {
			var out []homeRow
			for _, hd := range items.HolidaysOn(time.Now()) {
				out = append(out, homeRow{id: "holiday-" + hd.ID, title: hd.Title,
					fill: func() ([]items.Summary, error) { return h.Items.HolidayVideos(ctx, acc, hd, home, moreRowSize) },
					vodFill: func() ([]VodSuggestion, error) {
						return h.vodMuse(ctx, vodsuggest.MuseQuery{Keywords: hd.Keywords, Words: hd.ID})
					}})
			}
			return out, nil
		}},
		{id: "top-rated", title: "Top Rated", fill: func() ([]items.Summary, error) { return h.Items.TopRated(ctx, acc, home, moreRowSize) },
			vodFill: vodRow("acclaimed")},
		{id: "trending", title: "Trending Now", fill: func() ([]items.Summary, error) { return h.Items.Trending(ctx, acc, home, moreRowSize) },
			vodFill: vodRow("popular")},
		{id: "genres", title: "Your Genres", expand: func() ([]homeRow, error) {
			genres, err := h.Items.TasteGenres(ctx, acc, home, 2*genreRows)
			if err != nil || len(genres) == 0 {
				return nil, err
			}
			all, err := h.Items.VideoGenres(ctx)
			if err != nil {
				return nil, err
			}
			// A different few each day, from the genres they watch most.
			start := time.Now().YearDay() % len(genres)
			var out []homeRow
			for i := range min(genreRows, len(genres)) {
				g := genres[(start+i)%len(genres)]
				group := items.GenreGroup(g, all)
				if len(group) == 0 {
					group = []string{g}
				}
				out = append(out, homeRow{id: "genre-" + strings.ReplaceAll(strings.ToLower(g), " ", "-"), title: genreTitle(g),
					fill: func() ([]items.Summary, error) { return h.Items.GenreVideos(ctx, acc, g, home, moreRowSize) },
					vodFill: func() ([]VodSuggestion, error) {
						return h.vodMuse(ctx, vodsuggest.MuseQuery{Genres: [][]string{group}})
					}})
			}
			return out, nil
		}},
	}
}

// vodMuse are on-demand titles for q, best first (none when on demand isn't the caller's).
func (h *Handlers) vodMuse(ctx context.Context, q vodsuggest.MuseQuery) ([]VodSuggestion, error) {
	u, s, _, allowed := h.suggestSession(ctx)
	if !allowed {
		return nil, nil
	}
	list, err := h.VODSuggest.Muse(ctx, u, q, moreRowSize)
	if err != nil || len(list) == 0 {
		return nil, err
	}
	return h.toAPIVodSuggestions(ctx, s.User.ID, list)
}

// mixEntries puts library items and on-demand titles in one row: two from the library to
// each one on demand, then whichever is left, up to mixedRowSize.
func mixEntries(lib []items.Summary, vod []VodSuggestion) []HomeEntry {
	out := make([]HomeEntry, 0, min(len(lib)+len(vod), mixedRowSize))
	i, j := 0, 0
	for len(out) < mixedRowSize && (i < len(lib) || j < len(vod)) {
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
