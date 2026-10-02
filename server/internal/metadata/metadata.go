// Package metadata matches scanned items to online providers (TMDB for movies and TV) and
// stores titles, summaries, ratings, genres, cast and artwork. Locked fields (edited by an
// admin) are never overwritten.
package metadata

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"marquee/internal/metadata/deezer"
	"marquee/internal/metadata/musicbrainz"
	"marquee/internal/metadata/tmdb"
	"marquee/internal/scanner/naming"
	"marquee/internal/settings"
)

type Progress struct{ Done, Total int }

type Service struct {
	DB       *sql.DB
	Settings *settings.Store

	mu         sync.Mutex
	omdbBudget ratingsBudget
	deezer     *deezer.Client
	mb         *musicbrainz.Client
	client     *tmdb.Client
	key        string
	lang       string
}

func (s *Service) tmdb(lang string) (*tmdb.Client, error) {
	key := s.Settings.Get().Metadata.TMDBAPIKey
	if key == "" {
		return nil, tmdb.ErrNoKey
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client == nil || s.key != key || s.lang != lang {
		s.client = tmdb.New(key, lang)
		s.key, s.lang = key, lang
	}
	return s.client, nil
}

type pending struct {
	ID    int64
	Type  string
	Title string
	Year  int
	IDs   map[string]string
}

// MatchLibrary matches every unmatched movie/show in a library. It is a no-op (not an
// error) when no TMDB key is configured, so scans still succeed.
func (s *Service) MatchLibrary(ctx context.Context, libID int64, libType, language string, report func(Progress)) error {
	var typ string
	switch libType {
	case "movies":
		typ = "movie"
	case "shows", "anime":
		typ = "show"
	default:
		return nil
	}
	if language == "" {
		language = s.Settings.Get().General.MetadataLanguage
	}
	client, err := s.tmdb(language)
	if errors.Is(err, tmdb.ErrNoKey) {
		slog.Info("metadata matching skipped: no TMDB API key")
		return nil
	}

	items, err := s.unmatched(ctx, libID, typ)
	if err != nil {
		return err
	}
	var matched, failed int
	for i, it := range items {
		if err := ctx.Err(); err != nil {
			return err
		}
		if report != nil {
			report(Progress{Done: i, Total: len(items)})
		}
		var ok bool
		if typ == "movie" {
			ok, err = s.matchMovie(ctx, client, it)
		} else {
			ok, err = s.matchShow(ctx, client, it)
		}
		if errors.Is(err, tmdb.ErrInvalidKey) {
			return err
		}
		if err != nil {
			slog.Warn("metadata match failed", "item", it.Title, "err", err)
		}
		if ok {
			matched++
		} else {
			failed++
			s.DB.ExecContext(ctx, `UPDATE items SET match_state = 'failed', metadata_refreshed_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = ?`, it.ID)
		}
	}
	slog.Info("metadata matching finished", "library", libID, "matched", matched, "unmatched", failed)
	return nil
}

// MatcherVersion is bumped whenever matching improves, so items that failed under an older
// matcher are retried after an upgrade instead of waiting for the weekly retry.
const MatcherVersion = 5

// ResetFailedIfMatcherChanged marks failed items for retry after a matcher upgrade.
func (s *Service) ResetFailedIfMatcherChanged(ctx context.Context) {
	var v int
	s.DB.QueryRowContext(ctx, `SELECT CAST(value AS INTEGER) FROM settings WHERE key = 'matcher_version'`).Scan(&v)
	if v >= MatcherVersion {
		return
	}
	r, err := s.DB.ExecContext(ctx, `UPDATE items SET match_state = 'unmatched', metadata_refreshed_at = NULL WHERE match_state = 'failed'`)
	if err != nil {
		return
	}
	n, _ := r.RowsAffected()
	n += s.recheckSuspicious(ctx)
	s.DB.ExecContext(ctx, `INSERT INTO settings(key, value) VALUES ('matcher_version', ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, MatcherVersion)
	slog.Info("matcher upgraded; retrying previously failed items", "items", n)
}

// recheckSuspicious queues a re-match for automatic (unedited) matches that a newer matcher
// would likely do better: shows whose folder has no year (several shows can share the
// title), and movies whose matched title shares no word with the file name.
func (s *Service) recheckSuspicious(ctx context.Context) int64 {
	var n int64
	var shows []int64
	if rows, err := s.DB.QueryContext(ctx, `SELECT id FROM items WHERE type = 'show' AND match_state = 'matched'
		AND scan_key LIKE 't:%:0' AND locked_fields = '[]'`); err == nil {
		for rows.Next() {
			var id int64
			rows.Scan(&id)
			shows = append(shows, id)
		}
		rows.Close()
	}
	for _, id := range shows {
		s.forgetMatch(ctx, id)
	}
	n += int64(len(shows))
	roots := map[string]bool{}
	if rows, err := s.DB.QueryContext(ctx, `SELECT path FROM library_paths`); err == nil {
		for rows.Next() {
			var p string
			rows.Scan(&p)
			roots[p] = true
		}
		rows.Close()
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT i.id, i.title, COALESCE(i.year, 0),
		(SELECT f.path FROM media_versions v JOIN media_files f ON f.version_id = v.id WHERE v.item_id = i.id LIMIT 1)
		FROM items i WHERE i.type = 'movie' AND i.match_state = 'matched' AND i.locked_fields = '[]' AND i.scan_key LIKE 't:%'`)
	if err != nil {
		return n
	}
	var suspicious []int64
	for rows.Next() {
		var id int64
		var title string
		var year int
		var path sql.NullString
		rows.Scan(&id, &title, &year, &path)
		if !path.Valid {
			continue
		}
		dir := filepath.Dir(path.String)
		folder := filepath.Base(dir)
		if roots[dir] {
			folder = ""
		}
		want := naming.ParseMovie(folder, filepath.Base(path.String))
		if !naming.SharesWord(title, want.Title) && naming.MatchScore(title, year, want.Title, want.Year) < 3 {
			suspicious = append(suspicious, id)
		}
	}
	rows.Close()
	for _, id := range suspicious {
		s.forgetMatch(ctx, id)
	}
	return n + int64(len(suspicious))
}

// forgetMatch queues an item for a fresh match. Its agent-assigned provider ids are dropped
// so the matcher searches again instead of re-applying the old id. (Ids from file names are
// only on items whose scan key is id-based, which are never re-checked.)
func (s *Service) forgetMatch(ctx context.Context, id int64) {
	s.DB.ExecContext(ctx, `DELETE FROM external_ids WHERE item_id = ? AND provider IN ('tmdb', 'imdb', 'tvdb')`, id)
	s.DB.ExecContext(ctx, `UPDATE items SET match_state = 'unmatched' WHERE id = ?`, id)
}

// NeedsMatching reports whether a library has items awaiting a metadata attempt.
func (s *Service) NeedsMatching(ctx context.Context, libID int64) bool {
	var n int
	s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM items i WHERE library_id = ? AND type IN ('artist', 'album')
		AND metadata_refreshed_at IS NULL AND NOT EXISTS (SELECT 1 FROM artwork w WHERE w.item_id = i.id AND w.kind = 'poster')`, libID).Scan(&n)
	if n > 0 {
		return true
	}
	if s.Settings.Get().Metadata.TMDBAPIKey == "" {
		return false
	}
	s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM items WHERE library_id = ? AND type IN ('movie', 'show') AND (match_state = 'unmatched'
		OR (match_state = 'failed' AND (metadata_refreshed_at IS NULL OR metadata_refreshed_at < strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '-7 days'))))`,
		libID).Scan(&n)
	return n > 0
}

