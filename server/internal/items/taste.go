package items

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
)

// Taste is what a person likes and turned away from among the movies and shows, with what
// suggestions from outside the library (films and series on demand) need to match on: TMDB
// ids, TMDB's recommendations, genres, keywords and years. It is the same signal the
// library's recommendations use: favourites count most (above a 10/10 rating), then high
// ratings, then what they finished; low ratings and (when learning is on) abandoned titles
// are dislikes.
type Taste struct {
	Liked    []TasteTitle
	Disliked []TasteTitle
}

// TasteTitle is one movie or show in a person's taste.
type TasteTitle struct {
	ID       int64
	Type     string // movie, show
	Title    string
	Year     int
	TMDBID   int
	Weight   float64 // how much it says about their taste (a favourite 2, a 10/10 1.43, finished 1)
	Favorite bool
	Rating   float64 // their 0–10 rating, 0 = none
	Finished bool
	Genres   []string
	Keywords []string
	Recs     []int // TMDB's recommendations (same type), best first
}

// Weights of the signals: a favourite (favoriteWeight, 2) counts for more than a 10/10
// rating (10/7); a finished movie or show 1; a show they've started 0.6.
const (
	finishedWeight = 1.0
	startedWeight  = 0.6
)

// Taste gathers acc's user's taste: their favourite movies and shows, the ones they rated,
// and the ones they watched in the last ten years (at most limit liked titles, the
// strongest first), and what they turned away from. Only titles acc can see are included.
func (s *Store) Taste(ctx context.Context, acc Access, limit int) (Taste, error) {
	var out Taste
	uid := acc.UserID
	if uid == 0 {
		return out, nil
	}
	seeds, err := s.recentSeeds(ctx, uid, 3650, 200)
	if err != nil {
		return out, err
	}
	// Ratings without a recent play (an old favourite rated years ago, an imported rating).
	have := map[int64]bool{}
	for _, sd := range seeds {
		have[sd.ID] = true
	}
	err = eachRow(ctx, s.db, `SELECT x.item_id, x.rating FROM user_item_state x JOIN items i ON i.id = x.item_id AND i.type IN ('movie', 'show')
		AND i.extra_type IS NULL WHERE x.user_id = ? AND x.rating IS NOT NULL ORDER BY x.rating DESC LIMIT 200`, []any{uid}, func(r *sql.Rows) error {
		var sd Seed
		if err := r.Scan(&sd.ID, &sd.Rating); err != nil {
			return err
		}
		if !have[sd.ID] {
			have[sd.ID] = true
			seeds = append(seeds, sd)
		}
		return nil
	})
	if err != nil {
		return out, err
	}
	if seeds, err = s.withFavorites(ctx, uid, seeds, 100); err != nil {
		return out, err
	}
	disliked, err := s.disliked(ctx, uid)
	if err != nil {
		return out, err
	}
	isDisliked := map[int64]bool{}
	for _, id := range disliked {
		isDisliked[id] = true
	}
	var liked, neg []TasteTitle
	for _, sd := range seeds {
		t := TasteTitle{ID: sd.ID, Favorite: sd.Favorite, Rating: sd.Rating, Finished: sd.Finished}
		switch {
		case sd.Favorite:
			t.Weight = favoriteWeight
		case sd.Rating > 0 && sd.Rating < 5:
			neg = append(neg, t)
			continue
		case isDisliked[sd.ID]:
			continue // added with the dislikes below
		case sd.Rating > 0:
			t.Weight = sd.Rating / 7
		case sd.Finished:
			t.Weight = finishedWeight
		case sd.WatchedAt != "":
			t.Weight = startedWeight
		default:
			continue
		}
		liked = append(liked, t)
	}
	for _, id := range disliked {
		if !containsTitle(neg, id) {
			neg = append(neg, TasteTitle{ID: id})
		}
	}
	// Strongest first: favourites, then by weight (stable, so the most recent come first).
	sortTaste(liked)
	if limit > 0 && len(liked) > limit {
		liked = liked[:limit]
	}
	if out.Liked, err = s.fillTaste(ctx, acc, liked); err != nil {
		return out, err
	}
	out.Disliked, err = s.fillTaste(ctx, acc, neg)
	return out, err
}

func containsTitle(list []TasteTitle, id int64) bool {
	for _, t := range list {
		if t.ID == id {
			return true
		}
	}
	return false
}

func sortTaste(list []TasteTitle) {
	for i := 1; i < len(list); i++ {
		for j := i; j > 0 && list[j].Weight > list[j-1].Weight; j-- {
			list[j], list[j-1] = list[j-1], list[j]
		}
	}
}

