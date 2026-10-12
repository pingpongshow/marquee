package items

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// VideoQuery is how Muse for movies (USER-15) reads a prompt: hard constraints parsed with
// simple rules, and what's left to rank by meaning.
type VideoQuery struct {
	Types            []string   // movie, show (nil = both)
	YearFrom, YearTo int        // 0 = open
	Genres           [][]string // each group is a set of acceptable genre names; all groups must match
	Unwatched        bool       // not started
	MaxMinutes       int        // movies only; 0 = any
	MinMinutes       int
	MinRating        float64 // audience/IMDb/critic rating, 0–10
	Family           bool    // rated G/PG/TV-Y/TV-G/TV-PG (or unrated family/animation/kids titles)
	Rest             string  // the prompt without the constraint words, for the embedding
	// From the AI reading only (the keyword parser leaves them empty):
	People        []string // director or cast names in the library; every one must be credited
	Seeds         []int64  // "like <title>": library items whose meaning steers the ranking
	SeedTitles    []string
	ExcludeGenres []string // "not horror"
	labels        []string
}

// GenreGroup is the set of library genre names that satisfy name (e.g. "Science Fiction"
// also accepts "Sci-Fi & Fantasy"), or nil when the library has no such genre.
func GenreGroup(name string, genres []string) []string {
	k := strings.ToLower(strings.TrimSpace(name))
	if k == "" {
		return nil
	}
	have := map[string]string{}
	for _, g := range genres {
		have[strings.ToLower(g)] = g
	}
	if g, ok := have[k]; ok {
		if syn, ok := genreSynonyms[k]; ok && len(syn) == 1 {
			return append([]string{g}, without(syn[0], g)...)
		}
		return []string{g}
	}
	if syn, ok := genreSynonyms[k]; ok && len(syn) == 1 {
		return syn[0]
	}
	return nil
}

func without(list []string, s string) []string {
	var out []string
	for _, x := range list {
		if !strings.EqualFold(x, s) {
			out = append(out, x)
		}
	}
	return out
}

// Relabel rebuilds the "understood" labels from the fields (for a query filled in by the
// AI reading rather than ParseVideoPrompt).
func (q *VideoQuery) Relabel() {
	var lbl []string
	switch {
	case len(q.Types) == 1 && q.Types[0] == "movie":
		lbl = append(lbl, "Movies")
	case len(q.Types) == 1 && q.Types[0] == "show":
		lbl = append(lbl, "Shows")
	}
	switch {
	case q.YearFrom > 0 && q.YearTo == q.YearFrom+9 && q.YearFrom%10 == 0:
		lbl = append(lbl, fmt.Sprintf("%ds", q.YearFrom))
	case q.YearFrom > 0 && q.YearFrom == q.YearTo:
		lbl = append(lbl, strconv.Itoa(q.YearFrom))
	case q.YearFrom > 0 && q.YearTo > 0:
		lbl = append(lbl, fmt.Sprintf("%d–%d", q.YearFrom, q.YearTo))
	case q.YearFrom > 0:
		lbl = append(lbl, fmt.Sprintf("%d or later", q.YearFrom))
	case q.YearTo > 0:
		lbl = append(lbl, fmt.Sprintf("%d or earlier", q.YearTo))
	}
	for _, g := range q.Genres {
		lbl = append(lbl, g[0])
	}
	for _, g := range q.ExcludeGenres {
		lbl = append(lbl, "Not "+g)
	}
	for _, p := range q.People {
		lbl = append(lbl, "With "+p)
	}
	for _, t := range q.SeedTitles {
		lbl = append(lbl, "Like "+t)
	}
	if q.Unwatched {
		lbl = append(lbl, "Unwatched")
	}
	if q.MaxMinutes > 0 {
		lbl = append(lbl, fmt.Sprintf("Under %d min", q.MaxMinutes))
	}
	if q.MinMinutes > 0 {
		lbl = append(lbl, fmt.Sprintf("Over %d min", q.MinMinutes))
	}
	if q.MinRating > 0 {
		lbl = append(lbl, "Highly rated")
	}
	if q.Family {
		lbl = append(lbl, "Family-friendly")
	}
	q.labels = lbl
}

