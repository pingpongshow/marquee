// Package muse reads Muse prompts and improves results with the optional AI (a local
// Ollama): prompt understanding for movies and music, re-ranking, short titles, reasons
// and lyric themes. Every function falls back to the non-AI behaviour when the AI is off,
// unreachable or answers with something unusable.
package muse

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"marquee/internal/items"
)

// LLM is the chat part of ai.Client.
type LLM interface {
	Ready() bool
	Chat(ctx context.Context, system, user string, schema any, out any) error
}

// VideoLookup resolves names and titles the AI mentions to the library (items.Store).
type VideoLookup interface {
	VideoPerson(ctx context.Context, name string) (string, error)
	VideoByTitle(ctx context.Context, acc items.Access, title string, year int) (int64, string, bool, error)
}

// videoAnswer is what the chat model returns for a movie prompt.
type videoAnswer struct {
	Types         []string `json:"types"`
	YearFrom      int      `json:"yearFrom"`
	YearTo        int      `json:"yearTo"`
	Genres        []string `json:"genres"`
	ExcludeGenres []string `json:"excludeGenres"`
	Unwatched     bool     `json:"unwatched"`
	MaxMinutes    int      `json:"maxMinutes"`
	MinMinutes    int      `json:"minMinutes"`
	HighlyRated   bool     `json:"highlyRated"`
	KidFriendly   bool     `json:"kidFriendly"`
	People        []string `json:"people"`
	Like          []struct {
		Title string `json:"title"`
		Year  int    `json:"year"`
	} `json:"like"`
	Meaning string `json:"meaning"`
	Title   string `json:"title"`
}

func strArray() map[string]any {
	return map[string]any{"type": "array", "items": map[string]any{"type": "string"}}
}

var videoSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"types":         map[string]any{"type": "array", "items": map[string]any{"type": "string", "enum": []string{"movie", "show"}}},
		"yearFrom":      map[string]any{"type": "integer"},
		"yearTo":        map[string]any{"type": "integer"},
		"genres":        strArray(),
		"excludeGenres": strArray(),
		"unwatched":     map[string]any{"type": "boolean"},
		"maxMinutes":    map[string]any{"type": "integer"},
		"minMinutes":    map[string]any{"type": "integer"},
		"highlyRated":   map[string]any{"type": "boolean"},
		"kidFriendly":   map[string]any{"type": "boolean"},
		"people":        strArray(),
		"like": map[string]any{"type": "array", "items": map[string]any{"type": "object",
			"properties": map[string]any{"title": map[string]any{"type": "string"}, "year": map[string]any{"type": "integer"}},
			"required":   []string{"title"}}},
		"meaning": map[string]any{"type": "string"},
		"title":   map[string]any{"type": "string"},
	},
	"required": []string{"types", "yearFrom", "yearTo", "genres", "excludeGenres", "unwatched", "maxMinutes", "minMinutes",
		"highlyRated", "kidFriendly", "people", "like", "meaning", "title"},
}

const videoSystem = `You turn a request for something to watch into search filters for a home media library. Answer only with JSON.
Fields:
- types: ["movie"], ["show"] or [] for both.
- yearFrom, yearTo: release year bounds, 0 when open. "90s" is 1990-1999.
- genres: genres the request asks for, ONLY from the library's genre list given below, spelled exactly as listed. All must match, so add only what is clearly asked.
- excludeGenres: genres the request rules out ("not horror"), from the same list.
- unwatched: true when they want something they haven't seen.
- maxMinutes, minMinutes: running time bounds for movies, 0 when not asked. "short" is about 100, "long" about 140.
- highlyRated: true for "acclaimed", "best", "highly rated".
- kidFriendly: true for children or the whole family.
- people: directors or actors named in the request (full names), else [].
- like: titles the request compares to ("like Alien", "similar to The Office"), with a year if given, else [].
- meaning: what's left to search by meaning (themes, plot, mood, setting), in a few plain words, without the filters above. Empty when nothing is left.
- title: a short, catchy title for the results (2-5 words).`

// VideoReading is the AI's reading of a movie prompt.
type VideoReading struct {
	Query items.VideoQuery
	Title string
	AI    bool // false: the keyword parser read it
}

// ReadVideo turns a prompt into a query with the AI when ai is ready, validating every
// field against the library, and falls back to items.ParseVideoPrompt otherwise.
func ReadVideo(ctx context.Context, llm LLM, look VideoLookup, acc items.Access, prompt string, genres []string) VideoReading {
	fallback := VideoReading{Query: items.ParseVideoPrompt(prompt, genres)}
	if llm == nil || !llm.Ready() {
		return fallback
	}
	var a videoAnswer
	user := fmt.Sprintf("Library genres: %s\n\nRequest: %s", strings.Join(genres, ", "), prompt)
	if err := llm.Chat(ctx, videoSystem, user, videoSchema, &a); err != nil {
		slog.WarnContext(ctx, "muse: AI reading failed; using keywords", "err", err)
		return fallback
	}
	q, ok := validateVideo(ctx, a, look, acc, genres)
	if !ok {
		return fallback
	}
	return VideoReading{Query: q, Title: cleanTitle(a.Title), AI: true}
}

