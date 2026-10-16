package api

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"marquee/internal/library"
	"marquee/internal/settings"
	"marquee/internal/vodsave"
)

// Saving films and episodes on demand into the library (admins only).

var errVodSaveUnavailable = apiErr("unavailable", "Saving to the library isn't available on this server.")

// vodSaveAdmin reports whether the caller is signed in and an administrator, and whether
// saving is available.
func (h *Handlers) vodSaveAdmin(ctx context.Context) (authed, admin, available bool) {
	authed, admin = isAdmin(ctx)
	return authed, admin, h.VODSaves != nil && h.VOD != nil
}

func toAPIVodSaveSettings(v settings.VODSave) *VodSaveSettings {
	return &VodSaveSettings{
		MoviesFolder: ptr(v.MoviesFolder), ShowsFolder: ptr(v.ShowsFolder), Quality: ptr(VodSaveSettingsQuality(v.Quality)),
		MaxHeight: ptr(VodSaveSettingsMaxHeight(v.MaxHeight)), VideoCodec: ptr(VodSaveSettingsVideoCodec(v.VideoCodec)),
		VideoQuality: ptr(VodSaveSettingsVideoQuality(v.VideoQuality)), VideoKbps: ptr(v.VideoKbps),
		Audio: ptr(VodSaveSettingsAudio(v.Audio)), ConcurrentPerSource: ptr(v.ConcurrentPerSource),
		ScanAfterSave: ptr(v.ScanAfterSave), SkipExisting: ptr(v.SkipExisting), PauseWhileWatching: ptr(v.PauseWhileWatching),
		PauseScope: ptr(VodSaveSettingsPauseScope(v.PauseScope)), RetryFailed: ptr(v.RetryFailed),
		Destinations: ptr(toAPIVodSaveDestinations(v.Destinations, nil)),
	}
}

// toAPIVodSaveDestinations lists the save folders; with libs, each says whether a library
// holds it.
func toAPIVodSaveDestinations(ds []settings.VODSaveDestination, libs []library.Library) []VodSaveDestination {
	out := make([]VodSaveDestination, len(ds))
	for i, d := range ds {
		out[i] = VodSaveDestination{Id: ptr(d.ID), Name: d.Name, Folder: d.Folder, Kind: VodSaveDestinationKind(d.Kind)}
		if libs != nil {
			out[i].InLibrary = ptr(vodsave.InLibrary(libs, d.Folder))
		}
	}
	return out
}

// fillVodSaveCoverage says, for each save folder in the settings sent back, whether a
// library holds it.
func (h *Handlers) fillVodSaveCoverage(ctx context.Context, out *ServerSettings, s settings.Settings) {
	if out.VodSave == nil || h.Libraries == nil || len(s.VODSave.Destinations) == 0 {
		return
	}
	libs, err := h.Libraries.List(ctx)
	if err != nil {
		return
	}
	if libs == nil {
		libs = []library.Library{}
	}
	out.VodSave.Destinations = ptr(toAPIVodSaveDestinations(s.VODSave.Destinations, libs))
}

// fromAPIVodSaveDestinations is the list sent, keeping ids (new ones get one).
func fromAPIVodSaveDestinations(ds []VodSaveDestination) []settings.VODSaveDestination {
	out := make([]settings.VODSaveDestination, len(ds))
	for i, d := range ds {
		id := ""
		if d.Id != nil {
			id = strings.TrimSpace(*d.Id)
		}
		if id == "" {
			id = newWebhookID()
		}
		out[i] = settings.VODSaveDestination{ID: id, Name: strings.TrimSpace(d.Name), Folder: cleanFolder(d.Folder), Kind: string(d.Kind)}
	}
	return out
}

// checkSaveDestinations checks the save folders that are new or moved exist and are
// writable.
func checkSaveDestinations(before, after []settings.VODSaveDestination) error {
	old := map[string]string{}
	for _, d := range before {
		old[d.ID] = d.Folder
	}
	for _, d := range after {
		if f, ok := old[d.ID]; d.Folder != "" && filepath.IsAbs(d.Folder) && (!ok || f != d.Folder) {
			if err := checkSaveFolder(d.Name, d.Folder); err != nil {
				return err
			}
		}
	}
	return nil
}

