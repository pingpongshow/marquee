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

// Workers is how many repairs run at once. Each decodes a whole film, which keeps about a
// quarter of a 24-thread CPU busy (at low priority), so two leave room for transcodes.
const Workers = 2

// Service works through the repairs asked for in Library Health, Workers at a time: it works out
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
	running  map[int64]float64 // file → progress, of the repairs running now
}

func (s *Service) wakeCh() chan struct{} {
	s.wakeOnce.Do(func() { s.wake = make(chan struct{}, Workers) })
	return s.wake
}

// Wake starts the next repairs now rather than at the next minute.
func (s *Service) Wake() {
	for range Workers {
		select {
		case s.wakeCh() <- struct{}{}:
		default:
		}
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

// Live is this file's repair progress, when it's running.
func (s *Service) Live(fileID int64) (float64, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p, ok := s.running[fileID]
	return p, ok
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

// Run works through the queue until ctx ends, Workers repairs at a time: a repair
// interrupted by a restart starts again, and leftovers of one are removed.
func (s *Service) Run(ctx context.Context) {
	s.DB.ExecContext(ctx, `UPDATE timing_repairs SET state = 'queued', progress = 0 WHERE state = 'running'`)
	var wg sync.WaitGroup
	for range Workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.work(ctx)
		}()
	}
	wg.Wait()
}

func (s *Service) work(ctx context.Context) {
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

// next repairs the first queued file that isn't playing or being repaired by another
// worker; false when there was none.
func (s *Service) next(ctx context.Context) bool {
	if ctx.Err() != nil {
		return false
	}
	id := s.claim(ctx)
	if id == 0 {
		return false
	}
	s.repairOne(ctx, id)
	return true
}

// claim takes the next file for a worker: marked running, so no other worker takes it.
func (s *Service) claim(ctx context.Context) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range s.queued(ctx) {
		if _, taken := s.running[id]; taken || (s.Playing != nil && s.Playing(id)) {
			continue
		}
		if s.running == nil {
			s.running = map[int64]float64{}
		}
		s.running[id] = 0
		s.DB.ExecContext(ctx, `UPDATE timing_repairs SET state = 'running', progress = 0, error = '' WHERE file_id = ?`, id)
		return id
	}
	return 0
}

// queued are the files waiting for a repair, in the order they were asked for and then as
// Library Health lists them (by title), so the queue works down the list.
func (s *Service) queued(ctx context.Context) []int64 {
	rows, err := s.DB.QueryContext(ctx, `SELECT r.file_id FROM timing_repairs r
		LEFT JOIN media_files f ON f.id = r.file_id LEFT JOIN media_versions v ON v.id = f.version_id
		LEFT JOIN items i ON i.id = v.item_id
		WHERE r.state = 'queued' ORDER BY r.created_at, i.sort_title COLLATE NOCASE, i.id, r.file_id`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	return ids
}

func (s *Service) repairOne(ctx context.Context, id int64) {
	defer func() {
		s.mu.Lock()
		delete(s.running, id)
		s.mu.Unlock()
	}()
	err := s.Repair(ctx, id, func(p float64) {
		s.mu.Lock()
		s.running[id] = p
		s.mu.Unlock()
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
