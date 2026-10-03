package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"marquee/internal/auth"
	"marquee/internal/remote"
)

// Remote control (USER-14): apps long-poll their inbox (which lists them as players) and
// report their state; a person's other apps list those players and send them commands.

// remoteWait is how long the inbox and ?since= polls wait.
const remoteWait = 25 * time.Second

// maxRemoteItems bounds a play command's queue.
const maxRemoteItems = 1000

// canControl reports whether the caller may see and command a player: their own other
// devices, and for admins (but not invited friends) everyone's.
func canControl(s auth.Session, p remote.Player) bool {
	if p.DeviceID == s.DeviceID {
		return false
	}
	return p.UserID == s.User.ID || (s.User.IsAdmin && !s.User.Restrictions.Friend)
}

func toAPIPlayer(p remote.Player) RemotePlayer {
	out := RemotePlayer{DeviceId: p.DeviceID, Name: p.Name, Platform: p.Platform, UserId: p.UserID, UserName: p.UserName,
		Capabilities: make([]RemoteCapability, len(p.Capabilities)), Version: p.Version}
	for i, c := range p.Capabilities {
		out.Capabilities[i] = RemoteCapability(c)
	}
	if st, ok := p.State.(RemotePlayerState); ok {
		out.State = &st
	}
	return out
}

// deviceInfo describes the caller's device, from the hub when it's a player already.
func (h *Handlers) deviceInfo(ctx context.Context, s auth.Session) (remote.Info, error) {
	// Always from the database: device ids are reused after a user is deleted, so a cached
	// owner could be someone else.
	info := remote.Info{DeviceID: s.DeviceID, UserID: s.User.ID, UserName: s.User.DisplayName}
	devices, err := h.Auth.ListDevices(ctx, s.User.ID)
	if err != nil {
		return info, err
	}
	for _, d := range devices {
		if d.ID == s.DeviceID {
			info.Name, info.Platform = d.Name, d.Platform
			if info.Name == "" {
				info.Name = d.Product
			}
		}
	}
	return info, nil
}

func validRemoteState(st *RemotePlayerState) error {
	if !st.State.Valid() {
		return fmt.Errorf("unknown state %q", st.State)
	}
	if st.Volume != nil && (*st.Volume < 0 || *st.Volume > 1) {
		return errors.New("volume must be between 0 and 1")
	}
	return nil
}

