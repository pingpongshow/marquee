package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"

	"marquee/internal/library"
	"marquee/internal/settings"
)

// ---------- settings ----------

func toAPISettings(s settings.Settings) ServerSettings {
	order := make([]EncoderKind, len(s.Transcoder.EncoderOrder))
	for i, e := range s.Transcoder.EncoderOrder {
		order[i] = EncoderKind(e)
	}
	ladder := make([]QualityRung, len(s.Transcoder.RemoteLadder))
	for i, r := range s.Transcoder.RemoteLadder {
		ladder[i] = QualityRung{Label: r.Label, MaxHeight: r.MaxHeight, VideoKbps: r.VideoKbps}
	}
	return ServerSettings{
		Security: SecuritySettings{PinSignIn: ptr(SecuritySettingsPinSignIn(s.Security.PinSignIn))},
		General:  GeneralSettings{ServerName: ptr(s.General.ServerName), MetadataLanguage: ptr(s.General.MetadataLanguage)},
		Network: NetworkSettings{
			LanSubnets:     ptr(append([]string{}, s.Network.LANSubnets...)),
			LanUrl:         ptr(s.Network.LANURL),
			BonjourEnabled: ptr(s.Network.BonjourEnabled),
		},
		RemoteAccess: RemoteAccessSettings{
			Enabled:                  ptr(s.RemoteAccess.Enabled),
			RemoteUrl:                ptr(s.RemoteAccess.RemoteURL),
			UploadSpeedKbps:          ptr(s.RemoteAccess.UploadSpeedKbps),
			TotalRemoteLimitKbps:     ptr(s.RemoteAccess.TotalRemoteLimitKbps),
			PerStreamRemoteLimitKbps: ptr(s.RemoteAccess.PerStreamRemoteLimitKbps),
			MinStreamKbps:            ptr(s.RemoteAccess.MinStreamKbps),
		},
		Transcoder: TranscoderSettings{
			EncoderOrder:            &order,
			MaxConcurrentTranscodes: ptr(s.Transcoder.MaxConcurrentTranscodes),
			Preset:                  ptr(TranscoderSettingsPreset(s.Transcoder.Preset)),
			PreferHevcRemote:        ptr(s.Transcoder.PreferHEVCRemote),
			ToneMapping:             ptr(s.Transcoder.ToneMapping),
			ThrottleSegmentsAhead:   ptr(s.Transcoder.ThrottleSegmentsAhead),
			NvencSessions:           ptr(s.Transcoder.NVENCSessions),
			RemoteLadder:            &ladder,
		},
		Library: LibraryGlobalSettings{
			WatchFilesystem:         ptr(s.Library.WatchFilesystem),
			ScanOnStartup:           ptr(s.Library.ScanOnStartup),
			IgnorePatterns:          ptr(append([]string{}, s.Library.IgnorePatterns...)),
			BrowseRoots:             ptr(append([]string{}, s.Library.BrowseRoots...)),
			WatchedThresholdPercent: ptr(s.Library.WatchedThresholdPercent),
			Trickplay:               ptr(s.Library.Trickplay),
			DetectIntros:            ptr(s.Library.DetectIntros),
		},
		Metadata: MetadataSettings{
			TmdbApiKeySet:            ptr(s.Metadata.TMDBAPIKey != ""),
			FanartApiKeySet:          ptr(s.Metadata.FanartAPIKey != ""),
			OpenSubtitlesApiKeySet:   ptr(s.Metadata.OpenSubtitlesAPIKey != ""),
			OpenSubtitlesUsername:    nz(s.Metadata.OpenSubtitlesUser),
			OpenSubtitlesPasswordSet: ptr(s.Metadata.OpenSubtitlesPass != ""),
			OmdbApiKeySet:            ptr(s.Metadata.OMDbAPIKey != ""),
			OmdbDailyLimit:           ptr(s.Metadata.OMDbDailyLimit),
			AnimeEpisodeOrdering:     ptr(MetadataSettingsAnimeEpisodeOrdering(s.Metadata.AnimeEpisodeOrdering)),
		},
		Tasks: TaskSettings{
			MaintenanceWindowStart: ptr(s.Tasks.MaintenanceWindowStart),
			MaintenanceWindowHours: ptr(s.Tasks.MaintenanceWindowHours),
			BackupRetention:        ptr(s.Tasks.BackupRetention),
		},
		Music: &MusicSettings{
			SonicAnalysis:    ptr(s.Music.SonicAnalysis),
			OnlineLyrics:     ptr(s.Music.OnlineLyrics),
			LoudnessAnalysis: ptr(s.Music.LoudnessAnalysis),
		},
		Integrations: &IntegrationSettings{
			SeerrUrl:       ptr(s.Integrations.SeerrURL),
			SeerrApiKeySet: ptr(s.Integrations.SeerrAPIKey != ""),
		},
		Webhooks: ptr(toAPIWebhooks(s.Webhooks)),
	}
}

