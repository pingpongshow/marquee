// Package vodsuggest suggests films and series on demand (LIVE-6) from each person's taste:
// Home's Watch on VOD row, Also on VOD on the library's movie and show pages, Muse, and the
// on-demand pages' Recommended for you. Suggestions leave out titles the person's
// libraries have, ones they started or watched, ones rated above their limit and ones
// they said they're not interested in.
//
// Each person's suggestions are worked out in the background and kept: again when their
// taste changes (a favourite, a rating, something watched), when the catalogue changed
// and they're a few hours old, and at least twice a day. Reading them is cheap. Nothing
// needs the AI model; Muse's re-ranking (when on) re-ranks the on-demand titles too.
package vodsuggest

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"marquee/internal/items"
	"marquee/internal/vod"
	"marquee/internal/vodsave"
)

// Suggestion is a film or series on demand suggested to someone.
type Suggestion struct {
	Kind   string // movie, series
	ID     int64
	TMDBID int
	Score  float64
	Reason string
}

// User is who suggestions are for, with what they may see.
type User struct {
	ID int64
	// Access is their library access (taste only counts titles they can see).
	Access items.Access
	// MaxRating limits on-demand titles to this content rating ("" = none).
	MaxRating string
	// Visibility is the libraries whose titles aren't suggested (they have them).
	Visibility vodsave.Visibility
}

func (u User) cacheKey() string {
	return fmt.Sprint(u.ID, "|", u.MaxRating, "|", u.Visibility.HideRecordings, "|", u.Visibility.LibraryIDs == nil, jsonInts(u.Visibility.LibraryIDs))
}

// Service works out and keeps everyone's suggestions.
type Service struct {
	DB    *sql.DB
	Items *items.Store
	// VOD fills in the TMDB recommendations of on-demand titles someone watched that were
	// matched before they were kept; may be nil.
	VOD *vod.Service
	// InLibrary is the index of on-demand titles the libraries have; nil = none.
	InLibrary func(ctx context.Context) (*vodsave.LibraryIndex, error)
	// Now is the clock (tests).
	Now func() time.Time
	// CheckEvery is how often a person's taste is checked for changes (default a minute).
	CheckEvery time.Duration
	// CatalogEvery is the least time between rebuilds for catalogue changes (default 3 h);
	// MaxAge rebuilds suggestions this old anyway (default 12 h).
	CatalogEvery, MaxAge time.Duration

	cat     catalogCache
	mu      sync.Mutex
	entries map[string]*entry
	wg      sync.WaitGroup
}

// keep is how many suggestions are kept per person.
const keep = 300

type entry struct {
	list     []scored
	prof     *profile
	cat      *catalog
	built    time.Time
	taste    string
	catalog  string
	checked  time.Time
	building bool
	stale    bool // e.g. TMDB extras were fetched: rebuild at the next check
}

func (s *Service) clock() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) checkEvery() time.Duration {
	if s.CheckEvery != 0 {
		return s.CheckEvery
	}
	return time.Minute
}

func dflt(d, def time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return def
}

// Wait waits for background rebuilds (tests).
func (s *Service) Wait() { s.wg.Wait() }

// Forget drops everyone's kept suggestions for uid (they're worked out again when next asked).
func (s *Service) Forget(uid int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	prefix := strconv.FormatInt(uid, 10) + "|"
	for k := range s.entries {
		if strings.HasPrefix(k, prefix) {
			delete(s.entries, k)
		}
	}
}

