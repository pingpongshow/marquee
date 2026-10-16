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
	// HideRecordings hides the libraries Live TV recordings are filed in (a profile that may
	// not view DVR recordings, LIVE-5).
	HideRecordings bool
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
	if a.HideRecordings {
		conds = append(conds, "i.library_id NOT IN (SELECT id FROM libraries WHERE recordings = 1)")
	}
	if lvl := RatingLevel(a.MaxRating); lvl > 0 {
		// A Live TV recording with no rating of its own (or its show's) has the guide's rating
		// for the programme, when the guide gave one (LIVE-5).
		conds = append(conds, "(i.type NOT IN ('movie','show','season','episode') OR "+
			"(CASE WHEN COALESCE(i.content_rating, p.content_rating, g.content_rating) IS NOT NULL THEN "+
			ratingCase("COALESCE(i.content_rating, p.content_rating, g.content_rating)")+
			" WHEN i.library_id IN (SELECT id FROM libraries WHERE recordings = 1) THEN COALESCE((SELECT MAX("+ratingCase("r.rating")+
			") FROM dvr_recordings r JOIN media_files f ON f.path = r.path JOIN media_versions v ON v.id = f.version_id JOIN items e ON e.id = v.item_id"+
			" WHERE r.path <> '' AND r.rating <> '' AND (e.id = i.id OR e.parent_id = i.id OR e.grandparent_id = i.id)), 99) ELSE 99 END) <= ?)")
		args = append(args, lvl)
	}
	if len(conds) == 0 {
		return "1", nil
	}
	return strings.Join(conds, " AND "), args
}
