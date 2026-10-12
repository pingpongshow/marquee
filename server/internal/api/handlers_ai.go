package api

import (
	"context"
	"strings"

	"marquee/internal/ai"
	"marquee/internal/muse"
)

// ---------- AI (Ollama) settings, status and reasons ----------

func aiConfig(c ai.Config, url, chat, embed *string) ai.Config {
	if url != nil && strings.TrimSpace(*url) != "" {
		c.URL = strings.TrimRight(strings.TrimSpace(*url), "/")
	}
	if chat != nil && strings.TrimSpace(*chat) != "" {
		c.ChatModel = strings.TrimSpace(*chat)
	}
	if embed != nil && strings.TrimSpace(*embed) != "" {
		c.EmbedModel = strings.TrimSpace(*embed)
	}
	return c
}

func (h *Handlers) savedAIConfig() ai.Config {
	a := h.Settings.Get().AI
	return ai.Config{Enabled: a.Enabled, URL: a.URL, ChatModel: a.ChatModel, EmbedModel: a.EmbedModel, UnloadMinutes: a.UnloadMinutes}
}

func toAPIAIStatus(enabled bool, st ai.Status) AiStatus {
	out := AiStatus{Enabled: enabled, Reachable: st.Reachable, Version: nz(st.Version), Models: append([]string{}, st.Models...),
		ChatModelInstalled: ptr(st.ChatInstalled), EmbedModelInstalled: ptr(st.EmbedInstalled), Error: nz(st.Error), Loaded: []AiLoadedModel{}}
	for _, m := range st.Loaded {
		lm := AiLoadedModel{Name: m.Name, VramBytes: ptr(m.VRAMBytes)}
		if !m.ExpiresAt.IsZero() {
			lm.ExpiresAt = ptr(m.ExpiresAt)
		}
		out.Loaded = append(out.Loaded, lm)
	}
	return out
}

func (h *Handlers) aiClient() *ai.Client {
	if h.AI != nil {
		return h.AI
	}
	return &ai.Client{Config: h.savedAIConfig}
}

func (h *Handlers) AiStatus(ctx context.Context, _ AiStatusRequestObject) (AiStatusResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return AiStatus401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return AiStatus403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	cfg := h.savedAIConfig()
	c := h.aiClient()
	out := toAPIAIStatus(cfg.Enabled, c.Check(ctx, cfg))
	if msg, at, paused := c.LastError(); msg != "" {
		out.LastError, out.LastErrorAt = ptr(msg), ptr(at)
		out.Paused = ptr(paused)
	}
	if h.Embeddings != nil && h.Embeddings.Index != nil {
		var total int
		h.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM items WHERE type IN ('movie', 'show') AND extra_type IS NULL`).Scan(&total)
		out.Videos = &AiVideoIndex{Indexed: h.Embeddings.Index.Len(), Total: total, Model: nz(h.Embeddings.Index.Model())}
	}
	if h.LyricThemes != nil {
		ls := h.LyricThemes.Status(ctx)
		out.Lyrics = &AiLyricIndex{Indexed: ls.Indexed, WithLyrics: ls.WithLyrics, Tracks: ls.Tracks, Running: ls.Running}
	}
	return AiStatus200JSONResponse(out), nil
}

func (h *Handlers) TestAi(ctx context.Context, req TestAiRequestObject) (TestAiResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return TestAi401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return TestAi403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	cfg := h.savedAIConfig()
	if b := req.Body; b != nil {
		cfg = aiConfig(cfg, b.Url, b.ChatModel, b.EmbedModel)
	}
	st := h.aiClient().Check(ctx, cfg)
	if st.Reachable && st.Error == "" {
		var missing []string
		if !st.ChatInstalled {
			missing = append(missing, cfg.ChatModel)
		}
		if !st.EmbedInstalled {
			missing = append(missing, cfg.EmbedModel)
		}
		if len(missing) > 0 {
			st.Error = "not installed in Ollama: " + strings.Join(missing, ", ") + " (run ollama pull)"
		}
	}
	return TestAi200JSONResponse(toAPIAIStatus(cfg.Enabled, st)), nil
}

func (h *Handlers) RebuildLyricIndex(ctx context.Context, _ RebuildLyricIndexRequestObject) (RebuildLyricIndexResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return RebuildLyricIndex401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return RebuildLyricIndex403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	if h.LyricThemes != nil {
		if err := h.LyricThemes.Rebuild(ctx); err != nil {
			return nil, internal(ctx, "rebuildLyricIndex", err)
		}
		if h.Tasks != nil {
			h.Tasks.RunNow(context.WithoutCancel(ctx), "lyric-themes")
		}
	}
	return RebuildLyricIndex202Response{}, nil
}

func (h *Handlers) RecommendationReasons(ctx context.Context, req RecommendationReasonsRequestObject) (RecommendationReasonsResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return RecommendationReasons401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	ids := req.Body.ItemIds
	if len(ids) > 5 {
		return RecommendationReasons400JSONResponse{BadRequestJSONResponse(apiErr("invalid", "at most 5 items"))}, nil
	}
	out := RecommendationReasons200JSONResponse{}
	cfg := h.Settings.Get().AI
	if !cfg.On(cfg.Reasons) || h.AI == nil || h.Reasons == nil || len(ids) == 0 {
		return out, nil
	}
	acc := access(ctx)
	list, err := h.Items.ByIDs(ctx, acc, ids)
	if err != nil {
		return nil, internal(ctx, "recommendationReasons", err)
	}
	var vis []int64
	for _, it := range list {
		if it.Type == "movie" || it.Type == "show" {
			vis = append(vis, it.ID)
		}
	}
	syn, err := h.Items.Synopses(ctx, vis)
	if err != nil {
		return nil, internal(ctx, "recommendationReasons", err)
	}
	var cands []muse.Candidate
	for _, it := range list {
		if it.Type == "movie" || it.Type == "show" {
			cands = append(cands, muse.Candidate{ID: it.ID, Title: it.Title, Year: it.Year, Synopsis: syn[it.ID]})
		}
	}
	liked, err := h.Items.LikedTitles(ctx, acc, 8)
	if err != nil {
		return nil, internal(ctx, "recommendationReasons", err)
	}
	reasons := h.Reasons.Get(ctx, h.AI, s.User.ID, liked, cands)
	for _, c := range cands {
		if r, ok := reasons[c.ID]; ok {
			out = append(out, RecommendationReason{ItemId: c.ID, Reason: r})
		}
	}
	return out, nil
}
