package muse

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"marquee/internal/items"
	"marquee/internal/origins"
	"marquee/internal/soundprint"
)

// MusicLibrary is what a music prompt is matched against.
type MusicLibrary struct {
	Genres    []string         // genre tags, most used first
	Artists   map[int64]string // artist id → name
	Countries []string         // artist countries in the library (ISO codes, from artist details)
	Areas     []string         // artist areas and begin areas ("Manchester")
}

type musicAnswer struct {
	Genres          []string `json:"genres"`
	YearFrom        int      `json:"yearFrom"`
	YearTo          int      `json:"yearTo"`
	Artists         []string `json:"artists"`
	LikeArtists     []string `json:"likeArtists"`
	Favourites      bool     `json:"favourites"`
	LikeFavourites  bool     `json:"likeFavourites"`
	Unplayed        bool     `json:"unplayed"`
	New             bool     `json:"recentlyAdded"`
	Rediscover      bool     `json:"rediscover"`
	MaxTrackMinutes int      `json:"maxTrackMinutes"`
	TotalMinutes    int      `json:"totalMinutes"`
	Sound           string   `json:"sound"`
	Theme           string   `json:"theme"`
	TempoMin        int      `json:"tempoMin"`
	TempoMax        int      `json:"tempoMax"`
	Energy          string   `json:"energy"`
	Mode            string   `json:"mode"`
	Key             string   `json:"key"`
	Popularity      string   `json:"popularity"`
	Origins         []string `json:"origins"`
	ArtistType      string   `json:"artistType"`
	Gender          string   `json:"gender"`
	ActiveFrom      int      `json:"activeFrom"`
	ActiveTo        int      `json:"activeTo"`
	AlbumTypes      []string `json:"albumTypes"`
	ExcludeAlbum    []string `json:"excludeAlbumTypes"`
	Describe        string   `json:"describe"`
	Name            string   `json:"name"`
}

// releaseTypes are the album release types stored from MusicBrainz.
var releaseTypes = []string{"album", "ep", "single", "live", "compilation", "soundtrack", "remix", "demo"}

func enumOf(vals ...string) map[string]any {
	return map[string]any{"type": "string", "enum": vals}
}

var musicSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"genres":            strArray(),
		"yearFrom":          map[string]any{"type": "integer"},
		"yearTo":            map[string]any{"type": "integer"},
		"artists":           strArray(),
		"likeArtists":       strArray(),
		"favourites":        map[string]any{"type": "boolean"},
		"likeFavourites":    map[string]any{"type": "boolean"},
		"unplayed":          map[string]any{"type": "boolean"},
		"recentlyAdded":     map[string]any{"type": "boolean"},
		"rediscover":        map[string]any{"type": "boolean"},
		"maxTrackMinutes":   map[string]any{"type": "integer"},
		"totalMinutes":      map[string]any{"type": "integer"},
		"sound":             map[string]any{"type": "string"},
		"theme":             map[string]any{"type": "string"},
		"tempoMin":          map[string]any{"type": "integer"},
		"tempoMax":          map[string]any{"type": "integer"},
		"energy":            enumOf("", "low", "high"),
		"mode":              enumOf("", "major", "minor"),
		"key":               map[string]any{"type": "string"},
		"popularity":        enumOf("", "hits", "deep cuts"),
		"origins":           strArray(),
		"artistType":        enumOf("", "person", "group"),
		"gender":            enumOf("", "female", "male"),
		"activeFrom":        map[string]any{"type": "integer"},
		"activeTo":          map[string]any{"type": "integer"},
		"albumTypes":        map[string]any{"type": "array", "items": enumOf(releaseTypes...)},
		"excludeAlbumTypes": map[string]any{"type": "array", "items": enumOf(releaseTypes...)},
		"describe":          map[string]any{"type": "string"},
		"name":              map[string]any{"type": "string"},
	},
	"required": []string{"genres", "yearFrom", "yearTo", "artists", "likeArtists", "favourites", "likeFavourites", "unplayed", "recentlyAdded",
		"rediscover", "maxTrackMinutes", "totalMinutes", "sound", "theme", "tempoMin", "tempoMax", "energy", "mode", "key", "popularity",
		"origins", "artistType", "gender", "activeFrom", "activeTo", "albumTypes", "excludeAlbumTypes", "describe", "name"},
}

