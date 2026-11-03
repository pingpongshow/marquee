package intown

import (
	"regexp"
	"slices"
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// Artist is one library artist to find an attraction for.
type Artist struct {
	Name, MBID string
	// NameShared: the library has another artist with this name, so a name alone can't say
	// which one a match would be.
	NameShared bool
}

var suffixThe = regexp.MustCompile(`(?i),\s*the$`)

// normalizeName makes names comparable: case, accents, "&" against "and", punctuation and
// spacing don't count, and "Killers, The" is "The Killers". It's exact otherwise: "The Who"
// isn't "Who", so another band with a similar name stays another band, and initials stay
// initials: "M.I.A." isn't "Mia". A name that's all punctuation ("!!!") is kept, in lower case,
// as it's all there is to compare.
func normalizeName(name string) string {
	name = keepInitials(strings.TrimSpace(name))
	if suffixThe.MatchString(name) {
		name = "the " + suffixThe.ReplaceAllString(name, "")
	}
	// A chain holds state, so each call has its own.
	plain, _, err := transform.String(transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC), name)
	if err != nil {
		plain = name
	}
	var b strings.Builder
	space := false
	for _, r := range strings.ToLower(plain) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == initialDot:
			if space && b.Len() > 0 {
				b.WriteByte(' ')
			}
			space = false
			b.WriteRune(r)
		case r == '&':
			if b.Len() > 0 {
				b.WriteByte(' ')
			}
			b.WriteString("and")
			space = true
		case unicode.IsSpace(r) || r == '-' || r == '_' || r == '/':
			space = true
		}
	}
	if b.Len() == 0 {
		return strings.ToLower(name)
	}
	return b.String()
}

// initialDot stands for the dots of initials, which punctuation would otherwise lose.
const initialDot = '·'

// keepInitials marks words that are initials, two or more single letters with dots between
// them: "M.I.A." and "M.I.A" but not "T." or "Dr.".
func keepInitials(name string) string {
	words := strings.Fields(name)
	changed := false
	for i, w := range words {
		parts := strings.Split(strings.TrimSuffix(w, "."), ".")
		if len(parts) < 2 {
			continue
		}
		initials := true
		for _, p := range parts {
			r := []rune(p)
			initials = initials && len(r) == 1 && unicode.IsLetter(r[0])
		}
		if initials {
			words[i] = strings.Join(parts, string(initialDot))
			changed = true
		}
	}
	if !changed {
		return name
	}
	return strings.Join(words, " ")
}

// matchAttraction picks the attraction that is this artist, or none. A MusicBrainz id settles
// it. Failing that, the name must be exactly the same once normalised, with one attraction of
// that name and only one artist in the library it could be; anything less isn't a match, as a
// wrong band's shows would be worse than none. An attraction whose id says it's someone else
// is never this artist.
func matchAttraction(a Artist, candidates []Attraction) (Attraction, bool) {
	music := make([]Attraction, 0, len(candidates))
	for _, c := range candidates {
		if c.Music && c.ID != "" {
			music = append(music, c)
		}
	}
	if a.MBID != "" {
		var hits []Attraction
		for _, c := range music {
			if slices.Contains(c.MBIDs, a.MBID) {
				hits = append(hits, c)
			}
		}
		if len(hits) == 1 {
			return hits[0], true
		}
		if len(hits) > 1 {
			return Attraction{}, false
		}
	}
	if a.NameShared {
		return Attraction{}, false
	}
	want := normalizeName(a.Name)
	var named []Attraction
	for _, c := range music {
		if normalizeName(c.Name) == want {
			named = append(named, c)
		}
	}
	if len(named) != 1 {
		return Attraction{}, false
	}
	// The artist has an id and this attraction has a different one: someone else.
	if a.MBID != "" && len(named[0].MBIDs) > 0 {
		return Attraction{}, false
	}
	return named[0], true
}

// artistKey identifies an artist across libraries and rescans, since item ids don't.
func artistKey(name, mbid string) string {
	if mbid != "" {
		return "mbid:" + mbid
	}
	return "name:" + normalizeName(name)
}
