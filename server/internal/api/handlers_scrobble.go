package api

import (
	"context"
	"errors"
	"strings"

	"marquee/internal/scrobble"
)

// Scrobbling to ListenBrainz (MUSIC-12, D80).

func toAPIScrobble(st scrobble.Status) ScrobbleStatus {
	return ScrobbleStatus{Connected: st.Connected, Username: nz(st.Username), Error: nz(st.Error)}
}

func (h *Handlers) ListenBrainzStatus(ctx context.Context, _ ListenBrainzStatusRequestObject) (ListenBrainzStatusResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return ListenBrainzStatus401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	return ListenBrainzStatus200JSONResponse(toAPIScrobble(h.Scrobble.Status(ctx, s.User.ID))), nil
}

func (h *Handlers) ConnectListenBrainz(ctx context.Context, req ConnectListenBrainzRequestObject) (ConnectListenBrainzResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return ConnectListenBrainz401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	token := strings.TrimSpace(req.Body.Token)
	if token == "" {
		return ConnectListenBrainz400JSONResponse{BadRequestJSONResponse(apiErr("invalid", "a token is required"))}, nil
	}
	st, err := h.Scrobble.Connect(ctx, s.User.ID, token)
	if errors.Is(err, scrobble.ErrBadToken) {
		return ConnectListenBrainz400JSONResponse{BadRequestJSONResponse(apiErr("invalid_token", err.Error()))}, nil
	}
	if err != nil {
		return ConnectListenBrainz502JSONResponse{BadGatewayJSONResponse(apiErr("unreachable", "Couldn't reach ListenBrainz: "+err.Error()))}, nil
	}
	return ConnectListenBrainz200JSONResponse(toAPIScrobble(st)), nil
}

func (h *Handlers) DisconnectListenBrainz(ctx context.Context, _ DisconnectListenBrainzRequestObject) (DisconnectListenBrainzResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return DisconnectListenBrainz401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	if err := h.Scrobble.Disconnect(ctx, s.User.ID); err != nil {
		return nil, internal(ctx, "disconnectListenBrainz", err)
	}
	return DisconnectListenBrainz204Response{}, nil
}
