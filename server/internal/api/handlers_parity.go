package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"marquee/internal/auth"
	"marquee/internal/items"
	"marquee/internal/library"
	"marquee/internal/metadata"
)

// ---------- smart collections (META-7, D86) ----------

func smartFromAPI(r SmartCollectionRules) items.SmartCollection {
	sc := items.SmartCollection{ItemType: string(r.ItemType)}
	f := &sc.Filter
	set(&sc.Sort, r.Sort)
	set(&sc.Max, r.Limit)
	if r.Watch != nil {
		f.Watch = string(*r.Watch)
	}
	if r.Resolution != nil {
		f.Resolution = string(*r.Resolution)
	}
	set(&f.Genre, r.Genre)
	set(&f.Decade, r.Decade)
	set(&f.ContentRating, r.ContentRating)
	set(&f.HDR, r.Hdr)
	set(&f.YearFrom, r.YearFrom)
	set(&f.YearTo, r.YearTo)
	set(&f.AddedDays, r.AddedDays)
	set(&f.Studio, r.Studio)
	set(&f.PersonID, r.PersonId)
	if r.MinRating != nil {
		f.MinRating = float64(*r.MinRating)
	}
	return sc
}

func smartToAPI(sc items.SmartCollection) SmartCollectionRules {
	f := sc.Filter
	out := SmartCollectionRules{ItemType: SmartCollectionRulesItemType(sc.ItemType), Sort: nz(sc.Sort), Limit: nz(sc.Max),
		Genre: nz(f.Genre), Decade: nz(f.Decade), ContentRating: nz(f.ContentRating), Hdr: nz(f.HDR), YearFrom: nz(f.YearFrom),
		YearTo: nz(f.YearTo), AddedDays: nz(f.AddedDays), Studio: nz(f.Studio), PersonId: nz(f.PersonID)}
	if f.Watch != "" {
		out.Watch = ptr(SmartCollectionRulesWatch(f.Watch))
	}
	if f.Resolution != "" {
		out.Resolution = ptr(SmartCollectionRulesResolution(f.Resolution))
	}
	if f.MinRating > 0 {
		out.MinRating = ptr(float32(f.MinRating))
	}
	return out
}

func (h *Handlers) CreateSmartCollection(ctx context.Context, req CreateSmartCollectionRequestObject) (CreateSmartCollectionResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return CreateSmartCollection401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return CreateSmartCollection403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	if _, err := h.Libraries.Get(ctx, req.LibraryId); err != nil {
		return CreateSmartCollection404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "library not found"))}, nil
	}
	sc := smartFromAPI(req.Body.Rules)
	sc.LibraryID = req.LibraryId
	title := ""
	set(&title, req.Body.Title)
	id, err := h.Items.SaveSmart(ctx, sc, title)
	if err != nil {
		return CreateSmartCollection400JSONResponse{BadRequestJSONResponse(apiErr("invalid", err.Error()))}, nil
	}
	d, err := h.Items.Get(ctx, access(ctx), id, false)
	if err != nil {
		return nil, internal(ctx, "createSmartCollection", err)
	}
	return CreateSmartCollection201JSONResponse(toAPISummary(d.Summary)), nil
}

func (h *Handlers) UpdateSmartCollection(ctx context.Context, req UpdateSmartCollectionRequestObject) (UpdateSmartCollectionResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return UpdateSmartCollection401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return UpdateSmartCollection403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	d, err := h.Items.Get(ctx, access(ctx), req.CollectionId, false)
	if err != nil || d.Type != "collection" {
		return UpdateSmartCollection404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "collection not found"))}, nil
	}
	sc := smartFromAPI(req.Body.Rules)
	sc.ID, sc.LibraryID = d.ID, d.LibraryID
	title := ""
	set(&title, req.Body.Title)
	if _, err := h.Items.SaveSmart(ctx, sc, title); err != nil {
		return UpdateSmartCollection400JSONResponse{BadRequestJSONResponse(apiErr("invalid", err.Error()))}, nil
	}
	d, err = h.Items.Get(ctx, access(ctx), d.ID, false)
	if err != nil {
		return nil, internal(ctx, "updateSmartCollection", err)
	}
	return UpdateSmartCollection200JSONResponse(toAPISummary(d.Summary)), nil
}

// ---------- Home layout (USER-12) ----------

type savedRow struct {
	ID     string `json:"id"`
	Hidden bool   `json:"hidden,omitempty"`
}

