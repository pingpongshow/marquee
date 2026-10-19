package api

import (
	"context"
	"errors"
	"math"

	"marquee/internal/auth"
	"marquee/internal/vodsave"
)

// Following series on demand: new episodes are saved to the library automatically, within
// the follow's size limit and the follower's DVR quota (vodsave/follow.go).

// MayFollowVOD reports whether a user may follow series on demand: on-demand access, and
// an admin or allowed to record.
func MayFollowVOD(u auth.User) bool {
	s := auth.Session{User: u}
	ok, _ := vodAccess(s)
	return ok && canRecord(s)
}

// roundGB keeps two decimals of a size in GB (the API sends float32s).
func roundGB(v float64) float64 { return math.Round(max(v, 0)*100) / 100 }

func gbToBytes(v float32) int64 { return int64(roundGB(float64(v)) * 1_000_000_000) }

func toAPIVodFollow(f vodsave.Follow) VodFollow {
	out := VodFollow{SeriesId: f.SeriesID, Title: f.Title, UserId: f.UserID, UserName: nz(f.UserName), UsedBytes: f.UsedBytes,
		EpisodesSaved: f.EpisodesSaved, EpisodesQueued: nz(f.EpisodesQueued), Status: f.Status, StatusText: nz(f.StatusText),
		CreatedAt: f.CreatedAt, LastNewAt: f.LastNewAt, MaxGb: ptr(float32(roundGB(float64(f.MaxBytes) / 1_000_000_000)))}
	if f.HasPoster && f.SeriesID > 0 {
		out.PosterUrl = ptr(vodSeriesImageURL(f.SeriesID, "poster"))
	}
	return out
}

// vodFollowing is VodSeries(Detail).following for the caller: true when they follow the
// series, false when they could follow it (following is on, they may, and its source's
// episodes can be saved), nil (not shown) otherwise.
func (h *Handlers) vodFollowing(ctx context.Context, s auth.Session, seriesID int64, sourceID string) *bool {
	if h.VODSaves == nil {
		return nil
	}
	if _, err := h.VODSaves.FollowOf(ctx, s.User.ID, seriesID); err == nil {
		return ptr(true)
	}
	if h.VODSaves.FollowAvailable() == nil && MayFollowVOD(s.User) && h.VOD.Savable(sourceID) {
		return ptr(false)
	}
	return nil
}

// markVodFollowing marks the series in a list the caller follows.
func (h *Handlers) markVodFollowing(ctx context.Context, s auth.Session, ss []VodSeries) {
	if h.VODSaves == nil || len(ss) == 0 {
		return
	}
	ids := make([]int64, len(ss))
	for i := range ss {
		ids[i] = ss[i].Id
	}
	followed := h.VODSaves.Followed(ctx, s.User.ID, ids)
	for i := range ss {
		if followed[ss[i].Id] {
			ss[i].Following = ptr(true)
		}
	}
}

func (h *Handlers) ListVodFollows(ctx context.Context, req ListVodFollowsRequestObject) (ListVodFollowsResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return ListVodFollows401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	all := req.Params.All != nil && *req.Params.All
	if all && !s.User.IsAdmin {
		return ListVodFollows403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	out := ListVodFollows200JSONResponse{}
	if h.VODSaves == nil {
		return out, nil
	}
	uid := s.User.ID
	if all {
		uid = 0
	}
	list, err := h.VODSaves.Follows(ctx, uid)
	if err != nil {
		return nil, internal(ctx, "listVodFollows", err)
	}
	for _, f := range list {
		out = append(out, toAPIVodFollow(f))
	}
	return out, nil
}

func (h *Handlers) FollowVodSeries(ctx context.Context, req FollowVodSeriesRequestObject) (FollowVodSeriesResponseObject, error) {
	s, maxRating, authed, allowed := h.vodSession(ctx)
	switch {
	case !authed:
		return FollowVodSeries401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !allowed:
		return FollowVodSeries403JSONResponse{ForbiddenJSONResponse(errVodNotAllowed)}, nil
	case !canRecord(s):
		return FollowVodSeries403JSONResponse{ForbiddenJSONResponse(apiErr("not_allowed", "This profile may not record, so it can't follow series."))}, nil
	case h.VODSaves == nil:
		return FollowVodSeries409JSONResponse{ConflictJSONResponse(apiErr("follow_off", "Following series isn't available on this server."))}, nil
	}
	m, err := h.vodSeries(ctx, s, maxRating, req.SeriesId)
	if err != nil {
		return nil, internal(ctx, "followVodSeries", err)
	}
	if m == nil {
		return FollowVodSeries404JSONResponse{NotFoundJSONResponse(errVodSeriesNotFound)}, nil
	}
	var o vodsave.FollowOptions
	if b := req.Body; b != nil {
		if b.MaxGb != nil {
			v := gbToBytes(*b.MaxGb)
			o.MaxBytes = &v
		}
		o.IncludeExisting = b.IncludeExisting != nil && *b.IncludeExisting
		o.Destination = b.Destination
	}
	f, err := h.VODSaves.FollowSeries(ctx, s.User.ID, m.ID, o)
	conflict := func(code, msg string) (FollowVodSeriesResponseObject, error) {
		return FollowVodSeries409JSONResponse{ConflictJSONResponse(apiErr(code, msg))}, nil
	}
	switch {
	case err == nil:
		return FollowVodSeries200JSONResponse(toAPIVodFollow(f)), nil
	case errors.Is(err, vodsave.ErrFollowOff):
		return conflict("follow_off", "Following series is turned off (Settings → VOD).")
	case errors.Is(err, vodsave.ErrNoFolder):
		return conflict("no_folder", "There's no shows folder to save episodes to (Settings → VOD).")
	case errors.Is(err, vodsave.ErrDestination):
		return conflict("destination", err.Error())
	case errors.Is(err, vodsave.ErrNotSavable):
		return conflict("not_savable", "Episodes from this source can't be saved.")
	case errors.Is(err, vodsave.ErrNotFound):
		return FollowVodSeries404JSONResponse{NotFoundJSONResponse(errVodSeriesNotFound)}, nil
	}
	return nil, internal(ctx, "followVodSeries", err)
}

func (h *Handlers) UnfollowVodSeries(ctx context.Context, req UnfollowVodSeriesRequestObject) (UnfollowVodSeriesResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return UnfollowVodSeries401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	notFollowed := UnfollowVodSeries404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "not followed"))}
	uid := s.User.ID
	if p := req.Params.UserId; p != nil && *p != uid {
		if !s.User.IsAdmin {
			return notFollowed, nil
		}
		uid = *p
	}
	if h.VODSaves == nil {
		return notFollowed, nil
	}
	err := h.VODSaves.Unfollow(ctx, uid, req.SeriesId)
	switch {
	case errors.Is(err, vodsave.ErrNotFollowed):
		return notFollowed, nil
	case err != nil:
		return nil, internal(ctx, "unfollowVodSeries", err)
	}
	return UnfollowVodSeries204Response{}, nil
}
