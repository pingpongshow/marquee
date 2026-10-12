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
