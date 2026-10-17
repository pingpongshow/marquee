package api

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"

	"marquee/internal/auth"
	"marquee/internal/netclass"
	"marquee/internal/playback"
	"marquee/internal/vod"
	"marquee/internal/vodsave"
)

// TV series on demand from Xtream Codes sources (LIVE-6), alongside the films.

var errVodSeriesNotFound = apiErr("not_found", "series not found")
var errVodEpisodeNotFound = apiErr("not_found", "episode not found")

func vodSeriesImageURL(id int64, kind string) string {
	return "/api/v1/livetv/vod/series/" + strconv.FormatInt(id, 10) + "/image?kind=" + kind
}

func toAPIVodEpisode(e vod.Episode) VodEpisode {
	out := VodEpisode{Id: e.ID, SeriesId: e.SeriesID, SeriesTitle: e.SeriesTitle, Season: e.Season, Episode: e.Episode,
		Title: e.Title, Overview: nz(e.Overview), AirDate: nz(e.AirDate), RuntimeMs: nz(e.RuntimeMS),
		PositionMs: nz(e.PositionMS), Watched: e.Watched}
	if e.Rating > 0 {
		out.Rating = ptr(float32(e.Rating))
	}
	if e.HasStill {
		out.StillUrl = ptr("/api/v1/livetv/vod/episodes/" + strconv.FormatInt(e.ID, 10) + "/image")
	}
	return out
}

func toAPIVodSeries(m vod.Series) VodSeries {
	out := VodSeries{Id: m.ID, Title: m.Title, Category: nz(m.Category), ContentRating: nz(m.ContentRating), Year: nz(m.Year),
		SeasonCount: m.Seasons, EpisodeCount: m.Episodes, WatchedCount: m.Watched, Watched: m.AllWatched()}
	if m.Rating > 0 {
		out.Rating = ptr(float32(m.Rating))
	}
	if m.HasPoster {
		out.PosterUrl = ptr(vodSeriesImageURL(m.ID, "poster"))
	}
	return out
}

func (h *Handlers) ListVodSeries(ctx context.Context, req ListVodSeriesRequestObject) (ListVodSeriesResponseObject, error) {
	s, maxRating, authed, allowed := h.vodSession(ctx)
	switch {
	case !authed:
		return ListVodSeries401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !allowed:
		return ListVodSeries403JSONResponse{ForbiddenJSONResponse(errVodNotAllowed)}, nil
	}
	f := vod.Filter{UserID: s.User.ID, MaxRating: maxRating}
	p := req.Params
	set(&f.Category, p.Category)
	set(&f.Query, p.Q)
	set(&f.Limit, p.Limit)
	set(&f.Offset, p.Offset)
	if p.Sort != nil {
		f.Sort = string(*p.Sort)
	}
	applyVodFilters(&f, p.Genre, p.YearFrom, p.YearTo, p.MinRating, (*string)(p.Watched), p.SourceId)
	idx, vis := h.vodInLibrary(ctx, s)
	applyInLibraryFilter(&f, p.InLibrary, true, idx, vis)
	list, total, err := h.VOD.SeriesList(ctx, f)
	if err != nil {
		return nil, internal(ctx, "listVodSeries", err)
	}
	out := ListVodSeries200JSONResponse{Total: total, Series: make([]VodSeries, len(list))}
	for i, m := range list {
		out.Series[i] = toAPIVodSeries(m)
	}
	markVodSeries(out.Series, idx, vis)
	return out, nil
}

func (h *Handlers) ListVodSeriesCategories(ctx context.Context, _ ListVodSeriesCategoriesRequestObject) (ListVodSeriesCategoriesResponseObject, error) {
	_, maxRating, authed, allowed := h.vodSession(ctx)
	switch {
	case !authed:
		return ListVodSeriesCategories401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !allowed:
		return ListVodSeriesCategories403JSONResponse{ForbiddenJSONResponse(errVodNotAllowed)}, nil
	}
	cats, err := h.VOD.SeriesCategories(ctx, maxRating)
	if err != nil {
		return nil, internal(ctx, "listVodSeriesCategories", err)
	}
	out := make(ListVodSeriesCategories200JSONResponse, len(cats))
	for i, c := range cats {
		out[i] = VodCategory{Name: c.Name, Count: c.Count}
	}
	return out, nil
}

