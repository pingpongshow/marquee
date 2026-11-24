package api

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"marquee/internal/images"
	"marquee/internal/items"
)

func toAPIPlaylist(p items.Playlist) Playlist {
	return Playlist{Id: p.ID, Title: p.Title, Kind: PlaylistKind(p.Kind), ItemCount: p.ItemCount, DurationMs: p.DurationMS,
		ImageIds: p.ImageIDs, UpdatedAt: p.UpdatedAt, Rules: rulesToAPI(p.Rules), ReadOnly: nz(p.ReadOnly), OwnerName: nz(p.OwnerName),
		SharedWithAll: nz(p.SharedWithAll), ShareCount: nz(p.ShareCount), FolderId: nz(p.FolderID), LibraryId: nz(p.LibraryID),
		ImageVersion: nz(coverVersion(p.Cover))}
}

// coverVersion names a playlist's own picture: its file's name, which carries the image's hash,
// so it changes when the picture does.
func coverVersion(path string) string {
	if path == "" {
		return ""
	}
	return strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
}

// The API and store rule types share their JSON shape.
func rulesFromAPI(r *SmartRules) *items.SmartRules {
	if r == nil {
		return nil
	}
	var out items.SmartRules
	b, _ := json.Marshal(r)
	json.Unmarshal(b, &out)
	return &out
}

func rulesToAPI(r *items.SmartRules) *SmartRules {
	if r == nil {
		return nil
	}
	var out SmartRules
	b, _ := json.Marshal(r)
	json.Unmarshal(b, &out)
	return &out
}

var errPlaylistNotFound = apiErr("not_found", "playlist not found")

