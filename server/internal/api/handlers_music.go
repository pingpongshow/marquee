package api

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"strings"

	"marquee/internal/items"
	"marquee/internal/lyrics"
	"marquee/internal/sonic"
)

// keepFor limits sonic results to libraries the user can see (and optionally one library).
func keepFor(acc items.Access, libraryID *int64) sonic.Filter {
	return func(t *sonic.Track) bool {
		if libraryID != nil && t.LibraryID != *libraryID {
			return false
		}
		return acc.LibraryIDs == nil || slices.Contains(acc.LibraryIDs, t.LibraryID)
	}
}

func (h *Handlers) station(ctx context.Context, st sonic.Station) (Station, error) {
	list, err := h.Items.ByIDs(ctx, access(ctx), st.IDs)
	if err != nil {
		return Station{}, err
	}
	return Station{Title: st.Title, Items: summaries(list)}, nil
}

func sonicErr(err error) (code string, msg string, status int) {
	switch {
	case errors.Is(err, sonic.ErrUnavailable):
		return "sonic_unavailable", "Sonic analysis isn't running on the server.", 503
	case errors.Is(err, sonic.ErrNotAnalyzed):
		return "not_analyzed", "This music hasn't been sonically analysed yet.", 409
	}
	return "", "", 0
}

func (h *Handlers) MusicStatus(ctx context.Context, _ MusicStatusRequestObject) (MusicStatusResponseObject, error) {
	if _, ok := session(ctx); !ok {
		return MusicStatus401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	st := h.Sonic.Status(ctx)
	return MusicStatus200JSONResponse{Enabled: st.Enabled, Available: st.Available, Running: st.Running, Model: nz(st.Model),
		Device: nz(st.Device), Analyzed: st.Analyzed, Total: st.Total, Failed: nz(st.Failed), Progress: nz(st.Progress), RunTotal: nz(st.RunTotal)}, nil
}

func (h *Handlers) SonicSimilar(ctx context.Context, req SonicSimilarRequestObject) (SonicSimilarResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return SonicSimilar401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	acc := access(ctx)
	d, err := h.Items.Get(ctx, acc, req.ItemId, false)
	if errors.Is(err, items.ErrNotFound) {
		return SonicSimilar404JSONResponse{NotFoundJSONResponse(apiErr("not_found", err.Error()))}, nil
	} else if err != nil {
		return nil, internal(ctx, "similar", err)
	}
	limit := 20
	set(&limit, req.Params.Limit)
	keep := keepFor(acc, nil)
	var ids []int64
	switch d.Type {
	case "track":
		t := h.Sonic.Index.Get(d.ID)
		if t == nil {
			return SonicSimilar409JSONResponse{ConflictJSONResponse(apiErr("not_analyzed", sonic.ErrNotAnalyzed.Error()))}, nil
		}
		ids = sonic.TrackIDs(h.Sonic.Index.Similar(t, limit, keep))
	case "album", "artist":
		col := map[string]func(*sonic.Track) int64{"album": func(t *sonic.Track) int64 { return t.AlbumID }, "artist": func(t *sonic.Track) int64 { return t.ArtistID }}[d.Type]
		members := h.Sonic.Index.Where(func(t *sonic.Track) bool { return col(t) == d.ID })
		if len(members) == 0 {
			return SonicSimilar409JSONResponse{ConflictJSONResponse(apiErr("not_analyzed", sonic.ErrNotAnalyzed.Error()))}, nil
		}
		for _, g := range h.Sonic.Index.SimilarGroups(sonic.Mean(members), d.Type == "album", d.ID, limit, keep) {
			ids = append(ids, g.ID)
		}
	default:
		return SonicSimilar200JSONResponse{}, nil
	}
	_ = s
	list, err := h.Items.ByIDs(ctx, acc, ids)
	if err != nil {
		return nil, internal(ctx, "similar", err)
	}
	return SonicSimilar200JSONResponse(summaries(list)), nil
}

func (h *Handlers) MusicRadio(ctx context.Context, req MusicRadioRequestObject) (MusicRadioResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return MusicRadio401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	b := req.Body
	acc := access(ctx)
	o := sonic.Options{Length: 50, Keep: keepFor(acc, b.LibraryId), Avoid: h.Sonic.Avoid(ctx, s.User.ID), Exclude: map[int64]bool{}}
	set(&o.Length, b.Limit)
	if b.Exclude != nil {
		for _, id := range *b.Exclude {
			o.Exclude[id] = true
		}
	}
	bad := func(msg string) (MusicRadioResponseObject, error) {
		return MusicRadio400JSONResponse{BadRequestJSONResponse(apiErr("invalid", msg))}, nil
	}
	value := ""
	set(&value, b.Value)
	var st sonic.Station
	var err error
	switch b.Seed {
	case RadioRequestSeedItem:
		if b.ItemId == nil {
			return bad("itemId is required")
		}
		d, gerr := h.Items.Get(ctx, acc, *b.ItemId, false)
		if gerr != nil {
			return MusicRadio400JSONResponse{BadRequestJSONResponse(apiErr("not_found", "item not found"))}, nil
		}
		st, err = h.Sonic.RadioFromItem(ctx, d.ID, d.Title, o)
	case RadioRequestSeedGenre:
		ids, gerr := h.Items.TrackIDsByGenre(ctx, acc, value)
		if gerr != nil {
			return nil, internal(ctx, "radio", gerr)
		}
		v := h.Sonic.MeanOf(ids)
		if v == nil {
			return MusicRadio409JSONResponse{ConflictJSONResponse(apiErr("not_analyzed", "no analysed tracks in "+value))}, nil
		}
		st = h.Sonic.RadioFromVector(v, value+" Radio", o)
	case RadioRequestSeedDecade:
		dec, perr := strconv.Atoi(strings.TrimSuffix(value, "s"))
		if perr != nil {
			return bad("decade must be a year like 1990")
		}
		inner := o.Keep
		o.Keep = func(t *sonic.Track) bool { return t.Year >= dec && t.Year < dec+10 && inner(t) }
		members := h.Sonic.Index.Where(o.Keep)
		if len(members) == 0 {
			return MusicRadio409JSONResponse{ConflictJSONResponse(apiErr("not_analyzed", "no analysed tracks from the "+value))}, nil
		}
		st = h.Sonic.RadioFromVector(sonic.Mean(members), strconv.Itoa(dec)+"s Radio", o)
	case RadioRequestSeedMood:
		if value == "" {
			return bad("value (the mood) is required")
		}
		v, verr := h.Sonic.TextVector(ctx, value+" music")
		if verr != nil {
			err = verr
			break
		}
		st = h.Sonic.RadioFromVector(v, strings.ToUpper(value[:1])+value[1:]+" Radio", o)
	case RadioRequestSeedFavourites:
		liked := h.Sonic.Liked(ctx, s.User.ID)
		if len(liked) == 0 {
			return MusicRadio409JSONResponse{ConflictJSONResponse(apiErr("no_history", "Play or rate some music first."))}, nil
		}
		st = h.Sonic.RadioFromVector(sonic.Mean(liked), "Your Favourites Radio", o)
	case RadioRequestSeedLibrary:
		all := h.Sonic.Index.Where(o.Keep)
		if len(all) == 0 {
			return MusicRadio409JSONResponse{ConflictJSONResponse(apiErr("not_analyzed", sonic.ErrNotAnalyzed.Error()))}, nil
		}
		// Library radio wanders: re-anchor on a random track every run.
		first := all[len(o.Exclude)%len(all)]
		if len(o.Exclude) == 0 {
			first = all[int(s.User.ID+int64(len(all)))%len(all)]
		}
		st = sonic.Station{Title: "Library Radio", IDs: sonic.TrackIDs(h.Sonic.Index.Flow(first.Vec, first, o))}
	default:
		return bad("unknown seed")
	}
	if err != nil {
		if code, msg, status := sonicErr(err); status == 503 {
			return MusicRadio503JSONResponse{ServiceUnavailableJSONResponse(apiErr(code, msg))}, nil
		} else if status == 409 {
			return MusicRadio409JSONResponse{ConflictJSONResponse(apiErr(code, msg))}, nil
		}
		return nil, internal(ctx, "radio", err)
	}
	out, err := h.station(ctx, st)
	if err != nil {
		return nil, internal(ctx, "radio", err)
	}
	return MusicRadio200JSONResponse(out), nil
}

func (h *Handlers) MusicSage(ctx context.Context, req MusicSageRequestObject) (MusicSageResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return MusicSage401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	prompt := strings.TrimSpace(req.Body.Prompt)
	if len(prompt) < 2 {
		return MusicSage400JSONResponse{BadRequestJSONResponse(apiErr("invalid", "describe the music you want"))}, nil
	}
	n := 30
	set(&n, req.Body.Limit)
	o := sonic.Options{Keep: keepFor(access(ctx), req.Body.LibraryId), Avoid: h.Sonic.Avoid(ctx, s.User.ID)}
	st, err := h.Sonic.Sage(ctx, prompt, n, o)
	if err != nil {
		if code, msg, status := sonicErr(err); status == 503 {
			return MusicSage503JSONResponse{ServiceUnavailableJSONResponse(apiErr(code, msg))}, nil
		}
		return nil, internal(ctx, "sage", err)
	}
	out, err := h.station(ctx, st)
	if err != nil {
		return nil, internal(ctx, "sage", err)
	}
	return MusicSage200JSONResponse(out), nil
}

func (h *Handlers) MusicAdventure(ctx context.Context, req MusicAdventureRequestObject) (MusicAdventureResponseObject, error) {
	if _, ok := session(ctx); !ok {
		return MusicAdventure401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	acc := access(ctx)
	b := req.Body
	for _, id := range []int64{b.FromId, b.ToId} {
		if err := h.Items.Visible(ctx, acc, id); err != nil {
			return MusicAdventure404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "track not found"))}, nil
		}
	}
	from, to := h.Sonic.Index.Get(b.FromId), h.Sonic.Index.Get(b.ToId)
	if from == nil || to == nil {
		return MusicAdventure409JSONResponse{ConflictJSONResponse(apiErr("not_analyzed", sonic.ErrNotAnalyzed.Error()))}, nil
	}
	n := 15
	set(&n, b.Length)
	ts := h.Sonic.Index.Adventure(from, to, n, sonic.Options{Keep: keepFor(acc, nil)})
	out, err := h.station(ctx, sonic.Station{Title: "Sonic Adventure", IDs: sonic.TrackIDs(ts)})
	if err != nil {
		return nil, internal(ctx, "adventure", err)
	}
	return MusicAdventure200JSONResponse(out), nil
}