const musicSystem = `You turn a request for music into playlist filters for a home music library. Answer only with JSON.
Fields:
- genres: genres asked for, ONLY from the library's genre list below, spelled exactly as listed (any one may match). [] when none fits.
- yearFrom, yearTo: release year bounds, 0 when open. "80s" is 1980-1989.
- artists: artists the playlist must be BY ("songs by Queen"), else [].
- likeArtists: artists it should SOUND like ("like Radiohead", "in the style of"), else [].
- favourites: only their own favourites ("my favourites", "songs I love"). likeFavourites: music that SOUNDS like their favourites, not only the favourites themselves ("more like my favourites", "similar to songs I love"). unplayed: music they haven't heard. recentlyAdded: new additions. rediscover: old favourites they haven't played in a while.
- maxTrackMinutes: longest track allowed, 0 when not asked. totalMinutes: playlist length ("an hour of" = 60), 0 when not asked.
- sound: how the music should sound or feel, in a few words for an audio model (mood, energy, instruments, tempo, setting), e.g. "mellow acoustic guitar, rainy evening". Empty when the request is only filters.
- theme: what the LYRICS should be about when the request says so ("songs about the ocean" = "the ocean", "breakup songs" = "breakups"), else "".
- tempoMin, tempoMax: tempo bounds in BPM when asked ("fast" = 120-0, "slow" = 0-95, "around 120 bpm" = 112-128), else 0.
- energy: "high" for energetic/intense/workout music, "low" for calm/relaxing/sleepy music, else "".
- mode: "major" for happy/uplifting, "minor" for sad/melancholy/dark, else "". key: a musical key only when named ("in D minor" = "D"), else "".
- popularity: "hits" for hits/best-known/greatest songs, "deep cuts" for lesser-known/obscure tracks, else "".
- origins: where the ARTISTS are from, as countries, nationalities or cities ("Icelandic" = ["Icelandic"], "from Manchester" = ["Manchester"]), else [].
- artistType: "person" for solo artists/singer-songwriters, "group" for bands, else "". gender: "female" or "male" when the artists' gender is asked ("female singers"), else "".
- activeFrom, activeTo: years the ARTISTS were active, when asked ("bands from the 70s scene"), else 0. Use yearFrom/yearTo for when the music was released.
- albumTypes: only these release types ("live albums" = ["live"]), else []. excludeAlbumTypes: types ruled out ("no compilations" = ["compilation"]), else [].
- describe: what the ARTISTS or albums should be like, in a few words for matching artist bios and styles (scene, style, kind of artist), e.g. "Icelandic female singer-songwriter, experimental"; empty when the request says nothing about the artists.
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
	q.LikeFavourites = a.LikeFavourites || q.Favourites && likesFavourites(strings.ToLower(prompt))
	if q.LikeFavourites {
		q.Favourites = false // like them, not only them
	}
	for _, f := range []struct {
		on   bool
		name string
	}{{q.Favourites, "Favourites"}, {q.LikeFavourites, "Like your favourites"}, {q.Unplayed, "Unplayed"}, {q.New, "Recently added"}, {q.Rediscover, "Rediscover"}} {
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
	var describe []string
	lbl = append(lbl, applyMusicDetails(&q, a, lib, &describe)...)
	q.Describe = clip(strings.TrimSpace(strings.Join(append([]string{a.Describe}, describe...), " ")), 150)
	q.Sound = clip(strings.TrimSpace(strings.Join(append([]string{a.Sound}, unmatched...), " ")), 150)
	q.Theme = clip(strings.TrimSpace(a.Theme), 120)
	if q.Sound != "" {
		lbl = append(lbl, "Sounds "+q.Sound)
	}
	if len(lbl) == 0 && q.Theme == "" && q.Describe == "" {
		return MusicReading{}, false
	}
	q.Title = cleanTitle(a.Name)
	if q.Title == "" {
		q.Title = prompt
	}
	return MusicReading{Query: q, Understood: lbl, AI: true}, true
}

// applyMusicDetails validates the tempo, energy, key, popularity, origin, artist and album
// fields; origins the library doesn't know go to describe. It returns the labels.
func applyMusicDetails(q *soundprint.MusicQuery, a musicAnswer, lib MusicLibrary, describe *[]string) []string {
	var lbl []string
	lo, hi := float64(a.TempoMin), float64(a.TempoMax)
	if lo < 40 || lo > 250 {
		lo = 0
	}
	if hi < 40 || hi > 250 {
		hi = 0
	}
	if lo > 0 && hi > 0 && lo > hi {
		lo, hi = hi, lo
	}
	q.TempoMin, q.TempoMax = lo, hi
	if a.Energy == "low" || a.Energy == "high" {
		q.Energy = a.Energy
	}
	if a.Mode == "major" || a.Mode == "minor" {
		q.Mode = a.Mode
	}
	q.Key = noteName(a.Key)
	switch a.Popularity {
	case "hits":
		q.Popular = 1
	case "deep cuts":
		q.Popular = -1
	}
	for _, o := range a.Origins {
		if !addOrigin(q, lib, o) && strings.TrimSpace(o) != "" {
			*describe = append(*describe, "from "+strings.TrimSpace(o))
		}
	}
	if a.ArtistType == "person" || a.ArtistType == "group" {
		q.ArtistType = a.ArtistType
	}
	if a.Gender == "female" || a.Gender == "male" {
		q.Gender = a.Gender
	}
	maxYear := time.Now().Year() + 1
	if a.ActiveFrom >= 1900 && a.ActiveFrom <= maxYear {
		q.ActiveFrom = a.ActiveFrom
	}
	if a.ActiveTo >= 1900 && a.ActiveTo <= maxYear && (q.ActiveFrom == 0 || a.ActiveTo >= q.ActiveFrom) {
		q.ActiveTo = a.ActiveTo
	}
	valid := func(list []string) []string {
		var out []string
		for _, t := range list {
			t = strings.ToLower(strings.TrimSpace(t))
			if slices.Contains(releaseTypes, t) && !slices.Contains(out, t) {
				out = append(out, t)
			}
		}
		return out
	}
	q.AlbumTypes, q.ExcludeAlbumTypes = valid(a.AlbumTypes), valid(a.ExcludeAlbum)
	byNamedArtists(q)
	return append(lbl, DetailLabels(*q)...)
}

// byNamedArtists drops the artist traits (origin, solo or band, gender, active years) when
// the music must be by named artists: the traits can only describe those artists, so as
// filters they'd only take tracks away (the AI adds them unasked for "songs by U2 and
// Coldplay": Irish and British bands, active 1980–2020).
func byNamedArtists(q *soundprint.MusicQuery) {
	if len(q.ArtistIDs) > 0 {
		q.Countries, q.Areas, q.ArtistType, q.Gender, q.ActiveFrom, q.ActiveTo = nil, nil, "", "", 0, 0
	}
}

// Eased takes the labels of the filters a Muse playlist had to relax (ran is the query as
// it ran, Station.Query) out of understood, so the description doesn't promise live
// albums that aren't there.
func Eased(understood []string, asked, ran soundprint.MusicQuery) []string {
	still := map[string]bool{yearLabel(ran.YearFrom, ran.YearTo): true}
	for _, l := range DetailLabels(ran) {
		still[l] = true
	}
	gone := map[string]bool{}
	for _, l := range append(DetailLabels(asked), yearLabel(asked.YearFrom, asked.YearTo)) {
		if l != "" && !still[l] {
			gone[l] = true
		}
	}
	if len(gone) == 0 {
		return understood
	}
	var out []string
	for _, l := range understood {
		if !gone[l] {
			out = append(out, l)
		}
	}
	return out
}

// addOrigin maps a country, nationality or place onto the library's artist countries and
// areas; false when the library has no artists from there.
func addOrigin(q *soundprint.MusicQuery, lib MusicLibrary, o string) bool {
	o = strings.TrimSpace(strings.TrimPrefix(strings.ToLower(strings.TrimSpace(o)), "from "))
	if o == "" {
		return false
	}
	if org, ok := origins.Lookup(o); ok && slices.Contains(lib.Countries, org.Country) {
		if !slices.Contains(q.Countries, org.Country) {
			q.Countries = append(q.Countries, org.Country)
		}
		return true
	}
	for _, a := range lib.Areas {
		if strings.EqualFold(a, o) {
			if !slices.Contains(q.Areas, a) {
				q.Areas = append(q.Areas, a)
			}
			return true
		}
	}
	return false
}

var notes = []string{"C", "C#", "D", "D#", "E", "F", "F#", "G", "G#", "A", "A#", "B"}

// noteName is a key as the analysis names it ("Db" → "C#"; "" when not a key).
func noteName(k string) string {
	k = strings.TrimSpace(k)
	if k == "" || len(k) > 3 {
		return ""
	}
	k = strings.ToUpper(k[:1]) + strings.ToLower(k[1:])
	k = strings.NewReplacer("♯", "#", "♭", "b", "sharp", "#").Replace(k)
	flats := map[string]string{"Db": "C#", "Eb": "D#", "Gb": "F#", "Ab": "G#", "Bb": "A#", "Cb": "B", "Fb": "E"}
	if f, ok := flats[k]; ok {
		return f
	}
	if slices.Contains(notes, k) {
		return k
	}
	return ""
}

// DetailLabels describe the tempo, energy, key, popularity, origin, artist and album fields.
func DetailLabels(q soundprint.MusicQuery) []string {
	var lbl []string
	switch {
	case q.TempoMin > 0 && q.TempoMax > 0:
		lbl = append(lbl, fmt.Sprintf("%.0f–%.0f BPM", q.TempoMin, q.TempoMax))
	case q.TempoMin > 0:
		lbl = append(lbl, fmt.Sprintf("Over %.0f BPM", q.TempoMin))
	case q.TempoMax > 0:
		lbl = append(lbl, fmt.Sprintf("Under %.0f BPM", q.TempoMax))
	}
	if q.Energy != "" {
		lbl = append(lbl, strings.ToUpper(q.Energy[:1])+q.Energy[1:]+" energy")
	}
	switch {
	case q.Key != "" && q.Mode != "":
		lbl = append(lbl, q.Key+" "+q.Mode)
	case q.Key != "":
		lbl = append(lbl, "Key of "+q.Key)
	case q.Mode != "":
		lbl = append(lbl, "Mostly "+q.Mode+" keys")
	}
	switch q.Popular {
	case 1:
		lbl = append(lbl, "Hits")
	case -1:
		lbl = append(lbl, "Deep cuts")
	}
	for _, c := range q.Countries {
		lbl = append(lbl, "From "+strings.TrimPrefix(origins.CountryName(c), "the "))
	}
	for _, a := range q.Areas {
		lbl = append(lbl, "From "+a)
	}
	switch {
	case q.ArtistType == "person" && q.Gender != "":
		lbl = append(lbl, strings.ToUpper(q.Gender[:1])+q.Gender[1:]+" solo artists")
	case q.ArtistType == "person":
		lbl = append(lbl, "Solo artists")
	case q.ArtistType == "group":
		lbl = append(lbl, "Bands")
	case q.Gender != "":
		lbl = append(lbl, strings.ToUpper(q.Gender[:1])+q.Gender[1:]+" artists")
	}
	if l := yearLabel(q.ActiveFrom, q.ActiveTo); l != "" {
		lbl = append(lbl, "Active "+l)
	}
	for _, t := range q.AlbumTypes {
		lbl = append(lbl, albumTypeLabel(t))
	}
	for _, t := range q.ExcludeAlbumTypes {
		lbl = append(lbl, "No "+strings.ToLower(albumTypeLabel(t)))
	}
	return lbl
}

func albumTypeLabel(t string) string {
	return map[string]string{"album": "Studio albums", "ep": "EPs", "single": "Singles", "live": "Live albums", "compilation": "Compilations",
		"soundtrack": "Soundtracks", "remix": "Remixes", "demo": "Demos"}[t]
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

// likesFavourites reports a prompt (lower case) asking for music like the person's
// favourites ("more like my favourites", "similar to songs i love").
func likesFavourites(low string) bool {
	low = " " + strings.Join(strings.Fields(low), " ") + " "
	for _, l := range likeWords {
		for _, f := range favouriteWords {
			for _, mid := range []string{" ", " the ", " what ", " all "} {
				if strings.Contains(low, " "+l+mid+f+" ") {
					return true
				}
			}
		}
	}
	return false
}

var (
	reAbout = regexp.MustCompile(`\b(?:songs?|tracks?|music|lyrics)\s+about\s+(.+)$`)
	reNoun  = regexp.MustCompile(`\b(breakup|heartbreak|love|party|summer|christmas|road trip|drinking)\s+songs\b`)
)

var (
	favouriteWords = []string{"my favourites", "my favorites", "my favourite", "my favorite", "favourites", "favorites", "songs i love", "loved tracks"}
	// likeWords before a favourites phrase ask for music like the favourites, not only them.
	likeWords       = []string{"like", "similar to", "sounds like", "sound like", "in the style of", "inspired by", "reminds me of"}
	unplayedWords   = []string{"haven't heard", "have not heard", "never heard", "never played", "not played", "unplayed", "unheard", "new to me"}
	newWords        = []string{"recently added", "new additions", "newly added", "just added", "new music", "new arrivals"}
	rediscoverWords = []string{"rediscover", "forgotten favourites", "forgotten favorites", "haven't played in a while", "haven't listened to in a while", "old favourites", "old favorites"}
)

var (
	reBPM        = regexp.MustCompile(`\b(?:around|about|roughly|~)?\s*(\d{2,3})\s*bpm\b`)
	reBPMBetween = regexp.MustCompile(`\b(?:between\s+)?(\d{2,3})\s*(?:-|–|to|and)\s*(\d{2,3})\s*bpm\b`)
	reKey        = regexp.MustCompile(`\bin\s+([a-g])(#|♯|b|♭| sharp| flat)?\s+(major|minor)\b`)
	reFromPlace  = regexp.MustCompile(`\bfrom\s+([a-z][a-z.' -]{2,40}?)(?:\s*(?:,|$| and | or | in | with | that | who ))`)
)