// saveDestinationRefs are the save folders a kind (movie, episode) can go to, for a page.
func (h *Handlers) saveDestinationRefs(kind string) []VodSaveDestinationRef {
	ds := h.VODSaves.Destinations(kind)
	out := make([]VodSaveDestinationRef, len(ds))
	for i, d := range ds {
		out[i] = VodSaveDestinationRef{Id: d.ID, Name: d.Name}
	}
	return out
}

func applyVodSaveSettings(v *settings.VODSave, u VodSaveSettings) {
	if u.MoviesFolder != nil {
		v.MoviesFolder = cleanFolder(*u.MoviesFolder)
	}
	if u.ShowsFolder != nil {
		v.ShowsFolder = cleanFolder(*u.ShowsFolder)
	}
	if u.Quality != nil {
		v.Quality = string(*u.Quality)
	}
	if u.MaxHeight != nil {
		v.MaxHeight = int(*u.MaxHeight)
	}
	if u.VideoCodec != nil {
		v.VideoCodec = string(*u.VideoCodec)
	}
	if u.VideoQuality != nil {
		v.VideoQuality = string(*u.VideoQuality)
	}
	if u.Audio != nil {
		v.Audio = string(*u.Audio)
	}
	if u.PauseScope != nil {
		v.PauseScope = string(*u.PauseScope)
	}
	set(&v.VideoKbps, u.VideoKbps)
	set(&v.ConcurrentPerSource, u.ConcurrentPerSource)
	set(&v.ScanAfterSave, u.ScanAfterSave)
	set(&v.SkipExisting, u.SkipExisting)
	set(&v.PauseWhileWatching, u.PauseWhileWatching)
	set(&v.RetryFailed, u.RetryFailed)
	if u.Destinations != nil {
		v.Destinations = fromAPIVodSaveDestinations(*u.Destinations)
	}
}

func cleanFolder(p string) string {
	if p = strings.TrimSpace(p); p == "" {
		return ""
	}
	return filepath.Clean(p)
}

// checkSaveFolder is a folder to save to that exists and the server can write in.
func checkSaveFolder(kind, dir string) error {
	st, err := os.Stat(dir)
	switch {
	case err != nil:
		return &settings.ValidationError{Msg: "the " + kind + " folder " + dir + " doesn't exist (as the server sees it)"}
	case !st.IsDir():
		return &settings.ValidationError{Msg: "the " + kind + " folder " + dir + " isn't a folder"}
	}
	f, err := os.CreateTemp(dir, ".marquee-write-check-*")
	if err != nil {
		return &settings.ValidationError{Msg: "the server can't write in the " + kind + " folder " + dir}
	}
	f.Close()
	os.Remove(f.Name())
	return nil
}

func toAPIVodSaveState(st vodsave.ItemState) *VodSaveState {
	if st.ID == 0 {
		return nil
	}
	return &VodSaveState{Id: st.ID, State: VodSaveStateName(st.State), Progress: float32(st.Progress), Verified: st.Verified, Error: nz(st.Error)}
}

func toAPIVodSave(v vodsave.Save) VodSave {
	out := VodSave{Id: v.ID, SourceId: v.SourceID, SourceName: nz(v.SourceName), Kind: VodSaveKind(v.Kind), Title: v.Title,
		TargetPath: v.TargetPath, Quality: VodSaveQuality(v.Quality), State: VodSaveStateName(v.State), BytesDone: v.BytesDone,
		BytesTotal: v.BytesTotal, Progress: float32(v.Progress), Attempts: v.Attempts, Verified: v.Verified,
		VerifyNote: nz(v.VerifyNote), Sha256: nz(v.SHA256), Error: nz(v.Error), PauseReason: nz(v.PauseReason),
		CreatedAt: v.CreatedAt, StartedAt: v.StartedAt, FinishedAt: v.FinishedAt, NextAttemptAt: v.NextAttemptAt,
		EtaSeconds: nz(v.ETASeconds), ReplacedPath: nz(v.ReplacedPath), DestinationName: nz(v.DestinationName),
		DestinationFolder: nz(v.DestinationFolder)}
	if v.NoLibrary {
		out.NoLibrary = ptr(true)
	}
	if v.ReplaceFileID > 0 {
		out.Upgrade = ptr(true)
	}
	if v.SpeedBps > 0 {
		out.SpeedBps = ptr(float32(v.SpeedBps))
	}
	if v.Kind == "movie" {
		out.MovieId = ptr(v.VODID)
	} else {
		out.EpisodeId, out.SeriesId = ptr(v.VODID), nz(v.SeriesID)
	}
	return out
}

