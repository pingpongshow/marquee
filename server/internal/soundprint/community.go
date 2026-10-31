package soundprint

import (
	"context"
	"strings"
)

// What listeners elsewhere say (MUSIC-15): similar artists and community tags from
// ListenBrainz and Last.fm, fetched by the community package. Similar artists widen an
// artist's radio and Discovery beyond what merely sounds alike; tags ("melancholic",
// "chill", "female vocals") help moods and Muse.

// artistTagShare is how much an artist's tag counts for each of their tracks, against the
// track's own tag.
const artistTagShare = 0.7

// LoadCommunity reads the similar artists and tags into memory (at start, and after the
// community task fetches more).
func (s *Service) LoadCommunity(ctx context.Context) error {
	similar := map[int64]map[int64]float64{}
	rows, err := s.DB.QueryContext(ctx, `SELECT artist_id, similar_id, MAX(score) FROM music_similar_artists GROUP BY artist_id, similar_id`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var a, b int64
		var w float64
		if rows.Scan(&a, &b, &w) == nil {
			if similar[a] == nil {
				similar[a] = map[int64]float64{}
			}
			similar[a][b] = w
		}
	}
	rows.Close()
	tracks, artists := map[int64]map[string]float64{}, map[int64]map[string]float64{}
	rows, err = s.DB.QueryContext(ctx, `SELECT t.item_id, t.tag, MAX(t.weight), i.type FROM music_tags t JOIN items i ON i.id = t.item_id
		GROUP BY t.item_id, t.tag`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id int64
		var tag, typ string
		var w float64
		if rows.Scan(&id, &tag, &w, &typ) != nil {
			continue
		}
		into := tracks
		if typ == "artist" {
			into = artists
		}
		if into[id] == nil {
			into[id] = map[string]float64{}
		}
		into[id][tag] = w
	}
	rows.Close()
	s.learnMu.Lock()
	s.similar, s.tags, s.tagsByArt = similar, tracks, artists
	s.learnMu.Unlock()
	return nil
}

// SimilarArtists returns the artists listeners consider like this one (0–1, strongest 1).
func (s *Service) SimilarArtists(artistID int64) map[int64]float64 {
	s.learnMu.RLock()
	defer s.learnMu.RUnlock()
	return s.similar[artistID]
}

// TagScore is how strongly a track carries any of these tags, its own or its artist's
// (0–1). A word matches a tag that is it or contains it as a whole word ("chill" matches
// "chill out" but not "chillwave"), so a mood's words don't match unrelated tags.
func (s *Service) TagScore(t *Track, tags []string) float64 {
	if len(tags) == 0 {
		return 0
	}
	s.learnMu.RLock()
	own, art := s.tags[t.ItemID], s.tagsByArt[t.ArtistID]
	s.learnMu.RUnlock()
	best := 0.0
	for tag, w := range own {
		if tagMatches(tag, tags) {
			best = max(best, w)
		}
	}
	for tag, w := range art {
		if tagMatches(tag, tags) {
			best = max(best, artistTagShare*w)
		}
	}
	return best
}

func tagMatches(tag string, words []string) bool {
	for _, w := range words {
		if tag == w || strings.HasPrefix(tag, w+" ") || strings.HasSuffix(tag, " "+w) || strings.Contains(tag, " "+w+" ") {
			return true
		}
	}
	return false
}

// similarArtistBoost lifts the tracks of artists similar to these, for an artist's or a
// track's radio and Discovery: up to weight for the most similar.
func (s *Service) similarArtistBoost(artists map[int64]bool, weight float64, keep Filter) map[int64]float64 {
	near := map[int64]float64{}
	for a := range artists {
		for b, w := range s.SimilarArtists(a) {
			if !artists[b] {
				near[b] = max(near[b], w)
			}
		}
	}
	out := map[int64]float64{}
	if len(near) == 0 {
		return out
	}
	for _, t := range s.Index.Where(func(t *Track) bool { return near[t.ArtistID] > 0 && (keep == nil || keep(t)) }) {
		out[t.ItemID] = weight * near[t.ArtistID]
	}
	return out
}
