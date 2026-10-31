package api

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"

	"marquee/internal/auth"
	"marquee/internal/items"
	"marquee/internal/muse"
	"marquee/internal/vodsave"
	"marquee/internal/vodsuggest"
)

// Suggestions from the films and series on demand (LIVE-6, USER-16): Home's Watch on VOD
// row, Also on VOD on item pages, Muse, Discover's Watch now on VOD and the on-demand
// pages' Recommended for you.

// vodSuggestRow is Home's Watch on VOD row.
const (
	vodSuggestRow     = "vod-suggestions"
	vodSuggestRowSize = 20
)

// suggestSession is the signed-in user as suggestions see them, when suggestions are on
// and on demand is theirs to watch.
func (h *Handlers) suggestSession(ctx context.Context) (u vodsuggest.User, s auth.Session, authed, allowed bool) {
	s, maxRating, authed, allowed := h.vodSession(ctx)
	if !allowed || h.VODSuggest == nil {
		return u, s, authed, false
	}
	acc := userAccess(s.User)
	return vodsuggest.User{ID: s.User.ID, Access: acc, MaxRating: maxRating,
		Visibility: vodsave.Visibility{LibraryIDs: acc.LibraryIDs, HideRecordings: acc.HideRecordings}}, s, authed, true
}

// toAPIVodSuggestions loads the films and series of suggestions.
func (h *Handlers) toAPIVodSuggestions(ctx context.Context, uid int64, list []vodsuggest.Suggestion) ([]VodSuggestion, error) {
	var movieIDs, seriesIDs []int64
	for _, sg := range list {
		if sg.Kind == vodsuggest.Series {
			seriesIDs = append(seriesIDs, sg.ID)
		} else {
			movieIDs = append(movieIDs, sg.ID)
		}
	}
	movies, err := h.VOD.MoviesByID(ctx, movieIDs, uid)
	if err != nil {
		return nil, err
	}
	series, err := h.VOD.SeriesByID(ctx, seriesIDs, uid)
	if err != nil {
		return nil, err
	}
	out := make([]VodSuggestion, 0, len(list))
	for _, sg := range list {
		v := VodSuggestion{Reason: nz(sg.Reason), Score: ptr(float32(sg.Score))}
		if sg.Kind == vodsuggest.Series {
			m, ok := series[sg.ID]
			if !ok {
				continue
			}
			api := toAPIVodSeries(m)
			api.InLibrary = ptr(false)
			v.Kind, v.Key, v.Series = VodSuggestionKindSeries, "series:"+itoa(sg.ID), &api
		} else {
			m, ok := movies[sg.ID]
			if !ok {
				continue
			}
			api := toAPIVodMovie(m)
			api.InLibrary = ptr(false)
			v.Kind, v.Key, v.Movie = VodSuggestionKindMovie, "movie:"+itoa(sg.ID), &api
		}
		out = append(out, v)
	}
	return out, nil
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

func (h *Handlers) VodSuggestions(ctx context.Context, req VodSuggestionsRequestObject) (VodSuggestionsResponseObject, error) {
	if _, _, authed, allowed := h.vodSession(ctx); !authed {
		return VodSuggestions401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	} else if !allowed {
		return VodSuggestions403JSONResponse{ForbiddenJSONResponse(errVodNotAllowed)}, nil
	}
	u, s, _, ok := h.suggestSession(ctx)
	if !ok {
		return VodSuggestions200JSONResponse{}, nil // suggestions aren't set up
	}
	limit, kind := 20, ""
	set(&limit, req.Params.Limit)
	limit = min(max(limit, 1), 100)
	if req.Params.Kind != nil {
		kind = string(*req.Params.Kind)
	}
	list, err := h.VODSuggest.For(ctx, u, kind, limit)
	if err != nil {
		return nil, internal(ctx, "vodSuggestions", err)
	}
	out, err := h.toAPIVodSuggestions(ctx, s.User.ID, list)
	if err != nil {
		return nil, internal(ctx, "vodSuggestions", err)
	}
	return VodSuggestions200JSONResponse(out), nil
}

func (h *Handlers) HideVodSuggestion(ctx context.Context, req HideVodSuggestionRequestObject) (HideVodSuggestionResponseObject, error) {
	if _, _, authed, allowed := h.vodSession(ctx); !authed {
		return HideVodSuggestion401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	} else if !allowed {
		return HideVodSuggestion403JSONResponse{ForbiddenJSONResponse(errVodNotAllowed)}, nil
	}
	u, _, _, ok := h.suggestSession(ctx)
	if !ok {
		return HideVodSuggestion404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "not found"))}, nil
	}
	ok, err := h.VODSuggest.Hide(ctx, u.ID, string(req.Kind), req.VodId)
	if err != nil {
		return nil, internal(ctx, "hideVodSuggestion", err)
	}
	if !ok {
		return HideVodSuggestion404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "not found"))}, nil
	}
	return HideVodSuggestion204Response{}, nil
}