// homeRow is a row Home can show; fill loads its items.
type homeRow struct {
	id, title string
	lib       int64
	hidden    bool
	pinned    bool
	fill      func() ([]items.Summary, error)
	// expand, when set, makes the row several hubs (because-you-watched; USER-16).
	expand func() ([]homeRow, error)
}

func (h *Handlers) savedLayout(ctx context.Context, uid int64) []savedRow {
	var raw string
	h.DB.QueryRowContext(ctx, `SELECT layout FROM user_home_layout WHERE user_id = ?`, uid).Scan(&raw)
	var l struct {
		Rows []savedRow `json:"rows"`
	}
	json.Unmarshal([]byte(raw), &l)
	return l.Rows
}

// pinnedRow resolves a pinned collection-<id> or playlist-<id> the person can see.
func (h *Handlers) pinnedRow(ctx context.Context, uid int64, id string) (homeRow, bool) {
	kind, num, _ := strings.Cut(id, "-")
	n, err := strconv.ParseInt(num, 10, 64)
	if err != nil {
		return homeRow{}, false
	}
	acc := access(ctx)
	switch kind {
	case "collection":
		d, err := h.Items.Get(ctx, acc, n, false)
		if err != nil || d.Type != "collection" || !canSeeLibrary(ctx, d.LibraryID) {
			return homeRow{}, false
		}
		return homeRow{id: id, title: d.Title, lib: d.LibraryID, pinned: true, fill: func() ([]items.Summary, error) {
			list, _, err := h.Items.Children(ctx, acc, n, 0, 20)
			return list, err
		}}, true
	case "playlist":
		p, err := h.Items.Playlist(ctx, uid, n)
		if err != nil {
			return homeRow{}, false
		}
		return homeRow{id: id, title: p.Title, pinned: true, fill: func() ([]items.Summary, error) {
			list, _, err := h.Items.PlaylistItems(ctx, acc, n, 0, 20)
			out := make([]items.Summary, len(list))
			for i, e := range list {
				out[i] = e.Item
			}
			return out, err
		}}, true
	}
	return homeRow{}, false
}

// homeRows is every row Home can show, arranged by the person's layout.
func (h *Handlers) homeRows(ctx context.Context, uid int64, saved []savedRow) ([]homeRow, error) {
	acc := access(ctx)
	libs, err := h.Libraries.List(ctx)
	if err != nil {
		return nil, err
	}
	var home []int64
	for _, l := range libs {
		if canSeeLibrary(ctx, l.ID) && (l.Options.IncludeInHome == nil || *l.Options.IncludeInHome) {
			home = append(home, l.ID)
		}
	}
	defaults := []homeRow{
		{id: "continue-watching", title: "Continue Watching", fill: func() ([]items.Summary, error) { return h.Items.ContinueWatching(ctx, acc, home, 20) }},
		{id: "watchlist", title: "Your Watchlist", fill: func() ([]items.Summary, error) { return h.Items.Watchlist(ctx, acc, 30) }},
	}
	// Recommendations (USER-16). Always offered, so a saved layout keeps their place; they
	// stay empty until movies and shows are embedded.
	{
		home := append([]int64{}, home...) // none means none, not all
		defaults = append(defaults,
			homeRow{id: "recommended", title: "Recommended for You", fill: func() ([]items.Summary, error) {
				idx := h.videoIndex()
				if idx.Len() == 0 {
					return nil, nil
				}
				return h.Items.Recommended(ctx, acc, idx, home, 20)
			}},
			homeRow{id: "because-you-watched", title: "Because You Watched", expand: func() ([]homeRow, error) {
				idx := h.videoIndex()
				if idx.Len() == 0 {
					return nil, nil
				}
				rows, err := h.Items.BecauseYouWatched(ctx, acc, idx, home, 2, 20)
				out := make([]homeRow, len(rows))
				for i, b := range rows {
					list := b.Items
					out[i] = homeRow{id: fmt.Sprintf("because-%d", b.Seed.ID), title: "Because you watched " + b.Seed.Title,
						fill: func() ([]items.Summary, error) { return list, nil }}
				}
				return out, err
			}})
	}
	for _, l := range libs {
		if !canSeeLibrary(ctx, l.ID) || (l.Options.IncludeInHome != nil && !*l.Options.IncludeInHome) {
			continue
		}
		defaults = append(defaults, homeRow{id: fmt.Sprintf("recent-%d", l.ID), title: "Recently Added " + l.Name, lib: l.ID,
			fill: func() ([]items.Summary, error) { return h.Items.RecentlyAdded(ctx, acc, l.ID, string(l.Type), 20) }})
		if l.Type == library.Music {
			defaults = append(defaults, homeRow{id: fmt.Sprintf("played-%d", l.ID), title: "Recently Played in " + l.Name, lib: l.ID,
				fill: func() ([]items.Summary, error) { return h.Items.RecentlyPlayedAlbums(ctx, acc, l.ID, 20) }})
		}
	}
	byID := map[string]homeRow{}
	for _, r := range defaults {
		byID[r.id] = r
	}
	var out []homeRow
	used := map[string]bool{}
	for _, s := range saved {
		if used[s.ID] {
			continue
		}
		r, ok := byID[s.ID]
		if !ok {
			if r, ok = h.pinnedRow(ctx, uid, s.ID); !ok {
				continue // a library or collection that's gone
			}
		}
		used[s.ID] = true
		r.hidden = s.Hidden
		out = append(out, r)
	}
	for _, r := range defaults {
		if !used[r.id] {
			out = append(out, r) // new rows show up at the end
		}
	}
	return out, nil
}