func toAPIVodSaveResult(r vodsave.Result) VodSaveResult {
	out := VodSaveResult{Queued: r.Queued, Already: r.Already, Skipped: r.Skipped, Upgrades: nz(r.Upgrades), Saves: make([]VodSave, len(r.Saves))}
	for i, v := range r.Saves {
		out.Saves[i] = toAPIVodSave(v)
	}
	return out
}

// inLibrary is the 409 for a title the library already has: in_library when its copy is
// as good or better, upgrade_available when this one is better and no upgrade was asked for.
func inLibrary(err error) (Error, bool) {
	var e *vodsave.InLibraryError
	if !errors.As(err, &e) {
		return Error{}, false
	}
	code := "in_library"
	if e.Match.Verdict == vodsave.VerdictBetter {
		code = "upgrade_available"
	}
	return apiErr(code, e.Error()), true
}

func toAPIVodQuality(q vodsave.Quality) *VodQuality {
	if !q.Known() && q.BitrateKbps == 0 {
		return nil
	}
	return &VodQuality{Width: nz(q.Width), Height: nz(q.Height), Resolution: nz(q.Tier()), Codec: nz(q.Codec), Hdr: nz(q.HDR),
		BitrateKbps: nz(q.BitrateKbps), Summary: q.Summary()}
}

func toAPIVodLibraryMatch(m vodsave.Match) *VodLibraryMatch {
	out := &VodLibraryMatch{Verdict: VodLibraryMatchVerdict(m.Verdict), Reason: m.Reason, Quality: toAPIVodQuality(m.VOD)}
	if m.Pending {
		out.Pending = ptr(true)
	}
	if m.Library != nil {
		out.LibraryItemId, out.LibraryQuality = ptr(m.Library.ItemID), toAPIVodQuality(m.Library.Quality)
		if out.LibraryQuality == nil {
			out.LibraryQuality = &VodQuality{Summary: ""}
		}
	}
	return out
}

func toAPIVodRollup(r vodsave.Rollup) *VodLibraryRollup {
	return &VodLibraryRollup{None: r.None, Better: r.Better, Same: r.Same, Worse: r.Worse, Pending: nz(r.Pending)}
}

// vodSaveErr maps a save request's error to a 400/404 message ("" when it is internal).
func vodSaveErr(err error) (code, msg string) {
	switch {
	case errors.Is(err, vodsave.ErrNotFound):
		return "not_found", "not found"
	case errors.Is(err, vodsave.ErrNoFolder):
		return "no_folder", err.Error()
	case errors.Is(err, vodsave.ErrNotSavable):
		return "not_savable", err.Error()
	case errors.Is(err, vodsave.ErrDestination):
		return "bad_destination", err.Error()
	}
	return "", ""
}

