package api

import "context"

// Following series on demand: new episodes saved automatically (placeholders until built).

func (h *Handlers) ListVodFollows(ctx context.Context, req ListVodFollowsRequestObject) (ListVodFollowsResponseObject, error) {
	if _, ok := session(ctx); !ok {
		return ListVodFollows401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	return ListVodFollows200JSONResponse{}, nil
}

func (h *Handlers) FollowVodSeries(ctx context.Context, req FollowVodSeriesRequestObject) (FollowVodSeriesResponseObject, error) {
	if _, ok := session(ctx); !ok {
		return FollowVodSeries401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	return FollowVodSeries409JSONResponse{ConflictJSONResponse(apiErr("follow_off", "following series isn't available yet"))}, nil
}

func (h *Handlers) UnfollowVodSeries(ctx context.Context, req UnfollowVodSeriesRequestObject) (UnfollowVodSeriesResponseObject, error) {
	if _, ok := session(ctx); !ok {
		return UnfollowVodSeries401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	return UnfollowVodSeries404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "not followed"))}, nil
}