// Understood describes the reading, e.g. "Movies · 1990s · Science Fiction · like 'time travel'".
func (q VideoQuery) Understood() string {
	parts := append([]string{}, q.labels...)
	if q.Rest != "" {
		parts = append(parts, "like '"+q.Rest+"'")
	}
	if len(parts) == 0 {
		return "Top rated"
	}
	return strings.Join(parts, " · ")
}

// genreSynonyms maps words people use to the genre names TMDB gives movies and shows. Each
// entry is a list of groups: rom-com needs Romance and Comedy.
var genreSynonyms = map[string][][]string{
	"sci-fi": {{"Science Fiction", "Sci-Fi & Fantasy"}}, "scifi": {{"Science Fiction", "Sci-Fi & Fantasy"}},
	"sci fi": {{"Science Fiction", "Sci-Fi & Fantasy"}}, "science fiction": {{"Science Fiction", "Sci-Fi & Fantasy"}},
	"science-fiction": {{"Science Fiction", "Sci-Fi & Fantasy"}},
	"rom-com":         {{"Romance"}, {"Comedy"}}, "romcom": {{"Romance"}, {"Comedy"}}, "rom com": {{"Romance"}, {"Comedy"}},
	"romcoms": {{"Romance"}, {"Comedy"}}, "rom-coms": {{"Romance"}, {"Comedy"}}, "romantic comedy": {{"Romance"}, {"Comedy"}},
	"romantic comedies": {{"Romance"}, {"Comedy"}},
	"romance":           {{"Romance"}},
	"comedy":            {{"Comedy"}}, "comedies": {{"Comedy"}}, "funny": {{"Comedy"}}, "sitcom": {{"Comedy"}}, "sitcoms": {{"Comedy"}},
	"horror": {{"Horror"}}, "horrors": {{"Horror"}}, "slasher": {{"Horror"}},
	"thriller": {{"Thriller"}}, "thrillers": {{"Thriller"}},
	"action": {{"Action", "Action & Adventure"}}, "adventure": {{"Adventure", "Action & Adventure"}},
	"adventures": {{"Adventure", "Action & Adventure"}},
	"animated":   {{"Animation"}}, "animation": {{"Animation"}}, "cartoon": {{"Animation"}}, "cartoons": {{"Animation"}},
	"documentary": {{"Documentary"}}, "documentaries": {{"Documentary"}}, "docs": {{"Documentary"}}, "docuseries": {{"Documentary"}},
	"drama": {{"Drama"}}, "dramas": {{"Drama"}},
	"crime": {{"Crime"}}, "mystery": {{"Mystery"}}, "mysteries": {{"Mystery"}}, "whodunit": {{"Mystery"}},
	"fantasy": {{"Fantasy", "Sci-Fi & Fantasy"}},
	"western": {{"Western"}}, "westerns": {{"Western"}},
	"war": {{"War", "War & Politics"}}, "musical": {{"Music"}}, "musicals": {{"Music"}},
	"history": {{"History"}}, "historical": {{"History"}},
	"reality": {{"Reality"}}, "anime": {{"Animation"}},
}

var (
	reSpace = regexp.MustCompile(`\s+`)
	// Decades: 90s, 90's, ’90s, 1990s, 2000s.
	reDecade4 = regexp.MustCompile(`\b(19|20)(\d)0[’']?s\b`)
	reDecade2 = regexp.MustCompile(`(?:^|\s)[’']?(\d)0[’']?s\b`)
	// Year bounds.
	reBetween = regexp.MustCompile(`\bbetween\s+((?:19|20)\d\d)\s+(?:and|&|-)\s+((?:19|20)\d\d)\b`)
	reFrom    = regexp.MustCompile(`\b(from|since|after|newer than|later than|post)\s+((?:19|20)\d\d)\b`)
	reBefore  = regexp.MustCompile(`\b(before|until|up to|older than|pre|prior to)\s+((?:19|20)\d\d)\b`)
	reYear    = regexp.MustCompile(`\b(?:in\s+|from\s+)?((?:19|20)\d\d)\b`)
	// Length: "under 90 minutes", "less than 2 hours", "over two hours".
	reUnder = regexp.MustCompile(`\b(?:under|less than|shorter than|no more than|at most|max|below)\s+(an|one|two|three|\d+(?:\.\d+)?)\s*(minutes|minute|mins|min|hours|hour|hrs|hr|h|m)\b`)
	reOver  = regexp.MustCompile(`\b(?:over|more than|longer than|at least)\s+(an|one|two|three|\d+(?:\.\d+)?)\s*(minutes|minute|mins|min|hours|hour|hrs|hr|h|m)\b`)
)

