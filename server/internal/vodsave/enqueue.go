package vodsave

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"marquee/internal/vod"
)

// Result is what a save request did.
type Result struct {
	Queued   int // new saves
	Already  int // already queued or saved
	Skipped  int // already in the library (as well or better, or better without upgrades asked for)
	Upgrades int // of Queued, upgrades replacing the library's copy
	Saves    []Save
}

type item struct {
	sourceID, kind, sourceKey, file, title string
	vodID, seriesID                        int64
	tmdbID                                 int
	expectedMS                             int64
	// naming
	path            string
	season, episode int
	// replaceFileID: an upgrade replaces this library file.
	replaceFileID int64
	// The save folder chosen ("" = the default one, and for upgrades).
	destName, destFolder string
}

// SaveMovie queues a film. One the library has as well or better is refused
// (*InLibraryError); one better than the library's copy is saved as an upgrade replacing it
// when upgrade is set, else refused too.
func (s *Service) SaveMovie(ctx context.Context, movieID, userID int64, upgrade bool) (Result, error) {
	return s.SaveMovieTo(ctx, movieID, userID, upgrade, "")
}

// SaveMovieTo is SaveMovie into a save folder from the settings (dest, its id; "" = the
// default movies folder), which must take films (ErrDestination). The library is checked
// the same; an upgrade replaces the library's copy where it is, whatever dest is.
func (s *Service) SaveMovieTo(ctx context.Context, movieID, userID int64, upgrade bool, dest string) (Result, error) {
	s.init()
	c := s.cfg()
	root, d, err := s.destination(dest, "movie")
	if err != nil {
		return Result{}, err
	}
	r, err := s.movieRow(ctx, movieID)
	if err != nil {
		return Result{}, err
	}
	if !s.VOD.Savable(r.sourceID) || !strings.HasPrefix(r.key, "xtream:") {
		return Result{}, ErrNotSavable
	}
	m, err := s.movieMatch(ctx, movieID, r, true)
	if err != nil {
		return Result{}, err
	}
	if !m.Savable() || (m.Verdict == VerdictBetter && !upgrade) {
		return Result{}, &InLibraryError{m}
	}
	it := item{sourceID: r.sourceID, file: r.file, kind: "movie", sourceKey: strings.TrimPrefix(r.key, "xtream:"), vodID: movieID}
	name, year := libraryTitle(r.title, r.year)
	if r.tmdbID > 0 {
		it.tmdbID = r.tmdbID
		if s.Titles != nil {
			tctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			if m, err := s.Titles.MovieByID(tctx, r.tmdbID); err == nil && m != nil && m.Title != "" {
				name = m.Title
				if len(m.ReleaseDate) >= 4 {
					if y, err := strconv.Atoi(m.ReleaseDate[:4]); err == nil {
						year = y
					}
				}
			}
			cancel()
		}
	} else {
		// TMDB's runtime is the film's, not this file's, so only the source's is checked.
		it.expectedMS = r.runtime
	}
	it.title = titleWithYear(name, year)
	n := Naming{Kind: "movie", Root: root, Title: name, Year: year,
		Ext: outputExt(filepath.Ext(it.file), c.Quality)}
	if m.Verdict == VerdictBetter {
		// An upgrade goes where the library's copy is, which it replaces.
		n.Folder, it.replaceFileID = filepath.Dir(m.Library.Path), m.Library.FileID
	} else {
		n.Folder = findFolder(ctx, s.DB, "movie", root, name, year, it.tmdbID, dest == "")
		it.destName, it.destFolder = d.Name, d.Folder
	}
	it.path = n.Path()
	return s.enqueue(ctx, []item{it}, userID)
}

// SaveEpisode queues one episode.
// Like a film, one the library has is refused, or saved as an upgrade (see SaveMovie).
func (s *Service) SaveEpisode(ctx context.Context, episodeID, userID int64, upgrade bool) (Result, error) {
	return s.SaveEpisodeTo(ctx, episodeID, userID, upgrade, "")
}

// SaveEpisodeTo is SaveEpisode into a save folder from the settings (see SaveMovieTo).
func (s *Service) SaveEpisodeTo(ctx context.Context, episodeID, userID int64, upgrade bool, dest string) (Result, error) {
	if _, _, err := s.destination(dest, "episode"); err != nil {
		return Result{}, err
	}
	var seriesID int64
	err := s.DB.QueryRowContext(ctx, "SELECT series_id FROM vod_episodes WHERE id=?", episodeID).Scan(&seriesID)
	if errors.Is(err, sql.ErrNoRows) {
		return Result{}, ErrNotFound
	}
	if err != nil {
		return Result{}, err
	}
	return s.saveEpisodes(ctx, seriesID, nil, episodeID, userID, upgrade, dest)
}