func (h *Handlers) VodSeriesContinueWatching(ctx context.Context, _ VodSeriesContinueWatchingRequestObject) (VodSeriesContinueWatchingResponseObject, error) {
	s, maxRating, authed, allowed := h.vodSession(ctx)
	switch {
	case !authed:
		return VodSeriesContinueWatching401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !allowed:
		return VodSeriesContinueWatching403JSONResponse{ForbiddenJSONResponse(errVodNotAllowed)}, nil
	}
	list, err := h.VOD.SeriesContinueWatching(ctx, s.User.ID, maxRating)
	if err != nil {
		return nil, internal(ctx, "vodSeriesContinueWatching", err)
	}
	out := make(VodSeriesContinueWatching200JSONResponse, len(list))
	for i, u := range list {
		out[i] = toAPIVodSeries(u.Series)
		out[i].NextEpisode = ptr(toAPIVodEpisode(u.Episode))
	}
	idx, vis := h.vodInLibrary(ctx, s)
	markVodSeries(out, idx, vis)
	return out, nil
}

// vodSeries loads a series the user may see; nil when it doesn't exist or is rated above them.
func (h *Handlers) vodSeries(ctx context.Context, s auth.Session, maxRating string, id int64) (*vod.Series, error) {
	m, err := h.VOD.OneSeries(ctx, id, s.User.ID)
	if errors.Is(err, vod.ErrNotFound) || (err == nil && !vod.Allowed(m.ContentRating, maxRating)) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

// vodEpisode loads an episode the user may see (its series' rating applies).
func (h *Handlers) vodEpisode(ctx context.Context, s auth.Session, maxRating string, id int64) (*vod.Episode, error) {
	e, err := h.VOD.OneEpisode(ctx, id, s.User.ID)
	if errors.Is(err, vod.ErrNotFound) || (err == nil && !vod.Allowed(e.ContentRating, maxRating)) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &e, nil
}

func (h *Handlers) GetVodSeries(ctx context.Context, req GetVodSeriesRequestObject) (GetVodSeriesResponseObject, error) {
	s, maxRating, authed, allowed := h.vodSession(ctx)
	switch {
	case !authed:
		return GetVodSeries401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !allowed:
		return GetVodSeries403JSONResponse{ForbiddenJSONResponse(errVodNotAllowed)}, nil
	}
	m, err := h.vodSeries(ctx, s, maxRating, req.SeriesId)
	if err != nil {
		return nil, internal(ctx, "getVodSeries", err)
	}
	if m == nil {
		return GetVodSeries404JSONResponse{NotFoundJSONResponse(errVodSeriesNotFound)}, nil
	}
	// The episodes come from the source the first time, and again once stale.
	var episodesErr error
	if err := h.VOD.LoadEpisodes(ctx, m.ID, req.Params.Refresh != nil && *req.Params.Refresh); err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		episodesErr = err
	}
	if m, err = h.vodSeries(ctx, s, maxRating, req.SeriesId); err != nil {
		return nil, internal(ctx, "getVodSeries", fmt.Errorf("series %d: %v", req.SeriesId, err))
	}
	if m == nil { // removed by a refresh meanwhile
		return GetVodSeries404JSONResponse{NotFoundJSONResponse(errVodSeriesNotFound)}, nil
	}
	seasons, err := h.VOD.Seasons(ctx, m.ID, s.User.ID)
	if err != nil {
		return nil, internal(ctx, "getVodSeries", err)
	}
	eps, err := h.VOD.Episodes(ctx, m.ID, s.User.ID)
	if err != nil {
		return nil, internal(ctx, "getVodSeries", err)
	}
	next, err := h.VOD.NextUp(ctx, m.ID, s.User.ID)
	if err != nil {
		return nil, internal(ctx, "getVodSeries", err)
	}
	b := toAPIVodSeries(*m)
	out := VodSeriesDetail{Id: b.Id, Title: b.Title, Category: b.Category, ContentRating: b.ContentRating, Year: b.Year,
		Rating: b.Rating, PosterUrl: b.PosterUrl, SeasonCount: b.SeasonCount, EpisodeCount: b.EpisodeCount,
		WatchedCount: b.WatchedCount, Watched: b.Watched, Overview: nz(m.Overview), Genres: m.Genres,
		Director: nz(m.Director), TmdbId: nz(m.TMDBID), Matched: m.Matched,
		Seasons: make([]VodSeason, len(seasons)), Episodes: make([]VodEpisode, len(eps))}
	idx, vis := h.vodInLibrary(ctx, s)
	out.InLibrary = ptr(idx.Has(true, m.ID, vis))
	if out.Genres == nil {
		out.Genres = []string{}
	}
	if m.HasBackdrop {
		out.BackdropUrl = ptr(vodSeriesImageURL(m.ID, "backdrop"))
	}
	out.Cast = make([]struct {
		Character *string `json:"character,omitempty"`
		Name      string  `json:"name"`
	}, len(m.Cast))
	for i, c := range m.Cast {
		out.Cast[i].Name, out.Cast[i].Character = c.Name, nz(c.Character)
	}
	for i, z := range seasons {
		out.Seasons[i] = VodSeason{Season: z.Number, Name: z.Name, Overview: nz(z.Overview), AirDate: nz(z.AirDate),
			EpisodeCount: z.Episodes, WatchedCount: z.WatchedEpisodes}
		if z.HasPoster {
			out.Seasons[i].PosterUrl = ptr("/api/v1/livetv/vod/series/" + strconv.FormatInt(m.ID, 10) + "/image?season=" + strconv.Itoa(z.Number))
		}
	}
	for i, e := range eps {
		out.Episodes[i] = toAPIVodEpisode(e)
	}
	// Admins see what's saved into the library.
	if s.User.IsAdmin && h.VODSaves != nil {
		_, shows := h.VODSaves.Configured()
		dests := h.saveDestinationRefs("episode")
		out.CanSave = ptr((shows || len(dests) > 0) && h.VOD.Savable(m.SourceID))
		if *out.CanSave {
			out.SaveDefault, out.SaveDestinations = ptr(shows), &dests
		}
		if states, err := h.VODSaves.EpisodeStates(ctx, m.ID); err == nil {
			for i := range out.Episodes {
				out.Episodes[i].Save = toAPIVodSaveState(states[out.Episodes[i].Id])
			}
		}
		// How each episode compares with the library's copy, with each season's and the
		// show's counts.
		if *out.CanSave {
			if matches, err := h.VODSaves.EpisodeMatches(ctx, m.ID); err == nil {
				var show vodsave.Rollup
				seasons := map[int]*vodsave.Rollup{}
				for i := range out.Episodes {
					e := &out.Episodes[i]
					lm, ok := matches[e.Id]
					if !ok {
						continue
					}
					e.Library = toAPIVodLibraryMatch(lm)
					show.Add(lm)
					if seasons[e.Season] == nil {
						seasons[e.Season] = &vodsave.Rollup{}
					}
					seasons[e.Season].Add(lm)
				}
				out.Library = toAPIVodRollup(show)
				for i := range out.Seasons {
					if r := seasons[out.Seasons[i].Season]; r != nil {
						out.Seasons[i].Library = toAPIVodRollup(*r)
					}
				}
			} else {
				slog.WarnContext(ctx, "vod library matches", "err", err)
			}
		}
	}
	if next != nil {
		out.NextEpisode = ptr(toAPIVodEpisode(*next))
	}
	if episodesErr != nil && len(eps) == 0 {
		out.EpisodesError = ptr(episodesErr.Error())
	}
	return GetVodSeries200JSONResponse(out), nil
}

