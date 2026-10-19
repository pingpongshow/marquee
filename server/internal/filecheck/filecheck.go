// Package filecheck finds damaged new video files (the Check new files task): each movie,
// episode and personal video added to the library (or replaced) gets the playback check VOD
// downloads get, at low priority and one file at a time. Its length must be plausible where
// it is known, and a few seconds at its start and near its end must decode. Files that fail
// are listed in Library Health → Unplayable files with the reason; files that pass keep only
// media_files.checked_at. Files already in the library when the check arrived were marked
// checked by the migration and aren't checked.
package filecheck

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"regexp"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"marquee/internal/encode"
	"marquee/internal/tasks"
)

// TaskID is the scheduled task's id.
const TaskID = "check-new-files"

// Task is the scheduled task: hourly (new files are also checked straight after a scan
// adds them), at low priority, one file at a time.
func (s *Service) Task() tasks.Task {
	return tasks.Task{ID: TaskID, Name: "Check new files", Every: time.Hour,
		Description: "Checks each movie, episode and personal video added to the library (or replaced) for damage: a few seconds at its start and near its end must decode. Damaged files are listed in Library Health → Unplayable files. Runs after a scan adds files, at low priority, one file at a time; files that were already in the library aren't checked.",
		Run:         s.Run, Progress: s.Progress}
}

// Damaged is a file that failed its check.
type Damaged struct {
	FileID, ItemID int64
	Path, Reason   string
}

// Service checks new files.
type Service struct {
	DB     *sql.DB
	FFmpeg string
	// LookPath finds nice and ionice (nil = exec.LookPath).
	LookPath func(string) (string, error)
	// Decode replaces the decode test (tests); nil = encode.DecodeCheckLow.
	Decode func(ctx context.Context, path string, at float64) error
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

// Pending reports whether any file is waiting for its check (cheap: for triggering the task
// after a scan).
func (s *Service) Pending(ctx context.Context) bool {
	var n int
	s.DB.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM media_files WHERE checked_at IS NULL AND available = 1)`).Scan(&n)
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
	var total int
	if err := s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM media_files WHERE checked_at IS NULL AND available = 1`).Scan(&total); err != nil {
		return "", err
	}
	var checked, skipped int
	var damaged []Damaged
	var last int64
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		s.setProgress(checked+skipped+len(damaged), max(total, checked+skipped+len(damaged)+1))
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
	if checked+len(damaged)+skipped == 0 {
		return "No new files", nil
	}
	msg := fmt.Sprintf("%d checked, %d damaged", checked+len(damaged), len(damaged))
	if skipped > 0 {
		msg += fmt.Sprintf(", %d couldn't be read (tried again next time)", skipped)
	}
	return msg, nil
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