// SaveSeries queues a series' episodes, or one season's (season non-nil). The episodes are
// fetched from the source first when they haven't been. Only episodes the library doesn't
// have are queued, and (with upgrades) those better than the library's copies, replacing
// them; the others count as skipped.
func (s *Service) SaveSeries(ctx context.Context, seriesID int64, season *int, userID int64, upgrades bool) (Result, error) {
	return s.SaveSeriesTo(ctx, seriesID, season, userID, upgrades, "")
}

// SaveSeriesTo is SaveSeries into a save folder from the settings (see SaveMovieTo).
func (s *Service) SaveSeriesTo(ctx context.Context, seriesID int64, season *int, userID int64, upgrades bool, dest string) (Result, error) {
	if _, _, err := s.destination(dest, "episode"); err != nil {
		return Result{}, err
	}
	if err := s.VOD.LoadEpisodes(ctx, seriesID, false); err != nil {
		if errors.Is(err, vod.ErrNotFound) {
			return Result{}, ErrNotFound
		}
		return Result{}, err
	}
	return s.saveEpisodes(ctx, seriesID, season, 0, userID, upgrades, dest)
}

func (s *Service) saveEpisodes(ctx context.Context, seriesID int64, season *int, episodeID, userID int64, upgrade bool, dest string) (Result, error) {
	s.init()
	c := s.cfg()
	root, d, err := s.destination(dest, "episode")
	if err != nil {
		return Result{}, err
	}
	var srcID, key, title string
	var year, tmdbID int
	var matched bool
	err = s.DB.QueryRowContext(ctx, `SELECT source_id, source_key, title, COALESCE(year,0), COALESCE(tmdb_id,0), match_state='matched'
		FROM vod_series WHERE id=?`, seriesID).Scan(&srcID, &key, &title, &year, &tmdbID, &matched)
	if errors.Is(err, sql.ErrNoRows) {
		return Result{}, ErrNotFound
	}
	if err != nil {
		return Result{}, err
	}
	if !s.VOD.Savable(srcID) {
		return Result{}, ErrNotSavable
	}
	show, y := CleanTitle(title)
	if year == 0 {
		year = y
	}
	if !matched {
		tmdbID = 0
	}
	if tmdbID > 0 && s.Titles != nil {
		tctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		if t, err := s.Titles.TVByID(tctx, tmdbID); err == nil && t != nil && t.Name != "" {
			show = t.Name
			if len(t.FirstAirDate) >= 4 {
				if y, err := strconv.Atoi(t.FirstAirDate[:4]); err == nil {
					year = y
				}
			}
		}
		cancel()
	}
	folder := findFolder(ctx, s.DB, "show", root, show, year, tmdbID, dest == "")
	q := "SELECT id, source_key, season, episode, title, stream_url, runtime_ms FROM vod_episodes WHERE series_id=?"
	args := []any{seriesID}
	if season != nil {
		q += " AND season=?"
		args = append(args, *season)
	}
	if episodeID > 0 {
		q += " AND id=?"
		args = append(args, episodeID)
	}
	rows, err := s.DB.QueryContext(ctx, q+" ORDER BY season=0, season, episode, id", args...)
	if err != nil {
		return Result{}, err
	}
	var items []item
	for rows.Next() {
		var it item
		var etitle string
		var runtime int64
		if err := rows.Scan(&it.vodID, &it.sourceKey, &it.season, &it.episode, &etitle, &it.file, &runtime); err != nil {
			rows.Close()
			return Result{}, err
		}
		it.kind, it.sourceID, it.seriesID, it.tmdbID = "episode", srcID, seriesID, tmdbID
		it.destName, it.destFolder = d.Name, d.Folder
		it.expectedMS = runtime // the source's, or TMDB's when the source had none (checked loosely)
		if strings.HasPrefix(etitle, "Episode ") && etitle == fmt.Sprintf("Episode %d", it.episode) {
			etitle = "" // the placeholder for an episode without a name
		}
		etitle, _ = CleanTitle(etitle)
		it.title = fmt.Sprintf("%s - S%02dE%02d", show, it.season, it.episode)
		if etitle != "" {
			it.title += " - " + etitle
		}
		it.path = Naming{Kind: "episode", Root: root, Folder: folder, Show: show, Year: year, Season: it.season,
			Episode: it.episode, Title: etitle, Ext: outputExt(filepath.Ext(it.file), c.Quality)}.Path()
		items = append(items, it)
	}
	rows.Close()
	if len(items) == 0 {
		if episodeID > 0 {
			return Result{}, ErrNotFound
		}
		return Result{}, errNoEpisodes
	}
	// Compared with the library: one episode is checked now; a season's or show's use what's
	// known (unknown ones are checked in the background and left out for now).
	keys := make([]episodeKey, len(items))
	for i, it := range items {
		keys[i] = episodeKey{it.vodID, it.season, it.episode}
	}
	matches, err := s.episodeMatches(ctx, seriesID, keys, episodeID > 0)
	if err != nil {
		return Result{}, err
	}
	keep := items[:0]
	refused := 0
	for _, it := range items {
		m := matches[it.vodID]
		switch {
		case m.Verdict == VerdictBetter && upgrade:
			// An upgrade goes where the library's copy is, which it replaces.
			it.path = filepath.Join(filepath.Dir(m.Library.Path), filepath.Base(it.path))
			it.replaceFileID = m.Library.FileID
			it.destName, it.destFolder = "", ""
		case m.Verdict != VerdictNone:
			if episodeID > 0 {
				return Result{}, &InLibraryError{m}
			}
			refused++
			continue
		}
		keep = append(keep, it)
	}
	res, err := s.enqueue(ctx, keep, userID)
	res.Skipped += refused
	return res, err
}

