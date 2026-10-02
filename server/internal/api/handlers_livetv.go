package api

import (
	"bytes"
	"context"
	"errors"
	"slices"
	"strconv"
	"time"

	"marquee/internal/auth"
	"marquee/internal/livetv"
	"marquee/internal/netclass"
)

// Live TV (LIVE-1..4).

func toAPIProg(p *livetv.Prog) *LiveProgramme {
	if p == nil {
		return nil
	}
	out := &LiveProgramme{Id: p.ID, Start: p.Start, End: p.Stop, Title: p.Title, Subtitle: nz(p.Subtitle), Description: nz(p.Description),
		Category: nz(p.Category), Episode: nz(p.Episode), ImageUrl: nz(p.Image), Series: ptr(p.Series)}
	if p.Recording != "" {
		out.Recording = ptr(LiveProgrammeRecording(p.Recording))
	}
	return out
}

// liveFilter builds a channel filter, limited to what the user may watch (LIVE-4).
func liveFilter(s auth.Session, group *string, fav, recent, hidden *bool) livetv.Filter {
	f := livetv.Filter{Groups: liveGroups(s)}
	if group != nil {
		f.Group = *group
	}
	f.Favorites = fav != nil && *fav
	f.Recent = recent != nil && *recent
	f.Hidden = hidden != nil && *hidden
	return f
}

// liveGroups is the channel groups a user is limited to (nil = all; empty = no Live TV).
func liveGroups(s auth.Session) *[]string {
	r := s.User.Restrictions
	switch {
	case s.User.IsAdmin:
		return nil
	case !r.LiveTVAllowed():
		return &[]string{}
	}
	return r.LiveTVGroups
}

// liveAllowed reports whether the user may watch a channel in this group.
func liveAllowed(s auth.Session, group string) bool {
	g := liveGroups(s)
	return g == nil || slices.Contains(*g, group)
}