func layoutToAPI(rows []homeRow) HomeLayout {
	out := HomeLayout{Rows: make([]HomeLayoutRow, len(rows))}
	for i, r := range rows {
		out.Rows[i] = HomeLayoutRow{Id: r.id, Title: ptr(r.title), Hidden: ptr(r.hidden), Pinned: ptr(r.pinned)}
	}
	return out
}

func (h *Handlers) HomeHubs(ctx context.Context, _ HomeHubsRequestObject) (HomeHubsResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return HomeHubs401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	rows, err := h.homeRows(ctx, s.User.ID, h.savedLayout(ctx, s.User.ID))
	if err != nil {
		return nil, internal(ctx, "hubs", err)
	}
	out := HomeHubs200JSONResponse{}
	for _, r := range rows {
		if r.hidden {
			continue
		}
		subs := []homeRow{r}
		if r.expand != nil {
			if subs, err = r.expand(); err != nil {
				return nil, internal(ctx, "hubs", err)
			}
		}
		for _, r := range subs {
			list, err := r.fill()
			if err != nil {
				return nil, internal(ctx, "hubs", err)
			}
			if len(list) == 0 {
				continue
			}
			hub := Hub{Id: r.id, Title: r.title, LibraryId: nz(r.lib), Items: make([]ItemSummary, len(list))}
			for i, it := range list {
				hub.Items[i] = toAPISummary(it)
			}
			out = append(out, hub)
		}
	}
	return out, nil
}

func (h *Handlers) GetHomeLayout(ctx context.Context, _ GetHomeLayoutRequestObject) (GetHomeLayoutResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return GetHomeLayout401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	rows, err := h.homeRows(ctx, s.User.ID, h.savedLayout(ctx, s.User.ID))
	if err != nil {
		return nil, internal(ctx, "homeLayout", err)
	}
	return GetHomeLayout200JSONResponse(layoutToAPI(rows)), nil
}

func (h *Handlers) SetHomeLayout(ctx context.Context, req SetHomeLayoutRequestObject) (SetHomeLayoutResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return SetHomeLayout401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	if len(req.Body.Rows) > 200 {
		return SetHomeLayout400JSONResponse{BadRequestJSONResponse(apiErr("invalid", "too many rows"))}, nil
	}
	saved := make([]savedRow, 0, len(req.Body.Rows))
	for _, r := range req.Body.Rows {
		saved = append(saved, savedRow{ID: r.Id, Hidden: r.Hidden != nil && *r.Hidden})
	}
	// Keep only rows that exist (and pinned ones the person may see).
	rows, err := h.homeRows(ctx, s.User.ID, saved)
	if err != nil {
		return nil, internal(ctx, "homeLayout", err)
	}
	if len(saved) == 0 {
		_, err = h.DB.ExecContext(ctx, `DELETE FROM user_home_layout WHERE user_id = ?`, s.User.ID)
	} else {
		keep := make([]savedRow, len(rows))
		for i, r := range rows {
			keep[i] = savedRow{ID: r.id, Hidden: r.hidden}
		}
		data, _ := json.Marshal(map[string]any{"rows": keep})
		_, err = h.DB.ExecContext(ctx, `INSERT INTO user_home_layout (user_id, layout) VALUES (?, ?)
			ON CONFLICT (user_id) DO UPDATE SET layout = excluded.layout`, s.User.ID, string(data))
	}
	if err != nil {
		return nil, internal(ctx, "homeLayout", err)
	}
	return SetHomeLayout200JSONResponse(layoutToAPI(rows)), nil
}

