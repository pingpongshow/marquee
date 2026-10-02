package api

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"marquee/internal/items"
	"marquee/internal/metadata"
	"marquee/internal/metadata/tmdb"
)

func (h *Handlers) EditItem(ctx context.Context, req EditItemRequestObject) (EditItemResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return EditItem401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return EditItem403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	b := req.Body
	set := map[string]any{}
	str := func(name string, v *string) {
		if v != nil {
			set[name] = strings.TrimSpace(*v)
		}
	}
	str("title", b.Title)
	str("sortTitle", b.SortTitle)
	str("originalTitle", b.OriginalTitle)
	str("summary", b.Summary)
	str("tagline", b.Tagline)
	str("contentRating", b.ContentRating)
	str("studio", b.Studio)
	if t, ok := set["title"].(string); ok && t == "" {
		return EditItem400JSONResponse{BadRequestJSONResponse(apiErr("invalid", "title cannot be empty"))}, nil
	}
	if b.Year != nil {
		set["year"] = *b.Year
	}
	if b.OriginallyAvailableAt != nil {
		set["originallyAvailableAt"] = b.OriginallyAvailableAt.Format("2006-01-02")
		if _, given := set["year"]; !given {
			set["year"] = b.OriginallyAvailableAt.Year()
		}
	}
	var unlock []string
	if b.Unlock != nil {
		for _, u := range *b.Unlock {
			unlock = append(unlock, string(u))
		}
	}
	if err := h.Items.Edit(ctx, req.ItemId, set, unlock); errors.Is(err, items.ErrNotFound) {
		return EditItem404JSONResponse{NotFoundJSONResponse(apiErr("not_found", err.Error()))}, nil
	} else if err != nil {
		return nil, internal(ctx, "editItem", err)
	}
	out, err := h.detail(ctx, req.ItemId)
	if err != nil {
		return nil, internal(ctx, "editItem", err)
	}
	return EditItem200JSONResponse(out), nil
}

// metaError maps metadata errors to a user-facing message; ok=false means internal error.
func metaError(err error) (string, bool) {
	switch {
	case errors.Is(err, metadata.ErrNotMatchable), errors.Is(err, metadata.ErrNotMatched):
		return err.Error(), true
	case errors.Is(err, tmdb.ErrNoKey):
		return "add a TMDB API key in Settings → Metadata first", true
	case errors.Is(err, tmdb.ErrInvalidKey):
		return err.Error(), true
	case errors.Is(err, tmdb.ErrNotFound):
		return "that entry no longer exists on TMDB", true
	}
	return "", false
}

func (h *Handlers) SearchMatches(ctx context.Context, req SearchMatchesRequestObject) (SearchMatchesResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return SearchMatches401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return SearchMatches403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	title, year := "", 0
	set(&title, req.Params.Title)
	set(&year, req.Params.Year)
	cands, err := h.Metadata.Candidates(ctx, req.ItemId, strings.TrimSpace(title), year)
	if err != nil {
		if items.IsNotFound(err) {
			return SearchMatches404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "item not found"))}, nil
		}
		if msg, ok := metaError(err); ok {
			return SearchMatches400JSONResponse{BadRequestJSONResponse(apiErr("metadata", msg))}, nil
		}
		return nil, internal(ctx, "searchMatches", err)
	}
	out := make(SearchMatches200JSONResponse, len(cands))
	for i, c := range cands {
		out[i] = MatchCandidate{Provider: MatchCandidateProvider(c.Provider), Id: c.ID, Title: c.Title, OriginalTitle: nz(c.Original),
			Year: nz(c.Year), Overview: nz(c.Overview), PosterUrl: nz(c.PosterURL), Current: ptr(c.Current)}
	}
	return out, nil
}

func (h *Handlers) ApplyMatch(ctx context.Context, req ApplyMatchRequestObject) (ApplyMatchResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return ApplyMatch401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return ApplyMatch403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	id, err := strconv.Atoi(req.Body.Id)
	if err != nil || id <= 0 {
		return ApplyMatch400JSONResponse{BadRequestJSONResponse(apiErr("invalid", "invalid TMDB id"))}, nil
	}
	if err := h.Metadata.MatchTo(ctx, req.ItemId, id); err != nil {
		if items.IsNotFound(err) {
			return ApplyMatch404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "item not found"))}, nil
		}
		if msg, ok := metaError(err); ok {
			return ApplyMatch400JSONResponse{BadRequestJSONResponse(apiErr("metadata", msg))}, nil
		}
		return nil, internal(ctx, "applyMatch", err)
	}
	out, err := h.detail(ctx, req.ItemId)
	if err != nil {
		return nil, internal(ctx, "applyMatch", err)
	}
	return ApplyMatch200JSONResponse(out), nil
}

func (h *Handlers) RefreshItem(ctx context.Context, req RefreshItemRequestObject) (RefreshItemResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return RefreshItem401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return RefreshItem403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	if err := h.Metadata.Refresh(ctx, req.ItemId); err != nil {
		if items.IsNotFound(err) {
			return RefreshItem404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "item not found"))}, nil
		}
		if msg, ok := metaError(err); ok {
			return RefreshItem400JSONResponse{BadRequestJSONResponse(apiErr("metadata", msg))}, nil
		}
		return nil, internal(ctx, "refresh", err)
	}
	out, err := h.detail(ctx, req.ItemId)
	if err != nil {
		return nil, internal(ctx, "refresh", err)
	}
	return RefreshItem200JSONResponse(out), nil
}
