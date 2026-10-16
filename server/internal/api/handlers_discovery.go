package api

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"marquee/internal/ai"
	"marquee/internal/items"
	"marquee/internal/muse"
)

// ---------- listening recap (MUSIC-22) ----------

func recapYear(y *int) int {
	if y != nil && *y >= 1900 && *y <= 3000 {
		return *y
	}
	return time.Now().Year()
}

func recapEntries(list []items.RecapEntry) []RecapEntry {
	out := make([]RecapEntry, len(list))
	for i, e := range list {
		out[i] = RecapEntry{Item: toAPISummary(e.Item), Plays: e.Plays, Minutes: e.Minutes}
	}
	return out
}

func (h *Handlers) ListeningRecap(ctx context.Context, req ListeningRecapRequestObject) (ListeningRecapResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return ListeningRecap401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	r, err := h.Items.Recap(ctx, access(ctx), s.User.ID, recapYear(req.Params.Year))
	if err != nil {
		return nil, internal(ctx, "recap", err)
	}
	out := ListeningRecap{Year: r.Year, Minutes: r.Minutes, Plays: r.Plays, Tracks: r.Tracks, Artists: r.Artists, NewArtists: r.NewArtists,
		TopArtists: recapEntries(r.TopArtists), TopAlbums: recapEntries(r.TopAlbums), TopTracks: recapEntries(r.TopTracks),
		ByMonth: r.ByMonth[:], ByHour: r.ByHour[:], LongestStreakDays: r.LongestStreakDays, VideoHours: ptr(float32(r.VideoHours))}
	out.TopGenres = make([]struct {
		Name  string `json:"name"`
		Plays int    `json:"plays"`
	}, len(r.TopGenres))
	for i, g := range r.TopGenres {
		out.TopGenres[i].Name, out.TopGenres[i].Plays = g.Name, g.Plays
	}
	if d, err := time.Parse("2006-01-02", r.TopDay); err == nil {
		out.TopDay = &struct {
			Date    openapi_types.Date `json:"date"`
			Minutes int                `json:"minutes"`
		}{Date: openapi_types.Date{Time: d}, Minutes: r.TopDayMinutes}
	}
	if r.FirstTrack != nil {
		out.FirstTrack = ptr(toAPISummary(*r.FirstTrack))
	}
	return ListeningRecap200JSONResponse(out), nil
}

func (h *Handlers) RecapYears(ctx context.Context, _ RecapYearsRequestObject) (RecapYearsResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return RecapYears401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	years, err := h.Items.RecapYears(ctx, s.User.ID)
	if err != nil {
		return nil, internal(ctx, "recapYears", err)
	}
	return RecapYears200JSONResponse(years), nil
}

func (h *Handlers) RecapPlaylist(ctx context.Context, req RecapPlaylistRequestObject) (RecapPlaylistResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return RecapPlaylist401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	acc := access(ctx)
	year := recapYear(req.Params.Year)
	r, err := h.Items.Recap(ctx, acc, s.User.ID, year)
	if err != nil {
		return nil, internal(ctx, "recapPlaylist", err)
	}
	ids := make([]int64, len(r.TopTracks))
	for i, e := range r.TopTracks {
		ids[i] = e.Item.ID
	}
	p, err := h.Items.SaveRecapPlaylist(ctx, acc, year, ids)
	if errors.Is(err, items.ErrNotFound) {
		return RecapPlaylist404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "no music played that year"))}, nil
	}
	if err != nil {
		return nil, internal(ctx, "recapPlaylist", err)
	}
	return RecapPlaylist201JSONResponse(toAPIPlaylist(p)), nil
}

// ---------- Muse for movies (USER-15) ----------

// videoIndex is the movie and show embedding index (nil-safe: empty without the sidecar).
func (h *Handlers) videoIndex() *items.VideoIndex {
	if h.Embeddings == nil || h.Embeddings.Index == nil {
		return items.NewVideoIndex()
	}
	return h.Embeddings.Index
}

// rerankTop is how many candidates the AI re-ranks.
const rerankTop = 30

// museAIBudget is how long Muse's model calls may take in all. The apps give up on a
// request after 20 s, and loading a model from cold can take about that long, so past this
// Muse answers without the model (which keeps loading for the next request).
const museAIBudget = 12 * time.Second

