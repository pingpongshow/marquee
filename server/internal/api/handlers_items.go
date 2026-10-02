package api

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"marquee/internal/images"
	"marquee/internal/items"
	"marquee/internal/library"
)

// ---------- scans ----------

func (h *Handlers) ScanLibrary(ctx context.Context, req ScanLibraryRequestObject) (ScanLibraryResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return ScanLibrary401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return ScanLibrary403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	l, err := h.Libraries.Get(ctx, req.LibraryId)
	if errors.Is(err, library.ErrNotFound) {
		return ScanLibrary404JSONResponse{NotFoundJSONResponse(apiErr("not_found", err.Error()))}, nil
	}
	if err != nil {
		return nil, internal(ctx, "scanLibrary", err)
	}
	h.Scans.Queue(l.ID)
	return ScanLibrary202JSONResponse(h.toAPILibrary(l)), nil
}

func (h *Handlers) CancelLibraryScan(ctx context.Context, req CancelLibraryScanRequestObject) (CancelLibraryScanResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return CancelLibraryScan401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return CancelLibraryScan403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	h.Scans.Cancel(req.LibraryId)
	return CancelLibraryScan204Response{}, nil
}

func (h *Handlers) ScanAllLibraries(ctx context.Context, _ ScanAllLibrariesRequestObject) (ScanAllLibrariesResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return ScanAllLibraries401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return ScanAllLibraries403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	h.Scans.QueueAll(ctx)
	return ScanAllLibraries202Response{}, nil
}

// ---------- items ----------

func nz[T comparable](v T) *T {
	var zero T
	if v == zero {
		return nil
	}
	return &v
}

func toAPISummary(s items.Summary) ItemSummary {
	out := ItemSummary{
		Id: s.ID, LibraryId: s.LibraryID, Type: ItemType(s.Type), Title: s.Title,
		OriginalTitle: nz(s.OriginalTitle), Year: nz(s.Year), AbsoluteIndex: nz(s.AbsIndex), Disc: nz(s.Disc),
		ParentId: nz(s.ParentID), GrandparentId: nz(s.GrandparentID), ParentTitle: nz(s.ParentTitle),
		GrandparentTitle: nz(s.GrandparentTitle), ArtistCredit: nz(s.ArtistCredit),
		ChildCount: s.ChildCount, LeafCount: s.LeafCount, DurationMs: nz(s.DurationMS),
		Available: s.Available, MatchState: ItemSummaryMatchState(s.MatchState), AddedAt: s.AddedAt,
	}
	// Index 0 is meaningful (Specials, track 0 is not); report it for seasons/episodes.
	if s.Index != 0 || s.Type == "season" || s.Type == "episode" {
		out.Index = ptr(s.Index)
	}
	out.ViewOffsetMs = nz(s.ViewOffsetMS)
	out.ViewCount = nz(s.ViewCount)
	out.WatchedLeafCount = nz(s.WatchedLeaves)
	if s.UserRating > 0 {
		out.UserRating = ptr(float32(s.UserRating))
	}
	out.Watchlisted = nz(s.Watchlisted)
	if s.ExtraType != "" {
		out.ExtraType = ptr(ItemSummaryExtraType(s.ExtraType))
	}
	if s.ReleaseType != "" {
		out.ReleaseType = ptr(ItemSummaryReleaseType(s.ReleaseType))
	}
	if t, err := time.Parse(time.RFC3339Nano, s.LastViewedAt); err == nil {
		out.LastViewedAt = &t
	}
	if s.Poster+s.Backdrop+s.Thumb+s.Logo > 0 {
		out.Images = &ItemImages{Poster: nz(s.Poster), Backdrop: nz(s.Backdrop), Thumb: nz(s.Thumb), Logo: nz(s.Logo)}
	}
	if t, err := time.Parse("2006-01-02", s.ReleaseDate); err == nil {
		out.OriginallyAvailableAt = &openapi_types.Date{Time: t}
	}
	return out
}

func page(list []items.Summary, total, offset int) ItemPage {
	out := ItemPage{Items: make([]ItemSummary, len(list)), Total: total, Offset: offset}
	for i, s := range list {
		out.Items[i] = toAPISummary(s)
	}
	return out
}

