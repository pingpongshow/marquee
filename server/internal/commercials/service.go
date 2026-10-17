package commercials

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
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"marquee/internal/mediatrash"
	"marquee/internal/settings"
)

// Statuses of a recording.
const (
	StatusPending  = "pending"  // not looked at yet (the nightly task will get to it)
	StatusQueued   = "queued"   // waiting its turn
	StatusRunning  = "running"  // being checked or cut now
	StatusDetected = "detected" // breaks found (maybe none); the file is unchanged
	StatusWaiting  = "waiting"  // to be cut once nobody is watching it
	StatusCut      = "cut"      // the breaks were cut out; the original is in the media trash
	StatusRestored = "restored" // the original was put back
	StatusFailed   = "failed"   // Error says why
)

var (
	// ErrNotRecording is returned for items and files that aren't Live TV recordings.
	ErrNotRecording = errors.New("this isn't a Live TV recording")
	// ErrUnavailable is returned when this build has no Comskip.
	ErrUnavailable = errors.New("commercials can't be found: Comskip isn't installed")
	// ErrBusy is returned when a recording is being watched or recorded.
	ErrBusy = errors.New("the recording is being watched or recorded; try again when it isn't")
	// ErrCantRestore is returned when there's no original to put back.
	ErrCantRestore = errors.New("the original isn't in the media trash any more")
)

// Status is where a recording stands.
type Status struct {
	FileID     int64
	Status     string
	Breaks     []Break
	RemovedMS  int64
	Error      string
	ScannedAt  *time.Time
	CanRestore bool
}

// Service finds commercials in recordings with Comskip and, when asked, cuts them out. One
// recording at a time, at the lowest CPU and I/O priority.
type Service struct {
	DB *sql.DB
	// Comskip is the program's path; INI its settings file ("" = Comskip's defaults).
	Comskip, INI string
	// FFmpeg and FFprobe cut and check files.
	FFmpeg, FFprobe string
	// Encoders are the video encoders to try for a re-encode, best first (nvenc, qsv,
	// software); nil = software. Preset is the transcoder's speed preset.
	Encoders  func() []string
	Preset    func() string
	QSVDevice string
	// Dir holds recordings: only files inside it are looked at. Work files go in Dir/.commercials.
	Dir string
	// Roots, when set, are more folders holding recordings (the recordings folder chosen in
	// the settings, and earlier ones whose recordings are still in the library).
	Roots func() []string
	// Mode is the admin's setting: settings.CommercialsOff, ...Detect or ...Remove.
	Mode func() string
	// Trash takes the originals of cut recordings.
	Trash *mediatrash.Service
	// Rescan rescans a library after a file was replaced.
	Rescan func(libraryID int64)
	// Playing reports whether someone is watching a file.
	Playing func(fileID int64) bool
	// Pace is the pause between recordings in the nightly task; RetryBusy how long a cut
	// waits for a recording that's being watched; WaitForFile how long a finished recording
	// may take to appear in the library.
	Pace, RetryBusy, WaitForFile time.Duration
	// LookPath finds nice and ionice (tests); nil = exec.LookPath.
	LookPath func(string) (string, error)
	// Now is the clock (tests).
	Now func() time.Time

	work    sync.Mutex // one recording at a time, whoever starts it
	mu      sync.Mutex
	queue   []job
	wake    chan struct{}
	waiting sync.WaitGroup // recordings waiting to appear in the library (tests)

	done, total atomic.Int64
}

type job struct {
	fileID int64
	cut    bool
}

