package api

import (
	"context"
	"errors"
	"strconv"
	"strings"

	"marquee/internal/optimize"
	"marquee/internal/settings"
)

// The library optimizer (Library Health → Large files, admins only).

// optimizer is the optimizer service, or one that can list and queue (tests).
func (h *Handlers) optimizer() *optimize.Service {
	if h.Optimize != nil {
		return h.Optimize
	}
	return &optimize.Service{DB: h.DB, Settings: h.Settings.Get}
}

// adminOnly is 401 or 403 for callers who aren't admins (0 for admins).
func adminOnly(ctx context.Context) int {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return 401
	case !admin:
		return 403
	}
	return 0
}

func optimizeFilter(libraryID *int64, typ *string, minGB, minMbps *float32, res *int, codecs []string, hdr *string) optimize.Filter {
	var f optimize.Filter
	if libraryID != nil {
		f.LibraryID = *libraryID
	}
	if typ != nil {
		f.Type = *typ
	}
	if minGB != nil && *minGB > 0 {
		f.MinSizeBytes = int64(float64(*minGB) * 1e9)
	}
	if minMbps != nil && *minMbps > 0 {
		f.MinBitrateKbps = int(float64(*minMbps) * 1000)
	}
	if res != nil {
		f.Resolution = *res
	}
	for _, c := range codecs {
		if c = strings.TrimSpace(c); c != "" {
			f.Codecs = append(f.Codecs, c)
		}
	}
	if hdr != nil {
		b := *hdr == "yes"
		f.HDR = &b
	}
	return f
}

