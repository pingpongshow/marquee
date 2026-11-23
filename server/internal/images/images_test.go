package images

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"os"
	"path/filepath"
	"testing"

	"marquee/internal/db"
)

func TestLocalResizeAndCache(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(dir, "t.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	src := filepath.Join(dir, "poster.jpg")
	img := image.NewRGBA(image.Rect(0, 0, 1000, 1500))
	for x := 0; x < 1000; x++ {
		img.Set(x, x, color.RGBA{255, 0, 0, 255})
	}
	f, _ := os.Create(src)
	jpeg.Encode(f, img, nil)
	f.Close()
	d.Exec(`INSERT INTO libraries(id, name, type) VALUES (1, 'M', 'movies')`)
	d.Exec(`INSERT INTO items(id, library_id, type, title, sort_title) VALUES (1, 1, 'movie', 'x', 'x')`)
	d.Exec(`INSERT INTO artwork(id, item_id, kind, source, local_path, selected) VALUES (7, 1, 'poster', 'local', ?, 1)`, src)

	s := New(d, filepath.Join(dir, "cache"), "ffmpeg")
	p, ctype, err := s.Path(ctx, 7, 300)
	if err != nil {
		t.Fatal(err)
	}
	if ctype != "image/jpeg" {
		t.Errorf("ctype %s", ctype)
	}
	rf, _ := os.Open(p)
	cfg, _, err := image.DecodeConfig(rf)
	rf.Close()
	if err != nil || cfg.Width != 360 || cfg.Height != 540 {
		t.Fatalf("resized to %dx%d (%v), want 360x540", cfg.Width, cfg.Height, err)
	}
	// Second call is served from cache even if the source is gone.
	os.Remove(src)
	if p2, _, err := s.Path(ctx, 7, 360); err != nil || p2 != p {
		t.Fatalf("cache miss: %v %s", err, p2)
	}
	if _, _, err := s.Path(ctx, 999, 100); err != ErrNotFound {
		t.Errorf("missing artwork: %v", err)
	}
}

// A link an administrator chose is fetched only from the internet, and only an image is kept:
// a page cached in its place would stay the item's picture whatever the link served later.
func TestChosenLinkChecked(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(dir, "t.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	var pic bytes.Buffer
	png.Encode(&pic, image.NewRGBA(image.Rect(0, 0, 40, 60)))
	page := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if page {
			w.Write([]byte("<html>a page about the image</html>"))
			return
		}
		w.Write(pic.Bytes())
	}))
	defer srv.Close()
	s := New(d, filepath.Join(dir, "cache"), "ffmpeg")
	if _, _, err := s.CheckChosen(ctx, "poster", srv.URL+"/a.png"); !errors.Is(err, ErrPrivateAddress) {
		t.Fatalf("a link to this machine: %v", err)
	}
	s.AllowPrivateURLs = true
	if _, _, err := s.CheckChosen(ctx, "poster", srv.URL+"/a.png"); !errors.Is(err, ErrNotImage) {
		t.Fatalf("a page: %v", err)
	}
	// The page wasn't kept: once the link serves an image, it's the image.
	page = false
	if w, h, err := s.CheckChosen(ctx, "poster", srv.URL+"/a.png"); err != nil || w != 40 || h != 60 {
		t.Fatalf("an image: %dx%d %v", w, h, err)
	}
	srv.Close() // kept now: not fetched again
	if _, _, err := s.CheckChosen(ctx, "poster", srv.URL+"/a.png"); err != nil {
		t.Fatalf("the kept image: %v", err)
	}
}

func TestPublicAddr(t *testing.T) {
	for addr, want := range map[string]bool{
		"8.8.8.8": true, "2606:4700:4700::1111": true,
		"127.0.0.1": false, "::1": false, "10.1.2.3": false, "192.168.1.10": false, "172.16.0.1": false,
		"169.254.169.254": false, "100.100.1.1": false, "fd7a:115c:a1e0::1": false, "fe80::1": false,
		"::ffff:192.168.1.10": false, "0.0.0.0": false, "224.0.0.1": false,
	} {
		if got := publicAddr(netip.MustParseAddr(addr)); got != want {
			t.Errorf("%s: public %v, want %v", addr, got, want)
		}
	}
}
