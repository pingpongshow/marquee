package items

import (
	"strings"
)

// Access limits which items a user can see (USER-2).
type Access struct {
	// UserID adds this user's watch state to results (0 = none).
	UserID int64
	// LibraryIDs restricts to these libraries; nil means all libraries.
	LibraryIDs []int64
	// MaxRating hides movies/TV rated above it (and unrated ones). "" = no limit.
	MaxRating string
}

// Unrestricted sees everything (admins, and internal use).
var Unrestricted = Access{}

// ratingLevels orders US movie and TV ratings on one scale.
var ratingLevels = map[string]int{
	"TV-Y": 1, "G": 2, "TV-G": 2, "TV-Y7": 2, "TV-Y7-FV": 2, "PG": 3, "TV-PG": 3,
	"PG-13": 4, "TV-14": 4, "R": 5, "TV-MA": 5, "NC-17": 6,
}

// RatingLevel returns the level of a rating, or 0 if unknown.
func RatingLevel(r string) int { return ratingLevels[strings.ToUpper(strings.TrimSpace(r))] }

func ratingCase(expr string) string {
	var b strings.Builder
	b.WriteString("(CASE UPPER(" + expr + ")")
	for r, l := range ratingLevels {
		b.WriteString(" WHEN '" + r + "' THEN ")
		b.WriteString(itoa(l))
	}
	b.WriteString(" ELSE 99 END)")
	return b.String()
}

func itoa(n int) string {
	if n < 10 {
		return string(rune('0' + n))
	}
	return "99"
}

// clause returns a SQL condition (for the summary query aliases i, p, g) and its args.
func (a Access) clause() (string, []any) {
	var conds []string
	var args []any
	if a.LibraryIDs != nil {
		if len(a.LibraryIDs) == 0 {
			conds = append(conds, "0")
		} else {
			conds = append(conds, "i.library_id IN (?"+strings.Repeat(",?", len(a.LibraryIDs)-1)+")")
			for _, id := range a.LibraryIDs {
				args = append(args, id)
			}
		}
	}
	if lvl := RatingLevel(a.MaxRating); lvl > 0 {
		conds = append(conds, "(i.type NOT IN ('movie','show','season','episode') OR "+
			ratingCase("COALESCE(i.content_rating, p.content_rating, g.content_rating)")+" <= ?)")
		args = append(args, lvl)
	}
	if len(conds) == 0 {
		return "1", nil
	}
	return strings.Join(conds, " AND "), args
}
