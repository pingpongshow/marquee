package api

import (
	"context"
	"errors"
	"strings"

	"marquee/internal/auth"
	"marquee/internal/requests"
)

// Requests (REQ-1, REQ-2): search Seerr, ask for titles, and admins approve them.

func canRequest(s auth.Session) bool { return s.User.IsAdmin || s.User.Restrictions.CanRequest }

func toAPIDiscover(items []requests.Item, page, total int) DiscoverPage {
	out := DiscoverPage{Results: make([]DiscoverItem, len(items)), Page: page, TotalPages: total}
	for i, it := range items {
		d := DiscoverItem{TmdbId: it.TMDBID, MediaType: DiscoverItemMediaType(it.MediaType), Title: it.Title,
			Overview: nz(it.Overview), PosterUrl: nz(it.PosterURL), BackdropUrl: nz(it.BackdropURL), Availability: Availability(it.Availability)}
		if it.Year > 0 {
			d.Year = ptr(it.Year)
		}
		if it.TMDBRating > 0 {
			d.TmdbRating = ptr(float32(it.TMDBRating))
		}
		if it.ItemID > 0 {
			d.ItemId = ptr(it.ItemID)
		}
		if it.RequestID > 0 {
			d.RequestId = ptr(it.RequestID)
		}
		out.Results[i] = d
	}
	return out
}

func toAPIRequest(r requests.Request) MediaRequest {
	m := MediaRequest{Id: r.ID, TmdbId: r.TMDBID, MediaType: MediaRequestMediaType(r.MediaType), Title: r.Title, PosterUrl: nz(r.PosterURL),
		Seasons: r.Seasons, Status: RequestState(r.Status), Reason: nz(r.Reason), UserId: r.UserID, UserName: r.UserName,
		CreatedAt: r.CreatedAt, DecidedAt: r.DecidedAt, DecidedBy: nz(r.DecidedBy)}
	if r.Year > 0 {
		m.Year = ptr(r.Year)
	}
	if r.SeerrRequestID > 0 {
		m.SeerrRequestId = ptr(r.SeerrRequestID)
	}
	return m
}

func seerrErr(err error) (unavailable bool, e Error) {
	if errors.Is(err, requests.ErrNotConfigured) {
		return true, apiErr("not_configured", "Requests aren't set up on this server.")
	}
	return false, apiErr("seerr_error", err.Error())
}

func (h *Handlers) RequestsStatus(ctx context.Context, _ RequestsStatusRequestObject) (RequestsStatusResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return RequestsStatus401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	out := RequestsStatus{Enabled: h.Requests.Enabled(), CanRequest: canRequest(s)}
	if s.User.IsAdmin {
		out.PendingApprovals = h.Requests.PendingCount(ctx)
	}
	return RequestsStatus200JSONResponse(out), nil
}

