package items

import (
	"context"
	"database/sql"
	"sort"
	"strings"
	"time"
)

// More Home rows from what the person watches (USER-16): the next film of a series they've
// started, and more from the directors and actors of what they loved.

// NextInCollections is, for each film series (collection) the person has watched a film of,
// the next one after the latest they watched that they haven't started, most recently
// continued series first ("Next in The Dark Knight Collection").
func (s *Store) NextInCollections(ctx context.Context, acc Access, libIDs []int64, limit int) ([]Summary, error) {
	if len(libIDs) == 0 || acc.UserID == 0 {
		return nil, nil
	}
	acc.LibraryIDs = intersect(acc.LibraryIDs, libIDs)
	ac, aargs := acc.clause()
	type next struct {
		id    int64
		title string
	}
	var picks []next
	seen := map[int64]bool{}
	// The series they've watched in, most recent first, with the year of the latest film
	// watched; then each one's next unstarted film after it.
	rows, err := s.db.QueryContext(ctx, `SELECT ci.collection_id, c.title, MAX(COALESCE(i.year, 0)), MAX(x.last_viewed_at)
		FROM user_item_state x JOIN items i ON i.id = x.item_id AND i.type = 'movie'
		JOIN collection_items ci ON ci.item_id = i.id JOIN items c ON c.id = ci.collection_id
		WHERE x.user_id = ? AND x.play_count > 0 GROUP BY ci.collection_id ORDER BY MAX(x.last_viewed_at) DESC LIMIT 60`, acc.UserID)
	if err != nil {
		return nil, err
	}
	type series struct {
		id    int64
		title string
		year  int
	}
	var list []series
	for rows.Next() {
		var sr series
		var at sql.NullString
		if err := rows.Scan(&sr.id, &sr.title, &sr.year, &at); err != nil {
			rows.Close()
			return nil, err
		}
		list = append(list, sr)
	}
	rows.Close()
	for _, sr := range list {
		if len(picks) == limit {
			break
		}
		var id int64
		err := s.db.QueryRowContext(ctx, `SELECT i.id`+summaryFrom+` JOIN collection_items ci ON ci.item_id = i.id AND ci.collection_id = ?
			WHERE i.type = 'movie' AND i.extra_type IS NULL AND COALESCE(i.year, 0) > ? AND `+ac+` AND `+notStarted(acc.UserID)+`
			ORDER BY COALESCE(i.year, 0), ci.ord LIMIT 1`, append([]any{sr.id, sr.year}, aargs...)...).Scan(&id)
		if err == sql.ErrNoRows || seen[id] {
			continue
		}
		if err != nil {
			return nil, err
		}
		seen[id] = true
		picks = append(picks, next{id, sr.title})
	}
	out := make([]Pick, len(picks))
	for i, p := range picks {
		out[i] = Pick{ID: p.id, Reason: "Next in " + p.title}
	}
	return s.picked(ctx, acc, out)
}

// PersonRow is a row of titles from one director or actor the person loves.
type PersonRow struct {
	PersonID int64
	Name     string
	Director bool // directed most of what they loved; otherwise starred in it
	Items    []Summary
}

