package items

import (
	"context"
	"database/sql"
	"sort"
	"time"
)

// Recommendations (USER-16): "Because you watched X" and "Recommended for You" from the
// text embeddings of movies and shows, plus embedding-based related items.

// Seed is a movie or show the person finished recently.
type Seed struct {
	ID        int64
	Rating    float64 // the person's 0–10 rating, 0 = none
	Finished  bool    // a movie played, or a show with every episode watched
	WatchedAt string
}

// recentSeeds lists uid's movies and shows completed in the last days, most recent first:
// movies with a play, and shows through their watched episodes.
func (s *Store) recentSeeds(ctx context.Context, uid int64, days, limit int) ([]Seed, error) {
	since := isoUTC(time.Now().AddDate(0, 0, -days))
	var out []Seed
	err := eachRow(ctx, s.db, `SELECT w.id, MAX(w.at), COALESCE((SELECT r.rating FROM user_item_state r WHERE r.user_id = ?1 AND r.item_id = w.id), 0),
			CASE WHEN w.typ = 'movie' THEN 1 ELSE (SELECT COUNT(*) FROM items e JOIN user_item_state x ON x.item_id = e.id AND x.user_id = ?1
				AND x.play_count > 0 WHERE e.grandparent_id = w.id AND e.type = 'episode') >= MAX(w.leaves) END
		FROM (SELECT i.id AS id, i.type AS typ, x.last_viewed_at AS at, 0 AS leaves FROM user_item_state x
				JOIN items i ON i.id = x.item_id AND i.type = 'movie' AND i.extra_type IS NULL
				WHERE x.user_id = ?1 AND x.play_count > 0 AND x.last_viewed_at >= ?2
			UNION ALL
			SELECT g.id, g.type, x.last_viewed_at, g.leaf_count FROM user_item_state x
				JOIN items e ON e.id = x.item_id AND e.type = 'episode' JOIN items g ON g.id = e.grandparent_id AND g.extra_type IS NULL
				WHERE x.user_id = ?1 AND x.play_count > 0 AND x.last_viewed_at >= ?2) w
		GROUP BY w.id ORDER BY MAX(w.at) DESC LIMIT ?3`, []any{uid, since, limit}, func(r *sql.Rows) error {
		var sd Seed
		if err := r.Scan(&sd.ID, &sd.WatchedAt, &sd.Rating, &sd.Finished); err != nil {
			return err
		}
		out = append(out, sd)
		return nil
	})
	return out, err
}

// started is every movie and show uid has watched or started (a show once any episode is).
func (s *Store) started(ctx context.Context, uid int64) (map[int64]bool, error) {
	out := map[int64]bool{}
	err := eachRow(ctx, s.db, `SELECT x.item_id FROM user_item_state x WHERE x.user_id = ?1 AND (x.play_count > 0 OR x.view_offset_ms > 0)
		UNION SELECT e.grandparent_id FROM user_item_state x JOIN items e ON e.id = x.item_id AND e.type = 'episode'
		WHERE x.user_id = ?1 AND (x.play_count > 0 OR x.view_offset_ms > 0) AND e.grandparent_id IS NOT NULL`, []any{uid}, func(r *sql.Rows) error {
		var id int64
		if err := r.Scan(&id); err != nil {
			return err
		}
		out[id] = true
		return nil
	})
	return out, err
}

// visibleTop returns up to n of ranked (best first) that acc can see, in order.
func (s *Store) visibleTop(ctx context.Context, acc Access, ranked []int64, n int) ([]Summary, error) {
	out := []Summary{}
	for start := 0; start < len(ranked) && len(out) < n; start += 200 {
		list, err := s.ByIDs(ctx, acc, ranked[start:min(start+200, len(ranked))])
		if err != nil {
			return nil, err
		}
		for _, it := range list {
			if len(out) < n {
				out = append(out, it)
			}
		}
	}
	return out, nil
}

// libraryKeep accepts index entries in libIDs (nil = any).
func libraryKeep(libIDs []int64) func(*VideoVec) bool {
	if libIDs == nil {
		return func(*VideoVec) bool { return true }
	}
	set := map[int64]bool{}
	for _, id := range libIDs {
		set[id] = true
	}
	return func(v *VideoVec) bool { return set[v.LibraryID] }
}

// Because is one "Because you watched" row.
type Because struct {
	Seed  Summary
	Items []Summary
}

