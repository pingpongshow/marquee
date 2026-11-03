package appearance

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func pngOf(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.NRGBA{R: 200, A: 128})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// save stores an image with a commit that always succeeds, returning its format.
func save(s *Store, r io.Reader) (string, error) {
	var format string
	err := s.Replace(r, func(f string) error { format = f; return nil })
	return format, err
}

func TestSaveKeepsFormatAndReplaces(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	format, err := save(s, bytes.NewReader(pngOf(t, 64, 64)))
	if err != nil || format != "png" {
		t.Fatalf("png: %q, %v", format, err)
	}
	if _, err := os.Stat(s.Path("png")); err != nil {
		t.Fatal(err)
	}
	// A transparent pattern keeps its transparency.
	f, _ := os.Open(s.Path("png"))
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, a := img.At(0, 0).RGBA(); a == 0xffff {
		t.Error("lost transparency")
	}

	var j bytes.Buffer
	jpeg.Encode(&j, image.NewRGBA(image.Rect(0, 0, 32, 32)), nil)
	format, err = save(s, &j)
	if err != nil || format != "jpg" {
		t.Fatalf("jpeg: %q, %v", format, err)
	}
	if _, err := os.Stat(s.Path("png")); !errors.Is(err, os.ErrNotExist) {
		t.Error("the old PNG is still there")
	}
	clear := func() error { return nil }
	if err := s.Clear(clear); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.Path("jpg")); !errors.Is(err, os.ErrNotExist) {
		t.Error("Clear left the image")
	}
	if err := s.Clear(clear); err != nil {
		t.Errorf("clearing nothing: %v", err)
	}
}

func TestSaveRefuses(t *testing.T) {
	gif := append([]byte("GIF89a"), make([]byte, 64)...)
	for name, body := range map[string]string{
		"svg":        `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`,
		"xml svg":    `<?xml version="1.0"?><svg xmlns="http://www.w3.org/2000/svg"/>`,
		"html":       `<!doctype html><script>alert(1)</script>`,
		"gif":        string(gif),
		"png header": "\x89PNG\r\n\x1a\n not really",
		"empty":      "",
	} {
		s := &Store{Dir: t.TempDir()}
		if _, err := save(s, strings.NewReader(body)); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: %v", name, err)
		}
		if _, err := os.Stat(s.Path("png")); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s: something was stored", name)
		}
	}
}

func TestSaveLimits(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	if _, err := save(s, bytes.NewReader(make([]byte, MaxBytes+1))); !errors.Is(err, ErrTooLarge) {
		t.Errorf("too many bytes: %v", err)
	}
	if _, err := save(s, bytes.NewReader(pngOf(t, 9000, 8000))); !errors.Is(err, ErrInvalid) {
		t.Errorf("too many pixels: %v", err)
	}
	if _, err := save(s, bytes.NewReader(pngOf(t, 5000, 1000))); err != nil {
		t.Fatal(err)
	}
	f, _ := os.Open(s.Path("png"))
	defer f.Close()
	cfg, err := png.DecodeConfig(f)
	if err != nil || cfg.Width != maxSide || cfg.Height != 768 {
		t.Errorf("large image stored at %dx%d, %v", cfg.Width, cfg.Height, err)
	}
}

