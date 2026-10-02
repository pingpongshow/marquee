package metadata

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"marquee/internal/metadata/omdb"
)

// ratingsBudget tracks OMDb requests per UTC day so the free tier (1,000/day) is never exceeded.
type ratingsBudget struct {
	mu    sync.Mutex
	day   string
	used  int
	limit bool // OMDb said the limit is reached today
}

func (b *ratingsBudget) take(max int) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	today := time.Now().UTC().Format("2006-01-02")
	if b.day != today {
		b.day, b.used, b.limit = today, 0, false
	}
	if b.limit || b.used >= max {
		return false
	}
	b.used++
	return true
}

func (b *ratingsBudget) exhausted() {
	b.mu.Lock()
	b.limit = true
	b.mu.Unlock()
}

// RefreshRatings fetches OMDb ratings for matched movies and shows that have an IMDb id and
// no ratings yet (or ratings older than 30 days), within today's request budget. It is a
// no-op without an OMDb key.
func (s *Service) RefreshRatings(ctx context.Context, libID int64) error {
	cfg := s.Settings.Get().Metadata
	if cfg.OMDbAPIKey == "" {
		return nil
	}
	c := omdb.New(cfg.OMDbAPIKey)
	rows, err := s.DB.QueryContext(ctx, `SELECT i.id, e.value FROM items i JOIN external_ids e ON e.item_id = i.id AND e.provider = 'imdb'
		WHERE i.library_id = ? AND i.type IN ('movie', 'show')
		  AND (i.ratings_refreshed_at IS NULL OR i.ratings_refreshed_at < strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '-30 days'))
		ORDER BY i.ratings_refreshed_at IS NOT NULL, i.added_at DESC`, libID)
	if err != nil {
		return err
	}
	type todo struct {
		id   int64
		imdb string
	}
	var list []todo
	for rows.Next() {
		var t todo
		rows.Scan(&t.id, &t.imdb)
		list = append(list, t)
	}
	rows.Close()

	done := 0
	for _, t := range list {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !s.omdbBudget.take(cfg.OMDbDailyLimit) {
			break
		}
		r, err := c.ByIMDbID(ctx, t.imdb)
		switch {
		case errors.Is(err, omdb.ErrLimit):
			s.omdbBudget.exhausted()
			slog.Info("OMDb daily limit reached; remaining ratings will be fetched tomorrow")
			return nil
		case errors.Is(err, omdb.ErrInvalidKey):
			return err
		case errors.Is(err, omdb.ErrNotFound):
			s.DB.ExecContext(ctx, `UPDATE items SET ratings_refreshed_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = ?`, t.id)
			continue
		case err != nil:
			slog.Warn("OMDb lookup failed", "imdb", t.imdb, "err", err)
			continue
		}
		if _, err := s.DB.ExecContext(ctx, `UPDATE items SET imdb_rating = ?, imdb_votes = ?, rt_critic = ?, metacritic = ?,
			ratings_refreshed_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = ?`,
			nullFloat(r.IMDb), nullZero(r.IMDbVotes), nullNeg(r.RTCritic), nullNeg(r.Metacritic), t.id); err != nil {
			return err
		}
		done++
	}
	if done > 0 {
		slog.Info("OMDb ratings updated", "library", libID, "items", done, "remaining", len(list)-done)
	}
	return nil
}

func nullFloat(f float64) any {
	if f == 0 {
		return nil
	}
	return f
}

func nullNeg(n int) any {
	if n < 0 {
		return nil
	}
	return n
}
