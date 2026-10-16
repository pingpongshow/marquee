package api

import (
	"context"
	"errors"
	"strings"

	"marquee/internal/plexsync"
	"marquee/internal/settings"
	"marquee/internal/tasks"
)

func (h *Handlers) plexSyncStatus(ctx context.Context) PlexSyncStatus {
	c := h.Settings.Get().PlexSync
	out := PlexSyncStatus{
		Settings: PlexSyncSettings{Url: c.URL, TokenSet: c.Token != "", Enabled: c.Enabled, IntervalMinutes: c.IntervalMinutes,
			WatchState: c.WatchState, Ratings: c.Ratings, ResumePositions: c.ResumePositions, PlayHistory: c.PlayHistory,
			PathMappings: []PlexPathMapping{}},
		Users: []PlexSyncUser{},
	}
	for _, m := range c.PathMappings {
		out.Settings.PathMappings = append(out.Settings.PathMappings, PlexPathMapping{PlexPath: m.Plex, MarqueePath: m.Marquee})
	}
	if h.PlexSync == nil {
		return out
	}
	running, p := h.PlexSync.Running()
	out.Running = running
	if running {
		out.Progress = &struct {
			Done  int    `json:"done"`
			Step  string `json:"step"`
			Total int    `json:"total"`
		}{Done: p.Done, Step: p.Step, Total: p.Total}
	}
	out.Reachable = h.PlexSync.Reachable(ctx)
	out.LastError = nz(h.PlexSync.LastError(ctx))
	if r := h.PlexSync.LastReport(ctx); r != nil {
		rep := toAPIPlexSyncReport(r)
		out.LastRun = &rep
	}
	out.NextRunAt = h.PlexSync.NextRun(ctx)
	users, err := h.PlexSync.Users(ctx)
	if err == nil {
		for _, u := range users {
			out.Users = append(out.Users, PlexSyncUser{PlexAccountId: u.PlexAccountID, Name: u.Name, Kind: PlexSyncUserKind(u.Kind),
				UserId: nz(u.UserID), CanSync: u.CanSync, Reason: nz(u.Reason)})
		}
	}
	return out
}

func toAPIPlexSyncReport(r *plexsync.Report) PlexSyncReport {
	out := PlexSyncReport{StartedAt: r.StartedAt, FinishedAt: r.FinishedAt, DryRun: r.DryRun, Waiting: r.Waiting, ToPlex: r.ToPlex,
		ToMarquee: r.ToMarquee, HistoryPulled: r.HistoryPulled, Conflicts: r.Conflicts, Skipped: r.Skipped, Failed: r.Failed,
		Unchanged: r.Unchanged, NotInMarquee: r.NotInMarquee, UsersSynced: r.UsersSynced, Changes: []PlexSyncChange{},
		ChangesTruncated: r.ChangesTruncated, Errors: orEmpty(r.Errors)}
	for _, c := range r.Changes {
		out.Changes = append(out.Changes, PlexSyncChange{Direction: PlexSyncChangeDirection(c.Direction), User: c.User, Title: c.Title,
			Kind: PlexSyncChangeKind(c.Kind), From: nz(c.From), To: nz(c.To), Conflict: nz(c.Conflict)})
	}
	return out
}

func (h *Handlers) GetPlexSync(ctx context.Context, _ GetPlexSyncRequestObject) (GetPlexSyncResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return GetPlexSync401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return GetPlexSync403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	return GetPlexSync200JSONResponse(h.plexSyncStatus(ctx)), nil
}

func (h *Handlers) UpdatePlexSync(ctx context.Context, req UpdatePlexSyncRequestObject) (UpdatePlexSyncResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return UpdatePlexSync401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return UpdatePlexSync403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	b := req.Body
	_, err := h.Settings.Update(ctx, func(s *settings.Settings) error {
		c := &s.PlexSync
		if b.Url != nil {
			c.URL = strings.TrimRight(strings.TrimSpace(*b.Url), "/")
		}
		if b.Token != nil {
			c.Token = strings.TrimSpace(*b.Token)
		}
		set(&c.Enabled, b.Enabled)
		set(&c.IntervalMinutes, b.IntervalMinutes)
		set(&c.WatchState, b.WatchState)
		set(&c.Ratings, b.Ratings)
		set(&c.ResumePositions, b.ResumePositions)
		set(&c.PlayHistory, b.PlayHistory)
		if b.PathMappings != nil {
			c.PathMappings = nil
			for _, m := range *b.PathMappings {
				c.PathMappings = append(c.PathMappings, settings.PlexPathMapping{Plex: strings.TrimSpace(m.PlexPath), Marquee: strings.TrimSpace(m.MarqueePath)})
			}
		}
		if c.Enabled && !c.Configured() {
			return &settings.ValidationError{Msg: "set the Plex server address and token before turning on automatic sync"}
		}
		return nil
	})
	var ve *settings.ValidationError
	if errors.As(err, &ve) {
		return UpdatePlexSync400JSONResponse{BadRequestJSONResponse(apiErr("invalid", ve.Msg))}, nil
	}
	if err != nil {
		return nil, internal(ctx, "updatePlexSync", err)
	}
	return UpdatePlexSync200JSONResponse(h.plexSyncStatus(ctx)), nil
}