// Available reports whether Comskip is installed.
func (s *Service) Available() bool {
	if s == nil || s.Comskip == "" {
		return false
	}
	p, err := exec.LookPath(s.Comskip)
	if err != nil {
		return false
	}
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

func (s *Service) mode() string {
	if s.Mode == nil {
		return settings.CommercialsOff
	}
	return s.Mode()
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) lookPath(name string) (string, error) {
	if s.LookPath != nil {
		return s.LookPath(name)
	}
	return exec.LookPath(name)
}

// Command is how Comskip is started for path, writing its cut lists into outDir: under
// nice -n 19 and ionice -c3 (idle I/O) when those exist.
func (s *Service) Command(path, outDir string) (name string, args []string) {
	cs := []string{}
	if s.INI != "" {
		if _, err := os.Stat(s.INI); err == nil {
			cs = append(cs, "--ini="+s.INI)
		}
	}
	cs = append(cs, "--quiet", "--output="+outDir, "--output-filename=cut", path)
	return s.lowPriority(s.Comskip, cs...)
}

// prefixes are the recordings folders, each ending in a separator.
func (s *Service) prefixes() []string {
	dirs := []string{s.Dir}
	if s.Roots != nil {
		dirs = append(dirs, s.Roots()...)
	}
	var out []string
	for _, d := range dirs {
		if d == "" {
			continue
		}
		p := strings.TrimRight(d, string(filepath.Separator)) + string(filepath.Separator)
		if !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	return out
}

func (s *Service) isRecording(path string) bool {
	for _, p := range s.prefixes() {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

// rootOf is the recordings folder holding path (work files go there, on its filesystem).
func (s *Service) rootOf(path string) string {
	for _, p := range s.prefixes() {
		if strings.HasPrefix(path, p) {
			return filepath.Clean(p)
		}
	}
	return s.Dir
}

// inside is the SQL test for a file inside a recordings folder, and its arguments.
func (s *Service) inside() (string, []any) {
	ps := s.prefixes()
	if len(ps) == 0 {
		return "0", nil
	}
	conds := make([]string, len(ps))
	args := make([]any, 0, 2*len(ps))
	for i, p := range ps {
		conds[i] = `substr(f.path, 1, length(?)) = ?`
		args = append(args, p, p)
	}
	return "(" + strings.Join(conds, " OR ") + ")", args
}

type file struct {
	id, mtime, durationMS, libID int64
	path                         string
}

func (s *Service) file(ctx context.Context, fileID int64) (file, error) {
	cond, args := s.inside()
	var f file
	var dur sql.NullInt64
	err := s.DB.QueryRowContext(ctx, `SELECT f.id, f.path, f.mtime, f.duration_ms, f.library_id FROM media_files f WHERE f.id = ? AND `+cond,
		append([]any{fileID}, args...)...).Scan(&f.id, &f.path, &f.mtime, &dur, &f.libID)
	if errors.Is(err, sql.ErrNoRows) {
		return f, ErrNotRecording
	}
	f.durationMS = dur.Int64
	return f, err
}

// FileForItem is the recording file of a library item (its first part).
func (s *Service) FileForItem(ctx context.Context, itemID int64) (int64, error) {
	cond, args := s.inside()
	var id int64
	err := s.DB.QueryRowContext(ctx, `SELECT f.id FROM media_versions v JOIN media_files f ON f.version_id = v.id AND f.part_index = 0
		WHERE v.item_id = ? AND `+cond+` ORDER BY f.available DESC, v.id LIMIT 1`, append([]any{itemID}, args...)...).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotRecording
	}
	return id, err
}

// fileByPath waits (up to WaitForFile) for a finished recording to be added to the library.
func (s *Service) fileByPath(ctx context.Context, path string) (int64, error) {
	wait := s.WaitForFile
	if wait == 0 {
		wait = 15 * time.Minute
	}
	deadline := time.Now().Add(wait)
	for {
		var id int64
		err := s.DB.QueryRowContext(ctx, `SELECT id FROM media_files WHERE path = ?`, path).Scan(&id)
		if err == nil {
			return id, nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return 0, err
		}
		if time.Now().After(deadline) {
			return 0, fmt.Errorf("%s isn't in the library yet", filepath.Base(path))
		}
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(min(wait/10, 10*time.Second)):
		}
	}
}

// Status reports where a recording file stands.
func (s *Service) Status(ctx context.Context, fileID int64) (Status, error) {
	f, err := s.file(ctx, fileID)
	if err != nil {
		return Status{}, err
	}
	st := Status{FileID: fileID, Status: StatusPending, Breaks: []Break{}}
	var mtime int64
	var scanned sql.NullString
	var original string
	err = s.DB.QueryRowContext(ctx, `SELECT mtime, status, error, removed_ms, original, scanned_at FROM commercial_scans WHERE file_id = ?`, fileID).
		Scan(&mtime, &st.Status, &st.Error, &st.RemovedMS, &original, &scanned)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return st, nil
	case err != nil:
		return st, err
	}
	if t, err := time.Parse(time.RFC3339Nano, scanned.String); err == nil {
		st.ScannedAt = &t
	}
	if mtime != f.mtime && (st.Status == StatusDetected || st.Status == StatusFailed || st.Status == StatusWaiting) {
		// The file changed since: the old result doesn't apply.
		return Status{FileID: fileID, Status: StatusPending, Breaks: []Break{}}, nil
	}
	if st.Status == StatusCut && original != "" {
		_, err := os.Stat(original)
		st.CanRestore = err == nil
	}
	st.Breaks, err = s.breaks(ctx, fileID)
	return st, err
}

// StatusByPath reports on the recording file at path (ErrNotRecording when the library
// doesn't have it).
func (s *Service) StatusByPath(ctx context.Context, path string) (Status, error) {
	var id int64
	err := s.DB.QueryRowContext(ctx, `SELECT id FROM media_files WHERE path = ?`, path).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return Status{}, ErrNotRecording
	}
	if err != nil {
		return Status{}, err
	}
	return s.Status(ctx, id)
}