func (s *Service) unmatched(ctx context.Context, libID int64, typ string) ([]pending, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, title, COALESCE(year, 0), COALESCE(scan_key, '') FROM items
		WHERE library_id = ? AND type = ? AND (match_state = 'unmatched'
		   OR (match_state = 'failed' AND (metadata_refreshed_at IS NULL
		       OR metadata_refreshed_at < strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '-7 days'))))
		ORDER BY id`, libID, typ)
	if err != nil {
		return nil, err
	}
	var out []pending
	for rows.Next() {
		p := pending{Type: typ, IDs: map[string]string{}}
		var key string
		if err := rows.Scan(&p.ID, &p.Title, &p.Year, &key); err != nil {
			rows.Close()
			return nil, err
		}
		// Search with the year from the file/folder name ("t:<title>:<year>"), not one a
		// previous (possibly wrong) match wrote onto the item.
		if strings.HasPrefix(key, "t:") {
			if i := strings.LastIndexByte(key, ':'); i > 1 {
				if y, err := strconv.Atoi(key[i+1:]); err == nil {
					p.Year = y
				}
			}
		}
		out = append(out, p)
	}
	rows.Close()
	for i := range out {
		r, err := s.DB.QueryContext(ctx, `SELECT provider, value FROM external_ids WHERE item_id = ?`, out[i].ID)
		if err != nil {
			return nil, err
		}
		for r.Next() {
			var k, v string
			r.Scan(&k, &v)
			out[i].IDs[k] = v
		}
		r.Close()
	}
	return out, nil
}

// bestMatch picks the search result that best fits title/year, or 0.
func bestMatch(results []tmdb.SearchResult, title string, year int) int {
	want := naming.Normalize(naming.SortTitle(title))
	type scored struct {
		id    int
		score float64
	}
	var best []scored
	for _, r := range results {
		// Compare without leading articles: "Dog of Flanders" = "A Dog of Flanders".
		t := naming.Normalize(naming.SortTitle(r.DisplayTitle()))
		o := naming.Normalize(naming.SortTitle(r.Original()))
		var ts float64
		switch {
		case t == want || o == want:
			ts = 2
		case want != "" && (strings.HasPrefix(t, want) || strings.HasPrefix(want, t) || strings.Contains(t, want)):
			ts = 1
		default:
			continue
		}
		ys := 0.0
		if year > 0 {
			switch d := r.Year() - year; {
			case d == 0:
				ys = 2
			case d == 1 || d == -1:
				ys = 1
			default:
				if r.Year() > 0 {
					continue // a definite year mismatch is a different title
				}
			}
		}
		// Popularity breaks ties between remakes/same-name titles.
		best = append(best, scored{r.ID, ts + ys + min(float64(r.VoteCount)/5000, 0.5)})
	}
	if len(best) == 0 {
		return 0
	}
	sort.SliceStable(best, func(i, j int) bool { return best[i].score > best[j].score })
	if best[0].score < 2 { // needs at least an exact title, or a fuzzy title plus a year match
		return 0
	}
	return best[0].id
}

func (s *Service) matchMovie(ctx context.Context, c *tmdb.Client, it pending) (bool, error) {
	id, _ := strconv.Atoi(it.IDs["tmdb"])
	if id == 0 && it.IDs["imdb"] != "" {
		if movies, _, err := c.FindByExternalID(ctx, it.IDs["imdb"], "imdb_id"); err == nil && len(movies) > 0 {
			id = movies[0]
		}
	}
	if id == 0 {
		var err error
		id, err = searchMovie(ctx, c, it.Title, it.Year)
		if err != nil {
			return false, err
		}
		if id == 0 && strings.Contains(it.Title, " - ") {
			// Folder names use " - " where titles have ":" ("Kingdom IV - Return of the Great General").
			if id, err = searchMovie(ctx, c, strings.ReplaceAll(it.Title, " - ", ": "), it.Year); err != nil {
				return false, err
			}
		}
	}
	if id == 0 {
		return false, nil
	}
	m, err := c.Movie(ctx, id)
	if err != nil {
		return false, err
	}
	return true, s.applyMovie(ctx, it.ID, m)
}

// searchMovie searches with and without the year filter (TMDB filters on the primary
// release date, which can differ from the year in the folder name).
func searchMovie(ctx context.Context, c *tmdb.Client, title string, year int) (int, error) {
	res, err := c.SearchMovie(ctx, title, year)
	if err != nil {
		return 0, err
	}
	if id := bestMatch(res, title, year); id != 0 {
		return id, nil
	}
	if id := soleYearMatch(res, title, year); id != 0 {
		return id, nil
	}
	if year == 0 {
		return 0, nil
	}
	if res, err = c.SearchMovie(ctx, title, 0); err != nil {
		return 0, err
	}
	return bestMatch(res, title, year), nil
}

// soleYearMatch accepts TMDB's top result when it is the only result from exactly the
// requested year. TMDB searches alternative titles too, so this catches films filed under
// another name ("Godzilla Gigantis the Fire Monster" → "Godzilla Raids Again", 1955).
func soleYearMatch(res []tmdb.SearchResult, title string, year int) int {
	if year == 0 || len(res) == 0 || res[0].Year() != year {
		return 0
	}
	// It must still share a real word with the name ("The Dish" ≠ "Death Before Dishonour").
	if !naming.SharesWord(title, res[0].DisplayTitle()) && !naming.SharesWord(title, res[0].Original()) {
		return 0
	}
	for _, r := range res[1:] {
		if r.Year() == year {
			return 0
		}
	}
	return res[0].ID
}

func (s *Service) matchShow(ctx context.Context, c *tmdb.Client, it pending) (bool, error) {
	id, _ := strconv.Atoi(it.IDs["tmdb"])
	for _, src := range [][2]string{{"tvdb", "tvdb_id"}, {"imdb", "imdb_id"}} {
		if id == 0 && it.IDs[src[0]] != "" {
			if _, tv, err := c.FindByExternalID(ctx, it.IDs[src[0]], src[1]); err == nil && len(tv) > 0 {
				id = tv[0]
			}
		}
	}
	if id == 0 {
		res, err := c.SearchTV(ctx, it.Title, it.Year)
		if err != nil {
			return false, err
		}
		id = bestMatch(res, it.Title, it.Year)
		if id == 0 && strings.Contains(it.Title, " - ") {
			// Folder names use " - " where titles have ":" ("Star Trek - Discovery").
			alt := strings.ReplaceAll(it.Title, " - ", ": ")
			if res, err = c.SearchTV(ctx, alt, it.Year); err == nil {
				id = bestMatch(res, alt, it.Year)
			}
		}
		if id != 0 && it.Year == 0 {
			// Without a year, several shows can share the title ("One Piece": the 1999
			// anime and the 2023 series). Prefer the one whose seasons fit the files.
			if better, err := s.bestFitShow(ctx, c, it.ID, it.Title, res, id); err == nil {
				id = better
			}
		}
	}
	if id == 0 {
		return false, nil
	}
	show, err := c.TV(ctx, id)
	if err != nil {
		return false, err
	}
	if err := s.applyShow(ctx, it.ID, show); err != nil {
		return false, err
	}
	return true, s.applySeasons(ctx, c, it.ID, show)
}

// bestFitShow chooses among same-titled TMDB shows by how well their seasons and episode
// counts fit the local files. fallback is returned when nothing fits better.
func (s *Service) bestFitShow(ctx context.Context, c *tmdb.Client, showID int64, title string, res []tmdb.SearchResult, fallback int) (int, error) {
	want := naming.Normalize(naming.SortTitle(title))
	var same []int
	for _, r := range res {
		if naming.Normalize(naming.SortTitle(r.DisplayTitle())) == want || naming.Normalize(naming.SortTitle(r.Original())) == want {
			same = append(same, r.ID)
		}
	}
	if len(same) < 2 {
		return fallback, nil
	}
	local := map[int]int{} // season → episodes on disk
	rows, err := s.DB.QueryContext(ctx, `SELECT s.idx, COUNT(e.id) FROM items s LEFT JOIN items e ON e.parent_id = s.id
		WHERE s.parent_id = ? AND s.type = 'season' GROUP BY s.id`, showID)
	if err != nil {
		return fallback, err
	}
	for rows.Next() {
		var n, eps int
		rows.Scan(&n, &eps)
		local[n] = eps
	}
	rows.Close()
	best, bestFit := fallback, -1.0
	for i, id := range same {
		if i >= 4 {
			break
		}
		t, err := c.TV(ctx, id)
		if err != nil {
			continue
		}
		counts := map[int]int{}
		regular := 0
		for _, se := range t.Seasons {
			counts[se.SeasonNumber] = se.EpisodeCount
			if se.SeasonNumber > 0 {
				regular++
			}
		}
		fit := 0.0
		for n, eps := range local {
			if c, ok := counts[n]; ok && c >= eps {
				fit++
			}
		}
		// A show with far more seasons than the files have is a worse fit.
		fit -= math.Abs(float64(regular-len(local))) / 20
		if fit > bestFit || (fit == bestFit && id == fallback) {
			best, bestFit = id, fit
		}
	}
	return best, nil
}

// ---- writing ----

type fields map[string]any

// update sets item columns, skipping locked fields and empty values.
func update(ctx context.Context, tx *sql.Tx, itemID int64, f fields) error {
	var lockedJSON string
	if err := tx.QueryRowContext(ctx, `SELECT locked_fields FROM items WHERE id = ?`, itemID).Scan(&lockedJSON); err != nil {
		return err
	}
	var locked []string
	json.Unmarshal([]byte(lockedJSON), &locked)
	isLocked := map[string]bool{}
	for _, l := range locked {
		isLocked[l] = true
	}
	var sets []string
	var args []any
	keys := make([]string, 0, len(f))
	for k := range f {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := f[k]
		if isLocked[k] || v == nil || v == "" || v == 0 || v == 0.0 {
			continue
		}
		sets = append(sets, k+" = ?")
		args = append(args, v)
	}
	sets = append(sets, "updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')")
	args = append(args, itemID)
	_, err := tx.ExecContext(ctx, `UPDATE items SET `+strings.Join(sets, ", ")+` WHERE id = ?`, args...)
	return err
}

func year(date string) int {
	if len(date) >= 4 {
		y, _ := strconv.Atoi(date[:4])
		return y
	}
	return 0
}

func (s *Service) applyMovie(ctx context.Context, itemID int64, m *tmdb.Movie) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	studio := ""
	if len(m.Companies) > 0 {
		studio = m.Companies[0].Name
	}
	orig := m.OriginalTitle
	if orig == m.Title {
		orig = ""
	}
	if err := update(ctx, tx, itemID, fields{
		"title": m.Title, "sort_title": naming.SortTitle(m.Title), "original_title": orig, "summary": m.Overview,
		"tagline": m.Tagline, "year": year(m.ReleaseDate), "originally_available_at": m.ReleaseDate,
		"content_rating": m.Certification("US"), "studio": studio, "audience_rating": m.VoteAverage,
	}); err != nil {
		return err
	}
	if err := finishMatch(ctx, tx, itemID, map[string]string{"tmdb": strconv.Itoa(m.ID), "imdb": m.IMDBID}); err != nil {
		return err
	}
	if err := setGenres(ctx, tx, itemID, m.Genres); err != nil {
		return err
	}
	if err := setCredits(ctx, tx, itemID, m.Credits); err != nil {
		return err
	}
	if err := setArtwork(ctx, tx, itemID, m.Images, m.PosterPath, m.BackdropPath); err != nil {
		return err
	}
	if err := setCollection(ctx, tx, itemID, m.Collection); err != nil {
		return err
	}
	return tx.Commit()
}

func (s *Service) applyShow(ctx context.Context, itemID int64, t *tmdb.TVShow) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	studio := ""
	if len(t.Networks) > 0 {
		studio = t.Networks[0].Name
	}
	orig := t.OriginalName
	if orig == t.Name {
		orig = ""
	}
	if err := update(ctx, tx, itemID, fields{
		"title": t.Name, "sort_title": naming.SortTitle(t.Name), "original_title": orig, "summary": t.Overview,
		"tagline": t.Tagline, "year": year(t.FirstAirDate), "originally_available_at": t.FirstAirDate,
		"content_rating": t.Certification("US"), "studio": studio, "audience_rating": t.VoteAverage,
	}); err != nil {
		return err
	}
	ids := map[string]string{"tmdb": strconv.Itoa(t.ID), "imdb": t.ExternalIDs.IMDB}
	if t.ExternalIDs.TVDB > 0 {
		ids["tvdb"] = strconv.Itoa(t.ExternalIDs.TVDB)
	}
	if err := finishMatch(ctx, tx, itemID, ids); err != nil {
		return err
	}
	if err := setGenres(ctx, tx, itemID, t.Genres); err != nil {
		return err
	}
	if err := setCredits(ctx, tx, itemID, t.Credits); err != nil {
		return err
	}
	if err := setArtwork(ctx, tx, itemID, t.Images, t.PosterPath, t.BackdropPath); err != nil {
		return err
	}
	return tx.Commit()
}

// applySeasons fills season and episode metadata for the seasons present in the library.
func (s *Service) applySeasons(ctx context.Context, c *tmdb.Client, showID int64, t *tmdb.TVShow) error {
	known := map[int]bool{}
	for _, se := range t.Seasons {
		known[se.SeasonNumber] = true
	}
	byYear := seasonsByYear(t)
	rows, err := s.DB.QueryContext(ctx, `SELECT id, idx FROM items WHERE parent_id = ? AND type = 'season'`, showID)
	if err != nil {
		return err
	}
	type season struct {
		id  int64
		idx int
	}
	var seasons []season
	for rows.Next() {
		var se season
		rows.Scan(&se.id, &se.idx)
		seasons = append(seasons, se)
	}
	rows.Close()

	for _, se := range seasons {
		tmdbSeason := se.idx
		if !known[tmdbSeason] {
			// Year-numbered seasons (Sonarr/TVDB style "S2014E03", e.g. MythBusters) map to
			// the TMDB season that aired that year.
			n, ok := byYear[se.idx]
			if !ok {
				continue
			}
			tmdbSeason = n
		}
		data, err := c.Season(ctx, t.ID, tmdbSeason)
		if errors.Is(err, tmdb.ErrNotFound) {
			continue
		}
		if err != nil {
			return err
		}
		if se.idx == 1 && tmdbSeason == 1 {
			// Absolute numbering (common for anime): one local season holding episodes
			// past TMDB's season 1. Number TMDB's regular seasons consecutively instead.
			if abs, err := s.absoluteEpisodes(ctx, c, t, se.id, data); err != nil {
				return err
			} else if abs != nil {
				data = abs
			}
		}
		if err := s.applySeason(ctx, se.id, data); err != nil {
			return err
		}
	}
	return nil
}

// absoluteEpisodes returns season 1 with episodes from all regular TMDB seasons numbered
// 1…n, when the local season has episode numbers beyond TMDB's season 1. Otherwise nil.
func (s *Service) absoluteEpisodes(ctx context.Context, c *tmdb.Client, t *tmdb.TVShow, seasonID int64, first *tmdb.Season) (*tmdb.Season, error) {
	var maxLocal int
	s.DB.QueryRowContext(ctx, `SELECT COALESCE(MAX(idx), 0) FROM items WHERE parent_id = ? AND type = 'episode'`, seasonID).Scan(&maxLocal)
	if maxLocal <= len(first.Episodes) {
		return nil, nil
	}
	nums := []int{}
	for _, se := range t.Seasons {
		if se.SeasonNumber > 1 {
			nums = append(nums, se.SeasonNumber)
		}
	}
	sort.Ints(nums)
	all := append([]tmdb.Episode{}, first.Episodes...)
	for _, n := range nums {
		if len(all) >= maxLocal {
			break
		}
		sd, err := c.Season(ctx, t.ID, n)
		if errors.Is(err, tmdb.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		all = append(all, sd.Episodes...)
	}
	out := *first
	out.Episodes = make([]tmdb.Episode, len(all))
	for i, e := range all {
		e.EpisodeNumber = i + 1
		out.Episodes[i] = e
	}
	return &out, nil
}

// seasonsByYear maps an air year to a TMDB season number, for shows whose local seasons
// are numbered by year. Only regular seasons (number > 0) with a unique year are used.
func seasonsByYear(t *tmdb.TVShow) map[int]int {
	out := map[int]int{}
	dup := map[int]bool{}
	for _, se := range t.Seasons {
		y := year(se.AirDate)
		if se.SeasonNumber == 0 || y == 0 {
			continue
		}
		if _, exists := out[y]; exists {
			dup[y] = true
		}
		out[y] = se.SeasonNumber
	}
	for y := range dup {
		delete(out, y)
	}
	return out
}

func (s *Service) applySeason(ctx context.Context, seasonID int64, data *tmdb.Season) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := update(ctx, tx, seasonID, fields{"summary": data.Overview, "originally_available_at": data.AirDate, "year": year(data.AirDate)}); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE items SET match_state = 'matched' WHERE id = ?`, seasonID); err != nil {
		return err
	}
	if data.PosterPath != "" {
		if err := addArt(ctx, tx, seasonID, "poster", data.PosterPath, 0, 0, ""); err != nil {
			return err
		}
	}
	byNum := map[int]tmdb.Episode{}
	for _, e := range data.Episodes {
		byNum[e.EpisodeNumber] = e
	}
	rows, err := tx.QueryContext(ctx, `SELECT id, idx FROM items WHERE parent_id = ? AND type = 'episode'`, seasonID)
	if err != nil {
		return err
	}
	type ep struct {
		id  int64
		idx int
	}
	var eps []ep
	for rows.Next() {
		var e ep
		rows.Scan(&e.id, &e.idx)
		eps = append(eps, e)
	}
	rows.Close()
	for _, e := range eps {
		m, ok := byNum[e.idx]
		if !ok {
			continue
		}
		if err := update(ctx, tx, e.id, fields{"title": m.Name, "summary": m.Overview, "originally_available_at": m.AirDate,
			"year": year(m.AirDate), "audience_rating": m.VoteAverage}); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE items SET match_state = 'matched' WHERE id = ?`, e.id); err != nil {
			return err
		}
		if m.StillPath != "" {
			if err := addArt(ctx, tx, e.id, "thumb", m.StillPath, 0, 0, ""); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func finishMatch(ctx context.Context, tx *sql.Tx, itemID int64, ids map[string]string) error {
	for p, v := range ids {
		if v == "" || v == "0" {
			continue
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO external_ids(item_id, provider, value) VALUES (?, ?, ?)
			ON CONFLICT(item_id, provider) DO UPDATE SET value = excluded.value`, itemID, p, v); err != nil {
			return err
		}
	}
	_, err := tx.ExecContext(ctx, `UPDATE items SET match_state = 'matched', metadata_refreshed_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = ?`, itemID)
	return err
}

