// Package vodsave saves films and episodes from movies-on-demand (Xtream Codes) sources into
// the library's folders (admins only). Saves wait in a queue that survives restarts; a worker
// per source downloads them (resuming with Range requests), converts them when the settings
// ask for it, verifies them, and only then moves them into the library and scans it.
//
// Not to be confused with package downloads (offline copies on phones and tablets).
package vodsave

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"marquee/internal/library"
	"marquee/internal/metadata/tmdb"
	"marquee/internal/settings"
	"marquee/internal/vod"
)

// States of a save.
const (
	Queued      = "queued"
	Downloading = "downloading"
	Processing  = "processing"
	Verifying   = "verifying"
	Paused      = "paused" // paused by the admin (automatic pauses leave items queued)
	Done        = "done"
	Failed      = "failed"
	Cancelled   = "cancelled"
	Skipped     = "skipped"
)

var (
	ErrNotFound    = errors.New("not found")
	ErrNoFolder    = errors.New("choose a folder to save to in Settings → Movies on demand")
	ErrNotSavable  = vod.ErrNotSavable
	ErrWrongState  = errors.New("the save can't do that now")
	errNoEpisodes  = errors.New("the source gave no episodes for this series")
	activeStates   = []any{Queued, Downloading, Processing, Verifying, Paused}
	runningStates  = []any{Downloading, Processing, Verifying}
	finishedStates = []any{Done, Failed, Cancelled, Skipped}
)

// Playing is something being watched now, for pausing saves while watching.
type Playing struct {
	// Kind: vod (a film or episode on demand), live (a Live TV channel), library.
	Kind string
	// SourceID is the movies-on-demand source (vod).
	SourceID string
	// Host is the host the stream comes from (vod, live), to match a Dispatcharr Live TV
	// source with a Dispatcharr movie source on the same server.
	Host string
}

// Titles looks up TMDB's titles, so saved files are named as the library will match them.
type Titles interface {
	MovieByID(ctx context.Context, id int) (*tmdb.Movie, error)
	TVByID(ctx context.Context, id int) (*tmdb.TVShow, error)
}

// Service is the save queue.
type Service struct {
	DB       *sql.DB
	Settings *settings.Store
	VOD      *vod.Service
	Titles   Titles // optional
	HTTP     *http.Client
	FFmpeg   string
	FFprobe  string
	// Encoders are the encoders to try, best first (nvenc, qsv, software); nil = software.
	Encoders  func() []string
	QSVDevice string
	// Libraries lists the libraries (to scan the one a file was saved into); Scan queues a
	// library scan.
	Libraries func(ctx context.Context) ([]library.Library, error)
	Scan      func(libID int64)
	// Activity is what's being watched now (nil: nothing pauses saves).
	Activity func(ctx context.Context) []Playing
	// OnFailed hears a save that failed; OnDrained hears the queue empty after saving
	// (admin alerts).
	OnFailed  func(s Save)
	OnDrained func(done, failed int, titles []string)
	// FreeSpace reports the bytes free on the volume holding path (tests replace it).
	FreeSpace func(path string) (uint64, error)

	// Timings; zero values are the defaults.
	Tick             time.Duration   // how often the queue is looked at (2 s)
	Backoff          []time.Duration // waits after network failures (1, 3, 8, 18 min: 5 tries in ~30 min)
	Grace            time.Duration   // saves resume this long after watching stops (60 s)
	SourceDownWait   time.Duration   // first wait for a source that's down (1 min, doubling to 30)
	RetryFailedEvery time.Duration   // failed saves are queued again this often (4 h)
	ScanEvery        time.Duration   // while a library keeps getting files, scans at most this often (15 min)

	once        sync.Once
	mu          sync.Mutex
	wake        chan struct{}
	jobs        map[int64]*job
	lastActive  map[string]time.Time // source -> last time something was watched from it
	pauseReason map[string]string    // source -> why it is paused automatically
	down        map[string]sourceDown
	lastScan    map[int64]time.Time
	pendingScan map[int64]bool
	lastRetry   time.Time
	drain       drainCount
}

type sourceDown struct {
	until time.Time
	wait  time.Duration
	err   string
}

type drainCount struct {
	done, failed int
	titles       []string
}