var (
	fastWords       = []string{"fast", "uptempo", "up-tempo", "high tempo", "quick tempo"}
	slowWords       = []string{"slow", "downtempo", "down-tempo", "slow tempo"}
	highEnergyWords = []string{"high energy", "high-energy", "energetic", "intense", "pumped up", "workout", "work out", "gym"}
	lowEnergyWords  = []string{"low energy", "low-energy", "calm", "chill", "chilled", "relaxing", "relaxed", "sleepy", "for sleep", "soothing"}
	majorWords      = []string{"happy", "uplifting", "cheerful", "joyful", "feel good", "feel-good", "major key"}
	minorWords      = []string{"sad", "melancholy", "melancholic", "gloomy", "minor key", "heartbroken"}
	hitsWords       = []string{"greatest hits", "biggest hits", "best known", "best-known", "most popular", "hits", "popular songs"}
	deepCutWords    = []string{"deep cuts", "deep cut", "b-sides", "b sides", "lesser known", "lesser-known", "obscure", "hidden gems", "rarities"}
	femaleWords     = []string{"female singers", "female singer", "female vocalists", "female vocals", "female artists", "women singers", "women artists", "female-fronted", "female fronted", "female"}
	maleWords       = []string{"male singers", "male singer", "male vocalists", "male artists", "male"}
	soloWords       = []string{"singer-songwriters", "singer-songwriter", "singer songwriters", "solo artists", "solo artist", "solo singers"}
	bandWords       = []string{"bands", "groups"}
	liveWords       = []string{"live albums", "live recordings", "live versions", "live tracks", "live performances", "concert recordings"}
	noCompWords     = []string{"no compilations", "without compilations", "not compilations", "no greatest hits albums", "no best-ofs"}
)