// current returns u's kept suggestions, working them out now the first time and in the
// background when they're out of date.
func (s *Service) current(ctx context.Context, u User) (*entry, error) {
	k := u.cacheKey()
	s.mu.Lock()
	if s.entries == nil {
		s.entries = map[string]*entry{}
	}
	e := s.entries[k]
	s.mu.Unlock()
	if e == nil {
		fresh, err := s.build(ctx, u)
		if err != nil {
			return nil, err
		}
		s.mu.Lock()
		if cur := s.entries[k]; cur != nil && cur.built.After(fresh.built) {
			fresh = cur
		} else {
			s.entries[k] = fresh
		}
		s.mu.Unlock()
		return fresh, nil
	}
	now := s.clock()
	s.mu.Lock()
	due := !e.building && now.Sub(e.checked) >= s.checkEvery()
	stale := e.stale
	if due {
		e.checked = now
	}
	s.mu.Unlock()
	if !due {
		return e, nil
	}
	taste, err := s.Items.TasteVersion(ctx, u.ID)
	if err != nil {
		return e, nil
	}
	taste += s.vodTasteVersion(ctx, u.ID)
	cat, err := s.catalog(ctx, false)
	if err != nil {
		return e, nil
	}
	age := now.Sub(e.built)
	rebuild := stale || taste != e.taste || age >= dflt(s.MaxAge, 12*time.Hour) ||
		(cat.version != e.catalog && age >= dflt(s.CatalogEvery, 3*time.Hour))
	if !rebuild {
		return e, nil
	}
	s.mu.Lock()
	if e.building {
		s.mu.Unlock()
		return e, nil
	}
	e.building = true
	s.mu.Unlock()
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		fresh, err := s.build(context.WithoutCancel(ctx), u)
		s.mu.Lock()
		defer s.mu.Unlock()
		e.building = false
		if err != nil {
			slog.Warn("vod suggestions", "user", u.ID, "err", err)
			return
		}
		s.entries[k] = fresh
	}()
	return e, nil
}

// vodTasteVersion changes when uid watches a film or episode on demand.
func (s *Service) vodTasteVersion(ctx context.Context, uid int64) string {
	var a, b sql.NullString
	s.DB.QueryRowContext(ctx, `SELECT MAX(updated_at) FROM vod_progress WHERE user_id=?`, uid).Scan(&a)
	s.DB.QueryRowContext(ctx, `SELECT MAX(updated_at) FROM vod_episode_progress WHERE user_id=?`, uid).Scan(&b)
	return "|" + a.String + "|" + b.String
}

// exclusions are the titles not to suggest to someone right now.
type exclusions struct {
	tmdb   map[key]bool // started or watched (any copy)
	hidden map[string]map[int64]bool
	idx    *vodsave.LibraryIndex
	vis    vodsave.Visibility
	max    string
}

func (x *exclusions) excluded(t *title) bool {
	if x.tmdb[key{t.kind, t.tmdb}] || x.hidden[t.kind][t.id] {
		return true
	}
	if !vod.Allowed(t.contentRating, x.max) {
		return true
	}
	return x.idx.Has(t.kind == Series, t.id, x.vis)
}

func (s *Service) exclusions(ctx context.Context, u User, cat *catalog) (*exclusions, error) {
	x := &exclusions{tmdb: map[key]bool{}, hidden: map[string]map[int64]bool{Movie: {}, Series: {}}, vis: u.Visibility, max: u.MaxRating}
	if s.InLibrary != nil {
		idx, err := s.InLibrary(ctx)
		if err != nil {
			slog.WarnContext(ctx, "vod suggestions: in library", "err", err)
		}
		x.idx = idx
	}
	add := func(kind, q string) error {
		rows, err := s.DB.QueryContext(ctx, q, u.ID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				return err
			}
			if t := cat.get(kind, id); t != nil {
				x.tmdb[key{kind, t.tmdb}] = true
			}
		}
		return rows.Err()
	}
	if err := add(Movie, `SELECT movie_id FROM vod_progress WHERE user_id=? AND (watched=1 OR position_ms>0)`); err != nil {
		return nil, err
	}
	if err := add(Series, `SELECT DISTINCT series_id FROM vod_episode_progress WHERE user_id=?`); err != nil {
		return nil, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT kind, vod_id FROM vod_suggestion_hidden WHERE user_id=?`, u.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var kind string
		var id int64
		if err := rows.Scan(&kind, &id); err != nil {
			return nil, err
		}
		if x.hidden[kind] != nil {
			x.hidden[kind][id] = true
		}
		if t := cat.get(kind, id); t != nil {
			x.tmdb[key{kind, t.tmdb}] = true // every copy of it
		}
	}
	return x, rows.Err()
}

// vodSeeds are the films and series on demand u watched: a film to the end, a series with
// three episodes watched (or all of them; fewer counts for less).
func (s *Service) vodSeeds(ctx context.Context, u User, cat *catalog) ([]seed, []*title, error) {
	var out []seed
	var missing []*title
	add := func(kind string, id int64, finished bool) {
		t := cat.get(kind, id)
		if t == nil {
			return
		}
		if t.recs == nil {
			missing = append(missing, t)
		}
		w := 1.0
		if !finished {
			w = 0.6
		}
		out = append(out, seed{name: t.name, kind: kind, tmdb: t.tmdb, weight: w, finished: finished, genres: t.genres,
			keywords: t.keywords, year: t.year, recs: t.recs})
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT movie_id FROM vod_progress WHERE user_id=? AND watched=1 ORDER BY updated_at DESC LIMIT 50`, u.ID)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			add(Movie, id, true)
		}
	}
	rows.Close()
	rows, err = s.DB.QueryContext(ctx, `SELECT p.series_id, COUNT(*), (SELECT COUNT(*) FROM vod_episodes e WHERE e.series_id=p.series_id)
		FROM vod_episode_progress p WHERE p.user_id=? AND p.watched=1 GROUP BY p.series_id ORDER BY MAX(p.updated_at) DESC LIMIT 50`, u.ID)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var id int64
		var watched, all int
		if rows.Scan(&id, &watched, &all) == nil {
			add(Series, id, watched >= 3 || (all > 0 && watched >= all))
		}
	}
	rows.Close()
	return out, missing, nil
}