var decadeWords = map[string]int{"twenties": 1920, "thirties": 1930, "forties": 1940, "fifties": 1950, "sixties": 1960,
	"seventies": 1970, "eighties": 1980, "nineties": 1990, "noughties": 2000, "aughts": 2000}

// phrase lists, matched as whole words (longest first).
var (
	movieWords     = []string{"movies", "movie", "films", "film", "flicks", "flick", "feature"}
	showWords      = []string{"tv shows", "tv show", "tv series", "shows", "show", "series", "tv", "miniseries", "season"}
	unwatchedWords = []string{"i haven't seen", "i have not seen", "i haven't watched", "i have not watched", "haven't seen", "have not seen",
		"haven't watched", "not seen", "not watched", "never seen", "never watched", "unwatched", "unseen", "new to me", "i've not seen"}
	shortWords  = []string{"short", "quick", "brief"}
	longWords   = []string{"long", "epic", "lengthy"}
	ratedWords  = []string{"critically acclaimed", "highly rated", "highly-rated", "top rated", "top-rated", "well reviewed", "well-reviewed", "award winning", "award-winning", "acclaimed", "the best", "best", "masterpiece", "masterpieces"}
	familyWords = []string{"for the whole family", "for the family", "family friendly", "family-friendly", "kid friendly", "kid-friendly", "for children", "for kids", "for my kids", "children's", "childrens", "children", "kids", "kid", "family"}
	stopWords   = map[string]bool{"a": true, "an": true, "the": true, "some": true, "something": true, "anything": true, "i": true, "me": true,
		"want": true, "wanna": true, "to": true, "watch": true, "with": true, "about": true, "and": true, "or": true, "of": true, "for": true,
		"please": true, "give": true, "find": true, "show me": true, "recommend": true, "like": true, "that": true, "is": true, "are": true,
		"in": true, "from": true, "good": true, "great": true, "can": true, "you": true, "suggest": true, "my": true, "we": true, "us": true,
		"tonight": true, "on": true, "which": true, "set": true, "where": true, "something's": true}
	fillerPhrases = []string{"show me", "give me", "find me", "i want to watch", "i'd like to watch", "i'd like", "in the mood for", "recommend me"}
	phraseRes     sync.Map // phrase → *regexp.Regexp
)

// cut removes the first whole-word match of any phrase from s and reports whether one
// matched.
func cut(s *string, phrases []string) bool {
	for _, p := range phrases {
		re, ok := phraseRes.Load(p)
		if !ok {
			re, _ = phraseRes.LoadOrStore(p, regexp.MustCompile(`(^|[^\pL\pN'’-])`+regexp.QuoteMeta(p)+`($|[^\pL\pN'’-])`))
		}
		loc := re.(*regexp.Regexp).FindStringIndex(*s)
		if loc != nil {
			*s = (*s)[:loc[0]] + " " + (*s)[loc[1]:]
			return true
		}
	}
	return false
}

// stripStops turns full stops into spaces but keeps decimal points (2.5 hours).
func stripStops(s string) string {
	b := []byte(s)
	digit := func(i int) bool { return i >= 0 && i < len(b) && b[i] >= '0' && b[i] <= '9' }
	for i, c := range b {
		if c == '.' && !(digit(i-1) && digit(i+1)) {
			b[i] = ' '
		}
	}
	return string(b)
}

func numberWord(s string) float64 {
	switch s {
	case "an", "one":
		return 1
	case "two":
		return 2
	case "three":
		return 3
	}
	f, _ := strconv.ParseFloat(s, 64)
	return f
}

func toMinutes(n float64, unit string) int {
	if strings.HasPrefix(unit, "h") {
		return int(n * 60)
	}
	return int(n)
}

