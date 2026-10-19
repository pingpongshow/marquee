package vodsave

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"marquee/internal/scanner/naming"
	"marquee/internal/settings"
)

// Why a running save was stopped (context.Cause).
var (
	errCancelled   = errors.New("cancelled")
	errUserPaused  = errors.New("paused")
	errQueuePaused = errors.New("queue paused")
	errAutoPaused  = errors.New("paused while watching")
	errYielded     = errors.New("connection given to a viewer")
	errShutdown    = errors.New("server stopping")
)

// netError is a network failure: retried with backoff, or the source is down.
type netError struct{ err error }

func (e netError) Error() string { return e.err.Error() }

// permError fails the save; retryable ones are queued again by the background retry.
type permError struct {
	err       error
	retryable bool
}

func (e permError) Error() string { return e.err.Error() }

// verifyError is a file that didn't pass verification: downloaded again once, then failed.
type verifyError struct{ err error }

func (e verifyError) Error() string { return e.err.Error() }

// skipError: the file appeared in the library meanwhile.
type skipError struct{ msg string }

func (e skipError) Error() string { return e.msg }

// job is a running save, with its live progress.
type job struct {
	id       int64
	sourceID string
	cancel   context.CancelCauseFunc
	seq      int64 // start order (the newest give connections back first)
	// budget: the source's free connections decide how many this save uses (budget.go).
	budget bool
	wantCh chan struct{} // told when want changes

	mu         sync.Mutex
	want       int  // connections it may use (budget.go)
	conns      int  // connections it has open
	netDone    bool // the download is over (converting or verifying needs no connection)
	yielding   bool // stopping to give its connection back
	split      bool // a segmented download, which can use more connections
	step       string
	done       int64
	total      int64
	progress   float64
	speed      float64
	lastBytes  int64
	lastSample time.Time
}

// held is the connections the save counts as using: what it may use while downloading.
func (j *job) held() int {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.netDone || j.yielding {
		return 0
	}
	return j.want
}

// open is the connections it has open now.
func (j *job) open() int {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.conns
}

func (j *job) wantConns() int {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.want
}

func (j *job) setWant(n int) {
	j.mu.Lock()
	j.want = n
	j.mu.Unlock()
	select {
	case j.wantCh <- struct{}{}:
	default:
	}
}

func (j *job) addConns(d int) {
	j.mu.Lock()
	j.conns += d
	j.mu.Unlock()
}

func (j *job) setSplit(v bool) {
	j.mu.Lock()
	j.split = v
	j.mu.Unlock()
}

func (j *job) splittable() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.split && !j.netDone && !j.yielding
}

// yield stops the save to give its connections back; it goes back to the queue.
func (j *job) yield() {
	j.mu.Lock()
	j.yielding = true
	j.mu.Unlock()
	j.cancel(errYielded)
}

func (j *job) isYielding() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.yielding
}

// downloaded marks the download over: it holds no connection any more.
func (j *job) downloaded() {
	j.mu.Lock()
	j.netDone, j.conns, j.split = true, 0, false
	j.mu.Unlock()
}

func (j *job) setStep(step string) {
	j.mu.Lock()
	j.step, j.progress = step, 0
	j.mu.Unlock()
}

func (j *job) setBytes(done, total int64) {
	j.mu.Lock()
	j.done, j.total, j.lastBytes, j.lastSample = done, total, done, time.Now()
	j.mu.Unlock()
}

func (j *job) add(n int64) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.done += n
	if dt := time.Since(j.lastSample).Seconds(); dt >= 1 {
		inst := float64(j.done-j.lastBytes) / dt
		if j.speed == 0 {
			j.speed = inst
		} else {
			j.speed = 0.7*j.speed + 0.3*inst
		}
		j.lastBytes, j.lastSample = j.done, time.Now()
	}
}

func (j *job) setProgress(p float64) {
	j.mu.Lock()
	j.progress = p
	j.mu.Unlock()
}