func (h *Handlers) SearchRequestable(ctx context.Context, req SearchRequestableRequestObject) (SearchRequestableResponseObject, error) {
	s, ok := session(ctx)
	switch {
	case !ok:
		return SearchRequestable401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !canRequest(s):
		return SearchRequestable403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	page := 1
	if req.Params.Page != nil {
		page = *req.Params.Page
	}
	items, p, total, err := h.Requests.Search(ctx, s.User.ID, strings.TrimSpace(req.Params.Q), page)
	if err != nil {
		if na, e := seerrErr(err); na {
			return SearchRequestable503JSONResponse{ServiceUnavailableJSONResponse(e)}, nil
		} else {
			return SearchRequestable502JSONResponse{BadGatewayJSONResponse(e)}, nil
		}
	}
	return SearchRequestable200JSONResponse(toAPIDiscover(items, p, total)), nil
}

func (h *Handlers) DiscoverRequestable(ctx context.Context, req DiscoverRequestableRequestObject) (DiscoverRequestableResponseObject, error) {
	s, ok := session(ctx)
	switch {
	case !ok:
		return DiscoverRequestable401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !canRequest(s):
		return DiscoverRequestable403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	page, cat := 1, "trending"
	if req.Params.Page != nil {
		page = *req.Params.Page
	}
	if req.Params.Category != nil {
		cat = string(*req.Params.Category)
	}
	var b requests.Browse
	if v := req.Params.Network; v != nil {
		b.Network = *v
	}
	if v := req.Params.Studio; v != nil {
		b.Studio = *v
	}
	if v := req.Params.Genre; v != nil {
		b.Genre = *v
	}
	if v := req.Params.Sort; v != nil {
		b.Sort = string(*v)
	}
	items, p, total, err := h.Requests.Discover(ctx, s.User.ID, cat, b, page)
	if err != nil {
		if na, e := seerrErr(err); na {
			return DiscoverRequestable503JSONResponse{ServiceUnavailableJSONResponse(e)}, nil
		} else {
			return DiscoverRequestable502JSONResponse{BadGatewayJSONResponse(e)}, nil
		}
	}
	return DiscoverRequestable200JSONResponse(toAPIDiscover(items, p, total)), nil
}

func (h *Handlers) RequestableShow(ctx context.Context, req RequestableShowRequestObject) (RequestableShowResponseObject, error) {
	s, ok := session(ctx)
	switch {
	case !ok:
		return RequestableShow401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !canRequest(s):
		return RequestableShow403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	title, seasons, err := h.Requests.Show(ctx, req.TmdbId)
	if err != nil {
		if na, e := seerrErr(err); na {
			return RequestableShow503JSONResponse{ServiceUnavailableJSONResponse(e)}, nil
		} else {
			return RequestableShow502JSONResponse{BadGatewayJSONResponse(e)}, nil
		}
	}
	out := RequestableShow{TmdbId: req.TmdbId, Title: title}
	for _, x := range seasons {
		out.Seasons = append(out.Seasons, struct {
			Availability Availability `json:"availability"`
			EpisodeCount *int         `json:"episodeCount,omitempty"`
			Name         *string      `json:"name,omitempty"`
			Number       int          `json:"number"`
		}{Availability(x.Availability), ptr(x.Episodes), nz(x.Name), x.Number})
	}
	if out.Seasons == nil {
		out.Seasons = out.Seasons[:0]
	}
	return RequestableShow200JSONResponse(out), nil
}

func (h *Handlers) ListRequests(ctx context.Context, req ListRequestsRequestObject) (ListRequestsResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return ListRequests401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	user := s.User.ID
	if req.Params.Scope != nil && *req.Params.Scope == "all" {
		if !s.User.IsAdmin {
			return ListRequests403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
		}
		user = 0
	}
	status := ""
	if req.Params.Status != nil {
		status = string(*req.Params.Status)
	}
	list, err := h.Requests.List(ctx, user, status)
	if err != nil {
		return nil, internal(ctx, "listRequests", err)
	}
	out := make(ListRequests200JSONResponse, len(list))
	for i, r := range list {
		out[i] = toAPIRequest(r)
	}
	return out, nil
}

func (h *Handlers) CreateRequest(ctx context.Context, req CreateRequestRequestObject) (CreateRequestResponseObject, error) {
	s, ok := session(ctx)
	switch {
	case !ok:
		return CreateRequest401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !canRequest(s):
		return CreateRequest403JSONResponse{ForbiddenJSONResponse(apiErr("forbidden", "Ask an admin to let you request titles."))}, nil
	case !h.Requests.Enabled():
		return CreateRequest503JSONResponse{ServiceUnavailableJSONResponse(apiErr("not_configured", "Requests aren't set up on this server."))}, nil
	}
	var seasons []int
	if req.Body.Seasons != nil {
		seasons = *req.Body.Seasons
	}
	r, err := h.Requests.Create(ctx, s.User.ID, string(req.Body.MediaType), req.Body.TmdbId, seasons)
	switch {
	case errors.Is(err, requests.ErrDuplicate):
		return CreateRequest409JSONResponse{ConflictJSONResponse(apiErr("duplicate", "That has already been requested."))}, nil
	case errors.Is(err, requests.ErrAvailable):
		return CreateRequest409JSONResponse{ConflictJSONResponse(apiErr("available", "That's already in the library."))}, nil
	case err != nil:
		return CreateRequest400JSONResponse{BadRequestJSONResponse(apiErr("invalid", err.Error()))}, nil
	}
	return CreateRequest201JSONResponse(toAPIRequest(r)), nil
}

func (h *Handlers) CancelRequest(ctx context.Context, req CancelRequestRequestObject) (CancelRequestResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return CancelRequest401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	r, err := h.Requests.Get(ctx, req.RequestId)
	if errors.Is(err, requests.ErrNotFound) {
		return CancelRequest404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "request not found"))}, nil
	} else if err != nil {
		return nil, internal(ctx, "cancelRequest", err)
	}
	if r.UserID != s.User.ID && !s.User.IsAdmin {
		return CancelRequest403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	if err := h.Requests.Cancel(ctx, req.RequestId); errors.Is(err, requests.ErrDecided) {
		return CancelRequest409JSONResponse{ConflictJSONResponse(apiErr("decided", err.Error()))}, nil
	} else if err != nil {
		return nil, internal(ctx, "cancelRequest", err)
	}
	return CancelRequest204Response{}, nil
}

func (h *Handlers) ApproveRequest(ctx context.Context, req ApproveRequestRequestObject) (ApproveRequestResponseObject, error) {
	s, ok := session(ctx)
	switch {
	case !ok:
		return ApproveRequest401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !s.User.IsAdmin:
		return ApproveRequest403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	// Made in Seerr as the requester's linked Seerr user, if any.
	seerrUser := func(userID int64) *int64 {
		u, err := h.Auth.GetUser(ctx, userID)
		if err != nil {
			return nil
		}
		return u.Restrictions.SeerrUserID
	}
	r, err := h.Requests.Approve(ctx, req.RequestId, s.User.ID, seerrUser)
	switch {
	case errors.Is(err, requests.ErrNotFound):
		return ApproveRequest404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "request not found"))}, nil
	case errors.Is(err, requests.ErrDecided):
		return ApproveRequest409JSONResponse{ConflictJSONResponse(apiErr("decided", err.Error()))}, nil
	case err != nil:
		return nil, internal(ctx, "approveRequest", err)
	}
	return ApproveRequest200JSONResponse(toAPIRequest(r)), nil
}

