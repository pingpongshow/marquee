package api

import (
	"context"
	"errors"
	"time"

	"marquee/internal/auth"
	"marquee/internal/intown"
	"marquee/internal/items"
)

// In Town (MUSIC-27): concerts for you, from the admin's own Ticketmaster key.

func (h *Handlers) inTownOn() bool { return h.InTown != nil && h.InTown.Enabled() }

func inTownAttribution() InTownAttribution {
	return InTownAttribution{Name: "Ticketmaster", Url: "https://www.ticketmaster.com", Text: "Show data from Ticketmaster"}
}

func toIntownLocation(l *auth.Location) *intown.Location {
	if l == nil {
		return nil
	}
	return &intown.Location{PostalCode: l.PostalCode, City: l.City, CountryCode: l.CountryCode, Distance: l.Distance, Unit: l.Unit}
}

func toAPILocation(l auth.Location) InTownLocation {
	return InTownLocation{PostalCode: nz(l.PostalCode), City: nz(l.City), CountryCode: l.CountryCode, Distance: ptr(l.Distance), Unit: ptr(InTownLocationUnit(l.Unit))}
}

// toAPIShow is a show for any app: times in ISO 8601 plus the date and time at the venue.
func toAPIShow(v intown.ShowView, artists map[int64]ItemSummary) InTownShow {
	out := InTownShow{Id: v.ID, Title: v.Title, ArtistName: v.Act, Lineup: []string{}, Support: v.Support, LocalDate: v.LocalDate,
		LocalTime: nz(v.LocalTime), TimeZone: nz(v.TimeZone), VenueName: v.Venue.Name, City: v.Venue.City, Region: nz(v.Venue.Region),
		CountryCode: v.Venue.Country, Genre: nz(v.Genre), Status: InTownShowStatus(v.Status), TicketsUrl: v.URL, Source: v.Source,
		Reason: nz(v.Reason)}
	for _, a := range v.Acts {
		out.Lineup = append(out.Lineup, a.Name)
	}
	if v.ItemID != 0 {
		out.ArtistItemId = ptr(v.ItemID)
		if it, ok := artists[v.ItemID]; ok {
			out.Artist = &it
		}
	}
	if !v.StartsAt.IsZero() {
		out.StartsAt = ptr(v.StartsAt)
	}
	if v.HasDistance {
		out.Distance, out.DistanceUnit = ptr(v.Distance), ptr(InTownShowDistanceUnit(v.Unit))
	}
	if v.Near {
		out.Near = ptr(true)
	}
	if v.ReasonKind != "" {
		out.ReasonKind = ptr(InTownReasonKind(v.ReasonKind))
	}
	return out
}

func (h *Handlers) GetInTown(ctx context.Context, _ GetInTownRequestObject) (GetInTownResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return GetInTown401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	// Kids don't get In Town: nothing about it, and nothing sent on their behalf, whatever
	// location they had before they were made one.
	if s.User.IsKid() {
		return GetInTown200JSONResponse{Artists: []InTownArtist{}, Near: []InTownShow{}, Attribution: inTownAttribution()}, nil
	}
	loc := s.User.Preferences.Location
	out := GetInTown200JSONResponse{Enabled: false, NeedsLocation: loc == nil, Artists: []InTownArtist{}, Near: []InTownShow{}, Attribution: inTownAttribution()}
	if loc != nil {
		out.Location = ptr(toAPILocation(*loc))
	}
	if !h.inTownOn() {
		return out, nil
	}
	out.Enabled = true
	in, err := h.inTownInput(ctx, access(ctx), toIntownLocation(loc))
	if err != nil {
		return nil, internal(ctx, "inTown", err)
	}
	page, err := h.InTown.BuildPage(ctx, in)
	if intown.IsOff(err) { // turned off this moment
		out.Enabled = false
		return out, nil
	}
	if err != nil {
		return nil, internal(ctx, "inTown", err)
	}
	if loc != nil {
		out.LocationFound = ptr(page.LocationFound)
	}
	out.NearError = nz(page.NearError)
	// Artwork and links for artists in the libraries.
	ids := []int64{}
	seen := map[int64]bool{}
	note := func(v intown.ShowView) {
		if v.ItemID != 0 && !seen[v.ItemID] {
			seen[v.ItemID] = true
			ids = append(ids, v.ItemID)
		}
	}
	for _, a := range page.Artists {
		if !seen[a.ItemID] {
			seen[a.ItemID] = true
			ids = append(ids, a.ItemID)
		}
	}
	for _, v := range page.Near {
		note(v)
	}
	list, err := h.Items.ByIDs(ctx, access(ctx), ids)
	if err != nil {
		return nil, internal(ctx, "inTown", err)
	}
	artists := make(map[int64]ItemSummary, len(list))
	for _, it := range list {
		artists[it.ID] = toAPISummary(it)
	}
	for _, a := range page.Artists {
		summary, ok := artists[a.ItemID]
		if !ok {
			continue
		}
		row := InTownArtist{Artist: summary, ShowCount: a.ShowCount, NearCount: a.NearCount, Shows: make([]InTownShow, len(a.Shows)), Reason: nz(a.Reason)}
		if a.ReasonKind != "" {
			row.ReasonKind = ptr(InTownReasonKind(a.ReasonKind))
		}
		for i, v := range a.Shows {
			row.Shows[i] = toAPIShow(v, nil) // the artist is already on the row
		}
		out.Artists = append(out.Artists, row)
	}
	for _, v := range page.Near {
		out.Near = append(out.Near, toAPIShow(v, artists))
	}
	return out, nil
}