// fill copies the live state onto a listed save.
func (j *job) fill(v *Save) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if v.State != Downloading && v.State != Processing && v.State != Verifying {
		return
	}
	if j.step != "" {
		v.State = j.step
	}
	v.BytesDone = max(v.BytesDone, j.done)
	if j.total > 0 {
		v.BytesTotal = j.total
	}
	v.Progress = j.progress
	if j.step == Downloading {
		v.Connections = j.conns
		v.SpeedBps = j.speed
		if j.speed > 0 && v.BytesTotal > v.BytesDone {
			v.ETASeconds = int64(float64(v.BytesTotal-v.BytesDone) / j.speed)
		}
	}
}

// overall is how far the save is, 0–1, for "Saving 42%".
func (j *job) overall() float64 {
	j.mu.Lock()
	defer j.mu.Unlock()
	switch j.step {
	case Processing:
		return j.progress
	case Verifying:
		return 1
	}
	if j.total > 0 {
		return float64(j.done) / float64(j.total)
	}
	return 0
}

func (s *Service) tick() time.Duration       { return or(s.Tick, 2*time.Second) }
func (s *Service) grace() time.Duration      { return or(s.Grace, 15*time.Second) }
func (s *Service) scanEvery() time.Duration  { return or(s.ScanEvery, 15*time.Minute) }
func (s *Service) retryEvery() time.Duration { return or(s.RetryFailedEvery, 4*time.Hour) }
func (s *Service) downWait() time.Duration   { return or(s.SourceDownWait, time.Minute) }

func (s *Service) backoff() []time.Duration {
	if len(s.Backoff) > 0 {
		return s.Backoff
	}
	return []time.Duration{time.Minute, 3 * time.Minute, 8 * time.Minute, 18 * time.Minute}
}

func or(d, def time.Duration) time.Duration {
	if d > 0 {
		return d
	}
	return def
}

// Run works through the queue until ctx ends. Saves interrupted by a restart start again
// where they were.
func (s *Service) Run(ctx context.Context) {
	s.init()
	if _, err := s.DB.ExecContext(ctx, "UPDATE vod_saves SET state=? WHERE state IN (?,?,?)", append([]any{Queued}, runningStates...)...); err != nil {
		slog.Warn("vod saves", "err", err)
	}
	go s.runQuality(ctx)
	go s.runFollows(ctx)
	t := time.NewTicker(s.tick())
	defer t.Stop()
	for {
		s.step(ctx)
		select {
		case <-ctx.Done():
			s.stopJobs(func(*job) bool { return true }, errShutdown)
			return
		case <-t.C:
		case <-s.wake:
		}
	}
}

// stopJobs stops the running saves match picks, reporting whether any was.
func (s *Service) stopJobs(match func(*job) bool, why error) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	found := false
	for _, j := range s.jobs {
		if match(j) {
			j.cancel(why)
			found = true
		}
	}
	return found
}

