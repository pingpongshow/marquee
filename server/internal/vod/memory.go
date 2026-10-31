package vod

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"strconv"
	"strings"
)

// What each person watched and rated on demand, kept by the title (vod_memory) rather than the
// source's row: a film or series leaving its source takes its row, progress and watched marks
// with it, but not this, so ratings and what someone watched keep shaping their suggestions,
// and a title that comes back (under any id or source) has its rating again.

// Memory is one film or series someone watched or rated on demand.
type Memory struct {
	Kind     string // movie, series
	TMDBID   int
	Title    string
	Year     int
	Genres   []string
	Keywords []string
	Recs     []int   // TMDB's recommendations, best first
	Rating   float64 // 0–10, 0 = none
	Finished bool    // a film watched to the end
	Episodes int     // a series' episodes watched
	Total    int     // a series' episodes, when last seen
}

// ErrBadRating is a rating outside 0–10.
var ErrBadRating = errors.New("a rating is from 0 to 10")

// ident is how vod_memory knows a title: its TMDB id once matched, else its title and year.
func ident(tmdb int64, title string, year int) string {
	if tmdb > 0 {
		return "tmdb:" + strconv.FormatInt(tmdb, 10)
	}
	return "title:" + titleKey(title) + "|" + strconv.Itoa(year)
}

// titleIdent is the title-and-year ident, which a title had before it was matched.
func titleIdent(title string, year int) string { return ident(0, title, year) }

type vodRow struct {
	title, genres  string
	keywords, recs sql.NullString
	year           int
	tmdb           int64
}

func (s *Service) vodRow(ctx context.Context, kind string, id int64) (vodRow, error) {
	table := "vod_movies"
	if kind == "series" {
		table = "vod_series"
	}
	var r vodRow
	err := s.DB.QueryRowContext(ctx, `SELECT title, COALESCE(year,0), genres, keywords, tmdb_recs,
		CASE WHEN match_state='matched' THEN COALESCE(tmdb_id,0) ELSE 0 END FROM `+table+` WHERE id=?`, id).
		Scan(&r.title, &r.year, &r.genres, &r.keywords, &r.recs, &r.tmdb)
	return r, err
}

// snapshot makes sure uid's memory of a film or series exists and holds what's known about it
// now (a title matched since takes its title-and-year memory along), returning its ident.
func (s *Service) snapshot(ctx context.Context, uid int64, kind string, id int64) (string, error) {
	r, err := s.vodRow(ctx, kind, id)
	if err != nil {
		return "", err
	}
	key := ident(r.tmdb, r.title, r.year)
	if r.tmdb > 0 {
		s.DB.ExecContext(ctx, `UPDATE OR IGNORE vod_memory SET ident=?, tmdb_id=? WHERE user_id=? AND kind=? AND ident=?`,
			key, r.tmdb, uid, kind, titleIdent(r.title, r.year))
	}
	var tmdb any
	if r.tmdb > 0 {
		tmdb = r.tmdb
	}
	var year any
	if r.year > 0 {
		year = r.year
	}
	_, err = s.DB.ExecContext(ctx, `INSERT INTO vod_memory(user_id, kind, ident, tmdb_id, title, year, genres, keywords, tmdb_recs, updated_at)
		VALUES(?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(user_id, kind, ident) DO UPDATE SET tmdb_id=COALESCE(excluded.tmdb_id, tmdb_id), title=excluded.title,
			year=COALESCE(excluded.year, year), genres=excluded.genres, keywords=COALESCE(excluded.keywords, keywords),
			tmdb_recs=COALESCE(excluded.tmdb_recs, tmdb_recs), updated_at=excluded.updated_at`,
		uid, kind, key, tmdb, r.title, year, r.genres, r.keywords, r.recs, now())
	return key, err
}

// rememberFilm records whether uid finished a film.
func (s *Service) rememberFilm(ctx context.Context, uid, movieID int64, finished bool) error {
	key, err := s.snapshot(ctx, uid, "movie", movieID)
	if err != nil {
		return err
	}
	if finished {
		_, err = s.DB.ExecContext(ctx, `UPDATE vod_memory SET finished=1, watched_at=? WHERE user_id=? AND kind='movie' AND ident=?`, now(), uid, key)
	} else {
		_, err = s.DB.ExecContext(ctx, `UPDATE vod_memory SET finished=0 WHERE user_id=? AND kind='movie' AND ident=?`, uid, key)
	}
	return err
}

