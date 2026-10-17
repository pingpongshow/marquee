package vod

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sync/atomic"
	"time"

	"marquee/internal/metadata/tmdb"
)

// TMDB's recommendations, keywords and vote counts of the films and series on demand, for
// suggestions. New matches get them with their details; titles matched before they were
// kept get them from the "Fetch VOD suggestion data" task, one light request a title.

// ExtrasFetcher fetches only a title's recommendations, keywords and votes (one request:
// metadata.Service). A Matcher that is one is used for the backfill.
type ExtrasFetcher interface {
	MovieExtras(ctx context.Context, id int) (*tmdb.Movie, error)
	TVExtras(ctx context.Context, id int) (*tmdb.TVShow, error)
}

// BackfillEvery is the backfill's pace by default: 4 requests a second, well under TMDB's limits.
const BackfillEvery = 250 * time.Millisecond

// extrasTodo is a matched title without its extras.
type extrasTodo struct {
	series bool
	id     int64
	tmdb   int
}

// backfill counts the running backfill's progress.
type backfill struct {
	done, total atomic.Int64
}

// MissingExtras counts the matched films and series without TMDB's recommendations.
func (s *Service) MissingExtras(ctx context.Context) (int, error) {
	var n int
	err := s.DB.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM vod_movies WHERE match_state='matched' AND tmdb_id>0 AND tmdb_recs IS NULL)
		+ (SELECT COUNT(*) FROM vod_series WHERE match_state='matched' AND tmdb_id>0 AND tmdb_recs IS NULL)`).Scan(&n)
	return n, err
}

// BackfillProgress is the running backfill's titles done and to do.
func (s *Service) BackfillProgress() (done, total int) {
	return int(s.bf.done.Load()), int(s.bf.total.Load())
}

// BackfillExtras fetches the extras of up to limit (0 = all) matched titles without them,
// the best rated first, one request every `every` (0 = BackfillEvery), until ctx ends (the
// maintenance window closing: the rest wait for next time). It returns how many it filled.
func (s *Service) BackfillExtras(ctx context.Context, limit int, every time.Duration) (int, error) {
	fetch, ok := s.Matcher.(ExtrasFetcher)
	if !ok || ctx.Err() != nil {
		return 0, nil
	}
	if every <= 0 {
		every = BackfillEvery
	}
	q := `SELECT 0, id, tmdb_id, COALESCE(rating,0) AS r FROM vod_movies WHERE match_state='matched' AND tmdb_id>0 AND tmdb_recs IS NULL
		UNION ALL SELECT 1, id, tmdb_id, COALESCE(rating,0) FROM vod_series WHERE match_state='matched' AND tmdb_id>0 AND tmdb_recs IS NULL
		ORDER BY r DESC, 2`
	var args []any
	if limit > 0 {
		q += " LIMIT ?"
		args = append(args, limit)
	}
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return 0, err
	}
	var todo []extrasTodo
	for rows.Next() {
		var t extrasTodo
		var r float64
		if err := rows.Scan(&t.series, &t.id, &t.tmdb, &r); err != nil {
			rows.Close()
			return 0, err
		}
		todo = append(todo, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	s.bf.done.Store(0)
	s.bf.total.Store(int64(len(todo)))
	defer s.bf.total.Store(0)
	tick := time.NewTicker(every)
	defer tick.Stop()
	filled, failed := 0, 0
	for i, t := range todo {
		if i > 0 {
			select {
			case <-ctx.Done():
				return filled, nil
			case <-tick.C:
			}
		}
		if ctx.Err() != nil {
			return filled, nil
		}
		err := s.storeExtras(ctx, fetch, t)
		switch {
		case errors.Is(err, tmdb.ErrNoKey) || errors.Is(err, tmdb.ErrInvalidKey):
			return filled, err
		case ctx.Err() != nil:
			return filled, nil
		case err != nil:
			slog.Debug("vod extras", "tmdb", t.tmdb, "err", err)
			if failed++; failed >= 20 && filled == 0 {
				return filled, fmt.Errorf("TMDB isn't answering: %w", err)
			}
		default:
			filled++
		}
		s.bf.done.Store(int64(i + 1))
	}
	return filled, nil
}

// storeExtras fetches and keeps one title's extras. A title TMDB no longer has gets empty
// ones, so it isn't asked for again.
func (s *Service) storeExtras(ctx context.Context, fetch ExtrasFetcher, t extrasTodo) error {
	var recs []int
	var keywords []string
	var votes int
	if t.series {
		show, err := fetch.TVExtras(ctx, t.tmdb)
		if err != nil && !errors.Is(err, tmdb.ErrNotFound) {
			return err
		}
		if show != nil && err == nil {
			recs, keywords, votes = show.Recommendations.IDs(), show.KeywordNames(), show.VoteCount
		}
	} else {
		m, err := fetch.MovieExtras(ctx, t.tmdb)
		if err != nil && !errors.Is(err, tmdb.ErrNotFound) {
			return err
		}
		if m != nil && err == nil {
			recs, keywords, votes = m.Recommendations.IDs(), m.KeywordNames(), m.VoteCount
		}
	}
	table := "vod_movies"
	if t.series {
		table = "vod_series"
	}
	_, err := s.DB.ExecContext(ctx, "UPDATE "+table+" SET tmdb_recs=?, keywords=?, votes=? WHERE id=?", jsonList(recs), jsonList(keywords), votes, t.id)
	return err
}

// FetchTMDBExtras fills in one matched film's (or series') extras when it has none (someone
// watched it and suggestions need its recommendations now).
func (s *Service) FetchTMDBExtras(ctx context.Context, series bool, id int64) error {
	fetch, ok := s.Matcher.(ExtrasFetcher)
	if !ok {
		return nil
	}
	table := "vod_movies"
	if series {
		table = "vod_series"
	}
	var tmdbID int
	err := s.DB.QueryRowContext(ctx, "SELECT COALESCE(tmdb_id,0) FROM "+table+
		" WHERE id=? AND match_state='matched' AND tmdb_recs IS NULL", id).Scan(&tmdbID)
	if errors.Is(err, sql.ErrNoRows) || tmdbID <= 0 {
		return nil
	}
	if err != nil {
		return err
	}
	return s.storeExtras(ctx, fetch, extrasTodo{series: series, id: id, tmdb: tmdbID})
}
