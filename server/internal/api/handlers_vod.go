package api

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"marquee/internal/auth"
	"marquee/internal/netclass"
	"marquee/internal/playback"
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
		kind := VodSourceKindM3u
		if s.Xtream() {
			kind = VodSourceKindXtream
		}
		out[i] = VodSource{Id: ptr(s.ID), Name: s.Name, Kind: &kind, Url: s.URL, UserAgent: nz(s.UserAgent), Enabled: ptr(s.Enabled)}
		if s.Xtream() {
			out[i].Username, out[i].PasswordSet = ptr(s.Username), ptr(s.Password != "")
		}
	}
	return out
}

// fromAPIVodSource reads one source. The password is write-only: without one, the saved
// source's (same id) is kept.
func fromAPIVodSource(src VodSource, saved []settings.VODSource) settings.VODSource {
	id := ""
	if src.Id != nil {
		id = *src.Id
	}
	out := settings.VODSource{ID: id, Name: strings.TrimSpace(src.Name), Kind: "m3u",
		URL: strings.TrimRight(strings.TrimSpace(src.Url), "/"), Enabled: src.Enabled == nil || *src.Enabled}
	if src.Kind != nil && *src.Kind == VodSourceKindXtream {
		out.Kind = settings.VODKindXtream
	} else {
		out.URL = strings.TrimSpace(src.Url) // a playlist's address is kept as given
	}
	if src.UserAgent != nil {
		out.UserAgent = strings.TrimSpace(*src.UserAgent)
	}
	if out.Xtream() {
		if src.Username != nil {
			out.Username = strings.TrimSpace(*src.Username)
		}
		if src.Password != nil && *src.Password != "" {
			out.Password = *src.Password
		} else {
			// The saved password is only reused for the same server, so changing the address
			// can't send it somewhere else.
			for _, old := range saved {
				if old.ID == id && id != "" && strings.TrimRight(old.URL, "/") == strings.TrimRight(out.URL, "/") {
					out.Password = old.Password
				}
			}
		}
	}
	return out
}

func fromAPIVodSources(in []VodSource, saved []settings.VODSource) []settings.VODSource {
	out := make([]settings.VODSource, len(in))
	for i, src := range in {
		out[i] = fromAPIVodSource(src, saved)
		if out[i].ID == "" {
			out[i].ID = newWebhookID()
		}
	}
	return out
}