func (s *Service) breaks(ctx context.Context, fileID int64) ([]Break, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT start_ms, end_ms FROM commercial_breaks WHERE file_id = ? ORDER BY start_ms`, fileID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Break{}
	for rows.Next() {
		var b Break
		if err := rows.Scan(&b.StartMS, &b.EndMS); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

func (s *Service) stamp() string { return s.now().UTC().Format(time.RFC3339Nano) }

// setStatus records a status, keeping what was found.
func (s *Service) setStatus(ctx context.Context, f file, status, errMsg string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO commercial_scans (file_id, mtime, status, error) VALUES (?, ?, ?, ?)
		ON CONFLICT(file_id) DO UPDATE SET status = excluded.status, error = excluded.error`, f.id, f.mtime, status, errMsg)
	return err
}

// Queue queues a recording file: found, and cut when cut is set. It returns the new status.
func (s *Service) Queue(ctx context.Context, fileID int64, cut bool) (Status, error) {
	if !s.Available() {
		return Status{}, ErrUnavailable
	}
	f, err := s.file(ctx, fileID)
	if err != nil {
		return Status{}, err
	}
	// Cut already: never twice (the cut file would replace the original in the media trash).
	var cur string
	if err := s.DB.QueryRowContext(ctx, `SELECT status FROM commercial_scans WHERE file_id = ?`, f.id).Scan(&cur); err != nil && !errors.Is(err, sql.ErrNoRows) {
		return Status{}, err
	}
	if cur == StatusCut {
		return s.Status(ctx, fileID)
	}
	s.mu.Lock()
	found := false
	for i, j := range s.queue {
		if j.fileID == fileID {
			found = true
			s.queue[i].cut = j.cut || cut
		}
	}
	if !found {
		s.queue = append(s.queue, job{fileID: fileID, cut: cut})
	}
	s.mu.Unlock()
	if _, err := s.DB.ExecContext(ctx, `INSERT INTO commercial_scans (file_id, mtime, status) VALUES (?, ?, 'queued')
		ON CONFLICT(file_id) DO UPDATE SET status = 'queued', error = '' WHERE status NOT IN ('running', 'cut')`, f.id, f.mtime); err != nil {
		return Status{}, err
	}
	s.kick()
	return s.Status(ctx, fileID)
}

// RecordingFinished queues a recording that just finished, once the library has it.
func (s *Service) RecordingFinished(ctx context.Context, path string) {
	mode := s.mode()
	if mode == settings.CommercialsOff || !s.Available() || !s.isRecording(path) {
		return
	}
	s.waiting.Add(1)
	go func() {
		defer s.waiting.Done()
		id, err := s.fileByPath(ctx, path)
		if err != nil {
			if ctx.Err() == nil {
				slog.Warn("commercials: recording not in the library; the nightly task will look at it", "path", path, "err", err)
			}
			return
		}
		if _, err := s.Queue(ctx, id, mode == settings.CommercialsRemove); err != nil && ctx.Err() == nil {
			slog.Warn("commercials: queue", "path", path, "err", err)
		}
	}()
}