func (s *Service) step(ctx context.Context) {
	c := s.cfg()
	// Failed saves get another go every few hours.
	s.mu.Lock()
	retry := c.RetryFailed && time.Since(s.lastRetry) >= s.retryEvery()
	if retry {
		s.lastRetry = time.Now()
	}
	s.mu.Unlock()
	if retry {
		if r, err := s.DB.ExecContext(ctx, `UPDATE vod_saves SET state=?, attempts=0, verify_tries=0, next_attempt_at=NULL, finished_at=NULL
			WHERE state=? AND retryable=1`, Queued, Failed); err == nil {
			if n, _ := r.RowsAffected(); n > 0 {
				slog.Info("vod saves: retrying failed saves", "count", n)
			}
		}
	}

	sources := s.pendingSources(ctx)
	var playing []Playing
	if c.PauseWhileWatching && s.Activity != nil && len(sources) > 0 {
		playing = s.Activity(ctx)
	}
	// Sources whose provider's connection limit is known share its free connections.
	budgets := map[string]Budget{}
	if s.Connections != nil && s.VOD != nil && len(sources) > 0 {
		live := s.Connections()
		for _, src := range sources {
			if b := ConnectionBudget(src, s.VOD.SourceHost(src), live, playing); budgeted(c, b) {
				budgets[src] = b
			}
		}
	}
	s.autoPause(ctx, c, sources, playing, budgets)
	queuePaused := s.QueuePaused(ctx)
	// Outside the maintenance window (when downloads are limited to it), running saves stop
	// and keep their partial files, like a paused queue.
	if s.outsideWindow() {
		s.stopJobs(func(*job) bool { return true }, errQueuePaused)
		queuePaused = true
	}
	s.pruneHistory(ctx)

	for _, src := range sources {
		b, ok := budgets[src]
		s.mu.Lock()
		if !ok {
			delete(s.waitReason, src)
			delete(s.caps, src)
			s.mu.Unlock()
			continue
		}
		full := b.allowed(false)
		cap := s.connCap(src, b.allowed(c.PauseWhileWatching), full)
		spare, running := s.shrink(src, cap)
		s.waitReason[src] = ""
		if spare == 0 && running < maxConnections {
			switch {
			case cap < full:
				s.waitReason[src] = "Waiting: connections in use by viewers"
			case full < min(b.Limit, maxConnections):
				s.waitReason[src] = "Waiting: connections in use by recordings"
			}
		}
		down := time.Now().Before(s.down[src].until)
		s.mu.Unlock()
		if queuePaused || down {
			continue
		}
		started := 0
		if n := min(spare, maxConnections-running); n > 0 {
			for _, v := range s.nextQueued(ctx, src, n) {
				s.start(ctx, v, true)
				started++
			}
		}
		// Connections still free go to large downloads, as more segments.
		s.mu.Lock()
		s.grow(src, spare-started)
		s.mu.Unlock()
	}

	if !queuePaused {
		for _, src := range sources {
			if _, ok := budgets[src]; ok {
				continue
			}
			s.mu.Lock()
			blocked := s.pauseReason[src] != "" || time.Now().Before(s.down[src].until)
			running := 0
			for _, j := range s.jobs {
				if j.sourceID == src {
					running++
				}
			}
			s.mu.Unlock()
			if blocked || running >= c.ConcurrentPerSource {
				continue
			}
			for _, v := range s.nextQueued(ctx, src, c.ConcurrentPerSource-running) {
				s.start(ctx, v, false)
			}
		}
	}
	s.checkDrained(ctx)
	s.scanPending(ctx)
}

// nextQueued are the next n saves of a source waiting to run.
func (s *Service) nextQueued(ctx context.Context, src string, n int) []Save {
	rows, err := s.DB.QueryContext(ctx, "SELECT "+saveCols+` FROM vod_saves WHERE state=? AND source_id=?
		AND (next_attempt_at IS NULL OR next_attempt_at<=?) ORDER BY queue_pos, id LIMIT ?`, Queued, src, now(), n)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var next []Save
	for rows.Next() {
		if v, err := scanSave(rows); err == nil {
			next = append(next, v)
		}
	}
	return next
}

// pendingSources are the sources with saves waiting or running.
func (s *Service) pendingSources(ctx context.Context) []string {
	rows, err := s.DB.QueryContext(ctx, "SELECT DISTINCT source_id FROM vod_saves WHERE state IN (?,?,?,?) ORDER BY source_id",
		append([]any{Queued}, runningStates...)...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		rows.Scan(&id)
		out = append(out, id)
	}
	return out
}

// autoPause pauses the saves of sources something is being watched from (or of every
// source, with scope "any"), and resumes them once nothing has been for the grace period.
// Sources with a connection budget don't pause: they give back connections instead.
func (s *Service) autoPause(ctx context.Context, c settings.VODSave, sources []string, playing []Playing, budgets map[string]Budget) {
	t := time.Now()
	var stop []string
	for _, src := range sources {
		if _, ok := budgets[src]; ok {
			s.mu.Lock()
			delete(s.pauseReason, src)
			delete(s.lastActive, src)
			s.mu.Unlock()
			continue
		}
		host := ""
		if s.VOD != nil {
			host = s.VOD.SourceHost(src)
		}
		reason := ""
		if c.PauseWhileWatching {
			reason = pauseReasonFor(c.PauseScope, src, host, playing)
		}
		s.mu.Lock()
		switch last, ok := s.lastActive[src]; {
		case reason != "":
			if s.pauseReason[src] == "" {
				stop = append(stop, src)
				slog.Info("vod saves paused while watching", "source", src, "reason", reason)
			}
			s.lastActive[src], s.pauseReason[src] = t, reason
		case ok && c.PauseWhileWatching && t.Sub(last) < s.grace():
			// Recently watched: wait out the grace period (channel flipping doesn't thrash).
		default:
			if s.pauseReason[src] != "" {
				slog.Info("vod saves resumed", "source", src)
			}
			delete(s.pauseReason, src)
			delete(s.lastActive, src)
		}
		s.mu.Unlock()
	}
	for _, src := range stop {
		s.stopJobs(func(j *job) bool { return j.sourceID == src }, errAutoPaused)
	}
}