func toAPIWebhooks(ws []settings.Webhook) []Webhook {
	out := make([]Webhook, len(ws))
	for i, w := range ws {
		ev := make([]WebhookEvents, len(w.Events))
		for j, e := range w.Events {
			ev[j] = WebhookEvents(e)
		}
		out[i] = Webhook{Id: ptr(w.ID), Name: w.Name, Url: w.URL, Secret: nz(w.Secret), Events: ev, Enabled: w.Enabled}
	}
	return out
}

func set[T any](dst *T, src *T) {
	if src != nil {
		*dst = *src
	}
}

// applySettingsUpdate copies every non-nil field of u onto s.
func applySettingsUpdate(s *settings.Settings, u ServerSettingsUpdate) {
	if sec := u.Security; sec != nil && sec.PinSignIn != nil {
		s.Security.PinSignIn = string(*sec.PinSignIn)
	}
	if g := u.General; g != nil {
		set(&s.General.ServerName, g.ServerName)
		set(&s.General.MetadataLanguage, g.MetadataLanguage)
	}
	if n := u.Network; n != nil {
		set(&s.Network.LANSubnets, n.LanSubnets)
		set(&s.Network.LANURL, n.LanUrl)
		set(&s.Network.BonjourEnabled, n.BonjourEnabled)
	}
	if r := u.RemoteAccess; r != nil {
		set(&s.RemoteAccess.Enabled, r.Enabled)
		set(&s.RemoteAccess.RemoteURL, r.RemoteUrl)
		set(&s.RemoteAccess.UploadSpeedKbps, r.UploadSpeedKbps)
		set(&s.RemoteAccess.TotalRemoteLimitKbps, r.TotalRemoteLimitKbps)
		set(&s.RemoteAccess.PerStreamRemoteLimitKbps, r.PerStreamRemoteLimitKbps)
		set(&s.RemoteAccess.MinStreamKbps, r.MinStreamKbps)
	}
	if t := u.Transcoder; t != nil {
		if t.EncoderOrder != nil {
			s.Transcoder.EncoderOrder = make([]string, len(*t.EncoderOrder))
			for i, e := range *t.EncoderOrder {
				s.Transcoder.EncoderOrder[i] = string(e)
			}
		}
		set(&s.Transcoder.MaxConcurrentTranscodes, t.MaxConcurrentTranscodes)
		if t.Preset != nil {
			s.Transcoder.Preset = string(*t.Preset)
		}
		set(&s.Transcoder.PreferHEVCRemote, t.PreferHevcRemote)
		set(&s.Transcoder.ToneMapping, t.ToneMapping)
		set(&s.Transcoder.ThrottleSegmentsAhead, t.ThrottleSegmentsAhead)
		set(&s.Transcoder.NVENCSessions, t.NvencSessions)
		if t.RemoteLadder != nil {
			s.Transcoder.RemoteLadder = make([]settings.QualityRung, len(*t.RemoteLadder))
			for i, r := range *t.RemoteLadder {
				s.Transcoder.RemoteLadder[i] = settings.QualityRung{Label: r.Label, MaxHeight: r.MaxHeight, VideoKbps: r.VideoKbps}
			}
		}
	}
	if l := u.Library; l != nil {
		set(&s.Library.WatchFilesystem, l.WatchFilesystem)
		set(&s.Library.ScanOnStartup, l.ScanOnStartup)
		set(&s.Library.IgnorePatterns, l.IgnorePatterns)
		set(&s.Library.BrowseRoots, l.BrowseRoots)
		set(&s.Library.WatchedThresholdPercent, l.WatchedThresholdPercent)
		set(&s.Library.Trickplay, l.Trickplay)
		set(&s.Library.DetectIntros, l.DetectIntros)
	}
	if m := u.Metadata; m != nil {
		set(&s.Metadata.TMDBAPIKey, m.TmdbApiKey)
		set(&s.Metadata.FanartAPIKey, m.FanartApiKey)
		set(&s.Metadata.OpenSubtitlesAPIKey, m.OpenSubtitlesApiKey)
		set(&s.Metadata.OpenSubtitlesUser, m.OpenSubtitlesUsername)
		set(&s.Metadata.OpenSubtitlesPass, m.OpenSubtitlesPassword)
		set(&s.Metadata.OMDbAPIKey, m.OmdbApiKey)
		set(&s.Metadata.OMDbDailyLimit, m.OmdbDailyLimit)
		if m.AnimeEpisodeOrdering != nil {
			s.Metadata.AnimeEpisodeOrdering = string(*m.AnimeEpisodeOrdering)
		}
	}
	if t := u.Tasks; t != nil {
		set(&s.Tasks.MaintenanceWindowStart, t.MaintenanceWindowStart)
		set(&s.Tasks.MaintenanceWindowHours, t.MaintenanceWindowHours)
		set(&s.Tasks.BackupRetention, t.BackupRetention)
	}
	if m := u.Music; m != nil {
		set(&s.Music.SonicAnalysis, m.SonicAnalysis)
		set(&s.Music.OnlineLyrics, m.OnlineLyrics)
		set(&s.Music.LoudnessAnalysis, m.LoudnessAnalysis)
	}
	if i := u.Integrations; i != nil {
		if i.SeerrUrl != nil {
			s.Integrations.SeerrURL = strings.TrimRight(strings.TrimSpace(*i.SeerrUrl), "/")
		}
		set(&s.Integrations.SeerrAPIKey, i.SeerrApiKey)
	}
	if u.Webhooks != nil {
		s.Webhooks = make([]settings.Webhook, len(*u.Webhooks))
		for i, w := range *u.Webhooks {
			id := ""
			if w.Id != nil {
				id = *w.Id
			}
			if id == "" {
				id = newWebhookID()
			}
			ev := make([]string, len(w.Events))
			for j, e := range w.Events {
				ev[j] = string(e)
			}
			s.Webhooks[i] = settings.Webhook{ID: id, Name: strings.TrimSpace(w.Name), URL: strings.TrimSpace(w.Url), Events: ev, Enabled: w.Enabled}
			if w.Secret != nil {
				s.Webhooks[i].Secret = *w.Secret
			}
		}
	}
}