func (s *Service) kick() {
	s.mu.Lock()
	if s.wake == nil {
		s.wake = make(chan struct{}, 1)
	}
	w := s.wake
	s.mu.Unlock()
	select {
	case w <- struct{}{}:
	default:
	}
}

// Run works through queued recordings until ctx ends.
func (s *Service) Run(ctx context.Context) {
	s.mu.Lock()
	if s.wake == nil {
		s.wake = make(chan struct{}, 1)
	}
	w := s.wake
	s.mu.Unlock()
	// Left queued, running or waiting when the server stopped: queue them again.
	if rows, err := s.DB.QueryContext(ctx, `SELECT file_id, status FROM commercial_scans WHERE status IN ('queued', 'running', 'waiting')`); err == nil {
		var jobs []job
		for rows.Next() {
			var j job
			var status string
			if rows.Scan(&j.fileID, &status) == nil {
				j.cut = status == StatusWaiting || s.mode() == settings.CommercialsRemove
				jobs = append(jobs, j)
			}
		}
		rows.Close()
		for _, j := range jobs {
			s.DB.ExecContext(ctx, `UPDATE commercial_scans SET status = 'queued' WHERE file_id = ? AND status = 'running'`, j.fileID)
			s.mu.Lock()
			s.queue = append(s.queue, j)
			s.mu.Unlock()
		}
	}
	for {
		for s.next(ctx) {
		}
		select {
		case <-ctx.Done():
			return
		case <-w:
		}
	}
}

// next handles the first queued recording; false when there's none.
func (s *Service) next(ctx context.Context) bool {
	if ctx.Err() != nil {
		return false
	}
	s.mu.Lock()
	if len(s.queue) == 0 {
		s.mu.Unlock()
		return false
	}
	j := s.queue[0]
	s.queue = s.queue[1:]
	s.mu.Unlock()
	f, err := s.file(ctx, j.fileID)
	if err != nil {
		if errors.Is(err, ErrNotRecording) {
			s.DB.ExecContext(ctx, `DELETE FROM commercial_scans WHERE file_id = ?`, j.fileID)
		}
		return true
	}
	if _, err := s.process(ctx, f, j.cut); errors.Is(err, ErrBusy) {
		retry := s.RetryBusy
		if retry == 0 {
			retry = 10 * time.Minute
		}
		time.AfterFunc(retry, func() {
			if ctx.Err() == nil {
				s.Queue(ctx, j.fileID, true)
			}
		})
	}
	return true
}

