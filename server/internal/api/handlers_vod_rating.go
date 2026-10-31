package api

import (
	"context"
	"errors"
	"log/slog"

	"marquee/internal/items"
	"marquee/internal/vod"
	"marquee/internal/vodsave"
)

// ---------- Ratings of films and series on demand (LIVE-6) ----------

// rateVod sets the caller's rating of a film (series false) or series; found is false when
// there's no such title for them.
func (h *Handlers) rateVod(ctx context.Context, series bool, id int64, rating *float32) (found, authed, allowed bool, err error) {
	s, maxRating, authed, allowed := h.vodSession(ctx)
	if !authed || !allowed {
		return false, authed, allowed, nil
	}
	kind := "movie"
	if series {
		kind = "series"
		m, err := h.vodSeries(ctx, s, maxRating, id)
		if err != nil || m == nil {
			return false, true, true, err
		}
	} else if m, err := h.vodMovie(ctx, s, maxRating, id); err != nil || m == nil {
		return false, true, true, err
	}
	var r *float64
	if rating != nil {
		r = ptr(float64(*rating))
	}
	if err := h.VOD.SetRating(ctx, s.User.ID, kind, id, r); err != nil {
		return true, true, true, err
	}
	if h.VODSuggest != nil {
		h.VODSuggest.Forget(s.User.ID) // their suggestions follow the rating
	}
	return true, true, true, nil
}

func (h *Handlers) RateVodMovie(ctx context.Context, req RateVodMovieRequestObject) (RateVodMovieResponseObject, error) {
	found, authed, allowed, err := h.rateVod(ctx, false, req.MovieId, req.Body.Rating)
	switch {
	case !authed:
		return RateVodMovie401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !allowed:
		return RateVodMovie403JSONResponse{ForbiddenJSONResponse(errVodNotAllowed)}, nil
	case errors.Is(err, vod.ErrBadRating):
		return RateVodMovie400JSONResponse{BadRequestJSONResponse(apiErr("bad_request", err.Error()))}, nil
	case err != nil:
		return nil, internal(ctx, "rateVodMovie", err)
	case !found:
		return RateVodMovie404JSONResponse{NotFoundJSONResponse(errVodNotFound)}, nil
	}
	return RateVodMovie204Response{}, nil
}

func (h *Handlers) RateVodSeries(ctx context.Context, req RateVodSeriesRequestObject) (RateVodSeriesResponseObject, error) {
	found, authed, allowed, err := h.rateVod(ctx, true, req.SeriesId, req.Body.Rating)
	switch {
	case !authed:
		return RateVodSeries401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !allowed:
		return RateVodSeries403JSONResponse{ForbiddenJSONResponse(errVodNotAllowed)}, nil
	case errors.Is(err, vod.ErrBadRating):
		return RateVodSeries400JSONResponse{BadRequestJSONResponse(apiErr("bad_request", err.Error()))}, nil
	case err != nil:
		return nil, internal(ctx, "rateVodSeries", err)
	case !found:
		return RateVodSeries404JSONResponse{NotFoundJSONResponse(errVodSeriesNotFound)}, nil
	}
	return RateVodSeries204Response{}, nil
}

// vodTaste is what uid watched and rated on demand, for the library's recommendations: kept
// by TMDB id, so titles that left their source still count, whether or not VOD is on now.
func (h *Handlers) vodTaste(ctx context.Context, uid int64) items.VODTaste {
	var out items.VODTaste
	mems, err := vod.Memories(ctx, h.DB, uid)
	if err != nil {
		slog.WarnContext(ctx, "vod taste", "err", err)
		return out
	}
	for _, m := range mems {
		if m.TMDBID <= 0 {
			continue
		}
		ref := items.TMDBRef{Type: "movie", ID: m.TMDBID}
		finished, started := m.Finished, false
		if m.Kind == "series" {
			ref.Type = "show"
			finished, started = m.Episodes >= 3 || (m.Total > 0 && m.Episodes >= m.Total), m.Episodes > 0
		}
		out.Seen = append(out.Seen, ref)
		w := 0.0
		switch {
		case m.Rating > 0 && m.Rating < 5:
			continue // turned away from: only not recommended
		case m.Rating > 0:
			w = m.Rating / 7
		case finished:
			w = 1
		case started:
			w = 0.6
		default:
			continue
		}
		title, _ := vodsave.CleanTitle(m.Title) // "EN - Heat (1995)" reads "Heat"
		out.Liked = append(out.Liked, items.VODLike{TMDBRef: ref, Title: title, Weight: w, Recs: m.Recs})
	}
	return out
}
