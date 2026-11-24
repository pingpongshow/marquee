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
	"net"
	"net/http"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

var ErrNotFound = errors.New("image not found")

// ErrNotImage is returned for data that isn't a JPEG, PNG or WebP image this server can read.
var ErrNotImage = errors.New("not a JPEG, PNG or WebP image")

// ErrPrivateAddress is returned for an image link that leads to this machine or the home
// network.
var ErrPrivateAddress = errors.New("image links to this server or the home network aren't fetched")

// maxDownload bounds a downloaded image; a larger one is refused rather than cut short.
const maxDownload = 30 << 20

// Widths are the sizes images are resized to; requests snap up to the next one.
var Widths = []int{120, 240, 360, 480, 720, 960, 1280, 1920}

// maxPixels bounds the source images decoded (a 100-megapixel RGBA image takes 400 MB).
const maxPixels = 100_000_000

type Service struct {
	DB       *sql.DB
	CacheDir string
	FFmpeg   string
	HTTP     *http.Client
	// AllowPrivateURLs lets image links an administrator chose lead to private addresses
	// (tests serve them from loopback).
	AllowPrivateURLs bool

	chosenHTTP *http.Client // for links an administrator chose: public addresses only
	mu         sync.Mutex
	inflight   map[string]*call
	sem        chan struct{}
}

type call struct {
	done chan struct{}
	path string
	err  error
}

func New(db *sql.DB, cacheDir, ffmpeg string) *Service {
	s := &Service{DB: db, CacheDir: cacheDir, FFmpeg: ffmpeg, HTTP: &http.Client{Timeout: 30 * time.Second},
		inflight: map[string]*call{}, sem: make(chan struct{}, 6)}
	// The address is checked as each connection is made, so a redirect or a name that
	// resolves to a private address is refused too.
	dialer := &net.Dialer{Timeout: 10 * time.Second, Control: func(_, address string, _ syscall.RawConn) error {
		if s.AllowPrivateURLs {
			return nil
		}
		ap, err := netip.ParseAddrPort(address)
		if err != nil || !publicAddr(ap.Addr()) {
			return ErrPrivateAddress
		}
		return nil
	}}
	// No proxy: one would be dialled in the link's place, and fetch for it unchecked.
	s.chosenHTTP = &http.Client{Timeout: 30 * time.Second, Transport: &http.Transport{DialContext: dialer.DialContext,
		TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 20 * time.Second, MaxIdleConns: 4, IdleConnTimeout: time.Minute}}
	return s
}

// cgnat is the shared address space Tailscale uses, as private as the home network.
var cgnat = netip.MustParsePrefix("100.64.0.0/10")

// publicAddr reports whether a is an address on the internet: not this machine, the home
// network, a link-local address (cloud metadata) or Tailscale.
func publicAddr(a netip.Addr) bool {
	a = a.Unmap()
	return a.IsValid() && a.IsGlobalUnicast() && !a.IsPrivate() && !a.IsLoopback() && !a.IsLinkLocalUnicast() && !cgnat.Contains(a)
}

// CheckImage reads data's header: its size, or ErrNotImage when it isn't an image this server
// can decode (or is too large to).
func CheckImage(data []byte) (width, height int, err error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || cfg.Width <= 0 || cfg.Height <= 0 {
		return 0, 0, ErrNotImage
	}
	if cfg.Width*cfg.Height > maxPixels {
		return 0, 0, fmt.Errorf("%w: %dx%d is too large", ErrNotImage, cfg.Width, cfg.Height)
	}
	return cfg.Width, cfg.Height, nil
}

// CheckChosen downloads an image link an administrator chose for an item's artwork (META-13)
// and reports its size, so a link that isn't an image is refused when chosen rather than
// showing as a broken picture. The download is kept: showing the image doesn't fetch it again.
func (s *Service) CheckChosen(ctx context.Context, kind, url string) (width, height int, err error) {
	a := artwork{kind: kind, source: "chosen", remoteURL: url}
	data, err := s.original(ctx, a, cacheKey(a))
	if err != nil {
		return 0, 0, err
	}
	return CheckImage(data)
}

// FetchLink downloads the image at a link someone gave (a playlist's picture, MUSIC-30): public
// addresses only, and only an image. The caller keeps its own copy.
func (s *Service) FetchLink(ctx context.Context, url string) ([]byte, error) {
	a := artwork{kind: "poster", source: "chosen", remoteURL: url}
	return s.original(ctx, a, cacheKey(a))
}

// Uploaded returns a cached, resized copy of an image kept in the image cache (rel is its
// path inside it), as Path does for artwork.
func (s *Service) Uploaded(ctx context.Context, rel string, width int) (string, error) {
	p, _, err := s.pathFor(ctx, artwork{kind: "poster", source: "upload", localPath: rel}, SnapWidth(width))
	return p, err
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
	// Decoding allocates the full image first: refuse sizes that would exhaust memory.
	if cfg, _, err := image.DecodeConfig(bytes.NewReader(src)); err == nil && cfg.Width*cfg.Height > maxPixels {
		return fmt.Errorf("image too large: %dx%d", cfg.Width, cfg.Height)
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
	case "generated", "upload":
		// Made by the thumbnail task, or uploaded by an administrator (META-13); the path is
		// inside the image cache.
		p := filepath.Clean(a.localPath)
		if filepath.IsAbs(p) || strings.HasPrefix(p, "..") {
			return nil, ErrNotFound
		}
		return os.ReadFile(filepath.Join(s.CacheDir, p))
	}
	if a.remoteURL == "" {
		return nil, ErrNotFound
	}
	cached := filepath.Join(s.CacheDir, "originals", key[:2], key)
	if data, err := os.ReadFile(cached); err == nil {
		// Something kept before downloads were checked that isn't an image is fetched again.
		if _, _, cerr := image.DecodeConfig(bytes.NewReader(data)); cerr == nil {
			return data, nil
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, providerSized(a.kind, a.remoteURL), nil)
	if err != nil {
		return nil, err
	}
	client := s.HTTP
	if a.source == "chosen" && s.chosenHTTP != nil {
		client = s.chosenHTTP // any link an administrator gave (META-13)
	}
	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(err, ErrPrivateAddress) {
			return nil, ErrPrivateAddress
		}
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download %s: HTTP %d", a.remoteURL, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxDownload+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxDownload {
		return nil, fmt.Errorf("download %s: larger than %d MB", a.remoteURL, maxDownload>>20)
	}
	// Only an image is kept: a page or an error body cached here would stay the item's
	// picture for good, whatever the link served later.
	if _, _, err := CheckImage(data); err != nil {
		return nil, fmt.Errorf("download %s: %w", a.remoteURL, err)
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