func (s *Service) init() {
	s.once.Do(func() {
		s.wake = make(chan struct{}, 1)
		s.jobs = map[int64]*job{}
		s.lastActive = map[string]time.Time{}
		s.pauseReason = map[string]string{}
		s.down = map[string]sourceDown{}
		s.lastScan = map[int64]time.Time{}
		s.pendingScan = map[int64]bool{}
		s.lastRetry = time.Now()
	})
}

// Wake asks the worker to look at the queue now.
func (s *Service) Wake() {
	s.init()
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *Service) cfg() settings.VODSave { return s.Settings.Get().VODSave }

func now() string { return time.Now().UTC().Format(time.RFC3339) }

// Save is one queued film or episode.
type Save struct {
	ID            int64
	SourceID      string
	SourceName    string
	Kind          string // movie, episode
	VODID         int64  // the film's or episode's id
	SeriesID      int64
	SourceKey     string
	File          string
	Title         string
	TMDBID        int
	ExpectedMS    int64
	TargetPath    string
	Quality       string
	State         string
	BytesDone     int64
	BytesTotal    int64
	Attempts      int
	NextAttemptAt *time.Time
	Error         string
	Verified      bool
	VerifyNote    string
	SHA256        string
	CreatedAt     time.Time
	StartedAt     *time.Time
	FinishedAt    *time.Time
	RequestedBy   int64
	// Live, while it runs.
	Progress    float64 // 0–1 of the current step (processing, verifying); downloads use bytes
	SpeedBps    float64
	ETASeconds  int64
	PauseReason string // why a queued save isn't running (paused queue, watching, source down)
}

const saveCols = `id, source_id, kind, vod_id, COALESCE(series_id,0), source_key, file, title, COALESCE(tmdb_id,0), expected_ms,
	target_path, quality, state, bytes_done, bytes_total, attempts, next_attempt_at, error, verified, verify_note, sha256,
	created_at, started_at, finished_at, COALESCE(requested_by,0)`

func scanSave(sc interface{ Scan(...any) error }) (Save, error) {
	var v Save
	var next, created, started, finished sql.NullString
	err := sc.Scan(&v.ID, &v.SourceID, &v.Kind, &v.VODID, &v.SeriesID, &v.SourceKey, &v.File, &v.Title, &v.TMDBID, &v.ExpectedMS,
		&v.TargetPath, &v.Quality, &v.State, &v.BytesDone, &v.BytesTotal, &v.Attempts, &next, &v.Error, &v.Verified, &v.VerifyNote,
		&v.SHA256, &created, &started, &finished, &v.RequestedBy)
	if err != nil {
		return v, err
	}
	parse := func(n sql.NullString) *time.Time {
		if t, err := time.Parse(time.RFC3339, n.String); n.Valid && err == nil {
			return &t
		}
		return nil
	}
	v.NextAttemptAt, v.StartedAt, v.FinishedAt = parse(next), parse(started), parse(finished)
	if t := parse(created); t != nil {
		v.CreatedAt = *t
	}
	return v, nil
}

