// Package downloads converts videos for offline viewing on phones and tablets (M7). Jobs
// run one at a time on the server's encoders; finished files are kept for a week.
package downloads

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"marquee/internal/playback"
)

// Keep is how long finished conversions stay on the server.
const Keep = 7 * 24 * time.Hour

var ErrNotFound = errors.New("download not found")

type Job struct {
	ID         string
	UserID     int64
	ItemID     int64
	FileID     int64
	Quality    string
	Status     string
	Progress   float64
	Size       int64
	Path       string
	Error      string
	CreatedAt  time.Time
	FinishedAt *time.Time
}

// Service queues and runs conversions.
type Service struct {
	DB       *sql.DB
	Playback *playback.Manager
	Dir      string // <transcode dir>/downloads
	// Request builds the playback request (track preferences) for a user and item.
	Request func(ctx context.Context, userID, itemID, fileID int64) (playback.Request, error)

	once sync.Once
	wake chan struct{}
	mu   sync.Mutex
	live map[string]float64 // progress of running jobs
}

func newID() string {
	b := make([]byte, 12)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *Service) init() {
	s.once.Do(func() {
		s.wake = make(chan struct{}, 1)
		s.live = map[string]float64{}
	})
}

// Create queues a conversion, or returns an existing one for the same item and quality.
func (s *Service) Create(ctx context.Context, userID, itemID, fileID int64, quality string) (Job, error) {
	s.init()
	if _, ok := playback.OfflineQualities[quality]; !ok {
		return Job{}, errors.New("quality must be high, medium or low")
	}
	var id string
	err := s.DB.QueryRowContext(ctx, `SELECT id FROM download_jobs WHERE user_id = ? AND item_id = ? AND quality = ?
		AND COALESCE(file_id, 0) = ? AND status != 'failed'`, userID, itemID, quality, fileID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		id = newID()
		if _, err := s.DB.ExecContext(ctx, `INSERT INTO download_jobs (id, user_id, item_id, file_id, quality) VALUES (?, ?, ?, ?, ?)`,
			id, userID, itemID, nullID(fileID), quality); err != nil {
			return Job{}, err
		}
		select {
		case s.wake <- struct{}{}:
		default:
		}
	} else if err != nil {
		return Job{}, err
	}
	return s.Get(ctx, userID, id)
}

func nullID(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}

const cols = `id, user_id, item_id, COALESCE(file_id, 0), quality, status, progress, COALESCE(size, 0), COALESCE(path, ''), COALESCE(error, ''), created_at, finished_at`

func (s *Service) scan(row interface{ Scan(...any) error }) (Job, error) {
	var j Job
	var created string
	var finished sql.NullString
	err := row.Scan(&j.ID, &j.UserID, &j.ItemID, &j.FileID, &j.Quality, &j.Status, &j.Progress, &j.Size, &j.Path, &j.Error, &created, &finished)
	if err != nil {
		return j, err
	}
	j.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	if t, err := time.Parse(time.RFC3339Nano, finished.String); err == nil {
		j.FinishedAt = &t
	}
	s.mu.Lock()
	if p, ok := s.live[j.ID]; ok {
		j.Progress = p
	}
	s.mu.Unlock()
	return j, nil
}

// Get returns one of a user's jobs.
func (s *Service) Get(ctx context.Context, userID int64, id string) (Job, error) {
	s.init()
	j, err := s.scan(s.DB.QueryRowContext(ctx, `SELECT `+cols+` FROM download_jobs WHERE id = ? AND user_id = ?`, id, userID))
	if errors.Is(err, sql.ErrNoRows) {
		return j, ErrNotFound
	}
	return j, err
}

// List returns a user's jobs, newest first.
func (s *Service) List(ctx context.Context, userID int64) ([]Job, error) {
	s.init()
	rows, err := s.DB.QueryContext(ctx, `SELECT `+cols+` FROM download_jobs WHERE user_id = ? ORDER BY created_at DESC LIMIT 500`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Job
	for rows.Next() {
		j, err := s.scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

// Delete removes a job and its file (the app calls this once it has the file).
func (s *Service) Delete(ctx context.Context, userID int64, id string) error {
	j, err := s.Get(ctx, userID, id)
	if err != nil {
		return err
	}
	if j.Path != "" {
		os.Remove(j.Path)
	}
	_, err = s.DB.ExecContext(ctx, `DELETE FROM download_jobs WHERE id = ?`, id)
	return err
}

// Run converts queued jobs one at a time until ctx ends, and clears out old files.
func (s *Service) Run(ctx context.Context) {
	s.init()
	// Jobs cut short by a restart start over.
	s.DB.ExecContext(ctx, `UPDATE download_jobs SET status = 'queued', progress = 0 WHERE status = 'converting'`)
	tick := time.NewTicker(time.Hour)
	defer tick.Stop()
	for {
		s.cleanup(ctx)
		for ctx.Err() == nil && s.next(ctx) {
		}
		select {
		case <-ctx.Done():
			return
		case <-s.wake:
		case <-tick.C:
		}
	}
}

// next runs the oldest queued job; false when there is none.
func (s *Service) next(ctx context.Context) bool {
	j, err := s.scan(s.DB.QueryRowContext(ctx, `SELECT `+cols+` FROM download_jobs WHERE status = 'queued' ORDER BY created_at LIMIT 1`))
	if err != nil {
		return false
	}
	s.DB.ExecContext(ctx, `UPDATE download_jobs SET status = 'converting' WHERE id = ?`, j.ID)
	fail := func(err error) bool {
		slog.Warn("download conversion failed", "item", j.ItemID, "err", err)
		s.DB.ExecContext(context.WithoutCancel(ctx), `UPDATE download_jobs SET status = 'failed', error = ?,
			finished_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = ?`, err.Error(), j.ID)
		return true
	}
	r, err := s.Request(ctx, j.UserID, j.ItemID, j.FileID)
	if err != nil {
		return fail(err)
	}
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return fail(err)
	}
	out := filepath.Join(s.Dir, j.ID+".mp4")
	job, dur, err := s.Playback.OfflineJob(ctx, r, j.Quality, out)
	if err != nil {
		return fail(err)
	}
	slog.Info("converting for download", "item", j.ItemID, "quality", j.Quality, "encoder", job.Encoder)
	err = s.Playback.RunOffline(ctx, job, dur, func(p float64) {
		s.mu.Lock()
		s.live[j.ID] = p
		s.mu.Unlock()
	})
	s.mu.Lock()
	delete(s.live, j.ID)
	s.mu.Unlock()
	if err != nil {
		os.Remove(out)
		if ctx.Err() != nil {
			return false
		}
		return fail(err)
	}
	st, err := os.Stat(out)
	if err != nil {
		return fail(err)
	}
	s.DB.ExecContext(ctx, `UPDATE download_jobs SET status = 'ready', progress = 1, size = ?, path = ?,
		finished_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = ?`, st.Size(), out, j.ID)
	return true
}

func (s *Service) cleanup(ctx context.Context) {
	cutoff := time.Now().Add(-Keep).UTC().Format("2006-01-02T15:04:05.000Z")
	rows, err := s.DB.QueryContext(ctx, `SELECT id, COALESCE(path, '') FROM download_jobs WHERE finished_at < ?`, cutoff)
	if err != nil {
		return
	}
	var ids []string
	for rows.Next() {
		var id, path string
		if rows.Scan(&id, &path) == nil {
			if path != "" {
				os.Remove(path)
			}
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		s.DB.ExecContext(ctx, `DELETE FROM download_jobs WHERE id = ?`, id)
	}
}
