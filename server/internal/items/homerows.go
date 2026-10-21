package items

import (
	"context"
	"database/sql"
	"sort"
	"strings"
	"time"
)

// More Home rows (USER-12): a holiday row in the run-up to Halloween, Thanksgiving and
// Christmas, top rated, trending in the household and rows from the person's genres.

// Holiday is a holiday Home has a row for, and how its films and shows are recognised.
type Holiday struct {
	ID, Title string
	Keywords  []string // TMDB keyword tags (lower case)
	Words     []string // title words (lower case)
}

var (
	halloween_   = Holiday{ID: "halloween", Title: "Halloween Favourites", Keywords: []string{"halloween", "trick or treating", "jack-o'-lantern", "haunted house", "witch", "ghost", "vampire", "werewolf", "zombie", "monster", "slasher", "trick or treat"}, Words: []string{"halloween", "haunted", "ghost", "witch", "vampire", "zombie", "monster"}}
	thanksgiving = Holiday{ID: "thanksgiving", Title: "Thanksgiving Together", Keywords: []string{"thanksgiving", "family dinner", "family reunion", "pilgrim", "turkey", "family holiday", "dysfunctional family"}, Words: []string{"thanksgiving", "turkey"}}
	christmas    = Holiday{ID: "christmas", Title: "Christmas Classics", Keywords: []string{"christmas", "christmas party", "christmas eve", "christmas tree", "santa claus", "xmas", "holiday", "elf", "reindeer", "snowman", "christmas carol", "christmas spirit"}, Words: []string{"christmas", "xmas", "santa", "holiday", "noel", "grinch", "elf", "scrooge", "carol"}}
)

// HolidayWindow is how many days before a holiday its row appears (the day itself included).
const HolidayWindow = 30

// HolidaysOn are the holidays whose row shows on day t (in t's location), nearest first.
func HolidaysOn(t time.Time) []Holiday {
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
	var out []Holiday
	for _, h := range []struct {
		h  Holiday
		on func(year int) time.Time
	}{
		{halloween_, func(y int) time.Time { return time.Date(y, time.October, 31, 0, 0, 0, 0, t.Location()) }},
		{thanksgiving, func(y int) time.Time { return thanksgivingDay(y, t.Location()) }},
		{christmas, func(y int) time.Time { return time.Date(y, time.December, 25, 0, 0, 0, 0, t.Location()) }},
	} {
		d := h.on(day.Year())
		if !day.After(d) && !day.Before(d.AddDate(0, 0, -HolidayWindow)) {
			out = append(out, h.h)
		}
	}
	return out
}

// thanksgivingDay is the US Thanksgiving: the fourth Thursday of November.
func thanksgivingDay(year int, loc *time.Location) time.Time {
	d := time.Date(year, time.November, 1, 0, 0, 0, 0, loc)
	d = d.AddDate(0, 0, (int(time.Thursday)-int(d.Weekday())+7)%7)
	return d.AddDate(0, 0, 21)
}