// rememberSeries records how many of a series' episodes uid has watched.
func (s *Service) rememberSeries(ctx context.Context, uid, seriesID int64) error {
	key, err := s.snapshot(ctx, uid, "series", seriesID)
	if err != nil {
		return err
	}
	_, err = s.DB.ExecContext(ctx, `UPDATE vod_memory SET
		episodes=(SELECT COUNT(*) FROM vod_episode_progress WHERE user_id=? AND series_id=? AND watched=1),
		total=MAX(total, (SELECT COUNT(*) FROM vod_episodes WHERE series_id=?)),
		watched_at=COALESCE((SELECT MAX(updated_at) FROM vod_episode_progress WHERE user_id=? AND series_id=? AND watched=1), watched_at)
		WHERE user_id=? AND kind='series' AND ident=?`, uid, seriesID, seriesID, uid, seriesID, uid, key)
	return err
}

// remember records a change to a film or series' watched state; a failure is only logged
// (the watched mark itself was saved).
func (s *Service) remember(ctx context.Context, f func() error) {
	if err := f(); err != nil && !errors.Is(err, sql.ErrNoRows) {
		slog.WarnContext(ctx, "vod memory", "err", err)
	}
}

// SetRating sets uid's rating of a film or series (kind movie or series), 0–10 in half stars;
// nil clears it.
func (s *Service) SetRating(ctx context.Context, uid int64, kind string, id int64, rating *float64) error {
	if rating != nil && (*rating < 0 || *rating > 10) {
		return ErrBadRating
	}
	key, err := s.snapshot(ctx, uid, kind, id)
	if err != nil {
		return err
	}
	if rating == nil || *rating == 0 {
		_, err = s.DB.ExecContext(ctx, `UPDATE vod_memory SET rating=NULL, rated_at=? WHERE user_id=? AND kind=? AND ident=?`, now(), uid, kind, key)
	} else {
		_, err = s.DB.ExecContext(ctx, `UPDATE vod_memory SET rating=?, rated_at=? WHERE user_id=? AND kind=? AND ident=?`, *rating, now(), uid, kind, key)
	}
	return err
}

// Rating is uid's rating of a film or series, 0 when none (found by its TMDB id, or by its
// title and year when it isn't matched or was rated before it was).
func (s *Service) Rating(ctx context.Context, uid int64, kind string, id int64) (float64, error) {
	r, err := s.vodRow(ctx, kind, id)
	if err != nil {
		return 0, err
	}
	var v sql.NullFloat64
	err = s.DB.QueryRowContext(ctx, `SELECT rating FROM vod_memory WHERE user_id=? AND kind=? AND ident IN (?, ?) AND rating IS NOT NULL
		ORDER BY ident LIKE 'tmdb:%' DESC LIMIT 1`, uid, kind, ident(r.tmdb, r.title, r.year), titleIdent(r.title, r.year)).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return v.Float64, err
}

