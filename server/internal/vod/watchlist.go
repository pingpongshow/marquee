package vod

import (
	"context"
	"time"
)

// WatchRef is a film or series on someone's watchlist (USER-8).
type WatchRef struct {
	Series  bool
	ID      int64
	AddedAt time.Time
}

// addedFormat has milliseconds, fixed width, so the text sorts in time order (and adds in one second
// keep their order).
const addedFormat = "2006-01-02T15:04:05.000Z"

func watchKind(series bool) string {
	if series {
		return "series"
	}
	return "movie"
}

// SetWatchlisted puts a film or series on the person's watchlist, or takes it off. Adding one
// that's there keeps when it was first added.
func (s *Service) SetWatchlisted(ctx context.Context, userID int64, series bool, id int64, on bool) error {
	if !on {
		_, err := s.DB.ExecContext(ctx, "DELETE FROM vod_watchlist WHERE user_id=? AND kind=? AND vod_id=?", userID, watchKind(series), id)
		return err
	}
	_, err := s.DB.ExecContext(ctx, `INSERT INTO vod_watchlist(user_id, kind, vod_id, added_at) VALUES(?,?,?,?)
		ON CONFLICT(user_id, kind, vod_id) DO NOTHING`, userID, watchKind(series), id, time.Now().UTC().Format(addedFormat))
	return err
}

// Watchlisted reports whether a film or series is on the person's watchlist.
func (s *Service) Watchlisted(ctx context.Context, userID int64, series bool, id int64) (bool, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM vod_watchlist WHERE user_id=? AND kind=? AND vod_id=?",
		userID, watchKind(series), id).Scan(&n)
	return n > 0, err
}

// Watchlist is the person's films and series on demand, most recently added first: only those
// the sources still list, and no film they've watched (like the library's, a film leaves its
// watchlist once watched; a series stays).
func (s *Service) Watchlist(ctx context.Context, userID int64, limit int) ([]WatchRef, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT w.kind, w.vod_id, w.added_at FROM vod_watchlist w
		LEFT JOIN vod_movies m ON w.kind='movie' AND m.id=w.vod_id
		LEFT JOIN vod_progress p ON w.kind='movie' AND p.movie_id=w.vod_id AND p.user_id=w.user_id
		LEFT JOIN vod_series r ON w.kind='series' AND r.id=w.vod_id
		WHERE w.user_id=? AND (m.id IS NOT NULL OR r.id IS NOT NULL) AND COALESCE(p.watched, 0)=0
		ORDER BY w.added_at DESC, w.rowid DESC LIMIT ?`, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []WatchRef
	for rows.Next() {
		var kind, added string
		var r WatchRef
		if err := rows.Scan(&kind, &r.ID, &added); err != nil {
			return nil, err
		}
		r.Series = kind == "series"
		r.AddedAt, _ = time.Parse(time.RFC3339, added)
		out = append(out, r)
	}
	return out, rows.Err()
}
