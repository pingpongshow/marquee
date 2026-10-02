package avatars

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func TestEncodeCropsToCenteredSquare(t *testing.T) {
	// 300×100: red | green | blue thirds. The square crop is the green middle.
	src := image.NewRGBA(image.Rect(0, 0, 300, 100))
	for x := 0; x < 300; x++ {
		c := []color.RGBA{{255, 0, 0, 255}, {0, 255, 0, 255}, {0, 0, 255, 255}}[x/100]
		for y := 0; y < 100; y++ {
			src.Set(x, y, c)
		}
	}
	var in bytes.Buffer
	png.Encode(&in, src)
	img, _, err := image.Decode(&in)
	if err != nil {
		t.Fatal(err)
	}
	out, err := Encode(img)
	if err != nil {
		t.Fatal(err)
	}
	got, err := jpeg.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatal(err)
	}
	if b := got.Bounds(); b.Dx() != Size || b.Dy() != Size {
		t.Fatalf("size %v", b)
	}
	for _, p := range []image.Point{{10, 10}, {Size - 10, Size - 10}, {Size / 2, Size / 2}} {
		r, g, bl, _ := got.At(p.X, p.Y).RGBA()
		if g>>8 < 200 || r>>8 > 60 || bl>>8 > 60 {
			t.Fatalf("pixel %v = %d,%d,%d; want green", p, r>>8, g>>8, bl>>8)
		}
	}
}
