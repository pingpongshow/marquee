package soundprint

import (
	"context"
	"errors"
	"sort"
	"strings"
)

// MusicQuery is a Muse prompt read into the library's own terms (by the AI or by keyword
// matching): hard filters first, then what the music should sound like.
type MusicQuery struct {
	Genres           []string // library genre tags (album, artist or track); any may match
	YearFrom, YearTo int      // 0 = open
	ArtistIDs        []int64  // only these artists
	LikeArtistIDs    []int64  // sound like these artists
	Favourites       bool     // tracks they love (favourite songs, played often or rated 4+ stars)
	LikeFavourites   bool     // sound like their favourite songs ("more like my favourites")
	Unplayed         bool     // never played
	New              bool     // added in the last 60 days
	Rediscover       bool     // favourites not played for six months
	MaxTrackMinutes  int      // 0 = any
	TotalMinutes     int      // playlist length; 0 = by count
	Sound            string   // mood and sound words, for CLAP
	Theme            string   // what the lyrics are about (scored by the caller as Boost)
	Title            string   // a short playlist name ("" = the prompt)

	// From the analysis (BPM, energy, key) and artist and album details (MusicBrainz):
	TempoMin, TempoMax float64  // BPM bounds, 0 = open; a hard filter with 5% tolerance
	Energy             string   // "low" or "high", relative to the library; a hard filter
	Mode               string   // "major" or "minor": a soft boost
	Key                string   // "C"…"B" (sharps as "C#"): a soft boost, with Mode when given
	Popular            int      // 1 = hits (an artist's top tracks), -1 = deep cuts (not among them)
	Countries          []string // artist countries (ISO 3166-1); with Areas, any may match
	Areas              []string // artist areas or begin areas ("Manchester")
	ArtistType         string   // "person" (solo artists) or "group" (bands)
	Gender             string   // "female" or "male"
	ActiveFrom         int      // artists active at some point in these years (0 = open)
	ActiveTo           int
	AlbumTypes         []string // only these release types (album, ep, single, live, compilation, soundtrack…)
	ExcludeAlbumTypes  []string // never these
	// Describe is what the artists or albums should be like ("Icelandic female
	// singer-songwriters"); the caller scores artists and albums by it into ArtistScores and
	// AlbumScores (cosine similarity of their descriptions).
	Describe                  string
	ArtistScores, AlbumScores map[int64]float64
}

// Blend weights for the ranking vector: the described sound counts most, then the "like"
// artists, then the genres' own average sound.
const (
	blendSound  = 0.5
	blendSeeds  = 0.3
	blendGenres = 0.2
)

// soundPhrasings are the ways a sound description is put to CLAP; their mean is steadier
// than any single phrasing.
func soundPhrasings(sound string) []string {
	return []string{"music that sounds " + sound, sound + " music", "a song that is " + sound}
}

// TextVectorMulti embeds several phrasings and averages them.
func (s *Service) TextVectorMulti(ctx context.Context, texts []string) ([]float32, error) {
	vs, err := s.Client.EmbedText(ctx, texts)
	if err != nil {
		return nil, errors.Join(ErrUnavailable, err)
	}
	if len(vs) == 0 {
		return nil, ErrUnavailable
	}
	ts := make([]*Track, len(vs))
	for i, v := range vs {
		ts[i] = &Track{Vec: normalize(v)}
	}
	return Mean(ts), nil
}

// MuseFilters is the history and catalogue data a query's filters need, looked up once.
type MuseFilters struct {
	GenreTracks map[int64]bool  // tracks tagged with any of the genres (nil = no genre filter)
	Liked       map[int64]bool  // favourites
	Loved       []float32       // the sound of their favourites (for LikeFavourites)
	Played      map[int64]bool  // ever played
	Fresh       map[int64]bool  // added in the last 60 days
	Old         map[int64]bool  // favourites not played for six months
	DurationMS  map[int64]int64 // track lengths (for length limits)
	Artists     map[int64]bool  // artists passing the origin, trait and era filters (nil = no filter)
	AlbumsIn    map[int64]bool  // albums of the wanted release types (nil = no filter)
	AlbumsOut   map[int64]bool  // albums of excluded release types
	PopularRank map[int64]int   // track → rank among its artist's popular tracks (for hits and deep cuts)
	EnergyLow   float64         // energy at or below this is "low" (from the library's spread)
	EnergyHigh  float64         // at or above this is "high"
	Described   map[int64]bool  // artists and albums matching Describe (nil = no filter)
}