// PeopleYouLove are rows (at most rows of them) of unstarted titles from the directors and
// leading actors (top three billed) of what the person loved: favourites count most, then
// high ratings, then finished titles. A person needs two titles they liked and three to
// offer. Each row is ordered for the person by m.
func (s *Store) PeopleYouLove(ctx context.Context, acc Access, m *TasteModel, libIDs []int64, rows, limit int) ([]PersonRow, error) {
	if len(libIDs) == 0 || acc.UserID == 0 || m == nil || len(m.seeds) == 0 {
		return nil, nil
	}
	args := make([]any, 0, len(m.seeds))
	for id := range m.seeds {
		args = append(args, id)
	}
	type person struct {
		id       int64
		name     string
		score    float64
		liked    int
		directed int
	}
	byID := map[int64]*person{}
	err := eachRow(ctx, s.db, `SELECT c.item_id, c.person_id, p.name, c.role FROM credits c JOIN people p ON p.id = c.person_id
		WHERE c.item_id IN (?`+strings.Repeat(",?", len(args)-1)+`) AND (c.role = 'director' OR (c.role = 'actor' AND c.ord < 3))`, args,
		func(r *sql.Rows) error {
			var item, pid int64
			var name, role string
			if err := r.Scan(&item, &pid, &name, &role); err != nil {
				return err
			}
			p := byID[pid]
			if p == nil {
				p = &person{id: pid, name: name}
				byID[pid] = p
			}
			p.score += m.seeds[item].weight
			p.liked++
			if role == "director" {
				p.directed++
			}
			return nil
		})
	if err != nil {
		return nil, err
	}
	var people []*person
	for _, p := range byID {
		if p.liked >= 2 {
			people = append(people, p)
		}
	}
	sort.Slice(people, func(a, b int) bool {
		if people[a].score != people[b].score {
			return people[a].score > people[b].score
		}
		return people[a].id < people[b].id
	})
	acc.LibraryIDs = intersect(acc.LibraryIDs, libIDs)
	ac, aargs := acc.clause()
	var out []PersonRow
	for i, p := range people {
		if len(out) == rows || i == 15 { // the strongest fifteen are plenty to find a couple of rows in
			break
		}
		var cands []Cand
		err := eachRow(ctx, s.db, `SELECT DISTINCT i.id, COALESCE(i.audience_rating, i.imdb_rating, i.critic_rating, 0)`+summaryFrom+`
			JOIN credits c ON c.item_id = i.id AND c.person_id = ? AND (c.role = 'director' OR (c.role = 'actor' AND c.ord < 5))
			WHERE i.type IN ('movie', 'show') AND i.extra_type IS NULL AND `+ac+` AND `+notStarted(acc.UserID), append([]any{p.id}, aargs...),
			func(r *sql.Rows) error {
				var c Cand
				if err := r.Scan(&c.ID, &c.Rating); err != nil {
					return err
				}
				cands = append(cands, c)
				return nil
			})
		if err != nil {
			return nil, err
		}
		if len(cands) < 3 {
			continue
		}
		picks := m.Order(cands, limit)
		for i := range picks {
			picks[i].Reason = "" // the row's title says why
		}
		list, err := s.picked(ctx, acc, picks)
		if err != nil {
			return nil, err
		}
		if len(list) >= 3 {
			out = append(out, PersonRow{PersonID: p.id, Name: p.name, Director: p.directed*2 >= p.liked, Items: list})
		}
	}
	return out, nil
}

// ---------- what the person does with Home ----------

// SetNotInterested marks a movie or show Not interested for uid (on) or takes the mark off.
func (s *Store) SetNotInterested(ctx context.Context, uid, itemID int64, on bool) error {
	var err error
	if on {
		_, err = s.db.ExecContext(ctx, `INSERT INTO item_not_interested(user_id, item_id, at) VALUES (?, ?, ?)
			ON CONFLICT(user_id, item_id) DO NOTHING`, uid, itemID, isoUTC(time.Now()))
	} else {
		_, err = s.db.ExecContext(ctx, `DELETE FROM item_not_interested WHERE user_id = ? AND item_id = ?`, uid, itemID)
	}
	return err
}

// NotInterested reports whether uid marked the item Not interested.
func (s *Store) NotInterested(ctx context.Context, uid, itemID int64) bool {
	var one int
	return s.db.QueryRowContext(ctx, `SELECT 1 FROM item_not_interested WHERE user_id = ? AND item_id = ?`, uid, itemID).Scan(&one) == nil
}