func (h *Handlers) VodSeriesImage(ctx context.Context, req VodSeriesImageRequestObject) (VodSeriesImageResponseObject, error) {
	notFound := VodSeriesImage404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "no image"))}
	if h.VOD == nil {
		return notFound, nil
	}
	var b []byte
	var ct string
	var err error
	if req.Params.Season != nil {
		b, ct, err = h.VOD.SeasonImage(ctx, req.SeriesId, *req.Params.Season)
	} else {
		kind := "poster"
		set(&kind, req.Params.Kind)
		b, ct, err = h.VOD.SeriesImage(ctx, req.SeriesId, kind)
	}
	if err != nil {
		return notFound, nil
	}
	return VodSeriesImage200ImageResponse{Body: bytes.NewReader(b), ContentType: ct, ContentLength: int64(len(b))}, nil
}

func (h *Handlers) VodEpisodeImage(ctx context.Context, req VodEpisodeImageRequestObject) (VodEpisodeImageResponseObject, error) {
	notFound := VodEpisodeImage404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "no image"))}
	if h.VOD == nil {
		return notFound, nil
	}
	b, ct, err := h.VOD.EpisodeImage(ctx, req.EpisodeId)
	if err != nil {
		return notFound, nil
	}
	return VodEpisodeImage200ImageResponse{Body: bytes.NewReader(b), ContentType: ct, ContentLength: int64(len(b))}, nil
}

