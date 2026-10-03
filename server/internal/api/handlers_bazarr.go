package api

import (
	"context"
	"errors"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"marquee/internal/bazarr"
	"marquee/internal/health"
	"marquee/internal/mediatrash"
)

// ---------- Bazarr subtitles (META-12) ----------

var errBazarrOff = apiErr("not_configured", "Bazarr isn't set up on this server (Settings → Integrations)")

// bazarrCode is a Bazarr language code: ISO 639-1, optionally with a region (pt-BR).
var bazarrCode = regexp.MustCompile(`^[a-zA-Z]{2,3}(-[a-zA-Z]{2,4})?$`)

// bazarrTarget checks the caller may see the item and finds it in Bazarr. The error is the
// API error to send: nf=true for 404 (not set up, not visible, not managed), else 502.
func (h *Handlers) bazarrTarget(ctx context.Context, itemID int64) (t bazarr.Target, apiError *Error, nf bool) {
	if h.Items.Visible(ctx, access(ctx), itemID) != nil {
		e := apiErr("not_found", "item not found")
		return t, &e, true
	}
	if !h.Bazarr.Configured() {
		return t, &errBazarrOff, true
	}
	t, err := h.Bazarr.Resolve(ctx, itemID)
	switch {
	case errors.Is(err, bazarr.ErrNotManaged):
		e := apiErr("not_managed", "Bazarr doesn't manage this title (only movies and episodes from Radarr and Sonarr)")
		return t, &e, true
	case err != nil:
		slog.WarnContext(ctx, "bazarr lookup failed", "item", itemID, "err", err)
		e := apiErr("bazarr", err.Error())
		return t, &e, false
	}
	return t, nil, false
}

func (h *Handlers) BazarrStatus(ctx context.Context, req BazarrStatusRequestObject) (BazarrStatusResponseObject, error) {
	if _, ok := session(ctx); !ok {
		return BazarrStatus401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	if h.Items.Visible(ctx, access(ctx), req.ItemId) != nil {
		return BazarrStatus404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "item not found"))}, nil
	}
	out := BazarrStatus200JSONResponse{Configured: h.Bazarr.Configured(), Languages: []BazarrSubtitleLanguage{}}
	if !out.Configured {
		return out, nil
	}
	t, err := h.Bazarr.Resolve(ctx, req.ItemId)
	if err == nil {
		var have, missing []bazarr.Language
		have, missing, err = h.Bazarr.Languages(ctx, t)
		out.Managed = err == nil
		out.Languages = bazarrLanguages(have, missing)
	}
	if err != nil && !errors.Is(err, bazarr.ErrNotManaged) {
		out.Error = ptr(err.Error())
	}
	return out, nil
}

// bazarrLanguages lists each language once: the ones it has, then the ones it wants.
func bazarrLanguages(have, missing []bazarr.Language) []BazarrSubtitleLanguage {
	out := []BazarrSubtitleLanguage{}
	seen := map[string]bool{}
	add := func(l bazarr.Language, ok bool) {
		key := strings.ToLower(l.Code2) + "|" + boolKey(bool(l.Forced)) + boolKey(bool(l.HI))
		if l.Code2 == "" || seen[key] {
			return
		}
		seen[key] = true
		out = append(out, BazarrSubtitleLanguage{Code2: l.Code2, Code3: nz(l.Code3), Name: l.Name,
			Forced: bool(l.Forced), Hi: bool(l.HI), Have: ok})
	}
	for _, l := range have {
		add(l, true)
	}
	for _, l := range missing {
		add(l, false)
	}
	return out
}

func boolKey(b bool) string {
	if b {
		return "1"
	}
	return "0"
}

