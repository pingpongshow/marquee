package metadata

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"marquee/internal/metadata/tmdb"
)

// ErrNoTrailer means neither a local trailer nor one on TMDB was found.
var ErrNoTrailer = errors.New("no trailer")

// Trailer is a movie's or show's trailer: a local trailer item, or a YouTube video.
type Trailer struct {
	LocalItemID int64
	YouTubeKey  string
	Name        string
}

type trailerCache struct {
	mu      sync.Mutex
	entries map[int64]trailerEntry
}

type trailerEntry struct {
	t   *Trailer
	err error
	at  time.Time
}

const trailerTTL = 24 * time.Hour

// Trailer finds itemID's trailer (PLAY-22): a local trailer extra first, else the best
// YouTube trailer TMDB lists. TMDB answers are cached for a day, misses included.
func (s *Service) Trailer(ctx context.Context, itemID int64) (*Trailer, error) {
	var local int64
	var name string
	err := s.DB.QueryRowContext(ctx, `SELECT id, title FROM items WHERE parent_id = ? AND extra_type = 'trailer' ORDER BY id LIMIT 1`, itemID).Scan(&local, &name)
	if err == nil {
		return &Trailer{LocalItemID: local, Name: name}, nil
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}

	s.trailers.mu.Lock()
	if e, ok := s.trailers.entries[itemID]; ok && time.Since(e.at) < trailerTTL {
		s.trailers.mu.Unlock()
		return e.t, e.err
	}
	s.trailers.mu.Unlock()

	var typ, tmdbID string
	err = s.DB.QueryRowContext(ctx, `SELECT i.type, COALESCE((SELECT value FROM external_ids WHERE item_id = i.id AND provider = 'tmdb'), '')
		FROM items i WHERE i.id = ?`, itemID).Scan(&typ, &tmdbID)
	if err != nil {
		return nil, err
	}
	id, _ := strconv.Atoi(tmdbID)
	kind := map[string]string{"movie": "movie", "show": "tv"}[typ]
	if kind == "" || id == 0 {
		return nil, ErrNoTrailer
	}
	c, err := s.tmdb(s.Settings.Get().General.MetadataLanguage)
	if err != nil {
		return nil, ErrNoTrailer
	}
	videos, err := c.Videos(ctx, kind, id)
	if err != nil {
		return nil, err // not cached: TMDB may be briefly unreachable
	}
	t, terr := bestTrailer(videos)
	s.trailers.mu.Lock()
	if s.trailers.entries == nil {
		s.trailers.entries = map[int64]trailerEntry{}
	}
	s.trailers.entries[itemID] = trailerEntry{t: t, err: terr, at: time.Now()}
	s.trailers.mu.Unlock()
	return t, terr
}

// bestTrailer picks a YouTube video: trailers before teasers, official ones first, then
// the newest.
func bestTrailer(videos []tmdb.Video) (*Trailer, error) {
	rank := func(v tmdb.Video) int {
		r := 0
		switch v.Type {
		case "Trailer":
			r += 4
		case "Teaser":
			r += 2
		default:
			return -1
		}
		if v.Official {
			r++
		}
		return r
	}
	var yt []tmdb.Video
	for _, v := range videos {
		if strings.EqualFold(v.Site, "YouTube") && v.Key != "" && rank(v) >= 0 {
			yt = append(yt, v)
		}
	}
	if len(yt) == 0 {
		return nil, ErrNoTrailer
	}
	sort.SliceStable(yt, func(i, j int) bool {
		if ri, rj := rank(yt[i]), rank(yt[j]); ri != rj {
			return ri > rj
		}
		return yt[i].PublishedAt > yt[j].PublishedAt
	})
	return &Trailer{YouTubeKey: yt[0].Key, Name: yt[0].Name}, nil
}
