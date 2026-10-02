// Package naming parses titles, years, season/episode numbers and provider IDs out of
// media file and folder names. It handles Plex/Sonarr/Radarr naming as well as common
// scene and fansub release names.
package naming

import (
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// IDs are provider IDs embedded in names, e.g. "{tmdb-603}", "[imdbid-tt0133093]", "{tvdb-81189}".
type IDs struct {
	TMDB, TVDB, IMDB, AniDB string
}

type Movie struct {
	Title   string
	Year    int
	Edition string // from "{edition-Director's Cut}"
	IDs     IDs
}

type Episode struct {
	Season   int // -1 when only an absolute number is known
	Episodes []int
	Absolute int // anime absolute episode number, 0 if none
	AirDate  string
	Title    string // episode title when present in the filename
}

type Show struct {
	Title string
	Year  int
	IDs   IDs
}

var (
	reIDTag       = regexp.MustCompile(`(?i)[\[{](tmdb|tmdbid|tvdb|tvdbid|imdb|imdbid|anidb|anidbid)[-=]([a-z0-9]+)[\]}]`)
	reEdition     = regexp.MustCompile(`(?i)\{edition-([^}]+)\}`)
	reYearParen   = regexp.MustCompile(`\((\d{4})\)`)
	reYearBracket = regexp.MustCompile(`\[(\d{4})\]`)
	reBareYear    = regexp.MustCompile(`(?:^|[\s._(\[-])((?:19|20)\d{2})(?:$|[\s._)\]-])`)
	reEmptyParens = regexp.MustCompile(`\(\s*\)`)
	reBrackets    = regexp.MustCompile(`\[[^\]]*\]|\{[^}]*\}`)
	reSpaces      = regexp.MustCompile(`\s+`)

	// Release-name noise: everything from the first of these tokens onwards is dropped.
	reNoise = regexp.MustCompile(`(?i)(?:^|[\s._-])(2160p|1080p|1080i|720p|576p|480p|4k|uhd|bluray|blu-ray|bdrip|brrip|bdremux|remux|web-?dl|webrip|web|hdtv|hdrip|dvdrip|dvdscr|dvd|xvid|divx|x264|x265|h\.?264|h\.?265|hevc|avc|av1|10bit|hdr10?|dv|dolby|atmos|truehd|dts(?:-hd)?|ddp?5\.1|aac(?:2\.0)?|ac3|proper|repack|extended|unrated|remastered|imax|internal|limited|multi|subbed|dubbed)(?:$|[\s._-])`)

	reSxxEyy    = regexp.MustCompile(`(?i)(?:^|[^a-z0-9])S(\d{1,4})[\s._-]?E(\d{1,4})((?:[._-]?E\d{1,4}|-\d{1,4}\b)*)`)
	reMultiEp   = regexp.MustCompile(`(?i)-?E?(\d{1,4})`)
	reNxNN      = regexp.MustCompile(`(?i)(?:^|[\s._\[(-])(\d{1,2})x(\d{1,3})(?:$|[\s._\])-])`)
	reAirDate   = regexp.MustCompile(`(?:^|[\s._(\[-])((?:19|20)\d{2})[.\-_ ](\d{2})[.\-_ ](\d{2})(?:$|[\s._)\]-])`)
	reFansubEp  = regexp.MustCompile(`^\s*\[[^\]]+\]\s*(.+?)\s+-\s+(\d{1,4})(?:v\d)?(?:\s|\[|\(|$)`)
	reFansubAlt = regexp.MustCompile(`^\s*\[[^\]]+\]\s*(.+?)-(\d{1,4})(?:v\d)?\s*(?:\[|\(|$)`)
	reEpWord    = regexp.MustCompile(`(?i)(?:^|[\s._-])(?:ep|episode|e)[\s._-]?(\d{1,4})(?:$|[\s._-])`)
	reSeasonDir = regexp.MustCompile(`(?i)^(?:season|series|s)[\s._-]*(\d{1,4})$`)
	reSpecials  = regexp.MustCompile(`(?i)^specials?$`)
	reNthSeason = regexp.MustCompile(`(?i)\s+(?:(\d+)(?:st|nd|rd|th)\s+season|season\s+(\d+))\s*$`)
)

// ParseIDs extracts provider IDs from a name.
func ParseIDs(name string) IDs {
	var ids IDs
	for _, m := range reIDTag.FindAllStringSubmatch(name, -1) {
		v := m[2]
		switch strings.ToLower(strings.TrimSuffix(strings.ToLower(m[1]), "id")) {
		case "tmdb":
			ids.TMDB = v
		case "tvdb":
			ids.TVDB = v
		case "imdb":
			ids.IMDB = v
		case "anidb":
			ids.AniDB = v
		}
	}
	return ids
}

// ParseMovie parses a movie from its folder name, falling back to the file name when
// the folder carries no usable title. Pass folder="" for files at the library root.
func ParseMovie(folder, file string) Movie {
	base := strings.TrimSuffix(file, filepath.Ext(file))
	m := parseTitleYear(folder)
	f := parseTitleYear(base)
	// Prefer the file name when only it has a year: covers collection folders
	// ("Studio Ghibli Film Collection/…") and placeholder folders ("Untitled Film ()").
	if m.Title == "" || (m.Year == 0 && f.Year != 0) {
		m = f
	}
	m.IDs = mergeIDs(ParseIDs(folder), ParseIDs(base))
	if e := reEdition.FindStringSubmatch(base); e != nil {
		m.Edition = strings.TrimSpace(e[1])
	} else if e := reEdition.FindStringSubmatch(folder); e != nil {
		m.Edition = strings.TrimSpace(e[1])
	}
	return m
}

// ParseShowFolder parses a show's folder name, e.g. "Archer (2009)" or "Pachinko".
func ParseShowFolder(name string) Show {
	m := parseTitleYear(name)
	return Show{Title: m.Title, Year: m.Year, IDs: ParseIDs(name)}
}

// ParseSeasonFolder returns the season number for "Season 01", "S2", "Specials".
func ParseSeasonFolder(name string) (int, bool) {
	name = strings.TrimSpace(name)
	if reSpecials.MatchString(name) {
		return 0, true
	}
	if m := reSeasonDir.FindStringSubmatch(name); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n, true
	}
	return 0, false
}