// profile is u's taste as a profile.
func (s *Service) profile(ctx context.Context, u User, cat *catalog) (*profile, []*title, error) {
	taste, err := s.Items.Taste(ctx, u.Access, 100)
	if err != nil {
		return nil, nil, err
	}
	liked := librarySeeds(taste.Liked)
	vodLiked, missing, err := s.vodSeeds(ctx, u, cat)
	if err != nil {
		return nil, nil, err
	}
	liked = append(liked, vodLiked...)
	return buildProfile(liked, librarySeeds(taste.Disliked)), missing, nil
}

// build works out u's suggestions.
func (s *Service) build(ctx context.Context, u User) (*entry, error) {
	now := s.clock()
	taste, err := s.Items.TasteVersion(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	taste += s.vodTasteVersion(ctx, u.ID)
	cat, err := s.catalog(ctx, true)
	if err != nil {
		return nil, err
	}
	prof, missing, err := s.profile(ctx, u, cat)
	if err != nil {
		return nil, err
	}
	x, err := s.exclusions(ctx, u, cat)
	if err != nil {
		return nil, err
	}
	e := &entry{prof: prof, cat: cat, built: now, taste: taste, catalog: cat.version, checked: now}
	if !prof.empty() {
		ranked := s.rank(cat, prof, x, "", nil)
		if len(ranked) > 0 {
			prof.top = ranked[0].score
		}
		e.list = diversify(ranked, keep)
	} else {
		e.list = diversifyBy(popular(cat, x), keep, 0.85)
	}
	if len(missing) > 0 && s.VOD != nil {
		s.fetchExtras(u, missing)
	}
	return e, nil
}

// fetchExtras fetches, in the background, TMDB's recommendations for on-demand titles
// someone watched that were matched before they were kept (ten at a time), then has their
// suggestions worked out again.
func (s *Service) fetchExtras(u User, missing []*title) {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		ctx := context.Background()
		got := 0
		for i, t := range missing {
			if i == 10 {
				break
			}
			if err := s.VOD.FetchTMDBExtras(ctx, t.kind == Series, t.id); err != nil {
				slog.Debug("vod suggestions: TMDB extras", "title", t.name, "err", err)
				continue
			}
			got++
		}
		if got == 0 {
			return
		}
		s.mu.Lock()
		if e := s.entries[u.cacheKey()]; e != nil {
			e.stale, e.checked = true, time.Time{}
		}
		s.mu.Unlock()
	}()
}