// ParseVideoPrompt reads hard constraints from a Muse prompt. genres are the genre names
// in the person's libraries, matched as words in addition to the common synonyms.
func ParseVideoPrompt(prompt string, genres []string) VideoQuery {
	var q VideoQuery
	s := " " + strings.ToLower(reSpace.ReplaceAllString(strings.TrimSpace(prompt), " ")) + " "
	s = strings.NewReplacer("’", "'", "“", " ", "”", " ", "\"", " ", ",", " , ", "!", " ", "?", " ", ";", " ").Replace(s)
	s = stripStops(s)
	var lbl []string
	for cut(&s, fillerPhrases) {
	}

	// Types.
	movies, shows := cut(&s, movieWords), cut(&s, showWords)
	for cut(&s, movieWords) {
	}
	for cut(&s, showWords) {
	}
	switch {
	case movies && !shows:
		q.Types, lbl = []string{"movie"}, append(lbl, "Movies")
	case shows && !movies:
		q.Types, lbl = []string{"show"}, append(lbl, "Shows")
	}

	// Years and decades.
	if m := reBetween.FindStringSubmatch(s); m != nil {
		a, _ := strconv.Atoi(m[1])
		b, _ := strconv.Atoi(m[2])
		q.YearFrom, q.YearTo = min(a, b), max(a, b)
		s = strings.Replace(s, m[0], " ", 1)
	}
	if m := reFrom.FindStringSubmatch(s); m != nil {
		y, _ := strconv.Atoi(m[2])
		if m[1] == "after" || m[1] == "newer than" || m[1] == "later than" || m[1] == "post" {
			y++
		}
		q.YearFrom = y
		s = strings.Replace(s, m[0], " ", 1)
	}
	if m := reBefore.FindStringSubmatch(s); m != nil {
		y, _ := strconv.Atoi(m[2])
		if m[1] != "until" && m[1] != "up to" {
			y--
		}
		q.YearTo = y
		s = strings.Replace(s, m[0], " ", 1)
	}
	decade := 0
	if m := reDecade4.FindStringSubmatch(s); m != nil {
		decade, _ = strconv.Atoi(m[1] + m[2] + "0")
		s = strings.Replace(s, m[0], " ", 1)
	} else if m := reDecade2.FindStringSubmatch(s); m != nil {
		d, _ := strconv.Atoi(m[1])
		decade = 1900 + d*10
		if d <= 2 {
			decade = 2000 + d*10
		}
		s = strings.Replace(s, strings.TrimLeft(m[0], " "), " ", 1)
	} else {
		for w, d := range decadeWords {
			if cut(&s, []string{"the " + w, w}) {
				decade = d
				break
			}
		}
	}
	if decade > 0 {
		q.YearFrom, q.YearTo = decade, decade+9
	} else if q.YearFrom == 0 && q.YearTo == 0 {
		if m := reYear.FindStringSubmatch(s); m != nil {
			y, _ := strconv.Atoi(m[1])
			q.YearFrom, q.YearTo = y, y
			s = strings.Replace(s, m[0], " ", 1)
		}
	}
	switch {
	case decade > 0:
		lbl = append(lbl, fmt.Sprintf("%ds", decade))
	case q.YearFrom > 0 && q.YearFrom == q.YearTo:
		lbl = append(lbl, strconv.Itoa(q.YearFrom))
	case q.YearFrom > 0 && q.YearTo > 0:
		lbl = append(lbl, fmt.Sprintf("%d–%d", q.YearFrom, q.YearTo))
	case q.YearFrom > 0:
		lbl = append(lbl, fmt.Sprintf("%d or later", q.YearFrom))
	case q.YearTo > 0:
		lbl = append(lbl, fmt.Sprintf("%d or earlier", q.YearTo))
	}

	// Family before genres, so "family" means the ratings rather than the Family genre.
	if cut(&s, familyWords) {
		for cut(&s, familyWords) {
		}
		q.Family = true
	}

	// Genres: synonyms and the libraries' own genre names, longest phrase first.
	cands := map[string][][]string{}
	for k, v := range genreSynonyms {
		cands[k] = v
	}
	for _, g := range genres {
		k := strings.ToLower(g)
		if _, ok := cands[k]; !ok && k != "family" {
			cands[k] = [][]string{{g}}
		}
	}
	keys := make([]string, 0, len(cands))
	for k := range cands {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if len(keys[i]) != len(keys[j]) {
			return len(keys[i]) > len(keys[j])
		}
		return keys[i] < keys[j]
	})
	seen := map[string]bool{}
	for _, k := range keys {
		if !cut(&s, []string{k}) {
			continue
		}
		for cut(&s, []string{k}) {
		}
		for _, grp := range cands[k] {
			if !seen[grp[0]] {
				seen[grp[0]] = true
				q.Genres = append(q.Genres, grp)
				lbl = append(lbl, grp[0])
			}
		}
	}

	if cut(&s, unwatchedWords) {
		q.Unwatched, lbl = true, append(lbl, "Unwatched")
	}
	if m := reUnder.FindStringSubmatch(s); m != nil {
		q.MaxMinutes = toMinutes(numberWord(m[1]), m[2])
		s = strings.Replace(s, m[0], " ", 1)
	} else if cut(&s, []string{"under an hour"}) {
		q.MaxMinutes = 60
	} else if cut(&s, shortWords) {
		q.MaxMinutes = 100
	}
	if m := reOver.FindStringSubmatch(s); m != nil {
		q.MinMinutes = toMinutes(numberWord(m[1]), m[2])
		s = strings.Replace(s, m[0], " ", 1)
	} else if cut(&s, longWords) {
		for cut(&s, longWords) {
		}
		q.MinMinutes = 140
	}
	if q.MaxMinutes > 0 {
		lbl = append(lbl, fmt.Sprintf("Under %d min", q.MaxMinutes))
	}
	if q.MinMinutes > 0 {
		lbl = append(lbl, fmt.Sprintf("Over %d min", q.MinMinutes))
	}
	if cut(&s, ratedWords) {
		q.MinRating, lbl = 7.5, append(lbl, "Highly rated")
	}
	if q.Family {
		lbl = append(lbl, "Family-friendly")
	}

	// What's left, minus filler words.
	var rest []string
	for _, w := range strings.Fields(s) {
		if w == "," || stopWords[w] {
			continue
		}
		rest = append(rest, w)
	}
	q.Rest = strings.Join(rest, " ")
	q.labels = lbl
	return q
}