// ParseEpisode parses season/episode information from an episode file name.
// ok is false when no episode numbering could be found.
func ParseEpisode(file string) (ep Episode, ok bool) {
	base := strings.TrimSuffix(file, filepath.Ext(file))
	ep.Season = -1

	if m := reSxxEyy.FindStringSubmatchIndex(base); m != nil {
		ep.Season, _ = strconv.Atoi(base[m[2]:m[3]])
		first, _ := strconv.Atoi(base[m[4]:m[5]])
		ep.Episodes = []int{first}
		if m[6] >= 0 {
			for _, mm := range reMultiEp.FindAllStringSubmatch(base[m[6]:m[7]], -1) {
				n, _ := strconv.Atoi(mm[1])
				if n > ep.Episodes[len(ep.Episodes)-1] && n-first < 50 {
					ep.Episodes = expandRange(ep.Episodes, n)
				}
			}
		}
		ep.Title = episodeTitle(base[m[1]:])
		return ep, true
	}
	if m := reNxNN.FindStringSubmatchIndex(base); m != nil {
		ep.Season, _ = strconv.Atoi(base[m[2]:m[3]])
		n, _ := strconv.Atoi(base[m[4]:m[5]])
		ep.Episodes = []int{n}
		ep.Title = episodeTitle(base[m[1]:])
		return ep, true
	}
	if m := reAirDate.FindStringSubmatch(base); m != nil {
		ep.AirDate = m[1] + "-" + m[2] + "-" + m[3]
		return ep, true
	}
	if m := reFansubEp.FindStringSubmatch(base); m != nil {
		ep.Absolute, _ = strconv.Atoi(m[2])
		ep.Episodes = []int{ep.Absolute}
		return ep, true
	}
	if m := reFansubAlt.FindStringSubmatch(base); m != nil {
		ep.Absolute, _ = strconv.Atoi(m[2])
		ep.Episodes = []int{ep.Absolute}
		return ep, true
	}
	if m := reEpWord.FindStringSubmatch(base); m != nil {
		ep.Absolute, _ = strconv.Atoi(m[1])
		ep.Episodes = []int{ep.Absolute}
		return ep, true
	}
	return ep, false
}

