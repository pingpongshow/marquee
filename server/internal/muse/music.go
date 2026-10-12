package muse

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"marquee/internal/items"
	"marquee/internal/soundprint"
)

// MusicLibrary is what a music prompt is matched against.
type MusicLibrary struct {
	Genres  []string         // genre tags, most used first
	Artists map[int64]string // artist id → name
}

type musicAnswer struct {
	Genres          []string `json:"genres"`
	YearFrom        int      `json:"yearFrom"`
	YearTo          int      `json:"yearTo"`
	Artists         []string `json:"artists"`
	LikeArtists     []string `json:"likeArtists"`
	Favourites      bool     `json:"favourites"`
	Unplayed        bool     `json:"unplayed"`
	New             bool     `json:"recentlyAdded"`
	Rediscover      bool     `json:"rediscover"`
	MaxTrackMinutes int      `json:"maxTrackMinutes"`
	TotalMinutes    int      `json:"totalMinutes"`
	Sound           string   `json:"sound"`
	Theme           string   `json:"theme"`
	Name            string   `json:"name"`
}

var musicSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"genres":          strArray(),
		"yearFrom":        map[string]any{"type": "integer"},
		"yearTo":          map[string]any{"type": "integer"},
		"artists":         strArray(),
		"likeArtists":     strArray(),
		"favourites":      map[string]any{"type": "boolean"},
		"unplayed":        map[string]any{"type": "boolean"},
		"recentlyAdded":   map[string]any{"type": "boolean"},
		"rediscover":      map[string]any{"type": "boolean"},
		"maxTrackMinutes": map[string]any{"type": "integer"},
		"totalMinutes":    map[string]any{"type": "integer"},
		"sound":           map[string]any{"type": "string"},
		"theme":           map[string]any{"type": "string"},
		"name":            map[string]any{"type": "string"},
	},
	"required": []string{"genres", "yearFrom", "yearTo", "artists", "likeArtists", "favourites", "unplayed", "recentlyAdded",
		"rediscover", "maxTrackMinutes", "totalMinutes", "sound", "theme", "name"},
}

const musicSystem = `You turn a request for music into playlist filters for a home music library. Answer only with JSON.
Fields:
- genres: genres asked for, ONLY from the library's genre list below, spelled exactly as listed (any one may match). [] when none fits.
- yearFrom, yearTo: release year bounds, 0 when open. "80s" is 1980-1989.
- artists: artists the playlist must be BY ("songs by Queen"), else [].
- likeArtists: artists it should SOUND like ("like Radiohead", "in the style of"), else [].
- favourites: their own favourites. unplayed: music they haven't heard. recentlyAdded: new additions. rediscover: old favourites they haven't played in a while.
- maxTrackMinutes: longest track allowed, 0 when not asked. totalMinutes: playlist length ("an hour of" = 60), 0 when not asked.
- sound: how the music should sound or feel, in a few words for an audio model (mood, energy, instruments, tempo, setting), e.g. "mellow acoustic guitar, rainy evening". Empty when the request is only filters.
- theme: what the LYRICS should be about when the request says so ("songs about the ocean" = "the ocean", "breakup songs" = "breakups"), else "".
- name: a short, catchy playlist name (2-5 words).`

// MusicReading is a parsed music prompt.
type MusicReading struct {
	Query      soundprint.MusicQuery
	Understood []string
	AI         bool
}

// ReadMusic reads a music prompt with the AI when llm is ready, and with keyword matching
// against the library's genres and artists otherwise (or when the answer is unusable).
func ReadMusic(ctx context.Context, llm LLM, lib MusicLibrary, prompt string) MusicReading {
	if llm != nil && llm.Ready() {
		genres := lib.Genres
		if len(genres) > 300 {
			genres = genres[:300]
		}
		var a musicAnswer
		user := fmt.Sprintf("Library genres: %s\n\nRequest: %s", strings.Join(genres, ", "), prompt)
		if err := llm.Chat(ctx, musicSystem, user, musicSchema, &a); err != nil {
			slog.WarnContext(ctx, "muse: AI reading failed; using keywords", "err", err)
		} else if r, ok := validateMusic(a, lib, prompt); ok {
			return r
		}
	}
	return KeywordMusic(lib, prompt)
}

