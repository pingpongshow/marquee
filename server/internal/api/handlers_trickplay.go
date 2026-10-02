package api

import (
	"context"
	"database/sql"
	"errors"
	"os"

	"marquee/internal/trickplay"
)

// GetTrickplay describes an item's seek-bar preview sheets (PLAY-13).
func (h *Handlers) GetTrickplay(ctx context.Context, req GetTrickplayRequestObject) (GetTrickplayResponseObject, error) {
	if _, ok := session(ctx); !ok {
		return GetTrickplay401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	notFound := GetTrickplay404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "no previews for this item"))}
	if h.Trickplay == nil || h.Items.Visible(ctx, access(ctx), req.ItemId) != nil {
		return notFound, nil
	}
	in, err := h.Trickplay.ForItem(ctx, req.ItemId)
	if errors.Is(err, sql.ErrNoRows) {
		return notFound, nil
	}
	if err != nil {
		return nil, internal(ctx, "trickplay", err)
	}
	return GetTrickplay200JSONResponse{IntervalMs: in.IntervalMS, Width: in.Width, Height: in.Height,
		Columns: trickplay.Columns, Rows: trickplay.Rows, Count: in.Count, Sheets: in.Sheets()}, nil
}

// GetTrickplaySheet serves one sprite sheet.
func (h *Handlers) GetTrickplaySheet(ctx context.Context, req GetTrickplaySheetRequestObject) (GetTrickplaySheetResponseObject, error) {
	if _, ok := session(ctx); !ok {
		return GetTrickplaySheet401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	notFound := GetTrickplaySheet404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "no such sheet"))}
	if h.Trickplay == nil || h.Items.Visible(ctx, access(ctx), req.ItemId) != nil {
		return notFound, nil
	}
	in, err := h.Trickplay.ForItem(ctx, req.ItemId)
	if err != nil || req.Sheet < 0 || req.Sheet >= in.Sheets() {
		return notFound, nil
	}
	f, err := os.Open(h.Trickplay.SheetPath(in.FileID, req.Sheet))
	if err != nil {
		return notFound, nil
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, internal(ctx, "trickplay", err)
	}
	cache := "private, max-age=86400"
	return GetTrickplaySheet200ImagejpegResponse{Body: f, ContentLength: st.Size(), Headers: GetTrickplaySheet200ResponseHeaders{CacheControl: &cache}}, nil
}