// inTownInput gathers what the page goes by, limited to what the person may see.
func (h *Handlers) inTownInput(ctx context.Context, acc items.Access, loc *intown.Location) (intown.PageInput, error) {
	in := intown.PageInput{Location: loc}
	var err error
	if in.Artists, err = h.Items.MusicArtists(ctx); err != nil {
		return in, err
	}
	if in.Visible, err = h.Items.VisibleMusicArtists(ctx, acc); err != nil {
		return in, err
	}
	if in.Liking, err = h.Items.ArtistLikings(ctx, acc.UserID, in.Visible); err != nil {
		return in, err
	}
	if h.Soundprint != nil {
		in.Similar = h.Soundprint.SimilarArtists
		in.SoundAlike = func(liked []int64) map[int64]int64 { return h.Soundprint.SoundAlike(liked, keepFor(acc, nil)) }
	}
	return in, nil
}

func (h *Handlers) GetItemShows(ctx context.Context, req GetItemShowsRequestObject) (GetItemShowsResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return GetItemShows401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	if err := h.Items.Visible(ctx, itemAccess(ctx), req.ItemId); errors.Is(err, items.ErrNotFound) {
		return GetItemShows404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "item not found"))}, nil
	} else if err != nil {
		return nil, internal(ctx, "itemShows", err)
	}
	if s.User.IsKid() {
		return GetItemShows200JSONResponse{Shows: []InTownShow{}, Attribution: inTownAttribution()}, nil
	}
	loc := s.User.Preferences.Location
	out := GetItemShows200JSONResponse{Enabled: h.inTownOn(), NeedsLocation: loc == nil, Shows: []InTownShow{}, Attribution: inTownAttribution()}
	if !out.Enabled {
		return out, nil
	}
	artist, isArtist, err := h.Items.MusicArtistByID(ctx, req.ItemId)
	if err != nil {
		return nil, internal(ctx, "itemShows", err)
	}
	if !isArtist {
		return out, nil
	}
	shows, near, err := h.InTown.ArtistShows(ctx, artist.Name, artist.MBID, toIntownLocation(loc))
	if intown.IsOff(err) {
		out.Enabled = false
		return out, nil
	}
	if err != nil {
		return nil, internal(ctx, "itemShows", err)
	}
	out.NearCount = near
	for _, v := range shows {
		v.ItemID = req.ItemId
		out.Shows = append(out.Shows, toAPIShow(v, nil))
	}
	// A stale artist is looked up behind the page, which has its answer already.
	go func() {
		bg, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		h.InTown.RefreshArtist(bg, artist.Name, artist.MBID)
	}()
	return out, nil
}

