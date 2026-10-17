package vodsuggest

import (
	"math"
	"sort"
	"strings"

	"marquee/internal/items"
)

// Scoring. A person's taste (items.Taste: favourites above high ratings above what they
// finished, and what they turned away from), plus the films and series on demand they
// watched, becomes a profile:
//
//   - TMDB's recommendations of every liked title, mapped onto the catalogue by TMDB id
//     (rank 1 counts most), the strongest signal; titles recommended by several liked ones
//     count more, and ones recommended by disliked titles less;
//   - genre, decade and keyword affinities, so titles no liked title leads to can still
//     be suggested when they fit.
//
// Each suggestion keeps the liked title that led to it, for its reason.

// seed is a liked (or disliked) title, from the library or on demand.
type seed struct {
	name     string
	kind     string // movie or series: what its recommendations are
	tmdb     int
	weight   float64
	favorite bool
	rating   float64
	finished bool
	genres   []string
	keywords []string
	year     int
	recs     []int
}

// why is the kind of reason a suggestion has.
type why int

const (
	whyNone why = iota
	whyGenre
	whySimilar  // a title they started
	whyWatched  // a title they finished
	whyLoved    // rated 8 or more
	whyFavorite // one of their favourites
	whyPopular  // no taste yet: well rated and popular
)

type recHit struct {
	best, sum float64
	seed      string
	why       why
	weight    float64
}

type profile struct {
	rec      map[key]*recHit
	neg      map[key]float64
	genre    map[string]float64
	decade   map[int]float64
	keyword  map[string]float64
	topGenre string
	total    float64
	liked    map[key]bool // the liked titles themselves
	seeds    []seed       // the liked titles, strongest first (for Because you watched rows)
	top      float64      // the best score in the catalogue, for normalising
}

func (p *profile) empty() bool { return p == nil || p.total == 0 }

// rankWeight is a recommendation's weight by its rank: 1 for TMDB's first, falling by 2% a
// rank to 0.2.
func rankWeight(rank int) float64 { return max(0.2, 1-0.02*float64(rank)) }

func seedWhy(sd seed) why {
	switch {
	case sd.favorite:
		return whyFavorite
	case sd.rating >= 8:
		return whyLoved
	case sd.finished:
		return whyWatched
	default:
		return whySimilar
	}
}

func kindOf(itemType string) string {
	if itemType == "show" {
		return Series
	}
	return Movie
}

func librarySeeds(list []items.TasteTitle) []seed {
	out := make([]seed, 0, len(list))
	for _, t := range list {
		out = append(out, seed{name: t.Title, kind: kindOf(t.Type), tmdb: t.TMDBID, weight: t.Weight, favorite: t.Favorite,
			rating: t.Rating, finished: t.Finished, genres: lower(t.Genres), keywords: lower(t.Keywords), year: t.Year, recs: t.Recs})
	}
	return out
}

func lower(list []string) []string {
	out := make([]string, len(list))
	for i, s := range list {
		out[i] = strings.ToLower(strings.TrimSpace(s))
	}
	return out
}

// buildProfile turns liked and disliked titles into a profile.
func buildProfile(liked, disliked []seed) *profile {
	p := &profile{rec: map[key]*recHit{}, neg: map[key]float64{}, genre: map[string]float64{}, decade: map[int]float64{},
		keyword: map[string]float64{}, liked: map[key]bool{}}
	for _, sd := range liked {
		if sd.weight <= 0 {
			continue
		}
		p.total += sd.weight
		p.seeds = append(p.seeds, sd)
		if sd.tmdb > 0 {
			p.liked[key{sd.kind, sd.tmdb}] = true
		}
		w := seedWhy(sd)
		for rank, tm := range sd.recs {
			k := key{sd.kind, tm}
			v := sd.weight * rankWeight(rank)
			h := p.rec[k]
			if h == nil {
				h = &recHit{}
				p.rec[k] = h
			}
			h.sum += v
			if v > h.best || (v == h.best && w > h.why) {
				h.best, h.seed, h.why, h.weight = v, sd.name, w, sd.weight
			}
		}
		for _, g := range sd.genres {
			p.genre[g] += sd.weight
		}
		if sd.year > 0 {
			p.decade[sd.year/10*10] += sd.weight
		}
		for _, kw := range sd.keywords {
			p.keyword[kw] += sd.weight
		}
	}
	for _, sd := range disliked {
		for rank, tm := range sd.recs {
			p.neg[key{sd.kind, tm}] += rankWeight(rank)
		}
		for _, g := range sd.genres {
			p.genre[g] -= 0.3
		}
	}
	if p.total == 0 {
		return p
	}
	best := 0.0
	for g, v := range p.genre {
		v /= p.total
		p.genre[g] = v
		if v > best {
			best, p.topGenre = v, g
		}
	}
	maxDecade := 0.0
	for _, v := range p.decade {
		maxDecade = max(maxDecade, v)
	}
	for d, v := range p.decade {
		p.decade[d] = v / maxDecade
	}
	for k, v := range p.keyword {
		p.keyword[k] = v / p.total
	}
	return p
}

// Score weights: recommendations count most; then genre, keyword and decade affinity and
// TMDB's vote.
const (
	recWeight     = 1.0
	recOthers     = 0.25 // each further liked title recommending it adds this share
	negWeight     = 0.4
	genreWeight   = 0.35
	keywordWeight = 0.2
	decadeWeight  = 0.1
	voteWeight    = 0.15
)

