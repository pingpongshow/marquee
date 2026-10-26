package api

import (
	"context"
	"sort"
	"time"

	"marquee/internal/items"
	"marquee/internal/vod"
	"marquee/internal/vodsuggest"
)

// ---------- Films and series on demand on the watchlist (USER-8, LIVE-6) ----------

// watchVodMovie puts a film on the caller's watchlist or takes it off; found is false when
// there's no such film for them.
func (h *Handlers) watchVodMovie(ctx context.Context, id int64, on bool) (found, authed, allowed bool, err error) {
	s, maxRating, authed, allowed := h.vodSession(ctx)
	if !authed || !allowed {
		return false, authed, allowed, nil
	}
	m, err := h.vodMovie(ctx, s, maxRating, id)
	if err != nil || m == nil {
		return false, true, true, err
	}
	return true, true, true, h.VOD.SetWatchlisted(ctx, s.User.ID, false, m.ID, on)
}

func (h *Handlers) watchVodSeries(ctx context.Context, id int64, on bool) (found, authed, allowed bool, err error) {
	s, maxRating, authed, allowed := h.vodSession(ctx)
	if !authed || !allowed {
		return false, authed, allowed, nil
	}
	m, err := h.vodSeries(ctx, s, maxRating, id)
	if err != nil || m == nil {
		return false, true, true, err
	}
	return true, true, true, h.VOD.SetWatchlisted(ctx, s.User.ID, true, m.ID, on)
}

func (h *Handlers) AddVodMovieToWatchlist(ctx context.Context, req AddVodMovieToWatchlistRequestObject) (AddVodMovieToWatchlistResponseObject, error) {
	found, authed, allowed, err := h.watchVodMovie(ctx, req.MovieId, true)
	switch {
	case !authed:
		return AddVodMovieToWatchlist401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !allowed:
		return AddVodMovieToWatchlist403JSONResponse{ForbiddenJSONResponse(errVodNotAllowed)}, nil
	case err != nil:
		return nil, internal(ctx, "addVodMovieToWatchlist", err)
	case !found:
		return AddVodMovieToWatchlist404JSONResponse{NotFoundJSONResponse(errVodNotFound)}, nil
	}
	return AddVodMovieToWatchlist204Response{}, nil
}

func (h *Handlers) RemoveVodMovieFromWatchlist(ctx context.Context, req RemoveVodMovieFromWatchlistRequestObject) (RemoveVodMovieFromWatchlistResponseObject, error) {
	found, authed, allowed, err := h.watchVodMovie(ctx, req.MovieId, false)
	switch {
	case !authed:
		return RemoveVodMovieFromWatchlist401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !allowed:
		return RemoveVodMovieFromWatchlist403JSONResponse{ForbiddenJSONResponse(errVodNotAllowed)}, nil
	case err != nil:
		return nil, internal(ctx, "removeVodMovieFromWatchlist", err)
	case !found:
		return RemoveVodMovieFromWatchlist404JSONResponse{NotFoundJSONResponse(errVodNotFound)}, nil
	}
	return RemoveVodMovieFromWatchlist204Response{}, nil
}

func (h *Handlers) AddVodSeriesToWatchlist(ctx context.Context, req AddVodSeriesToWatchlistRequestObject) (AddVodSeriesToWatchlistResponseObject, error) {
	found, authed, allowed, err := h.watchVodSeries(ctx, req.SeriesId, true)
	switch {
	case !authed:
		return AddVodSeriesToWatchlist401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !allowed:
		return AddVodSeriesToWatchlist403JSONResponse{ForbiddenJSONResponse(errVodNotAllowed)}, nil
	case err != nil:
		return nil, internal(ctx, "addVodSeriesToWatchlist", err)
	case !found:
		return AddVodSeriesToWatchlist404JSONResponse{NotFoundJSONResponse(errVodSeriesNotFound)}, nil
	}
	return AddVodSeriesToWatchlist204Response{}, nil
}

func (h *Handlers) RemoveVodSeriesFromWatchlist(ctx context.Context, req RemoveVodSeriesFromWatchlistRequestObject) (RemoveVodSeriesFromWatchlistResponseObject, error) {
	found, authed, allowed, err := h.watchVodSeries(ctx, req.SeriesId, false)
	switch {
	case !authed:
		return RemoveVodSeriesFromWatchlist401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !allowed:
		return RemoveVodSeriesFromWatchlist403JSONResponse{ForbiddenJSONResponse(errVodNotAllowed)}, nil
	case err != nil:
		return nil, internal(ctx, "removeVodSeriesFromWatchlist", err)
	case !found:
		return RemoveVodSeriesFromWatchlist404JSONResponse{NotFoundJSONResponse(errVodSeriesNotFound)}, nil
	}
	return RemoveVodSeriesFromWatchlist204Response{}, nil
}

