package livetv

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DVR (LIVE-5, D76): series rules and one-off recordings of guide programmes. Recordings copy
// the channel's stream (no transcoding) into the recordings folder, laid out so the scanner
// files them as shows or movies in the "Recorded TV" and "Recorded Movies" libraries.

var (
	ErrRecordingNotFound = errors.New("recording not found")
	ErrProgrammeNotFound = errors.New("that programme isn't in the guide")
	ErrDVRUnavailable    = errors.New("recording isn't available: the recordings folder can't be written")
)

// Recording statuses.
const (
	StatusScheduled = "scheduled"
	StatusRecording = "recording"
	StatusCompleted = "completed"
	StatusFailed    = "failed"
	StatusCancelled = "cancelled"
)

// Recorder schedules and makes recordings.
type Recorder struct {
	Live   *Service
	FFmpeg string
	Dir    string // the recordings folder
	// Padding before and after each programme.
	Before, After func() time.Duration
	// Library returns the library for recorded shows or movies at dir, creating it if needed.
	Library func(ctx context.Context, movies bool, dir string) (int64, error)
	// Scan rescans a library after a recording lands in it.
	Scan func(libraryID int64)
	// Now is the clock (tests).
	Now func() time.Time

	mu     sync.Mutex
	active map[int64]context.CancelFunc
	wake   chan struct{}
	wg     sync.WaitGroup
}

// Recording is one scheduled, running or finished recording.
type Recording struct {
	ID, UserID                                                     int64
	RuleID, ChannelID                                              *int64
	ChannelName                                                    string
	Start, Stop                                                    time.Time
	Title, Subtitle, Description, Category, Episode, Image, Status string
	Path, Error                                                    string
	Size                                                           int64
	ItemID                                                         *int64
}

// Rule records every airing of a title.
type Rule struct {
	ID, UserID  int64
	Title       string
	ChannelID   *int64
	ChannelName string
	CreatedAt   time.Time
	Upcoming    int
}

func (r *Recorder) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *Recorder) pad() (time.Duration, time.Duration) {
	var b, a time.Duration
	if r.Before != nil {
		b = r.Before()
	}
	if r.After != nil {
		a = r.After()
	}
	return b, a
}

// Available reports whether recordings can be written.
func (r *Recorder) Available() bool {
	if r == nil || r.Dir == "" {
		return false
	}
	if err := os.MkdirAll(r.Dir, 0o755); err != nil {
		return false
	}
	f, err := os.CreateTemp(r.Dir, ".probe-*")
	if err != nil {
		return false
	}
	f.Close()
	os.Remove(f.Name())
	return true
}

// Wake makes the scheduler look again now.
func (r *Recorder) Wake() {
	if r.wake == nil {
		return
	}
	select {
	case r.wake <- struct{}{}:
	default:
	}
}

// Run schedules and starts recordings until ctx ends.
func (r *Recorder) Run(ctx context.Context) {
	r.mu.Lock()
	if r.wake == nil {
		r.wake = make(chan struct{}, 1)
	}
	r.active = map[int64]context.CancelFunc{}
	r.mu.Unlock()
	// Recordings cut short by a restart carry on (in a new part) if their programme hasn't ended.
	r.Live.DB.ExecContext(ctx, `UPDATE dvr_recordings SET status = 'scheduled' WHERE status = 'recording'`)
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		r.Tick(ctx)
		select {
		case <-ctx.Done():
			r.wg.Wait()
			return
		case <-t.C:
		case <-r.wake:
		}
	}
}

// Tick expands series rules, starts recordings that are due and marks missed ones.
func (r *Recorder) Tick(ctx context.Context) {
	if err := r.expandRules(ctx); err != nil {
		slog.Warn("dvr: series rules", "err", err)
	}
	before, after := r.pad()
	now := r.now().UTC()
	rows, err := r.Live.DB.QueryContext(ctx, `SELECT id, start, stop FROM dvr_recordings WHERE status = 'scheduled' AND start <= ?`,
		now.Add(before).Format(time.RFC3339))
	if err != nil {
		slog.Warn("dvr: due recordings", "err", err)
		return
	}
	type due struct {
		id          int64
		start, stop time.Time
	}
	var list []due
	for rows.Next() {
		var d due
		var s, e string
		if rows.Scan(&d.id, &s, &e) == nil {
			d.start, _ = time.Parse(time.RFC3339, s)
			d.stop, _ = time.Parse(time.RFC3339, e)
			list = append(list, d)
		}
	}
	rows.Close()
	for _, d := range list {
		end := d.stop.Add(after)
		if !end.After(now) {
			r.finish(ctx, d.id, nil, "missed: the server wasn't running")
			continue
		}
		r.start(ctx, d.id, end)
	}
}

