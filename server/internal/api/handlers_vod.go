package api

import (
	"bytes"
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"marquee/internal/auth"
	"marquee/internal/netclass"
	"marquee/internal/settings"
	"marquee/internal/vod"
)

// Movies on demand under Live TV (LIVE-6).

// vodAccess reports whether the user may watch films on demand (the Live TV permission,
// LIVE-4) and the content rating they're limited to (USER-2).
func vodAccess(s auth.Session) (bool, string) {
	if s.User.IsAdmin {
		return true, ""
	}
	r := s.User.Restrictions
	max := ""
	if r.MaxContentRating != nil {
		max = *r.MaxContentRating
	}
	return r.LiveTVAllowed(), max
}

// vodSession is the signed-in user, if movies on demand are on and theirs to watch.
func (h *Handlers) vodSession(ctx context.Context) (s auth.Session, maxRating string, authed, allowed bool) {
	s, authed = session(ctx)
	if !authed || h.VOD == nil || !h.VOD.Enabled() {
		return s, "", authed, false
	}
	allowed, maxRating = vodAccess(s)
	return s, maxRating, authed, allowed
}

var errVodNotAllowed = apiErr("not_allowed", "Movies on demand aren't available for this profile.")

func vodImageURL(id int64, kind string) string {
	return "/api/v1/livetv/vod/movies/" + strconv.FormatInt(id, 10) + "/image?kind=" + kind
}

func toAPIVodMovie(m vod.Movie) VodMovie {
	out := VodMovie{Id: m.ID, Title: m.Title, Watched: m.Watched, Category: nz(m.Category), ContentRating: nz(m.ContentRating),
		RuntimeMs: nz(m.RuntimeMS), PositionMs: nz(m.PositionMS), Year: nz(m.Year)}
	if m.Rating > 0 {
		out.Rating = ptr(float32(m.Rating))
	}
	if m.HasPoster {
		out.PosterUrl = ptr(vodImageURL(m.ID, "poster"))
	}
	return out
}

func toAPIVodMovies(ms []vod.Movie) []VodMovie {
	out := make([]VodMovie, len(ms))
	for i, m := range ms {
		out[i] = toAPIVodMovie(m)
	}
	return out
}

func toAPIVodSources(in []settings.VODSource) []VodSource {
	out := make([]VodSource, len(in))
	for i, s := range in {
		out[i] = VodSource{Id: ptr(s.ID), Name: s.Name, Url: s.URL, UserAgent: nz(s.UserAgent), Enabled: ptr(s.Enabled)}
	}
	return out
}

func fromAPIVodSources(in []VodSource) []settings.VODSource {
	out := make([]settings.VODSource, len(in))
	for i, src := range in {
		id := ""
		if src.Id != nil {
			id = *src.Id
		}
		if id == "" {
			id = newWebhookID()
		}
		out[i] = settings.VODSource{ID: id, Name: strings.TrimSpace(src.Name), URL: strings.TrimSpace(src.Url),
			Enabled: src.Enabled == nil || *src.Enabled}
		if src.UserAgent != nil {
			out[i].UserAgent = strings.TrimSpace(*src.UserAgent)
		}
	}
	return out
}

func (h *Handlers) vodStatus(ctx context.Context) (VodStatus, error) {
	st, err := h.VOD.Status(ctx)
	if err != nil {
		return VodStatus{}, err
	}
	out := VodStatus{Movies: st.Movies, Matched: st.Matched, Unmatched: st.Unmatched, Matching: h.VOD.Matching()}
	s, ok := session(ctx)
	if ok {
		allowed, _ := vodAccess(s)
		out.Enabled = allowed && h.VOD.Enabled() && st.Movies > 0
		if s.User.IsAdmin {
			out.TmdbConfigured = ptr(h.Settings.Get().Metadata.TMDBAPIKey != "")
		}
	}
	out.Sources = make([]struct {
		Error       *string    `json:"error,omitempty"`
		Id          string     `json:"id"`
		Movies      int        `json:"movies"`
		Name        string     `json:"name"`
		RefreshedAt *time.Time `json:"refreshedAt,omitempty"`
	}, len(st.Sources))
	for i, src := range st.Sources {
		out.Sources[i].Id, out.Sources[i].Name, out.Sources[i].Movies = src.ID, src.Name, src.Movies
		out.Sources[i].RefreshedAt, out.Sources[i].Error = src.RefreshedAt, nz(src.Error)
	}
	return out, nil
}