// validateVideo maps the answer onto the library: genres must exist, years and lengths be
// sensible, people be credited and "like" titles be in the library (otherwise they become
// meaning text). ok=false when the answer is unusable.
func validateVideo(ctx context.Context, a videoAnswer, look VideoLookup, acc items.Access, genres []string) (items.VideoQuery, bool) {
	var q items.VideoQuery
	seenType := map[string]bool{}
	for _, t := range a.Types {
		if (t == "movie" || t == "show") && !seenType[t] {
			seenType[t] = true
			q.Types = append(q.Types, t)
		}
	}
	if len(q.Types) == 2 {
		q.Types = nil
	}
	maxYear := time.Now().Year() + 2
	year := func(y int) int {
		if y < 1880 || y > maxYear {
			return 0
		}
		return y
	}
	q.YearFrom, q.YearTo = year(a.YearFrom), year(a.YearTo)
	if q.YearFrom > 0 && q.YearTo > 0 && q.YearFrom > q.YearTo {
		q.YearFrom, q.YearTo = q.YearTo, q.YearFrom
	}
	seen := map[string]bool{}
	for _, g := range a.Genres {
		if grp := items.GenreGroup(g, genres); grp != nil && !seen[grp[0]] {
			seen[grp[0]] = true
			q.Genres = append(q.Genres, grp)
		}
	}
	for _, g := range a.ExcludeGenres {
		if grp := items.GenreGroup(g, genres); grp != nil && !seen[grp[0]] {
			seen[grp[0]] = true
			q.ExcludeGenres = append(q.ExcludeGenres, grp...)
		}
	}
	q.Unwatched = a.Unwatched
	if a.MaxMinutes >= 20 && a.MaxMinutes <= 600 {
		q.MaxMinutes = a.MaxMinutes
	}
	if a.MinMinutes >= 20 && a.MinMinutes <= 600 && (q.MaxMinutes == 0 || a.MinMinutes < q.MaxMinutes) {
		q.MinMinutes = a.MinMinutes
	}
	if a.HighlyRated {
		q.MinRating = 7.5
	}
	q.Family = a.KidFriendly
	var rest []string
	for _, p := range a.People {
		p = strings.TrimSpace(p)
		if p == "" || len(q.People) >= 3 {
			continue
		}
		if name, err := look.VideoPerson(ctx, p); err == nil && name != "" {
			q.People = append(q.People, name)
		} else {
			rest = append(rest, p)
		}
	}
	for _, l := range a.Like {
		if strings.TrimSpace(l.Title) == "" || len(q.Seeds) >= 3 {
			continue
		}
		id, title, ok, err := look.VideoByTitle(ctx, acc, l.Title, year(l.Year))
		if err == nil && !ok && l.Year > 0 {
			id, title, ok, err = look.VideoByTitle(ctx, acc, l.Title, 0)
		}
		if err == nil && ok {
			q.Seeds, q.SeedTitles = append(q.Seeds, id), append(q.SeedTitles, title)
		} else {
			rest = append(rest, l.Title)
		}
	}
	q.Rest = strings.TrimSpace(strings.Join(append([]string{strings.TrimSpace(a.Meaning)}, rest...), " "))
	if len(q.Rest) > 200 {
		q.Rest = q.Rest[:200]
	}
	q.Relabel()
	if q.Understood() == "Top rated" {
		return q, false // the answer found nothing at all in the prompt: trust the keywords instead
	}
	return q, true
}

func cleanTitle(t string) string {
	t = strings.Trim(strings.TrimSpace(t), `"'“”`)
	if len([]rune(t)) > 60 {
		return ""
	}
	return t
}

// ---------- re-ranking ----------

// Candidate is a movie or show offered to the AI.
type Candidate struct {
	ID       int64
	Title    string
	Year     int
	Synopsis string
}

type rerankAnswer struct {
	Results []struct {
		N      int    `json:"n"`
		Reason string `json:"reason"`
	} `json:"results"`
}

var rerankSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{"results": map[string]any{"type": "array", "items": map[string]any{"type": "object",
		"properties": map[string]any{"n": map[string]any{"type": "integer"}, "reason": map[string]any{"type": "string"}},
		"required":   []string{"n", "reason"}}}},
	"required": []string{"results"},
}

const rerankSystem = `You rank titles from a home media library for a viewer's request. Answer only with JSON.
Return "results": every candidate number "n", best match for the request first, each with a "reason": one short sentence (under 15 words) on why it fits. Don't invent titles.`

