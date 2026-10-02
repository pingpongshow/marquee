package api

import (
	"context"
	"net/url"

	"marquee/internal/settings"
)

// TestWebhook sends a test event so admins can check a receiver (ADM-5).
func (h *Handlers) TestWebhook(ctx context.Context, req TestWebhookRequestObject) (TestWebhookResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return TestWebhook401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return TestWebhook403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	u, err := url.Parse(req.Body.Url)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return TestWebhook400JSONResponse{BadRequestJSONResponse(apiErr("invalid", "an http(s) URL is required"))}, nil
	}
	hook := settings.Webhook{Name: "test", URL: req.Body.Url}
	if req.Body.Secret != nil {
		hook.Secret = *req.Body.Secret
	}
	status, err := h.Webhooks.SendTest(ctx, hook)
	out := TestWebhook200JSONResponse{Ok: err == nil, Status: nz(status)}
	if err != nil {
		out.Error = ptr(err.Error())
	}
	return out, nil
}
