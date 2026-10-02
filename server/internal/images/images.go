// Package images serves artwork: local files, cover art embedded in audio files, and
// provider images (downloaded on first use). Images are resized to a fixed set of widths
// and cached on disk. An artwork row never changes content, so responses are immutable.
package images

import (
	"bytes"
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

var ErrNotFound = errors.New("image not found")

// Widths are the sizes images are resized to; requests snap up to the next one.
var Widths = []int{120, 240, 360, 480, 720, 960, 1280, 1920}

type Service struct {
	DB       *sql.DB
	CacheDir string
	FFmpeg   string
	HTTP     *http.Client

	mu       sync.Mutex
	inflight map[string]*call
	sem      chan struct{}
}

type call struct {
	done chan struct{}
	path string
	err  error
}

func New(db *sql.DB, cacheDir, ffmpeg string) *Service {
	return &Service{DB: db, CacheDir: cacheDir, FFmpeg: ffmpeg, HTTP: &http.Client{Timeout: 30 * time.Second},
		inflight: map[string]*call{}, sem: make(chan struct{}, 6)}
}

// SnapWidth rounds w up to a cached width; 0 means the largest.
func SnapWidth(w int) int {
	for _, s := range Widths {
		if w <= s && w > 0 {
			return s
		}
	}
	return Widths[len(Widths)-1]
}

type artwork struct {
	kind, source, localPath, remoteURL string
}

// Path returns a cached file for the artwork at the given width, generating it if needed.
// The returned content type is image/jpeg or image/png (logos keep transparency).
func (s *Service) Path(ctx context.Context, artworkID int64, width int) (string, string, error) {
	width = SnapWidth(width)
	var a artwork
	var local, remote sql.NullString
	err := s.DB.QueryRowContext(ctx, `SELECT kind, source, local_path, remote_url FROM artwork WHERE id = ?`, artworkID).
		Scan(&a.kind, &a.source, &local, &remote)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", ErrNotFound
	}
	if err != nil {
		return "", "", err
	}
	a.localPath, a.remoteURL = local.String, remote.String
	return s.pathFor(ctx, a, width)
}

// PersonPhoto returns a cached cast/crew photo.
func (s *Service) PersonPhoto(ctx context.Context, personID int64, width int) (string, string, error) {
	var url sql.NullString
	err := s.DB.QueryRowContext(ctx, `SELECT photo_path FROM people WHERE id = ?`, personID).Scan(&url)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !url.Valid) {
		return "", "", ErrNotFound
	}
	if err != nil {
		return "", "", err
	}
	return s.pathFor(ctx, artwork{kind: "person", source: "remote", remoteURL: url.String}, SnapWidth(width))
}

func (s *Service) pathFor(ctx context.Context, a artwork, width int) (string, string, error) {
	ext, ctype := "jpg", "image/jpeg"
	if a.kind == "logo" || a.kind == "clearart" {
		ext, ctype = "png", "image/png"
	}
	key := cacheKey(a)
	out := filepath.Join(s.CacheDir, "resized", key[:2], fmt.Sprintf("%s_%d.%s", key, width, ext))
	if _, err := os.Stat(out); err == nil {
		return out, ctype, nil
	}
	p, err := s.once(out, func() error {
		// Others may be waiting on this render: one client going away mustn't fail it.
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
		defer cancel()
		return s.render(rctx, a, key, width, out)
	})
	return p, ctype, err
}

// once deduplicates concurrent generation of the same output file.
func (s *Service) once(out string, fn func() error) (string, error) {
	s.mu.Lock()
	if c, ok := s.inflight[out]; ok {
		s.mu.Unlock()
		<-c.done
		return c.path, c.err
	}
	c := &call{done: make(chan struct{}), path: out}
	s.inflight[out] = c
	s.mu.Unlock()

	s.sem <- struct{}{}
	c.err = fn()
	<-s.sem

	s.mu.Lock()
	delete(s.inflight, out)
	s.mu.Unlock()
	close(c.done)
	return c.path, c.err
}