func (h *Handlers) RunPlexSync(ctx context.Context, _ RunPlexSyncRequestObject) (RunPlexSyncResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return RunPlexSync401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return RunPlexSync403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	if h.PlexSync == nil || h.Tasks == nil {
		return RunPlexSync400JSONResponse{BadRequestJSONResponse(apiErr("unavailable", "Plex sync isn't available"))}, nil
	}
	if !h.Settings.Get().PlexSync.Configured() {
		return RunPlexSync400JSONResponse{BadRequestJSONResponse(apiErr("not_configured", plexsync.ErrNotConfigured.Error()))}, nil
	}
	if running, _ := h.PlexSync.Running(); running {
		return RunPlexSync409JSONResponse{ConflictJSONResponse(apiErr("running", plexsync.ErrRunning.Error()))}, nil
	}
	// Through the scheduler, so the run shows in Scheduled Tasks with its result.
	if err := h.Tasks.RunNow(ctx, plexsync.TaskID); errors.Is(err, tasks.ErrTaskRunning) {
		return RunPlexSync409JSONResponse{ConflictJSONResponse(apiErr("running", plexsync.ErrRunning.Error()))}, nil
	} else if err != nil {
		return nil, internal(ctx, "runPlexSync", err)
	}
	st := h.plexSyncStatus(ctx)
	st.Running = true
	return RunPlexSync202JSONResponse(st), nil
}

func (h *Handlers) PreviewPlexSync(ctx context.Context, _ PreviewPlexSyncRequestObject) (PreviewPlexSyncResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return PreviewPlexSync401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return PreviewPlexSync403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	if h.PlexSync == nil {
		return PreviewPlexSync400JSONResponse{BadRequestJSONResponse(apiErr("unavailable", "Plex sync isn't available"))}, nil
	}
	rep, err := h.PlexSync.Run(ctx, true)
	switch {
	case errors.Is(err, plexsync.ErrNotConfigured):
		return PreviewPlexSync400JSONResponse{BadRequestJSONResponse(apiErr("not_configured", err.Error()))}, nil
	case errors.Is(err, plexsync.ErrRunning):
		return PreviewPlexSync409JSONResponse{ConflictJSONResponse(apiErr("running", err.Error()))}, nil
	case err != nil:
		// Already redacted: e.g. Plex rejected the token.
		return PreviewPlexSync400JSONResponse{BadRequestJSONResponse(apiErr("plex", err.Error()))}, nil
	}
	return PreviewPlexSync200JSONResponse(toAPIPlexSyncReport(rep)), nil
}

func (h *Handlers) SetPlexSyncUser(ctx context.Context, req SetPlexSyncUserRequestObject) (SetPlexSyncUserResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return SetPlexSyncUser401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return SetPlexSyncUser403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	if h.PlexSync == nil {
		return SetPlexSyncUser404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "no such Plex account"))}, nil
	}
	var uid int64
	if req.Body != nil && req.Body.UserId != nil {
		uid = *req.Body.UserId
	}
	switch err := h.PlexSync.SetUser(ctx, req.PlexAccountId, uid); {
	case errors.Is(err, plexsync.ErrNoAccount):
		return SetPlexSyncUser404JSONResponse{NotFoundJSONResponse(apiErr("not_found", err.Error()))}, nil
	case errors.Is(err, plexsync.ErrNoUser):
		return SetPlexSyncUser400JSONResponse{BadRequestJSONResponse(apiErr("invalid", err.Error()))}, nil
	case err != nil:
		return nil, internal(ctx, "setPlexSyncUser", err)
	}
	return SetPlexSyncUser200JSONResponse(h.plexSyncStatus(ctx)), nil
}

func (h *Handlers) StartPlexSignIn(ctx context.Context, _ StartPlexSignInRequestObject) (StartPlexSignInResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return StartPlexSignIn401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return StartPlexSignIn403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	if h.PlexSync == nil {
		return StartPlexSignIn502JSONResponse{BadGatewayJSONResponse(apiErr("unavailable", "Plex sync isn't available"))}, nil
	}
	p, err := h.PlexSync.StartPin(ctx)
	if err != nil {
		return StartPlexSignIn502JSONResponse{BadGatewayJSONResponse(apiErr("plex_tv", "couldn't reach plex.tv: "+err.Error()))}, nil
	}
	return StartPlexSignIn200JSONResponse{Id: p.ID, Code: p.Code, AuthUrl: p.AuthURL}, nil
}

func (h *Handlers) CheckPlexSignIn(ctx context.Context, req CheckPlexSignInRequestObject) (CheckPlexSignInResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return CheckPlexSignIn401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return CheckPlexSignIn403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	if h.PlexSync == nil {
		return CheckPlexSignIn502JSONResponse{BadGatewayJSONResponse(apiErr("unavailable", "Plex sync isn't available"))}, nil
	}
	ok, err := h.PlexSync.CheckPin(ctx, req.PinId)
	if err != nil {
		return CheckPlexSignIn502JSONResponse{BadGatewayJSONResponse(apiErr("plex_tv", "couldn't check with plex.tv: "+err.Error()))}, nil
	}
	return CheckPlexSignIn200JSONResponse{SignedIn: ok}, nil
}
