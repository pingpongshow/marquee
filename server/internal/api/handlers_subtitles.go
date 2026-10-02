package api

import (
	"context"
	"database/sql"
	"errors"

	"marquee/internal/subtitles"
)

func (h *Handlers) SearchSubtitles(ctx context.Context, req SearchSubtitlesRequestObject) (SearchSubtitlesResponseObject, error) {
	if _, ok := session(ctx); !ok {
		return SearchSubtitles401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	if h.Subtitles == nil || h.Items.Visible(ctx, access(ctx), req.ItemId) != nil {
		return SearchSubtitles404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "item not found"))}, nil
	}
	langs := "en"
	if req.Params.Languages != nil && *req.Params.Languages != "" {
		langs = *req.Params.Languages
	}
	res, err := h.Subtitles.Search(ctx, req.ItemId, langs)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return SearchSubtitles404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "this item has no file"))}, nil
	case errors.Is(err, subtitles.ErrNotVideo), errors.Is(err, subtitles.ErrNotConfigured), errors.Is(err, subtitles.ErrInvalid):
		return SearchSubtitles400JSONResponse{BadRequestJSONResponse(apiErr("not_available", err.Error()))}, nil
	case err != nil:
		return SearchSubtitles502JSONResponse{BadGatewayJSONResponse(apiErr("provider", err.Error()))}, nil
	}
	out := make(SearchSubtitles200JSONResponse, len(res))
	for i, r := range res {
		out[i] = SubtitleResult{FileId: r.FileID, Language: r.Language, Release: r.Release, FileName: nz(r.FileName), Downloads: r.Downloads,
			HearingImpaired: r.HearingImpaired, ForeignPartsOnly: nz(r.ForeignOnly), AiTranslated: r.AITranslated, HashMatch: r.HashMatch}
	}
	return out, nil
}

func (h *Handlers) DownloadSubtitle(ctx context.Context, req DownloadSubtitleRequestObject) (DownloadSubtitleResponseObject, error) {
	if _, ok := session(ctx); !ok {
		return DownloadSubtitle401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	if h.Subtitles == nil || h.Items.Visible(ctx, access(ctx), req.ItemId) != nil {
		return DownloadSubtitle404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "item not found"))}, nil
	}
	b := req.Body
	release := ""
	if b.Release != nil {
		release = *b.Release
	}
	id, left, err := h.Subtitles.Download(ctx, req.ItemId, b.FileId, b.Language, release, b.HearingImpaired != nil && *b.HearingImpaired)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return DownloadSubtitle404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "this item has no file"))}, nil
	case errors.Is(err, subtitles.ErrNotVideo), errors.Is(err, subtitles.ErrNotConfigured):
		return DownloadSubtitle400JSONResponse{BadRequestJSONResponse(apiErr("not_available", err.Error()))}, nil
	case err != nil:
		return DownloadSubtitle502JSONResponse{BadGatewayJSONResponse(apiErr("provider", err.Error()))}, nil
	}
	return DownloadSubtitle201JSONResponse{StreamId: id, Remaining: nz(left)}, nil
}

func (h *Handlers) RemoveSubtitle(ctx context.Context, req RemoveSubtitleRequestObject) (RemoveSubtitleResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return RemoveSubtitle401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return RemoveSubtitle403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	if err := h.Subtitles.Remove(ctx, req.StreamId); err != nil {
		return RemoveSubtitle400JSONResponse{BadRequestJSONResponse(apiErr("invalid", err.Error()))}, nil
	}
	return RemoveSubtitle204Response{}, nil
}
