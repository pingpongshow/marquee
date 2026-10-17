package api

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"

	"marquee/internal/auth"
	"marquee/internal/items"
	"marquee/internal/library"
	"marquee/internal/livetv"
	"marquee/internal/settings"
	"marquee/internal/vodsave"
)

// DVR (LIVE-5, D76). Recordings are the household's: everyone who may view DVR recordings
// sees them all (within their rating limit); scheduling is per user, and only whoever
// scheduled a recording or series, or an admin, may cancel or delete it.

func canRecord(s auth.Session) bool { return s.User.IsAdmin || s.User.Restrictions.DVRRecordAllowed() }

func canViewDVR(s auth.Session) bool { return s.User.IsAdmin || s.User.Restrictions.DVRViewAllowed() }

func canManage(s auth.Session, owner int64) bool { return s.User.IsAdmin || s.User.ID == owner }

// DVRQuota is a user's DVR quota: in bytes and GB (0 = unlimited), and whether it's their
// own rather than the server default. Admins aren't subject to the default: without a
// quota of their own they have no limit; one set for them applies as to anyone.
func DVRQuota(u auth.User, s settings.Settings) (bytes int64, gb int, own bool) {
	gb = s.Integrations.DVRDefaultQuotaGB
	if u.IsAdmin {
		gb = 0
	}
	if q := u.Restrictions.DVRQuotaGB; q != nil {
		gb, own = *q, true
	}
	return int64(gb) * 1_000_000_000, gb, own
}

// recordingVisible: a recording filed in the library follows the library's rules (rating
// limit, DVR viewing); one that isn't yet is judged by the guide's rating, and an unrated
// one is hidden from profiles with a rating limit, as the library does.
func recordingVisible(acc items.Access, x livetv.Recording, visibleItems map[int64]items.Summary) bool {
	if x.ItemID != nil {
		_, ok := visibleItems[*x.ItemID]
		return ok
	}
	if lvl := items.RatingLevel(acc.MaxRating); lvl > 0 {
		r := items.RatingLevel(x.Rating)
		return r > 0 && r <= lvl
	}
	return true
}

// visibleRecordings filters recordings to those the caller may see, with their library
// items (and the caller's watch state on them) by id.
func (h *Handlers) visibleRecordings(ctx context.Context, list []livetv.Recording) ([]livetv.Recording, map[int64]items.Summary, error) {
	acc := itemAccess(ctx)
	var ids []int64
	for _, x := range list {
		if x.ItemID != nil {
			ids = append(ids, *x.ItemID)
		}
	}
	vis := map[int64]items.Summary{}
	if len(ids) > 0 {
		got, err := h.Items.ByIDs(ctx, acc, ids)
		if err != nil {
			return nil, nil, err
		}
		for _, it := range got {
			vis[it.ID] = it
		}
	}
	out := list[:0:0]
	for _, x := range list {
		if recordingVisible(acc, x, vis) {
			out = append(out, x)
		}
	}
	return out, vis, nil
}

// onlyMine reports whether a list asks for the caller's own recordings or series only.
func onlyMine[T ~string](owner *T) bool { return owner != nil && *owner == "me" }

func toAPIRecording(x livetv.Recording) Recording {
	status := RecordingStatus(x.Status)
	return Recording{Id: x.ID, ChannelId: x.ChannelID, ChannelName: x.ChannelName, Start: x.Start, End: x.Stop, Title: x.Title,
		Subtitle: nz(x.Subtitle), Description: nz(x.Description), Episode: nz(x.Episode), Category: nz(x.Category), ImageUrl: nz(x.Image),
		Status: status, Series: x.RuleID != nil, RuleId: x.RuleID, Error: nz(x.Error), SizeBytes: nz(x.Size), ItemId: x.ItemID, UserId: &x.UserID,
		OwnerName: nz(x.OwnerName), Rating: nz(x.Rating), StoppedEarly: nz(x.StoppedEarly)}
}