func cacheKey(a artwork) string {
	h := sha1.Sum([]byte(a.source + "\x00" + a.localPath + "\x00" + a.remoteURL))
	return hex.EncodeToString(h[:])
}

func (s *Service) render(ctx context.Context, a artwork, key string, width int, out string) error {
	src, err := s.original(ctx, a, key)
	if err != nil {
		return err
	}
	img, _, err := image.Decode(bytes.NewReader(src))
	if err != nil {
		return fmt.Errorf("decode image: %w", err)
	}
	b := img.Bounds()
	if b.Dx() > width {
		h := b.Dy() * width / b.Dx()
		dst := image.NewRGBA(image.Rect(0, 0, width, h))
		draw.CatmullRom.Scale(dst, dst.Bounds(), img, b, draw.Src, nil)
		img = dst
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(out), ".tmp-*")
	if err != nil {
		return err
	}
	if strings.HasSuffix(out, ".png") {
		err = png.Encode(tmp, img)
	} else {
		err = jpeg.Encode(tmp, img, &jpeg.Options{Quality: 82})
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return os.Rename(tmp.Name(), out)
}

// original returns the source image bytes. Remote images are downloaded once and kept.
func (s *Service) original(ctx context.Context, a artwork, key string) ([]byte, error) {
	switch a.source {
	case "local", "plex":
		return os.ReadFile(a.localPath)
	case "embedded":
		return s.extractEmbedded(ctx, a.localPath)
	case "frame":
		return s.extractFrame(ctx, a.localPath)
	}
	if a.remoteURL == "" {
		return nil, ErrNotFound
	}
	cached := filepath.Join(s.CacheDir, "originals", key[:2], key)
	if data, err := os.ReadFile(cached); err == nil {
		return data, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, providerSized(a.kind, a.remoteURL), nil)
	if err != nil {
		return nil, err
	}
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: HTTP %d", a.remoteURL, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 30<<20))
	if err != nil {
		return nil, err
	}
	os.MkdirAll(filepath.Dir(cached), 0o755)
	os.WriteFile(cached, data, 0o644)
	return data, nil
}

// providerSized requests a reasonably sized TMDB rendition instead of the multi-megabyte
// original: plenty for every client, at a fraction of the disk and bandwidth.
func providerSized(kind, u string) string {
	const orig = "image.tmdb.org/t/p/original/"
	if !strings.Contains(u, orig) {
		return u
	}
	size := map[string]string{"poster": "w780", "backdrop": "w1280", "logo": "w500", "thumb": "w780", "person": "w185"}[kind]
	if size == "" {
		return u
	}
	return strings.Replace(u, orig, "image.tmdb.org/t/p/"+size+"/", 1)
}

// extractFrame grabs a representative frame (10 s in, or the first frame for short clips).
func (s *Service) extractFrame(ctx context.Context, mediaPath string) ([]byte, error) {
	for _, at := range []string{"10", "0"} {
		ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		cmd := exec.CommandContext(ctx, s.FFmpeg, "-v", "error", "-ss", at, "-i", mediaPath, "-frames:v", "1",
			"-vf", "scale='min(1280,iw)':-2", "-f", "image2pipe", "-c:v", "mjpeg", "-q:v", "3", "-")
		var out bytes.Buffer
		cmd.Stdout = &out
		err := cmd.Run()
		cancel()
		if err == nil && out.Len() > 0 {
			return out.Bytes(), nil
		}
	}
	return nil, fmt.Errorf("no frame could be extracted from %s", mediaPath)
}

func (s *Service) extractEmbedded(ctx context.Context, mediaPath string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, s.FFmpeg, "-v", "error", "-i", mediaPath, "-an", "-map", "0:v:0", "-c:v", "copy",
		"-frames:v", "1", "-f", "image2pipe", "-")
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("extract cover from %s: %w: %s", mediaPath, err, strings.TrimSpace(stderr.String()))
	}
	return out.Bytes(), nil
}