func (h *Handlers) BazarrDownload(ctx context.Context, req BazarrDownloadRequestObject) (BazarrDownloadResponseObject, error) {
	if _, ok := session(ctx); !ok {
		return BazarrDownload401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	b := req.Body
	if !bazarrCode.MatchString(b.Language) {
		return BazarrDownload400JSONResponse{BadRequestJSONResponse(apiErr("invalid", "language must be an ISO 639-1 code such as en"))}, nil
	}
	t, e, nf := h.bazarrTarget(ctx, req.ItemId)
	switch {
	case e != nil && nf:
		return BazarrDownload404JSONResponse{NotFoundJSONResponse(*e)}, nil
	case e != nil:
		return BazarrDownload502JSONResponse{BadGatewayJSONResponse(*e)}, nil
	}
	h.Bazarr.Download(t, bazarr.Options{Language: b.Language, Forced: b.Forced != nil && *b.Forced, HI: b.Hi != nil && *b.Hi})
	return BazarrDownload202Response{}, nil
}

func (h *Handlers) BazarrSearch(ctx context.Context, req BazarrSearchRequestObject) (BazarrSearchResponseObject, error) {
	if _, ok := session(ctx); !ok {
		return BazarrSearch401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	t, e, nf := h.bazarrTarget(ctx, req.ItemId)
	switch {
	case e != nil && nf:
		return BazarrSearch404JSONResponse{NotFoundJSONResponse(*e)}, nil
	case e != nil:
		return BazarrSearch502JSONResponse{BadGatewayJSONResponse(*e)}, nil
	}
	res, err := h.Bazarr.Search(ctx, t)
	if err != nil {
		slog.WarnContext(ctx, "bazarr search failed", "item", req.ItemId, "err", err)
		return BazarrSearch502JSONResponse{BadGatewayJSONResponse(apiErr("bazarr", err.Error()))}, nil
	}
	out := make(BazarrSearch200JSONResponse, 0, len(res))
	for _, c := range res {
		out = append(out, BazarrCandidate{Provider: c.Provider, Subtitle: c.Subtitle, Language: c.Language,
			Release: nz(strings.Join(c.ReleaseInfo, ", ")), Score: int(c.Score + 0.5), Hi: ptr(bool(c.HearingImpaired)),
			Forced: ptr(bool(c.Forced)), Uploader: nz(c.Uploader), OriginalFormat: ptr(bool(c.OriginalFormat))})
	}
	return out, nil
}

func (h *Handlers) BazarrPick(ctx context.Context, req BazarrPickRequestObject) (BazarrPickResponseObject, error) {
	if _, ok := session(ctx); !ok {
		return BazarrPick401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	t, e, nf := h.bazarrTarget(ctx, req.ItemId)
	switch {
	case e != nil && nf:
		return BazarrPick404JSONResponse{NotFoundJSONResponse(*e)}, nil
	case e != nil:
		return BazarrPick502JSONResponse{BadGatewayJSONResponse(*e)}, nil
	}
	b := req.Body
	if strings.TrimSpace(b.Provider) == "" || b.Subtitle == "" {
		return BazarrPick404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "pick a result from a search"))}, nil
	}
	h.Bazarr.Pick(t, bazarr.Pick{Provider: b.Provider, Subtitle: b.Subtitle, Forced: b.Forced != nil && *b.Forced,
		HI: b.Hi != nil && *b.Hi, OriginalFormat: b.OriginalFormat != nil && *b.OriginalFormat})
	return BazarrPick202Response{}, nil
}

// ---------- library health (ADM-11) ----------

// health returns the health service; it holds no state, so one is made when not wired.
func (h *Handlers) health() *health.Service {
	if h.Health != nil {
		return h.Health
	}
	return &health.Service{DB: h.DB, Bazarr: h.Bazarr}
}

func (h *Handlers) LibraryHealth(ctx context.Context, _ LibraryHealthRequestObject) (LibraryHealthResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return LibraryHealth401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return LibraryHealth403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	list, err := h.health().Checks(ctx)
	if err != nil {
		return nil, internal(ctx, "libraryHealth", err)
	}
	out := make(LibraryHealth200JSONResponse, len(list))
	for i, c := range list {
		out[i] = HealthCheck{Id: HealthCheckId(c.ID), Title: c.Title, Description: c.Description, Count: c.Count,
			Severity: HealthCheckSeverity(c.Severity), Available: ptr(c.Available)}
	}
	return out, nil
}

func (h *Handlers) LibraryHealthIssues(ctx context.Context, req LibraryHealthIssuesRequestObject) (LibraryHealthIssuesResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return LibraryHealthIssues401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return LibraryHealthIssues403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	if !health.Valid(req.CheckId) {
		return LibraryHealthIssues404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "no such check"))}, nil
	}
	offset, limit := paging(req.Params.Offset, req.Params.Limit)
	list, total, err := h.health().Issues(ctx, req.CheckId, offset, limit)
	switch {
	case err != nil && req.CheckId == "missingSubtitles":
		// Bazarr isn't set up or is unreachable: nothing to show (the overview marks the
		// check unavailable).
		if !errors.Is(err, bazarr.ErrNotConfigured) {
			slog.WarnContext(ctx, "library health: Bazarr", "err", err)
		}
		return LibraryHealthIssues200JSONResponse{Items: []HealthIssue{}, Offset: offset}, nil
	case err != nil:
		return nil, internal(ctx, "libraryHealthIssues", err)
	}
	// Item summaries in one query; the admin sees everything.
	var ids []int64
	for _, is := range list {
		ids = append(ids, is.ItemID)
		ids = append(ids, is.Related...)
	}
	sums, err := h.Items.ByIDs(ctx, access(ctx), ids)
	if err != nil {
		return nil, internal(ctx, "libraryHealthIssues", err)
	}
	byID := make(map[int64]ItemSummary, len(sums))
	for _, s := range sums {
		byID[s.ID] = toAPISummary(s)
	}
	// The files to compare, with their streams.
	var fileIDs []int64
	for _, is := range list {
		for _, f := range is.Files {
			fileIDs = append(fileIDs, f.FileID)
		}
	}
	files, err := h.Items.Files(ctx, fileIDs)
	if err != nil {
		return nil, internal(ctx, "libraryHealthIssues", err)
	}
	out := LibraryHealthIssues200JSONResponse{Items: make([]HealthIssue, 0, len(list)), Total: total, Offset: offset}
	for _, is := range list {
		item, ok := byID[is.ItemID]
		if !ok {
			continue // removed since the count
		}
		hi := HealthIssue{Item: item, Detail: is.Detail, FileId: nz(is.FileID), Path: nz(is.Path)}
		if len(is.Related) > 0 {
			rel := make([]ItemSummary, 0, len(is.Related))
			for _, id := range is.Related {
				if s, ok := byID[id]; ok {
					rel = append(rel, s)
				}
			}
			hi.Related = &rel
		}
		if len(is.Files) > 0 {
			fl := make([]IssueFile, 0, len(is.Files))
			for _, f := range is.Files {
				mf, ok := files[f.FileID]
				if !ok {
					continue
				}
				added, _ := time.Parse(time.RFC3339Nano, f.AddedAt)
				fl = append(fl, IssueFile{ItemId: f.ItemID, ItemTitle: f.ItemTitle, VersionLabel: nz(f.VersionLabel),
					File: toAPIMediaFile(*mf), AddedAt: added})
			}
			hi.Files = &fl
		}
		out.Items = append(out.Items, hi)
	}
	return out, nil
}