func (h *Handlers) SaveVodMovie(ctx context.Context, req SaveVodMovieRequestObject) (SaveVodMovieResponseObject, error) {
	s, _ := session(ctx)
	switch authed, admin, ok := h.vodSaveAdmin(ctx); {
	case !authed:
		return SaveVodMovie401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return SaveVodMovie403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	case !ok:
		return SaveVodMovie503JSONResponse{ServiceUnavailableJSONResponse(errVodSaveUnavailable)}, nil
	}
	r, err := h.VODSaves.SaveMovieTo(ctx, req.MovieId, s.User.ID, req.Params.Upgrade != nil && *req.Params.Upgrade, strings.TrimSpace(nzs(req.Params.Destination)))
	if e, ok := inLibrary(err); ok {
		return SaveVodMovie409JSONResponse{ConflictJSONResponse(e)}, nil
	}
	switch code, msg := vodSaveErr(err); {
	case code == "not_found":
		return SaveVodMovie404JSONResponse{NotFoundJSONResponse(errVodNotFound)}, nil
	case code != "":
		return SaveVodMovie400JSONResponse{BadRequestJSONResponse(apiErr(code, msg))}, nil
	case err != nil:
		return nil, internal(ctx, "saveVodMovie", err)
	}
	return SaveVodMovie200JSONResponse(toAPIVodSaveResult(r)), nil
}

func (h *Handlers) SaveVodEpisode(ctx context.Context, req SaveVodEpisodeRequestObject) (SaveVodEpisodeResponseObject, error) {
	s, _ := session(ctx)
	switch authed, admin, ok := h.vodSaveAdmin(ctx); {
	case !authed:
		return SaveVodEpisode401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return SaveVodEpisode403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	case !ok:
		return SaveVodEpisode503JSONResponse{ServiceUnavailableJSONResponse(errVodSaveUnavailable)}, nil
	}
	r, err := h.VODSaves.SaveEpisodeTo(ctx, req.EpisodeId, s.User.ID, req.Params.Upgrade != nil && *req.Params.Upgrade, strings.TrimSpace(nzs(req.Params.Destination)))
	if e, ok := inLibrary(err); ok {
		return SaveVodEpisode409JSONResponse{ConflictJSONResponse(e)}, nil
	}
	switch code, msg := vodSaveErr(err); {
	case code == "not_found":
		return SaveVodEpisode404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "episode not found"))}, nil
	case code != "":
		return SaveVodEpisode400JSONResponse{BadRequestJSONResponse(apiErr(code, msg))}, nil
	case err != nil:
		return nil, internal(ctx, "saveVodEpisode", err)
	}
	return SaveVodEpisode200JSONResponse(toAPIVodSaveResult(r)), nil
}

func (h *Handlers) SaveVodSeries(ctx context.Context, req SaveVodSeriesRequestObject) (SaveVodSeriesResponseObject, error) {
	s, _ := session(ctx)
	switch authed, admin, ok := h.vodSaveAdmin(ctx); {
	case !authed:
		return SaveVodSeries401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return SaveVodSeries403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	case !ok:
		return SaveVodSeries503JSONResponse{ServiceUnavailableJSONResponse(errVodSaveUnavailable)}, nil
	}
	r, err := h.VODSaves.SaveSeriesTo(ctx, req.SeriesId, req.Params.Season, s.User.ID, req.Params.IncludeUpgrades != nil && *req.Params.IncludeUpgrades,
		strings.TrimSpace(nzs(req.Params.Destination)))
	switch code, msg := vodSaveErr(err); {
	case code == "not_found":
		return SaveVodSeries404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "series not found"))}, nil
	case code != "":
		return SaveVodSeries400JSONResponse{BadRequestJSONResponse(apiErr(code, msg))}, nil
	case err != nil:
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		// The source didn't give the episodes (the message is redacted).
		return SaveVodSeries502JSONResponse{BadGatewayJSONResponse(apiErr("source", "The source didn't give the episodes: "+err.Error()))}, nil
	}
	return SaveVodSeries200JSONResponse(toAPIVodSaveResult(r)), nil
}

func (h *Handlers) ListVodSaves(ctx context.Context, _ ListVodSavesRequestObject) (ListVodSavesResponseObject, error) {
	switch authed, admin, ok := h.vodSaveAdmin(ctx); {
	case !authed:
		return ListVodSaves401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return ListVodSaves403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	case !ok:
		return ListVodSaves503JSONResponse{ServiceUnavailableJSONResponse(errVodSaveUnavailable)}, nil
	}
	list, err := h.VODSaves.List(ctx)
	if err != nil {
		return nil, internal(ctx, "listVodSaves", err)
	}
	movies, shows := h.VODSaves.Configured()
	movies = movies || len(h.VODSaves.Destinations("movie")) > 0
	shows = shows || len(h.VODSaves.Destinations("episode")) > 0
	out := VodSaveQueue{Saves: make([]VodSave, len(list)), Active: h.VODSaves.Active(ctx), Paused: h.VODSaves.QueuePaused(ctx),
		MoviesFolderSet: movies, ShowsFolderSet: shows}
	for i, v := range list {
		out.Saves[i] = toAPIVodSave(v)
	}
	return ListVodSaves200JSONResponse(out), nil
}