// hitsRank is how far down an artist's popular tracks a "hit" goes.
const hitsRank = 5

// tempoOK accepts bpm within the bounds with 5% tolerance. For a narrow range ("around 120
// bpm") half and double time also count, since beat tracking often lands an octave off.
func (q MusicQuery) tempoOK(bpm float64) bool {
	if q.TempoMin <= 0 && q.TempoMax <= 0 {
		return true
	}
	if bpm <= 0 {
		return false
	}
	in := func(b float64) bool {
		return (q.TempoMin <= 0 || b >= q.TempoMin*0.95) && (q.TempoMax <= 0 || b <= q.TempoMax*1.05)
	}
	if in(bpm) {
		return true
	}
	narrow := q.TempoMin > 0 && q.TempoMax > 0 && q.TempoMax-q.TempoMin <= 30
	return narrow && (in(bpm*2) || in(bpm/2))
}

// Keep is the hard filter of q on top of base.
func (q MusicQuery) Keep(f MuseFilters, base Filter, withGenres bool) Filter {
	artists := map[int64]bool{}
	for _, id := range q.ArtistIDs {
		artists[id] = true
	}
	return func(t *Track) bool {
		if base != nil && !base(t) {
			return false
		}
		if q.YearFrom > 0 && t.Year < q.YearFrom || q.YearTo > 0 && (t.Year == 0 || t.Year > q.YearTo) {
			return false
		}
		if len(artists) > 0 && !artists[t.ArtistID] {
			return false
		}
		if withGenres && f.GenreTracks != nil && !f.GenreTracks[t.ItemID] {
			return false
		}
		if q.Favourites && !f.Liked[t.ItemID] || q.Unplayed && f.Played[t.ItemID] || q.New && !f.Fresh[t.ItemID] || q.Rediscover && !f.Old[t.ItemID] {
			return false
		}
		if q.MaxTrackMinutes > 0 {
			if d := f.DurationMS[t.ItemID]; d <= 0 || d > int64(q.MaxTrackMinutes)*60000 {
				return false
			}
		}
		if !q.tempoOK(t.BPM) {
			return false
		}
		switch q.Energy {
		case "high":
			if t.Energy <= 0 || t.Energy < f.EnergyHigh {
				return false
			}
		case "low":
			if t.Energy <= 0 || t.Energy > f.EnergyLow {
				return false
			}
		}
		if f.Artists != nil && !f.Artists[t.ArtistID] {
			return false
		}
		if f.AlbumsIn != nil && !f.AlbumsIn[t.AlbumID] || f.AlbumsOut[t.AlbumID] {
			return false
		}
		if r := f.PopularRank[t.ItemID]; q.Popular > 0 && (r == 0 || r > hitsRank) || q.Popular < 0 && r > 0 {
			return false
		}
		if f.Described != nil && !f.Described[t.ArtistID] && !f.Described[t.AlbumID] {
			return false
		}
		return true
	}
}

// Artist and album descriptions: the best-matching ones (within describeSpread of the best,
// at most describeArtists artists and describeAlbums albums) filter the tracks; when that
// leaves too few, they become a boost of up to describeBoost instead.
const (
	describeSpread  = 0.08
	describeArtists = 30
	describeAlbums  = 40
	describeBoost   = 0.3
)

// topDescribed picks the best-scoring ids.
func topDescribed(scores map[int64]float64, n int, into map[int64]bool) {
	type sc struct {
		id int64
		s  float64
	}
	list := make([]sc, 0, len(scores))
	for id, s := range scores {
		list = append(list, sc{id, s})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].s != list[j].s {
			return list[i].s > list[j].s
		}
		return list[i].id < list[j].id
	})
	for i, x := range list {
		if i == n || x.s < list[0].s-describeSpread {
			break
		}
		into[x.id] = true
	}
}