// fillTaste adds titles, years, TMDB ids, genres, keywords and TMDB's recommendations,
// leaving out titles acc can't see.
func (s *Store) fillTaste(ctx context.Context, acc Access, list []TasteTitle) ([]TasteTitle, error) {
	if len(list) == 0 {
		return nil, nil
	}
	ids := make([]int64, len(list))
	for i, t := range list {
		ids[i] = t.ID
	}
	visible, err := s.ByIDs(ctx, acc, ids)
	if err != nil {
		return nil, err
	}
	byID := map[int64]Summary{}
	for _, it := range visible {
		byID[it.ID] = it
	}
	ph := "?" + strings.Repeat(",?", len(ids)-1)
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	tmdbIDs := map[int64]int{}
	if err := eachRow(ctx, s.db, `SELECT item_id, value FROM external_ids WHERE provider = 'tmdb' AND item_id IN (`+ph+`)`, args, func(r *sql.Rows) error {
		var id int64
		var v string
		if err := r.Scan(&id, &v); err != nil {
			return err
		}
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			tmdbIDs[id] = n
		}
		return nil
	}); err != nil {
		return nil, err
	}
	genres, keywords := map[int64][]string{}, map[int64][]string{}
	if err := eachRow(ctx, s.db, `SELECT it.item_id, t.kind, t.name FROM item_tags it JOIN tags t ON t.id = it.tag_id AND t.kind IN ('genre', 'keyword')
		WHERE it.item_id IN (`+ph+`)`, args, func(r *sql.Rows) error {
		var id int64
		var kind, name string
		if err := r.Scan(&id, &kind, &name); err != nil {
			return err
		}
		if kind == "genre" {
			genres[id] = append(genres[id], name)
		} else {
			keywords[id] = append(keywords[id], strings.ToLower(name))
		}
		return nil
	}); err != nil {
		return nil, err
	}
	recs := map[int64][]int{}
	if err := eachRow(ctx, s.db, `SELECT item_id, tmdb_id FROM tmdb_recommendations WHERE item_id IN (`+ph+`) ORDER BY item_id, rank`, args, func(r *sql.Rows) error {
		var id int64
		var tm int
		if err := r.Scan(&id, &tm); err != nil {
			return err
		}
		recs[id] = append(recs[id], tm)
		return nil
	}); err != nil {
		return nil, err
	}
	out := make([]TasteTitle, 0, len(list))
	for _, t := range list {
		it, ok := byID[t.ID]
		if !ok || (it.Type != "movie" && it.Type != "show") {
			continue
		}
		t.Type, t.Title, t.Year = it.Type, it.Title, it.Year
		t.TMDBID, t.Genres, t.Keywords, t.Recs = tmdbIDs[t.ID], genres[t.ID], keywords[t.ID], recs[t.ID]
		out = append(out, t)
	}
	return out, nil
}

// TasteOf is one movie or show with what suggestions match on (genres, keywords, TMDB id
// and recommendations), when acc can see it.
func (s *Store) TasteOf(ctx context.Context, acc Access, id int64) (TasteTitle, bool, error) {
	list, err := s.fillTaste(ctx, acc, []TasteTitle{{ID: id}})
	if err != nil || len(list) == 0 {
		return TasteTitle{}, false, err
	}
	return list[0], true, nil
}

// TasteVersion changes whenever acc's user's taste may have: a favourite added or removed,
// a rating, or something watched.
func (s *Store) TasteVersion(ctx context.Context, uid int64) (string, error) {
	var favs, rated int
	var lastFav, lastSeen, lastUpdate sql.NullString
	var ratingSum float64
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*), MAX(favorite_at) FROM user_item_state WHERE user_id = ? AND favorite_at IS NOT NULL`, uid).Scan(&favs, &lastFav)
	if err != nil {
		return "", err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT MAX(last_viewed_at) FROM user_item_state WHERE user_id = ?`, uid).Scan(&lastSeen); err != nil {
		return "", err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(rating), TOTAL(rating), MAX(updated_at) FROM user_item_state WHERE user_id = ?`, uid).Scan(&rated, &ratingSum, &lastUpdate); err != nil {
		return "", err
	}
	return strconv.Itoa(favs) + "|" + lastFav.String + "|" + lastSeen.String + "|" + strconv.Itoa(rated) + "|" +
		strconv.FormatFloat(ratingSum, 'f', 1, 64) + "|" + lastUpdate.String, nil
}
