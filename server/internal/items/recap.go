package items

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"time"
)

// RecapEntry is a top artist, album or track with how much it was played.
type RecapEntry struct {
	Item           Summary
	Plays, Minutes int
}

type GenreCount struct {
	Name  string
	Plays int
}

// Recap is a person's year in music (MUSIC-22).
type Recap struct {
	Year                             int
	Minutes, Plays, Tracks, Artists  int
	NewArtists                       int
	TopArtists, TopAlbums, TopTracks []RecapEntry
	TopGenres                        []GenreCount
	ByMonth                          [12]int // minutes
	ByHour                           [24]int // plays started
	TopDay                           string  // YYYY-MM-DD ("" = none)
	TopDayMinutes                    int
	LongestStreakDays                int
	FirstTrack                       *Summary
	VideoHours                       float64
}

// recapMinMS is how long a track must play to count (as Spotify-style recaps do).
const recapMinMS = "30000"

// recapPlays is every counted music play of a person between two instants (UTC ISO
// strings): pid, item, album and artist ids, when it started, and how long it played.
// Plays imported from Plex or synced from offline downloads carry no position, so they
// count as the whole track. CROSS JOIN keeps SQLite on the (user_id, started_at) index
// rather than scanning every track for each.
const recapPlays = `WITH plays AS (
	SELECT h.id AS pid, h.item_id AS item, COALESCE(i.parent_id, 0) AS album, COALESCE(i.grandparent_id, 0) AS artist, h.started_at AS at,
		CASE WHEN h.position_ms IS NULL OR (h.source != 'marquee' AND h.position_ms = 0) THEN COALESCE(i.duration_ms, 0)
		     WHEN COALESCE(i.duration_ms, 0) > 0 THEN MIN(h.position_ms, i.duration_ms) ELSE h.position_ms END AS ms
	FROM play_history h CROSS JOIN items i ON i.id = h.item_id AND i.type = 'track'
	WHERE h.user_id = ? AND h.started_at >= ? AND h.started_at < ?
), counted AS (SELECT * FROM plays WHERE ms >= ` + recapMinMS + `) `

