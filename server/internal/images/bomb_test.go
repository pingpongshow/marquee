package images

import (
	"bytes"
	"context"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"marquee/internal/db"
)

// An image file that claims enormous dimensions is refused before decoding, which would
// allocate the whole image (here 4 × 60000² bytes) and take the server down.
func TestHugeImageRefused(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(dir, "t.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	var b bytes.Buffer
	png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 1, 1)))
	data := b.Bytes()
	// IHDR follows the 8-byte signature: length, "IHDR", width, height… then its CRC.
	binary.BigEndian.PutUint32(data[16:], 60000)
	binary.BigEndian.PutUint32(data[20:], 60000)
	binary.BigEndian.PutUint32(data[29:], crc32.ChecksumIEEE(data[12:29]))
	src := filepath.Join(dir, "logo.png")
	os.WriteFile(src, data, 0o644)
	d.Exec(`INSERT INTO libraries(id, name, type) VALUES (1, 'M', 'movies')`)
	d.Exec(`INSERT INTO items(id, library_id, type, title, sort_title) VALUES (1, 1, 'movie', 'x', 'x')`)
	d.Exec(`INSERT INTO artwork(id, item_id, kind, source, local_path, selected) VALUES (7, 1, 'logo', 'local', ?, 1)`, src)
	s := New(d, filepath.Join(dir, "cache"), "ffmpeg")
	if _, _, err := s.Path(ctx, 7, 300); err == nil || !strings.Contains(err.Error(), "too large") {
		t.Fatalf("got %v, want a too-large error", err)
	}
}
