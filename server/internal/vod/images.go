package vod

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Image returns a film's poster or backdrop, fetched once and kept on disk. A film with
// no poster falls back to the playlist's logo.
func (s *Service) Image(ctx context.Context, id int64, kind string) ([]byte, string, error) {
	var poster, backdrop, logo string
	err := s.DB.QueryRowContext(ctx, "SELECT poster_url, backdrop_url, logo_url FROM vod_movies WHERE id=?", id).Scan(&poster, &backdrop, &logo)
	if err != nil {
		return nil, "", ErrNotFound
	}
	src := poster
	if kind == "backdrop" {
		src = backdrop
	} else if src == "" {
		src = logo
	}
	return s.fetchImage(ctx, src)
}

// SeriesImage returns a series' poster (else the source's cover) or backdrop.
func (s *Service) SeriesImage(ctx context.Context, id int64, kind string) ([]byte, string, error) {
	var poster, backdrop, logo string
	err := s.DB.QueryRowContext(ctx, "SELECT poster_url, backdrop_url, logo_url FROM vod_series WHERE id=?", id).Scan(&poster, &backdrop, &logo)
	if err != nil {
		return nil, "", ErrNotFound
	}
	src := or(poster, logo)
	if kind == "backdrop" {
		src = backdrop
	}
	return s.fetchImage(ctx, src)
}

// SeasonImage returns a season's poster.
func (s *Service) SeasonImage(ctx context.Context, seriesID int64, season int) ([]byte, string, error) {
	var poster string
	if err := s.DB.QueryRowContext(ctx, "SELECT poster_url FROM vod_seasons WHERE series_id=? AND season=?", seriesID, season).
		Scan(&poster); err != nil {
		return nil, "", ErrNotFound
	}
	return s.fetchImage(ctx, poster)
}

// EpisodeImage returns an episode's still.
func (s *Service) EpisodeImage(ctx context.Context, id int64) ([]byte, string, error) {
	var still string
	if err := s.DB.QueryRowContext(ctx, "SELECT still_url FROM vod_episodes WHERE id=?", id).Scan(&still); err != nil {
		return nil, "", ErrNotFound
	}
	return s.fetchImage(ctx, still)
}

// fetchImage fetches an image once and keeps it on disk.
func (s *Service) fetchImage(ctx context.Context, src string) ([]byte, string, error) {
	if src == "" {
		return nil, "", ErrNotFound
	}
	h := sha1.Sum([]byte(src))
	path := filepath.Join(s.ImageDir, hex.EncodeToString(h[:10]))
	if b, err := os.ReadFile(path); err == nil {
		return b, http.DetectContentType(b), nil
	}
	body, err := s.get(ctx, src, "")
	if err != nil {
		return nil, "", err
	}
	defer body.Close()
	b, err := io.ReadAll(io.LimitReader(body, 8<<20))
	if err != nil {
		return nil, "", err
	}
	ct := http.DetectContentType(b)
	if !strings.HasPrefix(ct, "image/") {
		return nil, "", ErrNotFound
	}
	if s.ImageDir != "" {
		os.MkdirAll(s.ImageDir, 0o755)
		os.WriteFile(path, b, 0o644)
	}
	return b, ct, nil
}