func isoUTC(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.000Z") }

// yearBounds is the calendar year in the server's local time, as UTC ISO strings.
func yearBounds(year int) (string, string) {
	return isoUTC(time.Date(year, 1, 1, 0, 0, 0, 0, time.Local)), isoUTC(time.Date(year+1, 1, 1, 0, 0, 0, 0, time.Local))
}

// Recap summarises uid's music plays in a calendar year; acc decides which items the top
// lists may show. A year without plays returns zeros.
func (s *Store) Recap(ctx context.Context, acc Access, uid int64, year int) (Recap, error) {
	r := Recap{Year: year, TopArtists: []RecapEntry{}, TopAlbums: []RecapEntry{}, TopTracks: []RecapEntry{}, TopGenres: []GenreCount{}}
	from, to := yearBounds(year)
	// The year's plays go into a temporary table on one connection, so the history is read
	// once and the aggregates below are cheap.
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return r, err
	}
	defer conn.Close()
	conn.ExecContext(ctx, `DROP TABLE IF EXISTS temp.recap_plays`)
	if _, err := conn.ExecContext(ctx, `CREATE TEMP TABLE recap_plays AS `+recapPlays+`SELECT * FROM counted`, uid, from, to); err != nil {
		return r, err
	}
	defer conn.ExecContext(context.WithoutCancel(ctx), `DROP TABLE IF EXISTS temp.recap_plays`)
	q := func(sel string, fn func(*sql.Rows) error, args ...any) error {
		rows, err := conn.QueryContext(ctx, sel, args...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			if err := fn(rows); err != nil {
				return err
			}
		}
		return rows.Err()
	}
	err = conn.QueryRowContext(ctx, `SELECT COALESCE(SUM(ms), 0) / 60000, COUNT(*), COUNT(DISTINCT item),
		COUNT(DISTINCT NULLIF(artist, 0)) FROM temp.recap_plays`).Scan(&r.Minutes, &r.Plays, &r.Tracks, &r.Artists)
	if err != nil || r.Plays == 0 {
		return r, err
	}
	// Top lists: ids first, then summaries the person may see.
	top := func(dst *[]RecapEntry, col string, n int) error {
		var ids []int64
		counts := map[int64][2]int{}
		err := q(`SELECT `+col+`, COUNT(*), SUM(ms) / 60000 FROM temp.recap_plays WHERE `+col+` != 0 GROUP BY `+col+`
			ORDER BY COUNT(*) DESC, SUM(ms) DESC, MIN(at) LIMIT `+strconv.Itoa(n), func(rows *sql.Rows) error {
			var id int64
			var plays, mins int
			if err := rows.Scan(&id, &plays, &mins); err != nil {
				return err
			}
			ids = append(ids, id)
			counts[id] = [2]int{plays, mins}
			return nil
		})
		if err != nil {
			return err
		}
		list, err := s.ByIDs(ctx, acc, ids)
		for _, it := range list {
			*dst = append(*dst, RecapEntry{Item: it, Plays: counts[it.ID][0], Minutes: counts[it.ID][1]})
		}
		return err
	}
	if err := top(&r.TopArtists, "artist", 10); err != nil {
		return r, err
	}
	if err := top(&r.TopAlbums, "album", 10); err != nil {
		return r, err
	}
	if err := top(&r.TopTracks, "item", 50); err != nil {
		return r, err
	}
	// Genres of the track, its album or its artist; a play counts once per genre. Plays are
	// counted per track first so the tag lookups are per track, not per play.
	err = q(`WITH per AS MATERIALIZED (SELECT item, album, artist, COUNT(*) AS n FROM temp.recap_plays GROUP BY item),
		tg AS (SELECT p.item, it.tag_id FROM per p JOIN item_tags it ON it.item_id = p.item
			UNION SELECT p.item, it.tag_id FROM per p JOIN item_tags it ON it.item_id = p.album
			UNION SELECT p.item, it.tag_id FROM per p JOIN item_tags it ON it.item_id = p.artist)
		SELECT t.name, SUM(p.n) FROM tg JOIN per p ON p.item = tg.item JOIN tags t ON t.id = tg.tag_id AND t.kind = 'genre'
		GROUP BY t.name ORDER BY 2 DESC, t.name LIMIT 5`, func(rows *sql.Rows) error {
		var g GenreCount
		if err := rows.Scan(&g.Name, &g.Plays); err != nil {
			return err
		}
		r.TopGenres = append(r.TopGenres, g)
		return nil
	})
	if err != nil {
		return r, err
	}
	// Months, hours and days in local time. SQLite's 'localtime' costs a system call per
	// row, so plays are grouped by UTC half hour here and converted once per group.
	var monthMS [12]int64
	dayMS := map[string]int64{}
	err = q(`SELECT substr(at, 1, 13), substr(at, 15, 2) >= '30', COUNT(*), SUM(ms) FROM temp.recap_plays GROUP BY 1, 2`, func(rows *sql.Rows) error {
		var hour string
		var half bool
		var n int
		var ms int64
		if err := rows.Scan(&hour, &half, &n, &ms); err != nil {
			return err
		}
		t, err := time.Parse("2006-01-02T15", hour)
		if err != nil {
			return nil
		}
		if half {
			t = t.Add(30 * time.Minute)
		}
		t = t.In(time.Local)
		monthMS[t.Month()-1] += ms
		r.ByHour[t.Hour()] += n
		dayMS[t.Format("2006-01-02")] += ms
		return nil
	})
	if err != nil {
		return r, err
	}
	for m, ms := range monthMS {
		r.ByMonth[m] = int(ms / 60000)
	}
	days := make([]string, 0, len(dayMS))
	for d := range dayMS {
		days = append(days, d)
	}
	sort.Strings(days)
	for _, d := range days {
		if mins := int(dayMS[d] / 60000); r.TopDay == "" || mins > r.TopDayMinutes {
			r.TopDay, r.TopDayMinutes = d, mins
		}
	}
	r.LongestStreakDays = longestStreak(days)
	var first int64
	if err := conn.QueryRowContext(ctx, `SELECT item FROM temp.recap_plays ORDER BY at, pid LIMIT 1`).Scan(&first); err != nil {
		return r, err
	}
	if list, err := s.ByIDs(ctx, acc, []int64{first}); err == nil && len(list) == 1 {
		r.FirstTrack = &list[0]
	}
	// Artists whose first counted play ever falls in this year.
	if err := conn.QueryRowContext(ctx, recapPlays+`SELECT COUNT(DISTINCT r.artist) FROM temp.recap_plays r
		WHERE r.artist != 0 AND r.artist NOT IN (SELECT artist FROM counted)`, uid, "", from).Scan(&r.NewArtists); err != nil {
		return r, err
	}
	// Video watched the same year: wall time capped at the item's length; imported plays
	// (no stop time to go by) count as the whole thing.
	err = conn.QueryRowContext(ctx, `SELECT COALESCE(SUM(CASE WHEN h.source = 'marquee' THEN
			MIN(MAX(julianday(COALESCE(h.stopped_at, h.started_at)) - julianday(h.started_at), 0) * 24, COALESCE(i.duration_ms, 0) / 3600000.0)
			ELSE COALESCE(i.duration_ms, 0) / 3600000.0 END), 0)
		FROM play_history h CROSS JOIN items i ON i.id = h.item_id AND i.type IN ('movie', 'episode')
		WHERE h.user_id = ? AND h.started_at >= ? AND h.started_at < ?`, uid, from, to).Scan(&r.VideoHours)
	return r, err
}