// DescribedSet is the artists and albums whose descriptions match best (nil when there are no scores).
func (q MusicQuery) DescribedSet() map[int64]bool {
	if len(q.ArtistScores) == 0 && len(q.AlbumScores) == 0 {
		return nil
	}
	out := map[int64]bool{}
	topDescribed(q.ArtistScores, describeArtists, out)
	topDescribed(q.AlbumScores, describeAlbums, out)
	return out
}

// describedBoost scores tracks by their artist's or album's description match, from 0 at
// the median score to describeBoost for the best.
func (q MusicQuery) describedBoost(tracks []*Track) map[int64]float64 {
	norm := func(scores map[int64]float64) func(int64) float64 {
		if len(scores) == 0 {
			return func(int64) float64 { return 0 }
		}
		all := make([]float64, 0, len(scores))
		for _, s := range scores {
			all = append(all, s)
		}
		sort.Float64s(all)
		top, mid := all[len(all)-1], all[(len(all)-1)/2]
		return func(id int64) float64 {
			s, ok := scores[id]
			if !ok || top <= mid || s <= mid {
				return 0
			}
			return (s - mid) / (top - mid)
		}
	}
	artist, album := norm(q.ArtistScores), norm(q.AlbumScores)
	out := map[int64]float64{}
	for _, t := range tracks {
		if b := describeBoost * max(artist(t.ArtistID), album(t.AlbumID)); b > 0 {
			out[t.ItemID] = b
		}
	}
	return out
}

// Mode and key boosts.
const (
	modeBoost = 0.05
	keyBoost  = 0.05
)

// keyBoosts favours tracks in the asked mode (and key).
func (q MusicQuery) keyBoosts(tracks []*Track) map[int64]float64 {
	if q.Mode == "" && q.Key == "" {
		return nil
	}
	out := map[int64]float64{}
	for _, t := range tracks {
		b := 0.0
		if q.Mode != "" && strings.EqualFold(t.Mode, q.Mode) {
			b += modeBoost
		}
		if q.Key != "" && strings.EqualFold(t.Key, q.Key) && (q.Mode == "" || strings.EqualFold(t.Mode, q.Mode)) {
			b += keyBoost
		}
		if b > 0 {
			out[t.ItemID] = b
		}
	}
	return out
}

func addBoosts(dst map[int64]float64, src map[int64]float64) map[int64]float64 {
	if len(src) == 0 {
		return dst
	}
	if dst == nil {
		dst = map[int64]float64{}
	}
	for id, b := range src {
		dst[id] += b
	}
	return dst
}

// BlendVectors is the normalized weighted sum of the vectors that aren't nil (nil when all are).
func BlendVectors(vs [][]float32, ws []float64) []float32 {
	var sum []float64
	for i, v := range vs {
		if v == nil {
			continue
		}
		if sum == nil {
			sum = make([]float64, len(v))
		}
		for j := 0; j < len(sum) && j < len(v); j++ {
			sum[j] += ws[i] * float64(v[j])
		}
	}
	if sum == nil {
		return nil
	}
	out := make([]float32, len(sum))
	for i, x := range sum {
		out[i] = float32(x)
	}
	return normalize(out)
}

// MuseQuery builds a Muse playlist from a parsed prompt: the hard filters choose which
// tracks may play, then the tracks closest to the blend of the described sound, the "like"
// artists and the genres' sound are picked (keeping artists varied). When filters leave
// too few tracks, the genre filter is relaxed into a ranking hint.
func (s *Service) MuseQuery(ctx context.Context, userID int64, q MusicQuery, n int, o Options) (Station, error) {
	f := s.museFilters(ctx, userID, q)
	return s.museQuery(ctx, q, f, n, o)
}