// FansubShow extracts the show title and season from a fansub release name such as
// "[Erai-raws] Kaijuu 8 Gou 2nd Season - 10 [720p]". Returns ok=false for other names.
func FansubShow(file string) (title string, season int, ok bool) {
	base := strings.TrimSuffix(file, filepath.Ext(file))
	m := reFansubEp.FindStringSubmatch(base)
	if m == nil {
		m = reFansubAlt.FindStringSubmatch(base)
	}
	if m == nil {
		return "", 0, false
	}
	title = strings.TrimSpace(m[1])
	season = 1
	if s := reNthSeason.FindStringSubmatch(title); s != nil {
		v := s[1]
		if v == "" {
			v = s[2]
		}
		season, _ = strconv.Atoi(v)
		title = strings.TrimSpace(title[:len(title)-len(s[0])])
	}
	return title, season, true
}

func expandRange(eps []int, upTo int) []int {
	for n := eps[len(eps)-1] + 1; n <= upTo; n++ {
		eps = append(eps, n)
	}
	return eps
}

// episodeTitle extracts a title following the episode marker, e.g. " - Mole Hunt Bluray-1080p".
func episodeTitle(rest string) string {
	rest = strings.TrimLeft(rest, " ._-")
	if i := reNoise.FindStringIndex(rest); i != nil {
		rest = rest[:i[0]]
	}
	rest = reBrackets.ReplaceAllString(rest, "")
	if strings.Count(rest, ".") >= 2 && !strings.Contains(rest, " ") {
		rest = strings.ReplaceAll(rest, ".", " ")
	}
	return strings.Trim(reSpaces.ReplaceAllString(rest, " "), " ._-")
}

// parseTitleYear cleans a folder or file name into a title and year.
func parseTitleYear(s string) Movie {
	s = reIDTag.ReplaceAllString(s, " ")
	s = reEdition.ReplaceAllString(s, " ")
	s = reEmptyParens.ReplaceAllString(s, " ")

	year := 0
	cut := -1
	if m := reYearParen.FindStringSubmatchIndex(s); m != nil {
		year, _ = strconv.Atoi(s[m[2]:m[3]])
		cut = m[0]
	} else if m := reYearBracket.FindStringSubmatchIndex(s); m != nil {
		year, _ = strconv.Atoi(s[m[2]:m[3]])
		cut = m[0]
	}

	// Scene names use dots or underscores instead of spaces.
	if !strings.Contains(s, " ") && (strings.Count(s, ".") >= 2 || strings.Contains(s, "_")) {
		s = strings.NewReplacer(".", " ", "_", " ").Replace(s)
		if cut >= 0 {
			cut = -1
			year = 0
			if m := reYearBracket.FindStringSubmatchIndex(s); m != nil {
				year, _ = strconv.Atoi(s[m[2]:m[3]])
				cut = m[0]
			}
		}
	}

	if cut < 0 {
		if n := reNoise.FindStringIndex(s); n != nil {
			s = s[:n[0]]
		}
		// A bare year: prefer the last one that isn't the whole title ("1917", "2012").
		locs := reBareYear.FindAllStringSubmatchIndex(s, -1)
		for i := len(locs) - 1; i >= 0; i-- {
			l := locs[i]
			before := strings.TrimSpace(s[:l[2]])
			after := strings.TrimSpace(s[l[3]:])
			if before == "" {
				// Leading year ("1955 Godzilla …"): keep it only if more title follows.
				if after != "" && len(locs) == 1 {
					y, _ := strconv.Atoi(s[l[2]:l[3]])
					year = y
					s = after
				}
				break
			}
			year, _ = strconv.Atoi(s[l[2]:l[3]])
			s = s[:l[2]]
			break
		}
	} else {
		s = s[:cut]
	}
	if n := reNoise.FindStringIndex(s); n != nil {
		s = s[:n[0]]
	}
	s = reBrackets.ReplaceAllString(s, " ")
	s = reSpaces.ReplaceAllString(s, " ")
	s = strings.TrimRight(strings.TrimLeft(s, " ._"), " .-_([")
	return Movie{Title: s, Year: year}
}

