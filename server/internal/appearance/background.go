package appearance

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const (
	// MaxBytes limits an upload.
	MaxBytes = 20 << 20
	// maxPixels refuses images that are small files but huge once decoded.
	maxPixels = 64_000_000
	// maxSide is the longest edge stored; no screen needs more behind a page.
	maxSide = 3840
)

var (
	ErrInvalid  = errors.New("use a PNG, JPEG or WebP image")
	ErrTooLarge = fmt.Errorf("image is larger than %d MB", MaxBytes>>20)
)

// Store keeps the page background in the config dir. Whatever arrives is decoded by its
// actual bytes, never its name or claimed type, and re-encoded, so only a plain image is ever
// served, without any metadata the original carried (a photo's location, say): it's readable
// without signing in.
type Store struct {
	Dir string
	// mu keeps a change to the file and to the settings that point at it together, so two
	// uploads can't leave the settings naming a file the other deleted.
	mu sync.Mutex
}

// Path is where a background in format ("png" or "jpg") is stored.
func (s *Store) Path(format string) string {
	if format != "png" && format != "jpg" {
		format = "jpg"
	}
	return filepath.Join(s.Dir, "background."+format)
}

// Replace validates r and stores it, then calls commit with its format to record it in the
// settings. The previous image in another format goes only once commit has succeeded.
func (s *Store) Replace(r io.Reader, commit func(format string) error) error {
	out, format, err := prepare(r)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	_, statErr := os.Stat(s.Path(format))
	existed := statErr == nil
	if err := s.write(format, out); err != nil {
		return err
	}
	if err := commit(format); err != nil {
		if !existed { // the settings still name the old file; this one would be left over
			os.Remove(s.Path(format))
		}
		return err
	}
	for _, other := range []string{"png", "jpg"} {
		if other != format {
			if err := os.Remove(s.Path(other)); err != nil && !errors.Is(err, os.ErrNotExist) {
				slog.Warn("remove old background", "err", err)
			}
		}
	}
	return nil
}

// Clear calls commit to take the image out of the settings, then deletes it.
func (s *Store) Clear(commit func() error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := commit(); err != nil {
		return err
	}
	for _, format := range []string{"png", "jpg"} {
		if err := os.Remove(s.Path(format)); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

// prepare turns an upload into the bytes to store, and their format.
func prepare(r io.Reader) ([]byte, string, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxBytes+1))
	if err != nil {
		return nil, "", err
	}
	if len(data) > MaxBytes {
		return nil, "", ErrTooLarge
	}
	// The decoders other packages have registered don't count: these three formats only, and
	// never SVG, which can carry scripts and which Go can't decode anyway.
	cfg, kind, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (kind != "png" && kind != "jpeg" && kind != "webp") {
		return nil, "", ErrInvalid
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > maxPixels {
		return nil, "", ErrInvalid
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, "", ErrInvalid
	}
	if kind == "jpeg" {
		src = orient(src, jpegOrientation(data))
	}
	out, format, err := encode(shrink(src), kind)
	return out, format, err
}

// write puts out in place in one step, through a temp file of its own, so a reader never
// sees a partial image.
func (s *Store) write(format string, out []byte) error {
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.Dir, "background-*.tmp")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) // does nothing once renamed
	_, err = tmp.Write(out)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Chmod(tmp.Name(), 0o644)
	}
	if err != nil {
		return err
	}
	return os.Rename(tmp.Name(), s.Path(format))
}

func shrink(src image.Image) image.Image {
	b := src.Bounds()
	side := max(b.Dx(), b.Dy())
	if side <= maxSide {
		return src
	}
	dst := image.NewRGBA(image.Rect(0, 0, max(1, b.Dx()*maxSide/side), max(1, b.Dy()*maxSide/side)))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Src, nil)
	return dst
}

// encode keeps PNGs as PNGs (patterns stay crisp) and anything with transparency too; other
// images become JPEGs.
func encode(src image.Image, kind string) ([]byte, string, error) {
	var buf bytes.Buffer
	if kind == "png" || !opaque(src) {
		err := (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&buf, src)
		return buf.Bytes(), "png", err
	}
	err := jpeg.Encode(&buf, src, &jpeg.Options{Quality: 90})
	return buf.Bytes(), "jpg", err
}

func opaque(src image.Image) bool {
	if o, ok := src.(interface{ Opaque() bool }); ok {
		return o.Opaque()
	}
	return true
}

// jpegOrientation reads the EXIF orientation of a JPEG (1 to 8; 1, the normal way up, if it
// has none). Phones store photos as the sensor saw them and leave turning them to this tag,
// which re-encoding would lose.
func jpegOrientation(data []byte) int {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return 1
	}
	for i := 2; i+4 <= len(data); {
		if data[i] != 0xFF {
			return 1
		}
		marker := data[i+1]
		if marker == 0xFF { // padding
			i++
			continue
		}
		if marker == 0xDA || marker == 0xD9 { // the image data, or its end: no more metadata
			return 1
		}
		size := int(binary.BigEndian.Uint16(data[i+2:]))
		if size < 2 || i+2+size > len(data) {
			return 1
		}
		if segment := data[i+4 : i+2+size]; marker == 0xE1 && bytes.HasPrefix(segment, []byte("Exif\x00\x00")) {
			return exifOrientation(segment[6:])
		}
		i += 2 + size
	}
	return 1
}

// exifOrientation finds the orientation tag in the first directory of EXIF's TIFF structure.
func exifOrientation(t []byte) int {
	if len(t) < 8 {
		return 1
	}
	var order binary.ByteOrder
	switch string(t[:2]) {
	case "II":
		order = binary.LittleEndian
	case "MM":
		order = binary.BigEndian
	default:
		return 1
	}
	dir := uint64(order.Uint32(t[4:]))
	if dir+2 > uint64(len(t)) {
		return 1
	}
	count := uint64(order.Uint16(t[dir:]))
	for k := uint64(0); k < count; k++ {
		entry := dir + 2 + 12*k
		if entry+12 > uint64(len(t)) {
			return 1
		}
		if order.Uint16(t[entry:]) != 0x0112 {
			continue
		}
		if v := int(order.Uint16(t[entry+8:])); order.Uint16(t[entry+2:]) == 3 && v >= 1 && v <= 8 {
			return v
		}
		return 1
	}
	return 1
}

// orient turns src the way an EXIF orientation says it should be shown.
func orient(src image.Image, orientation int) image.Image {
	if orientation < 2 || orientation > 8 {
		return src
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	from := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(from, from.Bounds(), src, b.Min, draw.Src)
	size := image.Rect(0, 0, w, h)
	if orientation >= 5 { // these swap width and height
		size = image.Rect(0, 0, h, w)
	}
	to := image.NewRGBA(size)
	for y := range h {
		row := from.Pix[y*from.Stride:]
		for x := range w {
			var dx, dy int
			switch orientation {
			case 2:
				dx, dy = w-1-x, y
			case 3:
				dx, dy = w-1-x, h-1-y
			case 4:
				dx, dy = x, h-1-y
			case 5:
				dx, dy = y, x
			case 6:
				dx, dy = h-1-y, x
			case 7:
				dx, dy = h-1-y, w-1-x
			case 8:
				dx, dy = y, w-1-x
			}
			copy(to.Pix[dy*to.Stride+dx*4:], row[x*4:x*4+4])
		}
	}
	return to
}