// trash returns the media trash; it holds no state, so one is made when not wired.
func (h *Handlers) trash() *mediatrash.Service {
	if h.Trash != nil {
		return h.Trash
	}
	return &mediatrash.Service{DB: h.DB}
}

// DeleteMediaFile moves a file to its library's trash (ADM-11): admins only, and only
// when Library → Allow media deletion is on.
func (h *Handlers) DeleteMediaFile(ctx context.Context, req DeleteMediaFileRequestObject) (DeleteMediaFileResponseObject, error) {
	s, ok := session(ctx)
	switch {
	case !ok:
		return DeleteMediaFile401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !s.User.IsAdmin:
		return DeleteMediaFile403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	case !h.Settings.Get().Library.AllowMediaDeletion:
		return DeleteMediaFile403JSONResponse{ForbiddenJSONResponse(apiErr("deletion_off",
			"Deleting media is turned off. Turn on Allow media deletion in Library settings."))}, nil
	}
	err := h.trash().Delete(ctx, req.FileId, s.User.Username)
	var me *mediatrash.MoveError
	switch {
	case err == nil:
		return DeleteMediaFile204Response{}, nil
	case errors.Is(err, mediatrash.ErrNotFound):
		return DeleteMediaFile404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "file not found"))}, nil
	case errors.As(err, &me):
		slog.WarnContext(ctx, "delete media file", "file", req.FileId, "err", err)
		return DeleteMediaFile409JSONResponse{ConflictJSONResponse(apiErr("cannot_move", me.Msg))}, nil
	}
	return nil, internal(ctx, "deleteMediaFile", err)
}

// healthNotFoundMsg names what wasn't found: the check or the item.
func healthNotFoundMsg(err error) Error {
	if errors.Is(err, health.ErrUnknownCheck) {
		return apiErr("not_found", "no such check")
	}
	return apiErr("not_found", "item not found")
}

func (h *Handlers) IgnoreHealthIssue(ctx context.Context, req IgnoreHealthIssueRequestObject) (IgnoreHealthIssueResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return IgnoreHealthIssue401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return IgnoreHealthIssue403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	err := h.health().Ignore(ctx, req.CheckId, req.ItemId)
	switch {
	case errors.Is(err, health.ErrUnknownCheck), errors.Is(err, health.ErrNotFound):
		return IgnoreHealthIssue404JSONResponse{NotFoundJSONResponse(healthNotFoundMsg(err))}, nil
	case err != nil:
		return nil, internal(ctx, "ignoreHealthIssue", err)
	}
	return IgnoreHealthIssue204Response{}, nil
}

func (h *Handlers) UnignoreHealthIssue(ctx context.Context, req UnignoreHealthIssueRequestObject) (UnignoreHealthIssueResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return UnignoreHealthIssue401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return UnignoreHealthIssue403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	err := h.health().Unignore(ctx, req.CheckId, req.ItemId)
	switch {
	case errors.Is(err, health.ErrUnknownCheck):
		return UnignoreHealthIssue404JSONResponse{NotFoundJSONResponse(healthNotFoundMsg(err))}, nil
	case err != nil:
		return nil, internal(ctx, "unignoreHealthIssue", err)
	}
	return UnignoreHealthIssue204Response{}, nil
}

// recordPlaybackError keeps an app's playback failure for the playbackErrors check.
func (h *Handlers) recordPlaybackError(ctx context.Context, itemID, fileID int64, msg string) {
	if err := health.RecordPlaybackError(ctx, h.DB, itemID, fileID, msg); err != nil {
		slog.WarnContext(ctx, "record playback error", "item", itemID, "err", err)
	}
}