func newWebhookID() string {
	b := make([]byte, 6)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (h *Handlers) GetSettings(ctx context.Context, _ GetSettingsRequestObject) (GetSettingsResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return GetSettings401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return GetSettings403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	return GetSettings200JSONResponse(toAPISettings(h.Settings.Get())), nil
}

func (h *Handlers) UpdateSettings(ctx context.Context, req UpdateSettingsRequestObject) (UpdateSettingsResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return UpdateSettings401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return UpdateSettings403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	next, err := h.Settings.Update(ctx, func(s *settings.Settings) error {
		applySettingsUpdate(s, *req.Body)
		return nil
	})
	var ve *settings.ValidationError
	if errors.As(err, &ve) {
		return UpdateSettings400JSONResponse{BadRequestJSONResponse(apiErr("invalid", ve.Msg))}, nil
	}
	if err != nil {
		return nil, internal(ctx, "updateSettings", err)
	}
	return UpdateSettings200JSONResponse(toAPISettings(next)), nil
}

// ---------- libraries ----------

func (h *Handlers) toAPILibrary(l library.Library) Library {
	o := l.Options.WithDefaults(l.Type, h.Settings.Get().General.MetadataLanguage)
	opts := LibraryOptions{
		Language:          o.Language,
		IgnorePatterns:    o.IgnorePatterns,
		IncludeInHome:     o.IncludeInHome,
		IncludeInSearch:   o.IncludeInSearch,
		ScanIntervalHours: o.ScanIntervalHours,
	}
	if o.EpisodeOrdering != nil {
		opts.EpisodeOrdering = ptr(LibraryOptionsEpisodeOrdering(*o.EpisodeOrdering))
	}
	out := Library{
		Id: l.ID, Name: l.Name, Type: LibraryType(l.Type), Paths: l.Paths, Options: opts,
		ItemCount: l.ItemCount, ScanStatus: LibraryScanStatusIdle, LastScannedAt: l.LastScannedAt,
		CreatedAt: l.CreatedAt, UpdatedAt: l.UpdatedAt,
	}
	if h.Scans != nil {
		st := h.Scans.Status(l.ID)
		out.ScanStatus = LibraryScanStatus(st.State)
		out.LastScanResult = nz(st.LastResult)
		out.LastScanError = nz(st.LastError)
		if p := st.Progress; p != nil {
			out.ScanProgress = &ScanProgress{Phase: ScanProgressPhase(p.Phase), Done: p.Done, Total: p.Total, Current: nz(p.Current)}
		}
	}
	return out
}

func fromAPIOptions(o *LibraryOptions) library.Options {
	if o == nil {
		return library.Options{}
	}
	out := library.Options{
		Language: o.Language, IgnorePatterns: o.IgnorePatterns, IncludeInHome: o.IncludeInHome,
		IncludeInSearch: o.IncludeInSearch, ScanIntervalHours: o.ScanIntervalHours,
	}
	if o.EpisodeOrdering != nil {
		out.EpisodeOrdering = ptr(string(*o.EpisodeOrdering))
	}
	return out
}

func (h *Handlers) ListLibraries(ctx context.Context, _ ListLibrariesRequestObject) (ListLibrariesResponseObject, error) {
	if _, ok := session(ctx); !ok {
		return ListLibraries401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	// Per-user library restrictions arrive with user management in M1.
	libs, err := h.Libraries.List(ctx)
	if err != nil {
		return nil, internal(ctx, "listLibraries", err)
	}
	out := ListLibraries200JSONResponse{}
	for _, l := range libs {
		if canSeeLibrary(ctx, l.ID) {
			out = append(out, h.toAPILibrary(l))
		}
	}
	return out, nil
}

func (h *Handlers) GetLibrary(ctx context.Context, req GetLibraryRequestObject) (GetLibraryResponseObject, error) {
	if _, ok := session(ctx); !ok {
		return GetLibrary401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	l, err := h.Libraries.Get(ctx, req.LibraryId)
	if errors.Is(err, library.ErrNotFound) || (err == nil && !canSeeLibrary(ctx, req.LibraryId)) {
		return GetLibrary404JSONResponse{NotFoundJSONResponse(apiErr("not_found", library.ErrNotFound.Error()))}, nil
	}
	if err != nil {
		return nil, internal(ctx, "getLibrary", err)
	}
	return GetLibrary200JSONResponse(h.toAPILibrary(l)), nil
}

func (h *Handlers) CreateLibrary(ctx context.Context, req CreateLibraryRequestObject) (CreateLibraryResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return CreateLibrary401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return CreateLibrary403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	b := req.Body
	l, err := h.Libraries.Create(ctx, b.Name, library.Type(b.Type), b.Paths, fromAPIOptions(b.Options))
	var ve *library.ValidationError
	if errors.As(err, &ve) {
		return CreateLibrary400JSONResponse{BadRequestJSONResponse(apiErr("invalid", ve.Msg))}, nil
	}
	if err != nil {
		return nil, internal(ctx, "createLibrary", err)
	}
	if h.Scans != nil {
		h.Scans.Queue(l.ID) // new libraries scan straight away, like Plex
	}
	h.librariesChanged()
	return CreateLibrary201JSONResponse(h.toAPILibrary(l)), nil
}

func (h *Handlers) UpdateLibrary(ctx context.Context, req UpdateLibraryRequestObject) (UpdateLibraryResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return UpdateLibrary401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return UpdateLibrary403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	b := req.Body
	var opts *library.Options
	if b.Options != nil {
		o := fromAPIOptions(b.Options)
		opts = &o
	}
	l, err := h.Libraries.Update(ctx, req.LibraryId, b.Name, b.Paths, opts)
	var ve *library.ValidationError
	switch {
	case errors.As(err, &ve):
		return UpdateLibrary400JSONResponse{BadRequestJSONResponse(apiErr("invalid", ve.Msg))}, nil
	case errors.Is(err, library.ErrNotFound):
		return UpdateLibrary404JSONResponse{NotFoundJSONResponse(apiErr("not_found", err.Error()))}, nil
	case err != nil:
		return nil, internal(ctx, "updateLibrary", err)
	}
	if b.Paths != nil && h.Scans != nil {
		h.Scans.Queue(l.ID) // folders changed: pick up new files and drop removed ones
	}
	if b.Paths != nil {
		h.librariesChanged()
	}
	return UpdateLibrary200JSONResponse(h.toAPILibrary(l)), nil
}

func (h *Handlers) DeleteLibrary(ctx context.Context, req DeleteLibraryRequestObject) (DeleteLibraryResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return DeleteLibrary401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return DeleteLibrary403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	err := h.Libraries.Delete(ctx, req.LibraryId)
	if errors.Is(err, library.ErrNotFound) {
		return DeleteLibrary404JSONResponse{NotFoundJSONResponse(apiErr("not_found", err.Error()))}, nil
	}
	if err != nil {
		return nil, internal(ctx, "deleteLibrary", err)
	}
	h.librariesChanged()
	return DeleteLibrary204Response{}, nil
}

// ---------- filesystem ----------

func (h *Handlers) BrowseFilesystem(ctx context.Context, req BrowseFilesystemRequestObject) (BrowseFilesystemResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return BrowseFilesystem401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return BrowseFilesystem403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	dir := ""
	if req.Params.Path != nil {
		dir = *req.Params.Path
	}
	entries, parent, err := library.Browse(h.Settings.Get().Library.BrowseRoots, dir)
	switch {
	case errors.Is(err, library.ErrOutsideRoots):
		return BrowseFilesystem403JSONResponse{ForbiddenJSONResponse(apiErr("outside_roots", err.Error()))}, nil
	case errors.Is(err, library.ErrNoSuchDir):
		return BrowseFilesystem404JSONResponse{NotFoundJSONResponse(apiErr("not_found", err.Error()))}, nil
	case err != nil:
		return nil, internal(ctx, "browse", err)
	}
	out := DirectoryListing{Entries: make([]DirectoryEntry, len(entries))}
	for i, e := range entries {
		out.Entries[i] = DirectoryEntry{Name: e.Name, Path: e.Path}
	}
	if dir != "" {
		out.Path = &dir
	}
	if parent != "" {
		out.Parent = &parent
	}
	return BrowseFilesystem200JSONResponse(out), nil
}

func (h *Handlers) librariesChanged() {
	if h.LibrariesChanged != nil {
		go h.LibrariesChanged()
	}
}