// expandRules schedules every guide airing that matches a series rule.
func (r *Recorder) expandRules(ctx context.Context) error {
	now := r.now().UTC().Format(time.RFC3339)
	rows, err := r.Live.DB.QueryContext(ctx, `SELECT d.id, d.user_id, p.channel_id, c.name, p.start, p.stop, p.title, p.subtitle, p.description, p.category, p.episode, p.image_url
		FROM dvr_rules d
		JOIN live_programmes p ON lower(p.title) = lower(d.title) AND (d.channel_id IS NULL OR d.channel_id = p.channel_id)
		JOIN live_channels c ON c.id = p.channel_id AND c.present = 1
		WHERE p.stop > ?
		  AND NOT EXISTS (SELECT 1 FROM dvr_recordings x WHERE x.channel_id = p.channel_id AND x.start = p.start)
		ORDER BY p.start`, now)
	if err != nil {
		return err
	}
	type cand struct {
		rule, user, channel                               int64
		channelName, start, stop                          string
		title, subtitle, description, category, ep, image string
	}
	var list []cand
	for rows.Next() {
		var c cand
		if err := rows.Scan(&c.rule, &c.user, &c.channel, &c.channelName, &c.start, &c.stop, &c.title, &c.subtitle, &c.description, &c.category, &c.ep, &c.image); err != nil {
			rows.Close()
			return err
		}
		list = append(list, c)
	}
	rows.Close()
	for _, c := range list {
		// The same episode on another channel or a repeat later on is recorded once.
		if c.ep != "" || c.subtitle != "" {
			var n int
			r.Live.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM dvr_recordings WHERE lower(title) = lower(?) AND status IN ('scheduled', 'recording', 'completed')
				AND ((? <> '' AND episode = ?) OR (? = '' AND ? <> '' AND subtitle = ?))`,
				c.title, c.ep, c.ep, c.ep, c.subtitle, c.subtitle).Scan(&n)
			if n > 0 {
				continue
			}
		}
		if _, err := r.Live.DB.ExecContext(ctx, `INSERT OR IGNORE INTO dvr_recordings (rule_id, user_id, channel_id, channel_name, start, stop, title, subtitle, description, category, episode, image_url, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			c.rule, c.user, c.channel, c.channelName, c.start, c.stop, c.title, c.subtitle, c.description, c.category, c.ep, c.image, now); err != nil {
			return err
		}
	}
	return nil
}

// Schedule records the programme on channelID starting at start, or every airing of its
// title when series is set (on any channel when anyChannel is also set).
func (r *Recorder) Schedule(ctx context.Context, userID, channelID int64, start time.Time, series, anyChannel bool) (rec *Recording, rule *Rule, err error) {
	var title, chName string
	err = r.Live.DB.QueryRowContext(ctx, `SELECT p.title, c.name FROM live_programmes p JOIN live_channels c ON c.id = p.channel_id WHERE p.channel_id = ? AND p.start = ?`,
		channelID, start.UTC().Format(time.RFC3339)).Scan(&title, &chName)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrProgrammeNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	now := r.now().UTC().Format(time.RFC3339)
	if series {
		var ch any = channelID
		if anyChannel {
			ch = nil
		}
		var id int64
		err := r.Live.DB.QueryRowContext(ctx, `SELECT id FROM dvr_rules WHERE lower(title) = lower(?) AND channel_id IS ?`, title, ch).Scan(&id)
		if errors.Is(err, sql.ErrNoRows) {
			res, e := r.Live.DB.ExecContext(ctx, `INSERT INTO dvr_rules (user_id, title, channel_id, created_at) VALUES (?, ?, ?, ?)`, userID, title, ch, now)
			if e != nil {
				return nil, nil, e
			}
			id, _ = res.LastInsertId()
		} else if err != nil {
			return nil, nil, err
		}
		if err := r.expandRules(ctx); err != nil {
			return nil, nil, err
		}
		r.Wake()
		rules, err := r.Rules(ctx)
		if err != nil {
			return nil, nil, err
		}
		for i := range rules {
			if rules[i].ID == id {
				return nil, &rules[i], nil
			}
		}
		return nil, nil, ErrRecordingNotFound
	}
	// A one-off: a programme cancelled earlier can be scheduled again.
	_, err = r.Live.DB.ExecContext(ctx, `INSERT INTO dvr_recordings (user_id, channel_id, channel_name, start, stop, title, subtitle, description, category, episode, image_url, created_at)
		SELECT ?, p.channel_id, ?, p.start, p.stop, p.title, p.subtitle, p.description, p.category, p.episode, p.image_url, ?
		FROM live_programmes p WHERE p.channel_id = ? AND p.start = ?
		ON CONFLICT (channel_id, start) DO UPDATE SET status = 'scheduled', error = '' WHERE status IN ('cancelled', 'failed')`,
		userID, chName, now, channelID, start.UTC().Format(time.RFC3339))
	if err != nil {
		return nil, nil, err
	}
	var id int64
	if err := r.Live.DB.QueryRowContext(ctx, `SELECT id FROM dvr_recordings WHERE channel_id = ? AND start = ?`, channelID, start.UTC().Format(time.RFC3339)).Scan(&id); err != nil {
		return nil, nil, err
	}
	r.Wake()
	got, err := r.Get(ctx, id)
	return &got, nil, err
}

const recCols = `r.id, r.user_id, r.rule_id, r.channel_id, r.channel_name, r.start, r.stop, r.title, r.subtitle, r.description, r.category, r.episode,
	r.image_url, r.status, r.path, r.error, r.size,
	(SELECT v.item_id FROM media_files f JOIN media_versions v ON v.id = f.version_id WHERE r.path <> '' AND f.path = r.path)`

func scanRec(row interface{ Scan(...any) error }) (Recording, error) {
	var x Recording
	var start, stop string
	err := row.Scan(&x.ID, &x.UserID, &x.RuleID, &x.ChannelID, &x.ChannelName, &start, &stop, &x.Title, &x.Subtitle, &x.Description, &x.Category,
		&x.Episode, &x.Image, &x.Status, &x.Path, &x.Error, &x.Size, &x.ItemID)
	x.Start, _ = time.Parse(time.RFC3339, start)
	x.Stop, _ = time.Parse(time.RFC3339, stop)
	return x, err
}

// Get looks one recording up.
func (r *Recorder) Get(ctx context.Context, id int64) (Recording, error) {
	x, err := scanRec(r.Live.DB.QueryRowContext(ctx, `SELECT `+recCols+` FROM dvr_recordings r WHERE r.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return x, ErrRecordingNotFound
	}
	return x, err
}

// List returns recordings now running, then upcoming ones (soonest first), then finished
// ones (newest first, at most 200). Cancelled ones aren't listed.
func (r *Recorder) List(ctx context.Context) ([]Recording, error) {
	rows, err := r.Live.DB.QueryContext(ctx, `SELECT `+recCols+` FROM dvr_recordings r WHERE r.status <> 'cancelled'
		ORDER BY CASE r.status WHEN 'recording' THEN 0 WHEN 'scheduled' THEN 1 ELSE 2 END,
		CASE WHEN r.status IN ('recording', 'scheduled') THEN r.start END ASC, r.start DESC LIMIT 500`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Recording
	finished := 0
	for rows.Next() {
		x, err := scanRec(rows)
		if err != nil {
			return nil, err
		}
		if x.Status == StatusCompleted || x.Status == StatusFailed {
			if finished++; finished > 200 {
				continue
			}
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// Rules lists series rules with how many airings are scheduled.
func (r *Recorder) Rules(ctx context.Context) ([]Rule, error) {
	rows, err := r.Live.DB.QueryContext(ctx, `SELECT d.id, d.user_id, d.title, d.channel_id, COALESCE(c.name, ''), d.created_at,
		(SELECT COUNT(*) FROM dvr_recordings x WHERE x.rule_id = d.id AND x.status IN ('scheduled', 'recording'))
		FROM dvr_rules d LEFT JOIN live_channels c ON c.id = d.channel_id ORDER BY lower(d.title)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Rule
	for rows.Next() {
		var x Rule
		var created string
		if err := rows.Scan(&x.ID, &x.UserID, &x.Title, &x.ChannelID, &x.ChannelName, &created, &x.Upcoming); err != nil {
			return nil, err
		}
		x.CreatedAt, _ = time.Parse(time.RFC3339, created)
		out = append(out, x)
	}
	return out, rows.Err()
}

// RuleOwner returns who made a rule.
func (r *Recorder) RuleOwner(ctx context.Context, id int64) (int64, error) {
	var uid int64
	err := r.Live.DB.QueryRowContext(ctx, `SELECT user_id FROM dvr_rules WHERE id = ?`, id).Scan(&uid)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrRecordingNotFound
	}
	return uid, err
}

// DeleteRule stops a series; its upcoming airings are dropped, recordings made are kept.
func (r *Recorder) DeleteRule(ctx context.Context, id int64) error {
	if _, err := r.RuleOwner(ctx, id); err != nil {
		return err
	}
	if _, err := r.Live.DB.ExecContext(ctx, `DELETE FROM dvr_recordings WHERE rule_id = ? AND status IN ('scheduled', 'cancelled')`, id); err != nil {
		return err
	}
	_, err := r.Live.DB.ExecContext(ctx, `DELETE FROM dvr_rules WHERE id = ?`, id)
	return err
}

// Cancel cancels an upcoming recording, stops one in progress (keeping what was recorded),
// or removes a finished one from the list, deleting its file too when deleteFile is set.
func (r *Recorder) Cancel(ctx context.Context, id int64, deleteFile bool) error {
	x, err := r.Get(ctx, id)
	if err != nil {
		return err
	}
	switch x.Status {
	case StatusScheduled:
		// Kept as cancelled so a series rule doesn't schedule it again.
		_, err = r.Live.DB.ExecContext(ctx, `UPDATE dvr_recordings SET status = 'cancelled' WHERE id = ? AND status = 'scheduled'`, id)
		return err
	case StatusRecording:
		r.mu.Lock()
		stop := r.active[id]
		r.mu.Unlock()
		if stop != nil {
			stop()
		}
		return nil
	}
	if deleteFile && x.Path != "" && r.inside(x.Path) {
		if err := os.Remove(x.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		os.Remove(filepath.Dir(x.Path)) // only when empty
		r.rescan(ctx, x.Path)
	}
	if x.RuleID != nil {
		// Keep the row (hidden) so the series doesn't record this airing again.
		_, err = r.Live.DB.ExecContext(ctx, `UPDATE dvr_recordings SET status = 'cancelled', path = '' WHERE id = ?`, id)
		return err
	}
	_, err = r.Live.DB.ExecContext(ctx, `DELETE FROM dvr_recordings WHERE id = ?`, id)
	return err
}

func (r *Recorder) inside(p string) bool {
	rel, err := filepath.Rel(r.Dir, p)
	return err == nil && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel)
}

// Active is the number of recordings in progress.
func (r *Recorder) Active() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.active)
}