func (h *Handlers) VodStatus(ctx context.Context, _ VodStatusRequestObject) (VodStatusResponseObject, error) {
	if _, ok := session(ctx); !ok {
		return VodStatus401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	st, err := h.vodStatus(ctx)
	if err != nil {
		return nil, internal(ctx, "vodStatus", err)
	}
	return VodStatus200JSONResponse(st), nil
}

func (h *Handlers) ListVodCategories(ctx context.Context, _ ListVodCategoriesRequestObject) (ListVodCategoriesResponseObject, error) {
	_, maxRating, authed, allowed := h.vodSession(ctx)
	switch {
	case !authed:
		return ListVodCategories401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !allowed:
		return ListVodCategories403JSONResponse{ForbiddenJSONResponse(errVodNotAllowed)}, nil
	}
	cats, err := h.VOD.Categories(ctx, maxRating)
	if err != nil {
		return nil, internal(ctx, "listVodCategories", err)
	}
	out := make(ListVodCategories200JSONResponse, len(cats))
	for i, c := range cats {
		out[i] = VodCategory{Name: c.Name, Count: c.Count}
	}
	return out, nil
}

func (h *Handlers) ListVodMovies(ctx context.Context, req ListVodMoviesRequestObject) (ListVodMoviesResponseObject, error) {
	s, maxRating, authed, allowed := h.vodSession(ctx)
	switch {
	case !authed:
		return ListVodMovies401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !allowed:
		return ListVodMovies403JSONResponse{ForbiddenJSONResponse(errVodNotAllowed)}, nil
	}
	f := vod.Filter{UserID: s.User.ID, MaxRating: maxRating}
	p := req.Params
	if p.Category != nil {
		f.Category = *p.Category
	}
	if p.Q != nil {
		f.Query = *p.Q
	}
	if p.Sort != nil {
		f.Sort = string(*p.Sort)
	}
	if p.Limit != nil {
		f.Limit = *p.Limit
	}
	if p.Offset != nil {
		f.Offset = *p.Offset
	}
	movies, total, err := h.VOD.Movies(ctx, f)
	if err != nil {
		return nil, internal(ctx, "listVodMovies", err)
	}
	return ListVodMovies200JSONResponse{Total: total, Movies: toAPIVodMovies(movies)}, nil
}

func (h *Handlers) VodContinueWatching(ctx context.Context, _ VodContinueWatchingRequestObject) (VodContinueWatchingResponseObject, error) {
	s, maxRating, authed, allowed := h.vodSession(ctx)
	switch {
	case !authed:
		return VodContinueWatching401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !allowed:
		return VodContinueWatching403JSONResponse{ForbiddenJSONResponse(errVodNotAllowed)}, nil
	}
	movies, err := h.VOD.ContinueWatching(ctx, s.User.ID, maxRating)
	if err != nil {
		return nil, internal(ctx, "vodContinueWatching", err)
	}
	return VodContinueWatching200JSONResponse(toAPIVodMovies(movies)), nil
}

// vodMovie loads a film the user may see; nil when it doesn't exist or is rated above them.
func (h *Handlers) vodMovie(ctx context.Context, s auth.Session, maxRating string, id int64) (*vod.Movie, error) {
	m, err := h.VOD.Movie(ctx, id, s.User.ID)
	if errors.Is(err, vod.ErrNotFound) || (err == nil && !vod.Allowed(m.ContentRating, maxRating)) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

var errVodNotFound = apiErr("not_found", "film not found")

func (h *Handlers) GetVodMovie(ctx context.Context, req GetVodMovieRequestObject) (GetVodMovieResponseObject, error) {
	s, maxRating, authed, allowed := h.vodSession(ctx)
	switch {
	case !authed:
		return GetVodMovie401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !allowed:
		return GetVodMovie403JSONResponse{ForbiddenJSONResponse(errVodNotAllowed)}, nil
	}
	m, err := h.vodMovie(ctx, s, maxRating, req.MovieId)
	if err != nil {
		return nil, internal(ctx, "getVodMovie", err)
	}
	if m == nil {
		return GetVodMovie404JSONResponse{NotFoundJSONResponse(errVodNotFound)}, nil
	}
	b := toAPIVodMovie(*m)
	out := VodMovieDetail{Id: b.Id, Title: b.Title, Watched: b.Watched, Category: b.Category, ContentRating: b.ContentRating,
		RuntimeMs: b.RuntimeMs, PositionMs: b.PositionMs, Year: b.Year, Rating: b.Rating, PosterUrl: b.PosterUrl,
		Overview: nz(m.Overview), Tagline: nz(m.Tagline), Genres: m.Genres, Director: nz(m.Director),
		TmdbId: nz(m.TMDBID), ImdbId: nz(m.IMDBID), Matched: m.Matched}
	if out.Genres == nil {
		out.Genres = []string{}
	}
	if m.HasBackdrop {
		out.BackdropUrl = ptr(vodImageURL(m.ID, "backdrop"))
	}
	out.Cast = make([]struct {
		Character *string `json:"character,omitempty"`
		Name      string  `json:"name"`
	}, len(m.Cast))
	for i, c := range m.Cast {
		out.Cast[i].Name, out.Cast[i].Character = c.Name, nz(c.Character)
	}
	return GetVodMovie200JSONResponse(out), nil
}

func (h *Handlers) VodMovieImage(ctx context.Context, req VodMovieImageRequestObject) (VodMovieImageResponseObject, error) {
	kind := "poster"
	if req.Params.Kind != nil {
		kind = string(*req.Params.Kind)
	}
	if h.VOD == nil {
		return VodMovieImage404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "no image"))}, nil
	}
	b, ct, err := h.VOD.Image(ctx, req.MovieId, kind)
	if err != nil {
		return VodMovieImage404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "no image"))}, nil
	}
	return VodMovieImage200ImageResponse{Body: bytes.NewReader(b), ContentType: ct, ContentLength: int64(len(b))}, nil
}

