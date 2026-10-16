package vodsave

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"

	"marquee/internal/scanner/naming"
	"marquee/internal/vod"
)

// Finding a film's or episode's copy in the library, and how the copy on demand compares.

// LibraryCopy is a film or episode in the library, with its best file.
type LibraryCopy struct {
	ItemID    int64
	FileID    int64 // the best file
	LibraryID int64
	Path      string
	Quality   Quality
}

// Match is how a film or episode on demand compares with the library.
type Match struct {
	Verdict string // none, better, same, worse
	Reason  string
	VOD     Quality
	// VODKnown: the copy on demand's quality is known. Pending: it's being checked (the
	// verdict is "same" until then).
	VODKnown, Pending bool
	Library           *LibraryCopy
}

// Savable reports whether the title can be saved (not in the library, or as an upgrade).
func (m Match) Savable() bool { return m.Verdict == VerdictNone || m.Verdict == VerdictBetter }

func match(lib *LibraryCopy, q Quality, known, pending bool, note string) Match {
	if lib == nil {
		return Match{Verdict: VerdictNone, Reason: "Not in the library", VOD: q, VODKnown: known, Pending: pending && !known}
	}
	m := Match{VOD: q, VODKnown: known, Pending: pending, Library: lib}
	if !known {
		m.Verdict, m.Reason = VerdictSame, note
		if m.Reason == "" {
			m.Reason = "The quality of the copy on demand isn't known"
		}
		return m
	}
	m.Verdict, m.Reason = Compare(q, lib.Quality)
	return m
}

// ---- the library's copies

const fileCols = `f.id, f.library_id, f.path, COALESCE(f.width,0), COALESCE(f.height,0), COALESCE(f.video_codec,''),
	COALESCE(f.hdr_format,''), COALESCE(f.dv_profile,0), COALESCE(f.bitrate_kbps,0)`

func scanFile(sc interface{ Scan(...any) error }, extra ...any) (LibraryCopy, error) {
	var c LibraryCopy
	var dv int
	err := sc.Scan(append(extra, &c.FileID, &c.LibraryID, &c.Path, &c.Quality.Width, &c.Quality.Height, &c.Quality.Codec,
		&c.Quality.HDR, &dv, &c.Quality.BitrateKbps)...)
	if c.Quality.HDR == "" && dv > 0 {
		c.Quality.HDR = "dolby_vision"
	}
	return c, err
}

// bestCopy is an item's best available file (nil when it has none).
func (s *Service) bestCopy(ctx context.Context, itemID int64) (*LibraryCopy, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+fileCols+` FROM media_versions v JOIN media_files f ON f.version_id=v.id
		WHERE v.item_id=? AND f.available=1 ORDER BY f.id`, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var best *LibraryCopy
	for rows.Next() {
		c, err := scanFile(rows)
		if err != nil {
			return nil, err
		}
		c.ItemID = itemID
		if best == nil || better(c.Quality, best.Quality) {
			best = &c
		}
	}
	return best, rows.Err()
}