// enqueue adds saves, leaving out what is already queued or saved. A failed save of the
// same item is queued again (it continues from what it had).
func (s *Service) enqueue(ctx context.Context, items []item, userID int64) (Result, error) {
	c := s.cfg()
	var res Result
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return res, err
	}
	defer tx.Rollback()
	var pos int64
	tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(queue_pos),0) FROM vod_saves").Scan(&pos)
	var ids []int64
	for _, it := range items {
		var id int64
		var state, oldPath string
		err := tx.QueryRowContext(ctx, `SELECT id, state, target_path FROM vod_saves WHERE source_id=? AND kind=? AND source_key=?
			ORDER BY id DESC LIMIT 1`, it.sourceID, it.kind, it.sourceKey).Scan(&id, &state, &oldPath)
		switch {
		case err == nil && (state == Failed):
			pos++
			var replace any
			if it.replaceFileID > 0 {
				replace = it.replaceFileID
			}
			if oldPath != it.path {
				removeParts(oldPath) // it goes elsewhere now (an upgrade, or the library's copy went)
			}
			if _, err := tx.ExecContext(ctx, `UPDATE vod_saves SET state=?, attempts=0, verify_tries=0, next_attempt_at=NULL,
				error='', finished_at=NULL, queue_pos=?, target_path=?, replace_file_id=?, destination_name=?, destination_folder=? WHERE id=?`,
				Queued, pos, it.path, replace, it.destName, it.destFolder, id); err != nil {
				return res, err
			}
			res.Queued++
			if it.replaceFileID > 0 {
				res.Upgrades++
			}
			ids = append(ids, id)
			continue
		case err == nil && state != Cancelled && state != Skipped:
			res.Already++
			ids = append(ids, id)
			continue
		case err != nil && !errors.Is(err, sql.ErrNoRows):
			return res, err
		}
		state, msg, finished := Queued, "", any(nil)
		if c.SkipExisting && it.replaceFileID == 0 {
			if f := existingFile(it.path, it.kind, it.season, it.episode); f != "" {
				state, msg = Skipped, "Already saved: "+filepath.Base(f)
			}
		}
		if state == Skipped {
			finished = now()
			res.Skipped++
		} else {
			res.Queued++
			if it.replaceFileID > 0 {
				res.Upgrades++
			}
		}
		pos++
		var tmdbID, seriesID, by, replace any
		if it.tmdbID > 0 {
			tmdbID = it.tmdbID
		}
		if it.seriesID > 0 {
			seriesID = it.seriesID
		}
		if userID > 0 {
			by = userID
		}
		if it.replaceFileID > 0 {
			replace = it.replaceFileID
		}
		r, err := tx.ExecContext(ctx, `INSERT INTO vod_saves(source_id, kind, vod_id, series_id, source_key, file, title, tmdb_id,
			expected_ms, target_path, quality, state, queue_pos, error, created_at, finished_at, requested_by, replace_file_id,
			destination_name, destination_folder)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, it.sourceID, it.kind, it.vodID, seriesID, it.sourceKey, it.file, it.title,
			tmdbID, it.expectedMS, it.path, c.Quality, state, pos, msg, now(), finished, by, replace, it.destName, it.destFolder)
		if err != nil {
			return res, err
		}
		id, _ = r.LastInsertId()
		ids = append(ids, id)
	}
	if err := tx.Commit(); err != nil {
		return res, err
	}
	for _, id := range ids {
		if v, err := s.one(ctx, id); err == nil {
			res.Saves = append(res.Saves, v)
		}
	}
	s.Wake()
	return res, nil
}
