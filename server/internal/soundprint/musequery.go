package soundprint

import (
	"context"
	"errors"
	"strings"
)

// MusicQuery is a Muse prompt read into the library's own terms (by the AI or by keyword
// matching): hard filters first, then what the music should sound like.
type MusicQuery struct {
	Genres           []string // library genre tags (album, artist or track); any may match
	YearFrom, YearTo int      // 0 = open
	ArtistIDs        []int64  // only these artists
	LikeArtistIDs    []int64  // sound like these artists
	Favourites       bool     // tracks they love (played often or rated 4+ stars)
	Unplayed         bool     // never played
	New              bool     // added in the last 60 days
	Rediscover       bool     // favourites not played for six months
	MaxTrackMinutes  int      // 0 = any
	TotalMinutes     int      // playlist length; 0 = by count
	Sound            string   // mood and sound words, for CLAP
	Theme            string   // what the lyrics are about (scored by the caller as Boost)
	Title            string   // a short playlist name ("" = the prompt)
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
	Played      map[int64]bool  // ever played
	Fresh       map[int64]bool  // added in the last 60 days
	Old         map[int64]bool  // favourites not played for six months
	DurationMS  map[int64]int64 // track lengths (for length limits)
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
		return true
	}
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
	keep := q.Keep(f, o.Keep, true)
	pool := s.Index.Where(keep)
	if len(pool) < min(n, 10) && f.GenreTracks != nil {
		keep = q.Keep(f, o.Keep, false)
		pool = s.Index.Where(keep)
	}
	if len(pool) == 0 {
		return Station{Title: q.Title}, nil
	}
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

func (q MusicQuery) filtered() bool {
	return len(q.Genres) > 0 || q.YearFrom > 0 || q.YearTo > 0 || len(q.ArtistIDs) > 0 || len(q.LikeArtistIDs) > 0 ||
		q.Favourites || q.Unplayed || q.New || q.Rediscover || q.MaxTrackMinutes > 0
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
		f.Old = ids(`SELECT item_id FROM user_item_state WHERE user_id = ? AND (rating >= 8 OR play_count >= 3)
			AND (last_viewed_at IS NULL OR last_viewed_at < strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '-180 days'))`, userID)
	}
	if q.Unplayed {
		f.Played = ids(`SELECT item_id FROM user_item_state WHERE user_id = ? AND play_count > 0`, userID)
	}
	if q.New {
		f.Fresh = ids(`SELECT id FROM items WHERE type = 'track' AND added_at > strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '-60 days')`)
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