func (h *Handlers) ListLargeFiles(ctx context.Context, req ListLargeFilesRequestObject) (ListLargeFilesResponseObject, error) {
	switch adminOnly(ctx) {
	case 401:
		return ListLargeFiles401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case 403:
		return ListLargeFiles403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	p := req.Params
	cfg := h.Settings.Get().Optimize
	presetID := cfg.DefaultPreset
	if p.PresetId != nil && *p.PresetId != "" {
		presetID = *p.PresetId
	}
	preset, ok := cfg.Preset(presetID)
	if !ok {
		return ListLargeFiles400JSONResponse{BadRequestJSONResponse(apiErr("invalid", "no such preset"))}, nil
	}
	var codecs []string
	if p.Codecs != nil {
		codecs = strings.Split(*p.Codecs, ",")
	}
	f := optimizeFilter(p.LibraryId, (*string)(p.Type), p.MinSizeGb, p.MinBitrateMbps, (*int)(p.Resolution), codecs, (*string)(p.Hdr))
	if p.Sort != nil {
		f.Sort = string(*p.Sort)
	}
	offset, limit := 0, 50
	if p.Offset != nil {
		offset = *p.Offset
	}
	if p.Limit != nil {
		limit = *p.Limit
	}
	page, err := h.optimizer().List(ctx, f, preset, offset, limit)
	if err != nil {
		return nil, internal(ctx, "listLargeFiles", err)
	}
	out := LargeFilePage{Items: make([]LargeFile, len(page.Files)), Total: page.Total, TotalSize: page.TotalSize, EstimatedSize: page.TotalEstimate}
	for i, lf := range page.Files {
		out.Items[i] = LargeFile{FileId: lf.FileID, ItemId: lf.ItemID, Title: lf.Title, Subtitle: nz(lf.Subtitle), Type: LargeFileType(lf.Type),
			Path: lf.Path, Size: lf.Size, Width: lf.Width, Height: lf.Height, VideoCodec: lf.VideoCodec, Hdr: lf.HDR,
			BitrateKbps: lf.BitrateKbps, DurationMs: lf.DurationMS, EstimatedSize: lf.Estimate, JobId: nz(lf.JobID)}
		if lf.JobState != "" {
			out.Items[i].JobState = ptr(OptimizeJobState(lf.JobState))
		}
	}
	return ListLargeFiles200JSONResponse(out), nil
}

func (h *Handlers) QueueOptimizeJobs(ctx context.Context, req QueueOptimizeJobsRequestObject) (QueueOptimizeJobsResponseObject, error) {
	switch adminOnly(ctx) {
	case 401:
		return QueueOptimizeJobs401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case 403:
		return QueueOptimizeJobs403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	b := req.Body
	if b == nil {
		return QueueOptimizeJobs400JSONResponse{BadRequestJSONResponse(apiErr("invalid", "a preset and files or a filter are needed"))}, nil
	}
	svc := h.optimizer()
	var ids []int64
	switch {
	case b.FileIds != nil && len(*b.FileIds) > 0:
		ids = *b.FileIds
	case b.Filter != nil:
		fl := b.Filter
		var codecs []string
		if fl.Codecs != nil {
			codecs = *fl.Codecs
		}
		var err error
		ids, err = svc.Matching(ctx, optimizeFilter(fl.LibraryId, (*string)(fl.Type), fl.MinSizeGb, fl.MinBitrateMbps, (*int)(fl.Resolution), codecs, (*string)(fl.Hdr)))
		if err != nil {
			return nil, internal(ctx, "queueOptimizeJobs", err)
		}
	default:
		return QueueOptimizeJobs400JSONResponse{BadRequestJSONResponse(apiErr("invalid", "choose files or a filter"))}, nil
	}
	queued, skipped, err := svc.Enqueue(ctx, ids, b.PresetId)
	var ve *settings.ValidationError
	switch {
	case errors.Is(err, optimize.ErrNoPreset):
		return QueueOptimizeJobs400JSONResponse{BadRequestJSONResponse(apiErr("invalid", "no such preset"))}, nil
	case errors.As(err, &ve):
		return QueueOptimizeJobs400JSONResponse{BadRequestJSONResponse(apiErr("invalid", ve.Msg))}, nil
	case err != nil:
		return nil, internal(ctx, "queueOptimizeJobs", err)
	}
	return QueueOptimizeJobs202JSONResponse{Queued: queued, Skipped: skipped}, nil
}

func (h *Handlers) ListOptimizeJobs(ctx context.Context, req ListOptimizeJobsRequestObject) (ListOptimizeJobsResponseObject, error) {
	switch adminOnly(ctx) {
	case 401:
		return ListOptimizeJobs401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case 403:
		return ListOptimizeJobs403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	p := req.Params
	offset, limit := 0, 50
	if p.Offset != nil {
		offset = *p.Offset
	}
	if p.Limit != nil {
		limit = *p.Limit
	}
	svc := h.optimizer()
	page, err := svc.Jobs(ctx, p.View != nil && *p.View == "history", offset, limit)
	if err != nil {
		return nil, internal(ctx, "listOptimizeJobs", err)
	}
	out := OptimizeJobPage{Items: make([]OptimizeJob, len(page.Jobs)), Total: page.Total, SavedBytes: page.SavedBytes, Optimized: page.Optimized,
		Paused: nz(svc.HoldReason())}
	for i, j := range page.Jobs {
		out.Items[i] = OptimizeJob{Id: j.ID, FileId: nz(j.FileID), ItemId: nz(j.ItemID), Title: j.Title, Path: j.Path, PresetId: j.PresetID,
			PresetName: j.PresetName, State: OptimizeJobState(j.State), Progress: float32(j.Progress), Fps: nz(float32(j.FPS)), Speed: nz(float32(j.Speed)),
			EtaSeconds: nz(j.ETASeconds), Encoder: nz(j.Encoder), BeforeSize: j.BeforeSize, AfterSize: j.AfterSize, Error: nz(j.Error),
			Paused: nz(j.Paused), CanRestore: j.CanRestore, CreatedAt: j.CreatedAt, StartedAt: j.StartedAt, FinishedAt: j.FinishedAt}
	}
	return ListOptimizeJobs200JSONResponse(out), nil
}

func (h *Handlers) ClearOptimizeJobs(ctx context.Context, _ ClearOptimizeJobsRequestObject) (ClearOptimizeJobsResponseObject, error) {
	switch adminOnly(ctx) {
	case 401:
		return ClearOptimizeJobs401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case 403:
		return ClearOptimizeJobs403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	n, err := h.optimizer().ClearFinished(ctx)
	if err != nil {
		return nil, internal(ctx, "clearOptimizeJobs", err)
	}
	return ClearOptimizeJobs200JSONResponse{Cleared: n}, nil
}

func (h *Handlers) OptimizeJobAction(ctx context.Context, req OptimizeJobActionRequestObject) (OptimizeJobActionResponseObject, error) {
	switch adminOnly(ctx) {
	case 401:
		return OptimizeJobAction401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case 403:
		return OptimizeJobAction403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	svc := h.optimizer()
	var err error
	switch req.Action {
	case "cancel":
		err = svc.Cancel(ctx, req.JobId)
	case "retry":
		err = svc.Retry(ctx, req.JobId)
	case "restore":
		err = svc.Restore(ctx, req.JobId)
	default:
		return OptimizeJobAction404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "no such action"))}, nil
	}
	switch {
	case errors.Is(err, optimize.ErrNotFound):
		return OptimizeJobAction404JSONResponse{NotFoundJSONResponse(apiErr("not_found", err.Error()))}, nil
	case errors.Is(err, optimize.ErrBadState):
		return OptimizeJobAction409JSONResponse{ConflictJSONResponse(apiErr("bad_state", "the job can't be "+map[string]string{
			"cancel": "cancelled", "retry": "retried", "restore": "restored"}[req.Action]+" now"))}, nil
	case errors.Is(err, optimize.ErrCantRestore):
		return OptimizeJobAction409JSONResponse{ConflictJSONResponse(apiErr("cant_restore", err.Error()))}, nil
	case errors.Is(err, optimize.ErrBusy):
		return OptimizeJobAction409JSONResponse{ConflictJSONResponse(apiErr("busy", err.Error()))}, nil
	case err != nil:
		if req.Action == "restore" {
			return OptimizeJobAction409JSONResponse{ConflictJSONResponse(apiErr("restore_failed", err.Error()))}, nil
		}
		return nil, internal(ctx, "optimizeJobAction", err)
	}
	return OptimizeJobAction204Response{}, nil
}

func toAPIOptimizePreset(p settings.OptimizePreset) OptimizePreset {
	return OptimizePreset{Id: p.ID, Name: p.Name, BuiltIn: ptr(p.BuiltIn), MaxHeight: OptimizePresetMaxHeight(p.MaxHeight), Codec: OptimizePresetCodec(p.Codec),
		Quality: OptimizePresetQuality(p.Quality), VideoKbps: nz(p.VideoKbps), Audio: OptimizePresetAudio(p.Audio), DropCommentary: ptr(p.DropCommentary),
		ImageSubtitles: OptimizePresetImageSubtitles(p.ImageSubtitles), Hdr: OptimizePresetHdr(p.HDR)}
}

func fromAPIOptimizePreset(p OptimizePreset) settings.OptimizePreset {
	out := settings.OptimizePreset{ID: strings.TrimSpace(p.Id), Name: strings.TrimSpace(p.Name), MaxHeight: int(p.MaxHeight), Codec: string(p.Codec),
		Quality: string(p.Quality), Audio: string(p.Audio), ImageSubtitles: string(p.ImageSubtitles), HDR: string(p.Hdr)}
	if p.VideoKbps != nil {
		out.VideoKbps = *p.VideoKbps
	}
	if p.DropCommentary != nil {
		out.DropCommentary = *p.DropCommentary
	}
	return out
}

func (h *Handlers) optimizeConfig(o settings.Optimize) OptimizeConfig {
	out := OptimizeConfig{Settings: OptimizeSettings{Presets: []OptimizePreset{}, DefaultPreset: o.DefaultPreset, Concurrency: o.Concurrency,
		MinSavingsPercent: o.MinSavingsPercent, PauseWhileWatching: o.PauseWhileWatching, WindowOnly: o.WindowOnly, Paused: o.Paused},
		Presets: []OptimizePreset{}, Av1Available: h.optimizer().AV1Available()}
	for _, p := range o.Presets {
		out.Settings.Presets = append(out.Settings.Presets, toAPIOptimizePreset(p))
	}
	for _, p := range o.AllPresets() {
		out.Presets = append(out.Presets, toAPIOptimizePreset(p))
	}
	return out
}

func (h *Handlers) GetOptimizeSettings(ctx context.Context, _ GetOptimizeSettingsRequestObject) (GetOptimizeSettingsResponseObject, error) {
	switch adminOnly(ctx) {
	case 401:
		return GetOptimizeSettings401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case 403:
		return GetOptimizeSettings403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	return GetOptimizeSettings200JSONResponse(h.optimizeConfig(h.Settings.Get().Optimize)), nil
}

func (h *Handlers) UpdateOptimizeSettings(ctx context.Context, req UpdateOptimizeSettingsRequestObject) (UpdateOptimizeSettingsResponseObject, error) {
	switch adminOnly(ctx) {
	case 401:
		return UpdateOptimizeSettings401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case 403:
		return UpdateOptimizeSettings403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	b := req.Body
	if b == nil {
		return UpdateOptimizeSettings400JSONResponse{BadRequestJSONResponse(apiErr("invalid", "settings are needed"))}, nil
	}
	av1 := h.optimizer().AV1Available()
	next, err := h.Settings.Update(ctx, func(s *settings.Settings) error {
		o := settings.Optimize{DefaultPreset: b.DefaultPreset, Concurrency: b.Concurrency, MinSavingsPercent: b.MinSavingsPercent,
			PauseWhileWatching: b.PauseWhileWatching, WindowOnly: b.WindowOnly, Paused: b.Paused, Presets: []settings.OptimizePreset{}}
		for _, p := range b.Presets {
			if p.BuiltIn != nil && *p.BuiltIn {
				continue
			}
			cp := fromAPIOptimizePreset(p)
			if cp.Codec == "av1" && !av1 && !presetHasCodec(s.Optimize.Presets, cp.ID, "av1") {
				return &settings.ValidationError{Msg: "this server has no AV1 encoder"}
			}
			o.Presets = append(o.Presets, cp)
		}
		s.Optimize = o
		return nil
	})
	var ve *settings.ValidationError
	if errors.As(err, &ve) {
		return UpdateOptimizeSettings400JSONResponse{BadRequestJSONResponse(apiErr("invalid", ve.Msg))}, nil
	}
	if err != nil {
		return nil, internal(ctx, "updateOptimizeSettings", err)
	}
	return UpdateOptimizeSettings200JSONResponse(h.optimizeConfig(next.Optimize)), nil
}

// presetHasCodec reports whether a stored preset already uses a codec (kept as it is).
func presetHasCodec(ps []settings.OptimizePreset, id, codec string) bool {
	for _, p := range ps {
		if p.ID == id && p.Codec == codec {
			return true
		}
	}
	return false
}

// optimizeActivity lists the optimizer's running jobs for the activity monitor.
func (h *Handlers) optimizeActivity(ctx context.Context) []ActivityTask {
	if h.Optimize == nil {
		return nil
	}
	var out []ActivityTask
	for _, a := range h.Optimize.Active(ctx) {
		title := "Optimizing " + a.Title
		switch {
		case a.Paused != "":
			title += " (" + strings.ToLower(a.Paused[:1]) + a.Paused[1:] + ")"
		case a.State == optimize.StateVerifying:
			title = "Checking the optimized " + a.Title
		}
		out = append(out, ActivityTask{Id: "optimize:" + strconv.FormatInt(a.ID, 10), Kind: ActivityTaskKindOptimize, State: ActivityTaskStateRunning, Title: title,
			Progress: &ScanProgress{Phase: Probing, Done: int(a.Fraction * 1000), Total: 1000}})
	}
	return out
}
