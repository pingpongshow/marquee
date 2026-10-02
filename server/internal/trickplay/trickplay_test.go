package trickplay

import (
	"context"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestThumbHeight(t *testing.T) {
	for _, c := range []struct{ w, h, want int }{{1920, 1080, 180}, {3840, 1608, 134}, {720, 480, 214}, {0, 0, 180}} {
		if got := thumbHeight(c.w, c.h); got != c.want {
			t.Errorf("thumbHeight(%d, %d) = %d, want %d", c.w, c.h, got, c.want)
		}
	}
}

// Generates sheets from a synthetic 2.5-minute clip with sparse keyframes.
func TestGenerate(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "clip.mp4")
	out, err := exec.Command(ffmpeg, "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=640x360:rate=10:duration=150",
		"-c:v", "libx264", "-g", "40", "-preset", "ultrafast", src).CombinedOutput()
	if err != nil {
		t.Fatalf("make clip: %v %s", err, out)
	}
	s := &Service{FFmpeg: ffmpeg, Dir: filepath.Join(dir, "trickplay")}
	if err := s.generate(context.Background(), job{fileID: 7, path: src, durationMS: 150_000, width: 640, height: 360}); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(s.SheetPath(7, 0))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	cfg, err := jpeg.DecodeConfig(f)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Width != Width*Columns || cfg.Height != 180*Rows {
		t.Fatalf("sheet is %dx%d", cfg.Width, cfg.Height)
	}
	if _, err := os.Stat(s.SheetPath(7, 1)); err == nil {
		t.Fatal("15 thumbnails should fit one sheet")
	}
}