func toAPIRule(r livetv.Rule) RecordingRule {
	return RecordingRule{Id: r.ID, Title: r.Title, ChannelId: r.ChannelID, ChannelName: nz(r.ChannelName), Upcoming: r.Upcoming,
		UserId: &r.UserID, CreatedAt: r.CreatedAt, OwnerName: nz(r.OwnerName), Paused: ptr(r.Paused)}
}

func (h *Handlers) ListRecordings(ctx context.Context, req ListRecordingsRequestObject) (ListRecordingsResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return ListRecordings401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	if !canViewDVR(s) || h.DVR == nil {
		return ListRecordings403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	list, err := h.DVR.List(ctx)
	var vis map[int64]items.Summary
	if err == nil {
		list, vis, err = h.visibleRecordings(ctx, list)
	}
	if err != nil {
		return nil, internal(ctx, "listRecordings", err)
	}
	mine := onlyMine(req.Params.Owner)
	out := ListRecordings200JSONResponse{}
	for _, x := range list {
		if mine && x.UserID != s.User.ID {
			continue
		}
		r := toAPIRecording(x)
		r.CanDelete = ptr(canManage(s, x.UserID))
		if x.Status == livetv.StatusCompleted && x.ItemID != nil {
			// Recordings aren't in Home's Continue Watching: the list shows where to resume.
			if it, ok := vis[*x.ItemID]; ok {
				r.ViewOffsetMs, r.DurationMs, r.Watched = ptr(it.ViewOffsetMS), nz(it.DurationMS), ptr(it.ViewCount > 0 && it.ViewOffsetMS == 0)
			}
			if s.User.IsAdmin {
				r.Commercials = h.recordingCommercials(ctx, x.Path)
			}
		}
		out = append(out, r)
	}
	return out, nil
}

func (h *Handlers) ScheduleRecording(ctx context.Context, req ScheduleRecordingRequestObject) (ScheduleRecordingResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return ScheduleRecording401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	if !canRecord(s) || h.DVR == nil {
		return ScheduleRecording403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	if !h.DVR.Available() {
		return ScheduleRecording503JSONResponse{ServiceUnavailableJSONResponse(apiErr("dvr_unavailable", livetv.ErrDVRUnavailable.Error()))}, nil
	}
	b := req.Body
	anyChannel := b.AnyChannel != nil && *b.AnyChannel
	rec, rule, err := h.DVR.Schedule(ctx, s.User.ID, b.ChannelId, b.Start, b.Series != nil && *b.Series, anyChannel)
	if errors.Is(err, livetv.ErrProgrammeNotFound) {
		return ScheduleRecording404JSONResponse{NotFoundJSONResponse(apiErr("not_found", err.Error()))}, nil
	}
	if errors.Is(err, livetv.ErrQuotaExceeded) {
		return ScheduleRecording409JSONResponse(apiErr("quota_exceeded", err.Error())), nil
	}
	if err != nil {
		return nil, internal(ctx, "scheduleRecording", err)
	}
	var out ScheduleRecording201JSONResponse
	if rec != nil {
		r := toAPIRecording(*rec)
		r.CanDelete = ptr(canManage(s, rec.UserID))
		out.Recording = &r
	}
	if rule != nil {
		r := toAPIRule(*rule)
		r.CanDelete = ptr(canManage(s, rule.UserID))
		out.Rule = &r
	}
	return out, nil
}

func (h *Handlers) CancelRecording(ctx context.Context, req CancelRecordingRequestObject) (CancelRecordingResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return CancelRecording401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	if !(canRecord(s) || canViewDVR(s)) || h.DVR == nil {
		return CancelRecording403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	x, err := h.DVR.Get(ctx, req.RecordingId)
	if errors.Is(err, livetv.ErrRecordingNotFound) {
		return CancelRecording404JSONResponse{NotFoundJSONResponse(apiErr("not_found", err.Error()))}, nil
	}
	if err != nil {
		return nil, internal(ctx, "cancelRecording", err)
	}
	deleteFile := req.Params.DeleteFile != nil && *req.Params.DeleteFile
	if !canManage(s, x.UserID) {
		return CancelRecording403JSONResponse{ForbiddenJSONResponse(apiErr("forbidden", "only an admin or whoever scheduled it can cancel or delete a recording"))}, nil
	}
	if err := h.DVR.Cancel(ctx, x.ID, deleteFile); err != nil {
		return nil, internal(ctx, "cancelRecording", err)
	}
	return CancelRecording204Response{}, nil
}

func (h *Handlers) StopRecording(ctx context.Context, req StopRecordingRequestObject) (StopRecordingResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return StopRecording401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	if !(canRecord(s) || canViewDVR(s)) || h.DVR == nil {
		return StopRecording403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	x, err := h.DVR.Get(ctx, req.RecordingId)
	if errors.Is(err, livetv.ErrRecordingNotFound) {
		return StopRecording404JSONResponse{NotFoundJSONResponse(apiErr("not_found", err.Error()))}, nil
	}
	if err != nil {
		return nil, internal(ctx, "stopRecording", err)
	}
	if !canManage(s, x.UserID) {
		return StopRecording403JSONResponse{ForbiddenJSONResponse(apiErr("forbidden", "only an admin or whoever scheduled it can stop a recording"))}, nil
	}
	keep := req.Body == nil || req.Body.Keep == nil || *req.Body.Keep
	// Filing a long recording takes a while; it carries on if the caller goes away.
	err = h.DVR.Stop(context.WithoutCancel(ctx), x.ID, keep)
	if errors.Is(err, livetv.ErrNotRecording) {
		return StopRecording409JSONResponse(apiErr("not_recording", "It isn't recording now.")), nil
	}
	if err != nil {
		return nil, internal(ctx, "stopRecording", err)
	}
	return StopRecording204Response{}, nil
}

func (h *Handlers) ListRecordingRules(ctx context.Context, req ListRecordingRulesRequestObject) (ListRecordingRulesResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return ListRecordingRules401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	if !(canRecord(s) || canViewDVR(s)) || h.DVR == nil {
		return ListRecordingRules403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	rules, err := h.DVR.Rules(ctx)
	if err != nil {
		return nil, internal(ctx, "listRecordingRules", err)
	}
	out := ListRecordingRules200JSONResponse{}
	for _, r := range rules {
		// A profile that may record but not view recordings sees only its own series.
		if (!canViewDVR(s) || onlyMine(req.Params.Owner)) && r.UserID != s.User.ID {
			continue
		}
		x := toAPIRule(r)
		x.CanDelete = ptr(canManage(s, r.UserID))
		out = append(out, x)
	}
	return out, nil
}

func (h *Handlers) DeleteRecordingRule(ctx context.Context, req DeleteRecordingRuleRequestObject) (DeleteRecordingRuleResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return DeleteRecordingRule401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	if !(canRecord(s) || canViewDVR(s)) || h.DVR == nil {
		return DeleteRecordingRule403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	owner, err := h.DVR.RuleOwner(ctx, req.RuleId)
	if err == nil && !canManage(s, owner) {
		return DeleteRecordingRule403JSONResponse{ForbiddenJSONResponse(apiErr("forbidden", "only an admin or whoever made it can stop a series"))}, nil
	}
	if err == nil {
		err = h.DVR.DeleteRule(ctx, req.RuleId)
	}
	if errors.Is(err, livetv.ErrRecordingNotFound) {
		return DeleteRecordingRule404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "series not found"))}, nil
	}
	if err != nil {
		return nil, internal(ctx, "deleteRecordingRule", err)
	}
	return DeleteRecordingRule204Response{}, nil
}

// dvrUsage is a user's usage and quota.
func (h *Handlers) dvrUsage(u auth.User, used livetv.Usage) DvrUsage {
	cfg := h.Settings.Get()
	bytes, gb, own := DVRQuota(u, cfg)
	src := DvrUsageQuotaSourceDefault
	if own {
		src = DvrUsageQuotaSourceUser
	}
	out := DvrUsage{UserId: u.ID, DisplayName: ptr(u.DisplayName), UsedBytes: used.Bytes, Recordings: used.Recordings,
		QuotaBytes: bytes, QuotaGb: ptr(gb), QuotaSource: src, OverQuota: bytes > 0 && used.Bytes >= bytes,
		QuotaAction: ptr(DvrUsageQuotaAction(cfg.Integrations.DVRQuotaAction)),
		CanRecord:   ptr(canRecord(auth.Session{User: u})), CanView: ptr(canViewDVR(auth.Session{User: u}))}
	return out
}

func (h *Handlers) GetRecordingUsage(ctx context.Context, _ GetRecordingUsageRequestObject) (GetRecordingUsageResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return GetRecordingUsage401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	if !(canRecord(s) || canViewDVR(s)) || h.DVR == nil {
		return GetRecordingUsage403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	used, err := h.DVR.UserUsage(ctx, s.User.ID)
	if err != nil {
		return nil, internal(ctx, "getRecordingUsage", err)
	}
	return GetRecordingUsage200JSONResponse(h.dvrUsage(s.User, used)), nil
}

func (h *Handlers) ListRecordingUsage(ctx context.Context, _ ListRecordingUsageRequestObject) (ListRecordingUsageResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return ListRecordingUsage401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return ListRecordingUsage403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	users, err := h.Auth.ListUsers(ctx)
	if err != nil {
		return nil, internal(ctx, "listRecordingUsage", err)
	}
	all := map[int64]livetv.Usage{}
	if h.DVR != nil {
		if all, err = h.DVR.AllUsage(ctx); err != nil {
			return nil, internal(ctx, "listRecordingUsage", err)
		}
	}
	out := make(ListRecordingUsage200JSONResponse, len(users))
	for i, u := range users {
		out[i] = h.dvrUsage(u, all[u.ID])
	}
	return out, nil
}

// fillDVRFolder adds the configured default recordings folder, and whether a library
// covers the folder recordings go in, to the settings sent back.
func (h *Handlers) fillDVRFolder(ctx context.Context, out *ServerSettings) {
	if out.Integrations == nil || h.DVR == nil {
		return
	}
	out.Integrations.DvrFolderDefault = ptr(h.DVR.Dir)
	if h.Libraries == nil {
		return
	}
	libs, err := h.Libraries.List(ctx)
	if err != nil {
		return
	}
	dir := h.DVR.NewFolder()
	covered := vodsave.InLibrary(libs, filepath.Join(dir, "TV"))
	for _, l := range libs {
		// Recorded Movies, once there is one, must cover it too.
		if l.Recordings && l.Type == library.Movies && !vodsave.InLibrary(libs, filepath.Join(dir, "Movies")) {
			covered = false
		}
	}
	out.Integrations.DvrFolderInLibrary = ptr(covered)
}

// coverDVRFolder points the recordings libraries at a newly chosen recordings folder (the
// Recorded TV library always; Recorded Movies when there is one), so new recordings appear
// in the library. Recordings already made stay where they are, and stay in the library.
func (h *Handlers) coverDVRFolder(ctx context.Context) {
	if h.DVR == nil || h.DVR.Library == nil || !h.DVR.Available() {
		return
	}
	dir := h.DVR.NewFolder()
	if _, err := h.DVR.Library(ctx, false, filepath.Join(dir, "TV")); err != nil {
		slog.WarnContext(ctx, "dvr: recordings library for the new folder", "err", err)
	}
	if h.Libraries == nil {
		return
	}
	libs, _ := h.Libraries.List(ctx)
	for _, l := range libs {
		if l.Recordings && l.Type == library.Movies {
			if _, err := h.DVR.Library(ctx, true, filepath.Join(dir, "Movies")); err != nil {
				slog.WarnContext(ctx, "dvr: recordings library for the new folder", "err", err)
			}
			break
		}
	}
}
