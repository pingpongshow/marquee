package api

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"marquee/internal/auth"
	"marquee/internal/items"
)

// access returns the item visibility rules for the caller.
func access(ctx context.Context) items.Access {
	s, ok := session(ctx)
	if !ok {
		return items.Unrestricted
	}
	if s.User.IsAdmin {
		return items.Access{UserID: s.User.ID}
	}
	acc := items.Access{UserID: s.User.ID}
	if r := s.User.Restrictions; r.LibraryIDs != nil {
		acc.LibraryIDs = append([]int64{}, *r.LibraryIDs...)
	}
	if r := s.User.Restrictions.MaxContentRating; r != nil {
		acc.MaxRating = *r
	}
	return acc
}

func canSeeLibrary(ctx context.Context, id int64) bool {
	acc := access(ctx)
	if acc.LibraryIDs == nil {
		return true
	}
	for _, l := range acc.LibraryIDs {
		if l == id {
			return true
		}
	}
	return false
}

func toAPIRestrictions(r auth.Restrictions) UserRestrictions {
	out := UserRestrictions{LibraryIds: r.LibraryIDs, AllowRemote: ptr(r.RemoteAllowed()), RemoteQualityKbps: ptr(r.RemoteQualityKbps)}
	if r.MaxContentRating != nil {
		out.MaxContentRating = ptr(UserRestrictionsMaxContentRating(*r.MaxContentRating))
	}
	return out
}

func fromAPIRestrictions(r *UserRestrictions) (auth.Restrictions, error) {
	var out auth.Restrictions
	if r == nil {
		return out, nil
	}
	out.LibraryIDs = r.LibraryIds
	if r.MaxContentRating != nil && *r.MaxContentRating != "" {
		v := string(*r.MaxContentRating)
		if items.RatingLevel(v) == 0 {
			return out, errors.New("unknown content rating " + v)
		}
		out.MaxContentRating = &v
	}
	out.AllowRemote = r.AllowRemote
	if r.RemoteQualityKbps != nil {
		if *r.RemoteQualityKbps < 0 {
			return out, errors.New("remote quality cannot be negative")
		}
		out.RemoteQualityKbps = *r.RemoteQualityKbps
	}
	return out, nil
}

func toAPIPrefs(p auth.Preferences) UserPreferences {
	out := UserPreferences{AudioLanguage: nz(p.AudioLanguage), SubtitleLanguage: nz(p.SubtitleLanguage),
		LocalQualityKbps: ptr(p.LocalQualityKbps), RemoteQualityKbps: ptr(p.RemoteQualityKbps)}
	if p.SubtitleMode != "" {
		out.SubtitleMode = ptr(UserPreferencesSubtitleMode(p.SubtitleMode))
	}
	return out
}

func fromAPIPrefs(p UserPreferences) auth.Preferences {
	out := auth.Preferences{}
	set(&out.AudioLanguage, p.AudioLanguage)
	set(&out.SubtitleLanguage, p.SubtitleLanguage)
	set(&out.LocalQualityKbps, p.LocalQualityKbps)
	set(&out.RemoteQualityKbps, p.RemoteQualityKbps)
	if p.SubtitleMode != nil {
		out.SubtitleMode = string(*p.SubtitleMode)
	}
	return out
}

