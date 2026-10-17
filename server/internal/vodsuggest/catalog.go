package vodsuggest

import (
	"context"
	"database/sql"
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The catalogue: every film and series on demand matched to TMDB, as suggestions need it,
// loaded once per change of the catalogue and shared by everyone.

const (
	Movie  = "movie"
	Series = "series"
)

// title is a film or series on demand.
type title struct {
	kind          string
	id            int64
	tmdb          int
	name          string
	year          int
	genres        []string // lower case
	keywords      []string // lower case; empty until known
	recs          []int    // TMDB's recommendations, best first; nil until known
	rating        float64  // TMDB's average vote out of 10, 0 = none
	votes         int      // how many votes TMDB has, 0 = unknown
	contentRating string
	runtimeMS     int64
	franchise     string
}

type key struct {
	kind string
	tmdb int
}

type catalog struct {
	version string
	titles  []*title
	byID    map[string]map[int64]*title // kind → id → title
	byTMDB  map[key][]*title            // every copy (several sources may list a title)
}

func (c *catalog) get(kind string, id int64) *title {
	if c == nil {
		return nil
	}
	return c.byID[kind][id]
}

type catalogCache struct {
	mu        sync.Mutex
	cat       *catalog
	version   string
	checkedAt time.Time
}

// catalogCheckEvery is how often the catalogue's version is looked up.
const catalogCheckEvery = time.Minute

// catalogVersion changes whenever a film or series is matched, added or removed.
func catalogVersion(ctx context.Context, db *sql.DB) (string, error) {
	var out strings.Builder
	for _, table := range []string{"vod_movies", "vod_series"} {
		var n, maxID int64
		var sum float64
		var recs int64
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(MAX(id),0), TOTAL(tmdb_id), COUNT(tmdb_recs) FROM `+table+
			` WHERE match_state='matched'`).Scan(&n, &maxID, &sum, &recs); err != nil {
			return "", err
		}
		out.WriteString(table)
		out.WriteString(":")
		out.WriteString(strings.Join([]string{itoa(n), itoa(maxID), itoa(int64(sum)), itoa(recs)}, ","))
		out.WriteString(";")
	}
	return out.String(), nil
}

// catalog returns the current catalogue, reloading it when it changed (checked at most
// once a minute unless force).
func (s *Service) catalog(ctx context.Context, force bool) (*catalog, error) {
	c := &s.cat
	c.mu.Lock()
	defer c.mu.Unlock()
	now := s.clock()
	if c.cat != nil && !force && now.Sub(c.checkedAt) < catalogCheckEvery {
		return c.cat, nil
	}
	v, err := catalogVersion(ctx, s.DB)
	if err != nil {
		return nil, err
	}
	c.checkedAt = now
	if c.cat != nil && v == c.version {
		return c.cat, nil
	}
	cat, err := loadCatalog(ctx, s.DB)
	if err != nil {
		return nil, err
	}
	cat.version = v
	c.cat, c.version = cat, v
	return cat, nil
}

func loadCatalog(ctx context.Context, db *sql.DB) (*catalog, error) {
	cat := &catalog{byID: map[string]map[int64]*title{Movie: {}, Series: {}}, byTMDB: map[key][]*title{}}
	for _, kind := range []string{Movie, Series} {
		table, runtime := "vod_movies", "m.runtime_ms"
		if kind == Series {
			table, runtime = "vod_series", "0"
		}
		rows, err := db.QueryContext(ctx, `SELECT m.id, m.tmdb_id, m.title, COALESCE(m.year,0), m.genres, COALESCE(m.keywords,''),
			COALESCE(m.tmdb_recs,''), COALESCE(m.rating,0), COALESCE(m.votes,0), m.content_rating, `+runtime+`
			FROM `+table+` m WHERE m.match_state='matched' AND m.tmdb_id > 0 ORDER BY m.id`)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			t := &title{kind: kind}
			var genres, keywords, recs string
			if err := rows.Scan(&t.id, &t.tmdb, &t.name, &t.year, &genres, &keywords, &recs, &t.rating, &t.votes, &t.contentRating, &t.runtimeMS); err != nil {
				rows.Close()
				return nil, err
			}
			json.Unmarshal([]byte(genres), &t.genres)
			for i, g := range t.genres {
				t.genres[i] = strings.ToLower(strings.TrimSpace(g))
			}
			if keywords != "" {
				json.Unmarshal([]byte(keywords), &t.keywords)
				for i, k := range t.keywords {
					t.keywords[i] = strings.ToLower(k)
				}
			}
			if recs != "" {
				t.recs = []int{}
				json.Unmarshal([]byte(recs), &t.recs)
			}
			t.franchise = franchiseKey(t.name)
			cat.titles = append(cat.titles, t)
			cat.byID[kind][t.id] = t
			k := key{kind, t.tmdb}
			cat.byTMDB[k] = append(cat.byTMDB[k], t)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
	}
	return cat, nil
}

var (
	yearSuffix   = regexp.MustCompile(`\s*[\(\[]\d{4}[\)\]]\s*$`)
	sequelSuffix = regexp.MustCompile(`(?i)(\s+(part|chapter|vol\.?|volume))?\s+([0-9]+|i{1,3}|iv|v|vi{1,3}|ix|x)$`)
	nonWord      = regexp.MustCompile(`[^\pL\pN]+`)
)

// franchiseKey is a title without its year, subtitle and sequel number ("Rocky II" and
// "Rocky III: ..." share "rocky"), for spreading suggestions over more than one franchise.
func franchiseKey(name string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	n = yearSuffix.ReplaceAllString(n, "")
	for _, sep := range []string{":", " - ", " – "} {
		if i := strings.Index(n, sep); i > 0 {
			n = n[:i]
		}
	}
	n = strings.TrimSpace(n)
	n = sequelSuffix.ReplaceAllString(n, "")
	n = strings.TrimPrefix(n, "the ")
	return strings.TrimSpace(nonWord.ReplaceAllString(n, " "))
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
