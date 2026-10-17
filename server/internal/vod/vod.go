// Package vod is movies on demand under Live TV (LIVE-6): films listed in M3U playlists
// such as Pluto TV's, matched to TMDB and streamed through the server. Nothing is
// downloaded; each film's (expiring) link is refreshed with its list.
package vod

import (
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"marquee/internal/items"
	"marquee/internal/livetv"
	"marquee/internal/metadata/tmdb"
	"marquee/internal/settings"
)

var ErrNotFound = errors.New("vod: not found")

// Matcher finds a film on TMDB (metadata.Service).
type Matcher interface {
	MatchMovie(ctx context.Context, title string, year int) (*tmdb.Movie, error)
	// MovieByID returns a film the source already named by its TMDB id.
	MovieByID(ctx context.Context, id int) (*tmdb.Movie, error)
}

type Service struct {
	DB       *sql.DB
	Settings *settings.Store
	Matcher  Matcher
	Streams  *Streams
	HTTP     *http.Client
	// ImageDir caches posters and backdrops.
	ImageDir string
	// OnSourceFailed, when set, hears a source that couldn't be loaded (admin alerts).
	OnSourceFailed func(src settings.VODSource, err error)
	// Now is the clock and Tick how often Run checks whether a sync is due (tests;
	// defaults time.Now and a minute).
	Now  func() time.Time
	Tick time.Duration

	refreshing  sync.Mutex
	matching    atomic.Bool
	lastAttempt atomic.Int64 // unix nanoseconds of the last refresh started
}

func now() string { return time.Now().UTC().Format(time.RFC3339) }

func (s *Service) clock() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) client() *http.Client {
	if s.HTTP != nil {
		return s.HTTP
	}
	return &http.Client{Timeout: 60 * time.Second}
}

// Sources are the enabled movie sources; none while movies on demand are off.
func (s *Service) Sources() []settings.VODSource {
	i := s.Settings.Get().Integrations
	if !i.VODEnabled {
		return nil
	}
	var out []settings.VODSource
	for _, src := range i.VODSources {
		if src.Enabled {
			out = append(out, src)
		}
	}
	return out
}

// Enabled reports whether the admin turned movies on demand on with a source.
func (s *Service) Enabled() bool { return len(s.Sources()) > 0 }

// Matching reports whether details and TMDB matches are being fetched now.
func (s *Service) Matching() bool { return s.matching.Load() }

func (s *Service) get(ctx context.Context, u, ua string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	if ua != "" {
		req.Header.Set("User-Agent", ua)
	}
	resp, err := s.client().Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		resp.Body.Close()
		return nil, fmt.Errorf("%s answered %d", u, resp.StatusCode)
	}
	return resp.Body, nil
}

// Run keeps the lists fresh until ctx ends: with automatic sync on (the default), a refresh
// is due vodRefreshHours after the last successful one (checked every minute, so a changed
// setting applies at once); off, lists reload only when asked, and once at start for a
// source that has never loaded. wake asks for a refresh now.
func (s *Service) Run(ctx context.Context, wake <-chan struct{}) {
	tick := s.Tick
	if tick <= 0 {
		tick = time.Minute
	}
	t := time.NewTicker(tick)
	defer t.Stop()
	start := true
	for {
		if s.due(ctx, start) {
			s.Refresh(ctx, false)
		}
		start = false
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-wake:
			if s.Enabled() {
				s.Refresh(ctx, false)
			}
		}
	}
}

// due reports whether an automatic refresh is due: a source has never loaded (at start, or
// with automatic sync on), or with automatic sync on the oldest successful refresh is
// vodRefreshHours old. A failing source is tried again after the same interval.
func (s *Service) due(ctx context.Context, start bool) bool {
	sources := s.Sources()
	if len(sources) == 0 {
		return false
	}
	i := s.Settings.Get().Integrations
	if !i.VODAutoRefresh && !start {
		return false
	}
	now := s.clock()
	every := time.Duration(max(i.VODRefreshHours, 1)) * time.Hour
	if last := s.lastAttempt.Load(); last != 0 && now.Sub(time.Unix(0, last)) < every {
		return false
	}
	var oldest time.Time
	for _, src := range sources {
		var at sql.NullString
		s.DB.QueryRowContext(ctx, `SELECT refreshed_at FROM vod_sources WHERE id = ?`, src.ID).Scan(&at)
		t, err := time.Parse(time.RFC3339, at.String)
		if !at.Valid || err != nil {
			return true // never loaded
		}
		if oldest.IsZero() || t.Before(oldest) {
			oldest = t
		}
	}
	return i.VODAutoRefresh && now.Sub(oldest) >= every
}

