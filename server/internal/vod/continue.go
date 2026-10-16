package vod

import (
	"context"
	"database/sql"
	"errors"
	"time"
)

// Films and episodes on demand in Home's Continue Watching, beside the library's.

// InProgress is a film or an episode the user stopped part-way through.
type InProgress struct {
	Movie   *Movie   // a film
	Episode *Episode // or an episode
	// SeriesHasPoster: the episode's series has a poster.
	SeriesHasPoster bool
	PositionMS      int64
	DurationMS      int64
	UpdatedAt       time.Time
}

// ContinueWatchingAll is every film and episode the user stopped part-way through (one
// episode per series, the latest), newest first, leaving out what they removed from
// Continue Watching since (until they watch it again) and what's rated above maxRating.
func (s *Service) ContinueWatchingAll(ctx context.Context, userID int64, maxRating string) ([]InProgress, error) {
	where, args := ratingClause(maxRating)
	rows, err := s.DB.QueryContext(ctx, "SELECT "+movieCols+`, p.updated_at FROM vod_movies m
		JOIN vod_progress p ON p.movie_id=m.id AND p.user_id=?
		WHERE p.position_ms>0 AND p.watched=0 AND `+where+`
		AND NOT EXISTS (SELECT 1 FROM vod_continue_hidden h WHERE h.user_id=p.user_id AND h.movie_id=m.id AND h.hidden_at>=p.updated_at)
		ORDER BY p.updated_at DESC`, append([]any{userID}, args...)...)
	if err != nil {
		return nil, err
	}
	var out []InProgress
	for rows.Next() {
		var at string
		var m Movie
		var genres, cast, added string
		if err := rows.Scan(&m.ID, &m.Title, &m.Category, &m.Year, &m.Overview, &m.Tagline, &genres, &m.ContentRating,
			&m.Rating, &m.RuntimeMS, &m.Director, &cast, &m.HasPoster, &m.HasBackdrop,
			&m.TMDBID, &m.IMDBID, &m.Matched, &m.PositionMS, &m.Watched, &m.StreamURL, &m.SourceID, &added, &at); err != nil {
			rows.Close()
			return nil, err
		}
		t, _ := time.Parse(time.RFC3339, at)
		out = append(out, InProgress{Movie: &m, PositionMS: m.PositionMS, DurationMS: m.RuntimeMS, UpdatedAt: t})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, err = s.DB.QueryContext(ctx, "SELECT "+episodeCols+`, (m.poster_url<>'' OR m.logo_url<>''), p.updated_at
		FROM vod_episodes e JOIN vod_series m ON m.id=e.series_id
		JOIN vod_episode_progress p ON p.episode_id=e.id AND p.user_id=?
		WHERE p.position_ms>0 AND p.watched=0 AND `+where+`
		AND NOT EXISTS (SELECT 1 FROM vod_continue_hidden h WHERE h.user_id=p.user_id AND h.series_id=m.id AND h.hidden_at>=p.updated_at)
		ORDER BY p.updated_at DESC`, append([]any{userID}, args...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	seen := map[int64]bool{}
	for rows.Next() {
		var e Episode
		var poster bool
		var at string
		if err := rows.Scan(&e.ID, &e.SeriesID, &e.SeriesTitle, &e.Season, &e.Episode, &e.Title, &e.Overview, &e.AirDate,
			&e.RuntimeMS, &e.Rating, &e.HasStill, &e.PositionMS, &e.Watched, &e.StreamURL, &e.SourceID, &e.ContentRating,
			&poster, &at); err != nil {
			return nil, err
		}
		if seen[e.SeriesID] {
			continue // the series' latest is already in
		}
		seen[e.SeriesID] = true
		t, _ := time.Parse(time.RFC3339, at)
		out = append(out, InProgress{Episode: &e, SeriesHasPoster: poster, PositionMS: e.PositionMS, DurationMS: e.RuntimeMS, UpdatedAt: t})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	sortInProgress(out)
	return out, nil
}

func sortInProgress(list []InProgress) {
	for i := 1; i < len(list); i++ {
		for j := i; j > 0 && list[j].UpdatedAt.After(list[j-1].UpdatedAt); j-- {
			list[j], list[j-1] = list[j-1], list[j]
		}
	}
}

// HideMovieFromContinue removes a film from the user's Continue Watching until they watch
// it again; the resume point is kept.
func (s *Service) HideMovieFromContinue(ctx context.Context, userID, movieID int64) error {
	var n int
	if err := s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM vod_movies WHERE id=?", movieID).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	return s.hide(ctx, userID, "movie_id", movieID)
}

func (s *Service) hide(ctx context.Context, userID int64, col string, id int64) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "DELETE FROM vod_continue_hidden WHERE user_id=? AND "+col+"=?", userID, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, "INSERT INTO vod_continue_hidden(user_id, "+col+", hidden_at) VALUES(?,?,?)", userID, id, now()); err != nil {
		return err
	}
	return tx.Commit()
}

// HideEpisodeFromContinue removes an episode's series from the user's Continue Watching
// until they watch one of its episodes again.
func (s *Service) HideEpisodeFromContinue(ctx context.Context, userID, episodeID int64) error {
	var seriesID int64
	err := s.DB.QueryRowContext(ctx, "SELECT series_id FROM vod_episodes WHERE id=?", episodeID).Scan(&seriesID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	return s.hide(ctx, userID, "series_id", seriesID)
}
