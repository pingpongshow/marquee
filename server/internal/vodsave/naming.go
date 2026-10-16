package vodsave

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"marquee/internal/scanner/naming"
)

// Saved files follow the library's Radarr/Sonarr layout:
//
//	{movies}/{Title} ({Year})/{Title} ({Year}) WEBDL-1080p.mkv
//	{shows}/{Show} ({Year})/Season 1/{Show} - S01E01 - {Episode} WEBDL-1080p.mkv
//
// An existing folder for the same film or show is reused, with its spelling.

// maxComponent is the longest a folder or file name may be, in bytes (file systems allow
// 255; the extension and ".part" fit in the rest).
const maxComponent = 180

// Naming is what a saved file's path is made from.
type Naming struct {
	Kind string // movie, episode
	// Folder is the film's or show's folder when one exists already (FindFolder); else one
	// is made in Root.
	Root, Folder string
	Title        string // the film, or the episode's title
	Year         int    // the film's or show's
	Show         string
	Season       int
	Episode      int
	// Width and Height are the saved video's (0 = unknown: no quality tag).
	Width, Height int
	Ext           string
}

// Path is where a film or episode is saved.
func (n Naming) Path() string {
	tag := ""
	if r := Resolution(n.Width, n.Height); r > 0 {
		tag = fmt.Sprintf(" WEBDL-%dp", r)
	}
	ext := "." + strings.TrimPrefix(n.Ext, ".")
	if n.Kind == "movie" {
		name := Sanitize(titleWithYear(Sanitize(n.Title, maxComponent-8), n.Year), maxComponent)
		folder := n.Folder
		if folder == "" {
			folder = filepath.Join(n.Root, name)
		}
		return filepath.Join(folder, name+tag+ext)
	}
	folder := n.Folder
	if folder == "" {
		folder = filepath.Join(n.Root, Sanitize(titleWithYear(Sanitize(n.Show, maxComponent-8), n.Year), maxComponent))
	}
	name := fmt.Sprintf("%s - S%02dE%02d", Sanitize(n.Show, 80), n.Season, n.Episode)
	if t := Sanitize(n.Title, maxComponent); strings.TrimSpace(n.Title) != "" && t != "Untitled" {
		name = Sanitize(name+" - "+t, maxComponent-len(tag))
	}
	return filepath.Join(folder, fmt.Sprintf("Season %d", n.Season), name+tag+ext)
}

// Resolution is the standard resolution (2160, 1080, 720, 576, 480) nearest a video's size;
// the width counts too, so a letterboxed 1920×800 film is 1080p. 0 when unknown.
func Resolution(width, height int) int {
	if height <= 0 && width <= 0 {
		return 0
	}
	best, bestDiff := 0, 1<<30
	for _, r := range []int{2160, 1080, 720, 576, 480} {
		if d := abs(height - r); d < bestDiff {
			best, bestDiff = r, d
		}
	}
	switch {
	case width >= 3200:
		best = max(best, 2160)
	case width >= 1800:
		best = max(best, 1080)
	case width >= 1200:
		best = max(best, 720)
	}
	return best
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

var (
	spaces = regexp.MustCompile(`\s+`)
	// Tags servers put before titles: "EN - ", "|EN| ", "[4K] ", "NF: ".
	junkPrefix = regexp.MustCompile(`(?i)^(?:\|[a-z0-9 ]{1,6}\|\s*|\[[^\]]{1,12}\]\s*|(?:[A-Z]{2,3}|4K|UHD|FHD|HD|SD)(?:-[A-Z]{2})?\s*[|\-:]\s+)`)
	// Tags after titles: "HEVC", "MULTI-SUB", "[4K]", "1080p", "(2019)".
	junkSuffix = regexp.MustCompile(`(?i)\s*(?:\[[^\]]{1,20}\]|\((?:4K|UHD|HD|FHD|HEVC|MULTI[- ]?SUBS?)\)|\b(?:HEVC|H\.?26[45]|x26[45]|MULTI[- ]?SUBS?|MULTI|4K|UHD|FHD|HDR10\+?|HDR|DV|2160p|1080p|720p|480p|WEB-?DL|WEBRip|BluRay)\b|[-|]\s*$)\s*$`)
	yearSuffix = regexp.MustCompile(`\s*\((\d{4})\)\s*$`)
	reserved   = map[string]bool{"CON": true, "PRN": true, "AUX": true, "NUL": true,
		"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true, "COM6": true, "COM7": true, "COM8": true, "COM9": true,
		"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true, "LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true}
)

// CleanTitle removes what servers add to a title: language and quality tags before and
// after it, and the year after it (returned when there is one, even twice).
func CleanTitle(title string) (string, int) {
	t := strings.TrimSpace(title)
	for {
		n := junkPrefix.ReplaceAllString(t, "")
		if n == t || strings.TrimSpace(n) == "" {
			break
		}
		t = strings.TrimSpace(n)
	}
	year := 0
	for {
		n := t
		if m := yearSuffix.FindStringSubmatch(n); m != nil {
			y, _ := strconv.Atoi(m[1])
			if y > 1880 && y < 2200 {
				if year == 0 {
					year = y
				}
				n = yearSuffix.ReplaceAllString(n, "")
			}
		}
		n = strings.TrimSpace(junkSuffix.ReplaceAllString(n, ""))
		if n == t || n == "" {
			break
		}
		t = n
	}
	return strings.TrimSpace(spaces.ReplaceAllString(t, " ")), year
}

// Sanitize makes a name safe on every file system: ":" becomes " -" (as in "Alien - Earth"),
// / \ * ? " < > | and control characters go, spaces collapse, no leading or trailing dots or
// spaces, no reserved Windows names, and at most max bytes (cut on a character). Curly
// quotes and other Unicode stay.
func Sanitize(s string, max int) string {
	s = strings.ToValidUTF8(s, "")
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == ':':
			b.WriteString(" -")
		case strings.ContainsRune(`/\*?"<>|`, r):
		case unicode.IsControl(r):
			b.WriteByte(' ')
		default:
			b.WriteRune(r)
		}
	}
	s = strings.TrimSpace(spaces.ReplaceAllString(b.String(), " "))
	if len(s) > max {
		cut := max
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		s = strings.TrimSpace(s[:cut])
	}
	s = strings.Trim(s, ". ")
	if s == "" {
		s = "Untitled"
	}
	if base, _, _ := strings.Cut(s, "."); reserved[strings.ToUpper(base)] {
		s = "_" + s
	}
	return s
}