// Refresh reloads every source's list, then fetches details and TMDB matches in the
// background. retryFailed also retries films TMDB didn't match before. While movies on
// demand are off nothing changes, so turning them back on keeps everyone's progress.
func (s *Service) Refresh(ctx context.Context, retryFailed bool) {
	if !s.Settings.Get().Integrations.VODEnabled {
		return
	}
	s.refreshing.Lock()
	s.lastAttempt.Store(s.clock().UnixNano())
	sources := s.Sources()
	keep := []any{}
	for _, src := range sources {
		keep = append(keep, src.ID)
		n, err := s.refreshSource(ctx, src)
		var msg any
		if err != nil {
			msg = err.Error()
			slog.Warn("movie source failed", "source", src.Name, "err", err)
		} else {
			slog.Info("movie source loaded", "source", src.Name, "movies", n)
		}
		// Series (Xtream Codes): one more request for their categories and one for the list.
		var series any
		if src.Xtream() {
			ns, serr := s.refreshXtreamSeries(ctx, src)
			if serr != nil {
				slog.Warn("series source failed", "source", src.Name, "err", serr)
				if err == nil {
					msg = "series: " + serr.Error()
					err = serr
				}
			} else {
				series = ns
				slog.Info("series source loaded", "source", src.Name, "series", ns)
			}
		} else if _, derr := s.DB.ExecContext(ctx, "DELETE FROM vod_series WHERE source_id=?", src.ID); derr == nil {
			series = 0
		}
		if err != nil && s.OnSourceFailed != nil {
			s.OnSourceFailed(src, err)
		}
		filmsOK := msg == nil || strings.HasPrefix(msg.(string), "series: ")
		if _, err := s.DB.ExecContext(ctx, `INSERT INTO vod_sources(id, name, movies, series, refreshed_at, error) VALUES(?,?,?,COALESCE(?,0),CASE WHEN ? THEN ? END,?)
			ON CONFLICT(id) DO UPDATE SET name=excluded.name, error=excluded.error,
			movies=CASE WHEN ? THEN excluded.movies ELSE vod_sources.movies END,
			series=CASE WHEN ? IS NOT NULL THEN excluded.series ELSE vod_sources.series END,
			refreshed_at=CASE WHEN ? THEN excluded.refreshed_at ELSE vod_sources.refreshed_at END`,
			src.ID, src.Name, n, series, filmsOK, s.clock().UTC().Format(time.RFC3339), msg, filmsOK, series, filmsOK); err != nil {
			slog.Warn("movie source status", "err", err)
		}
	}
	// Sources removed or turned off take their films with them.
	in := "''"
	if len(keep) > 0 {
		in = "?" + strings.Repeat(",?", len(keep)-1)
	}
	s.DB.ExecContext(ctx, "DELETE FROM vod_movies WHERE source_id NOT IN ("+in+")", keep...)
	s.DB.ExecContext(ctx, "DELETE FROM vod_series WHERE source_id NOT IN ("+in+")", keep...)
	s.DB.ExecContext(ctx, "DELETE FROM vod_sources WHERE id NOT IN ("+in+")", keep...)
	s.refreshing.Unlock()
	if len(sources) > 0 {
		go s.enrich(context.WithoutCancel(ctx), retryFailed)
	}
}

var plutoEpisode = regexp.MustCompile(`/episode/([0-9a-f]{24})/`)

// sourceKey identifies a film across refreshes. Links carry tokens that change, so it is
// Pluto's episode id, else the link without its query string.
func sourceKey(link string) string {
	if m := plutoEpisode.FindStringSubmatch(link); m != nil {
		return "pluto:" + m[1]
	}
	if u, err := url.Parse(link); err == nil {
		u.RawQuery, u.Fragment = "", ""
		link = u.String()
	}
	h := sha1.Sum([]byte(link))
	return "url:" + hex.EncodeToString(h[:8])
}

