package api

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"marquee/internal/appearance"
	"marquee/internal/settings"
)

// toAPIAppearance works out the theme to show at this moment, in the server's time zone.
func toAPIAppearance(a settings.Appearance, now time.Time) Appearance {
	active, holiday := appearance.Active(a.ThemeID, a.AutoHolidays, now)
	chosen := a.ThemeID
	if !appearance.KnownTheme(chosen) {
		chosen = appearance.DefaultTheme
	}
	out := Appearance{
		ActiveThemeId: active,
		ThemeId:       chosen,
		AutoHolidays:  a.AutoHolidays,
		Background:    AppearanceBackground{Mode: AppearanceBackgroundMode(a.BackgroundMode), Dim: dimStrength(a.BackgroundDim)},
	}
	if holiday != "" {
		out.HolidayId = &holiday
	}
	if a.BackgroundVersion != 0 {
		out.Background.ImageUrl = ptr(fmt.Sprintf("/api/v1/appearance/background?v=%d", a.BackgroundVersion))
	}
	return out
}

// dimStrength is how far the image fades towards the theme's own background, in percent. A
// value stored as a signed darken or lighten amount means its size.
func dimStrength(dim int) int {
	if dim < 0 {
		return -dim
	}
	return dim
}

func (h *Handlers) GetAppearance(_ context.Context, _ GetAppearanceRequestObject) (GetAppearanceResponseObject, error) {
	return GetAppearance200JSONResponse(toAPIAppearance(h.Settings.Get().Appearance, time.Now())), nil
}

func (h *Handlers) UpdateAppearance(ctx context.Context, req UpdateAppearanceRequestObject) (UpdateAppearanceResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return UpdateAppearance401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return UpdateAppearance403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	b := req.Body
	next, err := h.Settings.Update(ctx, func(s *settings.Settings) error {
		a := &s.Appearance
		if b.ThemeId != nil {
			if !appearance.KnownTheme(*b.ThemeId) {
				return &settings.ValidationError{Msg: "there's no such theme"}
			}
			a.ThemeID = *b.ThemeId
		}
		set(&a.AutoHolidays, b.AutoHolidays)
		if b.BackgroundMode != nil {
			a.BackgroundMode = string(*b.BackgroundMode)
		}
		if b.BackgroundDim != nil {
			a.BackgroundDim = dimStrength(*b.BackgroundDim)
		}
		return nil
	})
	var ve *settings.ValidationError
	if errors.As(err, &ve) {
		return UpdateAppearance400JSONResponse{BadRequestJSONResponse(apiErr("invalid", ve.Msg))}, nil
	}
	if err != nil {
		return nil, internal(ctx, "updateAppearance", err)
	}
	return UpdateAppearance200JSONResponse(toAPIAppearance(next.Appearance, time.Now())), nil
}

func (h *Handlers) GetAppearanceBackground(ctx context.Context, req GetAppearanceBackgroundRequestObject) (GetAppearanceBackgroundResponseObject, error) {
	notFound := GetAppearanceBackground404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "no background image"))}
	a := h.Settings.Get().Appearance
	if a.BackgroundVersion == 0 {
		return notFound, nil
	}
	f, err := os.Open(h.Backdrop.Path(a.BackgroundFormat))
	if errors.Is(err, os.ErrNotExist) {
		return notFound, nil
	}
	if err != nil {
		return nil, internal(ctx, "appearanceBackground", err)
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, internal(ctx, "appearanceBackground", err)
	}
	hdr := GetAppearanceBackground200ResponseHeaders{CacheControl: ptr("no-cache")}
	if req.Params.V != nil && *req.Params.V == a.BackgroundVersion {
		hdr.CacheControl = ptr(immutable)
	}
	if a.BackgroundFormat == "png" {
		return GetAppearanceBackground200ImagepngResponse{Body: f, ContentLength: st.Size(), Headers: hdr}, nil
	}
	return GetAppearanceBackground200ImagejpegResponse{Body: f, ContentLength: st.Size(), Headers: hdr}, nil
}

func (h *Handlers) SetAppearanceBackground(ctx context.Context, req SetAppearanceBackgroundRequestObject) (SetAppearanceBackgroundResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return SetAppearanceBackground401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return SetAppearanceBackground403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	var next settings.Settings
	err := h.Backdrop.Replace(req.Body, func(format string) (err error) {
		next, err = h.Settings.Update(ctx, func(s *settings.Settings) error {
			s.Appearance.BackgroundFormat = format
			s.Appearance.BackgroundVersion = time.Now().UnixMilli()
			return nil
		})
		return err
	})
	if errors.Is(err, appearance.ErrInvalid) || errors.Is(err, appearance.ErrTooLarge) {
		return SetAppearanceBackground400JSONResponse{BadRequestJSONResponse(apiErr("invalid_image", err.Error()))}, nil
	}
	if err != nil {
		return nil, internal(ctx, "setAppearanceBackground", err)
	}
	return SetAppearanceBackground200JSONResponse(toAPIAppearance(next.Appearance, time.Now())), nil
}

func (h *Handlers) DeleteAppearanceBackground(ctx context.Context, _ DeleteAppearanceBackgroundRequestObject) (DeleteAppearanceBackgroundResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return DeleteAppearanceBackground401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return DeleteAppearanceBackground403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	var next settings.Settings
	err := h.Backdrop.Clear(func() (err error) {
		next, err = h.Settings.Update(ctx, func(s *settings.Settings) error {
			s.Appearance.BackgroundFormat = ""
			s.Appearance.BackgroundVersion = 0
			return nil
		})
		return err
	})
	if err != nil {
		return nil, internal(ctx, "deleteAppearanceBackground", err)
	}
	return DeleteAppearanceBackground200JSONResponse(toAPIAppearance(next.Appearance, time.Now())), nil
}