func titleWithYear(title string, year int) string {
	if year > 0 {
		return fmt.Sprintf("%s (%d)", title, year)
	}
	return title
}

// FindFolder is an existing folder for a film or show: the library's (by TMDB id) when it
// is still there, else one in root whose name matches the title, with or without the year,
// ignoring case. "" when there is none.
func FindFolder(ctx context.Context, db *sql.DB, kind, root, title string, year, tmdbID int) string {
	if tmdbID > 0 && db != nil {
		if f := libraryFolder(ctx, db, kind, tmdbID); f != "" {
			return f
		}
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return ""
	}
	want := strings.ToLower(Sanitize(title, maxComponent))
	withYear := strings.ToLower(Sanitize(titleWithYear(title, year), maxComponent))
	var loose string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := strings.ToLower(e.Name())
		if name == withYear || (year == 0 && name == want) {
			return filepath.Join(root, e.Name())
		}
		if loose == "" && name == want {
			loose = e.Name() // an older folder without the year
		}
		if loose == "" && year == 0 {
			if p := naming.ParseShowFolder(e.Name()); strings.EqualFold(Sanitize(p.Title, maxComponent), Sanitize(title, maxComponent)) {
				loose = e.Name()
			}
		}
	}
	if loose != "" {
		return filepath.Join(root, loose)
	}
	return ""
}

// libraryFolder is the folder the library keeps a film or show in, found by its TMDB id.
func libraryFolder(ctx context.Context, db *sql.DB, kind string, tmdbID int) string {
	var q string
	if kind == "movie" {
		q = `SELECT f.path FROM external_ids x JOIN items i ON i.id=x.item_id AND i.type='movie'
			JOIN media_versions v ON v.item_id=i.id JOIN media_files f ON f.version_id=v.id
			WHERE x.provider='tmdb' AND x.value=? LIMIT 1`
	} else {
		q = `SELECT f.path FROM external_ids x JOIN items i ON i.id=x.item_id AND i.type='show'
			JOIN items e ON e.grandparent_id=i.id AND e.type='episode'
			JOIN media_versions v ON v.item_id=e.id JOIN media_files f ON f.version_id=v.id
			WHERE x.provider='tmdb' AND x.value=? LIMIT 1`
	}
	var p string
	if err := db.QueryRowContext(ctx, q, strconv.Itoa(tmdbID)).Scan(&p); err != nil {
		return ""
	}
	dir := filepath.Dir(p)
	if kind != "movie" {
		if _, ok := naming.ParseSeasonFolder(filepath.Base(dir)); ok {
			dir = filepath.Dir(dir)
		}
	}
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return ""
	}
	return dir
}

// existingFile is a video already saved for a film (any file in its folder) or an episode
// (the same season and episode in its season folder); "" when there is none.
func existingFile(path, kind string, season, episode int) string {
	dir := filepath.Dir(path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || strings.HasSuffix(name, ".part") || !videoExts[strings.ToLower(strings.TrimPrefix(filepath.Ext(name), "."))] {
			continue
		}
		if kind == "movie" {
			return filepath.Join(dir, name)
		}
		if ep, ok := naming.ParseEpisode(name); ok && ep.Season == season && len(ep.Episodes) > 0 && ep.Episodes[0] == episode {
			return filepath.Join(dir, name)
		}
	}
	return ""
}

var videoExts = map[string]bool{"mkv": true, "mp4": true, "m4v": true, "avi": true, "mov": true, "wmv": true, "ts": true,
	"m2ts": true, "mts": true, "mpg": true, "mpeg": true, "webm": true, "flv": true}

// freeName is path, or "name (2).ext" and so on when something is already there: a save
// never overwrites a file.
func freeName(path string) string {
	if _, err := os.Lstat(path); os.IsNotExist(err) {
		return path
	}
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(path, ext)
	for i := 2; ; i++ {
		p := fmt.Sprintf("%s (%d)%s", base, i, ext)
		if _, err := os.Lstat(p); os.IsNotExist(err) {
			return p
		}
	}
}

// keptContainers are files kept as they are with quality "original"; anything else (MPEG-TS,
// FLV, unknown) is remuxed to MKV, every stream copied.
var keptContainers = map[string]bool{"mkv": true, "mp4": true, "m4v": true, "mov": true, "webm": true, "avi": true}

// outputExt is the saved file's extension for a source extension and quality.
func outputExt(srcExt, quality string) string {
	srcExt = strings.ToLower(strings.TrimPrefix(srcExt, "."))
	if quality == "original" && keptContainers[srcExt] {
		return srcExt
	}
	return "mkv"
}