func mergeIDs(a, b IDs) IDs {
	if a.TMDB == "" {
		a.TMDB = b.TMDB
	}
	if a.TVDB == "" {
		a.TVDB = b.TVDB
	}
	if a.IMDB == "" {
		a.IMDB = b.IMDB
	}
	if a.AniDB == "" {
		a.AniDB = b.AniDB
	}
	return a
}

// Normalize lowercases and strips punctuation for fuzzy title comparison.
func Normalize(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		case r == '&':
			b.WriteString("and")
		}
	}
	return b.String()
}

// SortTitle drops leading articles: "The Matrix" → "Matrix".
func SortTitle(title string) string {
	lower := strings.ToLower(title)
	for _, a := range []string{"the ", "a ", "an "} {
		if strings.HasPrefix(lower, a) && len(title) > len(a) {
			return strings.TrimSpace(title[len(a):])
		}
	}
	return title
}

// ShowTitleFromFile derives a show title (and year) from an episode file that isn't inside
// a show folder, e.g. "Pachinko.S01E01.720p.WEB.mkv" → "Pachinko".
func ShowTitleFromFile(file string) Show {
	base := strings.TrimSuffix(file, filepath.Ext(file))
	if t, _, ok := FansubShow(file); ok {
		return Show{Title: t}
	}
	cut := -1
	for _, re := range []*regexp.Regexp{reSxxEyy, reNxNN, reAirDate} {
		if m := re.FindStringIndex(base); m != nil && (cut < 0 || m[0] < cut) {
			cut = m[0]
		}
	}
	if cut <= 0 {
		return Show{}
	}
	m := parseTitleYear(base[:cut])
	return Show{Title: m.Title, Year: m.Year, IDs: ParseIDs(base)}
}

// PartIndex detects stacked multi-part files: "Movie cd1.avi", "Movie - part2.mkv" → 1, 2.
func PartIndex(file string) int {
	if m := rePart.FindStringSubmatch(strings.TrimSuffix(file, filepath.Ext(file))); m != nil {
		n, _ := strconv.Atoi(m[1])
		return n
	}
	return 0
}

var rePart = regexp.MustCompile(`(?i)[\s._-](?:cd|disc|disk|dvd|part|pt)[\s._-]?(\d{1,2})$`)

// MatchScore rates how well a candidate title/year fits a title/year parsed from a file or
// folder name: 3 for the same title (ignoring articles and punctuation), 2 when one contains
// the other, 0 otherwise; plus 2 for the same year or 1 for an adjacent year.
func MatchScore(candTitle string, candYear int, wantTitle string, wantYear int) int {
	// Disambiguating years in titles ("ONE PIECE (2023)") aren't part of the name.
	candTitle = reYearParen.ReplaceAllString(candTitle, "")
	c, w := Normalize(SortTitle(candTitle)), Normalize(SortTitle(wantTitle))
	score := 0
	switch {
	case c == "" || w == "":
	case c == w:
		score = 3
	case len(c) >= 4 && len(w) >= 4 && 2*min(len(c), len(w)) >= max(len(c), len(w)) &&
		(strings.Contains(c, w) || strings.Contains(w, c)):
		score = 2 // containment counts only between similar-length titles ("Cell" ⊄ "Gauche the Cellist")
	}
	if candYear > 0 && wantYear > 0 {
		switch d := candYear - wantYear; {
		case d == 0:
			score += 2
		case d == 1 || d == -1:
			score++
		}
	}
	return score
}

var stopWords = map[string]bool{"the": true, "and": true, "of": true, "a": true, "an": true, "in": true, "on": true, "to": true, "for": true, "with": true}

// SharesWord reports whether two titles have a significant word in common.
func SharesWord(a, b string) bool {
	words := func(s string) map[string]bool {
		out := map[string]bool{}
		for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
			if (len(w) >= 3 || strings.ContainsAny(w, "0123456789")) && !stopWords[w] {
				out[w] = true
			}
		}
		return out
	}
	wb := words(b)
	for w := range words(a) {
		if wb[w] {
			return true
		}
	}
	return false
}