// keywordDetails reads tempo, energy, mode and key, popularity, origin, artist and album
// words without the AI.
func keywordDetails(q *soundprint.MusicQuery, lib MusicLibrary, low string) []string {
	has := func(words []string) bool {
		for _, w := range words {
			if strings.Contains(low, " "+w+" ") {
				return true
			}
		}
		return false
	}
	if m := reBPMBetween.FindStringSubmatch(low); m != nil {
		a, _ := strconv.Atoi(m[1])
		b, _ := strconv.Atoi(m[2])
		q.TempoMin, q.TempoMax = float64(min(a, b)), float64(max(a, b))
	} else if m := reBPM.FindStringSubmatch(low); m != nil {
		if n, _ := strconv.Atoi(m[1]); n >= 40 && n <= 250 {
			q.TempoMin, q.TempoMax = float64(n-8), float64(n+8)
		}
	} else if has(fastWords) {
		q.TempoMin = 120
	} else if has(slowWords) {
		q.TempoMax = 95
	}
	if has(highEnergyWords) {
		q.Energy = "high"
	} else if has(lowEnergyWords) {
		q.Energy = "low"
	}
	if m := reKey.FindStringSubmatch(low); m != nil {
		acc := map[string]string{"#": "#", "♯": "#", " sharp": "#", "b": "b", "♭": "b", " flat": "b"}[m[2]]
		q.Key, q.Mode = noteName(strings.ToUpper(m[1])+acc), m[3]
	} else if has(majorWords) {
		q.Mode = "major"
	} else if has(minorWords) {
		q.Mode = "minor"
	}
	if has(deepCutWords) {
		q.Popular = -1
	} else if has(hitsWords) {
		q.Popular = 1
	}
	// Countries the library's artists come from ("icelandic", "from sweden"), and places ("from manchester").
	found, _ := origins.Find(strings.Join(strings.Fields(low), " "))
	for _, o := range found {
		if slices.Contains(lib.Countries, o.Country) && !slices.Contains(q.Countries, o.Country) {
			q.Countries = append(q.Countries, o.Country)
		}
	}
	for _, m := range reFromPlace.FindAllStringSubmatch(low+" ", -1) {
		addOrigin(q, lib, strings.TrimSpace(m[1]))
	}
	switch {
	case has(femaleWords):
		q.Gender = "female"
	case has(maleWords):
		q.Gender = "male"
	}
	switch {
	case has(soloWords):
		q.ArtistType = "person"
	case has(bandWords):
		q.ArtistType = "group"
	}
	if has(liveWords) {
		q.AlbumTypes = []string{"live"}
	}
	if has(noCompWords) {
		q.ExcludeAlbumTypes = []string{"compilation"}
	}
	byNamedArtists(q)
	return DetailLabels(*q)
}

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
	if q.Favourites && likesFavourites(low) {
		q.Favourites, q.LikeFavourites = false, true
	}
	q.Unplayed = has(unplayedWords)
	q.New = has(newWords)
	for _, f := range []struct {
		on   bool
		name string
	}{{q.Favourites, "Favourites"}, {q.LikeFavourites, "Like your favourites"}, {q.Unplayed, "Unplayed"}, {q.New, "Recently added"}, {q.Rediscover, "Rediscover"}} {
		if f.on {
			lbl = append(lbl, f.name)
		}
	}
	lbl = append(lbl, keywordDetails(&q, lib, low)...)
	if m := reAbout.FindStringSubmatch(strings.TrimSpace(low)); m != nil {
		q.Theme = clip(strings.TrimSpace(m[1]), 120)
	} else if m := reNoun.FindStringSubmatch(low); m != nil {
		q.Theme = m[1]
	}
	q.Sound = strings.TrimSpace(prompt)
	if q.LikeFavourites {
		// Their favourites give the sound; only what else the prompt says goes to the audio model.
		q.Sound = withoutFavouriteWords(low)
	}
	q.Title = strings.TrimSpace(prompt)
	return MusicReading{Query: q, Understood: lbl}
}

// favouriteFiller are words that, around a favourites phrase, say nothing about the sound.
var favouriteFiller = map[string]bool{"more": true, "music": true, "songs": true, "song": true, "tracks": true, "play": true,
	"me": true, "some": true, "the": true, "what": true, "all": true, "stuff": true, "give": true, "a": true, "mix": true, "of": true, "playlist": true}

// withoutFavouriteWords is a prompt (lower case) without its favourites and "like" phrases
// and filler: "" for "more like my favourites", "mellow" for "mellow songs like my favourites".
func withoutFavouriteWords(low string) string {
	s := " " + strings.Join(strings.Fields(low), " ") + " "
	for _, w := range append(append([]string{}, favouriteWords...), likeWords...) {
		s = strings.ReplaceAll(s, " "+w+" ", " ")
	}
	var rest []string
	for _, w := range strings.Fields(s) {
		if w != "," && !favouriteFiller[w] {
			rest = append(rest, w)
		}
	}
	return strings.Join(rest, " ")
}
