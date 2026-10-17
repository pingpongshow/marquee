package items

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
)

// ---------- Favourites (the heart) ----------
//
// A favourite is the person's own mark on a movie, show, episode, song, album or artist,
// kept with the time it was made (user_item_state.favorite_at). Favourites are per user, so
// managed profiles have their own. Recommendations, Muse, radios and mixes treat them as the
// strongest liking signal, above a high star rating.

// favoriteCond is the SQL test for alias being uid's favourite.
func favoriteCond(uid int64, alias string) string {
	return `EXISTS (SELECT 1 FROM user_item_state fv WHERE fv.user_id = ` + strconv.FormatInt(uid, 10) +
		` AND fv.item_id = ` + alias + `.id AND fv.favorite_at IS NOT NULL)`
}

// SetFavorite marks an item as one of uid's favourites, or unmarks it. Marking it again
// keeps the original time.
func (s *Store) SetFavorite(ctx context.Context, uid, itemID int64, on bool) error {
	if on {
		_, err := s.db.ExecContext(ctx, `INSERT INTO user_item_state (user_id, item_id, favorite_at)
			VALUES (?, ?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
			ON CONFLICT(user_id, item_id) DO UPDATE SET favorite_at = COALESCE(favorite_at, excluded.favorite_at)`, uid, itemID)
		return err
	}
	_, err := s.db.ExecContext(ctx, `UPDATE user_item_state SET favorite_at = NULL WHERE user_id = ? AND item_id = ?`, uid, itemID)
	return err
}

// Favorites lists acc's user's favourites of the given types (nil = any) in libIDs (nil =
// any they can see), most recently made favourites first.
func (s *Store) Favorites(ctx context.Context, acc Access, types []string, libIDs []int64, limit int) ([]Summary, error) {
	if acc.UserID == 0 {
		return []Summary{}, nil
	}
	acc = within(acc, libIDs)
	ac, aargs := acc.clause()
	where := ac
	var args []any
	if len(types) > 0 {
		where += ` AND i.type IN (?` + strings.Repeat(",?", len(types)-1) + `)`
		for _, t := range types {
			args = append(args, t)
		}
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+cols(acc.UserID)+summaryFrom+`
		JOIN user_item_state fav ON fav.item_id = i.id AND fav.user_id = ? AND fav.favorite_at IS NOT NULL
		WHERE i.extra_type IS NULL AND `+where+` ORDER BY fav.favorite_at DESC, i.id DESC LIMIT ?`,
		append(append(append([]any{acc.UserID}, aargs...), args...), limit)...)
	if err != nil {
		return nil, err
	}
	return collect(rows)
}

// favoriteVideos is uid's favourite movies and shows, most recent first, with the shows of
// their favourite episodes (a favourite episode says they like the show).
func (s *Store) favoriteVideos(ctx context.Context, uid int64, limit int) ([]int64, error) {
	var out []int64
	if uid == 0 {
		return out, nil
	}
	err := eachRow(ctx, s.db, `SELECT id FROM (
			SELECT i.id AS id, x.favorite_at AS at FROM user_item_state x JOIN items i ON i.id = x.item_id AND i.type IN ('movie', 'show')
				WHERE x.user_id = ?1 AND x.favorite_at IS NOT NULL AND i.extra_type IS NULL
			UNION ALL
			SELECT e.grandparent_id, x.favorite_at FROM user_item_state x JOIN items e ON e.id = x.item_id AND e.type = 'episode'
				WHERE x.user_id = ?1 AND x.favorite_at IS NOT NULL AND e.grandparent_id IS NOT NULL)
		GROUP BY id ORDER BY MAX(at) DESC, id DESC LIMIT ?2`, []any{uid, limit}, func(r *sql.Rows) error {
		var id int64
		if err := r.Scan(&id); err != nil {
			return err
		}
		out = append(out, id)
		return nil
	})
	return out, err
}

// FavoriteVideos is favoriteVideos for callers outside the package (Muse "like my favourites").
func (s *Store) FavoriteVideos(ctx context.Context, uid int64, limit int) ([]int64, error) {
	return s.favoriteVideos(ctx, uid, limit)
}

// OnlyFavorites narrows acc to its user's favourites (for any listing that takes an Access).
func (a Access) OnlyFavorites() Access {
	a.extra = append(append([]string{}, a.extra...), favoriteCond(a.UserID, "i"))
	return a
}