func (h *Handlers) LiveTvStatus(ctx context.Context, _ LiveTvStatusRequestObject) (LiveTvStatusResponseObject, error) {
	if _, ok := session(ctx); !ok {
		return LiveTvStatus401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	return LiveTvStatus200JSONResponse(h.liveStatus(ctx)), nil
}

func (h *Handlers) liveStatus(ctx context.Context) LiveTvStatus {
	n, until, sources := h.LiveTV.Status(ctx)
	out := LiveTvStatus{Enabled: h.LiveTV.Enabled(), Channels: n, GuideUntil: until}
	if s, ok := session(ctx); ok {
		if g := liveGroups(s); g != nil && len(*g) == 0 {
			out.Enabled = false // this profile may not watch Live TV
		}
	}
	if h.DVR != nil {
		out.DvrAvailable = ptr(h.DVR.Available())
		out.RecordingsActive = ptr(h.DVR.Active())
		if s, ok := session(ctx); ok {
			out.CanRecord = ptr(canRecord(s) && *out.DvrAvailable)
		}
	}
	out.Sources = make([]struct {
		Channels    int        `json:"channels"`
		Error       *string    `json:"error,omitempty"`
		Id          string     `json:"id"`
		Name        string     `json:"name"`
		Programmes  int        `json:"programmes"`
		RefreshedAt *time.Time `json:"refreshedAt,omitempty"`
	}, len(sources))
	for i, s := range sources {
		out.Sources[i].Id, out.Sources[i].Name, out.Sources[i].Channels, out.Sources[i].Programmes = s.ID, s.Name, s.Channels, s.Programmes
		out.Sources[i].RefreshedAt, out.Sources[i].Error = s.RefreshedAt, nz(s.Error)
	}
	return out
}

func (h *Handlers) ListLiveChannels(ctx context.Context, req ListLiveChannelsRequestObject) (ListLiveChannelsResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return ListLiveChannels401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	list, err := h.LiveTV.Channels(ctx, s.User.ID, liveFilter(s, req.Params.Group, req.Params.Favorites, req.Params.Recent, req.Params.Hidden))
	if err != nil {
		return nil, internal(ctx, "liveChannels", err)
	}
	out := make(ListLiveChannels200JSONResponse, len(list))
	for i, c := range list {
		out[i] = LiveChannel{Id: c.ID, Number: nz(c.Number), Name: c.Name, Group: nz(c.Group), Favorite: c.Favorite, Hidden: nz(c.Hidden),
			Now: toAPIProg(c.Now), Next: toAPIProg(c.Next)}
		if c.HasLogo {
			out[i].LogoUrl = ptr(logoURL(c.ID))
		}
	}
	return out, nil
}

func logoURL(id int64) string {
	return "/api/v1/livetv/channels/" + strconv.FormatInt(id, 10) + "/logo"
}

func (h *Handlers) ListLiveGroups(ctx context.Context, _ ListLiveGroupsRequestObject) (ListLiveGroupsResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return ListLiveGroups401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	counts, order, err := h.LiveTV.Groups(ctx)
	if err != nil {
		return nil, internal(ctx, "liveGroups", err)
	}
	out := ListLiveGroups200JSONResponse{}
	for _, g := range order {
		if liveAllowed(s, g) {
			out = append(out, struct {
				Channels int    `json:"channels"`
				Name     string `json:"name"`
			}{counts[g], g})
		}
	}
	return out, nil
}

func (h *Handlers) LiveGuide(ctx context.Context, req LiveGuideRequestObject) (LiveGuideResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return LiveGuide401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	from, to := req.Params.Start, req.Params.End
	if !to.After(from) || to.Sub(from) > 24*time.Hour {
		return LiveGuide400JSONResponse{BadRequestJSONResponse(apiErr("invalid", "end must be after start and at most 24 hours later"))}, nil
	}
	rows, err := h.LiveTV.Guide(ctx, s.User.ID, liveFilter(s, req.Params.Group, req.Params.Favorites, req.Params.Recent, nil), from, to)
	if err != nil {
		return nil, internal(ctx, "liveGuide", err)
	}
	out := make(LiveGuide200JSONResponse, len(rows))
	for i, r := range rows {
		out[i].ChannelId = r.ChannelID
		out[i].Programmes = make([]LiveProgramme, len(r.Programmes))
		for j := range r.Programmes {
			out[i].Programmes[j] = *toAPIProg(&r.Programmes[j])
		}
	}
	return out, nil
}

func (h *Handlers) FavoriteLiveChannel(ctx context.Context, req FavoriteLiveChannelRequestObject) (FavoriteLiveChannelResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return FavoriteLiveChannel401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	if err := h.LiveTV.SetFavorite(ctx, s.User.ID, req.ChannelId, true); errors.Is(err, livetv.ErrNotFound) {
		return FavoriteLiveChannel404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "channel not found"))}, nil
	} else if err != nil {
		return nil, internal(ctx, "favoriteChannel", err)
	}
	return FavoriteLiveChannel204Response{}, nil
}

func (h *Handlers) UnfavoriteLiveChannel(ctx context.Context, req UnfavoriteLiveChannelRequestObject) (UnfavoriteLiveChannelResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return UnfavoriteLiveChannel401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	if err := h.LiveTV.SetFavorite(ctx, s.User.ID, req.ChannelId, false); err != nil {
		return nil, internal(ctx, "unfavoriteChannel", err)
	}
	return UnfavoriteLiveChannel204Response{}, nil
}

func (h *Handlers) HideLiveChannel(ctx context.Context, req HideLiveChannelRequestObject) (HideLiveChannelResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return HideLiveChannel401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	err := h.LiveTV.SetHidden(ctx, s.User.ID, req.ChannelId, true)
	if errors.Is(err, livetv.ErrNotFound) {
		return HideLiveChannel404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "channel not found"))}, nil
	} else if err != nil {
		return nil, internal(ctx, "hideChannel", err)
	}
	return HideLiveChannel204Response{}, nil
}

