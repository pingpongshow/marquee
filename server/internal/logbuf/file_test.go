package logbuf

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRotatingFile(t *testing.T) {
	dir := t.TempDir()
	r := &RotatingFile{Dir: dir, MaxBytes: 100, Keep: 2}
	line := strings.Repeat("x", 39) + "\n" // 40 bytes
	for i := 0; i < 8; i++ {
		r.Write([]byte(line))
	}
	for _, name := range []string{"marquee.log", "marquee.1.log", "marquee.2.log"} {
		b, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil || len(b) == 0 || len(b) > 100 {
			t.Errorf("%s: %d bytes, %v", name, len(b), err)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "marquee.3.log")); err == nil {
		t.Error("kept too many")
	}
}