func (s *Service) museQuery(ctx context.Context, q MusicQuery, f MuseFilters, n int, o Options) (Station, error) {
	if q.TotalMinutes > 0 {
		n = max(n, q.TotalMinutes/3+2)
	}
	f.Described = q.DescribedSet()
	keep := q.Keep(f, o.Keep, true)
	pool := s.Index.Where(keep)
	enough := min(n, 10)
	if len(pool) < enough && f.Described != nil {
		// Few tracks by the best-matching artists and albums: prefer them instead.
		f.Described = nil
		keep = q.Keep(f, o.Keep, true)
		pool = s.Index.Where(keep)
		o.Boost = addBoosts(cloneBoost(o.Boost), q.describedBoost(pool))
	}
	if len(pool) < enough && f.GenreTracks != nil {
		keep = q.Keep(f, o.Keep, false)
		pool = s.Index.Where(keep)
	}
	if len(pool) == 0 {
		return Station{Title: q.Title}, nil
	}
	o.Boost = addBoosts(cloneBoost(o.Boost), q.keyBoosts(pool))
	var sound, seeds, genres []float32
	var soundErr error
	if strings.TrimSpace(q.Sound) != "" {
		sound, soundErr = s.TextVectorMulti(ctx, soundPhrasings(strings.TrimSpace(q.Sound)))
	}
	if len(q.LikeArtistIDs) > 0 {
		like := map[int64]bool{}
		for _, id := range q.LikeArtistIDs {
			like[id] = true
		}
		seeds = Mean(s.Index.Where(func(t *Track) bool { return like[t.ArtistID] }))
	}
	if f.Loved != nil {
		seeds = BlendVectors([][]float32{seeds, f.Loved}, []float64{0.5, 0.5})
	}
	if f.GenreTracks != nil {
		genres = Mean(s.Index.Where(func(t *Track) bool { return f.GenreTracks[t.ItemID] && (o.Keep == nil || o.Keep(t)) }))
	}
	v := BlendVectors([][]float32{sound, seeds, genres}, []float64{blendSound, blendSeeds, blendGenres})
	if v == nil {
		if soundErr != nil && !q.filtered() {
			return Station{}, soundErr // nothing to go on without the sound model
		}
		v = Mean(pool) // filters only ("unplayed 90s"): the middle of what's left
	}
	o.Keep = keep
	ts := s.Index.Pick(v, n, o)
	if q.TotalMinutes > 0 && f.DurationMS != nil {
		var total int64
		limit := int64(q.TotalMinutes) * 60000
		for i, t := range ts {
			total += f.DurationMS[t.ItemID]
			if total > limit && i > 0 {
				ts = ts[:i]
				break
			}
		}
	}
	return Station{Title: q.Title, IDs: TrackIDs(ts)}, nil
}

func cloneBoost(b map[int64]float64) map[int64]float64 {
	if b == nil {
		return nil
	}
	out := make(map[int64]float64, len(b))
	for k, v := range b {
		out[k] = v
	}
	return out
}

func (q MusicQuery) filtered() bool {
	return len(q.Genres) > 0 || q.YearFrom > 0 || q.YearTo > 0 || len(q.ArtistIDs) > 0 || len(q.LikeArtistIDs) > 0 ||
		q.Favourites || q.LikeFavourites || q.Unplayed || q.New || q.Rediscover || q.MaxTrackMinutes > 0 ||
		q.TempoMin > 0 || q.TempoMax > 0 || q.Energy != "" || q.Popular != 0 || q.artistFiltered() ||
		len(q.AlbumTypes) > 0 || len(q.ExcludeAlbumTypes) > 0 || len(q.ArtistScores) > 0 || len(q.AlbumScores) > 0
}

func (q MusicQuery) artistFiltered() bool {
	return len(q.Countries) > 0 || len(q.Areas) > 0 || q.ArtistType != "" || q.Gender != "" || q.ActiveFrom > 0 || q.ActiveTo > 0
}

