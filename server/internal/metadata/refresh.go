package metadata

import (
	"context"
	"fmt"
	"log/slog"
)

// RefreshStale re-fetches metadata that is likely out of date (ADM-2): shows with recently
// added episodes or episodes that still have placeholder titles or no summary (TMDB fills
// these in after airing), and movies not refreshed for six months. Fields an admin edited
// stay locked. At most 150 shows and 100 movies per run, oldest first.
func (s *Service) RefreshStale(ctx context.Context) (string, error) {
	if _, err := s.tmdb(""); err != nil {
		return "TMDB isn't set up", nil
	}
	ids := func(q string) ([]int64, error) {
		rows, err := s.DB.QueryContext(ctx, q)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []int64
		for rows.Next() {
			var id int64
			if rows.Scan(&id) == nil {
				out = append(out, id)
			}
		}
		return out, rows.Err()
	}
	shows, err := ids(`SELECT sh.id FROM items sh WHERE sh.type = 'show' AND sh.match_state = 'matched'
		AND COALESCE(sh.metadata_refreshed_at, '') < strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '-6 days')
		AND EXISTS (SELECT 1 FROM items e WHERE e.grandparent_id = sh.id AND e.type = 'episode' AND (
			e.added_at > strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '-30 days')
			OR ((COALESCE(e.summary, '') = '' OR e.title GLOB 'Episode [0-9]*')
				AND COALESCE(e.originally_available_at, '9999') > date('now', '-120 days'))))
		ORDER BY COALESCE(sh.metadata_refreshed_at, '') LIMIT 150`)
	if err != nil {
		return "", err
	}
	movies, err := ids(`SELECT id FROM items WHERE type = 'movie' AND match_state = 'matched'
		AND COALESCE(metadata_refreshed_at, '') < strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '-180 days')
		ORDER BY COALESCE(metadata_refreshed_at, '') LIMIT 100`)
	if err != nil {
		return "", err
	}
	failed := 0
	for _, id := range append(shows, movies...) {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if err := s.Refresh(ctx, id); err != nil {
			failed++
			slog.Debug("metadata refresh", "item", id, "err", err)
		}
	}
	msg := fmt.Sprintf("Refreshed %d shows and %d movies", len(shows), len(movies))
	if failed > 0 {
		msg += fmt.Sprintf(" (%d failed)", failed)
	}
	return msg, nil
}