// familyMaxLevel is the highest rating level a family search allows (PG / TV-PG).
const familyMaxLevel = 3

// notStarted is an SQL condition (alias i) that uid hasn't watched or started the item;
// a show counts as started once any episode is.
func notStarted(uid int64) string {
	u := strconv.FormatInt(uid, 10)
	return `NOT EXISTS (SELECT 1 FROM user_item_state x WHERE x.user_id = ` + u + ` AND x.item_id = i.id AND (x.play_count > 0 OR x.view_offset_ms > 0))
		AND NOT EXISTS (SELECT 1 FROM items e JOIN user_item_state x ON x.item_id = e.id AND x.user_id = ` + u + `
			AND (x.play_count > 0 OR x.view_offset_ms > 0) WHERE i.type = 'show' AND e.grandparent_id = i.id AND e.type = 'episode')`
}

// VideoGenres lists the genre names of movies and shows.
func (s *Store) VideoGenres(ctx context.Context) ([]string, error) {
	var out []string
	err := eachRow(ctx, s.db, `SELECT DISTINCT t.name FROM tags t JOIN item_tags it ON it.tag_id = t.id
		JOIN items i ON i.id = it.item_id AND i.type IN ('movie', 'show') WHERE t.kind = 'genre'`, nil, func(r *sql.Rows) error {
		var n string
		if err := r.Scan(&n); err != nil {
			return err
		}
		out = append(out, n)
		return nil
	})
	return out, err
}

// MuseVideoResult is what Muse for movies found.
type MuseVideoResult struct {
	Items    []Summary
	Analysed float64 // share of the searched movies and shows that are embedded
}

