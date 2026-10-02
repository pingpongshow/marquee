package items

import (
	"context"
	"database/sql"
	"strconv"
	"strings"
)

// Filter narrows a library listing (the Plex library filter bar).
type Filter struct {
	Watch         string // "", "unwatched", "watched", "in_progress"
	Genre         string
	Decade        int // e.g. 1990
	ContentRating string
	Resolution    string // "4k", "1080", "720", "sd"
	HDR           bool
	Letter        string // "A"–"Z" or "#"
}

// letterExpr is the A–Z jump-bar bucket of an item: its sort title's first letter, or "#".
const letterExpr = `CASE WHEN upper(substr(i.sort_title, 1, 1)) BETWEEN 'A' AND 'Z' THEN upper(substr(i.sort_title, 1, 1)) ELSE '#' END`

var resolutionMin = map[string][2]int{"4k": {1700, 1 << 30}, "1080": {900, 1700}, "720": {600, 900}, "sd": {0, 600}}

// clause returns the SQL condition (aliases i, p, g) and args for f, for user uid.
func (f Filter) clause(uid int64) (string, []any) {
	conds := []string{"1"}
	var args []any
	u := strconv.FormatInt(uid, 10)
	played := `(SELECT play_count FROM user_item_state WHERE user_id = ` + u + ` AND item_id = i.id)`
	offset := `(SELECT view_offset_ms FROM user_item_state WHERE user_id = ` + u + ` AND item_id = i.id)`
	leavesWatched := `(SELECT COUNT(*) FROM items l JOIN user_item_state x ON x.item_id = l.id AND x.user_id = ` + u + ` AND x.play_count > 0
		WHERE (l.parent_id = i.id OR l.grandparent_id = i.id) AND l.type IN ('episode', 'track'))`
	leavesStarted := `(SELECT COUNT(*) FROM items l JOIN user_item_state x ON x.item_id = l.id AND x.user_id = ` + u + ` AND (x.play_count > 0 OR x.view_offset_ms > 0)
		WHERE (l.parent_id = i.id OR l.grandparent_id = i.id) AND l.type IN ('episode', 'track'))`
	container := `i.type IN ('show', 'season', 'artist', 'album')`
	switch f.Watch {
	case "unwatched":
		conds = append(conds, `CASE WHEN `+container+` THEN `+leavesWatched+` < i.leaf_count ELSE COALESCE(`+played+`, 0) = 0 END`)
	case "watched":
		conds = append(conds, `CASE WHEN `+container+` THEN i.leaf_count > 0 AND `+leavesWatched+` >= i.leaf_count ELSE COALESCE(`+played+`, 0) > 0 END`)
	case "in_progress":
		conds = append(conds, `CASE WHEN `+container+` THEN `+leavesStarted+` > 0 AND `+leavesWatched+` < i.leaf_count
			ELSE COALESCE(`+offset+`, 0) > 0 AND COALESCE(`+played+`, 0) = 0 END`)
	}
	if f.Genre != "" {
		conds = append(conds, `EXISTS (SELECT 1 FROM item_tags it JOIN tags t ON t.id = it.tag_id WHERE it.item_id = i.id AND t.kind = 'genre' AND t.name = ? COLLATE NOCASE)`)
		args = append(args, f.Genre)
	}
	if f.Decade > 0 {
		conds = append(conds, `i.year BETWEEN ? AND ?`)
		args = append(args, f.Decade, f.Decade+9)
	}
	if f.ContentRating != "" {
		conds = append(conds, `i.content_rating = ?`)
		args = append(args, f.ContentRating)
	}
	if r, ok := resolutionMin[f.Resolution]; ok || f.HDR {
		file := `SELECT 1 FROM media_versions v JOIN media_files mf ON mf.version_id = v.id
			LEFT JOIN items e ON e.id = v.item_id WHERE (v.item_id = i.id OR e.parent_id = i.id OR e.grandparent_id = i.id)`
		if ok {
			file += ` AND COALESCE(mf.height, 0) >= ` + strconv.Itoa(r[0]) + ` AND COALESCE(mf.height, 0) < ` + strconv.Itoa(r[1])
		}
		if f.HDR {
			file += ` AND mf.hdr_format IS NOT NULL`
		}
		conds = append(conds, `EXISTS (`+file+`)`)
	}
	if f.Letter != "" {
		conds = append(conds, letterExpr+` = ?`)
		args = append(args, strings.ToUpper(f.Letter))
	}
	return strings.Join(conds, " AND "), args
}

// Facets are the values a library's filters can take, with counts.
type Facets struct {
	Genres         []Facet
	Decades        []Facet
	ContentRatings []Facet
	Letters        []Letter // for the A–Z jump bar
}

// Letter is where a jump-bar letter starts in the title-sorted listing.
type Letter struct {
	Letter string
	Offset int
}

type Facet struct {
	Value string
	Count int
}

// Facets lists filter values for items of typ in a library that acc can see.
func (s *Store) Facets(ctx context.Context, acc Access, libID int64, typ string) (Facets, error) {
	ac, aargs := acc.clause()
	where := ` WHERE i.library_id = ? AND i.type = ? AND i.extra_type IS NULL AND ` + ac
	args := append([]any{libID, typ}, aargs...)
	var out Facets
	q := func(sel, group string, dst *[]Facet) error {
		return eachRow(ctx, s.db, `SELECT `+sel+`, COUNT(*)`+summaryFrom+group, args, func(r *sql.Rows) error {
			var f Facet
			if err := r.Scan(&f.Value, &f.Count); err != nil {
				return err
			}
			*dst = append(*dst, f)
			return nil
		})
	}
	if err := q(`t.name`, ` JOIN item_tags it ON it.item_id = i.id JOIN tags t ON t.id = it.tag_id AND t.kind = 'genre'`+where+
		` GROUP BY t.name ORDER BY t.name COLLATE NOCASE`, &out.Genres); err != nil {
		return out, err
	}
	if err := q(`CAST(i.year / 10 * 10 AS TEXT)`, where+` AND i.year > 0 GROUP BY i.year / 10 ORDER BY i.year / 10 DESC`, &out.Decades); err != nil {
		return out, err
	}
	if err := q(`i.content_rating`, where+` AND COALESCE(i.content_rating, '') != '' GROUP BY i.content_rating ORDER BY `+ratingCase("i.content_rating")+`, i.content_rating`, &out.ContentRatings); err != nil {
		return out, err
	}
	var present []Facet
	if err := q(letterExpr, where+` GROUP BY 1`, &present); err != nil {
		return out, err
	}
	have := map[string]bool{}
	for _, f := range present {
		have[f.Value] = true
	}
	if have["#"] {
		out.Letters = append(out.Letters, Letter{Letter: "#"})
	}
	for c := 'A'; c <= 'Z'; c++ {
		l := string(c)
		if !have[l] {
			continue
		}
		var n int
		if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*)`+summaryFrom+where+` AND i.sort_title COLLATE NOCASE < ?`, append(args, l)...).Scan(&n); err != nil {
			return out, err
		}
		out.Letters = append(out.Letters, Letter{Letter: l, Offset: n})
	}
	return out, nil
}