func (h *Handlers) ListPlaylists(ctx context.Context, req ListPlaylistsRequestObject) (ListPlaylistsResponseObject, error) {
	_, ok := session(ctx)
	if !ok {
		return ListPlaylists401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	kind := ""
	if req.Params.Kind != nil {
		kind = string(*req.Params.Kind)
	}
	var list []items.Playlist
	var err error
	if lib := req.Params.LibraryId; lib != nil {
		if kind == "audio" || !h.canSeeLibrary(ctx, *lib) {
			return ListPlaylists200JSONResponse{}, nil
		}
		list, err = h.Items.OwnCollections(ctx, access(ctx), *lib)
	} else {
		list, err = h.Items.Playlists(ctx, access(ctx), kind)
	}
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
	if b.Kind == PlaylistKindVideo {
		// A collection (USER-23): it belongs to a video library, and has no rules.
		if b.Rules != nil {
			return CreatePlaylist400JSONResponse{BadRequestJSONResponse(apiErr("invalid_rules", "a collection can't have rules"))}, nil
		}
		var lib int64
		set(&lib, b.LibraryId)
		if lib == 0 || !h.canSeeLibrary(ctx, lib) {
			return CreatePlaylist400JSONResponse{BadRequestJSONResponse(apiErr("invalid_library", items.ErrCollectionLibrary.Error()))}, nil
		}
		p, err := h.Items.CreateOwnCollection(ctx, access(ctx), b.Title, lib, ids)
		if errors.Is(err, items.ErrCollectionLibrary) {
			return CreatePlaylist400JSONResponse{BadRequestJSONResponse(apiErr("invalid_library", err.Error()))}, nil
		}
		if err != nil {
			return nil, internal(ctx, "createCollection", err)
		}
		return CreatePlaylist201JSONResponse(toAPIPlaylist(p)), nil
	}
	p, err := h.Items.CreatePlaylist(ctx, access(ctx), b.Title, string(b.Kind), ids, rulesFromAPI(b.Rules))
	if err != nil {
		if b.Rules != nil {
			return CreatePlaylist400JSONResponse{BadRequestJSONResponse(apiErr("invalid_rules", err.Error()))}, nil
		}
		return nil, internal(ctx, "createPlaylist", err)
	}
	return CreatePlaylist201JSONResponse(toAPIPlaylist(p)), nil
}

func (h *Handlers) GetPlaylist(ctx context.Context, req GetPlaylistRequestObject) (GetPlaylistResponseObject, error) {
	if _, ok := session(ctx); !ok {
		return GetPlaylist401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	p, err := h.Items.ReadablePlaylist(ctx, access(ctx), req.PlaylistId)
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
	if r := rulesFromAPI(req.Body.Rules); r != nil {
		if err := h.Items.SetRules(ctx, s.User.ID, req.PlaylistId, *r); errors.Is(err, items.ErrNotFound) {
			return UpdatePlaylist404JSONResponse{NotFoundJSONResponse(errPlaylistNotFound)}, nil
		} else if err != nil {
			return UpdatePlaylist400JSONResponse{BadRequestJSONResponse(apiErr("invalid_rules", err.Error()))}, nil
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
	case errors.Is(err, items.ErrPlaylistKind), errors.Is(err, items.ErrSmartPlaylist):
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

func toAPISharing(all bool, shares []items.PlaylistShare) PlaylistSharing {
	out := PlaylistSharing{All: all, Shares: make([]PlaylistShare, len(shares))}
	for i, sh := range shares {
		out.Shares[i] = PlaylistShare{UserId: sh.UserID, Name: sh.Name, Status: PlaylistShareStatus(sh.Status)}
	}
	return out
}

func (h *Handlers) GetPlaylistSharing(ctx context.Context, req GetPlaylistSharingRequestObject) (GetPlaylistSharingResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return GetPlaylistSharing401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	all, shares, err := h.Items.PlaylistShares(ctx, s.User.ID, req.PlaylistId)
	if errors.Is(err, items.ErrNotFound) {
		return GetPlaylistSharing404JSONResponse{NotFoundJSONResponse(errPlaylistNotFound)}, nil
	}
	if err != nil {
		return nil, internal(ctx, "playlistSharing", err)
	}
	return GetPlaylistSharing200JSONResponse(toAPISharing(all, shares)), nil
}

func (h *Handlers) SetPlaylistSharing(ctx context.Context, req SetPlaylistSharingRequestObject) (SetPlaylistSharingResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return SetPlaylistSharing401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	if req.Body.All != nil && *req.Body.All && !s.User.IsAdmin {
		return SetPlaylistSharing403JSONResponse{ForbiddenJSONResponse(apiErr("admin_only", "only admins can share a playlist with everyone"))}, nil
	}
	// Left out, it stays as it is: someone no longer an admin can still choose names.
	everyone, _, err := h.Items.PlaylistShares(ctx, s.User.ID, req.PlaylistId)
	if err == nil {
		set(&everyone, req.Body.All)
		err = h.Items.SetPlaylistShares(ctx, s.User.ID, req.PlaylistId, req.Body.UserIds, everyone)
	}
	if errors.Is(err, items.ErrNotFound) {
		return SetPlaylistSharing404JSONResponse{NotFoundJSONResponse(errPlaylistNotFound)}, nil
	}
	if err != nil {
		return nil, internal(ctx, "setPlaylistSharing", err)
	}
	all, shares, err := h.Items.PlaylistShares(ctx, s.User.ID, req.PlaylistId)
	if err != nil {
		return nil, internal(ctx, "setPlaylistSharing", err)
	}
	return SetPlaylistSharing200JSONResponse(toAPISharing(all, shares)), nil
}

func (h *Handlers) ListPlaylistShareTargets(ctx context.Context, _ ListPlaylistShareTargetsRequestObject) (ListPlaylistShareTargetsResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return ListPlaylistShareTargets401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	list, err := h.Items.PlaylistShareTargets(ctx, s.User.ID)
	if err != nil {
		return nil, internal(ctx, "playlistShareTargets", err)
	}
	out := make(ListPlaylistShareTargets200JSONResponse, len(list))
	for i, t := range list {
		out[i] = PlaylistShareTarget{Id: t.UserID, Name: t.Name}
	}
	return out, nil
}

func (h *Handlers) ListPlaylistInvites(ctx context.Context, _ ListPlaylistInvitesRequestObject) (ListPlaylistInvitesResponseObject, error) {
	if _, ok := session(ctx); !ok {
		return ListPlaylistInvites401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	list, err := h.Items.PlaylistInvites(ctx, access(ctx))
	if err != nil {
		return nil, internal(ctx, "playlistInvites", err)
	}
	out := make(ListPlaylistInvites200JSONResponse, len(list))
	for i, p := range list {
		out[i] = toAPIPlaylist(p)
	}
	return out, nil
}

func (h *Handlers) AcceptPlaylistShare(ctx context.Context, req AcceptPlaylistShareRequestObject) (AcceptPlaylistShareResponseObject, error) {
	if _, ok := session(ctx); !ok {
		return AcceptPlaylistShare401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	p, err := h.Items.AcceptPlaylistShare(ctx, access(ctx), req.PlaylistId)
	if errors.Is(err, items.ErrNotFound) {
		return AcceptPlaylistShare404JSONResponse{NotFoundJSONResponse(errPlaylistNotFound)}, nil
	}
	if err != nil {
		return nil, internal(ctx, "acceptPlaylistShare", err)
	}
	return AcceptPlaylistShare200JSONResponse(toAPIPlaylist(p)), nil
}

func (h *Handlers) RemovePlaylistShare(ctx context.Context, req RemovePlaylistShareRequestObject) (RemovePlaylistShareResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return RemovePlaylistShare401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	err := h.Items.RemovePlaylistShare(ctx, s.User.ID, req.PlaylistId)
	if errors.Is(err, items.ErrNotFound) {
		return RemovePlaylistShare404JSONResponse{NotFoundJSONResponse(errPlaylistNotFound)}, nil
	}
	if err != nil {
		return nil, internal(ctx, "removePlaylistShare", err)
	}
	return RemovePlaylistShare204Response{}, nil
}

// SetPlaylistItems reorders and removes a playlist's entries in one go (MUSIC-29).
func (h *Handlers) SetPlaylistItems(ctx context.Context, req SetPlaylistItemsRequestObject) (SetPlaylistItemsResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return SetPlaylistItems401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	err := h.Items.SetPlaylistOrder(ctx, access(ctx), req.PlaylistId, req.Body.EntryIds)
	switch {
	case errors.Is(err, items.ErrNotFound):
		return SetPlaylistItems404JSONResponse{NotFoundJSONResponse(errPlaylistNotFound)}, nil
	case errors.Is(err, items.ErrSmartPlaylist):
		return SetPlaylistItems400JSONResponse{BadRequestJSONResponse(apiErr("smart_playlist", err.Error()))}, nil
	case err != nil:
		return nil, internal(ctx, "setPlaylistItems", err)
	}
	p, err := h.Items.Playlist(ctx, s.User.ID, req.PlaylistId)
	if err != nil {
		return nil, internal(ctx, "setPlaylistItems", err)
	}
	return SetPlaylistItems200JSONResponse(toAPIPlaylist(p)), nil
}

// MergePlaylists combines playlists into a new one or the end of an existing one (MUSIC-29).
func (h *Handlers) MergePlaylists(ctx context.Context, req MergePlaylistsRequestObject) (MergePlaylistsResponseObject, error) {
	if _, ok := session(ctx); !ok {
		return MergePlaylists401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	b := req.Body
	if len(b.SourceIds) == 0 {
		return MergePlaylists400JSONResponse{BadRequestJSONResponse(apiErr("invalid", "choose the playlists to combine"))}, nil
	}
	var opt items.MergeOptions
	set(&opt.IntoID, b.IntoId)
	set(&opt.Title, b.Title)
	set(&opt.SkipDuplicates, b.SkipDuplicates)
	set(&opt.DeleteSources, b.DeleteSources)
	p, err := h.Items.MergePlaylists(ctx, access(ctx), b.SourceIds, opt)
	switch {
	case errors.Is(err, items.ErrNotFound):
		return MergePlaylists404JSONResponse{NotFoundJSONResponse(errPlaylistNotFound)}, nil
	case errors.Is(err, items.ErrPlaylistMix):
		return MergePlaylists400JSONResponse{BadRequestJSONResponse(apiErr("wrong_kind", err.Error()))}, nil
	case errors.Is(err, items.ErrSmartPlaylist):
		return MergePlaylists400JSONResponse{BadRequestJSONResponse(apiErr("smart_playlist", "a smart playlist fills itself from its rules: combine into a new playlist or one of your own"))}, nil
	case err != nil:
		return nil, internal(ctx, "mergePlaylists", err)
	}
	return MergePlaylists200JSONResponse(toAPIPlaylist(p)), nil
}

// ---------- A playlist's or collection's own picture (MUSIC-30) ----------

func (h *Handlers) GetPlaylistImage(ctx context.Context, req GetPlaylistImageRequestObject) (GetPlaylistImageResponseObject, error) {
	if _, ok := session(ctx); !ok {
		return GetPlaylistImage401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	missing := GetPlaylistImage404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "image not found"))}
	p, err := h.Items.ReadablePlaylist(ctx, access(ctx), req.PlaylistId)
	if err != nil || p.Cover == "" || h.Images == nil {
		return missing, nil
	}
	w := 0
	if req.Params.W != nil {
		w = min(max(*req.Params.W, 0), 3840)
	}
	path, err := h.Images.Uploaded(ctx, p.Cover, w)
	if err != nil {
		if !errors.Is(err, images.ErrNotFound) && !errors.Is(err, os.ErrNotExist) {
			slog.WarnContext(ctx, "playlist image", "playlist", req.PlaylistId, "err", err)
		}
		return missing, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, internal(ctx, "playlistImage", err)
	}
	st, _ := f.Stat()
	return GetPlaylistImage200ImagejpegResponse{Body: f, ContentLength: st.Size(), Headers: GetPlaylistImage200ResponseHeaders{CacheControl: ptr(immutable)}}, nil
}

// keepPlaylistImage checks data is an image, keeps it in the image cache and makes it the
// playlist's picture. msg is set when the image is refused.
func (h *Handlers) keepPlaylistImage(ctx context.Context, uid, id int64, data []byte) (p items.Playlist, msg string, err error) {
	ext := map[string]string{"image/jpeg": "jpg", "image/png": "png", "image/webp": "webp"}[http.DetectContentType(data)]
	if ext == "" {
		return p, "use a JPEG, PNG or WebP image", nil
	}
	if _, _, err := images.CheckImage(data); err != nil {
		return p, "that image couldn't be read: use a JPEG, PNG or WebP image", nil
	}
	if _, err := h.Items.Playlist(ctx, uid, id); err != nil {
		return p, "", err
	}
	sum := sha256.Sum256(data)
	rel := filepath.Join("uploads", fmt.Sprintf("playlist-%d-%x.%s", id, sum[:8], ext))
	abs := filepath.Join(h.Images.CacheDir, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return p, "", err
	}
	if err := os.WriteFile(abs, data, 0o644); err != nil {
		return p, "", err
	}
	if err := h.setPlaylistCover(ctx, uid, id, rel); err != nil {
		return p, "", err
	}
	p, err = h.Items.Playlist(ctx, uid, id)
	return p, "", err
}

// setPlaylistCover changes the picture and removes the file of the one it replaced.
func (h *Handlers) setPlaylistCover(ctx context.Context, uid, id int64, rel string) error {
	old, err := h.Items.SetPlaylistCover(ctx, uid, id, rel)
	if err != nil {
		return err
	}
	if old != "" && h.Images != nil && strings.HasPrefix(filepath.Clean(old), "uploads"+string(filepath.Separator)) {
		os.Remove(filepath.Join(h.Images.CacheDir, filepath.Clean(old)))
	}
	return nil
}

func (h *Handlers) UploadPlaylistImage(ctx context.Context, req UploadPlaylistImageRequestObject) (UploadPlaylistImageResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return UploadPlaylistImage401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	bad := func(msg string) (UploadPlaylistImageResponseObject, error) {
		return UploadPlaylistImage400JSONResponse{BadRequestJSONResponse(apiErr("invalid", msg))}, nil
	}
	if h.Images == nil {
		return bad("images aren't available")
	}
	data, err := io.ReadAll(io.LimitReader(req.Body, maxArtworkUpload+1))
	if err != nil {
		return bad("couldn't read the image")
	}
	if len(data) > maxArtworkUpload {
		return bad("the image is larger than 20 MB")
	}
	p, msg, err := h.keepPlaylistImage(ctx, s.User.ID, req.PlaylistId, data)
	switch {
	case msg != "":
		return bad(msg)
	case errors.Is(err, items.ErrNotFound):
		return UploadPlaylistImage404JSONResponse{NotFoundJSONResponse(errPlaylistNotFound)}, nil
	case err != nil:
		return nil, internal(ctx, "uploadPlaylistImage", err)
	}
	return UploadPlaylistImage200JSONResponse(toAPIPlaylist(p)), nil
}

func (h *Handlers) SetPlaylistImageLink(ctx context.Context, req SetPlaylistImageLinkRequestObject) (SetPlaylistImageLinkResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return SetPlaylistImageLink401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	bad := func(msg string) (SetPlaylistImageLinkResponseObject, error) {
		return SetPlaylistImageLink400JSONResponse{BadRequestJSONResponse(apiErr("invalid", msg))}, nil
	}
	if h.Images == nil {
		return bad("images aren't available")
	}
	u, perr := url.Parse(strings.TrimSpace(req.Body.Url))
	if perr != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return bad("an image link starts with https://")
	}
	// Whose playlist it is comes first: nothing is fetched for one that isn't theirs.
	if _, err := h.Items.Playlist(ctx, s.User.ID, req.PlaylistId); errors.Is(err, items.ErrNotFound) {
		return SetPlaylistImageLink404JSONResponse{NotFoundJSONResponse(errPlaylistNotFound)}, nil
	} else if err != nil {
		return nil, internal(ctx, "playlistImageLink", err)
	}
	data, err := h.Images.FetchLink(ctx, u.String())
	switch {
	case errors.Is(err, images.ErrPrivateAddress):
		return bad("that link leads to this server or your home network: use an image on the web, or upload it")
	case errors.Is(err, images.ErrNotImage):
		return bad("that link isn't a JPEG, PNG or WebP image: use the image's own address")
	case err != nil:
		slog.WarnContext(ctx, "playlist image link", "playlist", req.PlaylistId, "err", err)
		return bad("the image couldn't be downloaded from that link")
	}
	p, msg, err := h.keepPlaylistImage(ctx, s.User.ID, req.PlaylistId, data)
	switch {
	case msg != "":
		return bad(msg)
	case errors.Is(err, items.ErrNotFound):
		return SetPlaylistImageLink404JSONResponse{NotFoundJSONResponse(errPlaylistNotFound)}, nil
	case err != nil:
		return nil, internal(ctx, "playlistImageLink", err)
	}
	return SetPlaylistImageLink200JSONResponse(toAPIPlaylist(p)), nil
}

func (h *Handlers) RemovePlaylistImage(ctx context.Context, req RemovePlaylistImageRequestObject) (RemovePlaylistImageResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return RemovePlaylistImage401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	if err := h.setPlaylistCover(ctx, s.User.ID, req.PlaylistId, ""); errors.Is(err, items.ErrNotFound) {
		return RemovePlaylistImage404JSONResponse{NotFoundJSONResponse(errPlaylistNotFound)}, nil
	} else if err != nil {
		return nil, internal(ctx, "removePlaylistImage", err)
	}
	p, err := h.Items.Playlist(ctx, s.User.ID, req.PlaylistId)
	if err != nil {
		return nil, internal(ctx, "removePlaylistImage", err)
	}
	return RemovePlaylistImage200JSONResponse(toAPIPlaylist(p)), nil
}