// rank scores every title not excluded (of kind, "" = both; keepFn, when set, also has to
// accept it), best first, one copy per title.
func (s *Service) rank(cat *catalog, prof *profile, x *exclusions, kind string, keepFn func(*title) bool) []scored {
	var out []scored
	seen := map[key]bool{}
	for _, t := range cat.titles {
		if kind != "" && t.kind != kind {
			continue
		}
		k := key{t.kind, t.tmdb}
		if seen[k] || prof.liked[k] || x.excluded(t) || (keepFn != nil && !keepFn(t)) {
			continue
		}
		seen[k] = true
		sc, w, seedName := prof.score(t)
		if sc <= 0 {
			continue
		}
		out = append(out, scored{t: t, score: sc, why: w, seed: seedName})
	}
	sortScored(out)
	return out
}

// popular is, for someone with no taste yet, the well-rated popular titles they may see,
// best first, one copy per title.
func popular(cat *catalog, x *exclusions) []scored {
	var out []scored
	seen := map[key]bool{}
	for _, t := range cat.titles {
		k := key{t.kind, t.tmdb}
		if seen[k] || x.excluded(t) {
			continue
		}
		if sc := popularScore(t); sc > 0 {
			seen[k] = true
			out = append(out, scored{t: t, score: sc, why: whyPopular})
		}
	}
	sortScored(out)
	return out
}

// CatalogChanged has everyone's suggestions worked out again when next asked (after the
// catalogue's suggestion data was filled in).
func (s *Service) CatalogChanged() {
	s.cat.mu.Lock()
	s.cat.checkedAt = time.Time{}
	s.cat.mu.Unlock()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, e := range s.entries {
		e.stale, e.checked = true, time.Time{}
	}
}

// For returns up to limit suggestions for u, best first (kind "" = films and series).
func (s *Service) For(ctx context.Context, u User, kind string, limit int) ([]Suggestion, error) {
	e, err := s.current(ctx, u)
	if err != nil {
		return nil, err
	}
	if len(e.list) == 0 {
		return []Suggestion{}, nil
	}
	x, err := s.exclusions(ctx, u, e.cat)
	if err != nil {
		return nil, err
	}
	out := []Suggestion{}
	for _, c := range e.list {
		if len(out) == limit {
			break
		}
		if (kind != "" && c.t.kind != kind) || x.excluded(c.t) {
			continue
		}
		out = append(out, e.suggestion(c))
	}
	return out, nil
}

func (e *entry) suggestion(c scored) Suggestion {
	reason := c.reason
	if reason == "" {
		reason = reasonText(c.why, c.seed, e.prof.favouriteGenre(c.t))
	}
	return Suggestion{Kind: c.t.kind, ID: c.t.id, TMDBID: c.t.tmdb, Score: round(c.score), Reason: reason}
}

func round(f float64) float64 { return float64(int64(f*1000+0.5)) / 1000 }

// Hide stops suggesting a film or series to uid. It reports false when there's no such title.
func (s *Service) Hide(ctx context.Context, uid int64, kind string, id int64) (bool, error) {
	table := "vod_movies"
	if kind == Series {
		table = "vod_series"
	} else if kind != Movie {
		return false, nil
	}
	var n int
	if err := s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table+" WHERE id=?", id).Scan(&n); err != nil || n == 0 {
		return false, err
	}
	_, err := s.DB.ExecContext(ctx, `INSERT INTO vod_suggestion_hidden(user_id, kind, vod_id, hidden_at) VALUES(?,?,?,?)
		ON CONFLICT DO NOTHING`, uid, kind, id, s.clock().UTC().Format(time.RFC3339))
	return err == nil, err
}

// OnDemand finds which of the given TMDB ids (films and TV shows) are on demand for someone
// limited to maxRating: TMDB id → the film's (or series') id (the first source's, when
// several have it).
func (s *Service) OnDemand(ctx context.Context, maxRating string, movies, shows []int64) (map[int64]int64, map[int64]int64, error) {
	find := func(table string, ids []int64) (map[int64]int64, error) {
		out := map[int64]int64{}
		if len(ids) == 0 {
			return out, nil
		}
		rows, err := s.DB.QueryContext(ctx, `SELECT m.id, m.tmdb_id, m.content_rating FROM `+table+` m
			WHERE m.match_state='matched' AND m.tmdb_id IN (SELECT value FROM json_each(?)) ORDER BY m.id`, jsonInts(ids))
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var id, tmdb int64
			var rating string
			if err := rows.Scan(&id, &tmdb, &rating); err != nil {
				return nil, err
			}
			if _, ok := out[tmdb]; !ok && vod.Allowed(rating, maxRating) {
				out[tmdb] = id
			}
		}
		return out, rows.Err()
	}
	m, err := find("vod_movies", movies)
	if err != nil {
		return nil, nil, err
	}
	t, err := find("vod_series", shows)
	return m, t, err
}

