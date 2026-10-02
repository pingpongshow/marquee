package api

import (
	"context"
	"errors"

	"marquee/internal/auth"
)

// Two-factor sign-in (TOTP).

func (h *Handlers) TotpStatus(ctx context.Context, _ TotpStatusRequestObject) (TotpStatusResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return TotpStatus401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	on, left := h.Auth.TOTPEnabled(ctx, s.User.ID)
	out := TotpStatus200JSONResponse{Enabled: on}
	if on {
		out.RecoveryCodesLeft = ptr(left)
	}
	return out, nil
}

func (h *Handlers) SetupTotp(ctx context.Context, _ SetupTotpRequestObject) (SetupTotpResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return SetupTotp401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	secret, url, err := h.Auth.SetupTOTP(ctx, s.User.ID, s.User.Username, "Marquee ("+h.Settings.Get().General.ServerName+")")
	if errors.Is(err, auth.ErrTOTPEnabled) {
		return SetupTotp409JSONResponse{ConflictJSONResponse(apiErr("enabled", err.Error()))}, nil
	} else if err != nil {
		return nil, internal(ctx, "setupTotp", err)
	}
	return SetupTotp200JSONResponse{Secret: secret, OtpauthUrl: url}, nil
}

func (h *Handlers) EnableTotp(ctx context.Context, req EnableTotpRequestObject) (EnableTotpResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return EnableTotp401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	codes, err := h.Auth.EnableTOTP(ctx, s.User.ID, req.Body.Code)
	switch {
	case errors.Is(err, auth.ErrTOTPInvalid), errors.Is(err, auth.ErrTOTPNotSetUp):
		return EnableTotp400JSONResponse{BadRequestJSONResponse(apiErr("invalid_totp", err.Error()))}, nil
	case err != nil:
		return nil, internal(ctx, "enableTotp", err)
	}
	return EnableTotp200JSONResponse{RecoveryCodes: codes}, nil
}

func (h *Handlers) DisableTotp(ctx context.Context, req DisableTotpRequestObject) (DisableTotpResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return DisableTotp401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	target := s.User.ID
	if req.Body.UserId != nil && *req.Body.UserId != s.User.ID {
		// An admin helping someone who lost their phone and recovery codes.
		if !s.User.IsAdmin {
			return DisableTotp403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
		}
		target = *req.Body.UserId
	} else {
		code := ""
		set(&code, req.Body.Code)
		if on, _ := h.Auth.TOTPEnabled(ctx, target); on {
			if err := h.Auth.CheckTOTP(ctx, target, code); err != nil {
				return DisableTotp400JSONResponse{BadRequestJSONResponse(apiErr("invalid_totp", err.Error()))}, nil
			}
		}
	}
	if err := h.Auth.DisableTOTP(ctx, target); err != nil {
		return nil, internal(ctx, "disableTotp", err)
	}
	return DisableTotp204Response{}, nil
}