// BecauseYouWatched builds up to rows rows of n items each: for the person's recently
// finished movies and shows (last 60 days; ones they rated 7+ or finished come first), the
// most similar ones they haven't started, of the same type first. libIDs limits the
// suggestions (nil = all the person can see).
func (s *Store) BecauseYouWatched(ctx context.Context, acc Access, idx *VideoIndex, libIDs []int64, rows, n int) ([]Because, error) {
	if idx.Len() == 0 || acc.UserID == 0 {
		return nil, nil
	}
	seeds, err := s.recentSeeds(ctx, acc.UserID, 60, 50)
	if err != nil || len(seeds) == 0 {
		return nil, err
	}
	sort.SliceStable(seeds, func(i, j int) bool {
		return seedPreferred(seeds[i]) && !seedPreferred(seeds[j])
	})
	started, err := s.started(ctx, acc.UserID)
	if err != nil {
		return nil, err
	}
	neg, err := s.disliked(ctx, acc.UserID)
	if err != nil {
		return nil, err
	}
	full, acc := acc, within(acc, libIDs)
	inLib := libraryKeep(acc.LibraryIDs)
	var out []Because
	for _, sd := range seeds {
		if len(out) == rows {
			break
		}
		if sd.Rating > 0 && sd.Rating < 5 {
			continue // they didn't like it
		}
		v := idx.Get(sd.ID)
		if v == nil {
			continue
		}
		seed, err := s.ByIDs(ctx, full, []int64{sd.ID})
		if err != nil {
			return nil, err
		}
		if len(seed) == 0 {
			continue
		}
		ranked := idx.Nearest(v.Vec, func(c *VideoVec) bool { return c.ItemID != sd.ID && !started[c.ItemID] && inLib(c) }, 0)
		ranked = PenaliseSimilar(ranked, idx, neg)
		sort.SliceStable(ranked, func(i, j int) bool {
			ti, tj := idx.Get(ranked[i].ItemID).Type == v.Type, idx.Get(ranked[j].ItemID).Type == v.Type
			return ti && !tj
		})
		items, err := s.visibleTop(ctx, acc, scoredIDs(ranked, 400), n)
		if err != nil {
			return nil, err
		}
		if len(items) > 0 {
			out = append(out, Because{Seed: seed[0], Items: items})
		}
	}
	return out, nil
}

func seedPreferred(sd Seed) bool { return sd.Rating >= 7 || sd.Finished }

func scoredIDs(list []Scored, n int) []int64 {
	out := make([]int64, 0, min(n, len(list)))
	for _, sc := range list {
		if len(out) == n {
			break
		}
		out = append(out, sc.ItemID)
	}
	return out
}

// Recommended ranks the movies and shows the person hasn't started by cosine to the centroid
// of their last 20 completed ones, weighted by their rating where they gave one.
func (s *Store) Recommended(ctx context.Context, acc Access, idx *VideoIndex, libIDs []int64, n int) ([]Summary, error) {
	if idx.Len() == 0 || acc.UserID == 0 {
		return nil, nil
	}
	seeds, err := s.recentSeeds(ctx, acc.UserID, 3650, 20)
	if err != nil || len(seeds) == 0 {
		return nil, err
	}
	ids := make([]int64, len(seeds))
	weights := make([]float64, len(seeds))
	for i, sd := range seeds {
		ids[i], weights[i] = sd.ID, 1
		if sd.Rating > 0 {
			weights[i] = sd.Rating / 7 // 7/10 counts as usual, 10 a little more, 2 barely
		}
	}
	c := idx.Centroid(ids, weights)
	if c == nil {
		return nil, nil
	}
	started, err := s.started(ctx, acc.UserID)
	if err != nil {
		return nil, err
	}
	acc = within(acc, libIDs)
	inLib := libraryKeep(acc.LibraryIDs)
	ranked := idx.Nearest(c, func(v *VideoVec) bool { return !started[v.ItemID] && inLib(v) }, 400)
	neg, err := s.disliked(ctx, acc.UserID)
	if err != nil {
		return nil, err
	}
	ranked = PenaliseSimilar(ranked, idx, neg)
	return s.visibleTop(ctx, acc, scoredIDs(ranked, 400), n)
}

// RelatedByEmbedding returns the items most like id by meaning (same library and type), or
// ok=false when id isn't embedded.
func (s *Store) RelatedByEmbedding(ctx context.Context, acc Access, idx *VideoIndex, id int64, n int) ([]Summary, bool, error) {
	v := idx.Get(id)
	if v == nil {
		return nil, false, nil
	}
	ranked := idx.Nearest(v.Vec, func(c *VideoVec) bool { return c.ItemID != id && c.LibraryID == v.LibraryID && c.Type == v.Type }, 400)
	list, err := s.visibleTop(ctx, acc, scoredIDs(ranked, 400), n)
	return list, true, err
}