// ---------- cinema trailers (PLAY-18) ----------

func (h *Handlers) ListPrerolls(ctx context.Context, req ListPrerollsRequestObject) (ListPrerollsResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return ListPrerolls401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	acc := access(ctx)
	d, err := h.Items.Get(ctx, acc, req.ItemId, false)
	if errors.Is(err, items.ErrNotFound) {
		return ListPrerolls404JSONResponse{NotFoundJSONResponse(apiErr("not_found", err.Error()))}, nil
	}
	if err != nil {
		return nil, internal(ctx, "prerolls", err)
	}
	out := ListPrerolls200JSONResponse{}
	cfg := h.Settings.Get().Cinema
	optedOut := s.User.Preferences.CinemaTrailers != nil && !*s.User.Preferences.CinemaTrailers
	if d.Type != "movie" || optedOut {
		return out, nil
	}
	trailers, err := h.Items.Trailers(ctx, acc, d.ID, cfg.Trailers)
	if err != nil {
		return nil, internal(ctx, "prerolls", err)
	}
	for _, t := range trailers {
		out = append(out, toAPISummary(t))
	}
	if cfg.PrerollItemID != nil {
		if p, err := h.Items.Get(ctx, acc, *cfg.PrerollItemID, false); err == nil && p.ID != d.ID {
			out = append(out, toAPISummary(p.Summary))
		}
	}
	return out, nil
}

// ---------- sharing (USER-13) ----------

func toAPIInvite(in auth.Invite) Invite {
	return Invite{Id: in.ID, Note: nz(in.Note), CreatedAt: in.CreatedAt, ExpiresAt: ptr(in.ExpiresAt), UsedAt: in.UsedAt,
		UsedBy: nz(in.UsedBy), Restrictions: toAPIRestrictions(in.Restrictions)}
}

func (h *Handlers) ListInvites(ctx context.Context, _ ListInvitesRequestObject) (ListInvitesResponseObject, error) {
	switch authed, admin := adminCheck(ctx); {
	case !authed:
		return ListInvites401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return ListInvites403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	list, err := h.Auth.ListInvites(ctx)
	if err != nil {
		return nil, internal(ctx, "invites", err)
	}
	out := ListInvites200JSONResponse{}
	for _, in := range list {
		out = append(out, toAPIInvite(in))
	}
	return out, nil
}

func (h *Handlers) CreateInvite(ctx context.Context, req CreateInviteRequestObject) (CreateInviteResponseObject, error) {
	s, _ := session(ctx)
	switch authed, admin := adminCheck(ctx); {
	case !authed:
		return CreateInvite401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return CreateInvite403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	b := req.Body
	r, err := fromAPIRestrictions(b.Restrictions)
	if err != nil {
		return CreateInvite400JSONResponse{BadRequestJSONResponse(apiErr("invalid", err.Error()))}, nil
	}
	days := 7
	set(&days, b.ExpiresDays)
	days = min(max(days, 1), 90)
	note := ""
	set(&note, b.Note)
	in, token, err := h.Auth.CreateInvite(ctx, s.User.ID, note, time.Duration(days)*24*time.Hour, r)
	if err != nil {
		return nil, internal(ctx, "createInvite", err)
	}
	slog.InfoContext(ctx, "invite created", "by", s.User.Username, "note", note)
	return CreateInvite201JSONResponse{Invite: toAPIInvite(in), Token: token, Path: "/join/" + token}, nil
}

func (h *Handlers) DeleteInvite(ctx context.Context, req DeleteInviteRequestObject) (DeleteInviteResponseObject, error) {
	switch authed, admin := adminCheck(ctx); {
	case !authed:
		return DeleteInvite401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return DeleteInvite403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	if err := h.Auth.DeleteInvite(ctx, req.InviteId); errors.Is(err, auth.ErrInviteInvalid) {
		return DeleteInvite404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "invite not found"))}, nil
	} else if err != nil {
		return nil, internal(ctx, "deleteInvite", err)
	}
	return DeleteInvite204Response{}, nil
}

