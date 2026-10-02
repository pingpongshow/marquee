package api

import (
	"context"
	"errors"
	"sync"
	"time"

	"marquee/internal/auth"
)

// quickConnectAttempts limits how fast one user can try codes (they're short).
var quickConnectAttempts = struct {
	sync.Mutex
	m map[int64][]time.Time
}{m: map[int64][]time.Time{}}

func allowQuickConnectAttempt(userID int64) bool {
	quickConnectAttempts.Lock()
	defer quickConnectAttempts.Unlock()
	now := time.Now()
	recent := quickConnectAttempts.m[userID][:0]
	for _, t := range quickConnectAttempts.m[userID] {
		if now.Sub(t) < time.Minute {
			recent = append(recent, t)
		}
	}
	if len(recent) >= 10 {
		quickConnectAttempts.m[userID] = recent
		return false
	}
	quickConnectAttempts.m[userID] = append(recent, now)
	return true
}

func (h *Handlers) StartQuickConnect(ctx context.Context, req StartQuickConnectRequestObject) (StartQuickConnectResponseObject, error) {
	if !validDevice(req.Body.Device) {
		return StartQuickConnect400JSONResponse{BadRequestJSONResponse(apiErr("invalid_device", "device clientId and name are required"))}, nil
	}
	r, err := h.QuickConnect.Start(toDevice(req.Body.Device), requestInfo(ctx).ClientIP)
	if errors.Is(err, auth.ErrQuickConnectBusy) {
		return StartQuickConnect429JSONResponse{TooManyRequestsJSONResponse(apiErr("busy", err.Error()))}, nil
	}
	if err != nil {
		return nil, internal(ctx, "quickConnect", err)
	}
	return StartQuickConnect200JSONResponse{Code: r.Code, Secret: r.Secret, ExpiresAt: r.Expires, PollIntervalMs: 2000}, nil
}

func (h *Handlers) PollQuickConnect(ctx context.Context, req PollQuickConnectRequestObject) (PollQuickConnectResponseObject, error) {
	r, ok := h.QuickConnect.Poll(req.Secret)
	if !ok {
		return PollQuickConnect200JSONResponse{Status: QuickConnectStateStatusExpired}, nil
	}
	if !r.Approved {
		return PollQuickConnect200JSONResponse{Status: QuickConnectStateStatusPending}, nil
	}
	u, err := h.Auth.GetUser(ctx, r.UserID)
	if err != nil {
		return nil, internal(ctx, "quickConnect", err)
	}
	token, err := h.Auth.IssueToken(ctx, h.DB, u.ID, r.Device, requestInfo(ctx).ClientIP)
	if err != nil {
		return nil, internal(ctx, "quickConnect", err)
	}
	return PollQuickConnect200JSONResponse{Status: QuickConnectStateStatusApproved, Auth: &AuthResult{Token: token, User: toAPIUser(u)}}, nil
}

func (h *Handlers) AuthorizeQuickConnect(ctx context.Context, req AuthorizeQuickConnectRequestObject) (AuthorizeQuickConnectResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return AuthorizeQuickConnect401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	if !allowQuickConnectAttempt(s.User.ID) {
		return AuthorizeQuickConnect429JSONResponse{TooManyRequestsJSONResponse(apiErr("rate_limited", "too many attempts; wait a minute"))}, nil
	}
	d, err := h.QuickConnect.Approve(req.Body.Code, s.User.ID)
	if errors.Is(err, auth.ErrQuickConnectUnknown) {
		return AuthorizeQuickConnect404JSONResponse{NotFoundJSONResponse(apiErr("not_found", err.Error()))}, nil
	}
	if err != nil {
		return nil, internal(ctx, "quickConnect", err)
	}
	return AuthorizeQuickConnect200JSONResponse{DeviceName: d.Name}, nil
}
