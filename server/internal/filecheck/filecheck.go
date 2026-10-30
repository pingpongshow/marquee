// Package filecheck checks video files (the Check files task), at low priority and one file
// at a time. Each movie, episode and personal video added to the library (or replaced) gets
// the playback check VOD downloads get: its length must be plausible where it is known, and
// a few seconds at its start and near its end must decode. Files that fail are listed in
// Library Health → Unplayable files with the reason; files that pass keep only
// media_files.checked_at. Files already in the library when that check arrived were marked
// checked by the migration and aren't checked.
//
// Every video file, those already in the library included, also gets its frame timing
// checked once (frametiming.Check): files whose frames are timed in decode order are listed
// in Library Health → Frame timing errors, to be repaired (media_files.timing_checked_at and
// timing_error).
package filecheck

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"marquee/internal/encode"
	"marquee/internal/frametiming"
	"marquee/internal/tasks"
)

// TaskID is the scheduled task's id.
const TaskID = "check-new-files"

// Task is the scheduled task: hourly (new files are also checked straight after a scan
// adds them), at low priority, one file at a time.
func (s *Service) Task() tasks.Task {
	return tasks.Task{ID: TaskID, Name: "Check files", Every: time.Hour,
		Description: "Checks each movie, episode and personal video added to the library (or replaced) for damage: a few seconds at its start and near its end must decode. Damaged files are listed in Library Health → Unplayable files. Also checks every video file's frame timing once, those already in the library included; files whose frames are timed out of order are listed in Library Health → Frame timing errors. Runs after a scan adds files, at low priority, one file at a time.",
		Run:         s.Run, Progress: s.Progress}
}

// Damaged is a file that failed its check.
type Damaged struct {
	FileID, ItemID int64
	Path, Reason   string
}

// Service checks new files.
type Service struct {
	DB      *sql.DB
	FFmpeg  string
	FFprobe string
	// LookPath finds nice and ionice (nil = exec.LookPath).
	LookPath func(string) (string, error)
	// Decode replaces the decode test (tests); nil = encode.DecodeCheckLow.
	Decode func(ctx context.Context, path string, at float64) error
	// Timing replaces the frame timing check (tests); nil = frametiming.Check.
	Timing func(ctx context.Context, path string) (string, error)
	// OnDamaged, when set, hears the files a run found damaged (an admin alert).
	OnDamaged func(ctx context.Context, files []Damaged)

	mu          sync.Mutex
	done, total int
}

// eligible is the condition for a file that is checked: an available movie, episode or
// personal video (not an extra), not in a photo library or a Live TV recordings library,
// and not a DVR recording (the DVR checks its own).
const eligible = `i.type IN ('movie', 'episode', 'video') AND i.extra_type IS NULL
	AND l.type <> 'photos' AND l.recordings = 0
	AND NOT EXISTS (SELECT 1 FROM dvr_recordings r WHERE r.path = f.path AND r.path <> '')`

const now = `strftime('%Y-%m-%dT%H:%M:%fZ', 'now')`

