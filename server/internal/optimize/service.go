// Package optimize is the library optimizer (Library Health → Large files, admins only):
// oversized movie and episode files are listed from what the library knows, queued with a
// preset, re-encoded (NVENC, then Quick Sync, then the CPU, at the lowest priority) next to
// the original, checked, and swapped in when they're enough smaller. The original is then
// deleted, or, with keepOriginals on, goes to the media trash, where it can be restored
// from; the library item keeps its id, watch history and metadata. Playback comes first: jobs pause while anyone watches (a setting),
// keep a free NVENC session for playback and give theirs up when playback needs it.
package optimize

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sync"
	"time"

	"marquee/internal/encode"
	"marquee/internal/mediatrash"
	"marquee/internal/settings"
	"marquee/internal/tasks"
)

// Job states.
const (
	StateQueued    = "queued"
	StateRunning   = "running"
	StateVerifying = "verifying"
	StateDone      = "done"
	StateFailed    = "failed"
	StateCancelled = "cancelled"
	StateSkipped   = "skipped"
	StateRestored  = "restored"
)

// PartSuffix is added to the original's name for the file being made.
const PartSuffix = ".optimizing.part"

var (
	ErrNotFound    = errors.New("no such job")
	ErrBadState    = errors.New("the job can't do that now")
	ErrCantRestore = errors.New("the original isn't in the media trash any more")
	ErrBusy        = errors.New("the file is being watched; try again when it isn't")
	ErrNoPreset    = errors.New("no such preset")
)

// Service runs the optimizer queue.
type Service struct {
	DB       *sql.DB
	Settings func() settings.Settings
	// FFmpeg and FFprobe encode and check files.
	FFmpeg, FFprobe string
	// Encoders are the video encoders to try, best first (playback's EncoderChoices, which
	// already skips NVENC when playback has used up its sessions); nil = software.
	Encoders  func() []string
	QSVDevice string
	// PlaybackNVENC is how many NVENC encodes playback is running (nil = none).
	PlaybackNVENC func() int
	// HasEncoder reports whether FFmpeg has an encoder (e.g. libsvtav1); nil = all.
	HasEncoder func(name string) bool
	// Trash takes the originals.
	Trash *mediatrash.Service
	// Rescan rescans a library after a file was replaced.
	Rescan func(libraryID int64)
	// Playing reports whether someone is watching a file; Watching whether anyone is
	// watching anything.
	Playing  func(fileID int64) bool
	Watching func() bool
	// LookPath finds nice and ionice (tests); nil = exec.LookPath.
	LookPath func(string) (string, error)
	// Now is the clock (tests).
	Now func() time.Time
	// Tick is how often the queue is looked at (default 2 s); Grace how long after the last
	// viewer paused jobs resume (default 15 s); BusyRetry how often a finished file waits
	// for nobody to be watching it (default 30 s).
	Tick, Grace, BusyRetry time.Duration

	mu           sync.Mutex
	running      map[int64]*job
	wake         chan struct{}
	lastWatching time.Time
	holdReason   string
	wg           sync.WaitGroup
}

// job is a running job.
type job struct {
	id      int64
	cancel  context.CancelFunc
	why     error // why it was cancelled: errCancelled, errYield, errHold
	cmd     *exec.Cmd
	encoder string
	stopped bool // the encode is suspended (SIGSTOP)
	state   string
	prog    encode.Progress
	saved   time.Time
	note    string // e.g. waiting for nobody to be watching
}

var (
	errCancelled = errors.New("cancelled")
	errYield     = errors.New("gave the GPU to playback")
	errRestart   = errors.New("paused")
)

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) cfg() settings.Optimize {
	if s.Settings == nil {
		return settings.Defaults().Optimize
	}
	return s.Settings().Optimize
}

func (s *Service) stamp() string { return s.now().UTC().Format(time.RFC3339Nano) }

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

// Enqueue queues files with a preset. Files already queued or being optimized, and files
// that aren't available movies or episodes in a video library, are skipped.
func (s *Service) Enqueue(ctx context.Context, fileIDs []int64, presetID string) (queued, skipped int, err error) {
	p, ok := s.cfg().Preset(presetID)
	if !ok {
		return 0, 0, ErrNoPreset
	}
	if err := p.Validate(); err != nil {
		return 0, 0, err
	}
	raw, _ := json.Marshal(p)
	for _, id := range fileIDs {
		var itemID, libID, size int64
		var title, path string
		err := s.DB.QueryRowContext(ctx, `SELECT i.id, f.library_id, f.size, i.title, f.path FROM media_files f
			CROSS JOIN media_versions v ON v.id = f.version_id CROSS JOIN items i ON i.id = v.item_id
			CROSS JOIN libraries l ON l.id = f.library_id
			WHERE f.id = ? AND f.available = 1 AND i.type IN ('movie', 'episode') AND i.extra_type IS NULL
			  AND l.type IN ('movies', 'shows', 'anime', 'videos')
			  AND NOT EXISTS (SELECT 1 FROM optimize_jobs j WHERE j.file_id = f.id AND j.state IN ('queued', 'running', 'verifying'))`, id).
			Scan(&itemID, &libID, &size, &title, &path)
		if errors.Is(err, sql.ErrNoRows) {
			skipped++
			continue
		}
		if err != nil {
			return queued, skipped, err
		}
		if _, err := s.DB.ExecContext(ctx, `INSERT INTO optimize_jobs (file_id, item_id, library_id, title, path, preset_id, preset_name, preset, state, before_size)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'queued', ?)`, id, itemID, libID, displayTitle(ctx, s.DB, itemID, title), path, p.ID, p.Name, string(raw), size); err != nil {
			return queued, skipped, err
		}
		queued++
	}
	if queued > 0 {
		s.kick()
	}
	return queued, skipped, nil
}