func (h *Handlers) UnhideLiveChannel(ctx context.Context, req UnhideLiveChannelRequestObject) (UnhideLiveChannelResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return UnhideLiveChannel401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	err := h.LiveTV.SetHidden(ctx, s.User.ID, req.ChannelId, false)
	if errors.Is(err, livetv.ErrNotFound) {
		return UnhideLiveChannel404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "channel not found"))}, nil
	} else if err != nil {
		return nil, internal(ctx, "unhideChannel", err)
	}
	return UnhideLiveChannel204Response{}, nil
}

func (h *Handlers) LiveChannelLogo(ctx context.Context, req LiveChannelLogoRequestObject) (LiveChannelLogoResponseObject, error) {
	b, ct, err := h.LiveTV.Logo(ctx, req.ChannelId)
	if err != nil {
		return LiveChannelLogo404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "no logo"))}, nil
	}
	return LiveChannelLogo200ImageResponse{Body: bytes.NewReader(b), ContentType: ct, ContentLength: int64(len(b))}, nil
}

func (h *Handlers) PlayLiveChannel(ctx context.Context, req PlayLiveChannelRequestObject) (PlayLiveChannelResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return PlayLiveChannel401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	ri := requestInfo(ctx)
	remote := ri.Class == netclass.Remote
	u := s.User
	if remote && !(u.IsAdmin || u.Restrictions.RemoteAllowed()) {
		return PlayLiveChannel403JSONResponse{ForbiddenJSONResponse(apiErr("remote_not_allowed", "Live TV is only available at home for this profile."))}, nil
	}
	c, err := h.LiveTV.Channel(ctx, req.ChannelId)
	if errors.Is(err, livetv.ErrNotFound) {
		return PlayLiveChannel404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "channel not found"))}, nil
	} else if err != nil {
		return nil, internal(ctx, "playLive", err)
	}
	if !liveAllowed(s, c.Group) {
		return PlayLiveChannel403JSONResponse{ForbiddenJSONResponse(apiErr("not_allowed", "This channel isn't available for this profile."))}, nil
	}
	o := livetv.Options{VideoCodecs: req.Body.Profile.HlsVideoCodecs, AudioCodecs: req.Body.Profile.HlsAudioCodecs, Remote: remote, UserAgent: h.LiveTV.UserAgent(c.SourceID)}
	if req.Body.MaxBitrateKbps != nil {
		o.MaxKbps = *req.Body.MaxBitrateKbps
	}
	if remote && u.Restrictions.RemoteQualityKbps > 0 && (o.MaxKbps == 0 || u.Restrictions.RemoteQualityKbps < o.MaxKbps) {
		o.MaxKbps = u.Restrictions.RemoteQualityKbps
	}
	ls, err := h.LiveTV.Sessions.Start(ctx, c.ID, u.ID, c.StreamURL, o)
	if err != nil {
		if errors.Is(err, livetv.ErrUnavailable) {
			return PlayLiveChannel502JSONResponse{BadGatewayJSONResponse(apiErr("unavailable", err.Error()))}, nil
		}
		return nil, internal(ctx, "playLive", err)
	}
	h.LiveTV.Watched(ctx, u.ID, c.ID)
	return PlayLiveChannel200JSONResponse(LiveSession{Id: ls.ID, Url: "/api/v1/live/" + ls.ID + "/index.m3u8", ChannelId: c.ID,
		Method: LiveSessionMethod(ls.Method), VideoCodec: nz(ls.VideoCodec), AudioCodec: nz(ls.AudioCodec)}), nil
}

func (h *Handlers) StopLiveSession(ctx context.Context, req StopLiveSessionRequestObject) (StopLiveSessionResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return StopLiveSession401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	if ls := h.LiveTV.Sessions.Get(req.SessionId); ls != nil && (ls.UserID == s.User.ID || s.User.IsAdmin) {
		h.LiveTV.Sessions.Stop(req.SessionId)
	}
	return StopLiveSession204Response{}, nil
}

func (h *Handlers) RefreshLiveTv(ctx context.Context, _ RefreshLiveTvRequestObject) (RefreshLiveTvResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return RefreshLiveTv401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return RefreshLiveTv403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	h.LiveTV.Refresh(ctx)
	return RefreshLiveTv200JSONResponse(h.liveStatus(ctx)), nil
}
