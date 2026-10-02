package items

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Smart playlists (MUSIC-8): rules stored as JSON on the playlist and turned into SQL when
// the playlist is read, so they always reflect the library and the listener's history.

var ErrSmartPlaylist = errors.New("smart playlists fill themselves from their rules")

type Condition struct {
	Field string `json:"field"`
	Op    string `json:"op"`
	Value string `json:"value"`
}

type SmartRules struct {
	Match      string      `json:"match"` // all, any
	Conditions []Condition `json:"conditions"`
	Sort       string      `json:"sort,omitempty"`
	Limit      int         `json:"limit,omitempty"`
}

// fieldExpr maps a rule field to SQL over aliases i (item), p (parent), g (grandparent),
// us (the user's state) and so (sonic analysis). Numeric fields are flagged.
func fieldExpr(field string) (expr string, numeric bool, err error) {
	switch field {
	case "genre":
		return "", false, nil // handled with EXISTS
	case "artist":
		return "COALESCE(i.artist_credit, g.title, '')", false, nil
	case "album":
		return "COALESCE(p.title, '')", false, nil
	case "title":
		return "i.title", false, nil
	case "year":
		return "COALESCE(i.year, p.year, 0)", true, nil
	case "rating":
		return "COALESCE(us.rating, 0)", true, nil
	case "playCount":
		return "COALESCE(us.play_count, 0)", true, nil
	case "lastPlayedDays":
		return "COALESCE(julianday('now') - julianday(us.last_viewed_at), 100000)", true, nil
	case "addedDays":
		return "(julianday('now') - julianday(i.added_at))", true, nil
	case "bpm":
		return "COALESCE(so.bpm, 0)", true, nil
	case "energy":
		return "COALESCE(so.energy, 0)", true, nil
	case "key":
		return "COALESCE(so.musical_key || ' ' || so.mode, '')", false, nil
	case "durationSeconds":
		return "COALESCE(i.duration_ms, 0) / 1000.0", true, nil
	}
	return "", false, fmt.Errorf("unknown field %q", field)
}

var smartSorts = map[string]string{
	"random": "random()", "title": "i.sort_title COLLATE NOCASE", "artist": "COALESCE(i.artist_credit, g.title) COLLATE NOCASE, p.title, i.idx",
	"year": "COALESCE(i.year, p.year)", "-year": "COALESCE(i.year, p.year) DESC", "added": "i.added_at", "-added": "i.added_at DESC",
	"rating": "COALESCE(us.rating, 0)", "-rating": "COALESCE(us.rating, 0) DESC", "playCount": "COALESCE(us.play_count, 0)",
	"-playCount": "COALESCE(us.play_count, 0) DESC", "lastPlayed": "us.last_viewed_at", "-lastPlayed": "us.last_viewed_at DESC",
}

// Validate checks rules before they are saved.
func (r SmartRules) Validate() error {
	if r.Match != "all" && r.Match != "any" {
		return errors.New("match must be all or any")
	}
	if len(r.Conditions) == 0 || len(r.Conditions) > 20 {
		return errors.New("1–20 conditions")
	}
	for _, c := range r.Conditions {
		_, numeric, err := fieldExpr(c.Field)
		if err != nil {
			return err
		}
		switch c.Op {
		case "is", "isNot", "contains", "notContains":
		case "gt", "lt":
			if !numeric {
				return fmt.Errorf("%s can't be compared with greater/less than", c.Field)
			}
			if _, err := strconv.ParseFloat(c.Value, 64); err != nil {
				return fmt.Errorf("%s needs a number", c.Field)
			}
		default:
			return fmt.Errorf("unknown operator %q", c.Op)
		}
	}
	if _, ok := smartSorts[r.Sort]; r.Sort != "" && !ok {
		return fmt.Errorf("unknown sort %q", r.Sort)
	}
	return nil
}

// where builds the SQL condition for the rules (with args).
func (r SmartRules) where() (string, []any) {
	var parts []string
	var args []any
	for _, c := range r.Conditions {
		if c.Field == "genre" {
			cond := `EXISTS (SELECT 1 FROM item_tags it JOIN tags t ON t.id = it.tag_id AND t.kind = 'genre'
				WHERE it.item_id IN (i.id, i.parent_id, i.grandparent_id) AND `
			switch c.Op {
			case "is", "isNot":
				cond += `t.name = ? COLLATE NOCASE)`
				args = append(args, c.Value)
			default:
				cond += `t.name LIKE ?)`
				args = append(args, "%"+c.Value+"%")
			}
			if c.Op == "isNot" || c.Op == "notContains" {
				cond = "NOT " + cond
			}
			parts = append(parts, cond)
			continue
		}
		expr, numeric, _ := fieldExpr(c.Field)
		switch c.Op {
		case "is":
			if numeric {
				parts = append(parts, expr+" = ?")
				v, _ := strconv.ParseFloat(c.Value, 64)
				args = append(args, v)
			} else {
				parts = append(parts, expr+" = ? COLLATE NOCASE")
				args = append(args, c.Value)
			}
		case "isNot":
			parts = append(parts, expr+" != ? COLLATE NOCASE")
			args = append(args, c.Value)
		case "contains":
			parts = append(parts, expr+" LIKE ?")
			args = append(args, "%"+c.Value+"%")
		case "notContains":
			parts = append(parts, expr+" NOT LIKE ?")
			args = append(args, "%"+c.Value+"%")
		case "gt", "lt":
			v, _ := strconv.ParseFloat(c.Value, 64)
			parts = append(parts, expr+map[string]string{"gt": " > ?", "lt": " < ?"}[c.Op])
			args = append(args, v)
		}
	}
	join := " AND "
	if r.Match == "any" {
		join = " OR "
	}
	return "(" + strings.Join(parts, join) + ")", args
}

// smartFrom is the FROM clause smart playlists evaluate over.
func smartFrom(uid int64) string {
	return summaryFrom + ` LEFT JOIN user_item_state us ON us.item_id = i.id AND us.user_id = ` + strconv.FormatInt(uid, 10) +
		` LEFT JOIN sonic so ON so.item_id = i.id`
}

// SmartItems evaluates a smart playlist's rules for acc.
func (s *Store) SmartItems(ctx context.Context, acc Access, kind string, rules SmartRules, offset, limit int) ([]Summary, int, error) {
	ac, aargs := acc.clause()
	rc, rargs := rules.where()
	types := "'track'"
	if kind == "video" {
		types = "'movie', 'episode', 'video'"
	}
	from := smartFrom(acc.UserID) + ` WHERE i.type IN (` + types + `) AND i.extra_type IS NULL AND ` + ac + ` AND ` + rc
	args := append(append([]any{}, aargs...), rargs...)
	order := smartSorts[rules.Sort]
	if order == "" {
		order = smartSorts["artist"]
	}
	cap := rules.Limit
	if cap <= 0 || cap > 2000 {
		cap = 2000
	}
	var total int
	if err := s.db.QueryRowContext(ctx, `SELECT MIN(COUNT(*), ?)`+from, append([]any{cap}, args...)...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if offset >= total {
		return []Summary{}, total, nil
	}
	limit = min(limit, total-offset)
	rows, err := s.db.QueryContext(ctx, `SELECT `+cols(acc.UserID)+from+` ORDER BY `+order+` LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	list, err := collect(rows)
	return list, total, err
}

func parseRules(raw string) *SmartRules {
	if raw == "" {
		return nil
	}
	var r SmartRules
	if json.Unmarshal([]byte(raw), &r) != nil {
		return nil
	}
	return &r
}
