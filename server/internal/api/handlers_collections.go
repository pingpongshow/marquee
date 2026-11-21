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

// ---------- favourites (the heart) ----------

func (h *Handlers) AddFavorite(ctx context.Context, req AddFavoriteRequestObject) (AddFavoriteResponseObject, error) {
	sess, ok := session(ctx)
	if !ok {
		return AddFavorite401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	if err := h.Items.Visible(ctx, itemAccess(ctx), req.ItemId); err != nil {
		return AddFavorite404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "item not found"))}, nil
	}
	// A change made offline and sent later only applies if nothing newer happened (USER-18).
	if fresh, err := h.Items.Fresh(ctx, sess.User.ID, req.ItemId, "favorite", req.Params.At); err != nil {
		return nil, internal(ctx, "favorite", err)
	} else if !fresh {
		return AddFavorite204Response{}, nil
	}
	if err := h.Items.SetFavorite(ctx, sess.User.ID, req.ItemId, true); err != nil {
		return nil, internal(ctx, "favorite", err)
	}
	return AddFavorite204Response{}, nil
}

func (h *Handlers) RemoveFavorite(ctx context.Context, req RemoveFavoriteRequestObject) (RemoveFavoriteResponseObject, error) {
	sess, ok := session(ctx)
	if !ok {
		return RemoveFavorite401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	if err := h.Items.Visible(ctx, itemAccess(ctx), req.ItemId); err != nil {
		return RemoveFavorite404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "item not found"))}, nil
	}
	if fresh, err := h.Items.Fresh(ctx, sess.User.ID, req.ItemId, "favorite", req.Params.At); err != nil {
		return nil, internal(ctx, "favorite", err)
	} else if !fresh {
		return RemoveFavorite204Response{}, nil
	}
	if err := h.Items.SetFavorite(ctx, sess.User.ID, req.ItemId, false); err != nil {
		return nil, internal(ctx, "favorite", err)
	}
	return RemoveFavorite204Response{}, nil
}

// AddDislike gives a song the caller's thumbs down (MUSIC-25).
func (h *Handlers) AddDislike(ctx context.Context, req AddDislikeRequestObject) (AddDislikeResponseObject, error) {
	sess, ok := session(ctx)
	if !ok {
		return AddDislike401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	target, found, err := h.thumbTarget(ctx, req.ItemId)
	if err != nil {
		return nil, internal(ctx, "dislike", err)
	}
	if !found {
		return AddDislike404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "item not found"))}, nil
	}
	if target == 0 {
		return AddDislike400JSONResponse{BadRequestJSONResponse(apiErr("invalid", "only songs, films and shows can have a thumbs down"))}, nil
	}
	if err := h.Items.SetDislikedAt(ctx, sess.User.ID, target, true, req.Params.At); err != nil {
		return nil, internal(ctx, "dislike", err)
	}
	return AddDislike204Response{}, nil
}

// thumbTarget is what a thumbs up or down on an item goes on (USER-21): a song, film or show
// itself, and an episode's show; 0 for anything else. found is false when the caller can't
// see the item.
func (h *Handlers) thumbTarget(ctx context.Context, itemID int64) (target int64, found bool, err error) {
	d, err := h.Items.Get(ctx, itemAccess(ctx), itemID, false)
	if errors.Is(err, items.ErrNotFound) {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	switch d.Type {
	case "track", "movie", "show":
		return itemID, true, nil
	case "episode":
		if d.GrandparentID != 0 {
			return d.GrandparentID, true, nil
		}
	}
	return 0, true, nil
}

// AddLike gives a song, film or show the caller's thumbs up (USER-21).
func (h *Handlers) AddLike(ctx context.Context, req AddLikeRequestObject) (AddLikeResponseObject, error) {
	sess, ok := session(ctx)
	if !ok {
		return AddLike401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	target, found, err := h.thumbTarget(ctx, req.ItemId)
	if err != nil {
		return nil, internal(ctx, "like", err)
	}
	if !found {
		return AddLike404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "item not found"))}, nil
	}
	if target == 0 {
		return AddLike400JSONResponse{BadRequestJSONResponse(apiErr("invalid", "only songs, films and shows can have a thumbs up"))}, nil
	}
	if err := h.Items.SetLikedAt(ctx, sess.User.ID, target, true, req.Params.At); err != nil {
		return nil, internal(ctx, "like", err)
	}
	return AddLike204Response{}, nil
}

// RemoveLike takes back the caller's thumbs up.
func (h *Handlers) RemoveLike(ctx context.Context, req RemoveLikeRequestObject) (RemoveLikeResponseObject, error) {
	sess, ok := session(ctx)
	if !ok {
		return RemoveLike401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	target, found, err := h.thumbTarget(ctx, req.ItemId)
	if err != nil {
		return nil, internal(ctx, "like", err)
	}
	if !found {
		return RemoveLike404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "item not found"))}, nil
	}
	if target == 0 {
		target = req.ItemId
	}
	if err := h.Items.SetLikedAt(ctx, sess.User.ID, target, false, req.Params.At); err != nil {
		return nil, internal(ctx, "like", err)
	}
	return RemoveLike204Response{}, nil
}

// RemoveDislike takes back the caller's thumbs down on a song.
func (h *Handlers) RemoveDislike(ctx context.Context, req RemoveDislikeRequestObject) (RemoveDislikeResponseObject, error) {
	sess, ok := session(ctx)
	if !ok {
		return RemoveDislike401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	target, found, err := h.thumbTarget(ctx, req.ItemId)
	if err != nil {
		return nil, internal(ctx, "dislike", err)
	}
	if !found {
		return RemoveDislike404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "item not found"))}, nil
	}
	if target == 0 {
		target = req.ItemId
	}
	if err := h.Items.SetDislikedAt(ctx, sess.User.ID, target, false, req.Params.At); err != nil {
		return nil, internal(ctx, "dislike", err)
	}
	return RemoveDislike204Response{}, nil
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