// markSeries marks a series (or season) watched or not; false when it isn't the user's to see.
func (h *Handlers) markSeries(ctx context.Context, id int64, season *int, watched bool) (found, authed, allowed bool, err error) {
	s, maxRating, authed, allowed := h.vodSession(ctx)
	if !authed || !allowed {
		return false, authed, allowed, nil
	}
	m, err := h.vodSeries(ctx, s, maxRating, id)
	if err != nil || m == nil {
		return false, true, true, err
	}
	if watched {
		// Every episode, so fetch them if nobody has opened the series yet.
		if err := h.VOD.LoadEpisodes(ctx, m.ID, false); err != nil && ctx.Err() != nil {
			return false, true, true, ctx.Err()
		}
	}
	return true, true, true, h.VOD.SetSeriesWatched(ctx, s.User.ID, m.ID, season, watched)
}

func (h *Handlers) MarkVodSeriesWatched(ctx context.Context, req MarkVodSeriesWatchedRequestObject) (MarkVodSeriesWatchedResponseObject, error) {
	found, authed, allowed, err := h.markSeries(ctx, req.SeriesId, req.Params.Season, true)
	switch {
	case !authed:
		return MarkVodSeriesWatched401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !allowed:
		return MarkVodSeriesWatched403JSONResponse{ForbiddenJSONResponse(errVodNotAllowed)}, nil
	case err != nil:
		return nil, internal(ctx, "markVodSeriesWatched", err)
	case !found:
		return MarkVodSeriesWatched404JSONResponse{NotFoundJSONResponse(errVodSeriesNotFound)}, nil
	}
	return MarkVodSeriesWatched204Response{}, nil
}

func (h *Handlers) MarkVodSeriesUnwatched(ctx context.Context, req MarkVodSeriesUnwatchedRequestObject) (MarkVodSeriesUnwatchedResponseObject, error) {
	found, authed, allowed, err := h.markSeries(ctx, req.SeriesId, req.Params.Season, false)
	switch {
	case !authed:
		return MarkVodSeriesUnwatched401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !allowed:
		return MarkVodSeriesUnwatched403JSONResponse{ForbiddenJSONResponse(errVodNotAllowed)}, nil
	case err != nil:
		return nil, internal(ctx, "markVodSeriesUnwatched", err)
	case !found:
		return MarkVodSeriesUnwatched404JSONResponse{NotFoundJSONResponse(errVodSeriesNotFound)}, nil
	}
	return MarkVodSeriesUnwatched204Response{}, nil
}

func (h *Handlers) markEpisode(ctx context.Context, id int64, watched bool) (found, authed, allowed bool, err error) {
	s, maxRating, authed, allowed := h.vodSession(ctx)
	if !authed || !allowed {
		return false, authed, allowed, nil
	}
	e, err := h.vodEpisode(ctx, s, maxRating, id)
	if err != nil || e == nil {
		return false, true, true, err
	}
	return true, true, true, h.VOD.SetEpisodeWatched(ctx, s.User.ID, e.ID, watched)
}

func (h *Handlers) MarkVodEpisodeWatched(ctx context.Context, req MarkVodEpisodeWatchedRequestObject) (MarkVodEpisodeWatchedResponseObject, error) {
	found, authed, allowed, err := h.markEpisode(ctx, req.EpisodeId, true)
	switch {
	case !authed:
		return MarkVodEpisodeWatched401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !allowed:
		return MarkVodEpisodeWatched403JSONResponse{ForbiddenJSONResponse(errVodNotAllowed)}, nil
	case err != nil:
		return nil, internal(ctx, "markVodEpisodeWatched", err)
	case !found:
		return MarkVodEpisodeWatched404JSONResponse{NotFoundJSONResponse(errVodEpisodeNotFound)}, nil
	}
	return MarkVodEpisodeWatched204Response{}, nil
}

func (h *Handlers) MarkVodEpisodeUnwatched(ctx context.Context, req MarkVodEpisodeUnwatchedRequestObject) (MarkVodEpisodeUnwatchedResponseObject, error) {
	found, authed, allowed, err := h.markEpisode(ctx, req.EpisodeId, false)
	switch {
	case !authed:
		return MarkVodEpisodeUnwatched401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !allowed:
		return MarkVodEpisodeUnwatched403JSONResponse{ForbiddenJSONResponse(errVodNotAllowed)}, nil
	case err != nil:
		return nil, internal(ctx, "markVodEpisodeUnwatched", err)
	case !found:
		return MarkVodEpisodeUnwatched404JSONResponse{NotFoundJSONResponse(errVodEpisodeNotFound)}, nil
	}
	return MarkVodEpisodeUnwatched204Response{}, nil
}