func (s *Service) refreshSource(ctx context.Context, src settings.VODSource) (int, error) {
	if src.Xtream() {
		return s.refreshXtream(ctx, src)
	}
	body, err := s.get(ctx, src.URL, src.UserAgent)
	if err != nil {
		return 0, err
	}
	pl, err := livetv.ParseM3U(body)
	body.Close()
	if err != nil {
		return 0, err
	}
	if len(pl.Entries) == 0 {
		return 0, errors.New("the playlist lists no films")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	existing := map[string]int64{}
	rows, err := tx.QueryContext(ctx, "SELECT id, source_key FROM vod_movies WHERE source_id=?", src.ID)
	if err != nil {
		return 0, err
	}
	for rows.Next() {
		var id int64
		var k string
		rows.Scan(&id, &k)
		existing[k] = id
	}
	rows.Close()
	seen := map[string]bool{}
	added := now()
	for _, e := range pl.Entries {
		k := sourceKey(e.URL)
		if seen[k] || e.Name == "" {
			continue
		}
		seen[k] = true
		if _, err := tx.ExecContext(ctx, `INSERT INTO vod_movies(source_id, source_key, title, category, stream_url, logo_url, added_at)
			VALUES(?,?,?,?,?,?,?) ON CONFLICT(source_id, source_key) DO UPDATE SET title=excluded.title,
			category=excluded.category, stream_url=excluded.stream_url, logo_url=excluded.logo_url`,
			src.ID, k, strings.TrimSpace(e.Name), e.Group, e.URL, e.Logo, added); err != nil {
			return 0, err
		}
	}
	for k, id := range existing {
		if !seen[k] {
			if _, err := tx.ExecContext(ctx, "DELETE FROM vod_movies WHERE id=?", id); err != nil {
				return 0, err
			}
		}
	}
	return len(seen), tx.Commit()
}

// enrich fills in source details (Pluto's catalogue), then TMDB matches. One run at a time.
func (s *Service) enrich(ctx context.Context, retryFailed bool) {
	if !s.matching.CompareAndSwap(false, true) {
		return
	}
	defer s.matching.Store(false)
	if err := s.fetchPlutoDetails(ctx); err != nil {
		slog.Warn("pluto details", "err", err)
	}
	s.match(ctx, retryFailed)
	s.matchSeries(ctx, retryFailed)
}

var titleYear = regexp.MustCompile(`^(.*?)\s*\((\d{4})\)\s*$`)

func (s *Service) match(ctx context.Context, retryFailed bool) {
	if s.Matcher == nil {
		return
	}
	q := "SELECT id, title, COALESCE(year, 0), COALESCE(tmdb_id, 0) FROM vod_movies WHERE match_state='pending'"
	if retryFailed {
		q = "SELECT id, title, COALESCE(year, 0), COALESCE(tmdb_id, 0) FROM vod_movies WHERE match_state IN ('pending','failed')"
	}
	rows, err := s.DB.QueryContext(ctx, q+" ORDER BY id")
	if err != nil {
		return
	}
	type todo struct {
		id    int64
		title string
		year  int
		tmdb  int
	}
	var list []todo
	for rows.Next() {
		var t todo
		rows.Scan(&t.id, &t.title, &t.year, &t.tmdb)
		list = append(list, t)
	}
	rows.Close()
	matched := 0
	for _, t := range list {
		if ctx.Err() != nil || !s.Enabled() {
			return
		}
		title := t.title
		if m := titleYear.FindStringSubmatch(title); m != nil {
			title = m[1]
			if t.year == 0 {
				t.year, _ = strconv.Atoi(m[2])
			}
		}
		var m *tmdb.Movie
		var err error
		if t.tmdb > 0 {
			// The source named the film (Xtream Codes): no guessing by title.
			m, err = s.Matcher.MovieByID(ctx, t.tmdb)
			if err != nil && !errors.Is(err, tmdb.ErrNoKey) && !errors.Is(err, tmdb.ErrInvalidKey) {
				slog.Debug("movie by TMDB id", "title", t.title, "tmdb", t.tmdb, "err", err)
				m, err = nil, nil
			}
		}
		if m == nil && err == nil {
			m, err = s.Matcher.MatchMovie(ctx, title, t.year)
		}
		if errors.Is(err, tmdb.ErrNoKey) || errors.Is(err, tmdb.ErrInvalidKey) {
			return // films stay pending until a key is set
		}
		if err != nil {
			slog.Warn("movie match", "title", t.title, "err", err)
			continue
		}
		if m == nil {
			s.DB.ExecContext(ctx, "UPDATE vod_movies SET match_state='failed' WHERE id=?", t.id)
			continue
		}
		if err := s.applyTMDB(ctx, t.id, m); err != nil {
			slog.Warn("movie match", "title", t.title, "err", err)
			continue
		}
		matched++
	}
	if len(list) > 0 {
		slog.Info("movies matched to TMDB", "matched", matched, "tried", len(list))
	}
}

const tmdbImages = "https://image.tmdb.org/t/p/"

// applyTMDB stores a match. TMDB's details replace the source's; the source's title stays,
// since it is what the source shows.
func (s *Service) applyTMDB(ctx context.Context, id int64, m *tmdb.Movie) error {
	var year any
	if len(m.ReleaseDate) >= 4 {
		if y, err := strconv.Atoi(m.ReleaseDate[:4]); err == nil {
			year = y
		}
	}
	genres := make([]string, 0, len(m.Genres))
	for _, g := range m.Genres {
		genres = append(genres, g.Name)
	}
	var directors []string
	for _, c := range m.Credits.Crew {
		if c.Job == "Director" && len(directors) < 2 {
			directors = append(directors, c.Name)
		}
	}
	cast := make([]CastMember, 0, 12)
	for _, c := range m.Credits.Cast {
		if len(cast) == 12 {
			break
		}
		cast = append(cast, CastMember{Name: c.Name, Character: c.Character})
	}
	gj, _ := json.Marshal(genres)
	cj, _ := json.Marshal(cast)
	poster, backdrop := "", ""
	if m.PosterPath != "" {
		poster = tmdbImages + "w500" + m.PosterPath
	}
	if m.BackdropPath != "" {
		backdrop = tmdbImages + "w1280" + m.BackdropPath
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE vod_movies SET match_state='matched', tmdb_id=?, imdb_id=?,
		year=COALESCE(?, year), overview=CASE WHEN ?<>'' THEN ? ELSE overview END, tagline=?, genres=?,
		content_rating=CASE WHEN ?<>'' THEN ? ELSE content_rating END, rating=?,
		runtime_ms=CASE WHEN ?>0 THEN ? ELSE runtime_ms END,
		director=CASE WHEN ?<>'' THEN ? ELSE director END, cast_json=?,
		poster_url=CASE WHEN ?<>'' THEN ? ELSE poster_url END,
		backdrop_url=CASE WHEN ?<>'' THEN ? ELSE backdrop_url END WHERE id=?`,
		m.ID, m.IMDBID, year, m.Overview, m.Overview, m.Tagline, string(gj),
		m.Certification("US"), m.Certification("US"), m.VoteAverage,
		m.Runtime, int64(m.Runtime)*60000, strings.Join(directors, ", "), strings.Join(directors, ", "), string(cj),
		poster, poster, backdrop, backdrop, id)
	return err
}

// ---- browsing

type CastMember struct {
	Name      string `json:"name"`
	Character string `json:"character,omitempty"`
}

type Movie struct {
	ID            int64
	Title         string
	Category      string
	Year          int
	Overview      string
	Tagline       string
	Genres        []string
	ContentRating string
	Rating        float64
	RuntimeMS     int64
	Director      string
	Cast          []CastMember
	HasPoster     bool
	HasBackdrop   bool
	TMDBID        int
	IMDBID        string
	Matched       bool
	PositionMS    int64
	Watched       bool
	StreamURL     string
	SourceID      string
	AddedAt       time.Time
}

// Filter narrows a listing. MaxRating hides films rated above it, and unrated ones, as
// the library does for restricted profiles (USER-2).
type Filter struct {
	UserID    int64
	MaxRating string
	Category  string
	Query     string
	Sort      string // title (default), added, year, rating
	Limit     int
	Offset    int
	// Genre is one of the item's genres (TMDB's once matched, else the source's).
	Genre string
	// YearFrom and YearTo bound the year (0 = open); items without a year are left out.
	YearFrom, YearTo int
	// MinRating is the lowest average vote out of 10 (0 = any); unrated items are left out.
	MinRating float64
	// Watched is the user's state: unwatched, inProgress or watched ("" = any). For series:
	// has unwatched episodes, started but not finished, or every episode watched.
	Watched string
	// SourceID limits to one source.
	SourceID string
	// InLibrary, when set, keeps only the titles the library has (true) or doesn't (false);
	// LibraryIDs are the ids of those it has (worked out by vodsave's index, for the
	// libraries the user can see).
	InLibrary  *bool
	LibraryIDs []int64
}

// where builds the filter's conditions on m (vod_movies or vod_series).
func (f Filter) where(series bool) (string, []any) {
	where, args := ratingClause(f.MaxRating)
	if f.Category != "" {
		where += " AND m.category=?"
		args = append(args, f.Category)
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		where += " AND m.title LIKE ? ESCAPE '\\'"
		args = append(args, likeArg(q))
	}
	if f.Genre != "" {
		where += " AND EXISTS (SELECT 1 FROM json_each(m.genres) g WHERE g.value=? COLLATE NOCASE)"
		args = append(args, f.Genre)
	}
	if f.YearFrom > 0 {
		where += " AND m.year>=?"
		args = append(args, f.YearFrom)
	}
	if f.YearTo > 0 {
		where += " AND m.year<=?"
		args = append(args, f.YearTo)
	}
	if f.MinRating > 0 {
		where += " AND m.rating>=?"
		args = append(args, f.MinRating)
	}
	if f.SourceID != "" {
		where += " AND m.source_id=?"
		args = append(args, f.SourceID)
	}
	if f.InLibrary != nil {
		ids := f.LibraryIDs
		if ids == nil {
			ids = []int64{}
		}
		b, _ := json.Marshal(ids)
		not := ""
		if !*f.InLibrary {
			not = "NOT "
		}
		where += " AND m.id " + not + "IN (SELECT value FROM json_each(?))"
		args = append(args, string(b))
	}
	if !series {
		const p = "SELECT 1 FROM vod_progress w WHERE w.movie_id=m.id AND w.user_id=?"
		switch f.Watched {
		case "unwatched":
			where += " AND NOT EXISTS (" + p + " AND (w.watched=1 OR w.position_ms>0))"
			args = append(args, f.UserID)
		case "inProgress":
			where += " AND EXISTS (" + p + " AND w.watched=0 AND w.position_ms>0)"
			args = append(args, f.UserID)
		case "watched":
			where += " AND EXISTS (" + p + " AND w.watched=1)"
			args = append(args, f.UserID)
		}
		return where, args
	}
	// Every known episode watched (none known counts as not).
	const all = `((SELECT COUNT(*) FROM vod_episodes e WHERE e.series_id=m.id) > 0 AND (SELECT COUNT(*) FROM vod_episodes e WHERE e.series_id=m.id) <=
		(SELECT COUNT(*) FROM vod_episode_progress w WHERE w.series_id=m.id AND w.user_id=? AND w.watched=1))`
	switch f.Watched {
	case "unwatched":
		where += " AND NOT " + all
		args = append(args, f.UserID)
	case "inProgress":
		where += " AND EXISTS (SELECT 1 FROM vod_episode_progress w WHERE w.series_id=m.id AND w.user_id=?) AND NOT " + all
		args = append(args, f.UserID, f.UserID)
	case "watched":
		where += " AND " + all
		args = append(args, f.UserID)
	}
	return where, args
}

// Genres lists the genres of films (or series) the user may see, with how many have each.
func (s *Service) Genres(ctx context.Context, maxRating string, series bool) ([]Category, error) {
	table := "vod_movies"
	if series {
		table = "vod_series"
	}
	where, args := ratingClause(maxRating)
	rows, err := s.DB.QueryContext(ctx, "SELECT g.value, COUNT(*) FROM "+table+" m, json_each(m.genres) g WHERE g.type='text' AND g.value<>'' AND "+
		where+" GROUP BY g.value COLLATE NOCASE ORDER BY g.value COLLATE NOCASE", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Category
	for rows.Next() {
		var c Category
		rows.Scan(&c.Name, &c.Count)
		out = append(out, c)
	}
	return out, rows.Err()
}

var knownRatings = []string{"TV-Y", "G", "TV-G", "TV-Y7", "TV-Y7-FV", "PG", "TV-PG", "PG-13", "TV-14", "R", "TV-MA", "NC-17"}

// ratingClause limits m.content_rating to maxRating and below.
func ratingClause(maxRating string) (string, []any) {
	lvl := items.RatingLevel(maxRating)
	if lvl == 0 {
		return "1", nil
	}
	var args []any
	for _, r := range knownRatings {
		if items.RatingLevel(r) <= lvl {
			args = append(args, r)
		}
	}
	return "UPPER(m.content_rating) IN (?" + strings.Repeat(",?", len(args)-1) + ")", args
}

// Allowed reports whether a film's rating is within maxRating.
func Allowed(contentRating, maxRating string) bool {
	lvl := items.RatingLevel(maxRating)
	if lvl == 0 {
		return true
	}
	r := items.RatingLevel(contentRating)
	return r > 0 && r <= lvl
}

const movieCols = `m.id, m.title, m.category, COALESCE(m.year,0), m.overview, m.tagline, m.genres, m.content_rating,
	COALESCE(m.rating,0), m.runtime_ms, m.director, m.cast_json, (m.poster_url<>'' OR m.logo_url<>''), m.backdrop_url<>'',
	COALESCE(m.tmdb_id,0), m.imdb_id, m.match_state='matched', COALESCE(p.position_ms,0), COALESCE(p.watched,0),
	m.stream_url, m.source_id, m.added_at`

func scanMovie(sc interface{ Scan(...any) error }) (Movie, error) {
	var m Movie
	var genres, cast, added string
	err := sc.Scan(&m.ID, &m.Title, &m.Category, &m.Year, &m.Overview, &m.Tagline, &genres, &m.ContentRating,
		&m.Rating, &m.RuntimeMS, &m.Director, &cast, &m.HasPoster, &m.HasBackdrop,
		&m.TMDBID, &m.IMDBID, &m.Matched, &m.PositionMS, &m.Watched, &m.StreamURL, &m.SourceID, &added)
	if err != nil {
		return m, err
	}
	json.Unmarshal([]byte(genres), &m.Genres)
	json.Unmarshal([]byte(cast), &m.Cast)
	m.AddedAt, _ = time.Parse(time.RFC3339, added)
	return m, nil
}

// Movies lists films with the user's progress, and how many match in total.
func (s *Service) Movies(ctx context.Context, f Filter) ([]Movie, int, error) {
	where, args := f.where(false)
	var total int
	if err := s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM vod_movies m WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	order := "m.title COLLATE NOCASE"
	switch f.Sort {
	case "added":
		order = "m.added_at DESC, m.id DESC"
	case "year":
		order = "m.year IS NULL, m.year DESC, m.title COLLATE NOCASE"
	case "rating":
		order = "m.rating IS NULL, m.rating DESC, m.title COLLATE NOCASE"
	}
	limit := f.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := "SELECT " + movieCols + " FROM vod_movies m LEFT JOIN vod_progress p ON p.movie_id=m.id AND p.user_id=? WHERE " +
		where + " ORDER BY " + order + " LIMIT ? OFFSET ?"
	rows, err := s.DB.QueryContext(ctx, q, append(append([]any{f.UserID}, args...), limit, max(f.Offset, 0))...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []Movie
	for rows.Next() {
		m, err := scanMovie(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, m)
	}
	return out, total, rows.Err()
}

// ContinueWatching is films the user stopped part-way through, most recent first.
func (s *Service) ContinueWatching(ctx context.Context, userID int64, maxRating string) ([]Movie, error) {
	where, args := ratingClause(maxRating)
	rows, err := s.DB.QueryContext(ctx, "SELECT "+movieCols+` FROM vod_movies m JOIN vod_progress p ON p.movie_id=m.id AND p.user_id=?
		WHERE p.position_ms>0 AND `+where+`
		AND NOT EXISTS (SELECT 1 FROM vod_continue_hidden h WHERE h.user_id=p.user_id AND h.movie_id=m.id AND h.hidden_at>=p.updated_at)
		ORDER BY p.updated_at DESC LIMIT 20`, append([]any{userID}, args...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Movie
	for rows.Next() {
		m, err := scanMovie(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

type Category struct {
	Name  string
	Count int
}

// Categories are the source's groups (e.g. "'80s Movies") with how many films each holds.
func (s *Service) Categories(ctx context.Context, maxRating string) ([]Category, error) {
	where, args := ratingClause(maxRating)
	rows, err := s.DB.QueryContext(ctx, "SELECT m.category, COUNT(*) FROM vod_movies m WHERE m.category<>'' AND "+where+
		" GROUP BY m.category ORDER BY m.category COLLATE NOCASE", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Category
	for rows.Next() {
		var c Category
		rows.Scan(&c.Name, &c.Count)
		out = append(out, c)
	}
	return out, rows.Err()
}

// Movie returns one film with the user's progress.
func (s *Service) Movie(ctx context.Context, id, userID int64) (Movie, error) {
	m, err := scanMovie(s.DB.QueryRowContext(ctx, "SELECT "+movieCols+
		" FROM vod_movies m LEFT JOIN vod_progress p ON p.movie_id=m.id AND p.user_id=? WHERE m.id=?", userID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return m, ErrNotFound
	}
	return m, err
}

type Status struct {
	Movies, Matched, Unmatched int
	Series                     int
	Sources                    []SourceStatus
}

type SourceStatus struct {
	ID, Name    string
	Movies      int
	Series      int
	RefreshedAt *time.Time
	Error       string
}

func (s *Service) Status(ctx context.Context) (st Status, err error) {
	err = s.DB.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(match_state='matched'),0), COALESCE(SUM(match_state='failed'),0)
		FROM vod_movies`).Scan(&st.Movies, &st.Matched, &st.Unmatched)
	if err != nil {
		return
	}
	if err = s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM vod_series").Scan(&st.Series); err != nil {
		return
	}
	stored := map[string]SourceStatus{}
	rows, err := s.DB.QueryContext(ctx, "SELECT id, name, movies, series, refreshed_at, COALESCE(error,'') FROM vod_sources")
	if err != nil {
		return
	}
	for rows.Next() {
		var ss SourceStatus
		var at sql.NullString
		rows.Scan(&ss.ID, &ss.Name, &ss.Movies, &ss.Series, &at, &ss.Error)
		if t, err := time.Parse(time.RFC3339, at.String); err == nil {
			ss.RefreshedAt = &t
		}
		stored[ss.ID] = ss
	}
	rows.Close()
	for _, src := range s.Sources() {
		ss, ok := stored[src.ID]
		if !ok {
			ss = SourceStatus{ID: src.ID}
		}
		ss.Name = src.Name
		st.Sources = append(st.Sources, ss)
	}
	return st, nil
}

