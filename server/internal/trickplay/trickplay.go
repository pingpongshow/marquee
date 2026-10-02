// Package trickplay makes seek-bar preview thumbnails (PLAY-13). Every video gets one
// small frame per Interval, packed into JPEG sprite sheets of Columns×Rows tiles, the
// way Jellyfin and Plex do it. Only keyframes are decoded, which makes generation fast
// enough to run over a whole library in the maintenance window.
package trickplay

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	Interval = 10 * time.Second
	Width    = 320
	Columns  = 10
	Rows     = 10
	PerSheet = Columns * Rows
	// minDuration skips clips too short to need previews.
	minDuration = 2 * time.Minute
)

// Info describes one file's sprite sheets.
type Info struct {
	FileID     int64
	IntervalMS int64
	Width      int
	Height     int
	Count      int // thumbnails
}

// Sheets is how many sprite images there are.
func (i Info) Sheets() int { return (i.Count + PerSheet - 1) / PerSheet }

// Service generates and serves trickplay sheets.
type Service struct {
	DB      *sql.DB
	FFmpeg  string
	Dir     string // <cache>/trickplay
	Workers int
	Enabled func() bool

	done, total atomic.Int64
}

// Progress reports the current run's progress.
func (s *Service) Progress() (done, total int) { return int(s.done.Load()), int(s.total.Load()) }

// SheetPath is the image for a sheet (0-based).
func (s *Service) SheetPath(fileID int64, sheet int) string {
	return filepath.Join(s.Dir, strconv.FormatInt(fileID, 10), fmt.Sprintf("%d.jpg", sheet+1))
}

// ForItem returns the sheets for an item's main file (first version, first part).
func (s *Service) ForItem(ctx context.Context, itemID int64) (Info, error) {
	var in Info
	err := s.DB.QueryRowContext(ctx, `
		SELECT t.file_id, t.interval_ms, t.width, t.height, t.count FROM media_versions v
		JOIN media_files f ON f.version_id = v.id AND f.part_index = 0
		JOIN trickplay t ON t.file_id = f.id AND t.error IS NULL AND t.mtime = f.mtime
		WHERE v.item_id = ? ORDER BY v.id LIMIT 1`, itemID).Scan(&in.FileID, &in.IntervalMS, &in.Width, &in.Height, &in.Count)
	return in, err
}

type job struct {
	fileID     int64
	path       string
	mtime      int64
	durationMS int64
	width      int
	height     int
	hdr        bool
}

// Run generates sheets for videos that don't have them yet (newest first). It stops when
// ctx ends (the maintenance window closes) and continues from there next time.
func (s *Service) Run(ctx context.Context) (string, error) {
	if s.Enabled != nil && !s.Enabled() {
		return "Turned off", nil
	}
	todo, err := s.pending(ctx)
	if err != nil {
		return "", err
	}
	s.done.Store(0)
	s.total.Store(int64(len(todo)))
	if len(todo) == 0 {
		return "Nothing to do", s.cleanup(ctx)
	}
	start := time.Now()
	jobs := make(chan job)
	var wg sync.WaitGroup
	var failed atomic.Int64
	for range max(1, s.Workers) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				err := s.generate(ctx, j)
				if ctx.Err() != nil {
					return
				}
				if err != nil {
					failed.Add(1)
					slog.Warn("trickplay", "path", j.path, "err", err)
				}
				s.record(context.WithoutCancel(ctx), j, err)
				s.done.Add(1)
			}
		}()
	}
loop:
	for _, j := range todo {
		select {
		case jobs <- j:
		case <-ctx.Done():
			break loop
		}
	}
	close(jobs)
	wg.Wait()
	n := s.done.Load()
	msg := fmt.Sprintf("Made previews for %d videos in %s", n, time.Since(start).Round(time.Second))
	if f := failed.Load(); f > 0 {
		msg += fmt.Sprintf(" (%d failed)", f)
	}
	if ctx.Err() != nil {
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			return msg + fmt.Sprintf("; %d left for the next maintenance window", int64(len(todo))-n), nil
		}
		return "", ctx.Err()
	}
	return msg, s.cleanup(ctx)
}