// museFilters looks up what q's filters need.
func (s *Service) museFilters(ctx context.Context, userID int64, q MusicQuery) MuseFilters {
	var f MuseFilters
	ids := func(query string, args ...any) map[int64]bool {
		out := map[int64]bool{}
		rows, err := s.DB.QueryContext(ctx, query, args...)
		if err != nil {
			return out
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			if rows.Scan(&id) == nil {
				out[id] = true
			}
		}
		return out
	}
	if len(q.Genres) > 0 {
		args := make([]any, len(q.Genres))
		for i, g := range q.Genres {
			args[i] = g
		}
		f.GenreTracks = ids(`SELECT DISTINCT i.id FROM items i JOIN item_tags it ON it.item_id IN (i.id, i.parent_id, i.grandparent_id)
			JOIN tags t ON t.id = it.tag_id AND t.kind = 'genre' WHERE i.type = 'track' AND t.name COLLATE NOCASE IN (?`+strings.Repeat(",?", len(args)-1)+`)`, args...)
	}
	if q.Favourites || q.Rediscover {
		f.Liked = map[int64]bool{}
		for _, t := range s.Liked(ctx, userID) {
			f.Liked[t.ItemID] = true
		}
		f.Old = ids(`SELECT item_id FROM user_item_state WHERE user_id = ? AND (favorite_at IS NOT NULL OR rating >= 8 OR play_count >= 3)
			AND (last_viewed_at IS NULL OR last_viewed_at < strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '-180 days'))`, userID)
	}
	if q.LikeFavourites {
		f.Loved = s.Taste(ctx, userID, s.Liked(ctx, userID))
	}
	if q.Unplayed {
		f.Played = ids(`SELECT item_id FROM user_item_state WHERE user_id = ? AND play_count > 0`, userID)
	}
	if q.New {
		f.Fresh = ids(`SELECT id FROM items WHERE type = 'track' AND added_at > strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '-60 days')`)
	}
	if q.artistFiltered() {
		f.Artists = s.artistsMatching(ctx, q)
	}
	if len(q.AlbumTypes) > 0 {
		f.AlbumsIn = ids(`SELECT id FROM items WHERE type = 'album' AND release_type IN (?`+strings.Repeat(",?", len(q.AlbumTypes)-1)+`)`, anys(q.AlbumTypes)...)
	}
	if len(q.ExcludeAlbumTypes) > 0 {
		f.AlbumsOut = ids(`SELECT id FROM items WHERE type = 'album' AND release_type IN (?`+strings.Repeat(",?", len(q.ExcludeAlbumTypes)-1)+`)`, anys(q.ExcludeAlbumTypes)...)
	}
	if q.Popular != 0 {
		f.PopularRank = map[int64]int{}
		rows, err := s.DB.QueryContext(ctx, `SELECT track_id, MIN(rank) FROM music_popular GROUP BY track_id`)
		if err == nil {
			for rows.Next() {
				var id int64
				var r int
				if rows.Scan(&id, &r) == nil {
					f.PopularRank[id] = r
				}
			}
			rows.Close()
		}
	}
	if q.Energy != "" && s.Index != nil {
		f.EnergyLow, f.EnergyHigh = EnergySpread(s.Index.Where(nil))
	}
	if q.MaxTrackMinutes > 0 || q.TotalMinutes > 0 {
		f.DurationMS = map[int64]int64{}
		rows, err := s.DB.QueryContext(ctx, `SELECT id, COALESCE(duration_ms, 0) FROM items WHERE type = 'track'`)
		if err == nil {
			for rows.Next() {
				var id, d int64
				if rows.Scan(&id, &d) == nil {
					f.DurationMS[id] = d
				}
			}
			rows.Close()
		}
	}
	return f
}

