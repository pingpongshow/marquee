package vodsuggest

import (
	"context"
	"sort"
)

// The recommendations page: many rows of films or series on demand, each with its own
// angle, all from the person's own taste (never from what anyone else watched). A title
// shows in one row only, so the page goes deep instead of repeating itself.

// Row is one row of the recommendations page.
type Row struct {
	ID    string // top, because:<n>, genre:<name>, acclaimed, popular
	Title string
	Items []Suggestion
}

const (
	maxSeedRows  = 8 // Because you watched … rows
	maxGenreRows = 4 // More <genre> rows
	minRow       = 4 // fewer titles than this and the row isn't shown
)

// Rows are u's recommendation rows for kind ("" = films and series), up to perRow titles each.
func (s *Service) Rows(ctx context.Context, u User, kind string, perRow int) ([]Row, error) {
	e, err := s.current(ctx, u)
	if err != nil {
		return nil, err
	}
	x, err := s.exclusions(ctx, u, e.cat)
	if err != nil {
		return nil, err
	}
	used := map[key]bool{}
	ok := func(t *title) bool {
		return t != nil && (kind == "" || t.kind == kind) && !used[key{t.kind, t.tmdb}] && !x.excluded(t) && !e.prof.liked[key{t.kind, t.tmdb}]
	}
	var rows []Row
	// add makes a row from candidates in order (their own reason unless reason is set).
	add := func(id, title string, cands []scored, reason string) {
		var items []Suggestion
		var keys []key
		for _, c := range cands {
			if len(items) == perRow {
				break
			}
			k := key{c.t.kind, c.t.tmdb}
			if !ok(c.t) || contains(keys, k) {
				continue
			}
			sg := e.suggestion(c)
			if reason != "" {
				sg.Reason = reason
			}
			items = append(items, sg)
			keys = append(keys, k)
		}
		if len(items) < minRow {
			return
		}
		for _, k := range keys {
			used[k] = true
		}
		rows = append(rows, Row{ID: id, Title: title, Items: items})
	}

	if !e.prof.empty() {
		add("top", "Top picks for you", e.list, "")
	}

	// Because you watched …: the liked titles' own TMDB recommendations, in TMDB's order.
	n := 0
	seen := map[string]bool{}
	for _, sd := range strongestSeeds(e.prof) {
		if n == maxSeedRows {
			break
		}
		if (kind != "" && sd.kind != kind) || len(sd.recs) == 0 || seen[sd.name] {
			continue
		}
		seen[sd.name] = true
		var cands []scored
		for _, tm := range sd.recs {
			for _, t := range e.cat.byTMDB[key{sd.kind, tm}] {
				if ok(t) {
					cands = append(cands, scored{t: t, why: seedWhy(sd), seed: sd.name})
					break
				}
			}
		}
		before := len(rows)
		add("because:"+itoa(int64(n)), reasonText(seedWhy(sd), sd.name, ""), cands, "")
		if len(rows) > before {
			n++
		}
	}

	// More of their favourite genres, in taste order.
	for _, g := range topGenres(e.prof, maxGenreRows) {
		var cands []scored
		for _, c := range e.ranked {
			if containsStr(c.t.genres, g) {
				cands = append(cands, c)
			}
		}
		add("genre:"+g, "More "+titleCase(g), cands, "")
	}

	// Critically acclaimed: TMDB's best rated with plenty of votes, ordered by taste when
	// there is one.
	var acclaimed []scored
	for _, t := range e.cat.titles {
		if t.rating >= 7.8 && t.votes >= 500 {
			sc := t.rating
			if !e.prof.empty() && e.prof.top > 0 {
				p, _, _ := e.prof.score(t)
				sc += 5 * max(p, 0) / e.prof.top
			}
			acclaimed = append(acclaimed, scored{t: t, score: sc})
		}
	}
	sortScored(acclaimed)
	add("acclaimed", "Critically acclaimed", acclaimed, "Critically acclaimed")

	add("popular", "Popular on VOD", popular(e.cat, x), "Popular on VOD")
	return rows, nil
}

// strongestSeeds are the profile's liked titles, favourites first, then by weight.
func strongestSeeds(p *profile) []seed {
	if p.empty() {
		return nil
	}
	out := append([]seed(nil), p.seeds...)
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].favorite != out[j].favorite {
			return out[i].favorite
		}
		return out[i].weight > out[j].weight
	})
	return out
}

// topGenres are the profile's best-liked genres.
func topGenres(p *profile, n int) []string {
	if p.empty() {
		return nil
	}
	var out []string
	for g, v := range p.genre {
		if v > 0 && g != "" {
			out = append(out, g)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if p.genre[out[i]] != p.genre[out[j]] {
			return p.genre[out[i]] > p.genre[out[j]]
		}
		return out[i] < out[j]
	})
	return out[:min(n, len(out))]
}

func contains(list []key, k key) bool {
	for _, x := range list {
		if x == k {
			return true
		}
	}
	return false
}

func containsStr(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