func (h *Handlers) ItemVodSimilar(ctx context.Context, req ItemVodSimilarRequestObject) (ItemVodSimilarResponseObject, error) {
	if _, ok := session(ctx); !ok {
		return ItemVodSimilar401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	it, found, err := h.Items.TasteOf(ctx, access(ctx), req.ItemId)
	if err != nil {
		return nil, internal(ctx, "itemVodSimilar", err)
	}
	if !found {
		// Not a movie or show the caller can see: 404 only when it isn't theirs to open at all.
		if _, err := h.Items.Get(ctx, itemAccess(ctx), req.ItemId, false); errors.Is(err, items.ErrNotFound) {
			return ItemVodSimilar404JSONResponse{NotFoundJSONResponse(apiErr("not_found", err.Error()))}, nil
		}
		return ItemVodSimilar200JSONResponse{}, nil
	}
	u, s, _, allowed := h.suggestSession(ctx)
	if !allowed {
		return ItemVodSimilar200JSONResponse{}, nil
	}
	limit := 20
	set(&limit, req.Params.Limit)
	limit = min(max(limit, 1), 50)
	list, err := h.VODSuggest.Similar(ctx, u, it, limit)
	if err != nil {
		return nil, internal(ctx, "itemVodSimilar", err)
	}
	out, err := h.toAPIVodSuggestions(ctx, s.User.ID, list)
	if err != nil {
		return nil, internal(ctx, "itemVodSimilar", err)
	}
	return ItemVodSimilar200JSONResponse(out), nil
}

// vodSuggestHub is Home's Watch on VOD row; ok is false when it has nothing (or the caller
// can't watch on demand).
func (h *Handlers) vodSuggestHub(ctx context.Context, r homeRow, n int) (Hub, bool, error) {
	u, s, _, allowed := h.suggestSession(ctx)
	if !allowed {
		return Hub{}, false, nil
	}
	list, err := h.VODSuggest.For(ctx, u, "", n)
	if err != nil || len(list) == 0 {
		return Hub{}, false, err
	}
	entries, err := h.toAPIVodSuggestions(ctx, s.User.ID, list)
	if err != nil || len(entries) == 0 {
		return Hub{}, false, err
	}
	return Hub{Id: r.id, Title: r.title, Items: []ItemSummary{}, VodEntries: &entries}, true, nil
}

// museVod is Muse's on-demand matches for a reading of the prompt (includeVod).
func (h *Handlers) museVod(ctx context.Context, prompt string, q items.VideoQuery, rerank bool, limit int) ([]VodSuggestion, error) {
	u, s, _, allowed := h.suggestSession(ctx)
	if !allowed {
		return nil, nil
	}
	mq := vodsuggest.MuseQuery{YearFrom: q.YearFrom, YearTo: q.YearTo, Genres: q.Genres, ExcludeGenres: q.ExcludeGenres,
		MinRating: q.MinRating, MaxRating: q.MaxRating, MaxMinutes: q.MaxMinutes, MinMinutes: q.MinMinutes,
		Keywords: q.Keywords, Words: q.Rest}
	if q.Family && mq.MaxRating == "" {
		mq.MaxRating = "PG"
	}
	for _, t := range q.Types {
		if t == "show" {
			mq.Kinds = append(mq.Kinds, vodsuggest.Series)
		} else {
			mq.Kinds = append(mq.Kinds, vodsuggest.Movie)
		}
	}
	for _, id := range q.Seeds {
		if t, ok, err := h.Items.TasteOf(ctx, access(ctx), id); err == nil && ok {
			mq.Seeds = append(mq.Seeds, t)
		}
	}
	fetch := limit
	if rerank {
		fetch = max(limit, rerankTop)
	}
	list, err := h.VODSuggest.Muse(ctx, u, mq, fetch)
	if err != nil {
		return nil, err
	}
	out, err := h.toAPIVodSuggestions(ctx, s.User.ID, list)
	if err != nil {
		return nil, err
	}
	if rerank && len(out) > 1 {
		out = h.rerankVod(ctx, prompt, mq, out)
	}
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// rerankVod has the chat model re-rank Muse's on-demand matches (with their overviews), as
// it does the library's; on failure the order stays.
func (h *Handlers) rerankVod(ctx context.Context, prompt string, mq vodsuggest.MuseQuery, list []VodSuggestion) []VodSuggestion {
	top := list[:min(rerankTop, len(list))]
	cands := make([]muse.Candidate, len(top))
	byID := map[int64]VodSuggestion{}
	for i, v := range top {
		// Films and series share one numbering: films even, series odd.
		var id int64
		var title string
		var year int
		var overview string
		if v.Movie != nil {
			id, title = v.Movie.Id*2, v.Movie.Title
			if v.Movie.Year != nil {
				year = *v.Movie.Year
			}
			if m, err := h.VOD.Movie(ctx, v.Movie.Id, 0); err == nil {
				overview = m.Overview
			}
		} else if v.Series != nil {
			id, title = v.Series.Id*2+1, v.Series.Title
			if v.Series.Year != nil {
				year = *v.Series.Year
			}
			if m, err := h.VOD.OneSeries(ctx, v.Series.Id, 0); err == nil {
				overview = m.Overview
			}
		}
		cands[i] = muse.Candidate{ID: id, Title: title, Year: year, Synopsis: overview}
		byID[id] = v
	}
	var seeds []muse.Candidate
	for _, sd := range mq.Seeds {
		seeds = append(seeds, muse.Candidate{ID: -sd.ID, Title: sd.Title, Year: sd.Year})
	}
	order, why, err := muse.Rerank(ctx, h.AI, prompt, seeds, cands)
	if err != nil {
		slog.WarnContext(ctx, "muse on demand: re-ranking failed; keeping the ranking", "err", err)
		return list
	}
	out := make([]VodSuggestion, 0, len(list))
	for _, id := range order {
		v, ok := byID[id]
		if !ok {
			continue
		}
		if r, ok := why[id]; ok && r != "" {
			v.Reason = ptr(r)
		}
		out = append(out, v)
	}
	return append(out, list[len(top):]...)
}

// markOnDemand sets vod on Discover results the caller can watch on demand, unless the
// library already has all of it (a show missing episodes still gets it).
func (h *Handlers) markOnDemand(ctx context.Context, page *DiscoverPage) {
	_, maxRating, _, allowed := h.vodSession(ctx)
	if !allowed || h.VODSuggest == nil || len(page.Results) == 0 {
		return
	}
	var movies, shows []int64
	for _, d := range page.Results {
		if d.MediaType == DiscoverItemMediaTypeTv {
			shows = append(shows, d.TmdbId)
		} else {
			movies = append(movies, d.TmdbId)
		}
	}
	m, t, err := h.VODSuggest.OnDemand(ctx, maxRating, movies, shows)
	if err != nil {
		slog.WarnContext(ctx, "discover: on demand", "err", err)
		return
	}
	for i, d := range page.Results {
		if d.Availability == AvailabilityAvailable {
			continue
		}
		if d.MediaType == DiscoverItemMediaTypeTv {
			if id, ok := t[d.TmdbId]; ok {
				page.Results[i].Vod = &VodRef{Kind: VodRefKindSeries, Id: id}
			}
		} else if id, ok := m[d.TmdbId]; ok {
			page.Results[i].Vod = &VodRef{Kind: VodRefKindMovie, Id: id}
		}
		if v := page.Results[i].Vod; v != nil {
			h.markSavable(ctx, v)
		}
	}
}

// markSavable tells an admin (or a user allowed VOD downloads) whether Discover's title on
// demand can be queued in the VOD download tool, to its default folder (a film's latest save
// shows its state).
func (h *Handlers) markSavable(ctx context.Context, v *VodRef) {
	s, ok := session(ctx)
	if !ok || !(s.User.IsAdmin || s.User.Restrictions.VODDownloads) || h.VODSaves == nil || h.VOD == nil {
		return
	}
	movies, shows := h.VODSaves.Configured()
	table, configured := "vod_movies", movies
	if v.Kind == VodRefKindSeries {
		table, configured = "vod_series", shows
	}
	var source, key string
	if !configured || h.DB.QueryRowContext(ctx, `SELECT source_id, source_key FROM `+table+` WHERE id = ?`, v.Id).Scan(&source, &key) != nil {
		return
	}
	// As the download tool checks: an Xtream Codes source, and for a film one of its own.
	v.CanSave = ptr(h.VOD.Savable(source) && (v.Kind == VodRefKindSeries || strings.HasPrefix(key, "xtream:")))
	if *v.CanSave && v.Kind == VodRefKindMovie {
		if st, err := h.VODSaves.MovieState(ctx, v.Id); err == nil {
			v.Save = toAPIVodSaveState(st)
		}
	}
}

func (h *Handlers) VodRecommendations(ctx context.Context, req VodRecommendationsRequestObject) (VodRecommendationsResponseObject, error) {
	if _, _, authed, allowed := h.vodSession(ctx); !authed {
		return VodRecommendations401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	} else if !allowed {
		return VodRecommendations403JSONResponse{ForbiddenJSONResponse(errVodNotAllowed)}, nil
	}
	u, s, _, ok := h.suggestSession(ctx)
	if !ok {
		return VodRecommendations200JSONResponse{}, nil // suggestions aren't set up
	}
	perRow, kind := 30, ""
	set(&perRow, req.Params.PerRow)
	perRow = min(max(perRow, 4), 60)
	if req.Params.Kind != nil {
		kind = string(*req.Params.Kind)
	}
	rows, err := h.VODSuggest.Rows(ctx, u, kind, perRow)
	if err != nil {
		return nil, internal(ctx, "vodRecommendations", err)
	}
	// Load every row's titles at once, then share them out.
	var all []vodsuggest.Suggestion
	for _, r := range rows {
		all = append(all, r.Items...)
	}
	loaded, err := h.toAPIVodSuggestions(ctx, s.User.ID, all)
	if err != nil {
		return nil, internal(ctx, "vodRecommendations", err)
	}
	byKey := make(map[string]VodSuggestion, len(loaded))
	for _, v := range loaded {
		byKey[v.Key] = v
	}
	out := VodRecommendations200JSONResponse{}
	for _, r := range rows {
		row := VodRecommendationRow{Id: r.ID, Title: r.Title, Entries: []VodSuggestion{}}
		for _, sg := range r.Items {
			k := "movie:" + itoa(sg.ID)
			if sg.Kind == vodsuggest.Series {
				k = "series:" + itoa(sg.ID)
			}
			if v, ok := byKey[k]; ok {
				v.Reason = nz(sg.Reason)
				row.Entries = append(row.Entries, v)
			}
		}
		if len(row.Entries) > 0 {
			out = append(out, row)
		}
	}
	return out, nil
}
