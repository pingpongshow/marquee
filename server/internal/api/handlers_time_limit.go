package api

import "context"

// MyTimeLimit is the caller's time limit today (placeholder until built: no limits).
func (h *Handlers) MyTimeLimit(ctx context.Context, _ MyTimeLimitRequestObject) (MyTimeLimitResponseObject, error) {
	if _, ok := session(ctx); !ok {
		return MyTimeLimit401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	return MyTimeLimit200JSONResponse{Limited: false, AllowedNow: true}, nil
}