// ---- progress

// SetProgress records where the user is. Past the library's watched threshold the film
// counts as watched and starts from the beginning next time.
func (s *Service) SetProgress(ctx context.Context, userID, movieID, positionMS, durationMS int64) error {
	pct := int64(s.Settings.Get().Library.WatchedThresholdPercent)
	if pct <= 0 || pct > 100 {
		pct = 90
	}
	if durationMS > 0 && positionMS*100 >= durationMS*pct {
		return s.SetWatched(ctx, userID, movieID, true)
	}
	if positionMS < 10_000 {
		return nil // a few seconds in isn't worth resuming
	}
	_, err := s.DB.ExecContext(ctx, `INSERT INTO vod_progress(user_id, movie_id, position_ms, updated_at) VALUES(?,?,?,?)
		ON CONFLICT(user_id, movie_id) DO UPDATE SET position_ms=excluded.position_ms, updated_at=excluded.updated_at`,
		userID, movieID, positionMS, now())
	return err
}

// SetWatched marks a film watched or not, clearing the resume point either way.
func (s *Service) SetWatched(ctx context.Context, userID, movieID int64, watched bool) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO vod_progress(user_id, movie_id, position_ms, watched, updated_at) VALUES(?,?,0,?,?)
		ON CONFLICT(user_id, movie_id) DO UPDATE SET position_ms=0, watched=excluded.watched, updated_at=excluded.updated_at`,
		userID, movieID, watched, now())
	return err
}

// UserAgent is the one a source asks for, sent when fetching its films too.
func (s *Service) UserAgent(sourceID string) string {
	for _, src := range s.Settings.Get().Integrations.VODSources {
		if src.ID == sourceID {
			return src.UserAgent
		}
	}
	return ""
}
