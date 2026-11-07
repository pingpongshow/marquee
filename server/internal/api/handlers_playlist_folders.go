package api

import (
	"context"
	"errors"
	"strings"

	"marquee/internal/items"
)

// Playlist folders (MUSIC-28).

var errPlaylistFolderNotFound = apiErr("not_found", "folder or playlist not found")

func toAPIPlaylistFolder(f items.PlaylistFolder) PlaylistFolder {
	return PlaylistFolder{Id: f.ID, Name: f.Name, PlaylistCount: f.PlaylistCount}
}

func (h *Handlers) ListPlaylistFolders(ctx context.Context, req ListPlaylistFoldersRequestObject) (ListPlaylistFoldersResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return ListPlaylistFolders401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	list, err := h.Items.PlaylistFolders(ctx, s.User.ID)
	if err != nil {
		return nil, internal(ctx, "playlistFolders", err)
	}
	out := make(ListPlaylistFolders200JSONResponse, len(list))
	for i, f := range list {
		out[i] = toAPIPlaylistFolder(f)
	}
	return out, nil
}

func (h *Handlers) CreatePlaylistFolder(ctx context.Context, req CreatePlaylistFolderRequestObject) (CreatePlaylistFolderResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return CreatePlaylistFolder401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	if strings.TrimSpace(req.Body.Name) == "" {
		return CreatePlaylistFolder400JSONResponse{BadRequestJSONResponse(apiErr("invalid", "the folder needs a name"))}, nil
	}
	f, err := h.Items.CreatePlaylistFolder(ctx, s.User.ID, req.Body.Name)
	if err != nil {
		return nil, internal(ctx, "createPlaylistFolder", err)
	}
	return CreatePlaylistFolder201JSONResponse(toAPIPlaylistFolder(f)), nil
}

func (h *Handlers) RenamePlaylistFolder(ctx context.Context, req RenamePlaylistFolderRequestObject) (RenamePlaylistFolderResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return RenamePlaylistFolder401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	if strings.TrimSpace(req.Body.Name) == "" {
		return RenamePlaylistFolder400JSONResponse{BadRequestJSONResponse(apiErr("invalid", "the folder needs a name"))}, nil
	}
	f, err := h.Items.RenamePlaylistFolder(ctx, s.User.ID, req.FolderId, req.Body.Name)
	if errors.Is(err, items.ErrNotFound) {
		return RenamePlaylistFolder404JSONResponse{NotFoundJSONResponse(errPlaylistFolderNotFound)}, nil
	}
	if err != nil {
		return nil, internal(ctx, "renamePlaylistFolder", err)
	}
	return RenamePlaylistFolder200JSONResponse(toAPIPlaylistFolder(f)), nil
}

func (h *Handlers) DeletePlaylistFolder(ctx context.Context, req DeletePlaylistFolderRequestObject) (DeletePlaylistFolderResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return DeletePlaylistFolder401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	err := h.Items.DeletePlaylistFolder(ctx, s.User.ID, req.FolderId)
	if errors.Is(err, items.ErrNotFound) {
		return DeletePlaylistFolder404JSONResponse{NotFoundJSONResponse(errPlaylistFolderNotFound)}, nil
	}
	if err != nil {
		return nil, internal(ctx, "deletePlaylistFolder", err)
	}
	return DeletePlaylistFolder204Response{}, nil
}

func (h *Handlers) AddPlaylistToFolder(ctx context.Context, req AddPlaylistToFolderRequestObject) (AddPlaylistToFolderResponseObject, error) {
	if _, ok := session(ctx); !ok {
		return AddPlaylistToFolder401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	err := h.Items.MoveToPlaylistFolder(ctx, access(ctx), req.FolderId, req.PlaylistId)
	if errors.Is(err, items.ErrNotFound) {
		return AddPlaylistToFolder404JSONResponse{NotFoundJSONResponse(errPlaylistFolderNotFound)}, nil
	}
	if err != nil {
		return nil, internal(ctx, "addPlaylistToFolder", err)
	}
	return AddPlaylistToFolder204Response{}, nil
}

func (h *Handlers) RemovePlaylistFromFolder(ctx context.Context, req RemovePlaylistFromFolderRequestObject) (RemovePlaylistFromFolderResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return RemovePlaylistFromFolder401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	err := h.Items.RemoveFromPlaylistFolder(ctx, s.User.ID, req.FolderId, req.PlaylistId)
	if errors.Is(err, items.ErrNotFound) {
		return RemovePlaylistFromFolder404JSONResponse{NotFoundJSONResponse(errPlaylistFolderNotFound)}, nil
	}
	if err != nil {
		return nil, internal(ctx, "removePlaylistFromFolder", err)
	}
	return RemovePlaylistFromFolder204Response{}, nil
}