func (h *Handlers) GetInTownStatus(ctx context.Context, _ GetInTownStatusRequestObject) (GetInTownStatusResponseObject, error) {
	switch authed, adm := isAdmin(ctx); {
	case !authed:
		return GetInTownStatus401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !adm:
		return GetInTownStatus403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	cfg := h.Settings.Get().InTown
	out := GetInTownStatus200JSONResponse{Enabled: h.inTownOn(), KeySet: cfg.TicketmasterKey != "", DailyBudget: cfg.DailyBudget}
	if h.InTown == nil {
		return out, nil
	}
	st := h.InTown.Status(ctx)
	out.Running, out.ArtistsTotal, out.ArtistsChecked, out.ArtistsMatched = st.Running, st.ArtistsTotal, st.ArtistsChecked, st.ArtistsMatch
	out.ShowsCached, out.CallsToday, out.LastError = st.ShowsCached, st.CallsToday, nz(st.LastError)
	if !st.LastSync.IsZero() {
		out.LastSyncAt = ptr(st.LastSync)
	}
	if !st.Backoff.IsZero() {
		out.BackoffUntil = ptr(st.Backoff)
	}
	if st.QuotaLimit >= 0 {
		out.QuotaLimit = ptr(st.QuotaLimit)
	}
	if st.QuotaRemaining >= 0 {
		out.QuotaRemaining = ptr(st.QuotaRemaining)
	}
	return out, nil
}

func (h *Handlers) GetMyLocation(ctx context.Context, _ GetMyLocationRequestObject) (GetMyLocationResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return GetMyLocation401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	if s.User.IsKid() {
		return GetMyLocation403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	out := GetMyLocation200JSONResponse{}
	if l := s.User.Preferences.Location; l != nil {
		out.Location = ptr(toAPILocation(*l))
	}
	return out, nil
}

func (h *Handlers) SetMyLocation(ctx context.Context, req SetMyLocationRequestObject) (SetMyLocationResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return SetMyLocation401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	if s.User.IsKid() {
		return SetMyLocation403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	in := intown.Location{CountryCode: req.Body.CountryCode}
	set(&in.PostalCode, req.Body.PostalCode)
	set(&in.City, req.Body.City)
	set(&in.Distance, req.Body.Distance)
	if req.Body.Unit != nil {
		in.Unit = string(*req.Body.Unit)
	}
	clean, err := intown.Clean(in)
	if err != nil {
		return SetMyLocation400JSONResponse{BadRequestJSONResponse(apiErr("invalid", err.Error()))}, nil
	}
	loc := auth.Location{PostalCode: clean.PostalCode, City: clean.City, CountryCode: clean.CountryCode, Distance: clean.Distance, Unit: clean.Unit}
	// Each new location spends calls, shared by everyone, so there's a limit to changing it.
	changed := s.User.Preferences.Location == nil || *s.User.Preferences.Location != loc
	if changed && h.inTownOn() && !h.InTown.AllowLocationChange(s.User.ID) {
		return SetMyLocation400JSONResponse{BadRequestJSONResponse(apiErr("too_many_changes", "You've changed your location a lot today. Try again tomorrow."))}, nil
	}
	prefs := s.User.Preferences
	prefs.Location = &loc
	if _, err := h.Auth.Update(ctx, s.User.ID, auth.UserUpdate{Preferences: &prefs}); err != nil {
		return nil, internal(ctx, "setLocation", err)
	}
	if changed && h.inTownOn() {
		go func() {
			bg, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			defer cancel()
			h.InTown.LocationSet(bg, clean)
		}()
	}
	return SetMyLocation200JSONResponse(toAPILocation(loc)), nil
}

func (h *Handlers) DeleteMyLocation(ctx context.Context, _ DeleteMyLocationRequestObject) (DeleteMyLocationResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return DeleteMyLocation401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	if s.User.IsKid() {
		return DeleteMyLocation403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	prefs := s.User.Preferences
	prefs.Location = nil
	if _, err := h.Auth.Update(ctx, s.User.ID, auth.UserUpdate{Preferences: &prefs}); err != nil {
		return nil, internal(ctx, "deleteLocation", err)
	}
	return DeleteMyLocation204Response{}, nil
}