func anys(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

// Energy filters are relative to the library: "high" is the top 45% of analysed tracks,
// "low" the bottom 45% (a little overlap in the middle is the tolerance).
const (
	energyLowShare  = 0.45
	energyHighShare = 0.45
)

// EnergySpread returns the energies at or below which a track counts as low, and at or above
// which it counts as high.
func EnergySpread(tracks []*Track) (low, high float64) {
	var es []float64
	for _, t := range tracks {
		if t.Energy > 0 {
			es = append(es, t.Energy)
		}
	}
	if len(es) == 0 {
		return 0, 2
	}
	sort.Float64s(es)
	low = es[int(float64(len(es)-1)*energyLowShare)]
	high = es[int(float64(len(es)-1)*(1-energyHighShare))]
	return low, high
}

// artistsMatching lists the artists whose details pass q's origin, trait and era filters.
func (s *Service) artistsMatching(ctx context.Context, q MusicQuery) map[int64]bool {
	out := map[int64]bool{}
	where := []string{"kind = 'artist'", "found = 1"}
	var args []any
	var origin []string
	if len(q.Countries) > 0 {
		origin = append(origin, `country IN (?`+strings.Repeat(",?", len(q.Countries)-1)+`)`)
		args = append(args, anys(q.Countries)...)
	}
	if len(q.Areas) > 0 {
		ph := `(?` + strings.Repeat(",?", len(q.Areas)-1) + `)`
		origin = append(origin, `area COLLATE NOCASE IN `+ph+` OR begin_area COLLATE NOCASE IN `+ph)
		args = append(append(args, anys(q.Areas)...), anys(q.Areas)...)
	}
	if len(origin) > 0 {
		where = append(where, "("+strings.Join(origin, " OR ")+")")
	}
	switch q.ArtistType {
	case "person":
		where = append(where, "artist_type = 'person'")
	case "group":
		where = append(where, "artist_type IN ('group', 'orchestra', 'choir')")
	}
	if q.Gender != "" {
		where = append(where, "gender = ?")
		args = append(args, q.Gender)
	}
	if q.ActiveTo > 0 {
		where = append(where, "begin_year IS NOT NULL AND begin_year <= ?")
		args = append(args, q.ActiveTo)
	}
	if q.ActiveFrom > 0 {
		where = append(where, "begin_year IS NOT NULL AND (end_year IS NULL OR end_year >= ?)")
		args = append(args, q.ActiveFrom)
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT item_id FROM music_info WHERE `+strings.Join(where, " AND "), args...)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			out[id] = true
		}
	}
	return out
}

// MusicOrigins lists the artist countries (ISO codes) and areas in the library, for matching
// prompts without the AI.
func (s *Service) MusicOrigins(ctx context.Context) (countries, areas []string) {
	rows, err := s.DB.QueryContext(ctx, `SELECT DISTINCT country FROM music_info WHERE kind = 'artist' AND country IS NOT NULL`)
	if err == nil {
		for rows.Next() {
			var c string
			if rows.Scan(&c) == nil {
				countries = append(countries, c)
			}
		}
		rows.Close()
	}
	rows, err = s.DB.QueryContext(ctx, `SELECT DISTINCT a FROM (SELECT area AS a FROM music_info WHERE kind = 'artist' UNION SELECT begin_area FROM music_info WHERE kind = 'artist') WHERE a IS NOT NULL`)
	if err == nil {
		for rows.Next() {
			var a string
			if rows.Scan(&a) == nil {
				areas = append(areas, a)
			}
		}
		rows.Close()
	}
	return countries, areas
}

// ArtistNames lists the music artists (id → name), for matching prompts.
func (s *Service) ArtistNames(ctx context.Context) map[int64]string {
	out := map[int64]string{}
	rows, err := s.DB.QueryContext(ctx, `SELECT id, title FROM items WHERE type = 'artist'`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var name string
		if rows.Scan(&id, &name) == nil {
			out[id] = name
		}
	}
	return out
}

// MusicGenres lists the genre tags on tracks, albums and artists, most used first.
func (s *Service) MusicGenres(ctx context.Context) []string {
	var out []string
	rows, err := s.DB.QueryContext(ctx, `SELECT t.name FROM tags t JOIN item_tags it ON it.tag_id = t.id
		JOIN items i ON i.id = it.item_id AND i.type IN ('track', 'album', 'artist') WHERE t.kind = 'genre'
		GROUP BY t.name COLLATE NOCASE ORDER BY COUNT(*) DESC`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	for rows.Next() {
		var n string
		if rows.Scan(&n) == nil {
			out = append(out, n)
		}
	}
	return out
}
