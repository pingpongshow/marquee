// Package avatars stores user profile pictures (USER-11). Clients crop and position the
// picture; the server re-encodes whatever it receives as a 512×512 JPEG, so only images
// it can decode are ever stored or served.
package avatars

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

const (
	Size     = 512
	MaxBytes = 10 << 20
)

var (
	ErrInvalid  = errors.New("not a JPEG, PNG or WebP image")
	ErrTooLarge = fmt.Errorf("image is larger than %d MB", MaxBytes>>20)
)

type Store struct {
	DB  *sql.DB
	Dir string
}

func (s *Store) Path(userID int64) string {
	return filepath.Join(s.Dir, strconv.FormatInt(userID, 10)+".jpg")
}

// Save decodes r, center-crops it to a square, scales it to Size and stores it.
// It returns the new version.
func (s *Store) Save(ctx context.Context, userID int64, r io.Reader) (int64, error) {
	data, err := io.ReadAll(io.LimitReader(r, MaxBytes+1))
	if err != nil {
		return 0, err
	}
	if len(data) > MaxBytes {
		return 0, ErrTooLarge
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return 0, ErrInvalid
	}
	out, err := Encode(src)
	if err != nil {
		return 0, err
	}
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return 0, err
	}
	tmp := s.Path(userID) + ".tmp"
	if err := os.WriteFile(tmp, out, 0o644); err != nil {
		return 0, err
	}
	if err := os.Rename(tmp, s.Path(userID)); err != nil {
		return 0, err
	}
	version := time.Now().UnixMilli()
	_, err = s.DB.ExecContext(ctx, `UPDATE users SET avatar_version = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = ?`, version, userID)
	return version, err
}

// Remove deletes a user's picture.
func (s *Store) Remove(ctx context.Context, userID int64) error {
	if _, err := s.DB.ExecContext(ctx, `UPDATE users SET avatar_version = 0 WHERE id = ?`, userID); err != nil {
		return err
	}
	if err := os.Remove(s.Path(userID)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// Encode center-crops src to a square and returns it as a Size×Size JPEG.
func Encode(src image.Image) ([]byte, error) {
	b := src.Bounds()
	side := min(b.Dx(), b.Dy())
	if side == 0 {
		return nil, ErrInvalid
	}
	crop := image.Rect(0, 0, side, side).Add(image.Pt(b.Min.X+(b.Dx()-side)/2, b.Min.Y+(b.Dy()-side)/2))
	dst := image.NewRGBA(image.Rect(0, 0, Size, Size))
	draw.Draw(dst, dst.Bounds(), image.White, image.Point{}, draw.Src) // transparent PNGs get a white background
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, crop, draw.Over, nil)
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: 88}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