func (r *Recorder) start(ctx context.Context, id int64, end time.Time) {
	r.mu.Lock()
	if r.active == nil {
		r.active = map[int64]context.CancelFunc{}
	}
	if _, running := r.active[id]; running {
		r.mu.Unlock()
		return
	}
	rctx, cancel := context.WithDeadline(ctx, end)
	r.active[id] = cancel
	r.mu.Unlock()
	r.Live.DB.ExecContext(ctx, `UPDATE dvr_recordings SET status = 'recording', error = '' WHERE id = ?`, id)
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		defer func() {
			r.mu.Lock()
			delete(r.active, id)
			r.mu.Unlock()
			cancel()
		}()
		r.record(rctx, ctx, id)
	}()
}

// record captures the stream into parts until the programme (plus padding) ends or the
// recording is stopped, reconnecting when the source drops, then files the result.
func (r *Recorder) record(rctx, ctx context.Context, id int64) {
	x, err := r.Get(ctx, id)
	if err != nil {
		return
	}
	if x.ChannelID == nil {
		r.finish(ctx, id, nil, "the channel is gone")
		return
	}
	ch, err := r.Live.Channel(ctx, *x.ChannelID)
	if err != nil {
		r.finish(ctx, id, nil, "the channel is gone")
		return
	}
	work := filepath.Join(r.Dir, ".recording", strconv.FormatInt(id, 10))
	if err := os.MkdirAll(work, 0o755); err != nil {
		r.finish(ctx, id, nil, err.Error())
		return
	}
	slog.Info("dvr: recording", "id", id, "title", x.Title, "channel", ch.Name)
	ua := r.Live.UserAgent(ch.SourceID)
	var lastErr string
	for rctx.Err() == nil {
		parts, _ := filepath.Glob(filepath.Join(work, "part-*.ts"))
		out := filepath.Join(work, fmt.Sprintf("part-%03d.ts", len(parts)))
		left := time.Until(deadline(rctx))
		if left < 2*time.Second {
			break
		}
		args := []string{"-hide_banner", "-loglevel", "error", "-nostdin"}
		if ua != "" {
			args = append(args, "-user_agent", ua)
		}
		if strings.HasPrefix(ch.StreamURL, "http") {
			args = append(args, "-reconnect", "1", "-reconnect_streamed", "1", "-reconnect_delay_max", "5", "-rw_timeout", "15000000")
		}
		args = append(args, "-i", ch.StreamURL, "-map", "0:v:0?", "-map", "0:a?", "-c", "copy", "-f", "mpegts",
			"-t", strconv.Itoa(int(left.Seconds())+1), "-y", out)
		cmd := exec.CommandContext(rctx, r.FFmpeg, args...)
		cmd.Cancel = func() error { return cmd.Process.Signal(os.Interrupt) } // lets FFmpeg finish the file
		cmd.WaitDelay = 5 * time.Second
		msg, err := cmd.CombinedOutput()
		if err != nil && rctx.Err() == nil {
			lastErr = strings.TrimSpace(lastLine(string(msg)))
			if lastErr == "" {
				lastErr = err.Error()
			}
			slog.Warn("dvr: stream dropped, retrying", "id", id, "err", lastErr)
		}
		if fi, e := os.Stat(out); e == nil && fi.Size() == 0 {
			os.Remove(out)
		}
		// Wait a little before reconnecting, unless the programme is over.
		select {
		case <-rctx.Done():
		case <-time.After(3 * time.Second):
		}
	}
	if ctx.Err() != nil {
		return // shutting down: the parts stay and recording resumes after the restart
	}
	parts, _ := filepath.Glob(filepath.Join(work, "part-*.ts"))
	sort.Strings(parts)
	if len(parts) == 0 {
		if lastErr == "" {
			lastErr = "the channel didn't send anything"
		}
		os.RemoveAll(work)
		r.finish(ctx, id, nil, lastErr)
		return
	}
	dest, movies := r.destination(x)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		r.finish(ctx, id, nil, err.Error())
		return
	}
	if dest, err = r.join(ctx, parts, dest); err != nil {
		r.finish(ctx, id, nil, err.Error())
		return
	}
	os.RemoveAll(work)
	r.finish(ctx, id, &dest, "")
	if r.Library != nil {
		sub := "TV"
		if movies {
			sub = "Movies"
		}
		if lib, err := r.Library(ctx, movies, filepath.Join(r.Dir, sub)); err != nil {
			slog.Warn("dvr: library", "err", err)
		} else if r.Scan != nil {
			r.Scan(lib)
		}
	}
	slog.Info("dvr: recorded", "id", id, "path", dest)
}

