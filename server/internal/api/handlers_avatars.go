package api

import (
	"context"
	"errors"
	"fmt"
	"os"

	"marquee/internal/auth"
	"marquee/internal/avatars"
)

func avatarURL(u auth.User) *string {
	if u.Avatar == 0 {
		return nil
	}
	return ptr(fmt.Sprintf("/api/v1/users/%d/avatar?v=%d", u.ID, u.Avatar))
}

func toAPIProfile(u auth.User) Profile {
	return Profile{Id: u.ID, DisplayName: u.DisplayName, IsManaged: u.IsManaged,
		Requires: ProfileRequires(auth.SwitchCredentials(u)), AvatarUrl: avatarURL(u)}
}

// avatarTarget checks that the caller may change userID's picture: themselves, or anyone as an admin.
func avatarTarget(ctx context.Context, userID int64) (authed, allowed bool) {
	s, ok := session(ctx)
	if !ok {
		return false, false
	}
	return true, s.User.IsAdmin || s.User.ID == userID
}

func (h *Handlers) GetAvatar(ctx context.Context, req GetAvatarRequestObject) (GetAvatarResponseObject, error) {
	notFound := GetAvatar404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "no profile picture"))}
	// Same visibility as the names on the sign-in picker; signed-in users can see everyone's.
	if _, ok := session(ctx); !ok && !h.pinSignInAllowed(ctx) {
		return notFound, nil
	}
	f, err := os.Open(h.Avatars.Path(req.UserId))
	if errors.Is(err, os.ErrNotExist) {
		return notFound, nil
	}
	if err != nil {
		return nil, internal(ctx, "avatar", err)
	}
	st, _ := f.Stat()
	cache := "private, max-age=31536000, immutable"
	if req.Params.V == nil {
		cache = "private, no-cache"
	}
	return GetAvatar200ImagejpegResponse{Body: f, ContentLength: st.Size(), Headers: GetAvatar200ResponseHeaders{CacheControl: &cache}}, nil
}

func (h *Handlers) SetAvatar(ctx context.Context, req SetAvatarRequestObject) (SetAvatarResponseObject, error) {
	switch authed, ok := avatarTarget(ctx, req.UserId); {
	case !authed:
		return SetAvatar401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !ok:
		return SetAvatar403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	if _, err := h.Auth.GetUser(ctx, req.UserId); errors.Is(err, auth.ErrUserNotFound) {
		return SetAvatar404JSONResponse{NotFoundJSONResponse(apiErr("not_found", err.Error()))}, nil
	} else if err != nil {
		return nil, internal(ctx, "setAvatar", err)
	}
	if _, err := h.Avatars.Save(ctx, req.UserId, req.Body); err != nil {
		if errors.Is(err, avatars.ErrInvalid) || errors.Is(err, avatars.ErrTooLarge) {
			return SetAvatar400JSONResponse{BadRequestJSONResponse(apiErr("invalid_image", err.Error()))}, nil
		}
		return nil, internal(ctx, "setAvatar", err)
	}
	u, err := h.Auth.GetUser(ctx, req.UserId)
	if err != nil {
		return nil, internal(ctx, "setAvatar", err)
	}
	return SetAvatar200JSONResponse(toAPIUser(u)), nil
}

func (h *Handlers) DeleteAvatar(ctx context.Context, req DeleteAvatarRequestObject) (DeleteAvatarResponseObject, error) {
	switch authed, ok := avatarTarget(ctx, req.UserId); {
	case !authed:
		return DeleteAvatar401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !ok:
		return DeleteAvatar403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	if _, err := h.Auth.GetUser(ctx, req.UserId); errors.Is(err, auth.ErrUserNotFound) {
		return DeleteAvatar404JSONResponse{NotFoundJSONResponse(apiErr("not_found", err.Error()))}, nil
	} else if err != nil {
		return nil, internal(ctx, "deleteAvatar", err)
	}
	if err := h.Avatars.Remove(ctx, req.UserId); err != nil {
		return nil, internal(ctx, "deleteAvatar", err)
	}
	u, err := h.Auth.GetUser(ctx, req.UserId)
	if err != nil {
		return nil, internal(ctx, "deleteAvatar", err)
	}
	return DeleteAvatar200JSONResponse(toAPIUser(u)), nil
}