func (h *Handlers) ClearVodSaves(ctx context.Context, _ ClearVodSavesRequestObject) (ClearVodSavesResponseObject, error) {
	switch authed, admin, ok := h.vodSaveAdmin(ctx); {
	case !authed:
		return ClearVodSaves401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return ClearVodSaves403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	case !ok:
		return ClearVodSaves503JSONResponse{ServiceUnavailableJSONResponse(errVodSaveUnavailable)}, nil
	}
	if err := h.VODSaves.ClearFinished(ctx); err != nil {
		return nil, internal(ctx, "clearVodSaves", err)
	}
	return ClearVodSaves204Response{}, nil
}

func (h *Handlers) PauseVodSaves(ctx context.Context, _ PauseVodSavesRequestObject) (PauseVodSavesResponseObject, error) {
	switch authed, admin, ok := h.vodSaveAdmin(ctx); {
	case !authed:
		return PauseVodSaves401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return PauseVodSaves403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	case !ok:
		return PauseVodSaves503JSONResponse{ServiceUnavailableJSONResponse(errVodSaveUnavailable)}, nil
	}
	if err := h.VODSaves.SetQueuePaused(ctx, true); err != nil {
		return nil, internal(ctx, "pauseVodSaves", err)
	}
	return PauseVodSaves204Response{}, nil
}

func (h *Handlers) ResumeVodSaves(ctx context.Context, _ ResumeVodSavesRequestObject) (ResumeVodSavesResponseObject, error) {
	switch authed, admin, ok := h.vodSaveAdmin(ctx); {
	case !authed:
		return ResumeVodSaves401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return ResumeVodSaves403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	case !ok:
		return ResumeVodSaves503JSONResponse{ServiceUnavailableJSONResponse(errVodSaveUnavailable)}, nil
	}
	if err := h.VODSaves.SetQueuePaused(ctx, false); err != nil {
		return nil, internal(ctx, "resumeVodSaves", err)
	}
	return ResumeVodSaves204Response{}, nil
}

// saveAction runs one action on a save, mapping its errors: 0 = done, else 404 or 409.
func (h *Handlers) saveAction(ctx context.Context, op string, fn func() error) (int, error) {
	err := fn()
	switch {
	case errors.Is(err, vodsave.ErrNotFound):
		return 404, nil
	case errors.Is(err, vodsave.ErrWrongState):
		return 409, nil
	case err != nil:
		return 0, internal(ctx, op, err)
	}
	return 0, nil
}

var (
	errVodSaveNotFound = apiErr("not_found", "save not found")
	errVodSaveState    = apiErr("wrong_state", vodsave.ErrWrongState.Error())
)

func (h *Handlers) CancelVodSave(ctx context.Context, req CancelVodSaveRequestObject) (CancelVodSaveResponseObject, error) {
	switch authed, admin, ok := h.vodSaveAdmin(ctx); {
	case !authed:
		return CancelVodSave401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return CancelVodSave403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	case !ok:
		return CancelVodSave503JSONResponse{ServiceUnavailableJSONResponse(errVodSaveUnavailable)}, nil
	}
	switch code, err := h.saveAction(ctx, "cancelVodSave", func() error { return h.VODSaves.Cancel(ctx, req.SaveId) }); {
	case err != nil:
		return nil, err
	case code == 404:
		return CancelVodSave404JSONResponse{NotFoundJSONResponse(errVodSaveNotFound)}, nil
	case code == 409:
		return CancelVodSave409JSONResponse{ConflictJSONResponse(errVodSaveState)}, nil
	}
	return CancelVodSave204Response{}, nil
}