func deadline(ctx context.Context) time.Time {
	if d, ok := ctx.Deadline(); ok {
		return d
	}
	return time.Now().Add(time.Hour)
}

func lastLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		return s[i+1:]
	}
	return s
}

// join remuxes the parts (one or more, stream copy) into a Matroska file, whose index lets
// players seek and the server repackage it without a transcode. If that fails, the longest
// part is kept as MPEG-TS instead; the final path is returned.
func (r *Recorder) join(ctx context.Context, parts []string, dest string) (string, error) {
	in := []string{"-i", parts[0]}
	if len(parts) > 1 {
		list := filepath.Join(filepath.Dir(parts[0]), "parts.txt")
		var b strings.Builder
		for _, p := range parts {
			fmt.Fprintf(&b, "file '%s'\n", strings.ReplaceAll(p, "'", `'\''`))
		}
		if err := os.WriteFile(list, []byte(b.String()), 0o644); err != nil {
			return "", err
		}
		in = []string{"-f", "concat", "-safe", "0", "-i", list}
	}
	args := append([]string{"-hide_banner", "-loglevel", "error", "-nostdin"}, in...)
	args = append(args, "-map", "0:v?", "-map", "0:a?", "-c", "copy", "-f", "matroska", "-y", dest)
	out, err := exec.CommandContext(ctx, r.FFmpeg, args...).CombinedOutput()
	if err == nil {
		return dest, nil
	}
	os.Remove(dest)
	longest, size := parts[0], int64(-1)
	for _, p := range parts {
		if fi, e := os.Stat(p); e == nil && fi.Size() > size {
			longest, size = p, fi.Size()
		}
	}
	slog.Warn("dvr: remuxing failed; keeping the longest part as MPEG-TS", "err", lastLine(string(out)))
	ts := strings.TrimSuffix(dest, filepath.Ext(dest)) + ".ts"
	return ts, os.Rename(longest, ts)
}