func (h *Handlers) DeclineRequest(ctx context.Context, req DeclineRequestRequestObject) (DeclineRequestResponseObject, error) {
	s, ok := session(ctx)
	switch {
	case !ok:
		return DeclineRequest401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !s.User.IsAdmin:
		return DeclineRequest403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	reason := ""
	if req.Body != nil && req.Body.Reason != nil {
		reason = strings.TrimSpace(*req.Body.Reason)
	}
	r, err := h.Requests.Decline(ctx, req.RequestId, s.User.ID, reason)
	switch {
	case errors.Is(err, requests.ErrNotFound):
		return DeclineRequest404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "request not found"))}, nil
	case errors.Is(err, requests.ErrDecided):
		return DeclineRequest409JSONResponse{ConflictJSONResponse(apiErr("decided", err.Error()))}, nil
	case err != nil:
		return nil, internal(ctx, "declineRequest", err)
	}
	return DeclineRequest200JSONResponse(toAPIRequest(r)), nil
}

func (h *Handlers) ListSeerrUsers(ctx context.Context, _ ListSeerrUsersRequestObject) (ListSeerrUsersResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return ListSeerrUsers401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return ListSeerrUsers403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	users, err := h.Requests.Users(ctx)
	if err != nil {
		if na, e := seerrErr(err); na {
			return ListSeerrUsers503JSONResponse{ServiceUnavailableJSONResponse(e)}, nil
		} else {
			return ListSeerrUsers502JSONResponse{BadGatewayJSONResponse(e)}, nil
		}
	}
	out := make(ListSeerrUsers200JSONResponse, len(users))
	for i, u := range users {
		out[i].Id, out[i].DisplayName = u.ID, u.DisplayName
	}
	return out, nil
}

func (h *Handlers) TestSeerr(ctx context.Context, req TestSeerrRequestObject) (TestSeerrResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return TestSeerr401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return TestSeerr403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	key := ""
	if req.Body.ApiKey != nil {
		key = *req.Body.ApiKey
	}
	v, err := h.Requests.Test(ctx, strings.TrimRight(strings.TrimSpace(req.Body.Url), "/"), key)
	out := TestSeerr200JSONResponse{Ok: err == nil, Version: nz(v)}
	if err != nil {
		out.Error = ptr(err.Error())
	}
	return out, nil
}

func (h *Handlers) TitleRatings(ctx context.Context, req TitleRatingsRequestObject) (TitleRatingsResponseObject, error) {
	if _, ok := session(ctx); !ok {
		return TitleRatings401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	r, err := h.Requests.Ratings(ctx, string(req.MediaType), req.TmdbId)
	if errors.Is(err, requests.ErrNotConfigured) {
		return TitleRatings503JSONResponse{ServiceUnavailableJSONResponse(apiErr("not_configured", err.Error()))}, nil
	} else if err != nil {
		return nil, internal(ctx, "titleRatings", err)
	}
	var out TitleRatings
	if rt := r.RT; rt != nil && (rt.CriticsScore != nil || rt.AudienceScore != nil) {
		out.RottenTomatoes = &struct {
			AudienceRating *string `json:"audienceRating,omitempty"`
			AudienceScore  *int    `json:"audienceScore,omitempty"`
			CriticsRating  *string `json:"criticsRating,omitempty"`
			CriticsScore   *int    `json:"criticsScore,omitempty"`
			Url            *string `json:"url,omitempty"`
		}{nz(rt.AudienceRating), rt.AudienceScore, nz(rt.CriticsRating), rt.CriticsScore, nz(rt.URL)}
	}
	if im := r.IMDb; im != nil && im.CriticsScore != nil {
		v := float32(*im.CriticsScore)
		out.Imdb = &struct {
			Rating *float32 `json:"rating,omitempty"`
			Url    *string  `json:"url,omitempty"`
		}{&v, nz(im.URL)}
	}
	return TitleRatings200JSONResponse(out), nil
}

func toNamed(list []requests.Genre) []NamedId {
	out := make([]NamedId, len(list))
	for i, g := range list {
		out[i] = NamedId{Id: g.ID, Name: g.Name}
	}
	return out
}

func (h *Handlers) DiscoverFilters(ctx context.Context, _ DiscoverFiltersRequestObject) (DiscoverFiltersResponseObject, error) {
	if _, ok := session(ctx); !ok {
		return DiscoverFilters401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	movie, tv, err := h.Requests.Filters(ctx)
	if err != nil {
		if na, e := seerrErr(err); na {
			return DiscoverFilters503JSONResponse{ServiceUnavailableJSONResponse(e)}, nil
		} else {
			return DiscoverFilters502JSONResponse{BadGatewayJSONResponse(e)}, nil
		}
	}
	return DiscoverFilters200JSONResponse{Networks: toNamed(requests.Networks), Studios: toNamed(requests.Studios),
		MovieGenres: toNamed(movie), TvGenres: toNamed(tv)}, nil
}