func (h *Handlers) vodStatus(ctx context.Context) (VodStatus, error) {
	st, err := h.VOD.Status(ctx)
	if err != nil {
		return VodStatus{}, err
	}
	out := VodStatus{Movies: st.Movies, Series: st.Series, Matched: st.Matched, Unmatched: st.Unmatched, Matching: h.VOD.Matching()}
	s, ok := session(ctx)
	if ok {
		allowed, _ := vodAccess(s)
		out.Enabled = allowed && h.VOD.Enabled() && (st.Movies > 0 || st.Series > 0)
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
		Series      int        `json:"series"`
	}, len(st.Sources))
	for i, src := range st.Sources {
		out.Sources[i].Id, out.Sources[i].Name, out.Sources[i].Movies = src.ID, src.Name, src.Movies
		out.Sources[i].Series = src.Series
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
	applyVodFilters(&f, p.Genre, p.YearFrom, p.YearTo, p.MinRating, (*string)(p.Watched), p.SourceId)
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
	if err == nil && m != nil && !m.Matched && m.Overview == "" {
		h.VOD.Details(ctx, m.ID) // an Xtream Codes film's details, the first time it's opened
		m, err = h.vodMovie(ctx, s, maxRating, req.MovieId)
	}
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
	// Admins see whether it's saved into the library.
	if s.User.IsAdmin && h.VODSaves != nil {
		movies, _ := h.VODSaves.Configured()
		out.CanSave = ptr(movies && h.VOD.Savable(m.SourceID))
		if st, err := h.VODSaves.MovieState(ctx, m.ID); err == nil {
			out.Save = toAPIVodSaveState(st)
		}
		// How it compares with the library's copy (its quality is checked when unknown).
		if *out.CanSave {
			if lm, err := h.VODSaves.MovieMatch(ctx, m.ID); err == nil {
				out.Library = toAPIVodLibraryMatch(lm)
			} else {
				slog.WarnContext(ctx, "vod library match", "err", err)
			}
		}
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
	link, ua, hls := h.VOD.PlayLink(*m)
	if hls {
		st := h.VOD.Streams.Start(s.User.ID, m.ID, link, ua)
		return PlayVodMovie200JSONResponse(VodSession{Id: st.ID, Url: vod.StreamPrefix + st.ID + "/master.m3u8", MovieId: m.ID,
			StartMs: nz(m.PositionMS)}), nil
	}
	// A file (Xtream Codes): the server makes it into HLS, like a library transcode.
	var opts vodPlayOptions
	if b := req.Body; b != nil {
		opts = vodPlayOptions{Profile: b.Profile, MaxBitrateKbps: b.MaxBitrateKbps}
	}
	ps, apiErr503, apiErr502, err := h.startVodFile(ctx, s, "film", m.Title, link, ua, m.PositionMS, m.SourceID, opts)
	switch {
	case apiErr503 != nil:
		return PlayVodMovie503JSONResponse{ServiceUnavailableJSONResponse(*apiErr503)}, nil
	case apiErr502 != nil:
		return PlayVodMovie502JSONResponse{BadGatewayJSONResponse(*apiErr502)}, nil
	case err != nil:
		return nil, err
	}
	st := h.VOD.Streams.StartPlayback(s.User.ID, m.ID, ps.ID)
	return PlayVodMovie200JSONResponse(VodSession{Id: st.ID, Url: playback.StreamPrefix + ps.ID + "/master.m3u8", MovieId: m.ID,
		StartMs: nz(ps.StartMS)}), nil
}

type vodPlayOptions struct {
	Profile        *DeviceProfile
	MaxBitrateKbps *int
}

// startVodFile makes a remote file (Xtream Codes film or episode) into HLS, like a library
// transcode. A 503 or 502 error is returned as such; anything else is an internal error.
func (h *Handlers) startVodFile(ctx context.Context, s auth.Session, noun, title, link, ua string, startMS int64, sourceID string,
	o vodPlayOptions) (ps *playback.Session, unavailable, badGateway *Error, err error) {
	if h.Playback == nil {
		return nil, ptr(apiErr("unavailable", "playback isn't available")), nil, nil
	}
	ri := requestInfo(ctx)
	r := playback.ExternalRequest{UserID: s.User.ID, UserName: s.User.DisplayName, DeviceID: s.DeviceID, Title: title,
		URL: link, UserAgent: ua, StartMS: startMS, Remote: ri.Class == netclass.Remote, ClientIP: ri.ClientIP,
		Redact: func(msg string) string { return h.VOD.Redact(sourceID, msg) }}
	for _, d := range must(h.Auth.ListDevices(ctx, s.User.ID)) {
		if d.ID == s.DeviceID {
			r.DeviceName = d.Name
		}
	}
	if o.Profile != nil {
		p := toProfile(*o.Profile)
		r.Profile = &p
	}
	set(&r.Quality.RequestedKbps, o.MaxBitrateKbps)
	if r.Remote {
		r.Quality.UserPrefKbps = s.User.Preferences.RemoteQualityKbps
		r.Quality.UserCapKbps = s.User.Restrictions.RemoteQualityKbps
	} else {
		r.Quality.UserPrefKbps = s.User.Preferences.LocalQualityKbps
	}
	ps, err = h.Playback.StartExternal(ctx, r)
	switch {
	case errors.Is(err, playback.ErrBusy):
		return nil, ptr(apiErr("busy", err.Error())), nil, nil
	case errors.Is(err, playback.ErrUnavailable):
		return nil, nil, ptr(apiErr("unavailable", "The "+noun+" didn't load from its source: "+err.Error())), nil
	case err != nil:
		if ctx.Err() != nil {
			return nil, nil, nil, ctx.Err()
		}
		return nil, nil, nil, internal(ctx, "playVod", errors.New(h.VOD.Redact(sourceID, err.Error())))
	}
	return ps, nil, nil, nil
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
	st.Touch()
	if st.PlaybackID != "" && h.Playback != nil {
		h.Playback.Progress(ctx, st.PlaybackID, req.Body.PositionMs, "playing") // keeps it alive; shows where they are
	}
	var err error
	if st.EpisodeID != 0 {
		err = h.VOD.SetEpisodeProgress(ctx, s.User.ID, st.EpisodeID, req.Body.PositionMs, dur)
	} else {
		err = h.VOD.SetProgress(ctx, s.User.ID, st.MovieID, req.Body.PositionMs, dur)
	}
	if err != nil {
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

func (h *Handlers) TestVodSource(ctx context.Context, req TestVodSourceRequestObject) (TestVodSourceResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return TestVodSource401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return TestVodSource403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	if req.Body == nil || h.VOD == nil {
		return TestVodSource400JSONResponse{BadRequestJSONResponse(apiErr("invalid", "a source is required"))}, nil
	}
	src := fromAPIVodSource(*req.Body, h.Settings.Get().Integrations.VODSources)
	if !(strings.HasPrefix(src.URL, "http://") || strings.HasPrefix(src.URL, "https://")) {
		return TestVodSource400JSONResponse{BadRequestJSONResponse(apiErr("invalid", "the address must start with http:// or https://"))}, nil
	}
	if src.Xtream() && (src.Username == "" || src.Password == "") {
		return TestVodSource400JSONResponse{BadRequestJSONResponse(apiErr("invalid", "a username and password are needed"))}, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	r, err := h.VOD.Test(ctx, src)
	if err != nil {
		return TestVodSource200JSONResponse{Ok: false, Error: ptr(err.Error())}, nil
	}
	out := TestVodSource200JSONResponse{Ok: true, Movies: ptr(r.Movies), Account: nz(r.Account)}
	if src.Xtream() {
		out.Categories = ptr(r.Categories)
		if r.Series >= 0 {
			out.Series = ptr(r.Series)
		}
	}
	if !r.ExpiresAt.IsZero() {
		out.ExpiresAt = ptr(r.ExpiresAt)
	}
	return out, nil
}

// applyVodFilters reads the list filters films and series share.
func applyVodFilters(f *vod.Filter, genre *string, yearFrom, yearTo *int, minRating *float32, watched, sourceID *string) {
	set(&f.Genre, genre)
	set(&f.YearFrom, yearFrom)
	set(&f.YearTo, yearTo)
	if minRating != nil {
		f.MinRating = float64(*minRating)
	}
	set(&f.Watched, watched)
	set(&f.SourceID, sourceID)
}

func (h *Handlers) ListVodGenres(ctx context.Context, _ ListVodGenresRequestObject) (ListVodGenresResponseObject, error) {
	_, maxRating, authed, allowed := h.vodSession(ctx)
	switch {
	case !authed:
		return ListVodGenres401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !allowed:
		return ListVodGenres403JSONResponse{ForbiddenJSONResponse(errVodNotAllowed)}, nil
	}
	gs, err := h.VOD.Genres(ctx, maxRating, false)
	if err != nil {
		return nil, internal(ctx, "listVodGenres", err)
	}
	out := make(ListVodGenres200JSONResponse, len(gs))
	for i, c := range gs {
		out[i] = VodCategory{Name: c.Name, Count: c.Count}
	}
	return out, nil
}