func jsonInts(ids []int64) string {
	if ids == nil {
		ids = []int64{}
	}
	b, _ := json.Marshal(ids)
	return string(b)
}

// Similar is Also on VOD for a library movie or show: on-demand titles of its kind that
// TMDB recommends for it or that share its genres and keywords, ordered also by u's taste.
func (s *Service) Similar(ctx context.Context, u User, it items.TasteTitle, limit int) ([]Suggestion, error) {
	if it.Type != "movie" && it.Type != "show" {
		return []Suggestion{}, nil
	}
	e, err := s.current(ctx, u)
	if err != nil {
		return nil, err
	}
	x, err := s.exclusions(ctx, u, e.cat)
	if err != nil {
		return nil, err
	}
	kind := kindOf(it.Type)
	recRank := map[int]int{}
	for i, tm := range it.Recs {
		recRank[tm] = i
	}
	genres, keywords := set(lower(it.Genres)), set(lower(it.Keywords))
	var ranked []scored
	seen := map[key]bool{}
	for _, t := range e.cat.titles {
		k := key{t.kind, t.tmdb}
		if t.kind != kind || seen[k] || t.tmdb == it.TMDBID || x.excluded(t) {
			continue
		}
		var rec float64
		if r, ok := recRank[t.tmdb]; ok {
			rec = rankWeight(r)
		}
		g := overlap(genres, t.genres)
		kw := overlap(keywords, t.keywords)
		if rec == 0 && (g < 0.5 || len(genres) == 0) && kw < 0.2 {
			continue
		}
		seen[k] = true
		taste := 0.0
		if !e.prof.empty() && e.prof.top > 0 {
			sc, _, _ := e.prof.score(t)
			taste = max(sc, 0) / e.prof.top
		}
		sc := rec + 0.4*g + 0.3*kw + 0.3*taste + 0.1*t.rating/10
		ranked = append(ranked, scored{t: t, score: sc, reason: "Similar to " + it.Title})
	}
	sortScored(ranked)
	out := []Suggestion{}
	for _, c := range diversify(ranked, limit) {
		out = append(out, e.suggestion(c))
	}
	return out, nil
}

func set(list []string) map[string]bool {
	out := map[string]bool{}
	for _, s := range list {
		if s != "" {
			out[s] = true
		}
	}
	return out
}

// overlap is the share of want that have has (0 when want is empty).
func overlap(want map[string]bool, have []string) float64 {
	if len(want) == 0 || len(have) == 0 {
		return 0
	}
	n := 0
	for _, h := range have {
		if want[h] {
			n++
		}
	}
	return float64(n) / float64(min(len(want), max(len(have), 1)))
}

// MuseQuery is what Muse read from a prompt, as far as on-demand titles can be matched:
// the hard filters, and what steers the ranking.
type MuseQuery struct {
	Kinds            []string // movie, series (nil = both)
	YearFrom, YearTo int
	Genres           [][]string // each group: acceptable genres; all groups must match
	ExcludeGenres    []string
	MinRating        float64
	MaxRating        string // content rating limit from the prompt
	MaxMinutes       int    // films only
	MinMinutes       int
	Keywords         []string
	Words            string             // the prompt's free text
	Seeds            []items.TasteTitle // "like <title>": library titles
}