func paging(offset, limit *int) (int, int) {
	o, l := 0, 100
	if offset != nil && *offset > 0 {
		o = *offset
	}
	if limit != nil && *limit > 0 && *limit <= 500 {
		l = *limit
	}
	return o, l
}

func (h *Handlers) ListLibraryItems(ctx context.Context, req ListLibraryItemsRequestObject) (ListLibraryItemsResponseObject, error) {
	if _, ok := session(ctx); !ok {
		return ListLibraryItems401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	lib, err := h.Libraries.Get(ctx, req.LibraryId)
	if errors.Is(err, library.ErrNotFound) || (err == nil && !canSeeLibrary(ctx, req.LibraryId)) {
		return ListLibraryItems404JSONResponse{NotFoundJSONResponse(apiErr("not_found", library.ErrNotFound.Error()))}, nil
	}
	if err != nil {
		return nil, internal(ctx, "listItems", err)
	}
	typ := items.TopLevelType(string(lib.Type))
	if req.Params.Type != nil {
		typ = string(*req.Params.Type)
	}
	sort := "title"
	if req.Params.Sort != nil {
		sort = string(*req.Params.Sort)
	}
	offset, limit := paging(req.Params.Offset, req.Params.Limit)
	var f items.Filter
	p := req.Params
	if p.Watch != nil {
		f.Watch = string(*p.Watch)
	}
	if p.Resolution != nil {
		f.Resolution = string(*p.Resolution)
	}
	set(&f.Genre, p.Genre)
	set(&f.Decade, p.Decade)
	set(&f.ContentRating, p.ContentRating)
	set(&f.HDR, p.Hdr)
	set(&f.Letter, p.Letter)
	list, total, err := h.Items.List(ctx, access(ctx), lib.ID, typ, sort, f, offset, limit)
	if err != nil {
		return nil, internal(ctx, "listItems", err)
	}
	return ListLibraryItems200JSONResponse(page(list, total, offset)), nil
}

func (h *Handlers) ListItemChildren(ctx context.Context, req ListItemChildrenRequestObject) (ListItemChildrenResponseObject, error) {
	if _, ok := session(ctx); !ok {
		return ListItemChildren401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	if err := h.Items.Visible(ctx, access(ctx), req.ItemId); errors.Is(err, items.ErrNotFound) {
		return ListItemChildren404JSONResponse{NotFoundJSONResponse(apiErr("not_found", err.Error()))}, nil
	} else if err != nil {
		return nil, internal(ctx, "children", err)
	}
	offset, limit := paging(req.Params.Offset, req.Params.Limit)
	list, total, err := h.Items.Children(ctx, access(ctx), req.ItemId, offset, limit)
	if err != nil {
		return nil, internal(ctx, "children", err)
	}
	return ListItemChildren200JSONResponse(page(list, total, offset)), nil
}

func (h *Handlers) ListPopularTracks(ctx context.Context, req ListPopularTracksRequestObject) (ListPopularTracksResponseObject, error) {
	if _, ok := session(ctx); !ok {
		return ListPopularTracks401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	if err := h.Items.Visible(ctx, access(ctx), req.ItemId); errors.Is(err, items.ErrNotFound) {
		return ListPopularTracks404JSONResponse{NotFoundJSONResponse(apiErr("not_found", err.Error()))}, nil
	} else if err != nil {
		return nil, internal(ctx, "popular", err)
	}
	var ids []int64
	rows, err := h.DB.QueryContext(ctx, `SELECT track_id FROM music_popular WHERE artist_id = ? ORDER BY rank`, req.ItemId)
	if err != nil {
		return nil, internal(ctx, "popular", err)
	}
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	list, err := h.Items.ByIDs(ctx, access(ctx), ids)
	if err != nil {
		return nil, internal(ctx, "popular", err)
	}
	return ListPopularTracks200JSONResponse(page(list, len(list), 0)), nil
}

func (h *Handlers) GetItem(ctx context.Context, req GetItemRequestObject) (GetItemResponseObject, error) {
	if _, ok := session(ctx); !ok {
		return GetItem401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	out, err := h.detail(ctx, req.ItemId)
	if errors.Is(err, items.ErrNotFound) {
		return GetItem404JSONResponse{NotFoundJSONResponse(apiErr("not_found", err.Error()))}, nil
	}
	if err != nil {
		return nil, internal(ctx, "getItem", err)
	}
	return GetItem200JSONResponse(out), nil
}

// detail loads an item as the caller may see it.
func (h *Handlers) detail(ctx context.Context, id int64) (ItemDetail, error) {
	s, _ := session(ctx)
	d, err := h.Items.Get(ctx, access(ctx), id, s.User.IsAdmin)
	if err != nil {
		return ItemDetail{}, err
	}
	sum := toAPISummary(d.Summary)
	out := ItemDetail{
		Id: sum.Id, LibraryId: sum.LibraryId, Type: sum.Type, Title: sum.Title, OriginalTitle: sum.OriginalTitle,
		Year: sum.Year, Index: sum.Index, AbsoluteIndex: sum.AbsoluteIndex, Disc: sum.Disc, ParentId: sum.ParentId,
		GrandparentId: sum.GrandparentId, ParentTitle: sum.ParentTitle, GrandparentTitle: sum.GrandparentTitle,
		ArtistCredit: sum.ArtistCredit, ChildCount: sum.ChildCount, LeafCount: sum.LeafCount, DurationMs: sum.DurationMs,
		OriginallyAvailableAt: sum.OriginallyAvailableAt, Available: sum.Available,
		MatchState: ItemDetailMatchState(sum.MatchState), AddedAt: sum.AddedAt, Images: sum.Images,
		ViewOffsetMs: sum.ViewOffsetMs, ViewCount: sum.ViewCount, WatchedLeafCount: sum.WatchedLeafCount, LastViewedAt: sum.LastViewedAt,
		UserRating: sum.UserRating, Watchlisted: sum.Watchlisted, ExtraType: (*ItemDetailExtraType)(sum.ExtraType),
		ReleaseType: (*ItemDetailReleaseType)(sum.ReleaseType),
		AudienceRating: nz(float32(d.AudienceRating)), Credits: make([]Credit, len(d.Credits)),
		Summary: nz(d.Plot), Tagline: nz(d.Tagline), ContentRating: nz(d.ContentRating), Studio: nz(d.Studio),
		Genres: d.Genres, ExternalIds: d.ExternalIDs, LockedFields: d.LockedFields,
		Versions: make([]MediaVersion, len(d.Versions)), Chapters: make([]Chapter, len(d.Chapters)),
	}
	if xs, err := h.Items.Extras(ctx, access(ctx), id); err == nil && len(xs) > 0 {
		list := make([]ItemSummary, len(xs))
		for i, x := range xs {
			list[i] = toAPISummary(x)
		}
		out.Extras = &list
	}
	if cs, err := h.Items.CollectionsOf(ctx, access(ctx), id); err == nil && len(cs) > 0 {
		list := make([]ItemSummary, len(cs))
		for i, c := range cs {
			list[i] = toAPISummary(c)
		}
		out.Collections = &list
	}
	for i, v := range d.Versions {
		mv := MediaVersion{Id: v.ID, Label: v.Label, Files: make([]MediaFile, len(v.Files))}
		for j, f := range v.Files {
			mf := MediaFile{
				Id: f.ID, Path: nz(f.Path), Size: f.Size, Container: nz(f.Container), DurationMs: nz(f.DurationMS),
				BitrateKbps: nz(f.BitrateKbps), Width: nz(f.Width), Height: nz(f.Height), VideoCodec: nz(f.VideoCodec),
				AudioCodec: nz(f.AudioCodec), DvProfile: nz(f.DVProfile), PartIndex: f.PartIndex, Available: f.Available,
				Streams: make([]MediaStream, len(f.Streams)),
			}
			if f.HDRFormat != "" {
				mf.HdrFormat = ptr(MediaFileHdrFormat(f.HDRFormat))
			}
			for k, st := range f.Streams {
				mf.Streams[k] = MediaStream{
					Id: st.ID, Kind: MediaStreamKind(st.Kind), Codec: st.Codec, Profile: nz(st.Profile), Language: nz(st.Language),
					Title: nz(st.Title), Default: st.Default, Forced: st.Forced, HearingImpaired: st.HearingImpaired,
					External: st.External, Channels: nz(st.Channels), ChannelLayout: nz(st.ChannelLayout),
					SampleRate: nz(st.SampleRate), BitrateKbps: nz(st.BitrateKbps), Width: nz(st.Width), Height: nz(st.Height),
					FrameRate: nz(float32(st.FrameRate)), BitDepth: nz(st.Depth),
				}
			}
			mv.Files[j] = mf
		}
		out.Versions[i] = mv
	}
	if d.IMDbRating > 0 || d.RTCritic >= 0 || d.Metacritic >= 0 {
		r := Ratings{Imdb: nz(float32(d.IMDbRating)), ImdbVotes: nz(d.IMDbVotes)}
		if d.RTCritic >= 0 {
			r.RottenTomatoes = ptr(d.RTCritic)
		}
		if d.Metacritic >= 0 {
			r.Metacritic = ptr(d.Metacritic)
		}
		out.Ratings = &r
	}
	for i, c := range d.Credits {
		out.Credits[i] = Credit{PersonId: c.PersonID, Name: c.Name, Role: CreditRole(c.Role), Character: nz(c.Character), HasPhoto: ptr(c.HasPhoto)}
	}
	for i, c := range d.Chapters {
		out.Chapters[i] = Chapter{Title: nz(c.Title), StartMs: c.StartMS, EndMs: c.EndMS}
	}
	return out, nil
}

// ---------- images ----------

const immutable = "private, max-age=31536000, immutable"

func (h *Handlers) GetImage(ctx context.Context, req GetImageRequestObject) (GetImageResponseObject, error) {
	if _, ok := session(ctx); !ok {
		return GetImage401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	w := 0
	if req.Params.W != nil {
		w = *req.Params.W
	}
	path, ctype, err := h.Images.Path(ctx, req.ArtworkId, w)
	if errors.Is(err, images.ErrNotFound) || errors.Is(err, os.ErrNotExist) {
		return GetImage404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "image not found"))}, nil
	}
	if err != nil {
		slog.WarnContext(ctx, "image", "artwork", req.ArtworkId, "err", err)
		return GetImage404JSONResponse{NotFoundJSONResponse(apiErr("unavailable", "image unavailable"))}, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, internal(ctx, "image", err)
	}
	st, _ := f.Stat()
	hdr := GetImage200ResponseHeaders{CacheControl: ptr(immutable)}
	if ctype == "image/png" {
		return GetImage200ImagepngResponse{Body: f, ContentLength: st.Size(), Headers: hdr}, nil
	}
	return GetImage200ImagejpegResponse{Body: f, ContentLength: st.Size(), Headers: hdr}, nil
}

func (h *Handlers) GetPersonPhoto(ctx context.Context, req GetPersonPhotoRequestObject) (GetPersonPhotoResponseObject, error) {
	if _, ok := session(ctx); !ok {
		return GetPersonPhoto401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	w := 0
	if req.Params.W != nil {
		w = *req.Params.W
	}
	path, _, err := h.Images.PersonPhoto(ctx, req.PersonId, w)
	if err != nil {
		if !errors.Is(err, images.ErrNotFound) {
			slog.WarnContext(ctx, "person photo", "person", req.PersonId, "err", err)
		}
		return GetPersonPhoto404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "photo not found"))}, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, internal(ctx, "photo", err)
	}
	st, _ := f.Stat()
	return GetPersonPhoto200ImagejpegResponse{Body: f, ContentLength: st.Size(), Headers: GetPersonPhoto200ResponseHeaders{CacheControl: ptr(immutable)}}, nil
}