// pauseReasonFor says why a source's saves should pause for what's playing ("" = they needn't).
func pauseReasonFor(scope, sourceID, host string, playing []Playing) string {
	for _, p := range playing {
		if scope == "any" {
			switch p.Kind {
			case "live":
				return "Paused while Live TV is playing"
			case "vod":
				return "Paused while VOD is playing"
			default:
				return "Paused while something is playing"
			}
		}
		switch p.Kind {
		case "vod":
			if p.SourceID == sourceID || (host != "" && strings.EqualFold(p.Host, host)) {
				return "Paused while something from this source is playing"
			}
		case "live":
			if host != "" && strings.EqualFold(p.Host, host) {
				return "Paused while Live TV is playing"
			}
		}
	}
	return ""
}

// HostOf is a link's host name ("" when it has none).
func HostOf(link string) string {
	u, err := url.Parse(link)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

func (s *Service) start(ctx context.Context, v Save, budget bool) {
	jctx, cancel := context.WithCancelCause(ctx)
	j := &job{id: v.ID, sourceID: v.SourceID, cancel: cancel, step: Downloading, done: v.BytesDone, total: v.BytesTotal,
		budget: budget, want: 1, wantCh: make(chan struct{}, 1)}
	s.mu.Lock()
	s.seq++
	j.seq = s.seq
	s.jobs[v.ID] = j
	s.mu.Unlock()
	if _, err := s.DB.ExecContext(ctx, "UPDATE vod_saves SET state=?, started_at=COALESCE(started_at, ?), next_attempt_at=NULL WHERE id=?",
		Downloading, now(), v.ID); err != nil {
		slog.Warn("vod saves", "err", err)
	}
	go func() {
		res, err := s.run(jctx, j, v)
		s.finish(context.WithoutCancel(ctx), jctx, j, v, res, err)
	}()
}

// outcome is a finished save.
type outcome struct {
	path, note, sha string
	bytes           int64
	replaced        string // an upgrade: where the library's copy it replaced was
}

func (s *Service) setState(ctx context.Context, id int64, state string) {
	s.DB.ExecContext(ctx, "UPDATE vod_saves SET state=? WHERE id=? AND state IN (?,?,?)", append([]any{state, id}, runningStates...)...)
}

func (s *Service) finish(ctx context.Context, jctx context.Context, j *job, v Save, res outcome, err error) {
	defer func() {
		s.mu.Lock()
		delete(s.jobs, v.ID)
		s.mu.Unlock()
		s.Wake()
		s.wakeFollows() // a followed series may queue its next episode
	}()
	j.mu.Lock()
	done := j.done
	j.mu.Unlock()
	redact := func(msg string) string {
		if s.VOD != nil {
			return s.VOD.Redact(v.SourceID, msg)
		}
		return msg
	}
	if err == nil {
		s.DB.ExecContext(ctx, `UPDATE vod_saves SET state=?, target_path=?, verified=1, verify_note=?, sha256=?, bytes_done=?,
			bytes_total=CASE WHEN bytes_total=0 THEN ? ELSE bytes_total END, error='', finished_at=?, replaced_path=? WHERE id=?`,
			Done, res.path, res.note, res.sha, res.bytes, res.bytes, now(), res.replaced, v.ID)
		slog.Info("vod save done", "title", v.Title, "path", res.path, "replaced", res.replaced)
		s.followSaved(ctx, v.ID, res.path, res.bytes)
		s.mu.Lock()
		s.drain.done++
		if len(s.drain.titles) < 20 {
			s.drain.titles = append(s.drain.titles, v.Title)
		}
		s.mu.Unlock()
		s.saved(ctx, res.path, res.replaced != "")
		return
	}
	switch cause := context.Cause(jctx); {
	case errors.Is(cause, errCancelled):
		removeParts(v.TargetPath)
		return
	case errors.Is(cause, errUserPaused):
		s.DB.ExecContext(ctx, "UPDATE vod_saves SET bytes_done=? WHERE id=?", done, v.ID)
		return
	case errors.Is(cause, errQueuePaused), errors.Is(cause, errAutoPaused), errors.Is(cause, errYielded), errors.Is(cause, errShutdown):
		s.DB.ExecContext(ctx, "UPDATE vod_saves SET bytes_done=? WHERE id=?", done, v.ID)
		s.setState(ctx, v.ID, Queued)
		return
	}
	msg := redact(err.Error())
	var ne netError
	var ve verifyError
	var se skipError
	var pe permError
	switch {
	case errors.As(err, &se):
		removeParts(v.TargetPath)
		s.DB.ExecContext(ctx, "UPDATE vod_saves SET state=?, error=?, finished_at=? WHERE id=?", Skipped, se.msg, now(), v.ID)
	case errors.As(err, &ve):
		removeParts(v.TargetPath)
		var tries int
		s.DB.QueryRowContext(ctx, "SELECT verify_tries FROM vod_saves WHERE id=?", v.ID).Scan(&tries)
		if tries == 0 {
			slog.Warn("vod save didn't verify; downloading again", "title", v.Title, "err", msg)
			s.DB.ExecContext(ctx, `UPDATE vod_saves SET verify_tries=1, bytes_done=0, error=? WHERE id=?`,
				"Verification failed ("+msg+"); downloading again", v.ID)
			s.setState(ctx, v.ID, Queued)
			return
		}
		s.fail(ctx, v, "Verification failed: "+msg, false)
	case errors.As(err, &ne):
		if s.VOD != nil {
			if cerr := s.VOD.CheckSource(ctx, v.SourceID); cerr != nil {
				// The source is down, not this file: wait for it without using up a try.
				s.markDown(v.SourceID, redact(cerr.Error()))
				s.DB.ExecContext(ctx, "UPDATE vod_saves SET bytes_done=?, error=? WHERE id=?", done, "Source unreachable: "+redact(cerr.Error()), v.ID)
				s.setState(ctx, v.ID, Queued)
				return
			}
		}
		var attempts int
		s.DB.QueryRowContext(ctx, "UPDATE vod_saves SET attempts=attempts+1, bytes_done=? WHERE id=? RETURNING attempts", done, v.ID).Scan(&attempts)
		b := s.backoff()
		if attempts > len(b) {
			s.fail(ctx, v, fmt.Sprintf("Network error after %d tries: %s", attempts, msg), true)
			return
		}
		at := time.Now().Add(b[attempts-1])
		s.DB.ExecContext(ctx, "UPDATE vod_saves SET next_attempt_at=?, error=? WHERE id=?", at.UTC().Format(time.RFC3339),
			fmt.Sprintf("Network error (%s); try %d of %d at %s", msg, attempts+1, len(b)+1, at.Local().Format("15:04")), v.ID)
		s.setState(ctx, v.ID, Queued)
	case errors.As(err, &pe):
		s.fail(ctx, v, msg, pe.retryable)
	default:
		s.fail(ctx, v, msg, false)
	}
}

// fail marks a save failed; its partial download stays, so a retry continues from it.
func (s *Service) fail(ctx context.Context, v Save, msg string, retryable bool) {
	slog.Warn("vod save failed", "title", v.Title, "err", msg)
	s.DB.ExecContext(ctx, "UPDATE vod_saves SET state=?, error=?, retryable=?, finished_at=? WHERE id=?", Failed, msg, retryable, now(), v.ID)
	s.mu.Lock()
	s.drain.failed++
	s.mu.Unlock()
	if s.OnFailed != nil {
		v.State, v.Error = Failed, msg
		s.OnFailed(v)
	}
}

func (s *Service) markDown(src, msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.down[src]
	if d.wait == 0 {
		d.wait = s.downWait()
	} else {
		d.wait = min(d.wait*2, 30*time.Minute)
	}
	d.until, d.err = time.Now().Add(d.wait), msg
	s.down[src] = d
	slog.Warn("vod saves: source unreachable, waiting", "source", src, "for", d.wait, "err", msg)
}

func (s *Service) sourceUp(src string) {
	s.mu.Lock()
	delete(s.down, src)
	s.mu.Unlock()
}

// checkDrained tells about the saves made once nothing is left to do.
func (s *Service) checkDrained(ctx context.Context) {
	s.mu.Lock()
	idle := len(s.jobs) == 0
	d := s.drain
	s.mu.Unlock()
	if !idle || d.done+d.failed == 0 {
		return
	}
	var n int
	s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM vod_saves WHERE state IN (?,?,?,?)", append([]any{Queued}, runningStates...)...).Scan(&n)
	if n > 0 {
		return
	}
	s.mu.Lock()
	s.drain = drainCount{}
	s.mu.Unlock()
	if s.OnDrained != nil {
		s.OnDrained(d.done, d.failed, d.titles)
	}
}

// saved scans the library a file went into: now when nothing else is on its way there, or
// at most every ScanEvery while more is. An upgrade's library is always scanned (its item
// points at the new file, which the scan reads).
func (s *Service) saved(ctx context.Context, path string, upgrade bool) {
	if (!s.cfg().ScanAfterSave && !upgrade) || s.Libraries == nil || s.Scan == nil {
		return
	}
	libs, err := s.Libraries(ctx)
	if err != nil {
		return
	}
	l, ok := libraryFor(libs, path)
	if !ok {
		slog.Info("vod save: no library holds the folder, so nothing is scanned", "path", path)
		return
	}
	s.mu.Lock()
	s.pendingScan[l.ID] = true
	s.mu.Unlock()
	s.scanPending(ctx)
}

func (s *Service) scanPending(ctx context.Context) {
	s.mu.Lock()
	if len(s.pendingScan) == 0 {
		s.mu.Unlock()
		return
	}
	s.mu.Unlock()
	libs, err := s.Libraries(ctx)
	if err != nil {
		return
	}
	busy := map[int64]bool{}
	rows, err := s.DB.QueryContext(ctx, "SELECT target_path FROM vod_saves WHERE state IN (?,?,?,?)", append([]any{Queued}, runningStates...)...)
	if err == nil {
		for rows.Next() {
			var p string
			rows.Scan(&p)
			if l, ok := libraryFor(libs, p); ok {
				busy[l.ID] = true
			}
		}
		rows.Close()
	}
	var scan []int64
	s.mu.Lock()
	for id := range s.pendingScan {
		if !busy[id] || time.Since(s.lastScan[id]) >= s.scanEvery() {
			scan = append(scan, id)
			delete(s.pendingScan, id)
			s.lastScan[id] = time.Now()
		}
	}
	s.mu.Unlock()
	for _, id := range scan {
		slog.Info("vod saves: scanning library", "library", id)
		s.Scan(id)
	}
}

// removeParts deletes a save's partial files (and a segmented download's state).
func removeParts(target string) {
	for _, p := range []string{target + ".part", target + ".source.part"} {
		os.Remove(p)
		os.Remove(segsPath(p))
	}
}

// mkdirs makes a folder and any missing parents, group-writable (the server runs as 99:100).
func mkdirs(dir string) error {
	if st, err := os.Stat(dir); err == nil {
		if !st.IsDir() {
			return fmt.Errorf("%s isn't a folder", dir)
		}
		return nil
	}
	if err := mkdirs(filepath.Dir(dir)); err != nil {
		return err
	}
	if err := os.Mkdir(dir, 0o775); err != nil && !os.IsExist(err) {
		return err
	}
	return os.Chmod(dir, 0o775)
}

// episodeOf reads the season and episode back from an episode's file name.
func episodeOf(path string) (int, int) {
	if ep, ok := naming.ParseEpisode(filepath.Base(path)); ok && len(ep.Episodes) > 0 {
		return ep.Season, ep.Episodes[0]
	}
	return -1, -1
}
