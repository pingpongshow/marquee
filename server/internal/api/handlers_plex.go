package api

import (
	"context"
	"errors"

	"marquee/internal/plex"
)

var plexSectionTypes = map[int]PlexSectionPreviewType{1: PlexSectionPreviewTypeMovies, 2: PlexSectionPreviewTypeShows, 8: PlexSectionPreviewTypeMusic}

func toPlexMappings(in []PlexPathMapping) []plex.PathMapping {
	out := make([]plex.PathMapping, len(in))
	for i, m := range in {
		out[i] = plex.PathMapping{Plex: m.PlexPath, Marquee: m.MarqueePath}
	}
	return out
}

func (h *Handlers) plexStatus(ctx context.Context) (PlexImportStatus, error) {
	path, ok := h.Plex.Available()
	running, prog, lastErr := h.Plex.Status()
	out := PlexImportStatus{Available: ok, Running: running, LastError: nz(lastErr)}
	if ok {
		out.DatabasePath = &path
	}
	if running {
		out.Progress = &struct {
			Done  int    `json:"done"`
			Step  string `json:"step"`
			Total int    `json:"total"`
		}{Done: prog.Done, Step: prog.Step, Total: prog.Total}
	}
	rep, err := h.Plex.LastReport(ctx)
	if err != nil {
		return out, err
	}
	if rep != nil {
		r := PlexImportReport{
			StartedAt: rep.StartedAt, FinishedAt: rep.FinishedAt, Files: rep.Files, MatchedFiles: rep.MatchedFiles,
			UnmatchedSample: orEmpty(rep.UnmatchedSample), UsersCreated: rep.UsersCreated, UsersLinked: rep.UsersLinked,
			MatchesAgreed: rep.MatchesAgreed, MatchesApplied: rep.MatchesApplied, MatchesKept: orEmpty(rep.MatchesKept),
			MatchFailures: orEmpty(rep.MatchFailures), Artwork: rep.Posters, ArtworkMissing: rep.PostersMissing,
			WatchStateItems: rep.WatchStateItems, Ratings: rep.Ratings, HistoryImported: rep.HistoryImported,
			HistorySkipped: rep.HistorySkipped, Markers: rep.Markers, Playlists: rep.Playlists,
			PlaylistItems: rep.PlaylistItems, PlaylistItemsMissing: rep.PlaylistMissing, Warnings: orEmpty(rep.Warnings),
		}
		out.LastReport = &r
	}
	return out, nil
}

func orEmpty(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

func (h *Handlers) GetPlexImport(ctx context.Context, _ GetPlexImportRequestObject) (GetPlexImportResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return GetPlexImport401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return GetPlexImport403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	st, err := h.plexStatus(ctx)
	if err != nil {
		return nil, internal(ctx, "plexStatus", err)
	}
	return GetPlexImport200JSONResponse(st), nil
}

func (h *Handlers) PreviewPlexImport(ctx context.Context, req PreviewPlexImportRequestObject) (PreviewPlexImportResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return PreviewPlexImport401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return PreviewPlexImport403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	var mappings []plex.PathMapping
	if req.Body != nil && req.Body.PathMappings != nil {
		mappings = toPlexMappings(*req.Body.PathMappings)
	}
	pv, err := h.Plex.Preview(ctx, mappings)
	if errors.Is(err, plex.ErrNoDatabase) {
		return PreviewPlexImport400JSONResponse{BadRequestJSONResponse(apiErr("no_plex", "no Plex database found; mount your Plex data folder at /plex"))}, nil
	}
	if err != nil {
		return nil, internal(ctx, "plexPreview", err)
	}
	out := PlexImportPreview{Files: pv.Files, MatchedFiles: pv.Matched, UnmatchedSample: orEmpty(pv.UnmatchedSample),
		Sections: []PlexSectionPreview{}, Accounts: []PlexAccountPreview{}, PathMappings: []PlexPathMapping{}}
	for _, m := range pv.Mappings {
		out.PathMappings = append(out.PathMappings, PlexPathMapping{PlexPath: m.Plex, MarqueePath: m.Marquee})
	}
	for _, s := range pv.Sections {
		t, ok := plexSectionTypes[s.Type]
		if !ok {
			t = PlexSectionPreviewTypeOther
		}
		out.Sections = append(out.Sections, PlexSectionPreview{Id: s.ID, Name: s.Name, Type: t, Roots: s.Roots,
			LibraryId: nz(s.LibraryID), Files: s.Files, MatchedFiles: s.MatchedFiles})
	}
	for _, a := range pv.Accounts {
		out.Accounts = append(out.Accounts, PlexAccountPreview{Id: a.ID, Name: a.Name, IsOwner: a.IsOwner, Watched: a.Watched,
			InProgress: a.InProgress, Ratings: a.Ratings, History: a.History, Playlists: a.Playlists,
			SuggestedAction: PlexAccountPreviewSuggestedAction(a.SuggestedAction), SuggestedUserId: nz(a.SuggestedUserID),
			SuggestedUsername: nz(a.SuggestedUsername)})
	}
	return PreviewPlexImport200JSONResponse(out), nil
}

func (h *Handlers) StartPlexImport(ctx context.Context, req StartPlexImportRequestObject) (StartPlexImportResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return StartPlexImport401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return StartPlexImport403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	b := req.Body
	if len(b.PathMappings) == 0 {
		return StartPlexImport400JSONResponse{BadRequestJSONResponse(apiErr("invalid", "at least one folder mapping is required"))}, nil
	}
	opts := plex.Options{Mappings: toPlexMappings(b.PathMappings), Matches: true, WatchState: true, History: true,
		Playlists: true, Markers: true, Artwork: true}
	if inc := b.Include; inc != nil {
		set(&opts.Matches, inc.Matches)
		set(&opts.WatchState, inc.WatchState)
		set(&opts.History, inc.History)
		set(&opts.Playlists, inc.Playlists)
		set(&opts.Markers, inc.Markers)
		set(&opts.Artwork, inc.Artwork)
	}
	for _, a := range b.Accounts {
		c := plex.AccountChoice{PlexID: a.PlexId, Action: string(a.Action)}
		set(&c.UserID, a.UserId)
		set(&c.Username, a.Username)
		set(&c.MergeInto, a.MergeInto)
		if c.Action == "merge" && c.MergeInto == 0 {
			return StartPlexImport400JSONResponse{BadRequestJSONResponse(apiErr("invalid", "choose which account to merge into"))}, nil
		}
		if c.Action == "link" && c.UserID == 0 {
			return StartPlexImport400JSONResponse{BadRequestJSONResponse(apiErr("invalid", "choose a Marquee user to link each account to"))}, nil
		}
		if c.Action == "create" && c.Username == "" {
			return StartPlexImport400JSONResponse{BadRequestJSONResponse(apiErr("invalid", "new users need a username"))}, nil
		}
		opts.Accounts = append(opts.Accounts, c)
	}
	// The import outlives this request.
	if err := h.Plex.Start(context.WithoutCancel(ctx), opts); err != nil {
		return StartPlexImport409JSONResponse{ConflictJSONResponse(apiErr("running", err.Error()))}, nil
	}
	st, err := h.plexStatus(ctx)
	if err != nil {
		return nil, internal(ctx, "plexStart", err)
	}
	return StartPlexImport202JSONResponse(st), nil
}