// displayTitle is "Show · S01E02 · Title" for an episode, the title otherwise.
func displayTitle(ctx context.Context, db *sql.DB, itemID int64, title string) string {
	var show string
	var season, ep sql.NullInt64
	err := db.QueryRowContext(ctx, `SELECT COALESCE(gp.title, ''), par.idx, i.idx FROM items i
		LEFT JOIN items par ON par.id = i.parent_id LEFT JOIN items gp ON gp.id = i.grandparent_id
		WHERE i.id = ? AND i.type = 'episode'`, itemID).Scan(&show, &season, &ep)
	if err != nil || show == "" {
		return title
	}
	return fmt.Sprintf("%s · S%02dE%02d · %s", show, season.Int64, ep.Int64, title)
}

// Cancel stops a queued or running job (the original stays as it was).
func (s *Service) Cancel(ctx context.Context, id int64) error {
	s.mu.Lock()
	if j, ok := s.running[id]; ok {
		if j.state == StateVerifying && j.note == "" {
			s.mu.Unlock()
			return ErrBadState // checking or swapping: too late
		}
		j.why = errCancelled
		j.cancel()
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()
	res, err := s.DB.ExecContext(ctx, `UPDATE optimize_jobs SET state = 'cancelled', finished_at = ? WHERE id = ? AND state = 'queued'`, s.stamp(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return s.exists(ctx, id, ErrBadState)
	}
	return nil
}

func (s *Service) exists(ctx context.Context, id int64, otherwise error) error {
	var n int
	s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM optimize_jobs WHERE id = ?`, id).Scan(&n)
	if n == 0 {
		return ErrNotFound
	}
	return otherwise
}

// Retry queues a failed, cancelled or skipped job again, at the end of the queue.
func (s *Service) Retry(ctx context.Context, id int64) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE optimize_jobs SET state = 'queued', progress = 0, error = '', encoder = '', after_size = 0,
		cleared = 0, created_at = ?, started_at = NULL, finished_at = NULL
		WHERE id = ? AND state IN ('failed', 'cancelled', 'skipped') AND file_id IS NOT NULL
		  AND NOT EXISTS (SELECT 1 FROM optimize_jobs o WHERE o.file_id = optimize_jobs.file_id AND o.id <> optimize_jobs.id
			AND o.state IN ('queued', 'running', 'verifying'))`, s.stamp(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return s.exists(ctx, id, ErrBadState)
	}
	s.kick()
	return nil
}

// ClearFinished hides finished jobs from the queue (optimized ones stay in the history).
func (s *Service) ClearFinished(ctx context.Context) (int, error) {
	res, err := s.DB.ExecContext(ctx, `UPDATE optimize_jobs SET cleared = 1 WHERE cleared = 0 AND state IN ('done', 'failed', 'cancelled', 'skipped', 'restored')`)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// Run works through the queue until ctx ends.
func (s *Service) Run(ctx context.Context) {
	s.mu.Lock()
	if s.wake == nil {
		s.wake = make(chan struct{}, 1)
	}
	w := s.wake
	s.mu.Unlock()
	// Left running when the server stopped: start them again from the beginning.
	if rows, err := s.DB.QueryContext(ctx, `SELECT path FROM optimize_jobs WHERE state IN ('running', 'verifying')`); err == nil {
		var paths []string
		for rows.Next() {
			var p string
			if rows.Scan(&p) == nil {
				paths = append(paths, p)
			}
		}
		rows.Close()
		for _, p := range paths {
			os.Remove(p + PartSuffix)
		}
		s.DB.ExecContext(ctx, `UPDATE optimize_jobs SET state = 'queued', progress = 0 WHERE state IN ('running', 'verifying')`)
	}
	tick := s.Tick
	if tick == 0 {
		tick = 2 * time.Second
	}
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		s.step(ctx)
		select {
		case <-ctx.Done():
			s.wg.Wait()
			return
		case <-t.C:
		case <-w:
		}
	}
}

// hold says why jobs shouldn't run now ("" = they may).
func (s *Service) hold(c settings.Optimize) string {
	now := s.now()
	if c.Paused {
		return "Paused"
	}
	if c.WindowOnly && s.Settings != nil && !tasks.InWindow(now, s.Settings().Tasks) {
		return "Waiting for the maintenance window"
	}
	if c.PauseWhileWatching && s.Watching != nil {
		grace := s.Grace
		if grace == 0 {
			grace = 15 * time.Second
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.Watching() {
			s.lastWatching = now
		}
		if !s.lastWatching.IsZero() && now.Sub(s.lastWatching) < grace {
			return "Paused while someone is watching"
		}
	}
	return ""
}

// step pauses or resumes running jobs, gives NVENC back to playback and starts queued jobs.
func (s *Service) step(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	c := s.cfg()
	reason := s.hold(c)
	s.mu.Lock()
	s.holdReason = reason
	if s.running == nil {
		s.running = map[int64]*job{}
	}
	for _, j := range s.running {
		switch {
		case reason != "" && !j.stopped && j.cmd != nil:
			if err := suspend(j.cmd.Process); err == nil {
				j.stopped = true
			} else {
				j.why = errRestart // can't suspend here: stop it and start again later
				j.cancel()
			}
		case reason == "" && j.stopped && j.cmd != nil:
			if resume(j.cmd.Process) == nil {
				j.stopped = false
			}
		}
	}
	// Playback has priority on NVENC: when it needs the session an optimizer job holds,
	// the job gives it up (and starts again on another encoder).
	if limit := s.nvencLimit(); limit > 0 {
		if s.playbackNVENC()+s.nvencInUseLocked() >= limit {
			for _, j := range s.running {
				if j.encoder == "nvenc" && j.why == nil {
					j.why = errYield
					j.cancel()
					break
				}
			}
		}
	}
	free := c.Concurrency - len(s.running)
	s.mu.Unlock()
	if reason != "" || free <= 0 {
		return
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT id FROM optimize_jobs WHERE state = 'queued' ORDER BY created_at, id LIMIT ?`, free)
	if err != nil {
		return
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
		s.start(ctx, id)
	}
}

func (s *Service) nvencLimit() int {
	if s.Settings == nil {
		return 0
	}
	return s.Settings().Transcoder.NVENCSessions
}

func (s *Service) playbackNVENC() int {
	if s.PlaybackNVENC == nil {
		return 0
	}
	return s.PlaybackNVENC()
}

func (s *Service) nvencInUseLocked() int {
	n := 0
	for _, j := range s.running {
		if j.encoder == "nvenc" {
			n++
		}
	}
	return n
}

// start runs a job in the background.
func (s *Service) start(ctx context.Context, id int64) {
	jctx, cancel := context.WithCancel(ctx)
	j := &job{id: id, cancel: cancel, state: StateRunning}
	s.mu.Lock()
	if _, ok := s.running[id]; ok {
		s.mu.Unlock()
		cancel()
		return
	}
	s.running[id] = j
	s.mu.Unlock()
	if _, err := s.DB.ExecContext(ctx, `UPDATE optimize_jobs SET state = 'running', progress = 0, error = '', started_at = ? WHERE id = ?`, s.stamp(), id); err != nil {
		s.finish(j)
		return
	}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer s.finish(j)
		s.process(jctx, j)
	}()
}

func (s *Service) finish(j *job) {
	j.cancel()
	s.mu.Lock()
	delete(s.running, j.id)
	s.mu.Unlock()
	s.kick()
}

// Active is a running job, for the activity monitor.
type Active struct {
	ID       int64
	Title    string
	State    string
	Fraction float64
	Paused   string // why it's paused, "" = it isn't
}

// Active lists the running jobs.
func (s *Service) Active(ctx context.Context) []Active {
	s.mu.Lock()
	type snap struct {
		id       int64
		state    string
		fraction float64
		paused   string
	}
	var snaps []snap
	for _, j := range s.running {
		p := ""
		if j.stopped {
			p = s.holdReason
		} else if j.note != "" {
			p = j.note
		}
		snaps = append(snaps, snap{j.id, j.state, j.prog.Fraction, p})
	}
	s.mu.Unlock()
	var out []Active
	for _, sn := range snaps {
		var title string
		s.DB.QueryRowContext(ctx, `SELECT title FROM optimize_jobs WHERE id = ?`, sn.id).Scan(&title)
		out = append(out, Active{ID: sn.id, Title: title, State: sn.state, Fraction: sn.fraction, Paused: sn.paused})
	}
	return out
}

// HoldReason says why the queue isn't running now ("" = it is, or may).
func (s *Service) HoldReason() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.holdReason
}

// live is a running job's progress.
type live struct {
	prog    encode.Progress
	encoder string
	paused  string
	state   string
}

func (s *Service) liveFor(id int64) (live, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	j, ok := s.running[id]
	if !ok {
		return live{}, false
	}
	l := live{prog: j.prog, encoder: j.encoder, state: j.state}
	if j.stopped {
		l.paused = s.holdReason
	} else if j.note != "" {
		l.paused = j.note
	}
	return l, true
}