// Muse returns up to limit on-demand titles that fit a Muse prompt, best first: they pass
// its filters, then rank by the prompt (titles TMDB recommends for the "like" titles, its
// keywords and words) and u's taste.
func (s *Service) Muse(ctx context.Context, u User, q MuseQuery, limit int) ([]Suggestion, error) {
	e, err := s.current(ctx, u)
	if err != nil {
		return nil, err
	}
	x, err := s.exclusions(ctx, u, e.cat)
	if err != nil {
		return nil, err
	}
	seedRec := map[key]float64{}
	seedOf := map[key]string{}
	for _, sd := range q.Seeds {
		kind := kindOf(sd.Type)
		for r, tm := range sd.Recs {
			k := key{kind, tm}
			if w := rankWeight(r); w > seedRec[k] {
				seedRec[k], seedOf[k] = w, sd.Title
			}
		}
	}
	kws := set(lower(q.Keywords))
	words := promptWords(q.Words)
	exclude := set(lower(q.ExcludeGenres))
	groups := make([]map[string]bool, len(q.Genres))
	for i, g := range q.Genres {
		groups[i] = set(lower(g))
	}
	structured := len(q.Genres) > 0 || q.YearFrom > 0 || q.YearTo > 0 || q.MinRating > 0 || len(q.Kinds) > 0
	var ranked []scored
	seen := map[key]bool{}
	for _, t := range e.cat.titles {
		k := key{t.kind, t.tmdb}
		if seen[k] || x.excluded(t) || !museAllows(q, t, groups, exclude) {
			continue
		}
		rec := seedRec[k]
		var kw float64
		for _, w := range t.keywords {
			if kws[w] {
				kw++
			}
		}
		wm := wordMatch(words, t)
		if rec == 0 && kw == 0 && wm == 0 && (!structured || len(words) > 0 && len(q.Genres) == 0) {
			continue // nothing in the prompt leads to it
		}
		seen[k] = true
		taste := 0.0
		var reason string
		if !e.prof.empty() && e.prof.top > 0 {
			sc, w, seedName := e.prof.score(t)
			taste = max(sc, 0) / e.prof.top
			if w >= whySimilar {
				reason = reasonText(w, seedName, "")
			}
		}
		if rec > 0 {
			reason = "Like " + seedOf[k]
		}
		sc := 1.0*rec + 0.12*min(kw, 2) + 0.3*wm + 0.4*taste + 0.1*t.rating/10
		ranked = append(ranked, scored{t: t, score: sc, reason: reason})
	}
	sortScored(ranked)
	out := []Suggestion{}
	for _, c := range diversify(ranked, limit) {
		sg := e.suggestion(c)
		sg.Reason = c.reason
		out = append(out, sg)
	}
	return out, nil
}

func museAllows(q MuseQuery, t *title, groups []map[string]bool, exclude map[string]bool) bool {
	if len(q.Kinds) > 0 && !slices.Contains(q.Kinds, t.kind) {
		return false
	}
	if (q.YearFrom > 0 || q.YearTo > 0) && t.year == 0 {
		return false
	}
	if q.YearFrom > 0 && t.year < q.YearFrom || q.YearTo > 0 && t.year > q.YearTo {
		return false
	}
	if q.MinRating > 0 && t.rating < q.MinRating {
		return false
	}
	if q.MaxRating != "" && !vod.Allowed(t.contentRating, q.MaxRating) {
		return false
	}
	if t.kind == Movie && q.MaxMinutes > 0 && (t.runtimeMS <= 0 || t.runtimeMS > int64(q.MaxMinutes)*60000) {
		return false
	}
	if t.kind == Movie && q.MinMinutes > 0 && t.runtimeMS < int64(q.MinMinutes)*60000 {
		return false
	}
	for _, grp := range groups {
		ok := false
		for _, g := range t.genres {
			if grp[g] {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	for _, g := range t.genres {
		if exclude[g] {
			return false
		}
	}
	return true
}

var stopWords = set(strings.Fields(`a an and the of with in on for to about from by or that like movie movies film films show shows series tv
	something some me i want watch watching good great best new old`))

func promptWords(s string) []string {
	var out []string
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r > 127)
	}) {
		if len(w) > 2 && !stopWords[w] {
			out = append(out, w)
		}
	}
	return out
}

// wordMatch is the share of the prompt's words found in a title's name, genres or keywords.
func wordMatch(words []string, t *title) float64 {
	if len(words) == 0 {
		return 0
	}
	hay := " " + strings.ToLower(t.name) + " " + strings.Join(t.genres, " ") + " " + strings.Join(t.keywords, " ") + " "
	n := 0
	for _, w := range words {
		if strings.Contains(hay, w) {
			n++
		}
	}
	return float64(n) / float64(len(words))
}
