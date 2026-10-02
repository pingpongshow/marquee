package items

import (
	"context"
	"math/rand/v2"
)

// Trailers for cinema mode (PLAY-18): trailers of other movies the person can see, mostly
// ones they haven't watched and that were added recently, in a random order.
func (s *Store) Trailers(ctx context.Context, acc Access, exceptMovie int64, n int) ([]Summary, error) {
	if n <= 0 {
		return []Summary{}, nil
	}
	ac, aargs := acc.clause()
	// Extras are videos, which rating limits don't cover; a trailer is judged by its film.
	if lvl := RatingLevel(acc.MaxRating); lvl > 0 {
		ac += " AND " + ratingCase("COALESCE(i.content_rating, p.content_rating)") + " <= ?"
		aargs = append(aargs, lvl)
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+cols(acc.UserID)+summaryFrom+`
		WHERE i.extra_type = 'trailer' AND p.type = 'movie' AND i.parent_id != ? AND i.available = 1 AND `+ac+`
		ORDER BY COALESCE((SELECT play_count FROM user_item_state WHERE user_id = ? AND item_id = p.id), 0) > 0, p.added_at DESC
		LIMIT ?`, append(append([]any{exceptMovie}, aargs...), acc.UserID, n*6)...)
	if err != nil {
		return nil, err
	}
	list, err := collect(rows)
	if err != nil {
		return nil, err
	}
	// One trailer per movie.
	seen := map[int64]bool{}
	pool := list[:0]
	for _, t := range list {
		if !seen[t.ParentID] {
			seen[t.ParentID] = true
			pool = append(pool, t)
		}
	}
	rand.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
	return pool[:min(n, len(pool))], nil
}
