package api

import (
	"context"
	"errors"
	"time"

	"marquee/internal/syncplay"
)

// Watch together (SyncPlay).

func toAPIGroup(g syncplay.State) WatchGroup {
	out := WatchGroup{Id: g.ID, ItemId: g.ItemID, Title: g.Title, HostId: g.HostID, Version: g.Version, Playing: g.Playing,
		PositionMs: g.PositionMS, At: g.At, ServerTime: time.Now(), LastAction: nz(g.LastAction), LastBy: nz(g.LastBy)}
	out.Members = make([]struct {
		AvatarUrl *string `json:"avatarUrl,omitempty"`
		Buffering bool    `json:"buffering"`
		Name      string  `json:"name"`
		UserId    int64   `json:"userId"`
	}, len(g.Members))
	for i, m := range g.Members {
		out.Members[i].UserId, out.Members[i].Name, out.Members[i].AvatarUrl, out.Members[i].Buffering = m.UserID, m.Name, m.Avatar, m.Buffering
	}
	return out
}

func (h *Handlers) ListWatchGroups(ctx context.Context, _ ListWatchGroupsRequestObject) (ListWatchGroupsResponseObject, error) {
	if _, ok := session(ctx); !ok {
		return ListWatchGroups401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	acc := access(ctx)
	out := ListWatchGroups200JSONResponse{}
	for _, g := range h.SyncPlay.List() {
		// Only groups watching something this person may see.
		if _, err := h.Items.Get(ctx, acc, g.ItemID, false); err == nil {
			out = append(out, toAPIGroup(g))
		}
	}
	return out, nil
}

func (h *Handlers) CreateWatchGroup(ctx context.Context, req CreateWatchGroupRequestObject) (CreateWatchGroupResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return CreateWatchGroup401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	d, err := h.Items.Get(ctx, access(ctx), req.Body.ItemId, false)
	if err != nil {
		return CreateWatchGroup404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "item not found"))}, nil
	}
	title := d.Title
	if d.GrandparentTitle != "" {
		title = d.GrandparentTitle + " · " + d.Title
	}
	var pos int64
	if req.Body.PositionMs != nil {
		pos = *req.Body.PositionMs
	}
	g := h.SyncPlay.Create(s.User.ID, s.User.DisplayName, avatarURL(s.User), req.Body.ItemId, title, pos)
	return CreateWatchGroup201JSONResponse(toAPIGroup(g)), nil
}

func (h *Handlers) GetWatchGroup(ctx context.Context, req GetWatchGroupRequestObject) (GetWatchGroupResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return GetWatchGroup401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	since, wait := int64(-1), time.Duration(0)
	if req.Params.Since != nil {
		since, wait = *req.Params.Since, 25*time.Second
	}
	g, err := h.SyncPlay.Wait(ctx, req.GroupId, s.User.ID, since, wait)
	if errors.Is(err, syncplay.ErrNotFound) {
		return GetWatchGroup404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "this group has ended"))}, nil
	} else if err != nil {
		return nil, internal(ctx, "watchGroup", err)
	}
	return GetWatchGroup200JSONResponse(toAPIGroup(g)), nil
}

func (h *Handlers) JoinWatchGroup(ctx context.Context, req JoinWatchGroupRequestObject) (JoinWatchGroupResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return JoinWatchGroup401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	item, err := h.SyncPlay.ItemOf(req.GroupId)
	if err != nil {
		return JoinWatchGroup404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "this group has ended"))}, nil
	}
	if _, err := h.Items.Get(ctx, access(ctx), item, false); err != nil {
		return JoinWatchGroup403JSONResponse{ForbiddenJSONResponse(apiErr("forbidden", "You can't watch this title."))}, nil
	}
	g, err := h.SyncPlay.Join(req.GroupId, s.User.ID, s.User.DisplayName, avatarURL(s.User))
	if err != nil {
		return JoinWatchGroup404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "this group has ended"))}, nil
	}
	return JoinWatchGroup200JSONResponse(toAPIGroup(g)), nil
}

func (h *Handlers) LeaveWatchGroup(ctx context.Context, req LeaveWatchGroupRequestObject) (LeaveWatchGroupResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return LeaveWatchGroup401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	h.SyncPlay.Leave(req.GroupId, s.User.ID)
	return LeaveWatchGroup204Response{}, nil
}

func (h *Handlers) WatchGroupCommand(ctx context.Context, req WatchGroupCommandRequestObject) (WatchGroupCommandResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return WatchGroupCommand401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	g, err := h.SyncPlay.Command(req.GroupId, s.User.ID, string(req.Body.Action), req.Body.PositionMs)
	switch {
	case errors.Is(err, syncplay.ErrNotFound):
		return WatchGroupCommand404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "this group has ended"))}, nil
	case errors.Is(err, syncplay.ErrNotMember):
		return WatchGroupCommand403JSONResponse{ForbiddenJSONResponse(apiErr("forbidden", err.Error()))}, nil
	case err != nil:
		return WatchGroupCommand400JSONResponse{BadRequestJSONResponse(apiErr("invalid", err.Error()))}, nil
	}
	return WatchGroupCommand200JSONResponse(toAPIGroup(g)), nil
}