func (h *Handlers) VodNextEpisode(ctx context.Context, req VodNextEpisodeRequestObject) (VodNextEpisodeResponseObject, error) {
	s, maxRating, authed, allowed := h.vodSession(ctx)
	switch {
	case !authed:
		return VodNextEpisode401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !allowed:
		return VodNextEpisode403JSONResponse{ForbiddenJSONResponse(errVodNotAllowed)}, nil
	}
	e, err := h.vodEpisode(ctx, s, maxRating, req.EpisodeId)
	if err != nil {
		return nil, internal(ctx, "vodNextEpisode", err)
	}
	if e == nil {
		return VodNextEpisode404JSONResponse{NotFoundJSONResponse(errVodEpisodeNotFound)}, nil
	}
	next, err := h.VOD.NextEpisode(ctx, e.ID, s.User.ID)
	if err != nil {
		return nil, internal(ctx, "vodNextEpisode", err)
	}
	var out VodUpNext
	if next != nil {
		out.Episode = ptr(toAPIVodEpisode(*next))
	}
	return VodNextEpisode200JSONResponse(out), nil
}

func (h *Handlers) PlayVodEpisode(ctx context.Context, req PlayVodEpisodeRequestObject) (PlayVodEpisodeResponseObject, error) {
	s, maxRating, authed, allowed := h.vodSession(ctx)
	switch {
	case !authed:
		return PlayVodEpisode401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !allowed:
		return PlayVodEpisode403JSONResponse{ForbiddenJSONResponse(errVodNotAllowed)}, nil
	}
	if requestInfo(ctx).Class == netclass.Remote && !(s.User.IsAdmin || s.User.Restrictions.RemoteAllowed()) {
		return PlayVodEpisode403JSONResponse{ForbiddenJSONResponse(apiErr("remote_not_allowed", "Movies on demand are only available at home for this profile."))}, nil
	}
	e, err := h.vodEpisode(ctx, s, maxRating, req.EpisodeId)
	if err != nil {
		return nil, internal(ctx, "playVodEpisode", err)
	}
	if e == nil {
		return PlayVodEpisode404JSONResponse{NotFoundJSONResponse(errVodEpisodeNotFound)}, nil
	}
	link, ua, ok := h.VOD.EpisodePlayLink(*e)
	if !ok {
		return PlayVodEpisode404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "the episode's source is gone"))}, nil
	}
	var opts vodPlayOptions
	start := e.PositionMS
	if b := req.Body; b != nil {
		opts = vodPlayOptions{Profile: b.Profile, MaxBitrateKbps: b.MaxBitrateKbps}
		set(&start, b.StartMs)
	}
	title := fmt.Sprintf("%s · S%dE%d · %s", e.SeriesTitle, e.Season, e.Episode, e.Title)
	ps, unavailable, badGateway, err := h.startVodFile(ctx, s, "episode", title, link, ua, start, e.SourceID, opts)
	switch {
	case unavailable != nil:
		return PlayVodEpisode503JSONResponse{ServiceUnavailableJSONResponse(*unavailable)}, nil
	case badGateway != nil:
		return PlayVodEpisode502JSONResponse{BadGatewayJSONResponse(*badGateway)}, nil
	case err != nil:
		return nil, err
	}
	st := h.VOD.Streams.StartEpisodePlayback(s.User.ID, e.ID, ps.ID)
	return PlayVodEpisode200JSONResponse(VodSession{Id: st.ID, Url: playback.StreamPrefix + ps.ID + "/master.m3u8",
		MovieId: 0, EpisodeId: ptr(e.ID), StartMs: nz(ps.StartMS)}), nil
}

func (h *Handlers) ListVodSeriesGenres(ctx context.Context, _ ListVodSeriesGenresRequestObject) (ListVodSeriesGenresResponseObject, error) {
	_, maxRating, authed, allowed := h.vodSession(ctx)
	switch {
	case !authed:
		return ListVodSeriesGenres401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !allowed:
		return ListVodSeriesGenres403JSONResponse{ForbiddenJSONResponse(errVodNotAllowed)}, nil
	}
	gs, err := h.VOD.Genres(ctx, maxRating, true)
	if err != nil {
		return nil, internal(ctx, "listVodSeriesGenres", err)
	}
	out := make(ListVodSeriesGenres200JSONResponse, len(gs))
	for i, c := range gs {
		out[i] = VodCategory{Name: c.Name, Count: c.Count}
	}
	return out, nil
}