// longestStreak is the most consecutive dates (sorted YYYY-MM-DD) in a row.
func longestStreak(days []string) int {
	best, run := 0, 0
	var prev time.Time
	for _, d := range days {
		t, err := time.Parse("2006-01-02", d)
		if err != nil {
			continue
		}
		if run > 0 && t.Sub(prev) == 24*time.Hour {
			run++
		} else {
			run = 1
		}
		prev = t
		best = max(best, run)
	}
	return best
}

// RecapYears lists the years (server local time) in which uid played music, newest first.
// It probes each year between the first and last play rather than converting every play
// to local time.
func (s *Store) RecapYears(ctx context.Context, uid int64) ([]int, error) {
	out := []int{}
	const music = ` FROM play_history h CROSS JOIN items i ON i.id = h.item_id AND i.type = 'track' WHERE h.user_id = ?`
	var first, last sql.NullString
	if err := s.db.QueryRowContext(ctx, `SELECT MIN(h.started_at), MAX(h.started_at)`+music, uid).Scan(&first, &last); err != nil || !first.Valid {
		return out, err
	}
	from, err1 := time.Parse(time.RFC3339Nano, first.String)
	to, err2 := time.Parse(time.RFC3339Nano, last.String)
	if err1 != nil || err2 != nil {
		return out, nil
	}
	for y := to.In(time.Local).Year(); y >= from.In(time.Local).Year() && y > to.In(time.Local).Year()-100; y-- {
		lo, hi := yearBounds(y)
		var n int
		if err := s.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1`+music+` AND h.started_at >= ? AND h.started_at < ?)`, uid, lo, hi).Scan(&n); err != nil {
			return nil, err
		}
		if n == 1 {
			out = append(out, y)
		}
	}
	return out, nil
}

// RecapPlaylistTitle names the playlist of a year's top songs.
func RecapPlaylistTitle(year int) string { return fmt.Sprintf("Your Top Songs %d", year) }

// SaveRecapPlaylist creates the person's "Your Top Songs <year>" audio playlist from ids,
// or replaces the items of the one made before. ErrNotFound when there's nothing to add.
func (s *Store) SaveRecapPlaylist(ctx context.Context, acc Access, year int, ids []int64) (Playlist, error) {
	if len(ids) == 0 {
		return Playlist{}, ErrNotFound
	}
	title := RecapPlaylistTitle(year)
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM playlists WHERE user_id = ? AND kind = 'audio' AND title = ? AND smart_rule IS NULL
		ORDER BY id LIMIT 1`, acc.UserID, title).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		p, err := s.CreatePlaylist(ctx, acc, title, "audio", ids, nil)
		if err == nil && p.ItemCount == 0 {
			s.DeletePlaylist(ctx, acc.UserID, p.ID)
			return Playlist{}, ErrNotFound
		}
		return p, err
	}
	if err != nil {
		return Playlist{}, err
	}
	if _, err := s.db.ExecContext(ctx, `DELETE FROM playlist_items WHERE playlist_id = ?`, id); err != nil {
		return Playlist{}, err
	}
	if err := s.AddToPlaylist(ctx, acc, id, ids); errors.Is(err, ErrPlaylistKind) {
		return Playlist{}, ErrNotFound
	} else if err != nil {
		return Playlist{}, err
	}
	return s.Playlist(ctx, acc.UserID, id)
}
