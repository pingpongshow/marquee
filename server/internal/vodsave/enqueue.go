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
	Queued  int // new saves
	Already int // already queued or saved
	Skipped int // already in the library
	Saves   []Save
}

type item struct {
	sourceID, kind, sourceKey, file, title string
	vodID, seriesID                        int64
	tmdbID                                 int
	expectedMS                             int64
	// naming
	path            string
	season, episode int
	inLibrary       bool
}

// SaveMovie queues a film.
func (s *Service) SaveMovie(ctx context.Context, movieID, userID int64) (Result, error) {
	s.init()
	c := s.cfg()
	if c.MoviesFolder == "" {
		return Result{}, ErrNoFolder
	}
	var it item
	var title string
	var year, tmdbID int
	var runtime int64
	var matched bool
	var key string
	err := s.DB.QueryRowContext(ctx, `SELECT source_id, source_key, stream_url, title, COALESCE(year,0), COALESCE(tmdb_id,0),
		match_state='matched', runtime_ms FROM vod_movies WHERE id=?`, movieID).
		Scan(&it.sourceID, &key, &it.file, &title, &year, &tmdbID, &matched, &runtime)
	if errors.Is(err, sql.ErrNoRows) {
		return Result{}, ErrNotFound
	}
	if err != nil {
		return Result{}, err
	}
	if !s.VOD.Savable(it.sourceID) || !strings.HasPrefix(key, "xtream:") {
		return Result{}, ErrNotSavable
	}
	it.kind, it.sourceKey, it.vodID = "movie", strings.TrimPrefix(key, "xtream:"), movieID
	name, y := CleanTitle(title)
	if year == 0 {
		year = y
	}
	if matched && tmdbID > 0 {
		it.tmdbID = tmdbID
		if s.Titles != nil {
			tctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			if m, err := s.Titles.MovieByID(tctx, tmdbID); err == nil && m != nil && m.Title != "" {
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
		it.expectedMS = runtime
	}
	it.title = titleWithYear(name, year)
	n := Naming{Kind: "movie", Root: c.MoviesFolder, Title: name, Year: year,
		Ext: outputExt(filepath.Ext(it.file), c.Quality)}
	n.Folder = FindFolder(ctx, s.DB, "movie", c.MoviesFolder, name, year, it.tmdbID)
	it.path = n.Path()
	if c.SkipExisting && it.tmdbID > 0 {
		it.inLibrary = s.movieInLibrary(ctx, it.tmdbID)
	}
	return s.enqueue(ctx, []item{it}, userID)
}

// SaveEpisode queues one episode.
func (s *Service) SaveEpisode(ctx context.Context, episodeID, userID int64) (Result, error) {
	var seriesID int64
	err := s.DB.QueryRowContext(ctx, "SELECT series_id FROM vod_episodes WHERE id=?", episodeID).Scan(&seriesID)
	if errors.Is(err, sql.ErrNoRows) {
		return Result{}, ErrNotFound
	}
	if err != nil {
		return Result{}, err
	}
	return s.saveEpisodes(ctx, seriesID, nil, episodeID, userID)
}

// SaveSeries queues a series' episodes, or one season's (season non-nil). The episodes are
// fetched from the source first when they haven't been.
func (s *Service) SaveSeries(ctx context.Context, seriesID int64, season *int, userID int64) (Result, error) {
	if err := s.VOD.LoadEpisodes(ctx, seriesID, false); err != nil {
		if errors.Is(err, vod.ErrNotFound) {
			return Result{}, ErrNotFound
		}
		return Result{}, err
	}
	return s.saveEpisodes(ctx, seriesID, season, 0, userID)
}

func (s *Service) saveEpisodes(ctx context.Context, seriesID int64, season *int, episodeID, userID int64) (Result, error) {
	s.init()
	c := s.cfg()
	if c.ShowsFolder == "" {
		return Result{}, ErrNoFolder
	}
	var srcID, key, title string
	var year, tmdbID int
	var matched bool
	err := s.DB.QueryRowContext(ctx, `SELECT source_id, source_key, title, COALESCE(year,0), COALESCE(tmdb_id,0), match_state='matched'
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
	folder := FindFolder(ctx, s.DB, "show", c.ShowsFolder, show, year, tmdbID)
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
		it.expectedMS = runtime // the source's, or TMDB's when the source had none (checked loosely)
		if strings.HasPrefix(etitle, "Episode ") && etitle == fmt.Sprintf("Episode %d", it.episode) {
			etitle = "" // the placeholder for an episode without a name
		}
		etitle, _ = CleanTitle(etitle)
		it.title = fmt.Sprintf("%s - S%02dE%02d", show, it.season, it.episode)
		if etitle != "" {
			it.title += " - " + etitle
		}
		it.path = Naming{Kind: "episode", Root: c.ShowsFolder, Folder: folder, Show: show, Year: year, Season: it.season,
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
	if c.SkipExisting && tmdbID > 0 {
		have := s.episodesInLibrary(ctx, tmdbID)
		for i := range items {
			items[i].inLibrary = have[[2]int{items[i].season, items[i].episode}]
		}
	}
	return s.enqueue(ctx, items, userID)
}

func (s *Service) movieInLibrary(ctx context.Context, tmdbID int) bool {
	var n int
	s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM external_ids x JOIN items i ON i.id=x.item_id AND i.type='movie'
		WHERE x.provider='tmdb' AND x.value=?`, strconv.Itoa(tmdbID)).Scan(&n)
	return n > 0
}

func (s *Service) episodesInLibrary(ctx context.Context, tmdbID int) map[[2]int]bool {
	out := map[[2]int]bool{}
	rows, err := s.DB.QueryContext(ctx, `SELECT z.idx, e.idx FROM external_ids x JOIN items sh ON sh.id=x.item_id AND sh.type='show'
		JOIN items e ON e.grandparent_id=sh.id AND e.type='episode' JOIN items z ON z.id=e.parent_id
		WHERE x.provider='tmdb' AND x.value=? AND z.idx IS NOT NULL AND e.idx IS NOT NULL`, strconv.Itoa(tmdbID))
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var se, ep int
		rows.Scan(&se, &ep)
		out[[2]int{se, ep}] = true
	}
	return out
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
		var state string
		err := tx.QueryRowContext(ctx, `SELECT id, state FROM vod_saves WHERE source_id=? AND kind=? AND source_key=?
			ORDER BY id DESC LIMIT 1`, it.sourceID, it.kind, it.sourceKey).Scan(&id, &state)
		switch {
		case err == nil && (state == Failed):
			pos++
			if _, err := tx.ExecContext(ctx, `UPDATE vod_saves SET state=?, attempts=0, verify_tries=0, next_attempt_at=NULL,
				error='', finished_at=NULL, queue_pos=? WHERE id=?`, Queued, pos, id); err != nil {
				return res, err
			}
			res.Queued++
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
		if c.SkipExisting {
			if it.inLibrary {
				state, msg = Skipped, "Already in the library"
			} else if f := existingFile(it.path, it.kind, it.season, it.episode); f != "" {
				state, msg = Skipped, "Already saved: "+filepath.Base(f)
			}
		}
		if state == Skipped {
			finished = now()
			res.Skipped++
		} else {
			res.Queued++
		}
		pos++
		var tmdbID, seriesID, by any
		if it.tmdbID > 0 {
			tmdbID = it.tmdbID
		}
		if it.seriesID > 0 {
			seriesID = it.seriesID
		}
		if userID > 0 {
			by = userID
		}
		r, err := tx.ExecContext(ctx, `INSERT INTO vod_saves(source_id, kind, vod_id, series_id, source_key, file, title, tmdb_id,
			expected_ms, target_path, quality, state, queue_pos, error, created_at, finished_at, requested_by)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, it.sourceID, it.kind, it.vodID, seriesID, it.sourceKey, it.file, it.title,
			tmdbID, it.expectedMS, it.path, c.Quality, state, pos, msg, now(), finished, by)
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