// Pending reports whether any file is waiting for a check (cheap: for triggering the task
// after a scan).
func (s *Service) Pending(ctx context.Context) bool {
	var n int
	s.DB.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM media_files WHERE checked_at IS NULL AND available = 1)
		OR EXISTS (SELECT 1 FROM media_files WHERE timing_checked_at IS NULL AND available = 1)`).Scan(&n)
	return n == 1
}

// Progress is the running check's progress.
func (s *Service) Progress() (done, total int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.done, s.total
}

func (s *Service) setProgress(done, total int) {
	s.mu.Lock()
	s.done, s.total = done, total
	s.mu.Unlock()
}

type pending struct {
	id, itemID, size, mtime, durationMS, expectedMS int64
	path                                            string
	video                                           bool
}

// Run checks every file waiting for its check.
func (s *Service) Run(ctx context.Context) (string, error) {
	defer s.setProgress(0, 0)
	// Files that aren't checked (music, photos, extras, recordings) are marked done, so the
	// waiting list stays short.
	if _, err := s.DB.ExecContext(ctx, `UPDATE media_files SET checked_at = `+now+`, check_error = NULL
		WHERE checked_at IS NULL AND id NOT IN (SELECT f.id FROM media_files f
			CROSS JOIN media_versions v ON v.id = f.version_id CROSS JOIN items i ON i.id = v.item_id
			CROSS JOIN libraries l ON l.id = i.library_id
			WHERE f.checked_at IS NULL AND `+eligible+`)`); err != nil {
		return "", err
	}
	if err := s.skipTiming(ctx); err != nil {
		return "", err
	}
	var total, timingTotal int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM media_files WHERE checked_at IS NULL AND available = 1`).Scan(&total); err != nil {
		return "", err
	}
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM media_files WHERE timing_checked_at IS NULL AND available = 1`).Scan(&timingTotal); err != nil {
		return "", err
	}
	var checked, skipped int
	var damaged []Damaged
	var last int64
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		s.setProgress(checked+skipped+len(damaged), max(total, checked+skipped+len(damaged)+1)+timingTotal)
		p, err := s.next(ctx, last)
		if errors.Is(err, sql.ErrNoRows) {
			break
		}
		if err != nil {
			return "", err
		}
		last = p.id
		reason, err := s.check(ctx, p)
		if err != nil {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			// Gone or too slow to read: tried again next time.
			slog.Info("file check skipped", "path", p.path, "err", err)
			skipped++
			continue
		}
		var errVal any
		if reason != "" {
			errVal = reason
		}
		// Only when the file is still the one checked (a scan may have replaced it meanwhile).
		res, err := s.DB.ExecContext(ctx, `UPDATE media_files SET checked_at = `+now+`, check_error = ?
			WHERE id = ? AND size = ? AND mtime = ? AND checked_at IS NULL`, errVal, p.id, p.size, p.mtime)
		if err != nil {
			return "", err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			continue
		}
		if reason != "" {
			slog.Warn("damaged file", "path", p.path, "reason", reason)
			damaged = append(damaged, Damaged{FileID: p.id, ItemID: p.itemID, Path: p.path, Reason: reason})
		} else {
			checked++
		}
	}
	if len(damaged) > 0 && s.OnDamaged != nil {
		s.OnDamaged(ctx, damaged)
	}
	done := checked + skipped + len(damaged)
	timed, mistimed, timingSkipped, err := s.runTiming(ctx, func(n int) {
		s.setProgress(done+n, done+max(timingTotal, n+1))
	})
	if err != nil {
		return "", err
	}
	if done+timed+timingSkipped == 0 {
		return "No new files", nil
	}
	var parts []string
	if done > 0 {
		parts = append(parts, fmt.Sprintf("%d checked, %d damaged", checked+len(damaged), len(damaged)))
	}
	if timed > 0 {
		parts = append(parts, fmt.Sprintf("frame timing of %d checked, %d out of order", timed, mistimed))
	}
	if skipped+timingSkipped > 0 {
		parts = append(parts, fmt.Sprintf("%d couldn't be read (tried again next time)", skipped+timingSkipped))
	}
	return capitalize(strings.Join(parts, "; ")), nil
}

// timingEligible is the condition for a file whose frame timing is checked: as for the
// playback check, with a video stream, and not an AVI (frametiming.Checked).
const timingEligible = eligible + ` AND EXISTS (SELECT 1 FROM streams st WHERE st.file_id = f.id AND st.kind = 'video')
	AND LOWER(COALESCE(f.container, '')) <> 'avi' AND LOWER(f.path) NOT LIKE '%.avi'`

// skipTiming marks the files whose frame timing isn't checked as done, so the waiting list
// stays short.
func (s *Service) skipTiming(ctx context.Context) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE media_files SET timing_checked_at = `+now+`, timing_error = NULL
		WHERE timing_checked_at IS NULL AND id NOT IN (SELECT f.id FROM media_files f
			CROSS JOIN media_versions v ON v.id = f.version_id CROSS JOIN items i ON i.id = v.item_id
			CROSS JOIN libraries l ON l.id = i.library_id
			WHERE f.timing_checked_at IS NULL AND `+timingEligible+`)`)
	return err
}

// runTiming checks the frame timing of every file waiting for it, one at a time; progress
// hears how many are done.
func (s *Service) runTiming(ctx context.Context, progress func(int)) (checked, mistimed, skipped int, err error) {
	var last int64
	for {
		if err := ctx.Err(); err != nil {
			return 0, 0, 0, err
		}
		progress(checked + skipped)
		var id, size, mtime int64
		var path string
		err := s.DB.QueryRowContext(ctx, `SELECT f.id, f.path, f.size, f.mtime
			FROM media_files f CROSS JOIN media_versions v ON v.id = f.version_id CROSS JOIN items i ON i.id = v.item_id
			CROSS JOIN libraries l ON l.id = i.library_id
			WHERE f.timing_checked_at IS NULL AND f.available = 1 AND f.id > ? AND `+timingEligible+`
			ORDER BY f.id LIMIT 1`, last).Scan(&id, &path, &size, &mtime)
		if errors.Is(err, sql.ErrNoRows) {
			return checked, mistimed, skipped, nil
		}
		if err != nil {
			return 0, 0, 0, err
		}
		last = id
		reason, err := s.timing(ctx, path)
		if err != nil {
			if ctx.Err() != nil {
				return 0, 0, 0, ctx.Err()
			}
			slog.Info("frame timing check skipped", "path", path, "err", err)
			skipped++
			continue
		}
		var errVal any
		if reason != "" {
			errVal = reason
		}
		res, err := s.DB.ExecContext(ctx, `UPDATE media_files SET timing_checked_at = `+now+`, timing_error = ?
			WHERE id = ? AND size = ? AND mtime = ? AND timing_checked_at IS NULL`, errVal, id, size, mtime)
		if err != nil {
			return 0, 0, 0, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			continue
		}
		checked++
		if reason != "" {
			slog.Warn("frame timing out of order", "path", path, "reason", reason)
			mistimed++
		}
	}
}

