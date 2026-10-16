package api

import (
	"context"
	"errors"
	"strconv"
	"time"

	"marquee/internal/vod"
)

// Continue Watching with films and episodes on demand beside the library's titles, the
// whole list a page at a time, and removing on-demand titles from it.

// homeContinueSize is how many entries Home's Continue Watching row holds.
const homeContinueSize = 20

// homeLibraries are the libraries the caller sees on Home.
func (h *Handlers) homeLibraries(ctx context.Context) ([]int64, error) {
	libs, err := h.Libraries.List(ctx)
	if err != nil {
		return nil, err
	}
	home := []int64{} // none means none, not all
	for _, l := range libs {
		if h.canSeeLibrary(ctx, l.ID) && (l.Options.IncludeInHome == nil || *l.Options.IncludeInHome) {
			home = append(home, l.ID)
		}
	}
	return home, nil
}

type continueEntry struct {
	entry ContinueEntry
	when  time.Time
}

func libraryTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

// continueEntries is the caller's whole Continue Watching, newest first: the library's
// titles in the Home libraries, and films and episodes on demand when they may watch them.
func (h *Handlers) continueEntries(ctx context.Context, home []int64) ([]ContinueEntry, error) {
	lib, err := h.Items.ContinueWatchingAll(ctx, access(ctx), home)
	if err != nil {
		return nil, err
	}
	all := make([]continueEntry, 0, len(lib))
	for _, c := range lib {
		it := toAPISummary(c.Item)
		when := libraryTime(c.When)
		all = append(all, continueEntry{ContinueEntry{Kind: ContinueEntryKindItem, Key: "item:" + strconv.FormatInt(c.Item.ID, 10),
			Item: &it, PositionMs: c.Item.ViewOffsetMS, DurationMs: c.Item.DurationMS, LastWatchedAt: when}, when})
	}
	if s, maxRating, _, allowed := h.vodSession(ctx); allowed {
		list, err := h.VOD.ContinueWatchingAll(ctx, s.User.ID, maxRating)
		if err != nil {
			return nil, err
		}
		for _, p := range list {
			all = append(all, continueEntry{toAPIContinueVod(p), p.UpdatedAt})
		}
	}
	// Newest first (stable: each list is already in order).
	for i := 1; i < len(all); i++ {
		for j := i; j > 0 && all[j].when.After(all[j-1].when); j-- {
			all[j], all[j-1] = all[j-1], all[j]
		}
	}
	out := make([]ContinueEntry, len(all))
	for i, e := range all {
		out[i] = e.entry
	}
	return out, nil
}

func toAPIContinueVod(p vod.InProgress) ContinueEntry {
	e := ContinueEntry{PositionMs: p.PositionMS, DurationMs: p.DurationMS, LastWatchedAt: p.UpdatedAt}
	if p.Movie != nil {
		m := toAPIVodMovie(*p.Movie)
		e.Kind, e.Key, e.VodMovie = ContinueEntryKindVodMovie, "vodMovie:"+strconv.FormatInt(p.Movie.ID, 10), &m
		return e
	}
	ep := toAPIVodEpisode(*p.Episode)
	e.Kind, e.Key, e.VodEpisode = ContinueEntryKindVodEpisode, "vodEpisode:"+strconv.FormatInt(p.Episode.ID, 10), &ep
	if p.SeriesHasPoster {
		e.SeriesPosterUrl = ptr(vodSeriesImageURL(p.Episode.SeriesID, "poster"))
	}
	return e
}

func (h *Handlers) ContinueWatching(ctx context.Context, req ContinueWatchingRequestObject) (ContinueWatchingResponseObject, error) {
	if _, ok := session(ctx); !ok {
		return ContinueWatching401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	home, err := h.homeLibraries(ctx)
	if err != nil {
		return nil, internal(ctx, "continueWatching", err)
	}
	all, err := h.continueEntries(ctx, home)
	if err != nil {
		return nil, internal(ctx, "continueWatching", err)
	}
	offset, limit := 0, 50
	if req.Params.Offset != nil && *req.Params.Offset > 0 {
		offset = *req.Params.Offset
	}
	if req.Params.Limit != nil && *req.Params.Limit > 0 {
		limit = min(*req.Params.Limit, 200)
	}
	page := []ContinueEntry{}
	if offset < len(all) {
		page = all[offset:min(offset+limit, len(all))]
	}
	return ContinueWatching200JSONResponse{Total: len(all), Entries: page}, nil
}

func (h *Handlers) RemoveVodFromContinueWatching(ctx context.Context, req RemoveVodFromContinueWatchingRequestObject) (RemoveVodFromContinueWatchingResponseObject, error) {
	s, maxRating, authed, allowed := h.vodSession(ctx)
	switch {
	case !authed:
		return RemoveVodFromContinueWatching401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !allowed:
		return RemoveVodFromContinueWatching403JSONResponse{ForbiddenJSONResponse(errVodNotAllowed)}, nil
	}
	var err error
	switch req.Kind {
	case "movie":
		var m *vod.Movie
		if m, err = h.vodMovie(ctx, s, maxRating, req.VodId); err == nil {
			if m == nil {
				return RemoveVodFromContinueWatching404JSONResponse{NotFoundJSONResponse(errVodNotFound)}, nil
			}
			err = h.VOD.HideMovieFromContinue(ctx, s.User.ID, req.VodId)
		}
	case "episode":
		var e *vod.Episode
		if e, err = h.vodEpisode(ctx, s, maxRating, req.VodId); err == nil {
			if e == nil {
				return RemoveVodFromContinueWatching404JSONResponse{NotFoundJSONResponse(errVodEpisodeNotFound)}, nil
			}
			err = h.VOD.HideEpisodeFromContinue(ctx, s.User.ID, req.VodId)
		}
	default:
		return RemoveVodFromContinueWatching404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "kind is movie or episode"))}, nil
	}
	switch {
	case errors.Is(err, vod.ErrNotFound):
		return RemoveVodFromContinueWatching404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "not found"))}, nil
	case err != nil:
		return nil, internal(ctx, "removeVodFromContinueWatching", err)
	}
	return RemoveVodFromContinueWatching204Response{}, nil
}

// continueHub fills Home's Continue Watching hub: items (library only, as before) and the
// merged entries with the total.
func (h *Handlers) continueHub(ctx context.Context, r homeRow, home []int64) (Hub, bool, error) {
	list, err := h.Items.ContinueWatching(ctx, access(ctx), home, homeContinueSize)
	if err != nil {
		return Hub{}, false, err
	}
	all, err := h.continueEntries(ctx, home)
	if err != nil {
		return Hub{}, false, err
	}
	if len(list) == 0 && len(all) == 0 {
		return Hub{}, false, nil
	}
	hub := Hub{Id: r.id, Title: r.title, Items: make([]ItemSummary, len(list)), Total: ptr(len(all))}
	for i, it := range list {
		hub.Items[i] = toAPISummary(it)
	}
	entries := all[:min(len(all), homeContinueSize)]
	hub.Entries = &entries
	return hub, true, nil
}