func setGenres(ctx context.Context, tx *sql.Tx, itemID int64, genres []tmdb.Genre) error {
	if len(genres) == 0 {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM item_tags WHERE item_id = ? AND tag_id IN (SELECT id FROM tags WHERE kind = 'genre')`, itemID); err != nil {
		return err
	}
	for _, g := range genres {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO tags(kind, name) VALUES ('genre', ?)`, g.Name); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO item_tags(item_id, tag_id) SELECT ?, id FROM tags WHERE kind = 'genre' AND name = ?`, itemID, g.Name); err != nil {
			return err
		}
	}
	return nil
}

var crewRoles = map[string]string{"Director": "director", "Screenplay": "writer", "Writer": "writer", "Creator": "creator",
	"Producer": "producer", "Original Music Composer": "composer"}

func setCredits(ctx context.Context, tx *sql.Tx, itemID int64, cr tmdb.Credits) error {
	if len(cr.Cast) == 0 && len(cr.Crew) == 0 {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM credits WHERE item_id = ?`, itemID); err != nil {
		return err
	}
	person := func(id int, name, photo string) (int64, error) {
		var photoRef any
		if photo != "" {
			photoRef = tmdb.ImageBase + photo
		}
		var pid int64
		err := tx.QueryRowContext(ctx, `INSERT INTO people(name, tmdb_id, photo_path) VALUES (?, ?, ?)
			ON CONFLICT(tmdb_id) DO UPDATE SET name = excluded.name, photo_path = COALESCE(excluded.photo_path, people.photo_path)
			RETURNING id`, name, id, photoRef).Scan(&pid)
		return pid, err
	}
	for i, c := range cr.Cast {
		if i >= 30 {
			break
		}
		pid, err := person(c.ID, c.Name, c.ProfilePath)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO credits(item_id, person_id, role, character, ord) VALUES (?, ?, 'actor', ?, ?)`,
			itemID, pid, c.Character, c.Order); err != nil {
			return err
		}
	}
	n := 0
	for _, c := range cr.Crew {
		role, ok := crewRoles[c.Job]
		if !ok || n >= 15 {
			continue
		}
		n++
		pid, err := person(c.ID, c.Name, c.ProfilePath)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO credits(item_id, person_id, role, ord) VALUES (?, ?, ?, ?)`, itemID, pid, role, n); err != nil {
			return err
		}
	}
	return nil
}

