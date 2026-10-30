package frametiming

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"time"

	"marquee/internal/encode"
	"marquee/internal/mediatrash"
)

// PartSuffix is added to a file's name for its repaired copy while it's made (the scanner
// skips it: not a video extension).
const PartSuffix = ".repairing.part"

// Repair states (timing_repairs.state).
const (
	StateQueued  = "queued"
	StateRunning = "running"
	StateFailed  = "failed"
)

// ErrNotRepairable is a file that isn't waiting for a repair: no frame timing error, or not
// an MP4 or MOV file.
var ErrNotRepairable = errors.New("the file has no frame timing error that can be repaired")

// Service works through the repairs asked for in Library Health, one at a time: it works out
// the frame order (Order), writes a repaired copy beside the file (Rewrite), checks it, and
// puts it in the file's place, the original going to the media trash. The library keeps its
// record (the item keeps its id, history and metadata); the rescan reads the new file, and
// the next Check files confirms its timing.
type Service struct {
	DB      *sql.DB
	FFprobe string
	FFmpeg  string
	// LookPath finds nice and ionice (nil = exec.LookPath).
	LookPath func(string) (string, error)
	Trash    *mediatrash.Service
	// Rescan reads a library again (the repaired file's size and probe).
	Rescan func(libraryID int64)
	// Playing reports whether a file is playing: it isn't swapped then (tried again later).
	Playing func(fileID int64) bool

	wakeOnce sync.Once
	wake     chan struct{}
	mu       sync.Mutex
	running  int64
	progress float64
}

func (s *Service) wakeCh() chan struct{} {
	s.wakeOnce.Do(func() { s.wake = make(chan struct{}, 1) })
	return s.wake
}

// Wake starts the next repair now rather than at the next minute.
func (s *Service) Wake() {
	select {
	case s.wakeCh() <- struct{}{}:
	default:
	}
}

func stamp() string { return time.Now().UTC().Format("2006-01-02T15:04:05.000Z") }

// repairable are the files with a frame timing error that Rewrite can repair.
const repairable = `f.timing_error IS NOT NULL AND f.available = 1
	AND (LOWER(COALESCE(f.container, '')) LIKE '%mp4%' OR LOWER(COALESCE(f.container, '')) LIKE '%mov%'
		OR LOWER(f.path) LIKE '%.mp4' OR LOWER(f.path) LIKE '%.m4v' OR LOWER(f.path) LIKE '%.mov')`

// Queue asks for these files to be repaired (all of them when ids is nil): each one with a
// frame timing error that can be repaired, not already waiting. A failed repair is tried
// again. It returns how many were queued.
func (s *Service) Queue(ctx context.Context, ids []int64) (int, error) {
	q := `INSERT INTO timing_repairs (file_id, state, created_at)
		SELECT f.id, 'queued', ? FROM media_files f WHERE ` + repairable
	args := []any{stamp()}
	if ids != nil {
		if len(ids) == 0 {
			return 0, nil
		}
		q += ` AND f.id IN (?` + strings.Repeat(",?", len(ids)-1) + `)`
		for _, id := range ids {
			args = append(args, id)
		}
	}
	q += ` ON CONFLICT (file_id) DO UPDATE SET state = 'queued', progress = 0, error = '', created_at = excluded.created_at, finished_at = NULL
		WHERE timing_repairs.state = 'failed'`
	res, err := s.DB.ExecContext(ctx, q, args...)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	if ids != nil && n == 0 {
		var waiting int
		s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM timing_repairs WHERE file_id IN (?`+strings.Repeat(",?", len(ids)-1)+`)`, args[1:]...).Scan(&waiting)
		if waiting == 0 {
			return 0, ErrNotRepairable
		}
	}
	if n > 0 {
		s.Wake()
	}
	return int(n), nil
}

// Cancel takes a waiting or failed repair off the list (a running one finishes).
func (s *Service) Cancel(ctx context.Context, fileID int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM timing_repairs WHERE file_id = ? AND state <> 'running'`, fileID)
	return err
}

// State is a file's repair as Library Health shows it.
type State struct {
	State    string
	Progress float64
	Error    string
}

// States are the repairs of these files (files without one are left out).
func (s *Service) States(ctx context.Context, ids []int64) (map[int64]State, error) {
	return States(ctx, s.DB, ids, s.Live)
}

// Live is the running repair's progress, when it's this file's.
func (s *Service) Live(fileID int64) (float64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.progress, s.running == fileID && fileID != 0
}

// States reads the repairs of these files; live (when set) gives a running repair's progress.
func States(ctx context.Context, db *sql.DB, ids []int64, live func(int64) (float64, bool)) (map[int64]State, error) {
	out := map[int64]State{}
	if len(ids) == 0 {
		return out, nil
	}
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	rows, err := db.QueryContext(ctx, `SELECT file_id, state, progress, error FROM timing_repairs WHERE file_id IN (?`+
		strings.Repeat(",?", len(ids)-1)+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var st State
		if err := rows.Scan(&id, &st.State, &st.Progress, &st.Error); err != nil {
			return nil, err
		}
		if live != nil {
			if p, ok := live(id); ok {
				st.Progress = p
			}
		}
		out[id] = st
	}
	return out, rows.Err()
}