// MuseVideo filters movies and shows acc can see by q's hard constraints and ranks the rest
// by cosine to vec (the embedded prompt), or by rating when vec is nil. libID 0 = all.
func (s *Store) MuseVideo(ctx context.Context, acc Access, idx *VideoIndex, q VideoQuery, vec []float32, libID int64, limit int) (MuseVideoResult, error) {
	types := q.Types
	if len(types) == 0 {
		types = []string{"movie", "show"}
	}
	ac, aargs := acc.clause()
	base := ` WHERE i.type IN ('` + strings.Join(types, "','") + `') AND i.extra_type IS NULL AND ` + ac
	args := append([]any{}, aargs...)
	if libID > 0 {
		base += ` AND i.library_id = ?`
		args = append(args, libID)
	}
	// How much of what's searched is embedded.
	var res MuseVideoResult
	var pool []int64
	err := eachRow(ctx, s.db, `SELECT i.id`+summaryFrom+base, args, func(r *sql.Rows) error {
		var id int64
		if err := r.Scan(&id); err != nil {
			return err
		}
		pool = append(pool, id)
		return nil
	})
	if err != nil {
		return res, err
	}
	res.Items = []Summary{}
	if len(pool) == 0 {
		return res, nil
	}
	embedded := 0
	for _, id := range pool {
		if idx.Get(id) != nil {
			embedded++
		}
	}
	res.Analysed = float64(embedded) / float64(len(pool))

	f := Filter{YearFrom: q.YearFrom, YearTo: q.YearTo, MinRating: q.MinRating}
	fc, fargs := f.clause(acc.UserID)
	where, wargs := base+` AND `+fc, append(append([]any{}, args...), fargs...)
	for _, grp := range q.Genres {
		where += ` AND EXISTS (SELECT 1 FROM item_tags it JOIN tags t ON t.id = it.tag_id AND t.kind = 'genre'
			WHERE it.item_id = i.id AND t.name COLLATE NOCASE IN (?` + strings.Repeat(",?", len(grp)-1) + `))`
		for _, g := range grp {
			wargs = append(wargs, g)
		}
	}
	if q.Unwatched {
		where += ` AND ` + notStarted(acc.UserID)
	}
	if q.MaxMinutes > 0 {
		where += ` AND (i.type != 'movie' OR COALESCE(i.duration_ms, 0) BETWEEN 1 AND ?)`
		wargs = append(wargs, int64(q.MaxMinutes)*60000)
	}
	if q.MinMinutes > 0 {
		where += ` AND (i.type != 'movie' OR COALESCE(i.duration_ms, 0) >= ?)`
		wargs = append(wargs, int64(q.MinMinutes)*60000)
	}
	for _, p := range q.People {
		where += ` AND EXISTS (SELECT 1 FROM credits c JOIN people p ON p.id = c.person_id WHERE c.item_id = i.id AND p.name = ? COLLATE NOCASE)`
		wargs = append(wargs, p)
	}
	if len(q.ExcludeGenres) > 0 {
		where += ` AND NOT EXISTS (SELECT 1 FROM item_tags it JOIN tags t ON t.id = it.tag_id AND t.kind = 'genre'
			WHERE it.item_id = i.id AND t.name COLLATE NOCASE IN (?` + strings.Repeat(",?", len(q.ExcludeGenres)-1) + `))`
		for _, g := range q.ExcludeGenres {
			wargs = append(wargs, g)
		}
	}
	for _, id := range q.Seeds {
		where += ` AND i.id != ?`
		wargs = append(wargs, id)
	}
	if q.Family {
		where += ` AND (` + ratingCase("i.content_rating") + ` <= ? OR (COALESCE(i.content_rating, '') = '' AND EXISTS (SELECT 1 FROM item_tags it
			JOIN tags t ON t.id = it.tag_id AND t.kind = 'genre' WHERE it.item_id = i.id AND t.name IN ('Family', 'Animation', 'Kids'))))`
		wargs = append(wargs, familyMaxLevel)
	}
	rating := `COALESCE(i.audience_rating, i.imdb_rating, i.critic_rating, 0)`
	type cand struct {
		id     int64
		rating float64
	}
	var cands []cand
	err = eachRow(ctx, s.db, `SELECT i.id, `+rating+summaryFrom+where+` ORDER BY `+rating+` DESC, i.sort_title COLLATE NOCASE`, wargs, func(r *sql.Rows) error {
		var c cand
		if err := r.Scan(&c.id, &c.rating); err != nil {
			return err
		}
		cands = append(cands, c)
		return nil
	})
	if err != nil {
		return res, err
	}
	ids := make([]int64, 0, len(cands))
	// "like <title>": the seeds' meaning, blended with the prompt's.
	if c := idx.Centroid(q.Seeds, nil); c != nil {
		vec = blendVecs(vec, c, 0.5)
	}
	if vec != nil {
		// Embedded items by meaning; ones not embedded yet follow by rating.
		score := map[int64]float64{}
		for _, c := range cands {
			if v := idx.Get(c.id); v != nil {
				score[c.id] = dotVec(vec, v.Vec)
			}
		}
		sort.SliceStable(cands, func(i, j int) bool {
			si, iok := score[cands[i].id]
			sj, jok := score[cands[j].id]
			if iok != jok {
				return iok
			}
			return iok && si > sj
		})
	}
	for _, c := range cands {
		ids = append(ids, c.id)
		if len(ids) == limit {
			break
		}
	}
	res.Items, err = s.ByIDs(ctx, acc, ids)
	return res, err
}