func (h *Handlers) RemoteInbox(ctx context.Context, req RemoteInboxRequestObject) (RemoteInboxResponseObject, error) {
	s, ok := session(ctx)
	if !ok || s.DeviceID == 0 {
		return RemoteInbox401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	b := req.Body
	caps := make([]string, 0, len(b.Capabilities))
	for _, c := range b.Capabilities {
		if c.Valid() {
			caps = append(caps, string(c))
		}
	}
	var state any
	if b.State != nil && validRemoteState(b.State) == nil {
		state = *b.State
	}
	var cursor int64
	if b.Cursor != nil {
		cursor = *b.Cursor
	}
	info, err := h.deviceInfo(ctx, s)
	if err != nil {
		return nil, internal(ctx, "remoteInbox", err)
	}
	cmds, cursor, err := h.Remote.Inbox(ctx, info, caps, state, cursor, remoteWait)
	if errors.Is(err, remote.ErrFull) {
		slog.Warn("remote control: too many players", "device", s.DeviceID)
		select { // answer at the usual pace so the app doesn't poll in a tight loop
		case <-time.After(remoteWait):
		case <-ctx.Done():
		}
	}
	out := RemoteInbox200JSONResponse{Cursor: cursor, Commands: make([]RemoteCommand, 0, len(cmds))}
	for _, c := range cmds {
		if rc, ok := c.Body.(RemoteCommand); ok {
			out.Commands = append(out.Commands, rc)
		}
	}
	return out, nil
}

func (h *Handlers) ReportRemoteState(ctx context.Context, req ReportRemoteStateRequestObject) (ReportRemoteStateResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return ReportRemoteState401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	// The contract has no 400 here: a malformed state is dropped rather than shown.
	if validRemoteState(req.Body) == nil {
		h.Remote.SetState(s.DeviceID, *req.Body)
	}
	return ReportRemoteState204Response{}, nil
}

func (h *Handlers) ListRemotePlayers(ctx context.Context, _ ListRemotePlayersRequestObject) (ListRemotePlayersResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return ListRemotePlayers401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	out := ListRemotePlayers200JSONResponse{}
	for _, p := range h.Remote.Players() {
		if canControl(s, p) {
			out = append(out, toAPIPlayer(p))
		}
	}
	return out, nil
}

var errNoPlayer = apiErr("not_found", "that player isn't available")

func (h *Handlers) GetRemotePlayer(ctx context.Context, req GetRemotePlayerRequestObject) (GetRemotePlayerResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return GetRemotePlayer401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	p, err := h.Remote.Player(req.DeviceId)
	if err != nil || !canControl(s, p) {
		return GetRemotePlayer404JSONResponse{NotFoundJSONResponse(errNoPlayer)}, nil
	}
	if req.Params.Since != nil {
		if p, err = h.Remote.Wait(ctx, req.DeviceId, *req.Params.Since, remoteWait); err != nil {
			return GetRemotePlayer404JSONResponse{NotFoundJSONResponse(errNoPlayer)}, nil
		}
	}
	return GetRemotePlayer200JSONResponse(toAPIPlayer(p)), nil
}

// checkRemoteCommand validates a command for a player; play items must be visible to the
// player's user, since that's who will be watching.
func (h *Handlers) checkRemoteCommand(ctx context.Context, c *RemoteCommand, p remote.Player) (msg string, err error) {
	switch c.Type {
	case RemoteCommandTypePlay:
		if c.ItemIds == nil || len(*c.ItemIds) == 0 {
			return "play needs itemIds", nil
		}
		ids := *c.ItemIds
		if len(ids) > maxRemoteItems {
			return fmt.Sprintf("play takes at most %d items", maxRemoteItems), nil
		}
		if c.Index != nil && (*c.Index < 0 || *c.Index >= len(ids)) {
			return "index is outside itemIds", nil
		}
		if c.StartMs != nil && *c.StartMs < 0 {
			return "startMs cannot be negative", nil
		}
		u, err := h.Auth.GetUser(ctx, p.UserID)
		if err != nil {
			return "", err
		}
		distinct := map[int64]bool{}
		for _, id := range ids {
			distinct[id] = true
		}
		uniq := make([]int64, 0, len(distinct))
		for id := range distinct {
			uniq = append(uniq, id)
		}
		visible, err := h.Items.ByIDs(ctx, userAccess(u), uniq)
		if err != nil {
			return "", err
		}
		if len(visible) != len(uniq) {
			return "some of these items can't be played on that device", nil
		}
	case RemoteCommandTypeSeek:
		if c.PositionMs == nil || *c.PositionMs < 0 {
			return "seek needs positionMs", nil
		}
	case RemoteCommandTypeSetAudio, RemoteCommandTypeSetSubtitle:
		if c.StreamId == nil {
			return string(c.Type) + " needs streamId", nil
		}
	case RemoteCommandTypeSetVolume:
		if c.Volume == nil || *c.Volume < 0 || *c.Volume > 1 {
			return "setVolume needs a volume between 0 and 1", nil
		}
	default:
		if !c.Type.Valid() {
			return fmt.Sprintf("unknown command %q", c.Type), nil
		}
	}
	return "", nil
}

func (h *Handlers) SendRemoteCommand(ctx context.Context, req SendRemoteCommandRequestObject) (SendRemoteCommandResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return SendRemoteCommand401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	p, err := h.Remote.Player(req.DeviceId)
	if err != nil || !canControl(s, p) {
		return SendRemoteCommand404JSONResponse{NotFoundJSONResponse(errNoPlayer)}, nil
	}
	c := *req.Body
	if msg, err := h.checkRemoteCommand(ctx, &c, p); err != nil {
		return nil, internal(ctx, "sendRemoteCommand", err)
	} else if msg != "" {
		return SendRemoteCommand400JSONResponse{BadRequestJSONResponse(apiErr("invalid", msg))}, nil
	}
	from, err := h.deviceInfo(ctx, s)
	if err != nil {
		return nil, internal(ctx, "sendRemoteCommand", err)
	}
	c.From = ptr(from.Name)
	if err := h.Remote.Send(req.DeviceId, c); err != nil {
		return SendRemoteCommand404JSONResponse{NotFoundJSONResponse(errNoPlayer)}, nil
	}
	slog.Debug("remote command", "type", c.Type, "from", from.Name, "to", p.Name)
	return SendRemoteCommand204Response{}, nil
}