// Waiting is how many repairs are queued or running.
func (s *Service) Waiting(ctx context.Context) int {
	var n int
	s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM timing_repairs WHERE state IN ('queued', 'running')`).Scan(&n)
	return n
}

// Run works through the queue until ctx ends: a repair interrupted by a restart starts
// again, and leftovers of one are removed.
func (s *Service) Run(ctx context.Context) {
	s.DB.ExecContext(ctx, `UPDATE timing_repairs SET state = 'queued', progress = 0 WHERE state = 'running'`)
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		for s.next(ctx) {
		}
		select {
		case <-ctx.Done():
			return
		case <-s.wakeCh():
		case <-tick.C:
		}
	}
}

// next repairs the oldest queued file that isn't playing; false when there was none.
func (s *Service) next(ctx context.Context) bool {
	if ctx.Err() != nil {
		return false
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT file_id FROM timing_repairs WHERE state = 'queued' ORDER BY created_at, file_id`)
	if err != nil {
		return false
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		if s.Playing != nil && s.Playing(id) {
			continue
		}
		s.repairOne(ctx, id)
		return true
	}
	return false
}

func (s *Service) setProgress(id int64, p float64) {
	s.mu.Lock()
	s.running, s.progress = id, p
	s.mu.Unlock()
}

func (s *Service) repairOne(ctx context.Context, id int64) {
	s.DB.ExecContext(ctx, `UPDATE timing_repairs SET state = 'running', progress = 0, error = '' WHERE file_id = ?`, id)
	s.setProgress(id, 0)
	defer s.setProgress(0, 0)
	err := s.Repair(ctx, id, func(p float64) {
		s.setProgress(id, p)
	})
	wctx := context.WithoutCancel(ctx)
	switch {
	case err == nil:
		s.DB.ExecContext(wctx, `DELETE FROM timing_repairs WHERE file_id = ?`, id)
	case ctx.Err() != nil, errors.Is(err, errPlaying):
		s.DB.ExecContext(wctx, `UPDATE timing_repairs SET state = 'queued', progress = 0 WHERE file_id = ?`, id)
	default:
		slog.Warn("frame timing repair failed", "file", id, "err", err)
		s.DB.ExecContext(wctx, `UPDATE timing_repairs SET state = 'failed', error = ?, finished_at = ? WHERE file_id = ?`,
			clip(err.Error(), 300), stamp(), id)
	}
}

// Repair repairs one file now: progress hears the fraction done (working out the frame
// order is most of it).
func (s *Service) Repair(ctx context.Context, fileID int64, progress func(float64)) error {
	var path, container string
	var libID, durationMS int64
	var timingErr sql.NullString
	err := s.DB.QueryRowContext(ctx, `SELECT path, COALESCE(container, ''), library_id, COALESCE(duration_ms, 0), timing_error
		FROM media_files WHERE id = ? AND available = 1`, fileID).Scan(&path, &container, &libID, &durationMS, &timingErr)
	if errors.Is(err, sql.ErrNoRows) {
		return errors.New("the file is no longer in the library")
	}
	if err != nil {
		return err
	}
	if !timingErr.Valid {
		return nil // already repaired (or replaced)
	}
	if !Repairable(container, path) {
		return errors.New("only MP4 and MOV files can be repaired")
	}
	if s.Trash == nil {
		return errors.New("the media trash isn't available")
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("the file can't be read: %w", err)
	}
	part := path + PartSuffix
	os.Remove(part) // left by a repair that was interrupted
	order, err := Order(ctx, s.LookPath, s.FFprobe, path, func(p float64) { progress(0.9 * p) })
	if err != nil {
		return fmt.Errorf("couldn't work out the frame order: %w", err)
	}
	if _, err := Rewrite(path, part, order); err != nil {
		os.Remove(part)
		if errors.Is(err, ErrNotMP4) {
			return errors.New("the file's layout isn't one that can be repaired")
		}
		return err
	}
	progress(0.95)
	if err := s.verify(ctx, part, durationMS); err != nil {
		os.Remove(part)
		return fmt.Errorf("the repaired copy failed its check, so the file was left as it was: %w", err)
	}
	if s.Playing != nil && s.Playing(fileID) {
		os.Remove(part)
		return errPlaying
	}
	original, undo, err := s.Trash.Stash(ctx, fileID, "frame timing repaired")
	if err != nil {
		os.Remove(part)
		return fmt.Errorf("couldn't move the original to the media trash: %w", err)
	}
	if err := os.Rename(part, path); err != nil {
		undo()
		os.Remove(part)
		return err
	}
	// Read again by the rescan (a changed file clears its checks, so Check files confirms the
	// timing); until then, nothing to repair.
	wctx := context.WithoutCancel(ctx)
	s.DB.ExecContext(wctx, `UPDATE media_files SET size = -1, mtime = 0, timing_error = NULL WHERE id = ?`, fileID)
	slog.Info("frame timing repaired", "path", path, "original", original)
	if s.Rescan != nil {
		s.Rescan(libID)
	}
	progress(1)
	return nil
}

// errPlaying puts a repair back in the queue: the file started playing while it was made.
var errPlaying = errors.New("the file is playing; tried again later")

// verify checks the repaired copy before it replaces anything: its timing passes Check, it's
// as long as the original (within 1%), and a few seconds at its start, middle and end decode.
func (s *Service) verify(ctx context.Context, path string, durationMS int64) error {
	if reason, err := Check(ctx, s.LookPath, s.FFprobe, path); err != nil {
		return err
	} else if reason != "" {
		return errors.New(reason)
	}
	end := float64(durationMS) / 1000
	for _, at := range []float64{0, end * 0.5, max(0, end-min(30, end/3))} {
		if err := encode.DecodeCheckLow(ctx, s.LookPath, or(s.FFmpeg, "ffmpeg"), path, at, 5, true); err != nil {
			return fmt.Errorf("it doesn't decode at %ds: %v", int(at), err)
		}
	}
	return nil
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