func (h *Handlers) RetryVodSave(ctx context.Context, req RetryVodSaveRequestObject) (RetryVodSaveResponseObject, error) {
	switch authed, admin, ok := h.vodSaveAdmin(ctx); {
	case !authed:
		return RetryVodSave401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return RetryVodSave403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	case !ok:
		return RetryVodSave503JSONResponse{ServiceUnavailableJSONResponse(errVodSaveUnavailable)}, nil
	}
	switch code, err := h.saveAction(ctx, "retryVodSave", func() error { return h.VODSaves.Retry(ctx, req.SaveId) }); {
	case err != nil:
		return nil, err
	case code == 404:
		return RetryVodSave404JSONResponse{NotFoundJSONResponse(errVodSaveNotFound)}, nil
	case code == 409:
		return RetryVodSave409JSONResponse{ConflictJSONResponse(errVodSaveState)}, nil
	}
	return RetryVodSave204Response{}, nil
}

func (h *Handlers) MoveVodSaveToTop(ctx context.Context, req MoveVodSaveToTopRequestObject) (MoveVodSaveToTopResponseObject, error) {
	switch authed, admin, ok := h.vodSaveAdmin(ctx); {
	case !authed:
		return MoveVodSaveToTop401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return MoveVodSaveToTop403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	case !ok:
		return MoveVodSaveToTop503JSONResponse{ServiceUnavailableJSONResponse(errVodSaveUnavailable)}, nil
	}
	switch code, err := h.saveAction(ctx, "moveVodSaveToTop", func() error { return h.VODSaves.MoveToTop(ctx, req.SaveId) }); {
	case err != nil:
		return nil, err
	case code == 404:
		return MoveVodSaveToTop404JSONResponse{NotFoundJSONResponse(errVodSaveNotFound)}, nil
	case code == 409:
		return MoveVodSaveToTop409JSONResponse{ConflictJSONResponse(errVodSaveState)}, nil
	}
	return MoveVodSaveToTop204Response{}, nil
}

func (h *Handlers) PauseVodSave(ctx context.Context, req PauseVodSaveRequestObject) (PauseVodSaveResponseObject, error) {
	switch authed, admin, ok := h.vodSaveAdmin(ctx); {
	case !authed:
		return PauseVodSave401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return PauseVodSave403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	case !ok:
		return PauseVodSave503JSONResponse{ServiceUnavailableJSONResponse(errVodSaveUnavailable)}, nil
	}
	switch code, err := h.saveAction(ctx, "pauseVodSave", func() error { return h.VODSaves.Pause(ctx, req.SaveId) }); {
	case err != nil:
		return nil, err
	case code == 404:
		return PauseVodSave404JSONResponse{NotFoundJSONResponse(errVodSaveNotFound)}, nil
	case code == 409:
		return PauseVodSave409JSONResponse{ConflictJSONResponse(errVodSaveState)}, nil
	}
	return PauseVodSave204Response{}, nil
}

func (h *Handlers) ResumeVodSave(ctx context.Context, req ResumeVodSaveRequestObject) (ResumeVodSaveResponseObject, error) {
	switch authed, admin, ok := h.vodSaveAdmin(ctx); {
	case !authed:
		return ResumeVodSave401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return ResumeVodSave403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	case !ok:
		return ResumeVodSave503JSONResponse{ServiceUnavailableJSONResponse(errVodSaveUnavailable)}, nil
	}
	switch code, err := h.saveAction(ctx, "resumeVodSave", func() error { return h.VODSaves.Resume(ctx, req.SaveId) }); {
	case err != nil:
		return nil, err
	case code == 404:
		return ResumeVodSave404JSONResponse{NotFoundJSONResponse(errVodSaveNotFound)}, nil
	case code == 409:
		return ResumeVodSave409JSONResponse{ConflictJSONResponse(errVodSaveState)}, nil
	}
	return ResumeVodSave204Response{}, nil
}

// nzs is *p, or "" for nil.
func nzs(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}
