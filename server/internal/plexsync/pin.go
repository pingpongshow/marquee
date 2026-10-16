package plexsync

import (
	"context"
	"errors"
	"net/url"

	"marquee/internal/settings"
)

// PinRequest is a started Sign in with Plex: the admin opens AuthURL (or enters Code at
// plex.tv/link) and Marquee polls CheckPin.
type PinRequest struct {
	ID      int64
	Code    string
	AuthURL string
}

// StartPin asks plex.tv for a sign-in PIN.
func (s *Service) StartPin(ctx context.Context) (PinRequest, error) {
	_, tv := s.clients(s.Settings.Get().PlexSync)
	tv.retries = 0
	p, err := tv.createPin(ctx)
	if err != nil {
		return PinRequest{}, errors.New(tv.secrets.redact(err.Error()))
	}
	q := url.Values{"clientID": {tv.clientID}, "code": {p.Code}, "context[device][product]": {"Marquee"}}
	return PinRequest{ID: p.ID, Code: p.Code, AuthURL: "https://app.plex.tv/auth#?" + q.Encode()}, nil
}

// CheckPin reports whether the admin has approved the PIN; once they have, the token is
// saved as the sync's token.
func (s *Service) CheckPin(ctx context.Context, id int64) (bool, error) {
	_, tv := s.clients(s.Settings.Get().PlexSync)
	tv.retries = 0
	p, err := tv.checkPin(ctx, id)
	if err != nil {
		return false, errors.New(tv.secrets.redact(err.Error()))
	}
	if p.AuthToken == "" {
		return false, nil
	}
	_, err = s.Settings.Update(ctx, func(st *settings.Settings) error {
		st.PlexSync.Token = p.AuthToken
		return nil
	})
	return err == nil, err
}