func validateMusic(a musicAnswer, lib MusicLibrary, prompt string) (MusicReading, bool) {
	var q soundprint.MusicQuery
	var lbl []string
	gset := map[string]string{}
	for _, g := range lib.Genres {
		gset[strings.ToLower(g)] = g
	}
	seen := map[string]bool{}
	for _, g := range a.Genres {
		if name, ok := gset[strings.ToLower(strings.TrimSpace(g))]; ok && !seen[name] {
			seen[name] = true
			q.Genres = append(q.Genres, name)
			lbl = append(lbl, name)
		}
	}
	maxYear := time.Now().Year() + 1
	year := func(y int) int {
		if y < 1900 || y > maxYear {
			return 0
		}
		return y
	}
	q.YearFrom, q.YearTo = year(a.YearFrom), year(a.YearTo)
	if q.YearFrom > 0 && q.YearTo > 0 && q.YearFrom > q.YearTo {
		q.YearFrom, q.YearTo = q.YearTo, q.YearFrom
	}
	if l := yearLabel(q.YearFrom, q.YearTo); l != "" {
		lbl = append(lbl, l)
	}
	idx := artistIndex(lib.Artists)
	var unmatched []string
	for _, n := range a.Artists {
		if id, name, ok := idx.find(n); ok {
			q.ArtistIDs = append(q.ArtistIDs, id)
			lbl = append(lbl, "By "+name)
		} else if strings.TrimSpace(n) != "" {
			unmatched = append(unmatched, n)
		}
	}
	for _, n := range a.LikeArtists {
		if id, name, ok := idx.find(n); ok {
			q.LikeArtistIDs = append(q.LikeArtistIDs, id)
			lbl = append(lbl, "Like "+name)
		} else if strings.TrimSpace(n) != "" {
			unmatched = append(unmatched, n)
		}
	}
	q.Favourites, q.Unplayed, q.New, q.Rediscover = a.Favourites, a.Unplayed, a.New, a.Rediscover
	for _, f := range []struct {
		on   bool
		name string
	}{{q.Favourites, "Favourites"}, {q.Unplayed, "Unplayed"}, {q.New, "Recently added"}, {q.Rediscover, "Rediscover"}} {
		if f.on {
			lbl = append(lbl, f.name)
		}
	}
	if a.MaxTrackMinutes >= 1 && a.MaxTrackMinutes <= 60 {
		q.MaxTrackMinutes = a.MaxTrackMinutes
		lbl = append(lbl, fmt.Sprintf("Tracks under %d min", q.MaxTrackMinutes))
	}
	if a.TotalMinutes >= 10 && a.TotalMinutes <= 600 {
		q.TotalMinutes = a.TotalMinutes
		lbl = append(lbl, fmt.Sprintf("About %d min", q.TotalMinutes))
	}
	q.Sound = clip(strings.TrimSpace(strings.Join(append([]string{a.Sound}, unmatched...), " ")), 150)
	q.Theme = clip(strings.TrimSpace(a.Theme), 120)
	if q.Sound != "" {
		lbl = append(lbl, "Sounds "+q.Sound)
	}
	if len(lbl) == 0 && q.Theme == "" {
		return MusicReading{}, false
	}
	q.Title = cleanTitle(a.Name)
	if q.Title == "" {
		q.Title = prompt
	}
	return MusicReading{Query: q, Understood: lbl, AI: true}, true
}

func clip(s string, n int) string {
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

func yearLabel(from, to int) string {
	switch {
	case from > 0 && to == from+9 && from%10 == 0:
		return fmt.Sprintf("%ds", from)
	case from > 0 && from == to:
		return fmt.Sprint(from)
	case from > 0 && to > 0:
		return fmt.Sprintf("%d–%d", from, to)
	case from > 0:
		return fmt.Sprintf("%d or later", from)
	case to > 0:
		return fmt.Sprintf("%d or earlier", to)
	}
	return ""
}

// ---------- artist matching ----------

type artists struct {
	byKey map[string]int64
	names map[int64]string
	keys  []string // longest first
}

// artistKey normalizes a name for matching: lower case, no leading "the", letters and
// digits only.
func artistKey(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.TrimPrefix(s, "the ")
	s = strings.ReplaceAll(s, "&", " and ")
	var b strings.Builder
	space := false
	for _, r := range s {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			space = false
		case !space && b.Len() > 0:
			b.WriteByte(' ')
			space = true
		}
	}
	return strings.TrimSpace(b.String())
}

func artistIndex(m map[int64]string) artists {
	a := artists{byKey: map[string]int64{}, names: m}
	for id, n := range m {
		k := artistKey(n)
		if k == "" {
			continue
		}
		if old, ok := a.byKey[k]; !ok || id < old {
			a.byKey[k] = id
		}
	}
	for k := range a.byKey {
		a.keys = append(a.keys, k)
	}
	sort.Slice(a.keys, func(i, j int) bool {
		if len(a.keys[i]) != len(a.keys[j]) {
			return len(a.keys[i]) > len(a.keys[j])
		}
		return a.keys[i] < a.keys[j]
	})
	return a
}