// Memories is everything uid watched or rated on demand, most recent first.
func Memories(ctx context.Context, db *sql.DB, uid int64) ([]Memory, error) {
	rows, err := db.QueryContext(ctx, `SELECT kind, COALESCE(tmdb_id,0), title, COALESCE(year,0), COALESCE(genres,'[]'),
		COALESCE(keywords,''), COALESCE(tmdb_recs,''), COALESCE(rating,0), finished, episodes, total
		FROM vod_memory WHERE user_id=? AND (rating IS NOT NULL OR finished=1 OR episodes>0)
		ORDER BY MAX(COALESCE(watched_at,''), COALESCE(rated_at,'')) DESC`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Memory
	for rows.Next() {
		var m Memory
		var genres, keywords, recs string
		if err := rows.Scan(&m.Kind, &m.TMDBID, &m.Title, &m.Year, &genres, &keywords, &recs, &m.Rating, &m.Finished,
			&m.Episodes, &m.Total); err != nil {
			return nil, err
		}
		json.Unmarshal([]byte(genres), &m.Genres)
		if keywords != "" {
			json.Unmarshal([]byte(keywords), &m.Keywords)
		}
		if recs != "" {
			json.Unmarshal([]byte(recs), &m.Recs)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// MemoryVersion changes whenever uid's memory does (a rating, a film or episode watched).
func MemoryVersion(ctx context.Context, db *sql.DB, uid int64) string {
	var v sql.NullString
	db.QueryRowContext(ctx, `SELECT MAX(updated_at) || '/' || COUNT(*) || '/' || COALESCE(SUM(rating),0) || '/' || SUM(finished) || '/' || SUM(episodes)
		FROM vod_memory WHERE user_id=?`, uid).Scan(&v)
	return v.String
}

// fillMemory keeps the memories up to date with what's known about their titles while they're
// on demand: TMDB's recommendations and keywords arrive after the match, sometimes after
// someone watched it. Run after each refresh.
func (s *Service) fillMemory(ctx context.Context) {
	for _, t := range []struct{ kind, table string }{{"movie", "vod_movies"}, {"series", "vod_series"}} {
		s.DB.ExecContext(ctx, `UPDATE vod_memory SET
			tmdb_recs=COALESCE(tmdb_recs, (SELECT v.tmdb_recs FROM `+t.table+` v WHERE v.tmdb_id=vod_memory.tmdb_id AND v.match_state='matched' AND v.tmdb_recs IS NOT NULL LIMIT 1)),
			keywords=COALESCE(keywords, (SELECT v.keywords FROM `+t.table+` v WHERE v.tmdb_id=vod_memory.tmdb_id AND v.match_state='matched' AND v.keywords IS NOT NULL LIMIT 1))
			WHERE kind=? AND tmdb_id IS NOT NULL AND (tmdb_recs IS NULL OR keywords IS NULL)`, t.kind)
	}
}

// BackfillMemory remembers what was watched before the memory existed (films finished, series
// with watched episodes), once: titles already remembered are left alone.
func (s *Service) BackfillMemory(ctx context.Context) {
	type pair struct{ uid, id int64 }
	collect := func(q string) []pair {
		rows, err := s.DB.QueryContext(ctx, q)
		if err != nil {
			return nil
		}
		defer rows.Close()
		var out []pair
		for rows.Next() {
			var p pair
			if rows.Scan(&p.uid, &p.id) == nil {
				out = append(out, p)
			}
		}
		return out
	}
	films := collect(`SELECT p.user_id, p.movie_id FROM vod_progress p WHERE p.watched=1`)
	shows := collect(`SELECT DISTINCT user_id, series_id FROM vod_episode_progress WHERE watched=1`)
	known := func(uid int64, kind string, id int64) bool {
		r, err := s.vodRow(ctx, kind, id)
		if err != nil {
			return true
		}
		var one int
		return s.DB.QueryRowContext(ctx, `SELECT 1 FROM vod_memory WHERE user_id=? AND kind=? AND ident IN (?, ?)`,
			uid, kind, ident(r.tmdb, r.title, r.year), titleIdent(r.title, r.year)).Scan(&one) == nil
	}
	n := 0
	for _, p := range films {
		if !known(p.uid, "movie", p.id) {
			s.remember(ctx, func() error { return s.rememberFilm(ctx, p.uid, p.id, true) })
			n++
		}
	}
	for _, p := range shows {
		if !known(p.uid, "series", p.id) {
			s.remember(ctx, func() error { return s.rememberSeries(ctx, p.uid, p.id) })
			n++
		}
	}
	if n > 0 {
		slog.InfoContext(ctx, "vod memory: remembered what was watched before", "titles", n)
	}
	s.fillMemory(ctx)
}

// GenresLower is a memory's genres in lower case, as suggestions compare them.
func (m Memory) GenresLower() []string {
	out := make([]string, len(m.Genres))
	for i, g := range m.Genres {
		out[i] = strings.ToLower(g)
	}
	return out
}