func (s *Service) one(ctx context.Context, id int64) (Save, error) {
	v, err := scanSave(s.DB.QueryRowContext(ctx, "SELECT "+saveCols+" FROM vod_saves WHERE id=?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return v, ErrNotFound
	}
	return v, err
}

func in(n int) string { return "?" + strings.Repeat(",?", n-1) }

// List is the queue: unfinished saves in the order they run, then finished ones, newest
// first; with live progress, speed and why queued ones wait.
func (s *Service) List(ctx context.Context) ([]Save, error) {
	s.init()
	rows, err := s.DB.QueryContext(ctx, "SELECT "+saveCols+" FROM vod_saves ORDER BY state IN ("+in(len(finishedStates))+
		"), CASE WHEN state IN ("+in(len(finishedStates))+") THEN -id ELSE queue_pos END, id",
		append(append([]any{}, finishedStates...), finishedStates...)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Save
	for rows.Next() {
		v, err := scanSave(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	paused := s.QueuePaused(ctx)
	names := map[string]string{}
	s.mu.Lock()
	for i := range out {
		v := &out[i]
		if _, ok := names[v.SourceID]; !ok && s.VOD != nil {
			names[v.SourceID] = s.VOD.SourceName(v.SourceID)
		}
		v.SourceName = names[v.SourceID]
		if j := s.jobs[v.ID]; j != nil {
			j.fill(v)
		}
		if v.State == Queued {
			switch {
			case paused:
				v.PauseReason = "Queue paused"
			case s.pauseReason[v.SourceID] != "":
				v.PauseReason = s.pauseReason[v.SourceID]
			case s.down[v.SourceID].err != "":
				d := s.down[v.SourceID]
				v.PauseReason = "Source unreachable (" + d.err + "); trying again at " + d.until.Local().Format("15:04")
			}
		}
	}
	s.mu.Unlock()
	return out, nil
}

// Active reports whether anything is queued or running (the web polls while it is).
func (s *Service) Active(ctx context.Context) bool {
	var n int
	s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM vod_saves WHERE state IN ("+in(len(activeStates)-1)+")", activeStates[:4]...).Scan(&n)
	return n > 0
}

const pausedKey = "vod_save_paused"

// QueuePaused reports whether the admin paused the whole queue.
func (s *Service) QueuePaused(ctx context.Context) bool {
	var v string
	s.DB.QueryRowContext(ctx, "SELECT value FROM settings WHERE key=?", pausedKey).Scan(&v)
	return v == "true"
}

// SetQueuePaused pauses or resumes the whole queue. Running saves stop and keep their
// partial files, to continue from there.
func (s *Service) SetQueuePaused(ctx context.Context, paused bool) error {
	s.init()
	if _, err := s.DB.ExecContext(ctx, `INSERT INTO settings(key, value) VALUES(?,?) ON CONFLICT(key) DO UPDATE SET value=excluded.value`,
		pausedKey, fmt.Sprint(paused)); err != nil {
		return err
	}
	if paused {
		s.stopJobs(func(*job) bool { return true }, errQueuePaused)
	}
	s.Wake()
	return nil
}

// Pause pauses one save (it keeps what it downloaded); Resume queues it again.
func (s *Service) Pause(ctx context.Context, id int64) error {
	s.init()
	r, err := s.DB.ExecContext(ctx, "UPDATE vod_saves SET state=? WHERE id=? AND state IN (?,?,?,?)", Paused, id, Queued, Downloading, Processing, Verifying)
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return s.stateErr(ctx, id)
	}
	s.stopJobs(func(j *job) bool { return j.id == id }, errUserPaused)
	return nil
}

func (s *Service) Resume(ctx context.Context, id int64) error {
	s.init()
	r, err := s.DB.ExecContext(ctx, "UPDATE vod_saves SET state=?, next_attempt_at=NULL WHERE id=? AND state=?", Queued, id, Paused)
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return s.stateErr(ctx, id)
	}
	s.Wake()
	return nil
}

func (s *Service) stateErr(ctx context.Context, id int64) error {
	if _, err := s.one(ctx, id); err != nil {
		return err
	}
	return ErrWrongState
}

// Cancel stops a save and removes its partial files.
func (s *Service) Cancel(ctx context.Context, id int64) error {
	s.init()
	v, err := s.one(ctx, id)
	if err != nil {
		return err
	}
	if v.State == Done || v.State == Skipped || v.State == Cancelled {
		return ErrWrongState
	}
	if _, err := s.DB.ExecContext(ctx, "UPDATE vod_saves SET state=?, finished_at=?, error='' WHERE id=?", Cancelled, now(), id); err != nil {
		return err
	}
	if !s.stopJobs(func(j *job) bool { return j.id == id }, errCancelled) {
		removeParts(v.TargetPath)
	}
	return nil
}

// Retry queues a failed or cancelled save again; a failed one continues from what it had
// downloaded.
func (s *Service) Retry(ctx context.Context, id int64) error {
	s.init()
	r, err := s.DB.ExecContext(ctx, `UPDATE vod_saves SET state=?, attempts=0, verify_tries=0, next_attempt_at=NULL, error='',
		finished_at=NULL, queue_pos=(SELECT COALESCE(MAX(queue_pos),0)+1 FROM vod_saves) WHERE id=? AND state IN (?,?)`, Queued, id, Failed, Cancelled)
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return s.stateErr(ctx, id)
	}
	s.Wake()
	return nil
}

// MoveToTop makes a waiting save the next to run.
func (s *Service) MoveToTop(ctx context.Context, id int64) error {
	r, err := s.DB.ExecContext(ctx, `UPDATE vod_saves SET queue_pos=(SELECT COALESCE(MIN(queue_pos),0)-1 FROM vod_saves)
		WHERE id=? AND state IN (?,?)`, id, Queued, Paused)
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return s.stateErr(ctx, id)
	}
	s.Wake()
	return nil
}

