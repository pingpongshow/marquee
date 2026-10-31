package api

import (
	"context"
	"errors"
	"math/rand/v2"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"marquee/internal/ai"
	"marquee/internal/items"
	"marquee/internal/lyrics"
	"marquee/internal/muse"
	"marquee/internal/settings"
	"marquee/internal/soundprint"
)

// clampCount keeps a client's count within the API's bounds: the server doesn't validate
// requests against the spec, and the station builders' work grows with the count (a huge
// Sound Journey length scans the whole index once per step; a negative one panicked).
func clampCount(n, lo, hi int) int {
	return min(max(n, lo), hi)
}

// upperFirst capitalises the first letter, whole ("électro" → "Électro"; slicing its first
// byte split the letter into invalid UTF-8).
func upperFirst(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	if r == utf8.RuneError {
		return s
	}
	return string(unicode.ToUpper(r)) + s[n:]
}

// keepFor limits Soundprint results to libraries the user can see (and optionally one library).
func keepFor(acc items.Access, libraryID *int64) soundprint.Filter {
	return func(t *soundprint.Track) bool {
		if libraryID != nil && t.LibraryID != *libraryID {
			return false
		}
		return acc.LibraryIDs == nil || slices.Contains(acc.LibraryIDs, t.LibraryID)
	}
}

func (h *Handlers) station(ctx context.Context, st soundprint.Station) (Station, error) {
	list, err := h.Items.ByIDs(ctx, access(ctx), st.IDs)
	if err != nil {
		return Station{}, err
	}
	out := Station{Title: st.Title, Items: summaries(list)}
	if st.Context != "" {
		out.Context = ptr(st.Context)
	}
	return out, nil
}

// playContext keeps a play's context to the shape the apps get it in (letters, digits, ':',
// '-' and '_'; at most 64), so the statistics aren't fed arbitrary text.
func playContext(c string) string {
	c = strings.ToLower(strings.TrimSpace(c))
	if len(c) > 64 {
		return ""
	}
	for _, r := range c {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == ':' || r == '-' || r == '_') {
			return ""
		}
	}
	return c
}

// soundModelOr is a sound model setting, "general" when unset.
func soundModelOr(m string) string {
	if m == soundprint.ModelMusic {
		return m
	}
	return soundprint.ModelGeneral
}

// mixContext is the statistics context of a mix by its id (daily-2 → mix:daily).
func mixContext(id string) string {
	if strings.HasPrefix(id, "daily-") {
		return "mix:daily"
	}
	return "mix:" + id
}

// asideWords lets a Muse prompt that asks for live recordings, remixes or interludes have them.
func asideWords(prompt string) soundprint.Aside {
	p := " " + strings.ToLower(prompt) + " "
	var a soundprint.Aside
	if strings.Contains(p, " live ") || strings.Contains(p, "concert") || strings.Contains(p, "unplugged") {
		a |= soundprint.AsideLive
	}
	if strings.Contains(p, "remix") {
		a |= soundprint.AsideRemix
	}
	if strings.Contains(p, "interlude") || strings.Contains(p, "skit") || strings.Contains(p, " intro") {
		a |= soundprint.AsideInterlude
	}
	return a
}

func soundprintErr(err error) (code string, msg string, status int) {
	switch {
	case errors.Is(err, soundprint.ErrUnavailable):
		return "soundprint_unavailable", "Soundprint analysis isn't running on the server.", 503
	case errors.Is(err, soundprint.ErrNotAnalyzed):
		return "not_analyzed", "This music hasn't had its Soundprint analysis yet.", 409
	}
	return "", "", 0
}