func validPIN(p string) bool {
	if len(p) != 4 {
		return false
	}
	for _, c := range p {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func adminCheck(ctx context.Context) (authed, admin bool) { return isAdmin(ctx) }

// ---------- /me ----------

func (h *Handlers) UpdateMe(ctx context.Context, req UpdateMeRequestObject) (UpdateMeResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return UpdateMe401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	b := req.Body
	upd := auth.UserUpdate{}
	if b.DisplayName != nil {
		name := strings.TrimSpace(*b.DisplayName)
		if name == "" || len(name) > 64 {
			return UpdateMe400JSONResponse{BadRequestJSONResponse(apiErr("invalid", "display name must be 1–64 characters"))}, nil
		}
		upd.DisplayName = &name
	}
	if b.NewPassword != nil {
		if s.User.IsManaged {
			return UpdateMe403JSONResponse{ForbiddenJSONResponse(apiErr("forbidden", "managed profiles can't set a password"))}, nil
		}
		if len(*b.NewPassword) < 8 {
			return UpdateMe400JSONResponse{BadRequestJSONResponse(apiErr("invalid", "password must be at least 8 characters"))}, nil
		}
		cur := ""
		if b.CurrentPassword != nil {
			cur = *b.CurrentPassword
		}
		// Changing an existing password needs the current one; setting a first one doesn't.
		if s.User.HasPassword {
			if ok, err := h.Auth.CheckPassword(ctx, s.User.ID, cur); err != nil {
				return nil, internal(ctx, "updateMe", err)
			} else if !ok {
				return UpdateMe403JSONResponse{ForbiddenJSONResponse(apiErr("wrong_password", "current password is incorrect"))}, nil
			}
		}
		upd.Password = b.NewPassword
	}
	if b.Pin != nil {
		if *b.Pin != "" && !validPIN(*b.Pin) {
			return UpdateMe400JSONResponse{BadRequestJSONResponse(apiErr("invalid", "PIN must be 4 digits"))}, nil
		}
		if s.User.IsManaged && !s.User.IsAdmin {
			// A child shouldn't be able to remove or change the PIN their parent set.
			return UpdateMe403JSONResponse{ForbiddenJSONResponse(apiErr("forbidden", "ask an administrator to change this PIN"))}, nil
		}
		upd.PIN = b.Pin
	}
	if b.Preferences != nil {
		p := fromAPIPrefs(*b.Preferences)
		upd.Preferences = &p
	}
	u, err := h.Auth.Update(ctx, s.User.ID, upd)
	if err != nil {
		return nil, internal(ctx, "updateMe", err)
	}
	return UpdateMe200JSONResponse(toAPIUser(u)), nil
}

// ---------- users (admin) ----------

func (h *Handlers) ListUsers(ctx context.Context, _ ListUsersRequestObject) (ListUsersResponseObject, error) {
	switch authed, admin := adminCheck(ctx); {
	case !authed:
		return ListUsers401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return ListUsers403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	list, err := h.Auth.ListUsers(ctx)
	if err != nil {
		return nil, internal(ctx, "listUsers", err)
	}
	out := make(ListUsers200JSONResponse, len(list))
	for i, u := range list {
		out[i] = toAPIUser(u)
	}
	return out, nil
}

func (h *Handlers) CreateUser(ctx context.Context, req CreateUserRequestObject) (CreateUserResponseObject, error) {
	switch authed, admin := adminCheck(ctx); {
	case !authed:
		return CreateUser401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return CreateUser403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	b := req.Body
	bad := func(msg string) (CreateUserResponseObject, error) {
		return CreateUser400JSONResponse{BadRequestJSONResponse(apiErr("invalid", msg))}, nil
	}
	n := auth.NewUser{Username: strings.TrimSpace(b.Username)}
	if n.Username == "" || len(n.Username) > 64 {
		return bad("username must be 1–64 characters")
	}
	set(&n.DisplayName, b.DisplayName)
	n.DisplayName = strings.TrimSpace(n.DisplayName)
	set(&n.Password, b.Password)
	set(&n.PIN, b.Pin)
	set(&n.IsAdmin, b.IsAdmin)
	set(&n.IsManaged, b.IsManaged)
	if n.Password != "" && len(n.Password) < 8 {
		return bad("password must be at least 8 characters")
	}
	if n.PIN != "" && !validPIN(n.PIN) {
		return bad("PIN must be 4 digits")
	}
	r, err := fromAPIRestrictions(b.Restrictions)
	if err != nil {
		return bad(err.Error())
	}
	n.Restrictions = r
	u, err := h.Auth.Create(ctx, n)
	switch {
	case errors.Is(err, auth.ErrUsernameTaken):
		return CreateUser409JSONResponse{ConflictJSONResponse(apiErr("username_taken", err.Error()))}, nil
	case errors.Is(err, auth.ErrNoAuthMeans):
		return bad(err.Error())
	case err != nil:
		return nil, internal(ctx, "createUser", err)
	}
	return CreateUser201JSONResponse(toAPIUser(u)), nil
}

func (h *Handlers) UpdateUser(ctx context.Context, req UpdateUserRequestObject) (UpdateUserResponseObject, error) {
	switch authed, admin := adminCheck(ctx); {
	case !authed:
		return UpdateUser401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return UpdateUser403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	b := req.Body
	bad := func(msg string) (UpdateUserResponseObject, error) {
		return UpdateUser400JSONResponse{BadRequestJSONResponse(apiErr("invalid", msg))}, nil
	}
	upd := auth.UserUpdate{DisplayName: b.DisplayName, Password: b.Password, PIN: b.Pin, IsAdmin: b.IsAdmin}
	if b.Password != nil && len(*b.Password) < 8 {
		return bad("password must be at least 8 characters")
	}
	if b.Pin != nil && *b.Pin != "" && !validPIN(*b.Pin) {
		return bad("PIN must be 4 digits")
	}
	if b.Restrictions != nil {
		r, err := fromAPIRestrictions(b.Restrictions)
		if err != nil {
			return bad(err.Error())
		}
		upd.Restrictions = &r
	}
	u, err := h.Auth.Update(ctx, req.UserId, upd)
	switch {
	case errors.Is(err, auth.ErrUserNotFound):
		return UpdateUser404JSONResponse{NotFoundJSONResponse(apiErr("not_found", err.Error()))}, nil
	case errors.Is(err, auth.ErrLastAdmin):
		return UpdateUser409JSONResponse{ConflictJSONResponse(apiErr("last_admin", err.Error()))}, nil
	case errors.Is(err, auth.ErrNoAuthMeans):
		return bad("set a password to make this user an administrator")
	case err != nil && strings.Contains(err.Error(), "managed profiles"):
		return bad(err.Error())
	case err != nil:
		return nil, internal(ctx, "updateUser", err)
	}
	return UpdateUser200JSONResponse(toAPIUser(u)), nil
}

func (h *Handlers) DeleteUser(ctx context.Context, req DeleteUserRequestObject) (DeleteUserResponseObject, error) {
	switch authed, admin := adminCheck(ctx); {
	case !authed:
		return DeleteUser401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return DeleteUser403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	err := h.Auth.Delete(ctx, req.UserId)
	switch {
	case errors.Is(err, auth.ErrUserNotFound):
		return DeleteUser404JSONResponse{NotFoundJSONResponse(apiErr("not_found", err.Error()))}, nil
	case errors.Is(err, auth.ErrLastAdmin):
		return DeleteUser409JSONResponse{ConflictJSONResponse(apiErr("last_admin", err.Error()))}, nil
	case err != nil:
		return nil, internal(ctx, "deleteUser", err)
	}
	if err := h.Avatars.Remove(ctx, req.UserId); err != nil {
		slog.WarnContext(ctx, "remove avatar", "user", req.UserId, "err", err)
	}
	return DeleteUser204Response{}, nil
}

// ---------- profiles ----------

func (h *Handlers) ListProfiles(ctx context.Context, _ ListProfilesRequestObject) (ListProfilesResponseObject, error) {
	if _, ok := session(ctx); !ok {
		return ListProfiles401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	list, err := h.Auth.ListUsers(ctx)
	if err != nil {
		return nil, internal(ctx, "profiles", err)
	}
	out := make(ListProfiles200JSONResponse, len(list))
	for i, u := range list {
		out[i] = toAPIProfile(u)
	}
	return out, nil
}

func (h *Handlers) SwitchProfile(ctx context.Context, req SwitchProfileRequestObject) (SwitchProfileResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return SwitchProfile401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	target, err := h.Auth.GetUser(ctx, req.UserId)
	if errors.Is(err, auth.ErrUserNotFound) {
		return SwitchProfile404JSONResponse{NotFoundJSONResponse(apiErr("not_found", err.Error()))}, nil
	}
	if err != nil {
		return nil, internal(ctx, "switch", err)
	}
	b := req.Body
	pin, pw := "", ""
	set(&pin, b.Pin)
	set(&pw, b.Password)
	ip := requestInfo(ctx).ClientIP
	// Admins can switch into managed profiles without the PIN (like a Plex Home admin).
	if !(s.User.IsAdmin && target.IsManaged) {
		err = h.Auth.VerifySwitch(ctx, ip, target, pin, pw)
		var rl *auth.RateLimitedError
		switch {
		case errors.As(err, &rl):
			return SwitchProfile429JSONResponse{TooManyRequestsJSONResponse(apiErr("rate_limited", rl.Error()))}, nil
		case errors.Is(err, auth.ErrWrongPIN):
			return SwitchProfile401JSONResponse{UnauthorizedJSONResponse(apiErr("wrong_pin", err.Error()))}, nil
		case err != nil:
			return nil, internal(ctx, "switch", err)
		}
	}
	if !validDevice(b.Device) {
		return SwitchProfile401JSONResponse{UnauthorizedJSONResponse(apiErr("invalid_device", "device clientId and name are required"))}, nil
	}
	token, err := h.Auth.IssueToken(ctx, h.DB, target.ID, toDevice(b.Device), ip)
	if err != nil {
		return nil, internal(ctx, "switch", err)
	}
	// The previous profile's session on this device ends.
	if err := h.Auth.RevokeDevice(ctx, s.DeviceID); err != nil {
		return nil, internal(ctx, "switch", err)
	}
	return SwitchProfile200JSONResponse{Token: token, User: toAPIUser(target)}, nil
}

// ---------- devices ----------

func (h *Handlers) ListDevices(ctx context.Context, _ ListDevicesRequestObject) (ListDevicesResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return ListDevices401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	uid := s.User.ID
	if s.User.IsAdmin {
		uid = 0
	}
	list, err := h.Auth.ListDevices(ctx, uid)
	if err != nil {
		return nil, internal(ctx, "devices", err)
	}
	out := make(ListDevices200JSONResponse, len(list))
	for i, d := range list {
		out[i] = Device{Id: d.ID, UserId: d.UserID, UserName: d.UserName, Name: d.Name, Platform: d.Platform,
			Product: nz(d.Product), Version: nz(d.Version), LastIp: nz(d.LastIP), LastSeenAt: d.LastSeenAt,
			CreatedAt: d.CreatedAt, Current: d.ID == s.DeviceID}
	}
	return out, nil
}

func (h *Handlers) RevokeDevice(ctx context.Context, req RevokeDeviceRequestObject) (RevokeDeviceResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return RevokeDevice401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	owner, err := h.Auth.DeviceOwner(ctx, req.DeviceId)
	if errors.Is(err, auth.ErrUserNotFound) {
		return RevokeDevice404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "device not found"))}, nil
	}
	if err != nil {
		return nil, internal(ctx, "revoke", err)
	}
	if owner != s.User.ID && !s.User.IsAdmin {
		return RevokeDevice403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	if err := h.Auth.RevokeDevice(ctx, req.DeviceId); err != nil {
		return nil, internal(ctx, "revoke", err)
	}
	return RevokeDevice204Response{}, nil
}

// ---------- search ----------

func (h *Handlers) Search(ctx context.Context, req SearchRequestObject) (SearchResponseObject, error) {
	if _, ok := session(ctx); !ok {
		return Search401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	limit := 10
	if req.Params.Limit != nil && *req.Params.Limit > 0 && *req.Params.Limit <= 50 {
		limit = *req.Params.Limit
	}
	// Search only libraries the user can see that are included in search.
	acc := access(ctx)
	libs, err := h.Libraries.List(ctx)
	if err != nil {
		return nil, internal(ctx, "search", err)
	}
	ids := []int64{}
	for _, l := range libs {
		if (l.Options.IncludeInSearch == nil || *l.Options.IncludeInSearch) && canSeeLibrary(ctx, l.ID) {
			ids = append(ids, l.ID)
		}
	}
	acc.LibraryIDs = ids
	groups, err := h.Items.Search(ctx, acc, req.Params.Q, limit)
	if err != nil {
		return nil, internal(ctx, "search", err)
	}
	out := SearchResults{Query: req.Params.Q, Groups: make([]SearchGroup, len(groups))}
	for i, g := range groups {
		sg := SearchGroup{Type: ItemType(g.Type), Items: make([]ItemSummary, len(g.Items))}
		for j, it := range g.Items {
			sg.Items[j] = toAPISummary(it)
		}
		out.Groups[i] = sg
	}
	return Search200JSONResponse(out), nil
}
