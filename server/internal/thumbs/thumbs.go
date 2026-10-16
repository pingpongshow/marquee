// Package thumbs makes thumbnails for episodes and personal videos that have no still
// (TMDB has none for many episodes): a representative frame from about 30% into the file,
// picked by FFmpeg's thumbnail filter from a few seconds of video (so it's rarely black or
// blurred), 640 pixels wide, stored as a JPEG in the image cache with an artwork row of
// source 'generated'. A real still always wins: adding one removes the generated thumb
// (a trigger, migration 00042), and items with one are never given a generated thumb.
package thumbs

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"marquee/internal/encode"
)

// Width is how wide generated thumbnails are.
const Width = 640

// Dir is the folder in the image cache holding generated thumbnails.
const Dir = "generated"

// Service generates thumbnails.
type Service struct {
	DB     *sql.DB
	FFmpeg string
	// CacheDir is the image cache (images.Service.CacheDir); thumbnails go in CacheDir/generated.
	CacheDir string
	// CUDA decodes on the NVIDIA GPU, falling back to the CPU.
	CUDA bool
	// Pace is the pause between files.
	Pace time.Duration
	// LookPath finds nice and ionice (tests); nil = exec.LookPath.
	LookPath func(string) (string, error)

	running     atomic.Bool
	done, total atomic.Int64
}

// Progress reports the current run's progress.
func (s *Service) Progress() (done, total int) { return int(s.done.Load()), int(s.total.Load()) }

// Candidate is an item without a still and the file to take a frame from.
type Candidate struct {
	ItemID, FileID    int64
	Path              string
	MTime, DurationMS int64
	Width, Height     int
	HDR               bool
}

// Pending lists the episodes and personal videos (not extras) with no still and no
// generated thumb, newest first: no thumbnail at all, or for a video only the frame the
// scan picked at 10 s. Files that failed before are left out until they change.
func (s *Service) Pending(ctx context.Context) ([]Candidate, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT i.id, f.id, f.path, f.mtime, f.duration_ms, COALESCE(f.width, 0), COALESCE(f.height, 0), COALESCE(f.hdr_format, '')
		FROM items i
		CROSS JOIN media_versions v ON v.item_id = i.id
		CROSS JOIN media_files f ON f.version_id = v.id AND f.part_index = 0
		LEFT JOIN generated_thumb_failures g ON g.file_id = f.id AND g.mtime = f.mtime
		WHERE i.type IN ('episode', 'video') AND i.extra_type IS NULL
		  AND f.available = 1 AND f.duration_ms > 0 AND f.video_codec IS NOT NULL AND g.file_id IS NULL
		  AND NOT EXISTS (SELECT 1 FROM artwork a WHERE a.item_id = i.id AND a.kind = 'thumb'
				AND (a.source <> 'frame' OR i.type = 'episode'))
		GROUP BY i.id
		ORDER BY i.added_at DESC, i.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Candidate
	for rows.Next() {
		var c Candidate
		var hdr string
		if err := rows.Scan(&c.ItemID, &c.FileID, &c.Path, &c.MTime, &c.DurationMS, &c.Width, &c.Height, &hdr); err != nil {
			return nil, err
		}
		c.HDR = hdr != ""
		out = append(out, c)
	}
	return out, rows.Err()
}