// ClearFinished removes done, failed, cancelled and skipped saves from the list (failed
// ones' partial files go too).
func (s *Service) ClearFinished(ctx context.Context) error {
	rows, err := s.DB.QueryContext(ctx, "SELECT target_path FROM vod_saves WHERE state=?", Failed)
	if err != nil {
		return err
	}
	var paths []string
	for rows.Next() {
		var p string
		rows.Scan(&p)
		paths = append(paths, p)
	}
	rows.Close()
	for _, p := range paths {
		removeParts(p)
	}
	_, err = s.DB.ExecContext(ctx, "DELETE FROM vod_saves WHERE state IN ("+in(len(finishedStates))+")", finishedStates...)
	return err
}

// ---- what's saved

// ItemState is a film's or episode's latest save ("" when never saved).
type ItemState struct {
	ID       int64
	State    string
	Progress float64 // 0–1
	Verified bool
	Error    string
}

func (s *Service) itemState(v Save) ItemState {
	st := ItemState{ID: v.ID, State: v.State, Verified: v.Verified, Error: v.Error}
	if v.BytesTotal > 0 {
		st.Progress = float64(v.BytesDone) / float64(v.BytesTotal)
	}
	s.mu.Lock()
	if j := s.jobs[v.ID]; j != nil {
		j.fill(&v)
		st.Progress = j.overall()
	}
	s.mu.Unlock()
	if v.State == Done || v.State == Skipped {
		st.Progress = 1
	}
	return st
}

// MovieState is a film's latest save.
func (s *Service) MovieState(ctx context.Context, movieID int64) (ItemState, error) {
	s.init()
	v, err := scanSave(s.DB.QueryRowContext(ctx, "SELECT "+saveCols+` FROM vod_saves WHERE kind='movie' AND
		(source_id, 'xtream:'||source_key)=(SELECT source_id, source_key FROM vod_movies WHERE id=?) ORDER BY id DESC LIMIT 1`, movieID))
	if errors.Is(err, sql.ErrNoRows) {
		return ItemState{}, nil
	}
	if err != nil {
		return ItemState{}, err
	}
	return s.itemState(v), nil
}

// EpisodeStates are the latest saves of a series' episodes, by episode id.
func (s *Service) EpisodeStates(ctx context.Context, seriesID int64) (map[int64]ItemState, error) {
	s.init()
	rows, err := s.DB.QueryContext(ctx, `SELECT e.id, MAX(v.id) FROM vod_episodes e JOIN vod_series m ON m.id=e.series_id
		JOIN vod_saves v ON v.kind='episode' AND v.source_id=m.source_id AND v.source_key=e.source_key
		WHERE e.series_id=? GROUP BY e.id`, seriesID)
	if err != nil {
		return nil, err
	}
	latest := map[int64]int64{}
	for rows.Next() {
		var eid, sid int64
		rows.Scan(&eid, &sid)
		latest[eid] = sid
	}
	rows.Close()
	out := map[int64]ItemState{}
	for eid, sid := range latest {
		if v, err := s.one(ctx, sid); err == nil {
			out[eid] = s.itemState(v)
		}
	}
	return out, nil
}

// Configured reports which kinds can be saved (their folder is set).
func (s *Service) Configured() (movies, shows bool) {
	c := s.cfg()
	return c.MoviesFolder != "", c.ShowsFolder != ""
}

// libraryFor is the library whose folder holds path (the deepest one).
func libraryFor(libs []library.Library, path string) (library.Library, bool) {
	var best library.Library
	bestLen := -1
	for _, l := range libs {
		for _, p := range l.Paths {
			p = filepath.Clean(p)
			if (path == p || strings.HasPrefix(path, p+string(filepath.Separator))) && len(p) > bestLen {
				best, bestLen = l, len(p)
			}
		}
	}
	return best, bestLen >= 0
}
