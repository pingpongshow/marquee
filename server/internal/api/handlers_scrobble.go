package api

import (
	"context"
	"errors"
	"strings"

	"marquee/internal/scrobble"
)

// Scrobbling to ListenBrainz and Last.fm (MUSIC-12, D80, D81).

func toAPIScrobble(st scrobble.Status) ScrobbleStatus {
	return ScrobbleStatus{Connected: st.Connected, Username: nz(st.Username), Error: nz(st.Error)}
}

func (h *Handlers) ListenBrainzStatus(ctx context.Context, _ ListenBrainzStatusRequestObject) (ListenBrainzStatusResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return ListenBrainzStatus401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	return ListenBrainzStatus200JSONResponse(toAPIScrobble(h.Scrobble.Status(ctx, s.User.ID, "listenbrainz"))), nil
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
	if err := h.Scrobble.Disconnect(ctx, s.User.ID, "listenbrainz"); err != nil {
		return nil, internal(ctx, "disconnectListenBrainz", err)
	}
	return DisconnectListenBrainz204Response{}, nil
}

func (h *Handlers) LastFmStatus(ctx context.Context, _ LastFmStatusRequestObject) (LastFmStatusResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return LastFmStatus401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	out := toAPIScrobble(h.Scrobble.Status(ctx, s.User.ID, "lastfm"))
	out.Available = ptr(h.Scrobble.LastFMAvailable())
	return LastFmStatus200JSONResponse(out), nil
}

func (h *Handlers) LastFmAuthUrl(ctx context.Context, req LastFmAuthUrlRequestObject) (LastFmAuthUrlResponseObject, error) {
	if _, ok := session(ctx); !ok {
		return LastFmAuthUrl401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	cb := ""
	if req.Body.CallbackUrl != nil {
		cb = *req.Body.CallbackUrl
	}
	u, err := h.Scrobble.LastFMAuthURL(cb)
	if err != nil {
		return LastFmAuthUrl503JSONResponse{ServiceUnavailableJSONResponse(apiErr("not_configured", err.Error()))}, nil
	}
	return LastFmAuthUrl200JSONResponse{Url: u}, nil
}

func (h *Handlers) ConnectLastFm(ctx context.Context, req ConnectLastFmRequestObject) (ConnectLastFmResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return ConnectLastFm401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	st, err := h.Scrobble.ConnectLastFM(ctx, s.User.ID, strings.TrimSpace(req.Body.Token))
	switch {
	case errors.Is(err, scrobble.ErrLastFMOff), errors.Is(err, scrobble.ErrLastFMKey):
		return ConnectLastFm503JSONResponse{ServiceUnavailableJSONResponse(apiErr("not_configured", err.Error()))}, nil
	case errors.Is(err, scrobble.ErrBadToken):
		return ConnectLastFm400JSONResponse{BadRequestJSONResponse(apiErr("invalid_token", "Last.fm didn't accept the approval; try connecting again"))}, nil
	case err != nil:
		return ConnectLastFm502JSONResponse{BadGatewayJSONResponse(apiErr("unreachable", "Couldn't reach Last.fm: "+err.Error()))}, nil
	}
	out := toAPIScrobble(st)
	out.Available = ptr(true)
	return ConnectLastFm200JSONResponse(out), nil
}

func (h *Handlers) DisconnectLastFm(ctx context.Context, _ DisconnectLastFmRequestObject) (DisconnectLastFmResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return DisconnectLastFm401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	if err := h.Scrobble.Disconnect(ctx, s.User.ID, "lastfm"); err != nil {
		return nil, internal(ctx, "disconnectLastFm", err)
	}
	return DisconnectLastFm204Response{}, nil
}