// ---------- activity ----------

func (h *Handlers) GetActivity(ctx context.Context, _ GetActivityRequestObject) (GetActivityResponseObject, error) {
	sess, ok := session(ctx)
	if !ok {
		return GetActivity401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	out := Activity{ServerVersion: h.Version, Tasks: []ActivityTask{}}
	if !sess.User.IsAdmin || h.Scans == nil {
		return GetActivity200JSONResponse(out), nil
	}
	if h.Playback != nil {
		for _, s := range h.Playback.List() {
			if s.Preloading() {
				continue
			}
			snap := s.Snapshot()
			verb := map[string]string{"paused": "paused"}[snap.State]
			if verb == "" {
				verb = "playing"
			}
			out.Tasks = append(out.Tasks, ActivityTask{Id: "stream:" + s.ID, Kind: Stream, State: ActivityTaskStateRunning,
				Title: fmt.Sprintf("%s is %s %s", s.UserName, verb, s.Title),
				Progress: &ScanProgress{Phase: Probing, Done: int(snap.PositionMS / 1000), Total: int(s.Media.DurationMS / 1000),
					Current: ptr(fmt.Sprintf("%s · %s · %d kbps", s.Decision.String(), map[bool]string{true: "Remote", false: "Local"}[s.Remote], s.BitrateKbps()))}})
		}
	}
	if h.Plex != nil {
		if running, p, _ := h.Plex.Status(); running {
			out.Tasks = append(out.Tasks, ActivityTask{Id: "import:plex", Kind: Import, State: ActivityTaskStateRunning,
				Title:    "Importing from Plex: " + p.Step,
				Progress: &ScanProgress{Phase: Metadata, Done: p.Done, Total: p.Total}})
		}
	}
	if h.Tasks != nil {
		for _, t := range h.Tasks.Running() {
			if t.Progress == nil {
				continue // short tasks aren't worth showing
			}
			done, total := t.Progress()
			out.Tasks = append(out.Tasks, ActivityTask{Id: "task:" + t.ID, Kind: Task, State: ActivityTaskStateRunning, Title: t.Name,
				Progress: &ScanProgress{Phase: Metadata, Done: done, Total: total}})
		}
	}
	active := h.Scans.Active()
	if len(active) == 0 {
		return GetActivity200JSONResponse(out), nil
	}
	libs, err := h.Libraries.List(ctx)
	if err != nil {
		return nil, internal(ctx, "activity", err)
	}
	for _, l := range libs { // library order keeps the list stable
		st, ok := active[l.ID]
		if !ok {
			continue
		}
		t := ActivityTask{Id: fmt.Sprintf("scan:%d", l.ID), Kind: Scan, LibraryId: ptr(l.ID), State: ActivityTaskStateQueued}
		t.Title = "Waiting to scan " + l.Name
		if st.State == "scanning" {
			t.State = ActivityTaskStateRunning
			t.Title = "Scanning " + l.Name
			if p := st.Progress; p != nil {
				if p.Phase == "metadata" {
					t.Title = "Getting metadata for " + l.Name
				}
				t.Progress = &ScanProgress{Phase: ScanProgressPhase(p.Phase), Done: p.Done, Total: p.Total, Current: nz(p.Current)}
			}
		}
		out.Tasks = append(out.Tasks, t)
	}
	return GetActivity200JSONResponse(out), nil
}

// ---------- logs ----------

func (h *Handlers) GetLogs(ctx context.Context, req GetLogsRequestObject) (GetLogsResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return GetLogs401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return GetLogs403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	out := GetLogs200JSONResponse{}
	if h.Logs == nil {
		return out, nil
	}
	min := slog.LevelInfo
	if req.Params.Level != nil {
		min.UnmarshalText([]byte(*req.Params.Level))
	}
	limit := 500
	if req.Params.Limit != nil && *req.Params.Limit > 0 && *req.Params.Limit <= 5000 {
		limit = *req.Params.Limit
	}
	for _, e := range h.Logs.Recent(min, limit) {
		lvl := LogEntryLevel(strings.ToLower(e.Level.String()))
		le := LogEntry{Time: e.Time, Level: lvl, Message: e.Message}
		if len(e.Attrs) > 0 {
			attrs := e.Attrs
			le.Attrs = &attrs
		}
		out = append(out, le)
	}
	return out, nil
}