func (h *Handlers) GetInvitation(ctx context.Context, req GetInvitationRequestObject) (GetInvitationResponseObject, error) {
	in, err := h.Auth.LookupInvite(ctx, requestInfo(ctx).ClientIP, req.Token)
	var rl *auth.RateLimitedError
	switch {
	case errors.As(err, &rl):
		return GetInvitation429JSONResponse{TooManyRequestsJSONResponse(apiErr("rate_limited", rl.Error()))}, nil
	case errors.Is(err, auth.ErrInviteInvalid):
		return GetInvitation404JSONResponse{NotFoundJSONResponse(apiErr("invalid_invite", err.Error()))}, nil
	case err != nil:
		return nil, internal(ctx, "invitation", err)
	}
	return GetInvitation200JSONResponse{ServerName: h.Settings.Get().General.ServerName, InvitedBy: nz(in.InvitedBy),
		Note: nz(in.Note), ExpiresAt: in.ExpiresAt}, nil
}

func (h *Handlers) AcceptInvitation(ctx context.Context, req AcceptInvitationRequestObject) (AcceptInvitationResponseObject, error) {
	b := req.Body
	if !validDevice(b.Device) {
		return AcceptInvitation400JSONResponse{BadRequestJSONResponse(apiErr("invalid_device", "device clientId and name are required"))}, nil
	}
	n := auth.NewUser{Username: strings.TrimSpace(b.Username), Password: b.Password}
	set(&n.DisplayName, b.DisplayName)
	n.DisplayName = strings.TrimSpace(n.DisplayName)
	if n.Username == "" || len(n.Username) > 64 {
		return AcceptInvitation400JSONResponse{BadRequestJSONResponse(apiErr("invalid", "username must be 1–64 characters"))}, nil
	}
	if len(n.Password) < 8 {
		return AcceptInvitation400JSONResponse{BadRequestJSONResponse(apiErr("invalid", "password must be at least 8 characters"))}, nil
	}
	ip := requestInfo(ctx).ClientIP
	u, err := h.Auth.AcceptInvite(ctx, ip, req.Token, n)
	var rl *auth.RateLimitedError
	switch {
	case errors.As(err, &rl):
		return AcceptInvitation429JSONResponse{TooManyRequestsJSONResponse(apiErr("rate_limited", rl.Error()))}, nil
	case errors.Is(err, auth.ErrInviteInvalid):
		return AcceptInvitation404JSONResponse{NotFoundJSONResponse(apiErr("invalid_invite", err.Error()))}, nil
	case errors.Is(err, auth.ErrUsernameTaken):
		return AcceptInvitation409JSONResponse{ConflictJSONResponse(apiErr("username_taken", err.Error()))}, nil
	case err != nil:
		return nil, internal(ctx, "acceptInvitation", err)
	}
	slog.InfoContext(ctx, "invite accepted", "username", u.Username, "ip", ip)
	token, err := h.Auth.IssueToken(ctx, h.DB, u.ID, toDevice(b.Device), ip)
	if err != nil {
		return nil, internal(ctx, "acceptInvitation", err)
	}
	return AcceptInvitation200JSONResponse{Token: token, User: toAPIUser(u)}, nil
}

// GetItemTrailer finds a movie's or show's trailer (PLAY-22). Nothing is downloaded: a
// YouTube trailer is only looked up, and the client plays it from YouTube.
func (h *Handlers) GetItemTrailer(ctx context.Context, req GetItemTrailerRequestObject) (GetItemTrailerResponseObject, error) {
	if _, ok := session(ctx); !ok {
		return GetItemTrailer401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	if h.Metadata == nil || h.Items.Visible(ctx, access(ctx), req.ItemId) != nil {
		return GetItemTrailer404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "item not found"))}, nil
	}
	t, err := h.Metadata.Trailer(ctx, req.ItemId)
	switch {
	case errors.Is(err, metadata.ErrNoTrailer), errors.Is(err, sql.ErrNoRows):
		return GetItemTrailer404JSONResponse{NotFoundJSONResponse(apiErr("no_trailer", "No trailer found for this title."))}, nil
	case err != nil:
		return GetItemTrailer404JSONResponse{NotFoundJSONResponse(apiErr("no_trailer", "Couldn't look up the trailer right now."))}, nil
	}
	if t.LocalItemID != 0 {
		return GetItemTrailer200JSONResponse{Source: ItemTrailerSourceLocal, ItemId: ptr(t.LocalItemID), Name: nz(t.Name)}, nil
	}
	return GetItemTrailer200JSONResponse{Source: ItemTrailerSourceYoutube, YoutubeKey: ptr(t.YouTubeKey), Url: ptr("https://www.youtube.com/watch?v=" + t.YouTubeKey),
		Name: nz(t.Name)}, nil
}