// candidates are the library's items of a type for a title: by TMDB id, else (careful) by
// the same title and year where the item has no other TMDB id.
func (s *Service) candidates(ctx context.Context, typ string, tmdbID int, title string, year int) ([]int64, error) {
	var ids []int64
	if tmdbID > 0 {
		rows, err := s.DB.QueryContext(ctx, `SELECT i.id FROM external_ids x JOIN items i ON i.id=x.item_id
			WHERE x.provider='tmdb' AND x.value=? AND i.type=? AND i.extra_type IS NULL ORDER BY i.id`, strconv.Itoa(tmdbID), typ)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id int64
			rows.Scan(&id)
			ids = append(ids, id)
		}
		rows.Close()
		if len(ids) > 0 {
			return ids, nil
		}
	}
	want := naming.Normalize(title)
	if year <= 0 || want == "" {
		return nil, nil
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT i.id, i.title, COALESCE(i.original_title,''),
		COALESCE((SELECT x.value FROM external_ids x WHERE x.item_id=i.id AND x.provider='tmdb' LIMIT 1),'')
		FROM items i WHERE i.type=? AND i.year=? AND i.extra_type IS NULL ORDER BY i.id`, typ, year)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var t, orig, tmdb string
		if err := rows.Scan(&id, &t, &orig, &tmdb); err != nil {
			return nil, err
		}
		if tmdb != "" && tmdbID > 0 && tmdb != strconv.Itoa(tmdbID) {
			continue // another film or show of the same name
		}
		if naming.Normalize(t) == want || (orig != "" && naming.Normalize(orig) == want) {
			ids = append(ids, id)
		}
	}
	return ids, rows.Err()
}

// MovieCopy is a film's copy in the library (nil when there's none).
func (s *Service) MovieCopy(ctx context.Context, tmdbID int, title string, year int) (*LibraryCopy, error) {
	ids, err := s.candidates(ctx, "movie", tmdbID, title, year)
	if err != nil {
		return nil, err
	}
	var best *LibraryCopy
	for _, id := range ids {
		c, err := s.bestCopy(ctx, id)
		if err != nil {
			return nil, err
		}
		if c != nil && (best == nil || better(c.Quality, best.Quality)) {
			best = c
		}
	}
	return best, nil
}

// EpisodeCopies are a show's episodes in the library, by season and episode.
func (s *Service) EpisodeCopies(ctx context.Context, tmdbID int, title string, year int) (map[[2]int]*LibraryCopy, error) {
	out := map[[2]int]*LibraryCopy{}
	shows, err := s.candidates(ctx, "show", tmdbID, title, year)
	if err != nil {
		return nil, err
	}
	for _, show := range shows {
		rows, err := s.DB.QueryContext(ctx, `SELECT e.id, z.idx, e.idx, `+fileCols+` FROM items e JOIN items z ON z.id=e.parent_id
			JOIN media_versions v ON v.item_id=e.id JOIN media_files f ON f.version_id=v.id
			WHERE e.grandparent_id=? AND e.type='episode' AND f.available=1 AND z.idx IS NOT NULL AND e.idx IS NOT NULL
			ORDER BY f.id`, show)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var item int64
			var se, ep int
			c, err := scanFile(rows, &item, &se, &ep)
			if err != nil {
				rows.Close()
				return nil, err
			}
			c.ItemID = item
			k := [2]int{se, ep}
			if cur := out[k]; cur == nil || better(c.Quality, cur.Quality) {
				out[k] = &c
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	return out, nil
}

// ---- comparisons for the pages admins open

type movieRow struct {
	sourceID, key, file, title string
	year, tmdbID               int
	matched                    bool
	runtime                    int64
}

func (s *Service) movieRow(ctx context.Context, movieID int64) (movieRow, error) {
	var r movieRow
	err := s.DB.QueryRowContext(ctx, `SELECT source_id, source_key, stream_url, title, COALESCE(year,0), COALESCE(tmdb_id,0),
		match_state='matched', runtime_ms FROM vod_movies WHERE id=?`, movieID).
		Scan(&r.sourceID, &r.key, &r.file, &r.title, &r.year, &r.tmdbID, &r.matched, &r.runtime)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	if !r.matched {
		r.tmdbID = 0 // the source's guess isn't trusted until TMDB confirmed it
	}
	return r, err
}

// libraryTitle is the title and year to look a film or show up by.
func libraryTitle(title string, year int) (string, int) {
	name, y := CleanTitle(title)
	if year == 0 {
		year = y
	}
	return name, year
}

// movieMatch compares a film with the library; sync checks its quality now when needed.
func (s *Service) movieMatch(ctx context.Context, movieID int64, r movieRow, sync bool) (Match, error) {
	name, year := libraryTitle(r.title, r.year)
	lib, err := s.MovieCopy(ctx, r.tmdbID, name, year)
	if err != nil {
		return Match{}, err
	}
	stored, ok, err := s.VOD.MovieQuality(ctx, movieID)
	if err != nil {
		return Match{}, err
	}
	var sp *vod.Quality
	if ok {
		sp = &stored
	}
	if lib == nil {
		// Nothing to compare with: no check needed, but say what's known.
		q := Quality{}
		if sp != nil && sp.Error == "" {
			q = toQuality(*sp)
		}
		return match(nil, q, q.Known(), false, ""), nil
	}
	q, known, pending, note := s.vodQuality(ctx, qualityKey{false, movieID}, sp, sync)
	return match(lib, q, known, pending, note), nil
}

// MovieMatch is how a film compares with the library's copy, for its page. Its quality,
// when unknown, is checked in the background (Pending).
func (s *Service) MovieMatch(ctx context.Context, movieID int64) (Match, error) {
	r, err := s.movieRow(ctx, movieID)
	if err != nil {
		return Match{}, err
	}
	return s.movieMatch(ctx, movieID, r, false)
}

type seriesRow struct {
	sourceID, title string
	year, tmdbID    int
}

func (s *Service) seriesRow(ctx context.Context, seriesID int64) (seriesRow, error) {
	var r seriesRow
	var matched bool
	err := s.DB.QueryRowContext(ctx, `SELECT source_id, title, COALESCE(year,0), COALESCE(tmdb_id,0), match_state='matched'
		FROM vod_series WHERE id=?`, seriesID).Scan(&r.sourceID, &r.title, &r.year, &r.tmdbID, &matched)
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	if !matched {
		r.tmdbID = 0
	}
	return r, err
}

type episodeKey struct {
	id              int64
	season, episode int
}

// episodeMatches compares episodes with the library's. sync checks unknown qualities now
// (one episode's save); otherwise they're checked in the background.
func (s *Service) episodeMatches(ctx context.Context, seriesID int64, eps []episodeKey, sync bool) (map[int64]Match, error) {
	r, err := s.seriesRow(ctx, seriesID)
	if err != nil {
		return nil, err
	}
	name, year := libraryTitle(r.title, r.year)
	copies, err := s.EpisodeCopies(ctx, r.tmdbID, name, year)
	if err != nil {
		return nil, err
	}
	stored, err := s.VOD.EpisodeQualities(ctx, seriesID)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]Match, len(eps))
	for _, e := range eps {
		lib := copies[[2]int{e.season, e.episode}]
		var sp *vod.Quality
		if q, ok := stored[e.id]; ok {
			sp = &q
		}
		if lib == nil {
			q := Quality{}
			if sp != nil && sp.Error == "" {
				q = toQuality(*sp)
			}
			out[e.id] = match(nil, q, q.Known(), false, "")
			continue
		}
		q, known, pending, note := s.vodQuality(ctx, qualityKey{true, e.id}, sp, sync)
		out[e.id] = match(lib, q, known, pending, note)
	}
	return out, nil
}

// EpisodeMatches is how each of a series' episodes compares with the library, by episode id.
func (s *Service) EpisodeMatches(ctx context.Context, seriesID int64) (map[int64]Match, error) {
	rows, err := s.DB.QueryContext(ctx, "SELECT id, season, episode FROM vod_episodes WHERE series_id=?", seriesID)
	if err != nil {
		return nil, err
	}
	var eps []episodeKey
	for rows.Next() {
		var e episodeKey
		rows.Scan(&e.id, &e.season, &e.episode)
		eps = append(eps, e)
	}
	rows.Close()
	return s.episodeMatches(ctx, seriesID, eps, false)
}

// Rollup counts a season's or show's episodes by verdict.
type Rollup struct {
	None, Better, Same, Worse int
	Pending                   int // being checked (counted as same meanwhile)
}

func (r *Rollup) Add(m Match) {
	switch m.Verdict {
	case VerdictNone:
		r.None++
	case VerdictBetter:
		r.Better++
	case VerdictWorse:
		r.Worse++
	default:
		r.Same++
	}
	if m.Pending && !m.VODKnown {
		r.Pending++
	}
}

// InLibraryError refuses saving a title the library already has as well or better, or
// one that would replace the library's copy when an upgrade wasn't asked for.
type InLibraryError struct {
	Match Match
}

func (e *InLibraryError) Error() string {
	if e.Match.Verdict == VerdictBetter {
		return fmt.Sprintf("Already in the library (%s); this copy is better (%s): save it as an upgrade to replace the library's",
			e.Match.Library.Quality.Summary(), e.Match.VOD.Summary())
	}
	return "Already in the library: " + e.Match.Reason
}