// blendVecs is the normalized w·a + (1−w)·b (b alone when a is nil).
func blendVecs(a, b []float32, w float64) []float32 {
	if a == nil {
		return b
	}
	out := make([]float32, min(len(a), len(b)))
	for i := range out {
		out[i] = float32(w*float64(a[i]) + (1-w)*float64(b[i]))
	}
	return normalizeVec(out)
}

// VideoPerson finds a director's or actor's name as the library spells it (case-insensitive;
// "" when nobody by that name is credited on a movie or show).
func (s *Store) VideoPerson(ctx context.Context, name string) (string, error) {
	var out string
	err := s.db.QueryRowContext(ctx, `SELECT p.name FROM people p JOIN credits c ON c.person_id = p.id
		JOIN items i ON i.id = c.item_id AND i.type IN ('movie', 'show') WHERE p.name = ? COLLATE NOCASE LIMIT 1`, strings.TrimSpace(name)).Scan(&out)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return out, err
}

// VideoByTitle finds the movie or show acc can see with this title (best rated when there
// are several, e.g. remakes; year narrows it when given). ok=false when there's none.
func (s *Store) VideoByTitle(ctx context.Context, acc Access, title string, year int) (int64, string, bool, error) {
	ac, aargs := acc.clause()
	q := `SELECT i.id, i.title` + summaryFrom + ` WHERE i.type IN ('movie', 'show') AND i.extra_type IS NULL AND (i.title = ? COLLATE NOCASE OR i.original_title = ? COLLATE NOCASE) AND ` + ac
	args := append([]any{strings.TrimSpace(title), strings.TrimSpace(title)}, aargs...)
	if year > 0 {
		q += ` AND i.year = ?`
		args = append(args, year)
	}
	q += ` ORDER BY COALESCE(i.audience_rating, i.imdb_rating, i.critic_rating, 0) DESC LIMIT 1`
	var id int64
	var t string
	err := s.db.QueryRowContext(ctx, q, args...).Scan(&id, &t)
	if err == sql.ErrNoRows {
		return 0, "", false, nil
	}
	return id, t, err == nil, err
}

// Synopses returns a one-line description per item (tagline, or the summary's first
// sentence), for asking the AI to re-rank.
func (s *Store) Synopses(ctx context.Context, ids []int64) (map[int64]string, error) {
	out := map[int64]string{}
	if len(ids) == 0 {
		return out, nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	err := eachRow(ctx, s.db, `SELECT id, COALESCE(tagline, ''), COALESCE(summary, '') FROM items WHERE id IN (?`+strings.Repeat(",?", len(ids)-1)+`)`, args, func(r *sql.Rows) error {
		var id int64
		var tagline, summary string
		if err := r.Scan(&id, &tagline, &summary); err != nil {
			return err
		}
		out[id] = OneLine(tagline, summary)
		return nil
	})
	return out, err
}

// OneLine shortens a description to about one sentence.
func OneLine(tagline, summary string) string {
	s := strings.TrimSpace(summary)
	if i := strings.Index(s, ". "); i > 0 && i < 220 {
		s = s[:i+1]
	}
	if len(s) > 220 {
		s = strings.TrimSpace(s[:220]) + "…"
	}
	if t := strings.TrimSpace(tagline); t != "" && len(s) < 60 {
		s = strings.TrimSpace(t + " " + s)
	}
	return s
}