func (h *Handlers) MuseVideo(ctx context.Context, req MuseVideoRequestObject) (MuseVideoResponseObject, error) {
	if _, ok := session(ctx); !ok {
		return MuseVideo401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	b := req.Body
	prompt := strings.TrimSpace(b.Prompt)
	if len(prompt) < 2 || len(prompt) > 300 {
		return MuseVideo400JSONResponse{BadRequestJSONResponse(apiErr("invalid", "describe what you'd like to watch (2–300 characters)"))}, nil
	}
	limit := 40
	set(&limit, b.Limit)
	limit = min(max(limit, 1), 100)
	var lib int64
	set(&lib, b.LibraryId)
	if lib > 0 && !h.canSeeLibrary(ctx, lib) {
		return MuseVideo400JSONResponse{BadRequestJSONResponse(apiErr("invalid", "library not found"))}, nil
	}
	ctx = ai.WithBudget(ctx, museAIBudget) // see museAIBudget
	genres, err := h.Items.VideoGenres(ctx)
	if err != nil {
		return nil, internal(ctx, "museVideo", err)
	}
	cfg := h.Settings.Get().AI
	var llm muse.LLM
	if cfg.On(cfg.UnderstandPrompts) && h.AI != nil {
		llm = h.AI
	}
	keywords, err := h.Items.VideoKeywords(ctx)
	if err != nil {
		return nil, internal(ctx, "museVideo", err)
	}
	reading := muse.ReadVideo(ctx, llm, h.Items, access(ctx), prompt, items.VideoVocab{Genres: genres, Keywords: keywords})
	q := reading.Query
	if b.Types != nil && len(*b.Types) > 0 {
		var allowed []string
		for _, t := range *b.Types {
			if t != "movie" && t != "show" {
				return MuseVideo400JSONResponse{BadRequestJSONResponse(apiErr("invalid", "types are movie and show"))}, nil
			}
			if len(q.Types) == 0 || slices.Contains(q.Types, string(t)) {
				allowed = append(allowed, string(t))
			}
		}
		if len(allowed) == 0 {
			allowed = []string{string((*b.Types)[0])} // the request's types win over the prompt's
		}
		q.Types = allowed
	}
	idx := h.videoIndex()
	var vec []float32
	if q.Rest != "" {
		v, model, err := h.Embeddings.Query(ctx, q.Rest)
		switch {
		case errors.Is(err, items.ErrEmbedderUnavailable) && idx.Len() == 0:
			return MuseVideo503JSONResponse{ServiceUnavailableJSONResponse(apiErr("unavailable",
				"Muse needs the analysis service, which isn't running"))}, nil
		case err != nil:
			slog.WarnContext(ctx, "muse for movies: ranking by rating", "err", err)
		case model != idx.Model():
			slog.WarnContext(ctx, "muse for movies: the index is from another model; ranking by rating", "query", model, "index", idx.Model())
		default:
			vec = v
		}
	}
	// With AI, the top candidates (hard filters already applied) are re-ranked by the chat model.
	rerank := cfg.On(cfg.RerankMovies) && h.AI != nil && h.AI.Ready() && (q.Rest != "" || len(q.Seeds) > 0)
	fetch := limit
	if rerank {
		fetch = max(limit, rerankTop)
	}
	res, err := h.Items.MuseVideo(ctx, access(ctx), idx, q, vec, lib, fetch)
	if err != nil {
		return nil, internal(ctx, "museVideo", err)
	}
	var reasons []RecommendationReason
	if rerank && len(res.Items) > 1 {
		top := res.Items[:min(rerankTop, len(res.Items))]
		ids := make([]int64, len(top))
		for i, it := range top {
			ids[i] = it.ID
		}
		ids = append(ids, q.Seeds...)
		syn, err := h.Items.Synopses(ctx, ids)
		if err != nil {
			return nil, internal(ctx, "museVideo", err)
		}
		cands := make([]muse.Candidate, len(top))
		byID := map[int64]items.Summary{}
		for i, it := range top {
			cands[i] = muse.Candidate{ID: it.ID, Title: it.Title, Year: it.Year, Synopsis: syn[it.ID]}
			byID[it.ID] = it
		}
		var seeds []muse.Candidate
		if len(q.Seeds) > 0 {
			if list, err := h.Items.ByIDs(ctx, access(ctx), q.Seeds); err == nil {
				for _, it := range list {
					seeds = append(seeds, muse.Candidate{ID: it.ID, Title: it.Title, Year: it.Year, Synopsis: syn[it.ID]})
				}
			}
		}
		if order, why, err := muse.Rerank(ctx, h.AI, prompt, seeds, cands); err != nil {
			slog.WarnContext(ctx, "muse for movies: re-ranking failed; keeping the ranking", "err", err)
		} else {
			ranked := make([]items.Summary, 0, len(res.Items))
			for _, id := range order {
				ranked = append(ranked, byID[id])
				if r, ok := why[id]; ok && len(ranked) <= limit {
					reasons = append(reasons, RecommendationReason{ItemId: id, Reason: r})
				}
			}
			res.Items = append(ranked, res.Items[len(top):]...)
		}
	}
	if len(res.Items) > limit {
		res.Items = res.Items[:limit]
	}
	title := reading.Title
	if title == "" {
		title = prompt
	}
	out := MuseVideo200JSONResponse{Items: summaries(res.Items), Understood: q.Understood(), Analysed: ptr(float32(res.Analysed)), Title: ptr(title)}
	if len(reasons) > 0 {
		out.Reasons = &reasons
	}
	return out, nil
}