func (s *Service) timing(ctx context.Context, path string) (string, error) {
	if _, err := os.Stat(path); err != nil {
		return "", err
	}
	if s.Timing != nil {
		return s.Timing(ctx, path)
	}
	return frametiming.Check(ctx, s.LookPath, s.FFprobe, path)
}

// next is the first file after id waiting for its check, with its expected length when
// known (a movie or episode saved from on demand: the source's runtime).
func (s *Service) next(ctx context.Context, after int64) (pending, error) {
	var p pending
	var video int
	err := s.DB.QueryRowContext(ctx, `SELECT f.id, i.id, f.path, f.size, f.mtime, COALESCE(f.duration_ms, 0),
			COALESCE((SELECT MAX(vs.expected_ms) FROM vod_saves vs WHERE vs.target_path = f.path AND vs.state = 'done'), 0),
			EXISTS (SELECT 1 FROM streams st WHERE st.file_id = f.id AND st.kind = 'video')
		FROM media_files f CROSS JOIN media_versions v ON v.id = f.version_id CROSS JOIN items i ON i.id = v.item_id
		CROSS JOIN libraries l ON l.id = i.library_id
		WHERE f.checked_at IS NULL AND f.available = 1 AND f.id > ? AND `+eligible+`
		ORDER BY f.id LIMIT 1`, after).
		Scan(&p.id, &p.itemID, &p.path, &p.size, &p.mtime, &p.durationMS, &p.expectedMS, &video)
	p.video = video == 1
	return p, err
}

// check returns why the file is damaged ("" = it plays), or an error when it couldn't be
// checked. Files without a known length or without video are left to Library Health's
// other reasons (Zero duration, No audio or video streams): only what decodes is tested.
func (s *Service) check(ctx context.Context, p pending) (string, error) {
	if _, err := os.Stat(p.path); err != nil {
		return "", err
	}
	if !p.video {
		return "", nil
	}
	if p.durationMS > 0 && p.expectedMS > 0 {
		if reason := Length(p.durationMS, p.expectedMS); reason != "" {
			return reason, nil
		}
	}
	positions := []float64{0}
	if p.durationMS > 0 {
		positions = append(positions, EndPosition(p.durationMS))
	}
	for i, at := range positions {
		if err := s.decode(ctx, p.path, at); err != nil {
			if errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
				return "", err
			}
			where := "the start"
			if i > 0 {
				where = "the end"
			}
			return capitalize(fmt.Sprintf("%s doesn't decode: %s", where, ffmpegPrefix.ReplaceAllString(err.Error(), ""))), nil
		}
	}
	return "", nil
}

// ffmpegPrefix is the context FFmpeg starts its messages with ("[in#0/matroska,webm @ 0x…] ").
var ffmpegPrefix = regexp.MustCompile(`^\[[^\]]*@ ?0x[0-9a-f]+\] *`)

func (s *Service) decode(ctx context.Context, path string, at float64) error {
	if s.Decode != nil {
		return s.Decode(ctx, path, at)
	}
	ffmpeg := s.FFmpeg
	if ffmpeg == "" {
		ffmpeg = "ffmpeg"
	}
	return encode.DecodeCheckLow(ctx, s.LookPath, ffmpeg, path, at, 5, false)
}

// EndPosition is where the end check starts (seconds), as for VOD downloads: the last 30
// seconds, or the last third of a short file.
func EndPosition(durationMS int64) float64 {
	secs := float64(durationMS) / 1000
	return max(0, secs-min(30, secs/3))
}

// Length is why a file's length isn't plausible against the length expected ("" = it is):
// within 2%, with a minute's slack, as for VOD downloads whose runtime isn't the file's own.
func Length(durationMS, expectedMS int64) string {
	tol := max(expectedMS/50, 60_000)
	if d := durationMS - expectedMS; d > tol || -d > tol {
		return fmt.Sprintf("It is %s long; expected %s", clock(durationMS), clock(expectedMS))
	}
	return ""
}

func clock(ms int64) string {
	d := time.Duration(ms) * time.Millisecond
	return fmt.Sprintf("%d:%02d:%02d", int(d.Hours()), int(d.Minutes())%60, int(d.Seconds())%60)
}

func capitalize(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	if n == 0 {
		return s
	}
	return string(unicode.ToUpper(r)) + s[n:]
}