// busy reports whether someone is watching the file or it's still being recorded.
func (s *Service) busy(ctx context.Context, f file) bool {
	if s.Playing != nil && s.Playing(f.id) {
		return true
	}
	var n int
	s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM dvr_recordings WHERE status = 'recording' AND path = ?`, f.path).Scan(&n)
	return n > 0
}

// process finds a recording's commercials (unless that's been done for the file as it is)
// and, when cut is set, cuts them out. It returns how many breaks there are.
func (s *Service) process(ctx context.Context, f file, cut bool) (int, error) {
	s.work.Lock()
	defer s.work.Unlock()
	if f2, err := s.file(ctx, f.id); err == nil {
		f = f2 // it may have changed while it waited
	}
	var status string
	var mtime, found int64
	err := s.DB.QueryRowContext(ctx, `SELECT status, mtime, breaks FROM commercial_scans WHERE file_id = ?`, f.id).Scan(&status, &mtime, &found)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	if status == StatusCut {
		// Cut already: never twice.
		s.setStatus(context.WithoutCancel(ctx), f, StatusCut, "")
		return int(found), nil
	}
	if err := s.setStatus(ctx, f, StatusRunning, ""); err != nil {
		return 0, err
	}
	wctx := context.WithoutCancel(ctx)
	// Detection holds for the file as it is: earlier findings are kept for a cut.
	known := mtime == f.mtime && (status == StatusDetected || status == StatusWaiting || (status == StatusFailed && found > 0))
	var breaks []Break
	if known {
		breaks, err = s.breaks(ctx, f.id)
	}
	if !known || err != nil {
		start := time.Now()
		breaks, err = s.detect(ctx, f)
		if ctx.Err() != nil {
			s.DB.ExecContext(wctx, `UPDATE commercial_scans SET status = 'queued' WHERE file_id = ? AND status = 'running'`, f.id)
			return 0, ctx.Err()
		}
		if err != nil {
			slog.Warn("commercials: detection failed", "path", f.path, "err", err)
			s.DB.ExecContext(wctx, `DELETE FROM commercial_breaks WHERE file_id = ?`, f.id)
			s.DB.ExecContext(wctx, `UPDATE commercial_scans SET mtime = ?, status = 'failed', error = ?, breaks = 0, removed_ms = 0, scanned_at = ? WHERE file_id = ?`,
				f.mtime, err.Error(), s.stamp(), f.id)
			return 0, err
		}
		if err := s.saveDetected(wctx, f, breaks); err != nil {
			return 0, err
		}
		slog.Info("commercials: detected", "path", f.path, "breaks", len(breaks), "took", time.Since(start).Round(time.Second))
	} else {
		s.setStatus(wctx, f, StatusDetected, "")
	}
	if !cut || len(breaks) == 0 {
		return len(breaks), nil
	}
	if s.busy(ctx, f) {
		s.setStatus(wctx, f, StatusWaiting, "")
		return len(breaks), ErrBusy
	}
	start := time.Now()
	removed, err := s.cut(ctx, f, breaks)
	if ctx.Err() != nil {
		s.setStatus(wctx, f, StatusQueued, "")
		return len(breaks), ctx.Err()
	}
	switch {
	case errors.Is(err, ErrBusy):
		s.setStatus(wctx, f, StatusWaiting, "")
		return len(breaks), err
	case err != nil:
		slog.Warn("commercials: cutting failed; the recording is unchanged", "path", f.path, "err", err)
		s.DB.ExecContext(wctx, `UPDATE commercial_scans SET status = 'failed', error = ?, scanned_at = ? WHERE file_id = ?`,
			"Couldn't remove the commercials (the recording is unchanged): "+err.Error(), s.stamp(), f.id)
		return len(breaks), err
	}
	slog.Info("commercials: removed", "path", f.path, "breaks", len(breaks), "removed", clock(removed), "took", time.Since(start).Round(time.Second))
	return len(breaks), nil
}

func (s *Service) saveDetected(ctx context.Context, f file, breaks []Break) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := putBreaks(ctx, tx, f.id, breaks); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO commercial_scans (file_id, mtime, status, error, breaks, removed_ms, original, scanned_at)
		VALUES (?, ?, 'detected', '', ?, 0, '', ?)
		ON CONFLICT(file_id) DO UPDATE SET mtime = excluded.mtime, status = 'detected', error = '', breaks = excluded.breaks,
			removed_ms = 0, original = '', scanned_at = excluded.scanned_at`,
		f.id, f.mtime, len(breaks), s.stamp()); err != nil {
		return err
	}
	return tx.Commit()
}