func (h *Handlers) MarkVodWatched(ctx context.Context, req MarkVodWatchedRequestObject) (MarkVodWatchedResponseObject, error) {
	s, maxRating, authed, allowed := h.vodSession(ctx)
	switch {
	case !authed:
		return MarkVodWatched401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !allowed:
		return MarkVodWatched403JSONResponse{ForbiddenJSONResponse(errVodNotAllowed)}, nil
	}
	m, err := h.vodMovie(ctx, s, maxRating, req.MovieId)
	if err == nil && m != nil {
		err = h.VOD.SetWatched(ctx, s.User.ID, m.ID, true)
	}
	if err != nil {
		return nil, internal(ctx, "markVodWatched", err)
	}
	if m == nil {
		return MarkVodWatched404JSONResponse{NotFoundJSONResponse(errVodNotFound)}, nil
	}
	return MarkVodWatched204Response{}, nil
}

func (h *Handlers) MarkVodUnwatched(ctx context.Context, req MarkVodUnwatchedRequestObject) (MarkVodUnwatchedResponseObject, error) {
	s, maxRating, authed, allowed := h.vodSession(ctx)
	switch {
	case !authed:
		return MarkVodUnwatched401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !allowed:
		return MarkVodUnwatched403JSONResponse{ForbiddenJSONResponse(errVodNotAllowed)}, nil
	}
	m, err := h.vodMovie(ctx, s, maxRating, req.MovieId)
	if err == nil && m != nil {
		err = h.VOD.SetWatched(ctx, s.User.ID, m.ID, false)
	}
	if err != nil {
		return nil, internal(ctx, "markVodUnwatched", err)
	}
	if m == nil {
		return MarkVodUnwatched404JSONResponse{NotFoundJSONResponse(errVodNotFound)}, nil
	}
	return MarkVodUnwatched204Response{}, nil
}

func (h *Handlers) PlayVodMovie(ctx context.Context, req PlayVodMovieRequestObject) (PlayVodMovieResponseObject, error) {
	s, maxRating, authed, allowed := h.vodSession(ctx)
	switch {
	case !authed:
		return PlayVodMovie401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !allowed:
		return PlayVodMovie403JSONResponse{ForbiddenJSONResponse(errVodNotAllowed)}, nil
	}
	if requestInfo(ctx).Class == netclass.Remote && !(s.User.IsAdmin || s.User.Restrictions.RemoteAllowed()) {
		return PlayVodMovie403JSONResponse{ForbiddenJSONResponse(apiErr("remote_not_allowed", "Movies on demand are only available at home for this profile."))}, nil
	}
	m, err := h.vodMovie(ctx, s, maxRating, req.MovieId)
	if err != nil {
		return nil, internal(ctx, "playVodMovie", err)
	}
	if m == nil {
		return PlayVodMovie404JSONResponse{NotFoundJSONResponse(errVodNotFound)}, nil
	}
	st := h.VOD.Streams.Start(s.User.ID, m.ID, m.StreamURL, h.VOD.UserAgent(m.SourceID))
	return PlayVodMovie200JSONResponse(VodSession{Id: st.ID, Url: vod.StreamPrefix + st.ID + "/master.m3u8", MovieId: m.ID,
		StartMs: nz(m.PositionMS)}), nil
}

func (h *Handlers) ReportVodPlayback(ctx context.Context, req ReportVodPlaybackRequestObject) (ReportVodPlaybackResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return ReportVodPlayback401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	var st *vod.Stream
	if h.VOD != nil {
		st = h.VOD.Streams.Get(req.SessionId)
	}
	if st == nil || st.UserID != s.User.ID {
		return ReportVodPlayback404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "session not found"))}, nil
	}
	var dur int64
	if req.Body.DurationMs != nil {
		dur = *req.Body.DurationMs
	}
	if err := h.VOD.SetProgress(ctx, s.User.ID, st.MovieID, req.Body.PositionMs, dur); err != nil {
		return nil, internal(ctx, "reportVodPlayback", err)
	}
	return ReportVodPlayback204Response{}, nil
}

func (h *Handlers) StopVodSession(ctx context.Context, req StopVodSessionRequestObject) (StopVodSessionResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return StopVodSession401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	if h.VOD == nil {
		return StopVodSession204Response{}, nil
	}
	if st := h.VOD.Streams.Get(req.SessionId); st != nil && (st.UserID == s.User.ID || s.User.IsAdmin) {
		h.VOD.Streams.Stop(req.SessionId)
	}
	return StopVodSession204Response{}, nil
}

func (h *Handlers) RefreshVod(ctx context.Context, _ RefreshVodRequestObject) (RefreshVodResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return RefreshVod401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return RefreshVod403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	h.VOD.Refresh(ctx, true)
	st, err := h.vodStatus(ctx)
	if err != nil {
		return nil, internal(ctx, "refreshVod", err)
	}
	return RefreshVod200JSONResponse(st), nil
}