// HolidayVideos are movies and shows in libIDs for h (a keyword tag or a word in the title),
// most matches first, then by rating.
func (s *Store) HolidayVideos(ctx context.Context, acc Access, h Holiday, libIDs []int64, limit int) ([]Summary, error) {
	if len(libIDs) == 0 {
		return nil, nil
	}
	acc.LibraryIDs = intersect(acc.LibraryIDs, libIDs)
	ac, aargs := acc.clause()
	var args []any
	kw := "?" + strings.Repeat(",?", len(h.Keywords)-1)
	for _, k := range h.Keywords {
		args = append(args, k)
	}
	var titles []string
	for _, w := range h.Words {
		titles = append(titles, `(' ' || LOWER(i.title) || ' ') LIKE ?`)
		args = append(args, "% "+w+"%")
	}
	q := `SELECT i.id, (SELECT COUNT(*) FROM item_tags it JOIN tags t ON t.id = it.tag_id AND t.kind = 'keyword'
			WHERE it.item_id = i.id AND LOWER(t.name) IN (` + kw + `)) + 2 * (` + strings.Join(titles, " OR ") + `) AS hits` +
		summaryFrom + ` WHERE i.type IN ('movie', 'show') AND i.extra_type IS NULL AND ` + ac + `
		ORDER BY hits DESC, COALESCE(i.audience_rating, i.imdb_rating, i.critic_rating, 0) DESC`
	args = append(args, aargs...)
	var ids []int64
	err := eachRow(ctx, s.db, `SELECT id FROM (`+q+`) WHERE hits > 0 LIMIT ?`, append(args, limit), func(r *sql.Rows) error {
		var id int64
		if err := r.Scan(&id); err != nil {
			return err
		}
		ids = append(ids, id)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.ByIDs(ctx, acc, ids)
}

// TopRated are well-rated movies and shows in libIDs the person hasn't started, best first.
func (s *Store) TopRated(ctx context.Context, acc Access, libIDs []int64, limit int) ([]Summary, error) {
	return s.unwatchedByRating(ctx, acc, libIDs, nil, 7.5, limit)
}

// GenreVideos are movies and shows in libIDs with genre the person hasn't started, best rated first.
func (s *Store) GenreVideos(ctx context.Context, acc Access, genre string, libIDs []int64, limit int) ([]Summary, error) {
	return s.unwatchedByRating(ctx, acc, libIDs, []string{genre}, 0, limit)
}

func (s *Store) unwatchedByRating(ctx context.Context, acc Access, libIDs []int64, genres []string, minRating float64, limit int) ([]Summary, error) {
	if len(libIDs) == 0 {
		return nil, nil
	}
	acc.LibraryIDs = intersect(acc.LibraryIDs, libIDs)
	ac, aargs := acc.clause()
	rating := `COALESCE(i.audience_rating, i.imdb_rating, i.critic_rating, 0)`
	where := ` WHERE i.type IN ('movie', 'show') AND i.extra_type IS NULL AND ` + ac + ` AND ` + notStarted(acc.UserID) + ` AND ` + rating + ` >= ?`
	args := append(append([]any{}, aargs...), minRating)
	if len(genres) > 0 {
		where += ` AND EXISTS (SELECT 1 FROM item_tags it JOIN tags t ON t.id = it.tag_id AND t.kind = 'genre'
			WHERE it.item_id = i.id AND t.name COLLATE NOCASE IN (?` + strings.Repeat(",?", len(genres)-1) + `))`
		for _, g := range genres {
			args = append(args, g)
		}
	}
	var ids []int64
	err := eachRow(ctx, s.db, `SELECT i.id`+summaryFrom+where+` ORDER BY `+rating+` DESC, i.sort_title COLLATE NOCASE LIMIT ?`, append(args, limit),
		func(r *sql.Rows) error {
			var id int64
			if err := r.Scan(&id); err != nil {
				return err
			}
			ids = append(ids, id)
			return nil
		})
	if err != nil {
		return nil, err
	}
	return s.ByIDs(ctx, acc, ids)
}

// Trending are the movies and shows in libIDs most watched in the household over the last
// month: by how many people watched, then how many plays.
func (s *Store) Trending(ctx context.Context, acc Access, libIDs []int64, limit int) ([]Summary, error) {
	if len(libIDs) == 0 {
		return nil, nil
	}
	acc.LibraryIDs = intersect(acc.LibraryIDs, libIDs)
	var ids []int64
	err := eachRow(ctx, s.db, `SELECT CASE WHEN e.type = 'episode' THEN e.grandparent_id ELSE e.id END AS vid
		FROM play_history h JOIN items e ON e.id = h.item_id AND e.type IN ('movie', 'episode') AND e.extra_type IS NULL
		WHERE h.started_at >= ? GROUP BY vid HAVING vid IS NOT NULL
		ORDER BY COUNT(DISTINCT h.user_id) DESC, COUNT(*) DESC LIMIT ?`,
		[]any{isoUTC(time.Now().AddDate(0, 0, -30)), limit * 2}, func(r *sql.Rows) error {
			var id int64
			if err := r.Scan(&id); err != nil {
				return err
			}
			ids = append(ids, id)
			return nil
		})
	if err != nil {
		return nil, err
	}
	list, err := s.ByIDs(ctx, acc, ids) // drops what acc can't see
	if len(list) > limit {
		list = list[:limit]
	}
	return list, err
}

// TasteGenres are the movie and show genres in libIDs the person watches most, best first;
// with no history, the libraries' most common genres.
func (s *Store) TasteGenres(ctx context.Context, acc Access, libIDs []int64, n int) ([]string, error) {
	if len(libIDs) == 0 {
		return nil, nil
	}
	in := "?" + strings.Repeat(",?", len(libIDs)-1)
	libArgs := make([]any, len(libIDs))
	for i, id := range libIDs {
		libArgs[i] = id
	}
	count := map[string]int{}
	err := eachRow(ctx, s.db, `SELECT t.name, COUNT(*) FROM user_item_state x JOIN items e ON e.id = x.item_id AND e.type IN ('movie', 'episode')
		JOIN items v ON v.id = CASE WHEN e.type = 'episode' THEN e.grandparent_id ELSE e.id END
		JOIN item_tags it ON it.item_id = v.id JOIN tags t ON t.id = it.tag_id AND t.kind = 'genre'
		WHERE x.user_id = ? AND x.play_count > 0 AND v.library_id IN (`+in+`) GROUP BY t.name`, append([]any{acc.UserID}, libArgs...),
		func(r *sql.Rows) error {
			var g string
			var c int
			if err := r.Scan(&g, &c); err != nil {
				return err
			}
			count[g] = c
			return nil
		})
	if err != nil {
		return nil, err
	}
	if len(count) < n {
		err = eachRow(ctx, s.db, `SELECT t.name, COUNT(*) FROM items i JOIN item_tags it ON it.item_id = i.id
			JOIN tags t ON t.id = it.tag_id AND t.kind = 'genre'
			WHERE i.type IN ('movie', 'show') AND i.library_id IN (`+in+`) GROUP BY t.name`, libArgs, func(r *sql.Rows) error {
			var g string
			var c int
			if err := r.Scan(&g, &c); err != nil {
				return err
			}
			if _, ok := count[g]; !ok {
				count[g] = -1_000_000 + c // after every genre they watch
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	out := make([]string, 0, len(count))
	for g := range count {
		out = append(out, g)
	}
	sort.Slice(out, func(i, j int) bool {
		if count[out[i]] != count[out[j]] {
			return count[out[i]] > count[out[j]]
		}
		return out[i] < out[j]
	})
	return out[:min(n, len(out))], nil
}