func (h *Handlers) MusicMixes(ctx context.Context, req MusicMixesRequestObject) (MusicMixesResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return MusicMixes401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	out := MusicMixes200JSONResponse{}
	for i, m := range h.Sonic.DailyMixes(ctx, s.User.ID, keepFor(access(ctx), req.Params.LibraryId)) {
		st, err := h.station(ctx, m.Station)
		if err != nil {
			return nil, internal(ctx, "mixes", err)
		}
		st.Id = ptr("daily-" + strconv.Itoa(i+1))
		st.Description = ptr(mixDescription(st.Items))
		out = append(out, st)
	}
	return out, nil
}

// mixDescription names a few artists in a mix ("Portishead, Massive Attack and more").
func mixDescription(list []ItemSummary) string {
	var names []string
	seen := map[string]bool{}
	for _, it := range list {
		a := ""
		if it.GrandparentTitle != nil {
			a = *it.GrandparentTitle
		}
		if a != "" && !seen[a] {
			seen[a] = true
			names = append(names, a)
		}
		if len(names) == 3 {
			break
		}
	}
	if len(names) == 0 {
		return ""
	}
	return strings.Join(names, ", ") + " and more"
}

func (h *Handlers) RateItem(ctx context.Context, req RateItemRequestObject) (RateItemResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return RateItem401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	if err := h.Items.Visible(ctx, access(ctx), req.ItemId); err != nil {
		return RateItem404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "item not found"))}, nil
	}
	var r *float64
	if req.Body.Rating != nil {
		v := float64(*req.Body.Rating)
		if v < 0 || v > 10 {
			return RateItem400JSONResponse{BadRequestJSONResponse(apiErr("invalid", "rating must be 0–10"))}, nil
		}
		r = &v
	}
	if err := h.Items.SetRating(ctx, s.User.ID, req.ItemId, r); err != nil {
		return nil, internal(ctx, "rate", err)
	}
	return RateItem204Response{}, nil
}

func (h *Handlers) GetLyrics(ctx context.Context, req GetLyricsRequestObject) (GetLyricsResponseObject, error) {
	if _, ok := session(ctx); !ok {
		return GetLyrics401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	if err := h.Items.Visible(ctx, access(ctx), req.ItemId); err != nil {
		return GetLyrics404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "item not found"))}, nil
	}
	l, err := h.Lyrics.Get(ctx, req.ItemId)
	if errors.Is(err, lyrics.ErrNone) {
		return GetLyrics404JSONResponse{NotFoundJSONResponse(apiErr("no_lyrics", err.Error()))}, nil
	}
	if err != nil {
		return nil, internal(ctx, "lyrics", err)
	}
	out := Lyrics{Synced: l.Synced, Source: LyricsSource(l.Source), Lines: make([]LyricLine, len(l.Lines))}
	for i, ln := range l.Lines {
		out.Lines[i] = LyricLine{Text: ln.Text}
		if ln.TimeMS >= 0 {
			out.Lines[i].TimeMs = ptr(ln.TimeMS)
		}
	}
	return GetLyrics200JSONResponse(out), nil
}
