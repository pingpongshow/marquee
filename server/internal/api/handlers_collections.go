package api

import (
	"context"
	"errors"

	"marquee/internal/items"
)

// ---------- watchlist (USER-8) ----------

func (h *Handlers) AddToWatchlist(ctx context.Context, req AddToWatchlistRequestObject) (AddToWatchlistResponseObject, error) {
	sess, ok := session(ctx)
	if !ok {
		return AddToWatchlist401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	if err := h.Items.Visible(ctx, access(ctx), req.ItemId); err != nil {
		return AddToWatchlist404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "item not found"))}, nil
	}
	// A change made offline and sent later only applies if nothing newer happened (USER-18).
	if fresh, err := h.Items.Fresh(ctx, sess.User.ID, req.ItemId, "watchlist", req.Params.At); err != nil {
		return nil, internal(ctx, "watchlist", err)
	} else if !fresh {
		return AddToWatchlist204Response{}, nil
	}
	if err := h.Items.SetWatchlist(ctx, sess.User.ID, req.ItemId, true); err != nil {
		return nil, internal(ctx, "watchlist", err)
	}
	return AddToWatchlist204Response{}, nil
}

func (h *Handlers) RemoveFromWatchlist(ctx context.Context, req RemoveFromWatchlistRequestObject) (RemoveFromWatchlistResponseObject, error) {
	sess, ok := session(ctx)
	if !ok {
		return RemoveFromWatchlist401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	// A change made offline and sent later only applies if nothing newer happened (USER-18).
	if fresh, err := h.Items.Fresh(ctx, sess.User.ID, req.ItemId, "watchlist", req.Params.At); err != nil {
		return nil, internal(ctx, "watchlist", err)
	} else if !fresh {
		return RemoveFromWatchlist204Response{}, nil
	}
	if err := h.Items.SetWatchlist(ctx, sess.User.ID, req.ItemId, false); err != nil {
		return nil, internal(ctx, "watchlist", err)
	}
	return RemoveFromWatchlist204Response{}, nil
}

func (h *Handlers) GetWatchlist(ctx context.Context, _ GetWatchlistRequestObject) (GetWatchlistResponseObject, error) {
	if _, ok := session(ctx); !ok {
		return GetWatchlist401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	list, err := h.Items.Watchlist(ctx, access(ctx), 500)
	if err != nil {
		return nil, internal(ctx, "watchlist", err)
	}
	out := make(GetWatchlist200JSONResponse, len(list))
	for i, it := range list {
		out[i] = toAPISummary(it)
	}
	return out, nil
}

// ---------- collections (META-7) ----------

func (h *Handlers) CreateCollection(ctx context.Context, req CreateCollectionRequestObject) (CreateCollectionResponseObject, error) {
	authed, admin := isAdmin(ctx)
	if !authed {
		return CreateCollection401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	if !admin {
		return CreateCollection403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	if _, err := h.Libraries.Get(ctx, req.LibraryId); err != nil {
		return CreateCollection404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "library not found"))}, nil
	}
	id, err := h.Items.CreateCollection(ctx, req.LibraryId, req.Body.Title)
	if err != nil {
		return CreateCollection400JSONResponse{BadRequestJSONResponse(apiErr("invalid", err.Error()))}, nil
	}
	if req.Body.ItemIds != nil {
		if err := h.Items.AddToCollection(ctx, id, *req.Body.ItemIds); err != nil {
			return nil, internal(ctx, "createCollection", err)
		}
	}
	d, err := h.Items.Get(ctx, access(ctx), id, false)
	if err != nil {
		return nil, internal(ctx, "createCollection", err)
	}
	return CreateCollection201JSONResponse(toAPISummary(d.Summary)), nil
}

func (h *Handlers) DeleteCollection(ctx context.Context, req DeleteCollectionRequestObject) (DeleteCollectionResponseObject, error) {
	authed, admin := isAdmin(ctx)
	if !authed {
		return DeleteCollection401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	if !admin {
		return DeleteCollection403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	if err := h.Items.DeleteCollection(ctx, req.CollectionId); isMissing(err) {
		return DeleteCollection404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "collection not found"))}, nil
	} else if err != nil {
		return nil, internal(ctx, "deleteCollection", err)
	}
	return DeleteCollection204Response{}, nil
}

func (h *Handlers) AddToCollection(ctx context.Context, req AddToCollectionRequestObject) (AddToCollectionResponseObject, error) {
	authed, admin := isAdmin(ctx)
	if !authed {
		return AddToCollection401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	if !admin {
		return AddToCollection403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	if err := h.Items.AddToCollection(ctx, req.CollectionId, req.Body.ItemIds); isMissing(err) {
		return AddToCollection404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "collection not found"))}, nil
	} else if err != nil {
		return nil, internal(ctx, "addToCollection", err)
	}
	return AddToCollection204Response{}, nil
}

func (h *Handlers) RemoveFromCollection(ctx context.Context, req RemoveFromCollectionRequestObject) (RemoveFromCollectionResponseObject, error) {
	authed, admin := isAdmin(ctx)
	if !authed {
		return RemoveFromCollection401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	if !admin {
		return RemoveFromCollection403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	if err := h.Items.RemoveFromCollection(ctx, req.CollectionId, req.ItemId); isMissing(err) {
		return RemoveFromCollection404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "collection not found"))}, nil
	} else if err != nil {
		return nil, internal(ctx, "removeFromCollection", err)
	}
	return RemoveFromCollection204Response{}, nil
}

func isMissing(err error) bool {
	return errors.Is(err, items.ErrNotFound) || errors.Is(err, items.ErrNotCollection)
}