// Run generates thumbnails for every pending item, one at a time at low priority, pausing
// Pace between them; items added while it runs (a scan finished meanwhile) are done too.
// It stops when ctx ends (the maintenance window closes) and continues from there next time.
func (s *Service) Run(ctx context.Context) (string, error) {
	if !s.running.CompareAndSwap(false, true) {
		return "Already running", nil
	}
	defer s.running.Store(false)
	todo, err := s.Pending(ctx)
	if err != nil {
		return "", err
	}
	s.done.Store(0)
	s.total.Store(int64(len(todo)))
	tried := map[int64]bool{}
	var made, failed, left int
	for len(todo) > 0 && ctx.Err() == nil {
		for i, c := range todo {
			if i > 0 && s.Pace > 0 {
				select {
				case <-ctx.Done():
				case <-time.After(s.Pace):
				}
			}
			if ctx.Err() != nil {
				left += len(todo) - i
				break
			}
			tried[c.ItemID] = true
			err := s.Generate(ctx, c)
			if ctx.Err() != nil {
				left += len(todo) - i
				break
			}
			if err != nil {
				failed++
				slog.Info("thumbnails: no frame", "path", c.Path, "err", err)
			} else {
				made++
			}
			s.done.Add(1)
		}
		if ctx.Err() != nil {
			break
		}
		// Anything new since the list was made?
		more, err := s.Pending(ctx)
		if err != nil {
			return "", err
		}
		todo = todo[:0]
		for _, c := range more {
			if !tried[c.ItemID] {
				todo = append(todo, c)
			}
		}
		s.total.Add(int64(len(todo)))
	}
	if made == 0 && failed == 0 && ctx.Err() == nil {
		return "Nothing to do", s.cleanup(ctx)
	}
	msg := fmt.Sprintf("Made %d thumbnails", made)
	if failed > 0 {
		msg += fmt.Sprintf(" (%d files couldn't be read)", failed)
	}
	if err := ctx.Err(); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return msg + fmt.Sprintf("; %d left for next time", left), nil
		}
		return "", err
	}
	return msg, s.cleanup(ctx)
}

// Position is where the frame is looked for: about 30% in, but never in the first two or
// the last three minutes (titles and credits) when the file is long enough to avoid them.
func Position(durationMS int64) float64 {
	d := float64(durationMS) / 1000
	at := d * 0.3
	lo, hi := min(120, d*0.1), d-min(180, d*0.1)
	return max(lo, min(at, hi))
}

// Args is the FFmpeg command that writes the frame to out: 4 seconds from at, the most
// representative frame of them, tone-mapped when HDR, scaled to Width.
func Args(path string, at float64, hdr, cuda bool, out string) []string {
	a := []string{"-hide_banner", "-v", "error", "-nostdin", "-y"}
	if cuda {
		a = append(a, "-hwaccel", "cuda")
	}
	a = append(a, "-ss", strconv.FormatFloat(at, 'f', 3, 64), "-t", "4", "-i", path, "-map", "0:V:0", "-an", "-sn", "-dn")
	chain := []string{"thumbnail=n=60"}
	if hdr {
		chain = append(chain, encode.ToneMap)
	}
	chain = append(chain, fmt.Sprintf("scale=%d:-2", Width), "format=yuvj420p")
	return append(a, "-vf", strings.Join(chain, ","), "-frames:v", "1", "-c:v", "mjpeg", "-q:v", "3", "-f", "image2", out)
}

// Generate takes a frame for one candidate and makes it the item's thumbnail. A failure
// is remembered for the file as it is.
func (s *Service) Generate(ctx context.Context, c Candidate) error {
	if _, err := os.Stat(c.Path); err != nil {
		s.failed(ctx, c, "the file can't be read")
		return fmt.Errorf("the file can't be read: %w", err)
	}
	rel := filepath.Join(Dir, fmt.Sprintf("%02d", c.ItemID%100), fmt.Sprintf("%d-%d-%d.jpg", c.ItemID, c.FileID, time.Now().UnixNano()))
	out := filepath.Join(s.CacheDir, rel)
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	tmp := out + ".tmp"
	err := s.frame(ctx, c, Position(c.DurationMS), tmp)
	if err != nil && ctx.Err() == nil {
		err = s.frame(ctx, c, float64(c.DurationMS)/2000, tmp) // the middle, if the first try found nothing
	}
	if err != nil {
		os.Remove(tmp)
		if ctx.Err() == nil {
			s.failed(ctx, c, err.Error())
		}
		return err
	}
	if err := os.Rename(tmp, out); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := s.save(ctx, c, rel); err != nil {
		os.Remove(out)
		return err
	}
	return nil
}

