// Package stats summarises play history (ADM-4): what was played, by whom, on what, and
// how it was delivered, Tautulli-style. Per-user reports double as "Year in music"
// (MUSIC-11).
package stats

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type Count struct {
	ID    int64
	Title string
	Sub   string // e.g. an album's artist
	Plays int
	Hours float64
}

type Day struct {
	Date         string // YYYY-MM-DD, server local time
	Video, Music int
	Hours        float64
}

type Report struct {
	Since                time.Time
	Plays                int
	Hours                float64
	Users                int
	Days                 []Day
	Movies, Shows        []Count
	Artists, Albums      []Count
	Tracks               []Count
	People               []Count // users
	Platforms            []Count
	Methods              map[string]int // direct_play, direct_stream, transcode
	Local, Remote        int
	MusicHours, VideoHrs float64
}

// hours is how long a play lasted: wall time from start to stop, but never longer than
// the item (a paused-overnight session shouldn't count as eight hours of watching).
const hours = `MIN(MAX(julianday(COALESCE(h.stopped_at, h.started_at)) - julianday(h.started_at), 0) * 24,
	CASE WHEN i.duration_ms > 0 THEN i.duration_ms / 3600000.0 ELSE 24 END)`

const from = ` FROM play_history h LEFT JOIN items i ON i.id = h.item_id `

// Build makes a report for the last days days (0 = all time), for one user or everyone.
func Build(ctx context.Context, db *sql.DB, days int, userID int64, limit int) (Report, error) {
	r := Report{Methods: map[string]int{}}
	where := ` WHERE 1 = 1`
	var args []any
	if days > 0 {
		r.Since = time.Now().AddDate(0, 0, -days)
		where += ` AND h.started_at >= ?`
		args = append(args, r.Since.UTC().Format("2006-01-02T15:04:05.000Z"))
	}
	if userID > 0 {
		where += ` AND h.user_id = ?`
		args = append(args, userID)
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(`+hours+`), 0), COUNT(DISTINCT h.user_id),
		COALESCE(SUM(CASE WHEN i.type = 'track' THEN `+hours+` END), 0)`+from+where, args...).
		Scan(&r.Plays, &r.Hours, &r.Users, &r.MusicHours); err != nil {
		return r, err
	}
	r.VideoHrs = r.Hours - r.MusicHours

	rows, err := db.QueryContext(ctx, `SELECT date(h.started_at, 'localtime') AS d,
		SUM(CASE WHEN i.type = 'track' THEN 0 ELSE 1 END), SUM(CASE WHEN i.type = 'track' THEN 1 ELSE 0 END), SUM(`+hours+`)`+
		from+where+` GROUP BY d ORDER BY d`, args...)
	if err != nil {
		return r, err
	}
	for rows.Next() {
		var d Day
		if err := rows.Scan(&d.Date, &d.Video, &d.Music, &d.Hours); err != nil {
			rows.Close()
			return r, err
		}
		r.Days = append(r.Days, d)
	}
	rows.Close()

	top := func(dst *[]Count, idCol, titleCol, subCol, join, cond string) error {
		q := fmt.Sprintf(`SELECT %s, %s, %s, COUNT(*), SUM(%s)`+from+join+where+` AND %s AND %s IS NOT NULL
			GROUP BY %s ORDER BY COUNT(*) DESC, SUM(%s) DESC LIMIT ?`, idCol, titleCol, subCol, hours, cond, idCol, idCol, hours)
		rows, err := db.QueryContext(ctx, q, append(append([]any{}, args...), limit)...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var c Count
			var id any
			var sub sql.NullString
			if err := rows.Scan(&id, &c.Title, &sub, &c.Plays, &c.Hours); err != nil {
				return err
			}
			c.ID, _ = id.(int64) // platforms are grouped by name, not id
			c.Sub = sub.String
			*dst = append(*dst, c)
		}
		return rows.Err()
	}
	steps := []func() error{
		func() error { return top(&r.Movies, "i.id", "i.title", "i.year", "", "i.type = 'movie'") },
		func() error {
			return top(&r.Shows, "i.grandparent_id", "g.title", "g.year", "LEFT JOIN items g ON g.id = i.grandparent_id ", "i.type = 'episode'")
		},
		func() error {
			return top(&r.Artists, "i.grandparent_id", "g.title", "NULL", "LEFT JOIN items g ON g.id = i.grandparent_id ", "i.type = 'track'")
		},
		func() error {
			return top(&r.Albums, "i.parent_id", "p.title", "COALESCE(p.artist_credit, g.title)",
				"LEFT JOIN items p ON p.id = i.parent_id LEFT JOIN items g ON g.id = i.grandparent_id ", "i.type = 'track'")
		},
		func() error {
			return top(&r.Tracks, "i.id", "i.title", "COALESCE(i.artist_credit, g.title)", "LEFT JOIN items g ON g.id = i.grandparent_id ", "i.type = 'track'")
		},
		func() error {
			return top(&r.People, "h.user_id", "COALESCE(u.display_name, u.username)", "NULL", "LEFT JOIN users u ON u.id = h.user_id ", "1 = 1")
		},
		func() error {
			return top(&r.Platforms, "COALESCE(d.platform, h.source)", "COALESCE(d.platform, h.source)", "NULL",
				"LEFT JOIN devices d ON d.id = h.device_id ", "1 = 1")
		},
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return r, err
		}
	}

	rows, err = db.QueryContext(ctx, `SELECT COALESCE(h.decision, ''), COALESCE(h.network_class, ''), COUNT(*)`+from+where+
		` GROUP BY 1, 2`, args...)
	if err != nil {
		return r, err
	}
	defer rows.Close()
	for rows.Next() {
		var method, network string
		var n int
		if err := rows.Scan(&method, &network, &n); err != nil {
			return r, err
		}
		if method != "" {
			r.Methods[method] += n
		}
		switch network {
		case "local":
			r.Local += n
		case "remote":
			r.Remote += n
		}
	}
	return r, rows.Err()
}