// HomeShown records the titles Home showed uid today, by row (the first few of each row:
// what's on screen without scrolling): for how long each has been passed over, and for
// counting a play soon after for its row.
func (s *Store) HomeShown(ctx context.Context, uid int64, shown map[string][]int64) error {
	if uid == 0 || len(shown) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now, today := time.Now(), time.Now().Format("2006-01-02")
	counted := map[int64]bool{}
	for row, ids := range shown {
		for _, id := range ids {
			if _, err := tx.ExecContext(ctx, `INSERT INTO home_shown(user_id, item_id, row_id, shown_at) VALUES (?, ?, ?, ?)
				ON CONFLICT(user_id, item_id, row_id) DO UPDATE SET shown_at = excluded.shown_at`, uid, id, row, isoUTC(now)); err != nil {
				return err
			}
			if counted[id] || !SuggestionRow(row) {
				continue
			}
			counted[id] = true
			if _, err := tx.ExecContext(ctx, `INSERT INTO home_seen(user_id, item_id, days, last_day) VALUES (?, ?, 1, ?)
				ON CONFLICT(user_id, item_id) DO UPDATE SET days = days + (last_day <> excluded.last_day), last_day = excluded.last_day`,
				uid, id, today); err != nil {
				return err
			}
		}
	}
	// A month is long enough to remember.
	if _, err := tx.ExecContext(ctx, `DELETE FROM home_shown WHERE shown_at < ?`, isoUTC(now.AddDate(0, -1, 0))); err != nil {
		return err
	}
	return tx.Commit()
}

// SuggestionRow reports whether a Home row (family) suggests titles, as opposed to Continue
// Watching, the watchlist, favourites and Recently Added: only suggestions give way when passed
// over, or sink when never played from.
func SuggestionRow(row string) bool {
	switch row {
	case "continue-watching", "watchlist", "favorites", "recent", "played", "watch-again":
		return false
	}
	return true
}

// Opened notes that uid opened or played a title (a show for an episode): it's no longer
// being passed over.
func (s *Store) Opened(ctx context.Context, uid, itemID int64) {
	s.db.ExecContext(ctx, `DELETE FROM home_seen WHERE user_id = ? AND item_id IN (?,
		(SELECT grandparent_id FROM items WHERE id = ? AND type = 'episode'), (SELECT parent_id FROM items WHERE id = ? AND type = 'season'))`,
		uid, itemID, itemID, itemID)
}

// homeWindow is how soon after Home showed a title a play counts for its row.
const homeWindow = 48 * time.Hour

// HomeContext is the Home row a play of itemID by uid counts for ("home:recommended"), when Home
// showed it (or its show) in the last two days: the row that showed it most recently; "" otherwise.
func (s *Store) HomeContext(ctx context.Context, uid, itemID int64) string {
	var row string
	err := s.db.QueryRowContext(ctx, `SELECT row_id FROM home_shown WHERE user_id = ? AND shown_at >= ? AND item_id IN (?,
		(SELECT grandparent_id FROM items WHERE id = ? AND type = 'episode')) ORDER BY shown_at DESC LIMIT 1`,
		uid, isoUTC(time.Now().Add(-homeWindow)), itemID, itemID).Scan(&row)
	if err != nil {
		return ""
	}
	return "home:" + row
}

// RowUse is how a Home row did for someone over the last month: titles it showed, and plays
// that came from it.
type RowUse struct {
	Shown, Plays int
}

// HomeRowUse is how each Home row (family) did for uid over the last month.
func (s *Store) HomeRowUse(ctx context.Context, uid int64) (map[string]RowUse, error) {
	out := map[string]RowUse{}
	since := isoUTC(time.Now().AddDate(0, -1, 0))
	if err := eachRow(ctx, s.db, `SELECT row_id, COUNT(*) FROM home_shown WHERE user_id = ? AND shown_at >= ? GROUP BY row_id`, []any{uid, since},
		func(r *sql.Rows) error {
			var row string
			var n int
			if err := r.Scan(&row, &n); err != nil {
				return err
			}
			u := out[row]
			u.Shown = n
			out[row] = u
			return nil
		}); err != nil {
		return nil, err
	}
	err := eachRow(ctx, s.db, `SELECT SUBSTR(context, 6), COUNT(*) FROM play_history WHERE user_id = ? AND started_at >= ? AND context LIKE 'home:%'
		GROUP BY context`, []any{uid, since}, func(r *sql.Rows) error {
		var row string
		var n int
		if err := r.Scan(&row, &n); err != nil {
			return err
		}
		u := out[row]
		u.Plays = n
		out[row] = u
		return nil
	})
	return out, err
}
