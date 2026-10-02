package images

import (
	"context"
	"image"
	"image/color"
	"image/jpeg"
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