func putBreaks(ctx context.Context, tx *sql.Tx, fileID int64, breaks []Break) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM commercial_breaks WHERE file_id = ?`, fileID); err != nil {
		return err
	}
	for _, b := range breaks {
		if _, err := tx.ExecContext(ctx, `INSERT INTO commercial_breaks (file_id, start_ms, end_ms) VALUES (?, ?, ?)`, fileID, b.StartMS, b.EndMS); err != nil {
			return err
		}
	}
	return nil
}

// detect runs Comskip and reads its cut list.
func (s *Service) detect(ctx context.Context, f file) ([]Break, error) {
	if !s.Available() {
		return nil, ErrUnavailable
	}
	if _, err := os.Stat(f.path); err != nil {
		return nil, fmt.Errorf("the file can't be read: %w", err)
	}
	out, err := os.MkdirTemp("", "marquee-comskip-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(out)
	// Generous: software decoding at idle priority can be slower than real time on a busy server.
	limit := max(time.Hour, 3*time.Duration(f.durationMS)*time.Millisecond)
	cctx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()
	name, args := s.Command(f.path, out)
	cmd := exec.CommandContext(cctx, name, args...)
	cmd.WaitDelay = 5 * time.Second
	var stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stderr, &stderr
	runErr := cmd.Run()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if cctx.Err() != nil {
		return nil, fmt.Errorf("Comskip took longer than %s", limit.Round(time.Minute))
	}
	// Comskip exits 1 when it finds no commercials (still writing its cut lists); anything
	// else, or no cut list at all, is a failure.
	var exitErr *exec.ExitError
	if runErr != nil && !(errors.As(runErr, &exitErr) && exitErr.ExitCode() == 1) {
		return nil, fmt.Errorf("Comskip failed: %v%s", runErr, tail(stderr.String()))
	}
	var raw []Break
	if b, err := os.ReadFile(filepath.Join(out, "cut.edl")); err == nil {
		if raw, err = ParseEDL(bytes.NewReader(b)); err != nil {
			return nil, err
		}
	} else if b, err := os.ReadFile(filepath.Join(out, "cut.txt")); err == nil {
		if raw, err = ParseTXT(bytes.NewReader(b)); err != nil {
			return nil, err
		}
	} else {
		if runErr != nil {
			return nil, fmt.Errorf("Comskip failed: %v%s", runErr, tail(stderr.String()))
		}
		return nil, errors.New("Comskip didn't write a cut list" + tail(stderr.String()))
	}
	return Normalize(raw, f.durationMS), nil
}

// cut makes the recording without its breaks, checks it, and puts it in place of the
// original, which goes to the media trash. Any failure before the swap leaves the
// recording as it was. It returns the time removed.
func (s *Service) cut(ctx context.Context, f file, breaks []Break) (int64, error) {
	if s.Trash == nil {
		return 0, errors.New("the media trash isn't available")
	}
	pi, err := s.probe(ctx, f.path)
	if err != nil {
		return 0, err
	}
	if pi.Video == 0 || pi.DurationMS <= 0 {
		return 0, errors.New("the recording has no video or no known length")
	}
	kfs, err := s.keyframes(ctx, f.path, pi.StartMS, pi.DurationMS)
	if err != nil {
		return 0, err
	}
	plan := MakePlan(breaks, kfs, pi.DurationMS)
	if len(plan.Removed) == 0 {
		return 0, errors.New("nothing to cut")
	}
	work := filepath.Join(s.rootOf(f.path), ".commercials", strconv.FormatInt(f.id, 10))
	if err := os.MkdirAll(work, 0o755); err != nil {
		return 0, err
	}
	defer os.RemoveAll(work)
	out := filepath.Join(work, "cut"+filepath.Ext(f.path))
	err = errors.New("stream copy can't cut it cleanly")
	if plan.Copy {
		if err = s.cutCopy(ctx, f.path, out, work, pi, plan); err == nil {
			err = s.verify(ctx, out, pi, plan)
		}
		if err != nil && ctx.Err() == nil {
			slog.Info("commercials: stream copy didn't work; re-encoding", "path", f.path, "err", err)
		}
	}
	if err != nil {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		plan = exactPlan(breaks, pi.DurationMS)
		os.Remove(out)
		if err = s.cutReencode(ctx, f.path, out, work, pi, plan); err == nil {
			err = s.verify(ctx, out, pi, plan)
		}
		if err != nil {
			return 0, err
		}
	}
	if s.busy(ctx, f) {
		return 0, ErrBusy
	}
	return plan.RemovedMS(), s.swap(ctx, f, out, plan)
}

// swap puts the cut file in the original's place under the same name, so the library item
// keeps its id, watch state and metadata (as a VOD upgrade does): the original moves to the
// media trash, the file record is marked for the scan to read again, watch positions move
// with the cuts, and markers found in the original are dropped (they'd be in the wrong place).
func (s *Service) swap(ctx context.Context, f file, cut string, plan Plan) error {
	original, undo, err := s.Trash.Stash(ctx, f.id, "commercials removed")
	if err != nil {
		return fmt.Errorf("couldn't move the original to the media trash: %w", err)
	}
	if err := os.Rename(cut, f.path); err != nil {
		undo()
		return fmt.Errorf("couldn't put the cut recording in place: %w", err)
	}
	var mtime int64
	if fi, err := os.Stat(f.path); err == nil {
		mtime = fi.ModTime().Unix()
	}
	wctx := context.WithoutCancel(ctx)
	if err := s.replaced(wctx, f, plan.KeptMS(), func(ms int64) int64 { return CutPosition(ms, plan.Removed) }, func(tx *sql.Tx) error {
		if err := putBreaks(wctx, tx, f.id, plan.Removed); err != nil {
			return err
		}
		_, err := tx.ExecContext(wctx, `UPDATE commercial_scans SET mtime = ?, status = 'cut', error = '', breaks = ?, removed_ms = ?, original = ?, scanned_at = ?
			WHERE file_id = ?`, mtime, len(plan.Removed), plan.RemovedMS(), original, s.stamp(), f.id)
		return err
	}); err != nil {
		// The files are in place; the scan sorts the record out.
		slog.Warn("commercials: couldn't update the library after cutting", "path", f.path, "err", err)
	}
	return nil
}

// replaced records that the file at f.path was replaced: the record is read again by the
// next scan (size and mtime reset, as a VOD upgrade does), watch positions on its item are
// moved with move, detected markers are dropped, and update adds the caller's changes.
func (s *Service) replaced(ctx context.Context, f file, durationMS int64, move func(int64) int64, update func(*sql.Tx) error) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `UPDATE media_files SET size = -1, mtime = 0, duration_ms = ? WHERE id = ?`, durationMS, f.id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM markers WHERE file_id = ? AND source = 'detected'`, f.id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM marker_scans WHERE file_id = ?`, f.id); err != nil {
		return err
	}
	rows, err := tx.QueryContext(ctx, `SELECT u.user_id, u.item_id, u.view_offset_ms FROM user_item_state u
		JOIN media_versions v ON v.item_id = u.item_id JOIN media_files mf ON mf.version_id = v.id
		WHERE mf.id = ? AND u.view_offset_ms > 0`, f.id)
	if err != nil {
		return err
	}
	type pos struct{ user, item, ms int64 }
	var ps []pos
	for rows.Next() {
		var p pos
		if rows.Scan(&p.user, &p.item, &p.ms) == nil {
			ps = append(ps, p)
		}
	}
	rows.Close()
	for _, p := range ps {
		if _, err := tx.ExecContext(ctx, `UPDATE user_item_state SET view_offset_ms = ? WHERE user_id = ? AND item_id = ?`, move(p.ms), p.user, p.item); err != nil {
			return err
		}
	}
	if err := update(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	if s.Rescan != nil {
		s.Rescan(f.libID)
	}
	return nil
}

// Restore puts back the original of a cut recording while it's still in the media trash;
// the cut file goes to the trash instead.
func (s *Service) Restore(ctx context.Context, fileID int64) (Status, error) {
	f, err := s.file(ctx, fileID)
	if err != nil {
		return Status{}, err
	}
	s.work.Lock()
	defer s.work.Unlock()
	var status, original string
	err = s.DB.QueryRowContext(ctx, `SELECT status, original FROM commercial_scans WHERE file_id = ?`, f.id).Scan(&status, &original)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && (status != StatusCut || original == "")) {
		return Status{}, ErrCantRestore
	}
	if err != nil {
		return Status{}, err
	}
	if _, err := os.Stat(original); err != nil {
		return Status{}, ErrCantRestore
	}
	if s.busy(ctx, f) {
		return Status{}, ErrBusy
	}
	if s.Trash == nil {
		return Status{}, errors.New("the media trash isn't available")
	}
	removed, err := s.breaks(ctx, f.id)
	if err != nil {
		return Status{}, err
	}
	_, undo, err := s.Trash.Stash(ctx, f.id, "original restored")
	if err != nil {
		return Status{}, fmt.Errorf("couldn't move the cut recording to the media trash: %w", err)
	}
	if err := os.Rename(original, f.path); err != nil {
		undo()
		return Status{}, fmt.Errorf("couldn't put the original back: %w", err)
	}
	var mtime int64
	if fi, err := os.Stat(f.path); err == nil {
		mtime = fi.ModTime().Unix()
	}
	var durationMS int64
	if pi, err := s.probe(ctx, f.path); err == nil {
		durationMS = pi.DurationMS
	}
	wctx := context.WithoutCancel(ctx)
	if err := s.replaced(wctx, f, durationMS, func(ms int64) int64 { return OriginalPosition(ms, removed) }, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(wctx, `UPDATE commercial_scans SET mtime = ?, status = 'restored', error = '', removed_ms = 0, original = '', scanned_at = ?
			WHERE file_id = ?`, mtime, s.stamp(), f.id)
		return err
	}); err != nil {
		slog.Warn("commercials: couldn't update the library after restoring", "path", f.path, "err", err)
	}
	slog.Info("commercials: original restored", "path", f.path)
	return s.Status(ctx, fileID)
}

func tail(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		s = s[i+1:]
	}
	if r := []rune(s); len(r) > 200 {
		s = string(r[:200])
	}
	return ": " + s
}

// Progress reports the nightly task's progress.
func (s *Service) Progress() (done, total int) { return int(s.done.Load()), int(s.total.Load()) }

// pending lists recording files the setting says to handle, newest first: never looked at,
// changed since, and with remove on, ones found but not cut yet. Failures aren't retried
// by themselves (an admin can ask again), and cut or restored recordings are left alone.
func (s *Service) pending(ctx context.Context, cut bool) ([]file, error) {
	cond, cargs := s.inside()
	q := `SELECT f.id, f.path, f.mtime, COALESCE(f.duration_ms, 0), f.library_id FROM media_files f
		LEFT JOIN commercial_scans c ON c.file_id = f.id
		WHERE f.available = 1 AND f.duration_ms > 0 AND ` + cond + `
		  AND (c.file_id IS NULL OR (c.mtime <> f.mtime AND c.status IN ('detected', 'failed', 'waiting'))`
	if cut {
		q += ` OR (c.mtime = f.mtime AND c.status IN ('detected', 'waiting') AND c.breaks > 0)`
	}
	q += `) ORDER BY f.id DESC`
	rows, err := s.DB.QueryContext(ctx, q, cargs...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []file
	for rows.Next() {
		var f file
		if err := rows.Scan(&f.id, &f.path, &f.mtime, &f.durationMS, &f.libID); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// Backlog (a nightly task) handles recordings the setting covers that haven't been, one at
// a time, pausing between them. It stops when ctx ends and continues next time.
func (s *Service) Backlog(ctx context.Context) (string, error) {
	mode := s.mode()
	if mode == settings.CommercialsOff {
		return "Turned off", nil
	}
	if !s.Available() {
		return "Comskip isn't installed", nil
	}
	cut := mode == settings.CommercialsRemove
	list, err := s.pending(ctx, cut)
	if err != nil {
		return "", err
	}
	s.done.Store(0)
	s.total.Store(int64(len(list)))
	var found, failed, busy int
	for i, f := range list {
		if ctx.Err() != nil {
			break
		}
		if i > 0 && s.Pace > 0 {
			select {
			case <-ctx.Done():
			case <-time.After(s.Pace):
			}
			if ctx.Err() != nil {
				break
			}
		}
		n, err := s.process(ctx, f, cut)
		if ctx.Err() != nil {
			break
		}
		switch {
		case errors.Is(err, ErrBusy):
			busy++
		case err != nil:
			failed++
		}
		found += n
		s.done.Add(1)
	}
	verb := "found"
	if cut {
		verb = "found and removed"
	}
	msg := fmt.Sprintf("Checked %d recordings: %s %d commercial breaks", s.done.Load(), verb, found)
	if failed > 0 {
		msg += fmt.Sprintf("; %d failed", failed)
	}
	if busy > 0 {
		msg += fmt.Sprintf("; %d being watched, left for next time", busy)
	}
	if err := ctx.Err(); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return msg + fmt.Sprintf("; %d left for next time", int64(len(list))-s.done.Load()), nil
		}
		return "", err
	}
	return msg, nil
}