// vodWatchlist is the caller's films and series on demand on their watchlist, for the Your
// Watchlist row (mixedEntries), with when each was added (by key). Empty when on demand isn't
// theirs to watch; titles over their content rating are left out.
func (h *Handlers) vodWatchlist(ctx context.Context, n int) ([]VodSuggestion, map[string]time.Time, error) {
	s, maxRating, authed, allowed := h.vodSession(ctx)
	if !authed || !allowed {
		return nil, nil, nil
	}
	refs, err := h.VOD.Watchlist(ctx, s.User.ID, n)
	if err != nil || len(refs) == 0 {
		return nil, nil, err
	}
	list := make([]vodsuggest.Suggestion, len(refs))
	added := map[string]time.Time{}
	for i, r := range refs {
		kind, key := vodsuggest.Movie, "movie:"
		if r.Series {
			kind, key = vodsuggest.Series, "series:"
		}
		list[i] = vodsuggest.Suggestion{Kind: kind, ID: r.ID}
		added[key+itoa(r.ID)] = r.AddedAt
	}
	all, err := h.toAPIVodSuggestions(ctx, s.User.ID, list)
	if err != nil {
		return nil, nil, err
	}
	out := all[:0]
	for _, v := range all {
		rating := ""
		switch {
		case v.Movie != nil && v.Movie.ContentRating != nil:
			rating = *v.Movie.ContentRating
		case v.Series != nil && v.Series.ContentRating != nil:
			rating = *v.Series.ContentRating
		}
		if vod.Allowed(rating, maxRating) {
			v.Score = nil // not a suggestion
			out = append(out, v)
		}
	}
	return out, added, nil
}

// mergeWatchlist puts the library's watchlist and the on-demand one together, most recently
// added first (each list is already in that order; a tie keeps the library's first).
func mergeWatchlist(lib []items.Summary, libAdded map[int64]time.Time, vodList []VodSuggestion, vodAdded map[string]time.Time, max int) []HomeEntry {
	type entry struct {
		at time.Time
		e  HomeEntry
	}
	all := make([]entry, 0, len(lib)+len(vodList))
	for _, it := range lib {
		s := toAPISummary(it)
		all = append(all, entry{at: libAdded[it.ID], e: HomeEntry{Kind: HomeEntryKindItem, Key: "item:" + itoa(it.ID), Item: &s}})
	}
	for _, v := range vodList {
		v := v
		all = append(all, entry{at: vodAdded[v.Key], e: HomeEntry{Kind: HomeEntryKindVod, Key: "vod:" + v.Key, Vod: &v}})
	}
	sort.SliceStable(all, func(a, b int) bool { return all[a].at.After(all[b].at) })
	out := make([]HomeEntry, 0, min(len(all), max))
	for _, e := range all {
		if len(out) == max {
			break
		}
		out = append(out, e.e)
	}
	return out
}

// watchlistRow is the Your Watchlist Home row: the library's titles (items), and with them the
// films and series on demand on it for apps that ask (mixed), in the order they were added.
func (h *Handlers) watchlistRow(ctx context.Context, acc items.Access, n int) homeRow {
	var vodAdded map[string]time.Time
	return homeRow{id: "watchlist", title: "Your Watchlist",
		fill: func() ([]items.Summary, error) { return h.Items.Watchlist(ctx, acc, n) },
		vodFill: func() ([]VodSuggestion, error) {
			list, added, err := h.vodWatchlist(ctx, n)
			vodAdded = added
			return list, err
		},
		merge: func(lib []items.Summary, vodList []VodSuggestion, max int) ([]HomeEntry, error) {
			ids := make([]int64, len(lib))
			for i, it := range lib {
				ids[i] = it.ID
			}
			libAdded, err := h.Items.WatchlistAdded(ctx, acc.UserID, ids)
			if err != nil {
				return nil, err
			}
			return mergeWatchlist(lib, libAdded, vodList, vodAdded, max), nil
		}}
}