func jpegOf(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 32, 16)) // red on the left, blue on the right
	for y := range 16 {
		for x := range 32 {
			c := color.RGBA{R: 255, A: 255}
			if x >= 16 {
				c = color.RGBA{B: 255, A: 255}
			}
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// withOrientation adds an EXIF segment holding the orientation tag after the JPEG's header.
func withOrientation(jpg []byte, order binary.ByteOrder, orientation uint16) []byte {
	var tiff bytes.Buffer
	if order == binary.LittleEndian {
		tiff.WriteString("II")
	} else {
		tiff.WriteString("MM")
	}
	binary.Write(&tiff, order, uint16(42))
	binary.Write(&tiff, order, uint32(8))
	binary.Write(&tiff, order, uint16(1))
	binary.Write(&tiff, order, []uint16{0x0112, 3}) // tag, type SHORT
	binary.Write(&tiff, order, uint32(1))
	binary.Write(&tiff, order, []uint16{orientation, 0})
	binary.Write(&tiff, order, uint32(0))
	segment := append([]byte("Exif\x00\x00"), tiff.Bytes()...)
	out := []byte{0xFF, 0xD8, 0xFF, 0xE1, byte((len(segment) + 2) >> 8), byte(len(segment) + 2)}
	out = append(out, segment...)
	return append(out, jpg[2:]...)
}

// A phone photo tagged to be turned a quarter turn clockwise comes out the right way up: the
// left of the sensor's picture is now the top.
func TestPhotoOrientation(t *testing.T) {
	for name, order := range map[string]binary.ByteOrder{"little-endian": binary.LittleEndian, "big-endian": binary.BigEndian} {
		s := &Store{Dir: t.TempDir()}
		if _, err := save(s, bytes.NewReader(withOrientation(jpegOf(t), order, 6))); err != nil {
			t.Fatal(name, err)
		}
		f, err := os.Open(s.Path("jpg"))
		if err != nil {
			t.Fatal(name, err)
		}
		defer f.Close()
		img, err := jpeg.Decode(f)
		if err != nil {
			t.Fatal(name, err)
		}
		if got := img.Bounds().Size(); got != image.Pt(16, 32) {
			t.Fatalf("%s: stored at %v", name, got)
		}
		if r, _, b, _ := img.At(8, 4).RGBA(); r < b {
			t.Errorf("%s: the top isn't red", name)
		}
		if r, _, b, _ := img.At(8, 28).RGBA(); b < r {
			t.Errorf("%s: the bottom isn't blue", name)
		}
	}
}

func TestOrient(t *testing.T) {
	// A 2×1 image, a then b. Where each lands, for every orientation.
	a, b := color.RGBA{R: 200, A: 255}, color.RGBA{B: 200, A: 255}
	src := image.NewRGBA(image.Rect(0, 0, 2, 1))
	src.Set(0, 0, a)
	src.Set(1, 0, b)
	for orientation, want := range map[int]struct{ size, a, b image.Point }{
		1: {image.Pt(2, 1), image.Pt(0, 0), image.Pt(1, 0)},
		2: {image.Pt(2, 1), image.Pt(1, 0), image.Pt(0, 0)},
		3: {image.Pt(2, 1), image.Pt(1, 0), image.Pt(0, 0)},
		4: {image.Pt(2, 1), image.Pt(0, 0), image.Pt(1, 0)},
		5: {image.Pt(1, 2), image.Pt(0, 0), image.Pt(0, 1)},
		6: {image.Pt(1, 2), image.Pt(0, 0), image.Pt(0, 1)},
		7: {image.Pt(1, 2), image.Pt(0, 1), image.Pt(0, 0)},
		8: {image.Pt(1, 2), image.Pt(0, 1), image.Pt(0, 0)},
	} {
		got := orient(src, orientation)
		if got.Bounds().Size() != want.size || got.At(want.a.X, want.a.Y) != a || got.At(want.b.X, want.b.Y) != b {
			t.Errorf("orientation %d: size %v", orientation, got.Bounds().Size())
		}
	}
}

// Bad metadata means the picture as it is, never a panic or a refusal.
func TestOrientationIgnoresBadData(t *testing.T) {
	good := withOrientation(jpegOf(t), binary.LittleEndian, 6)
	if got := jpegOrientation(good); got != 6 {
		t.Fatalf("good: %d", got)
	}
	for i := range 40 {
		jpegOrientation(good[:i])
	}
	if got := jpegOrientation(withOrientation(jpegOf(t), binary.LittleEndian, 99)); got != 1 {
		t.Errorf("orientation 99: %d", got)
	}
	if got := jpegOrientation(jpegOf(t)); got != 1 {
		t.Errorf("no EXIF: %d", got)
	}
}

// If the settings can't be saved they still point at the old image, so it must still be there.
func TestReplaceKeepsOldImageWhenCommitFails(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	if _, err := save(s, bytes.NewReader(pngOf(t, 8, 8))); err != nil {
		t.Fatal(err)
	}
	failed := errors.New("no")
	if err := s.Replace(bytes.NewReader(jpegOf(t)), func(string) error { return failed }); !errors.Is(err, failed) {
		t.Fatalf("err: %v", err)
	}
	if _, err := os.Stat(s.Path("png")); err != nil {
		t.Errorf("old image: %v", err)
	}
	if _, err := os.Stat(s.Path("jpg")); !errors.Is(err, os.ErrNotExist) {
		t.Error("the new image was left over")
	}
	if err := s.Clear(func() error { return failed }); !errors.Is(err, failed) {
		t.Fatalf("clear: %v", err)
	}
	if _, err := os.Stat(s.Path("png")); err != nil {
		t.Errorf("after a failed clear: %v", err)
	}
}

// Uploads at the same moment in different formats always leave the file the settings name,
// complete, and nothing else.
func TestConcurrentReplacesStayConsistent(t *testing.T) {
	s := &Store{Dir: t.TempDir()}
	p, j := pngOf(t, 8, 8), jpegOf(t)
	var settingsFormat string // guarded by the store's lock, like the real settings
	var wg sync.WaitGroup
	for i := range 16 {
		body := p
		if i%2 == 1 {
			body = j
		}
		wg.Go(func() {
			if err := s.Replace(bytes.NewReader(body), func(f string) error { settingsFormat = f; return nil }); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	f, err := os.Open(s.Path(settingsFormat))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, _, err := image.Decode(f); err != nil {
		t.Errorf("stored image: %v", err)
	}
	files, _ := filepath.Glob(filepath.Join(s.Dir, "*"))
	if len(files) != 1 {
		t.Errorf("left behind: %v", files)
	}
}
