package metadata

import (
	"context"
	"strings"

	"marquee/internal/metadata/tmdb"
)

// MatchMovie finds a film on TMDB by title and year (0 = unknown) and returns its full
// details, for films that aren't library items: movies on demand (LIVE-6). It uses the
// library matcher's scoring, so a weak or ambiguous result is no match: nil, nil.
// Without a TMDB key it returns tmdb.ErrNoKey.
func (s *Service) MatchMovie(ctx context.Context, title string, year int) (*tmdb.Movie, error) {
	c, err := s.tmdb(s.Settings.Get().General.MetadataLanguage)
	if err != nil {
		return nil, err
	}
	id, err := searchMovie(ctx, c, title, year)
	if err == nil && id == 0 && strings.Contains(title, " - ") {
		id, err = searchMovie(ctx, c, strings.ReplaceAll(title, " - ", ": "), year)
	}
	if err != nil || id == 0 {
		return nil, err
	}
	return c.Movie(ctx, id)
}

// MovieByID returns a film's full details by its TMDB id, for movies on demand whose source
// names it (Xtream Codes). Without a TMDB key it returns tmdb.ErrNoKey.
func (s *Service) MovieByID(ctx context.Context, id int) (*tmdb.Movie, error) {
	c, err := s.tmdb(s.Settings.Get().General.MetadataLanguage)
	if err != nil {
		return nil, err
	}
	return c.Movie(ctx, id)
}
