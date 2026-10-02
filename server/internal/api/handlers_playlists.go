package api

import (
	"context"
	"errors"
	"strings"

	"marquee/internal/items"
)

func toAPIPlaylist(p items.Playlist) Playlist {
	return Playlist{Id: p.ID, Title: p.Title, Kind: PlaylistKind(p.Kind), ItemCount: p.ItemCount, DurationMs: p.DurationMS,
		ImageIds: p.ImageIDs, UpdatedAt: p.UpdatedAt}
}

var errPlaylistNotFound = apiErr("not_found", "playlist not found")

func (h *Handlers) ListPlaylists(ctx context.Context, req ListPlaylistsRequestObject) (ListPlaylistsResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return ListPlaylists401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	kind := ""
	if req.Params.Kind != nil {
		kind = string(*req.Params.Kind)
	}
	list, err := h.Items.Playlists(ctx, s.User.ID, kind)
	if err != nil {
		return nil, internal(ctx, "playlists", err)
	}
	out := make(ListPlaylists200JSONResponse, len(list))
	for i, p := range list {
		out[i] = toAPIPlaylist(p)
	}
	return out, nil
}

func (h *Handlers) CreatePlaylist(ctx context.Context, req CreatePlaylistRequestObject) (CreatePlaylistResponseObject, error) {
	if _, ok := session(ctx); !ok {
		return CreatePlaylist401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	b := req.Body
	if strings.TrimSpace(b.Title) == "" || (b.Kind != PlaylistKindVideo && b.Kind != PlaylistKindAudio) {
		return CreatePlaylist400JSONResponse{BadRequestJSONResponse(apiErr("invalid", "a title and a kind (video or audio) are required"))}, nil
	}
	var ids []int64
	set(&ids, b.ItemIds)
	p, err := h.Items.CreatePlaylist(ctx, access(ctx), b.Title, string(b.Kind), ids)
	if err != nil {
		return nil, internal(ctx, "createPlaylist", err)
	}
	return CreatePlaylist201JSONResponse(toAPIPlaylist(p)), nil
}

func (h *Handlers) GetPlaylist(ctx context.Context, req GetPlaylistRequestObject) (GetPlaylistResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return GetPlaylist401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	p, err := h.Items.Playlist(ctx, s.User.ID, req.PlaylistId)
	if errors.Is(err, items.ErrNotFound) {
		return GetPlaylist404JSONResponse{NotFoundJSONResponse(errPlaylistNotFound)}, nil
	}
	if err != nil {
		return nil, internal(ctx, "playlist", err)
	}
	return GetPlaylist200JSONResponse(toAPIPlaylist(p)), nil
}

func (h *Handlers) UpdatePlaylist(ctx context.Context, req UpdatePlaylistRequestObject) (UpdatePlaylistResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return UpdatePlaylist401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	if t := req.Body.Title; t != nil {
		if strings.TrimSpace(*t) == "" {
			return UpdatePlaylist400JSONResponse{BadRequestJSONResponse(apiErr("invalid", "the title can't be empty"))}, nil
		}
		if err := h.Items.RenamePlaylist(ctx, s.User.ID, req.PlaylistId, *t); errors.Is(err, items.ErrNotFound) {
			return UpdatePlaylist404JSONResponse{NotFoundJSONResponse(errPlaylistNotFound)}, nil
		} else if err != nil {
			return nil, internal(ctx, "updatePlaylist", err)
		}
	}
	p, err := h.Items.Playlist(ctx, s.User.ID, req.PlaylistId)
	if errors.Is(err, items.ErrNotFound) {
		return UpdatePlaylist404JSONResponse{NotFoundJSONResponse(errPlaylistNotFound)}, nil
	}
	if err != nil {
		return nil, internal(ctx, "updatePlaylist", err)
	}
	return UpdatePlaylist200JSONResponse(toAPIPlaylist(p)), nil
}

func (h *Handlers) DeletePlaylist(ctx context.Context, req DeletePlaylistRequestObject) (DeletePlaylistResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return DeletePlaylist401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	err := h.Items.DeletePlaylist(ctx, s.User.ID, req.PlaylistId)
	if errors.Is(err, items.ErrNotFound) {
		return DeletePlaylist404JSONResponse{NotFoundJSONResponse(errPlaylistNotFound)}, nil
	}
	if err != nil {
		return nil, internal(ctx, "deletePlaylist", err)
	}
	return DeletePlaylist204Response{}, nil
}

func (h *Handlers) ListPlaylistItems(ctx context.Context, req ListPlaylistItemsRequestObject) (ListPlaylistItemsResponseObject, error) {
	if _, ok := session(ctx); !ok {
		return ListPlaylistItems401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	offset, limit := 0, 500
	set(&offset, req.Params.Offset)
	set(&limit, req.Params.Limit)
	limit = min(max(limit, 1), 2000)
	list, total, err := h.Items.PlaylistItems(ctx, access(ctx), req.PlaylistId, max(offset, 0), limit)
	if errors.Is(err, items.ErrNotFound) {
		return ListPlaylistItems404JSONResponse{NotFoundJSONResponse(errPlaylistNotFound)}, nil
	}
	if err != nil {
		return nil, internal(ctx, "playlistItems", err)
	}
	out := PlaylistItemPage{Items: make([]PlaylistEntry, len(list)), Total: total, Offset: offset}
	for i, e := range list {
		out.Items[i] = PlaylistEntry{EntryId: e.EntryID, Item: toAPISummary(e.Item)}
	}
	return ListPlaylistItems200JSONResponse(out), nil
}

func (h *Handlers) AddPlaylistItems(ctx context.Context, req AddPlaylistItemsRequestObject) (AddPlaylistItemsResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return AddPlaylistItems401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	err := h.Items.AddToPlaylist(ctx, access(ctx), req.PlaylistId, req.Body.ItemIds)
	switch {
	case errors.Is(err, items.ErrNotFound):
		return AddPlaylistItems404JSONResponse{NotFoundJSONResponse(errPlaylistNotFound)}, nil
	case errors.Is(err, items.ErrPlaylistKind):
		return AddPlaylistItems400JSONResponse{BadRequestJSONResponse(apiErr("wrong_kind", err.Error()))}, nil
	case err != nil:
		return nil, internal(ctx, "addPlaylistItems", err)
	}
	p, err := h.Items.Playlist(ctx, s.User.ID, req.PlaylistId)
	if err != nil {
		return nil, internal(ctx, "addPlaylistItems", err)
	}
	return AddPlaylistItems200JSONResponse(toAPIPlaylist(p)), nil
}

func (h *Handlers) RemovePlaylistItem(ctx context.Context, req RemovePlaylistItemRequestObject) (RemovePlaylistItemResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return RemovePlaylistItem401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	err := h.Items.RemovePlaylistItem(ctx, s.User.ID, req.PlaylistId, req.EntryId)
	if errors.Is(err, items.ErrNotFound) {
		return RemovePlaylistItem404JSONResponse{NotFoundJSONResponse(errPlaylistNotFound)}, nil
	}
	if err != nil {
		return nil, internal(ctx, "removePlaylistItem", err)
	}
	return RemovePlaylistItem204Response{}, nil
}

func (h *Handlers) MovePlaylistItem(ctx context.Context, req MovePlaylistItemRequestObject) (MovePlaylistItemResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return MovePlaylistItem401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	var after int64
	set(&after, req.Body.AfterEntryId)
	err := h.Items.MovePlaylistItem(ctx, s.User.ID, req.PlaylistId, req.EntryId, after)
	if errors.Is(err, items.ErrNotFound) {
		return MovePlaylistItem404JSONResponse{NotFoundJSONResponse(errPlaylistNotFound)}, nil
	}
	if err != nil {
		return nil, internal(ctx, "movePlaylistItem", err)
	}
	return MovePlaylistItem204Response{}, nil
}