// setArtwork replaces TMDB artwork for an item. Local artwork stays selected if present.
func setArtwork(ctx context.Context, tx *sql.Tx, itemID int64, imgs tmdb.Images, poster, backdrop string) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM artwork WHERE item_id = ? AND source = 'tmdb'`, itemID); err != nil {
		return err
	}
	pick := func(list []tmdb.Image, fallback string, preferNoText bool) (tmdb.Image, bool) {
		if len(list) == 0 {
			return tmdb.Image{FilePath: fallback}, fallback != ""
		}
		best := list[0]
		for _, im := range list[1:] {
			// Backdrops without text (null language) look better behind UI.
			if preferNoText && (im.Language == nil) != (best.Language == nil) {
				if im.Language == nil {
					best = im
				}
				continue
			}
			if im.VoteAverage*float64(min(im.VoteCount, 20)) > best.VoteAverage*float64(min(best.VoteCount, 20)) {
				best = im
			}
		}
		return best, true
	}
	if p, ok := pick(imgs.Posters, poster, false); ok {
		if err := addArt(ctx, tx, itemID, "poster", p.FilePath, p.Width, p.Height, lang(p)); err != nil {
			return err
		}
	}
	if b, ok := pick(imgs.Backdrops, backdrop, true); ok {
		if err := addArt(ctx, tx, itemID, "backdrop", b.FilePath, b.Width, b.Height, lang(b)); err != nil {
			return err
		}
	}
	if len(imgs.Logos) > 0 {
		l, _ := pick(imgs.Logos, "", false)
		if err := addArt(ctx, tx, itemID, "logo", l.FilePath, l.Width, l.Height, lang(l)); err != nil {
			return err
		}
	}
	return nil
}

func lang(im tmdb.Image) string {
	if im.Language == nil {
		return ""
	}
	return *im.Language
}

func addArt(ctx context.Context, tx *sql.Tx, itemID int64, kind, path string, w, h int, language string) error {
	if path == "" {
		return nil
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO artwork(item_id, kind, source, remote_url, width, height, language, selected)
		SELECT ?, ?, 'tmdb', ?, ?, ?, ?, NOT EXISTS (SELECT 1 FROM artwork WHERE item_id = ? AND kind = ? AND selected = 1)`,
		itemID, kind, tmdb.ImageBase+path, nullZero(w), nullZero(h), nullEmpty(language), itemID, kind)
	return err
}

func nullZero(n int) any {
	if n == 0 {
		return nil
	}
	return n
}

func nullEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// Describe is used in logs and errors.
func (p pending) String() string { return fmt.Sprintf("%s %q (%d)", p.Type, p.Title, p.Year) }
