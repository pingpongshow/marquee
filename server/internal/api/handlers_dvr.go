package api

import (
	"context"
	"errors"

	"marquee/internal/auth"
	"marquee/internal/livetv"
)

// DVR (LIVE-5, D76).

func canRecord(s auth.Session) bool { return s.User.IsAdmin || s.User.Restrictions.CanRecord }

func toAPIRecording(x livetv.Recording) Recording {
	status := RecordingStatus(x.Status)
	return Recording{Id: x.ID, ChannelId: x.ChannelID, ChannelName: x.ChannelName, Start: x.Start, End: x.Stop, Title: x.Title,
		Subtitle: nz(x.Subtitle), Description: nz(x.Description), Episode: nz(x.Episode), Category: nz(x.Category), ImageUrl: nz(x.Image),
		Status: status, Series: x.RuleID != nil, RuleId: x.RuleID, Error: nz(x.Error), SizeBytes: nz(x.Size), ItemId: x.ItemID, UserId: &x.UserID}
}

func toAPIRule(r livetv.Rule) RecordingRule {
	return RecordingRule{Id: r.ID, Title: r.Title, ChannelId: r.ChannelID, ChannelName: nz(r.ChannelName), Upcoming: r.Upcoming,
		UserId: &r.UserID, CreatedAt: r.CreatedAt}
}

func (h *Handlers) ListRecordings(ctx context.Context, _ ListRecordingsRequestObject) (ListRecordingsResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return ListRecordings401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	if !canRecord(s) || h.DVR == nil {
		return ListRecordings403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	list, err := h.DVR.List(ctx)
	if err != nil {
		return nil, internal(ctx, "listRecordings", err)
	}
	out := make(ListRecordings200JSONResponse, len(list))
	for i, x := range list {
		out[i] = toAPIRecording(x)
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
	if c, err := h.LiveTV.Channel(ctx, b.ChannelId); err == nil && !liveAllowed(s, c.Group) {
		return ScheduleRecording403JSONResponse{ForbiddenJSONResponse(apiErr("not_allowed", "This channel isn't available for this profile."))}, nil
	}
	rec, rule, err := h.DVR.Schedule(ctx, s.User.ID, b.ChannelId, b.Start, b.Series != nil && *b.Series, b.AnyChannel != nil && *b.AnyChannel)
	if errors.Is(err, livetv.ErrProgrammeNotFound) {
		return ScheduleRecording404JSONResponse{NotFoundJSONResponse(apiErr("not_found", err.Error()))}, nil
	}
	if err != nil {
		return nil, internal(ctx, "scheduleRecording", err)
	}
	var out ScheduleRecording201JSONResponse
	if rec != nil {
		r := toAPIRecording(*rec)
		out.Recording = &r
	}
	if rule != nil {
		r := toAPIRule(*rule)
		out.Rule = &r
	}
	return out, nil
}

func (h *Handlers) CancelRecording(ctx context.Context, req CancelRecordingRequestObject) (CancelRecordingResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return CancelRecording401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	if !canRecord(s) || h.DVR == nil {
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
	// Anyone who may record can cancel what's coming up (a shared household DVR); deleting a
	// recording's file is for admins and whoever scheduled it.
	if deleteFile && !s.User.IsAdmin && x.UserID != s.User.ID {
		return CancelRecording403JSONResponse{ForbiddenJSONResponse(apiErr("forbidden", "only an admin or whoever scheduled it can delete a recording"))}, nil
	}
	if err := h.DVR.Cancel(ctx, x.ID, deleteFile); err != nil {
		return nil, internal(ctx, "cancelRecording", err)
	}
	return CancelRecording204Response{}, nil
}

func (h *Handlers) ListRecordingRules(ctx context.Context, _ ListRecordingRulesRequestObject) (ListRecordingRulesResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return ListRecordingRules401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	if !canRecord(s) || h.DVR == nil {
		return ListRecordingRules403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	rules, err := h.DVR.Rules(ctx)
	if err != nil {
		return nil, internal(ctx, "listRecordingRules", err)
	}
	out := make(ListRecordingRules200JSONResponse, len(rules))
	for i, r := range rules {
		out[i] = toAPIRule(r)
	}
	return out, nil
}

func (h *Handlers) DeleteRecordingRule(ctx context.Context, req DeleteRecordingRuleRequestObject) (DeleteRecordingRuleResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return DeleteRecordingRule401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	if !canRecord(s) || h.DVR == nil {
		return DeleteRecordingRule403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	err := h.DVR.DeleteRule(ctx, req.RuleId)
	if errors.Is(err, livetv.ErrRecordingNotFound) {
		return DeleteRecordingRule404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "series not found"))}, nil
	}
	if err != nil {
		return nil, internal(ctx, "deleteRecordingRule", err)
	}
	return DeleteRecordingRule204Response{}, nil
}