func (s *Service) frame(ctx context.Context, c Candidate, at float64, out string) error {
	try := func(cuda bool) error {
		cctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
		defer cancel()
		name, args := encode.LowPriority(s.LookPath, or(s.FFmpeg, "ffmpeg"), Args(c.Path, at, c.HDR, cuda, out)...)
		cmd := exec.CommandContext(cctx, name, args...)
		var stderr bytes.Buffer
		cmd.Stderr = &encode.LimitWriter{W: &stderr, N: 4096}
		if err := cmd.Run(); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return fmt.Errorf("FFmpeg: %s", encode.LastLine(stderr.String(), err))
		}
		if fi, err := os.Stat(out); err != nil || fi.Size() == 0 {
			return errors.New("no frame came out")
		}
		return nil
	}
	if s.CUDA {
		if err := try(true); err == nil || ctx.Err() != nil {
			return err
		}
	}
	return try(false)
}

// save records the thumbnail as the item's selected thumb, unless a real still arrived
// meanwhile. A video's scan-time frame stays but isn't selected.
func (s *Service) save(ctx context.Context, c Candidate, rel string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var real int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM artwork WHERE item_id = ? AND kind = 'thumb' AND source NOT IN ('frame', 'generated')`,
		c.ItemID).Scan(&real); err != nil {
		return err
	}
	if real > 0 {
		return errors.New("the item has a still now")
	}
	var old []string
	rows, err := tx.QueryContext(ctx, `SELECT local_path FROM artwork WHERE item_id = ? AND kind = 'thumb' AND source = 'generated'`, c.ItemID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var p sql.NullString
		if rows.Scan(&p) == nil && p.String != "" {
			old = append(old, p.String)
		}
	}
	rows.Close()
	if _, err := tx.ExecContext(ctx, `DELETE FROM artwork WHERE item_id = ? AND kind = 'thumb' AND source = 'generated'`, c.ItemID); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE artwork SET selected = 0 WHERE item_id = ? AND kind = 'thumb'`, c.ItemID); err != nil {
		return err
	}
	h := Width * 9 / 16
	if c.Width > 0 && c.Height > 0 {
		h = Width * c.Height / c.Width
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO artwork (item_id, kind, source, local_path, width, height, selected) VALUES (?, 'thumb', 'generated', ?, ?, ?, 1)`,
		c.ItemID, rel, Width, h); err != nil {
		return err
	}
	tx.ExecContext(ctx, `DELETE FROM generated_thumb_failures WHERE file_id = ?`, c.FileID)
	if err := tx.Commit(); err != nil {
		return err
	}
	for _, p := range old {
		os.Remove(filepath.Join(s.CacheDir, p))
	}
	return nil
}

func (s *Service) failed(ctx context.Context, c Candidate, msg string) {
	s.DB.ExecContext(context.WithoutCancel(ctx), `INSERT INTO generated_thumb_failures (file_id, mtime, error) VALUES (?, ?, ?)
		ON CONFLICT(file_id) DO UPDATE SET mtime = excluded.mtime, error = excluded.error, at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')`,
		c.FileID, c.MTime, msg)
}

// cleanup removes generated images no artwork row points at (a real still replaced them,
// or the item went).
func (s *Service) cleanup(ctx context.Context) error {
	root := filepath.Join(s.CacheDir, Dir)
	if _, err := os.Stat(root); err != nil {
		return nil
	}
	known := map[string]bool{}
	rows, err := s.DB.QueryContext(ctx, `SELECT local_path FROM artwork WHERE source = 'generated'`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var p sql.NullString
		if rows.Scan(&p) == nil {
			known[filepath.Clean(p.String)] = true
		}
	}
	rows.Close()
	return filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(s.CacheDir, p)
		if err == nil && !known[rel] {
			os.Remove(p)
		}
		return nil
	})
}

func or(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