// within narrows acc to libIDs (nil = no further limit).
func within(acc Access, libIDs []int64) Access {
	if libIDs != nil {
		acc.LibraryIDs = intersect(acc.LibraryIDs, libIDs)
	}
	return acc
}

// ---------- learning from behaviour ----------

// disliked lists the movies and shows uid turned away from (when learning is on): rated
// 2 stars or lower, movies abandoned in the first quarter and not resumed for two weeks,
// and shows dropped after an episode or two (nothing watched for a month), from the last
// six months.
func (s *Store) disliked(ctx context.Context, uid int64) ([]int64, error) {
	if s.Learn == nil || !s.Learn() {
		return nil, nil
	}
	var out []int64
	err := eachRow(ctx, s.db, `SELECT x.item_id FROM user_item_state x JOIN items i ON i.id = x.item_id AND i.type IN ('movie', 'show')
			WHERE x.user_id = ?1 AND x.rating IS NOT NULL AND x.rating <= 4
		UNION SELECT x.item_id FROM user_item_state x JOIN items i ON i.id = x.item_id AND i.type = 'movie'
			WHERE x.user_id = ?1 AND x.play_count = 0 AND x.view_offset_ms > 0 AND COALESCE(i.duration_ms, 0) > 0
			  AND x.view_offset_ms < i.duration_ms / 4 AND x.last_viewed_at < ?2 AND x.last_viewed_at > ?3
		UNION SELECT g.id FROM user_item_state x JOIN items e ON e.id = x.item_id AND e.type = 'episode' JOIN items g ON g.id = e.grandparent_id
			WHERE x.user_id = ?1 AND (x.play_count > 0 OR x.view_offset_ms > 0) AND g.leaf_count > 3
			GROUP BY g.id HAVING COUNT(*) <= 2 AND MAX(x.last_viewed_at) < ?4 AND MAX(x.last_viewed_at) > ?3`,
		[]any{uid, isoUTC(time.Now().AddDate(0, 0, -14)), isoUTC(time.Now().AddDate(0, 0, -180)), isoUTC(time.Now().AddDate(0, 0, -30))},
		func(r *sql.Rows) error {
			var id int64
			if err := r.Scan(&id); err != nil {
				return err
			}
			out = append(out, id)
			return nil
		})
	return out, err
}

// Similarity above dislikeFloor to something the person turned away from costs
// dislikeWeight per unit (at most 0.2): a near-copy of an abandoned film falls behind
// equally good matches, a film merely in the same genre barely moves.
const (
	dislikeFloor  = 0.6
	dislikeWeight = 0.5
)

// PenaliseSimilar lowers each candidate by its similarity to the closest disliked title and
// re-sorts, best first. Candidates that aren't embedded, or no dislikes, leave it unchanged.
func PenaliseSimilar(ranked []Scored, idx *VideoIndex, disliked []int64) []Scored {
	var negs [][]float32
	for _, id := range disliked {
		if v := idx.Get(id); v != nil {
			negs = append(negs, v.Vec)
		}
	}
	if len(negs) == 0 || len(ranked) == 0 {
		return ranked
	}
	out := make([]Scored, len(ranked))
	for i, sc := range ranked {
		out[i] = sc
		v := idx.Get(sc.ItemID)
		if v == nil {
			continue
		}
		worst := 0.0
		for _, n := range negs {
			worst = max(worst, dotVec(v.Vec, n))
		}
		if worst > dislikeFloor {
			out[i].Score -= dislikeWeight * (worst - dislikeFloor)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	return out
}

// LikedTitles names up to n movies and shows acc's user recently finished or rated 7+, for
// explaining recommendations.
func (s *Store) LikedTitles(ctx context.Context, acc Access, n int) ([]string, error) {
	seeds, err := s.recentSeeds(ctx, acc.UserID, 365, 40)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for _, sd := range seeds {
		if seedPreferred(sd) && !(sd.Rating > 0 && sd.Rating < 5) {
			ids = append(ids, sd.ID)
		}
		if len(ids) == n {
			break
		}
	}
	if len(ids) == 0 {
		return nil, nil
	}
	list, err := s.ByIDs(ctx, acc, ids)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(list))
	for _, it := range list {
		out = append(out, it.Title)
	}
	return out, nil
}