// Rerank asks the AI to order cands (already filtered) for prompt. It returns the ids in
// the new order (every candidate exactly once; ones the answer missed keep their order at
// the end) and a reason per id. err when the AI is unavailable or the answer is unusable.
func Rerank(ctx context.Context, llm LLM, prompt string, cands []Candidate) ([]int64, map[int64]string, error) {
	if llm == nil || !llm.Ready() {
		return nil, nil, fmt.Errorf("AI not ready")
	}
	if len(cands) < 2 {
		return nil, nil, fmt.Errorf("nothing to rank")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Request: %s\n\nCandidates:\n", prompt)
	for i, c := range cands {
		fmt.Fprintf(&b, "%d. %s", i+1, c.Title)
		if c.Year > 0 {
			fmt.Fprintf(&b, " (%d)", c.Year)
		}
		if c.Synopsis != "" {
			b.WriteString(" — " + c.Synopsis)
		}
		b.WriteByte('\n')
	}
	var a rerankAnswer
	if err := llm.Chat(ctx, rerankSystem, b.String(), rerankSchema, &a); err != nil {
		return nil, nil, err
	}
	order, reasons := applyRerank(cands, a)
	if len(reasons) == 0 {
		return nil, nil, fmt.Errorf("the answer named no candidates")
	}
	return order, reasons, nil
}

// applyRerank validates the answer: unknown or repeated numbers are ignored and missing
// candidates follow in their original order.
func applyRerank(cands []Candidate, a rerankAnswer) ([]int64, map[int64]string) {
	used := make([]bool, len(cands))
	order := make([]int64, 0, len(cands))
	reasons := map[int64]string{}
	for _, r := range a.Results {
		i := r.N - 1
		if i < 0 || i >= len(cands) || used[i] {
			continue
		}
		used[i] = true
		order = append(order, cands[i].ID)
		if s := cleanReason(r.Reason); s != "" {
			reasons[cands[i].ID] = s
		}
	}
	for i, c := range cands {
		if !used[i] {
			order = append(order, c.ID)
		}
	}
	return order, reasons
}

func cleanReason(s string) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > 140 {
		s = string(r[:140]) + "…"
	}
	return s
}

// ---------- reasons for recommendations ----------

// Reasons writes one-line "why this" reasons for recommended items, given titles the
// person enjoyed. Results are cached per person and item for a week.
type Reasons struct {
	mu    sync.Mutex
	cache map[reasonKey]reasonEntry
}

type reasonKey struct{ user, item int64 }

type reasonEntry struct {
	text string
	at   time.Time
}

const reasonTTL = 7 * 24 * time.Hour

const reasonsSystem = `You explain movie and TV recommendations in a home media library. Answer only with JSON.
For each numbered recommendation, give "reason": one short, specific sentence (under 15 words) on why this viewer may enjoy it, based on the titles they enjoyed. Don't invent facts.`

// Get returns reasons for cands (at most the first 5), from the cache or one AI call.
func (r *Reasons) Get(ctx context.Context, llm LLM, userID int64, liked []string, cands []Candidate) map[int64]string {
	out := map[int64]string{}
	if len(cands) > 5 {
		cands = cands[:5]
	}
	r.mu.Lock()
	if r.cache == nil {
		r.cache = map[reasonKey]reasonEntry{}
	}
	var todo []Candidate
	now := time.Now()
	for _, c := range cands {
		if e, ok := r.cache[reasonKey{userID, c.ID}]; ok && now.Sub(e.at) < reasonTTL {
			out[c.ID] = e.text
		} else {
			todo = append(todo, c)
		}
	}
	r.mu.Unlock()
	if len(todo) == 0 || llm == nil || !llm.Ready() || len(liked) == 0 {
		return out
	}
	var b strings.Builder
	fmt.Fprintf(&b, "The viewer enjoyed: %s\n\nRecommendations:\n", strings.Join(liked, "; "))
	for i, c := range todo {
		fmt.Fprintf(&b, "%d. %s", i+1, c.Title)
		if c.Year > 0 {
			fmt.Fprintf(&b, " (%d)", c.Year)
		}
		if c.Synopsis != "" {
			b.WriteString(" — " + c.Synopsis)
		}
		b.WriteByte('\n')
	}
	var a rerankAnswer
	if err := llm.Chat(ctx, reasonsSystem, b.String(), rerankSchema, &a); err != nil {
		slog.WarnContext(ctx, "muse: reasons failed", "err", err)
		return out
	}
	_, reasons := applyRerank(todo, a)
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.cache) > 20000 {
		for k, e := range r.cache {
			if now.Sub(e.at) >= reasonTTL || len(r.cache) > 15000 {
				delete(r.cache, k)
			}
		}
	}
	for id, text := range reasons {
		r.cache[reasonKey{userID, id}] = reasonEntry{text, now}
		out[id] = text
	}
	return out
}
