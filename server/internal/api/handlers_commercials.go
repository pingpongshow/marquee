package api

import (
	"context"
	"errors"

	"marquee/internal/commercials"
)

// Commercials in Live TV recordings (LIVE-5): found with Comskip and, when wanted, cut out.

func (h *Handlers) toAPICommercialScan(st commercials.Status) CommercialScan {
	breaks := make([]CommercialBreak, len(st.Breaks))
	for i, b := range st.Breaks {
		breaks[i] = CommercialBreak{StartMs: b.StartMS, EndMs: b.EndMS}
	}
	return CommercialScan{Status: CommercialScanStatus(st.Status), Breaks: breaks, RemovedMs: nz(st.RemovedMS), Error: nz(st.Error),
		ScannedAt: st.ScannedAt, CanRestore: ptr(st.CanRestore), Available: ptr(h.Commercials.Available())}
}

func (h *Handlers) fillComskipAvailable(out *ServerSettings) {
	if out.Integrations != nil {
		out.Integrations.ComskipAvailable = ptr(h.Commercials.Available())
	}
}

// recordingCommercials is a finished recording's status, for admins.
func (h *Handlers) recordingCommercials(ctx context.Context, path string) *CommercialScan {
	if h.Commercials == nil || path == "" {
		return nil
	}
	st, err := h.Commercials.StatusByPath(ctx, path)
	if err != nil {
		return nil
	}
	out := h.toAPICommercialScan(st)
	return &out
}

// commercialsFile is an admin's recording file for an item; ok false with the status code
// to send otherwise (401, 403, 404).
func (h *Handlers) commercialsFile(ctx context.Context, itemID int64) (fileID int64, code int, err error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return 0, 401, nil
	case !admin:
		return 0, 403, nil
	}
	if h.Commercials == nil {
		return 0, 404, nil
	}
	fileID, err = h.Commercials.FileForItem(ctx, itemID)
	if errors.Is(err, commercials.ErrNotRecording) {
		return 0, 404, nil
	}
	return fileID, 0, err
}

var errNotRecording = apiErr("not_recording", commercials.ErrNotRecording.Error())

func (h *Handlers) GetItemCommercials(ctx context.Context, req GetItemCommercialsRequestObject) (GetItemCommercialsResponseObject, error) {
	fileID, code, err := h.commercialsFile(ctx, req.ItemId)
	switch {
	case err != nil:
		return nil, internal(ctx, "getItemCommercials", err)
	case code == 401:
		return GetItemCommercials401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case code == 403:
		return GetItemCommercials403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	case code == 404:
		return GetItemCommercials404JSONResponse{NotFoundJSONResponse(errNotRecording)}, nil
	}
	st, err := h.Commercials.Status(ctx, fileID)
	if errors.Is(err, commercials.ErrNotRecording) {
		return GetItemCommercials404JSONResponse{NotFoundJSONResponse(errNotRecording)}, nil
	}
	if err != nil {
		return nil, internal(ctx, "getItemCommercials", err)
	}
	return GetItemCommercials200JSONResponse(h.toAPICommercialScan(st)), nil
}

func (h *Handlers) RemoveItemCommercials(ctx context.Context, req RemoveItemCommercialsRequestObject) (RemoveItemCommercialsResponseObject, error) {
	fileID, code, err := h.commercialsFile(ctx, req.ItemId)
	switch {
	case err != nil:
		return nil, internal(ctx, "removeItemCommercials", err)
	case code == 401:
		return RemoveItemCommercials401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case code == 403:
		return RemoveItemCommercials403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	case code == 404:
		return RemoveItemCommercials404JSONResponse{NotFoundJSONResponse(errNotRecording)}, nil
	}
	st, err := h.Commercials.Queue(ctx, fileID, true)
	switch {
	case errors.Is(err, commercials.ErrUnavailable):
		return RemoveItemCommercials503JSONResponse{ServiceUnavailableJSONResponse(apiErr("comskip_unavailable", err.Error()))}, nil
	case errors.Is(err, commercials.ErrNotRecording):
		return RemoveItemCommercials404JSONResponse{NotFoundJSONResponse(errNotRecording)}, nil
	case err != nil:
		return nil, internal(ctx, "removeItemCommercials", err)
	}
	return RemoveItemCommercials202JSONResponse(h.toAPICommercialScan(st)), nil
}

func (h *Handlers) RestoreItemCommercials(ctx context.Context, req RestoreItemCommercialsRequestObject) (RestoreItemCommercialsResponseObject, error) {
	fileID, code, err := h.commercialsFile(ctx, req.ItemId)
	switch {
	case err != nil:
		return nil, internal(ctx, "restoreItemCommercials", err)
	case code == 401:
		return RestoreItemCommercials401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case code == 403:
		return RestoreItemCommercials403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	case code == 404:
		return RestoreItemCommercials404JSONResponse{NotFoundJSONResponse(errNotRecording)}, nil
	}
	st, err := h.Commercials.Restore(ctx, fileID)
	switch {
	case errors.Is(err, commercials.ErrCantRestore):
		return RestoreItemCommercials409JSONResponse{ConflictJSONResponse(apiErr("no_original", err.Error()))}, nil
	case errors.Is(err, commercials.ErrBusy):
		return RestoreItemCommercials409JSONResponse{ConflictJSONResponse(apiErr("busy", err.Error()))}, nil
	case errors.Is(err, commercials.ErrNotRecording):
		return RestoreItemCommercials404JSONResponse{NotFoundJSONResponse(errNotRecording)}, nil
	case err != nil:
		return nil, internal(ctx, "restoreItemCommercials", err)
	}
	return RestoreItemCommercials200JSONResponse(h.toAPICommercialScan(st)), nil
}