func (a artists) find(name string) (int64, string, bool) {
	id, ok := a.byKey[artistKey(name)]
	return id, a.names[id], ok
}

// ---------- keyword reading (no AI) ----------

var (
	reAbout = regexp.MustCompile(`\b(?:songs?|tracks?|music|lyrics)\s+about\s+(.+)$`)
	reNoun  = regexp.MustCompile(`\b(breakup|heartbreak|love|party|summer|christmas|road trip|drinking)\s+songs\b`)
)

var (
	favouriteWords  = []string{"my favourites", "my favorites", "my favourite", "my favorite", "favourites", "favorites", "songs i love", "loved tracks"}
	unplayedWords   = []string{"haven't heard", "have not heard", "never heard", "never played", "not played", "unplayed", "unheard", "new to me"}
	newWords        = []string{"recently added", "new additions", "newly added", "just added", "new music", "new arrivals"}
	rediscoverWords = []string{"rediscover", "forgotten favourites", "forgotten favorites", "haven't played in a while", "haven't listened to in a while", "old favourites", "old favorites"}
)

// KeywordMusic reads a prompt without the AI: decades and years, genre and artist names
// as whole words ("like X" and bare names steer the sound; "by X" filters), history words
// and "songs about X" themes. The whole prompt still describes the sound, as before.
func KeywordMusic(lib MusicLibrary, prompt string) MusicReading {
	var q soundprint.MusicQuery
	var lbl []string
	low := " " + strings.ToLower(strings.Join(strings.Fields(prompt), " ")) + " "
	low = strings.NewReplacer("’", "'", ",", " , ", "!", " ", "?", " ").Replace(low)

	// Years and genres: the movie parser's rules, with the music genres.
	vq := items.ParseVideoPrompt(low, lib.Genres)
	q.YearFrom, q.YearTo = vq.YearFrom, vq.YearTo
	if l := yearLabel(q.YearFrom, q.YearTo); l != "" {
		lbl = append(lbl, l)
	}
	gset := map[string]string{}
	for _, g := range lib.Genres {
		gset[strings.ToLower(g)] = g
	}
	seen := map[string]bool{}
	for _, grp := range vq.Genres {
		for _, g := range grp {
			if name, ok := gset[strings.ToLower(g)]; ok && !seen[name] {
				seen[name] = true
				q.Genres = append(q.Genres, name)
				lbl = append(lbl, name)
			}
		}
	}

	// Artists, longest name first; each match is removed so "Simon & Garfunkel" doesn't also match "Simon".
	idx := artistIndex(lib.Artists)
	norm := " " + artistKey(low) + " "
	for _, k := range idx.keys {
		pos := strings.Index(norm, " "+k+" ")
		if pos < 0 {
			continue
		}
		before := strings.TrimSuffix(strings.TrimSpace(norm[:pos]), " the")
		id := idx.byKey[k]
		switch {
		case strings.HasSuffix(before, "by") || strings.HasSuffix(before, "only"):
			q.ArtistIDs = append(q.ArtistIDs, id)
			lbl = append(lbl, "By "+idx.names[id])
		case strings.HasSuffix(before, "like") || strings.HasSuffix(before, "similar to") || strings.HasSuffix(before, "style of") || len(k) >= 5 || strings.Contains(k, " "):
			q.LikeArtistIDs = append(q.LikeArtistIDs, id)
			lbl = append(lbl, "Like "+idx.names[id])
		default:
			continue // a short bare word ("air", "yes") is more likely just a word
		}
		norm = norm[:pos] + " " + norm[pos+len(k)+1:]
		if len(q.ArtistIDs)+len(q.LikeArtistIDs) >= 5 {
			break
		}
	}

	has := func(words []string) bool {
		for _, w := range words {
			if strings.Contains(low, " "+w+" ") {
				return true
			}
		}
		return false
	}
	q.Rediscover = has(rediscoverWords)
	q.Favourites = !q.Rediscover && has(favouriteWords)
	q.Unplayed = has(unplayedWords)
	q.New = has(newWords)
	for _, f := range []struct {
		on   bool
		name string
	}{{q.Favourites, "Favourites"}, {q.Unplayed, "Unplayed"}, {q.New, "Recently added"}, {q.Rediscover, "Rediscover"}} {
		if f.on {
			lbl = append(lbl, f.name)
		}
	}
	if m := reAbout.FindStringSubmatch(strings.TrimSpace(low)); m != nil {
		q.Theme = clip(strings.TrimSpace(m[1]), 120)
	} else if m := reNoun.FindStringSubmatch(low); m != nil {
		q.Theme = m[1]
	}
	q.Sound = strings.TrimSpace(prompt)
	q.Title = strings.TrimSpace(prompt)
	return MusicReading{Query: q, Understood: lbl}
}