func (s *Service) pending(ctx context.Context) ([]job, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT f.id, f.path, f.mtime, f.duration_ms, COALESCE(f.width, 0), COALESCE(f.height, 0), COALESCE(f.hdr_format, '')
		FROM media_files f
		JOIN libraries l ON l.id = f.library_id AND l.type IN ('movies', 'shows', 'anime', 'videos')
		JOIN media_versions v ON v.id = f.version_id JOIN items i ON i.id = v.item_id
		LEFT JOIN trickplay t ON t.file_id = f.id
		WHERE f.available = 1 AND f.video_codec IS NOT NULL AND f.duration_ms >= ? AND COALESCE(f.width, 0) > 0
		  AND (t.file_id IS NULL OR (t.mtime != f.mtime))
		ORDER BY i.added_at DESC`, minDuration.Milliseconds())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []job
	for rows.Next() {
		var j job
		var hdr string
		if err := rows.Scan(&j.fileID, &j.path, &j.mtime, &j.durationMS, &j.width, &j.height, &hdr); err != nil {
			return nil, err
		}
		j.hdr = hdr != ""
		out = append(out, j)
	}
	return out, rows.Err()
}

// thumbHeight keeps the frame's shape at Width pixels wide (even, for the JPEG encoder).
func thumbHeight(w, h int) int {
	if w <= 0 || h <= 0 {
		return Width * 9 / 16
	}
	return max(2, int(math.Round(float64(Width)*float64(h)/float64(w)/2))*2)
}

// Args is the FFmpeg command for a file's sheets.
func Args(path string, height int, hdr bool, outPattern string) []string {
	// round=up makes tile n the last keyframe at or before n × Interval (the default
	// rounding picks one up to half an interval later).
	chain := []string{fmt.Sprintf("fps=1/%d:round=up", int(Interval.Seconds()))}
	if hdr {
		chain = append(chain, "tonemapx=tonemap=bt2390:desat=0:peak=100:t=bt709:m=bt709:p=bt709:format=yuv420p")
	}
	chain = append(chain, fmt.Sprintf("scale=%d:%d:flags=fast_bilinear", Width, height), fmt.Sprintf("tile=%dx%d", Columns, Rows))
	return []string{"-hide_banner", "-v", "error", "-threads", "2", "-skip_frame", "nokey", "-i", path,
		"-an", "-sn", "-dn", "-map", "0:v:0", "-vf", strings.Join(chain, ","), "-fps_mode", "passthrough",
		"-q:v", "5", "-f", "image2", outPattern}
}

func (s *Service) generate(ctx context.Context, j job) error {
	h := thumbHeight(j.width, j.height)
	final := filepath.Join(s.Dir, strconv.FormatInt(j.fileID, 10))
	tmp := final + ".tmp"
	os.RemoveAll(tmp)
	if err := os.MkdirAll(tmp, 0o755); err != nil {
		return err
	}
	args := Args(j.path, h, j.hdr, filepath.Join(tmp, "%d.jpg"))
	cmd := exec.CommandContext(ctx, s.FFmpeg, args...)
	if nice, err := exec.LookPath("nice"); err == nil {
		cmd = exec.CommandContext(ctx, nice, append([]string{"-n", "15", s.FFmpeg}, args...)...)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		os.RemoveAll(tmp)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("ffmpeg: %w: %s", err, strings.TrimSpace(lastLine(stderr.String())))
	}
	if _, err := os.Stat(filepath.Join(tmp, "1.jpg")); err != nil {
		os.RemoveAll(tmp)
		return errors.New("no frames decoded")
	}
	os.RemoveAll(final)
	return os.Rename(tmp, final)
}

func lastLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndex(s, "\n"); i >= 0 {
		return s[i+1:]
	}
	return s
}

func (s *Service) record(ctx context.Context, j job, genErr error) {
	// FFmpeg doesn't emit a tile for the final partial interval, so count whole ones.
	count := max(1, int(time.Duration(j.durationMS)*time.Millisecond/Interval))
	var msg any
	if genErr != nil {
		msg = genErr.Error()
	}
	_, err := s.DB.ExecContext(ctx, `INSERT INTO trickplay (file_id, mtime, interval_ms, width, height, count, error)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(file_id) DO UPDATE SET mtime = excluded.mtime, interval_ms = excluded.interval_ms, width = excluded.width,
		height = excluded.height, count = excluded.count, error = excluded.error,
		generated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')`,
		j.fileID, j.mtime, Interval.Milliseconds(), Width, thumbHeight(j.width, j.height), count, msg)
	if err != nil {
		slog.Warn("trickplay record", "err", err)
	}
}

// cleanup removes sheets for files that no longer exist.
func (s *Service) cleanup(ctx context.Context) error {
	entries, err := os.ReadDir(s.Dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, e := range entries {
		id, err := strconv.ParseInt(strings.TrimSuffix(e.Name(), ".tmp"), 10, 64)
		if err != nil {
			continue
		}
		var n int
		s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM trickplay WHERE file_id = ? AND error IS NULL`, id).Scan(&n)
		if n == 0 || strings.HasSuffix(e.Name(), ".tmp") {
			os.RemoveAll(filepath.Join(s.Dir, e.Name()))
		}
	}
	return nil
}