func (r *Recorder) finish(ctx context.Context, id int64, path *string, errMsg string) {
	if path == nil {
		r.Live.DB.ExecContext(ctx, `UPDATE dvr_recordings SET status = 'failed', error = ? WHERE id = ?`, errMsg, id)
		slog.Warn("dvr: recording failed", "id", id, "err", errMsg)
		return
	}
	var size int64
	if fi, err := os.Stat(*path); err == nil {
		size = fi.Size()
	}
	r.Live.DB.ExecContext(ctx, `UPDATE dvr_recordings SET status = 'completed', path = ?, size = ?, error = '' WHERE id = ?`, *path, size, id)
}

// rescan rescans the library holding a recording's folder.
func (r *Recorder) rescan(ctx context.Context, path string) {
	if r.Library == nil || r.Scan == nil {
		return
	}
	movies := strings.HasPrefix(path, filepath.Join(r.Dir, "Movies")+string(filepath.Separator))
	sub := "TV"
	if movies {
		sub = "Movies"
	}
	if lib, err := r.Library(ctx, movies, filepath.Join(r.Dir, sub)); err == nil {
		r.Scan(lib)
	}
}

var (
	reEpisode = regexp.MustCompile(`(?i)^S(\d+)\s*E(\d+)$`)
	badChars  = strings.NewReplacer("/", "-", `\`, "-", ":", " -", "*", "", "?", "", `"`, "'", "<", "", ">", "", "|", "-")
)

// isMovie: films in the guide have a film category and no episode.
func isMovie(x Recording) bool {
	c := strings.ToLower(x.Category)
	return x.Episode == "" && (strings.Contains(c, "movie") || strings.Contains(c, "film"))
}

func clean(s string) string {
	s = strings.TrimSpace(badChars.Replace(s))
	s = strings.Trim(s, ". ")
	if r := []rune(s); len(r) > 100 {
		s = strings.TrimSpace(string(r[:100]))
	}
	if s == "" {
		s = "Recording"
	}
	return s
}

// destination names a recording the way the scanner expects: Movies/Title/Title.mkv, or
// TV/Show/Season N/Show - S01E02 - Name.mkv, or TV/Show/Show - 2026-10-02 - Name.mkv when
// the guide has no episode number.
func (r *Recorder) destination(x Recording) (string, bool) {
	title := clean(x.Title)
	var p string
	movie := isMovie(x)
	switch m := reEpisode.FindStringSubmatch(x.Episode); {
	case movie:
		p = filepath.Join(r.Dir, "Movies", title, title+".mkv")
	case m != nil:
		s, _ := strconv.Atoi(m[1])
		e, _ := strconv.Atoi(m[2])
		name := fmt.Sprintf("%s - S%02dE%02d", title, s, e)
		if x.Subtitle != "" {
			name += " - " + clean(x.Subtitle)
		}
		p = filepath.Join(r.Dir, "TV", title, fmt.Sprintf("Season %d", s), name+".mkv")
	default:
		name := title + " - " + x.Start.Local().Format("2006-01-02")
		if x.Subtitle != "" {
			name += " - " + clean(x.Subtitle)
		}
		p = filepath.Join(r.Dir, "TV", title, name+".mkv")
	}
	// Never overwrite an earlier recording.
	base := strings.TrimSuffix(p, ".mkv")
	for i := 2; ; i++ {
		if _, err := os.Stat(p); errors.Is(err, os.ErrNotExist) {
			return p, movie
		}
		p = fmt.Sprintf("%s (%d).mkv", base, i)
	}
}
