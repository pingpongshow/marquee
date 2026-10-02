package logbuf

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// RotatingFile keeps the log on disk across restarts and container updates (ADM-6):
// marquee.log, rotated to marquee.1.log … marquee.<Keep>.log once it reaches MaxBytes.
type RotatingFile struct {
	Dir      string
	MaxBytes int64
	Keep     int

	mu   sync.Mutex
	f    *os.File
	size int64
}

func (r *RotatingFile) path(n int) string {
	if n == 0 {
		return filepath.Join(r.Dir, "marquee.log")
	}
	return filepath.Join(r.Dir, fmt.Sprintf("marquee.%d.log", n))
}

func (r *RotatingFile) open() error {
	if err := os.MkdirAll(r.Dir, 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(r.path(0), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	st, _ := f.Stat()
	r.f, r.size = f, 0
	if st != nil {
		r.size = st.Size()
	}
	return nil
}

func (r *RotatingFile) rotate() {
	r.f.Close()
	r.f = nil
	os.Remove(r.path(r.Keep))
	for n := r.Keep - 1; n >= 0; n-- {
		os.Rename(r.path(n), r.path(n+1))
	}
}

// Write never fails the caller: a log that can't be written mustn't stop the server.
func (r *RotatingFile) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f != nil && r.MaxBytes > 0 && r.size+int64(len(p)) > r.MaxBytes {
		r.rotate()
	}
	if r.f == nil {
		if err := r.open(); err != nil {
			return len(p), nil
		}
	}
	n, _ := r.f.Write(p)
	r.size += int64(n)
	return len(p), nil
}