// score is how well t fits the profile, with the reason.
func (p *profile) score(t *title) (float64, why, string) {
	var rec float64
	var w why
	var seedName string
	if h := p.rec[key{t.kind, t.tmdb}]; h != nil {
		rec = h.best + recOthers*(h.sum-h.best)
		w, seedName = h.why, h.seed
	}
	rec -= negWeight * p.neg[key{t.kind, t.tmdb}]
	g := p.genreAffinity(t.genres)
	var kw float64
	for _, k := range t.keywords {
		kw += p.keyword[k]
	}
	kw = min(kw, 1)
	if rec <= 0 && g <= 0 && kw <= 0 {
		return 0, whyNone, "" // nothing in their taste leads to it
	}
	var dec float64
	if t.year > 0 {
		dec = p.decade[t.year/10*10]
	}
	s := recWeight*rec + genreWeight*g + keywordWeight*kw + decadeWeight*dec + voteWeight*t.rating/10
	if w == whyNone && g > 0 {
		w = whyGenre
	}
	return s, w, seedName
}

// genreAffinity is how much the profile likes a set of genres: mostly the best-liked one,
// partly their average.
func (p *profile) genreAffinity(genres []string) float64 {
	if len(genres) == 0 {
		return 0
	}
	best, sum := 0.0, 0.0
	for _, g := range genres {
		v := p.genre[g]
		best = max(best, v)
		sum += max(v, 0)
	}
	return 0.6*best + 0.4*sum/float64(len(genres))
}

// favouriteGenre is the title's genre the profile likes most, in its own words.
func (p *profile) favouriteGenre(t *title) string {
	best, out := 0.0, ""
	for _, g := range t.genres {
		if v := p.genre[g]; v > best {
			best, out = v, g
		}
	}
	return out
}

func reasonText(w why, seedName, genre string) string {
	switch w {
	case whyFavorite:
		return "Because you love " + seedName
	case whyLoved:
		return "Because you loved " + seedName
	case whyWatched:
		return "Because you watched " + seedName
	case whySimilar:
		return "Similar to " + seedName
	case whyPopular:
		return "Popular on VOD"
	case whyGenre:
		if genre != "" {
			return "Because you like " + titleCase(genre)
		}
	}
	return ""
}

func titleCase(s string) string {
	words := strings.Fields(s)
	for i, w := range words {
		if w == "&" || w == "and" || w == "of" {
			continue
		}
		words[i] = strings.ToUpper(w[:1]) + w[1:]
	}
	return strings.Join(words, " ")
}

// scored is a candidate with its score and reason.
type scored struct {
	t      *title
	score  float64
	why    why
	seed   string
	reason string
}

// diversify picks up to n of ranked (best first) so one franchise, one liked title or one
// genre doesn't fill the list: each pick lowers the rest that share its franchise (a lot),
// the liked title that led to it, or its main genre (a little).
func diversify(ranked []scored, n int) []scored { return diversifyBy(ranked, n, 0.97) }

// diversifyBy is diversify with how much each pick of a main genre lowers the rest of it.
func diversifyBy(ranked []scored, n int, genreDecay float64) []scored {
	if len(ranked) > 4*n+50 {
		ranked = ranked[:4*n+50]
	}
	pool := append([]scored(nil), ranked...)
	out := make([]scored, 0, min(n, len(pool)))
	franchise, bySeed, byGenre := map[string]int{}, map[string]int{}, map[string]int{}
	used := make([]bool, len(pool))
	for len(out) < n {
		bi, bv := -1, math.Inf(-1)
		for i, c := range pool {
			if used[i] {
				continue
			}
			v := c.score
			if c.t.franchise != "" {
				v *= math.Pow(0.4, float64(franchise[c.t.kind+"|"+c.t.franchise]))
			}
			if c.seed != "" {
				v *= math.Pow(0.85, float64(bySeed[c.seed]))
			}
			if len(c.t.genres) > 0 {
				v *= math.Pow(genreDecay, float64(byGenre[c.t.genres[0]]))
			}
			if v > bv {
				bi, bv = i, v
			}
		}
		if bi < 0 {
			break
		}
		used[bi] = true
		c := pool[bi]
		out = append(out, c)
		if c.t.franchise != "" {
			franchise[c.t.kind+"|"+c.t.franchise]++
		}
		if c.seed != "" {
			bySeed[c.seed]++
		}
		if len(c.t.genres) > 0 {
			byGenre[c.t.genres[0]]++
		}
	}
	return out
}

func sortScored(list []scored) {
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].score != list[j].score {
			return list[i].score > list[j].score
		}
		return list[i].t.id < list[j].t.id
	})
}

// For someone with no taste to go on yet: well-rated, popular titles, TMDB's vote average
// pulled towards popularMean by how few votes it has (titles whose count isn't known yet
// count as having a few), plus a little for the number of votes.
const (
	popularVotes = 300.0 // votes at which a title's own average counts for half
	popularMean  = 6.0
	popularMin   = 6.5 // TMDB average below this isn't "well rated"
	unknownVotes = 30.0
)

func popularScore(t *title) float64 {
	if t.rating < popularMin {
		return 0
	}
	v := float64(t.votes)
	if v <= 0 {
		v = unknownVotes
	}
	return (v/(v+popularVotes))*t.rating + (popularVotes/(v+popularVotes))*popularMean + 0.15*math.Log10(1+v)
}