func (h *Handlers) MusicStatus(ctx context.Context, _ MusicStatusRequestObject) (MusicStatusResponseObject, error) {
	if _, ok := session(ctx); !ok {
		return MusicStatus401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	st := h.Soundprint.Status(ctx)
	out := MusicStatus200JSONResponse{Enabled: st.Enabled, Available: st.Available, Running: st.Running, Model: nz(st.Model),
		Device: nz(st.Device), Analyzed: st.Analyzed, Total: st.Total, Failed: nz(st.Failed), Progress: nz(st.Progress), RunTotal: nz(st.RunTotal),
		Phase: nz(st.Phase), SoundModel: ptr(MusicStatusSoundModel(soundModelOr(st.SoundModel))), Traits: nz(st.Traits), Languages: nz(st.Languages)}
	if t := st.Trial; t != nil {
		score := func(m *soundprint.ModelScore) *SoundModelScore {
			if m == nil {
				return nil
			}
			return &SoundModelScore{GenreMatch: float32(m.GenreMatch), PlayedMatch: nz(float32(m.PlayedMatch)), TextMatch: nz(float32(m.TextMatch))}
		}
		trial := &SoundModelTrial{Model: SoundModelTrialModel(t.Model), Analyzed: t.Analyzed, Total: t.Total, Current: score(t.Current), Tried: score(t.Tried)}
		if at, err := time.Parse(time.RFC3339, t.At); err == nil {
			trial.At = &at
		}
		out.Trial = trial
	}
	return out, nil
}

// ChooseSoundModel switches to the sound model being tried, or keeps the current one (MUSIC-1).
func (h *Handlers) ChooseSoundModel(ctx context.Context, req ChooseSoundModelRequestObject) (ChooseSoundModelResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return ChooseSoundModel401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return ChooseSoundModel403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	if req.Body == nil {
		return ChooseSoundModel409JSONResponse(apiErr("invalid", "action is required")), nil
	}
	switch req.Body.Action {
	case SoundModelChoiceActionSwitch:
		if err := h.Soundprint.SwitchModel(ctx); errors.Is(err, soundprint.ErrNoTrial) {
			return ChooseSoundModel409JSONResponse(apiErr("no_trial", err.Error())), nil
		} else if err != nil {
			return nil, internal(ctx, "sound model", err)
		}
		// Tracks the new model couldn't analyse (or added since) are analysed now.
		if h.Tasks != nil {
			h.Tasks.Trigger(context.WithoutCancel(ctx), "soundprint")
		}
	case SoundModelChoiceActionKeep:
		active := h.Soundprint.ActiveModel(ctx)
		if _, err := h.Settings.Update(ctx, func(s *settings.Settings) error {
			s.Music.SoundModel = active
			return nil
		}); err != nil {
			return nil, internal(ctx, "sound model", err)
		}
		if err := h.Soundprint.EndTrial(ctx); err != nil {
			return nil, internal(ctx, "sound model", err)
		}
	}
	return ChooseSoundModel204Response{}, nil
}

func (h *Handlers) SoundsLike(ctx context.Context, req SoundsLikeRequestObject) (SoundsLikeResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return SoundsLike401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	acc := access(ctx)
	d, err := h.Items.Get(ctx, acc, req.ItemId, false)
	if errors.Is(err, items.ErrNotFound) {
		return SoundsLike404JSONResponse{NotFoundJSONResponse(apiErr("not_found", err.Error()))}, nil
	} else if err != nil {
		return nil, internal(ctx, "similar", err)
	}
	limit := 20
	set(&limit, req.Params.Limit)
	limit = clampCount(limit, 1, 50)
	keep := keepFor(acc, nil)
	var ids []int64
	switch d.Type {
	case "track":
		t := h.Soundprint.Index.Get(d.ID)
		if t == nil {
			return SoundsLike409JSONResponse{ConflictJSONResponse(apiErr("not_analyzed", soundprint.ErrNotAnalyzed.Error()))}, nil
		}
		ids = soundprint.TrackIDs(h.Soundprint.Index.Similar(t, limit, keep))
	case "album", "artist":
		col := map[string]func(*soundprint.Track) int64{"album": func(t *soundprint.Track) int64 { return t.AlbumID }, "artist": func(t *soundprint.Track) int64 { return t.ArtistID }}[d.Type]
		members := h.Soundprint.Index.Where(func(t *soundprint.Track) bool { return col(t) == d.ID })
		if len(members) == 0 {
			return SoundsLike409JSONResponse{ConflictJSONResponse(apiErr("not_analyzed", soundprint.ErrNotAnalyzed.Error()))}, nil
		}
		for _, g := range h.Soundprint.Index.SimilarGroups(soundprint.Mean(members), d.Type == "album", d.ID, limit, keep) {
			ids = append(ids, g.ID)
		}
	default:
		return SoundsLike200JSONResponse{}, nil
	}
	_ = s
	list, err := h.Items.ByIDs(ctx, acc, ids)
	if err != nil {
		return nil, internal(ctx, "similar", err)
	}
	return SoundsLike200JSONResponse(summaries(list)), nil
}

func (h *Handlers) MusicRadio(ctx context.Context, req MusicRadioRequestObject) (MusicRadioResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return MusicRadio401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	b := req.Body
	acc := access(ctx)
	o := soundprint.Options{Length: 50, Keep: keepFor(acc, b.LibraryId), Avoid: h.Soundprint.Avoid(ctx, s.User.ID), Exclude: map[int64]bool{},
		Favour: h.Soundprint.Favour(ctx, s.User.ID), CoPlay: h.Soundprint.CoPlay, Languages: h.Soundprint.ListenerLanguages(ctx, s.User.ID)}
	set(&o.Length, b.Limit)
	o.Length = clampCount(o.Length, 5, 100)
	if b.Exclude != nil {
		for _, id := range *b.Exclude {
			o.Exclude[id] = true
		}
	}
	// Steering (MUSIC-17): what the listener skipped and played through so far.
	if b.Skipped != nil {
		o.Skipped = (*b.Skipped)[:min(len(*b.Skipped), 100)]
	}
	if b.Finished != nil {
		o.Finished = (*b.Finished)[:min(len(*b.Finished), 100)]
	}
	bad := func(msg string) (MusicRadioResponseObject, error) {
		return MusicRadio400JSONResponse{BadRequestJSONResponse(apiErr("invalid", msg))}, nil
	}
	value := ""
	set(&value, b.Value)
	var st soundprint.Station
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
		st, err = h.Soundprint.RadioFromItem(ctx, d.ID, d.Title, o)
		st.Context = "radio:item"
	case RadioRequestSeedGenre:
		ids, gerr := h.Items.TrackIDsByGenre(ctx, acc, value)
		if gerr != nil {
			return nil, internal(ctx, "radio", gerr)
		}
		v := h.Soundprint.MeanOf(ids)
		if v == nil {
			return MusicRadio409JSONResponse{ConflictJSONResponse(apiErr("not_analyzed", "no analysed tracks in "+value))}, nil
		}
		st = h.Soundprint.RadioFromVector(v, value+" Radio", o)
		st.Context = "radio:genre"
	case RadioRequestSeedDecade:
		dec, perr := strconv.Atoi(strings.TrimSuffix(value, "s"))
		if perr != nil {
			return bad("decade must be a year like 1990")
		}
		inner := o.Keep
		o.Keep = func(t *soundprint.Track) bool { return t.Year >= dec && t.Year < dec+10 && inner(t) }
		members := h.Soundprint.Index.Where(o.Keep)
		if len(members) == 0 {
			return MusicRadio409JSONResponse{ConflictJSONResponse(apiErr("not_analyzed", "no analysed tracks from the "+strconv.Itoa(dec)+"s"))}, nil
		}
		st = h.Soundprint.RadioFromVector(soundprint.Mean(members), strconv.Itoa(dec)+"s Radio", o)
		st.Context = "radio:decade"
	case RadioRequestSeedMood:
		if value == "" {
			return bad("value (the mood) is required")
		}
		// Each mood's descriptions, traits, energy and tags (MUSIC-18).
		st, err = h.Soundprint.MoodRadio(ctx, value, o)
		st.Context = "radio:mood"
	case RadioRequestSeedFavourites:
		// The sound of their favourite songs first, then what they play and rate highly.
		taste := h.Soundprint.Taste(ctx, s.User.ID, h.Soundprint.Liked(ctx, s.User.ID))
		if taste == nil {
			return MusicRadio409JSONResponse{ConflictJSONResponse(apiErr("no_history", "Play, rate or favourite some music first."))}, nil
		}
		st = h.Soundprint.RadioFromVector(taste, "Your Favourites Radio", o)
		st.Context = "radio:favourites"
	case RadioRequestSeedLibrary:
		all := h.Soundprint.Index.Where(o.Keep)
		if len(all) == 0 {
			return MusicRadio409JSONResponse{ConflictJSONResponse(apiErr("not_analyzed", soundprint.ErrNotAnalyzed.Error()))}, nil
		}
		// Library radio wanders: re-anchor on a random track every run.
		first := all[len(o.Exclude)%len(all)]
		if len(o.Exclude) == 0 {
			first = all[int(s.User.ID+int64(len(all)))%len(all)]
		}
		st = soundprint.Station{Title: "Library Radio", IDs: soundprint.TrackIDs(h.Soundprint.Index.Flow(first.Vec, first, o)), Context: "radio:library"}
	default:
		return bad("unknown seed")
	}
	if err != nil {
		if code, msg, status := soundprintErr(err); status == 503 {
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

func (h *Handlers) MusicMuse(ctx context.Context, req MusicMuseRequestObject) (MusicMuseResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return MusicMuse401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	prompt := strings.TrimSpace(req.Body.Prompt)
	if len(prompt) < 2 {
		return MusicMuse400JSONResponse{BadRequestJSONResponse(apiErr("invalid", "describe the music you want"))}, nil
	}
	n := 30
	set(&n, req.Body.Limit)
	n = clampCount(n, 5, 100)
	o := soundprint.Options{Keep: keepFor(access(ctx), req.Body.LibraryId), Avoid: h.Soundprint.Avoid(ctx, s.User.ID),
		Favour: h.Soundprint.Favour(ctx, s.User.ID), Arc: true, AllowAside: asideWords(prompt)}
	// Read the prompt (AI, or keywords against the library's genres and artists), filter,
	// then rank by sound, "like" artists, genres and, with AI, lyric themes.
	ctx = ai.WithBudget(ctx, museAIBudget) // see museAIBudget
	cfg := h.Settings.Get().AI
	var llm muse.LLM
	if cfg.On(cfg.UnderstandPrompts) && h.AI != nil {
		llm = h.AI
	}
	lib := muse.MusicLibrary{Genres: h.Soundprint.MusicGenres(ctx), Artists: h.Soundprint.ArtistNames(ctx)}
	lib.Countries, lib.Areas = h.Soundprint.MusicOrigins(ctx)
	reading := muse.ReadMusic(ctx, llm, lib, prompt)
	// Descriptive artist or album criteria ("Icelandic female singer-songwriters"): rank
	// artists and albums by their descriptions; the best ones filter (or boost) the tracks.
	if d := reading.Query.Describe; d != "" && h.MusicDocs != nil {
		if artists, albums := h.MusicDocs.Rank(ctx, d); len(artists)+len(albums) > 0 {
			reading.Query.ArtistScores, reading.Query.AlbumScores = artists, albums
			reading.Understood = append(reading.Understood, "Artists: "+d)
		}
	}
	if reading.Query.Theme != "" && cfg.On(cfg.LyricsThemes) {
		if boost := h.LyricThemes.Boost(ctx, reading.Query.Theme); len(boost) > 0 {
			o.Boost = boost
			reading.Understood = append(reading.Understood, "About "+reading.Query.Theme)
		}
	}
	if reading.Query.Title == "" {
		reading.Query.Title = prompt
	}
	st, err := h.Soundprint.MuseQuery(ctx, s.User.ID, reading.Query, n, o)
	st.Context = "muse"
	if err != nil {
		if code, msg, status := soundprintErr(err); status == 503 {
			return MusicMuse503JSONResponse{ServiceUnavailableJSONResponse(apiErr(code, msg))}, nil
		}
		return nil, internal(ctx, "muse", err)
	}
	out, err := h.station(ctx, st)
	if err != nil {
		return nil, internal(ctx, "muse", err)
	}
	reading.Understood = muse.Eased(reading.Understood, reading.Query, st.Query)
	if len(reading.Understood) > 0 {
		out.Description = ptr(strings.Join(reading.Understood, " · "))
	}
	return MusicMuse200JSONResponse(out), nil
}

func (h *Handlers) MusicJourney(ctx context.Context, req MusicJourneyRequestObject) (MusicJourneyResponseObject, error) {
	if _, ok := session(ctx); !ok {
		return MusicJourney401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	acc := access(ctx)
	b := req.Body
	for _, id := range []int64{b.FromId, b.ToId} {
		if err := h.Items.Visible(ctx, acc, id); err != nil {
			return MusicJourney404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "track not found"))}, nil
		}
	}
	from, to := h.Soundprint.Index.Get(b.FromId), h.Soundprint.Index.Get(b.ToId)
	if from == nil || to == nil {
		return MusicJourney409JSONResponse{ConflictJSONResponse(apiErr("not_analyzed", soundprint.ErrNotAnalyzed.Error()))}, nil
	}
	n := 15
	set(&n, b.Length)
	n = clampCount(n, 3, 60)
	ts := h.Soundprint.Index.Journey(from, to, n, soundprint.Options{Keep: keepFor(acc, nil)})
	out, err := h.station(ctx, soundprint.Station{Title: "Sound Journey", IDs: soundprint.TrackIDs(ts), Context: "journey"})
	if err != nil {
		return nil, internal(ctx, "journey", err)
	}
	return MusicJourney200JSONResponse(out), nil
}

func (h *Handlers) MusicMixes(ctx context.Context, req MusicMixesRequestObject) (MusicMixesResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return MusicMixes401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	out := MusicMixes200JSONResponse{}
	fav, err := h.favoritesMix(ctx, req.Params.LibraryId)
	if err != nil {
		return nil, internal(ctx, "mixes", err)
	}
	if fav != nil {
		out = append(out, *fav)
	}
	for i, m := range h.Soundprint.DailyMixes(ctx, s.User.ID, mixScope(req.Params.LibraryId), keepFor(access(ctx), req.Params.LibraryId)) {
		m.Context = mixContext(m.ID)
		st, err := h.station(ctx, m.Station)
		if err != nil {
			return nil, internal(ctx, "mixes", err)
		}
		st.Id = ptr(m.ID)
		if m.ID == "" {
			st.Id = ptr("daily-" + strconv.Itoa(i+1))
		}
		st.Description = ptr(mixDescription(st.Items))
		if m.Description != "" {
			st.Description = ptr(m.Description)
		}
		out = append(out, st)
	}
	return out, nil
}

// favoritesMixSize is how many of the person's favourite songs the Favourites mix plays.
const favoritesMixSize = 50

// favoritesMix is the person's favourite songs shuffled (nil with fewer than three). It needs
// no Soundprint analysis.
func (h *Handlers) favoritesMix(ctx context.Context, libraryID *int64) (*Station, error) {
	var libs []int64
	if libraryID != nil {
		libs = []int64{*libraryID}
	}
	all, err := h.Items.Favorites(ctx, access(ctx), []string{"track"}, libs, 2000)
	if err != nil {
		return nil, err
	}
	var list []items.Summary
	for _, it := range all {
		if it.Available {
			list = append(list, it)
		}
	}
	if len(list) < 3 {
		return nil, nil
	}
	rand.Shuffle(len(list), func(i, j int) { list[i], list[j] = list[j], list[i] })
	list = list[:min(len(list), favoritesMixSize)]
	return &Station{Id: ptr("favorites"), Title: "Favourites Mix", Description: ptr("Your favourite songs, shuffled"), Context: ptr("mix:favorites"), Items: summaries(list)}, nil
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
	// A change made offline and sent later only applies if nothing newer happened (USER-18).
	if fresh, err := h.Items.Fresh(ctx, s.User.ID, req.ItemId, "rating", req.Body.At); err != nil {
		return nil, internal(ctx, "rate", err)
	} else if !fresh {
		return RateItem204Response{}, nil
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

// MusicDJ picks a DJ track (MUSIC-6).
func (h *Handlers) MusicDJ(ctx context.Context, req MusicDJRequestObject) (MusicDJResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return MusicDJ401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	none := MusicDJ404JSONResponse{NotFoundJSONResponse(apiErr("nothing", "nothing fits right now"))}
	b := req.Body
	acc := access(ctx)
	t := h.Soundprint.Index.Get(b.TrackId)
	if t == nil || h.Items.Visible(ctx, acc, b.TrackId) != nil {
		return none, nil
	}
	o := soundprint.Options{Keep: keepFor(acc, &t.LibraryID), Avoid: h.Soundprint.Avoid(ctx, s.User.ID), Exclude: map[int64]bool{}}
	if b.Exclude != nil {
		for _, id := range *b.Exclude {
			o.Exclude[id] = true
		}
	}
	plays := func(ids []int64) map[int64]int {
		out := map[int64]int{}
		for _, id := range ids {
			var n int
			h.DB.QueryRowContext(ctx, `SELECT COALESCE(play_count, 0) FROM user_item_state WHERE user_id = ? AND item_id = ?`, s.User.ID, id).Scan(&n)
			out[id] = n
		}
		return out
	}
	pick := h.Soundprint.Index.DJPick(t, string(b.Mode), o, plays)
	if pick == nil {
		return none, nil
	}
	d, err := h.Items.Get(ctx, acc, pick.ItemID, false)
	if err != nil {
		return none, nil
	}
	return MusicDJ200JSONResponse(toAPISummary(d.Summary)), nil
}

// mixScope keys cached mixes by library ("" = all the caller can see).
func mixScope(lib *int64) string {
	if lib == nil {
		return ""
	}
	return strconv.FormatInt(*lib, 10)
}
