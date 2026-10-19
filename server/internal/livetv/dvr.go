package livetv

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
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
	ErrQuotaExceeded     = errors.New("you're at your recording quota: delete some recordings to record more")
	ErrNotRecording      = errors.New("it isn't recording now")
)

// Messages for recordings that weren't made.
const (
	msgOverQuota    = "Over your recording quota"
	msgRecordingOff = "Recording is turned off for this profile"
	bytesPerGB      = 1_000_000_000
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
	Dir    string // the default recordings folder (the server's configuration)
	// Folder, when set and not empty, is the recordings folder chosen in the settings; new
	// recordings go there. Recordings made elsewhere stay where they are.
	Folder func() string
	// Padding before and after each programme.
	Before, After func() time.Duration
	// Library returns the library for recorded shows or movies at dir, creating it if needed.
	Library func(ctx context.Context, movies bool, dir string) (int64, error)
	// Scan rescans a library after a recording lands in it.
	Scan func(libraryID int64)
	// Now is the clock (tests).
	Now func() time.Time
	// OnFailed, when set, hears a recording that failed (admin alerts).
	OnFailed func(id int64, title, errMsg string)
	// OnRecorded, when set, hears a recording that finished, with its file (commercial
	// detection). It's called after the library rescan is queued.
	OnRecorded func(path string)

	// Quotas and permissions (LIVE-5). Each is optional: without them everyone may record
	// and nobody has a quota.
	// CanRecord reports whether a user may record now; a user who may not has their series
	// paused (kept, but nothing new is scheduled or recorded).
	CanRecord func(ctx context.Context, userID int64) bool
	// QuotaBytes is a user's recording quota in bytes (0 = unlimited).
	QuotaBytes func(ctx context.Context, userID int64) int64
	// QuotaAction is settings.DVRQuotaBlock or settings.DVRQuotaDeleteOldest.
	QuotaAction func() string
	// Playing reports whether someone is watching the file at path now.
	Playing func(path string) bool
	// Trash moves a recording's file to the media trash and out of the library.
	Trash func(ctx context.Context, path string) error
	// OnQuota hears a recording that wasn't made, or recordings removed to make room
	// (admin alerts).
	OnQuota func(userID int64, owner, title, msg string)
	// FollowedBytes is, by user, the size of the episodes on demand their followed series
	// saved that are still in the library: they count towards the DVR quota like recordings.
	FollowedBytes func(ctx context.Context) map[int64]int64

	mu     sync.Mutex
	active map[int64]*activeRec
	wake   chan struct{}
	wg     sync.WaitGroup
}

// activeRec is a recording in progress.
type activeRec struct {
	cancel context.CancelFunc
	done   chan struct{} // closed once it has stopped and been filed (or discarded)
	info   ActiveRecording
	// capturing: the recorder is still writing parts (shared live streams follow them).
	capturing bool
	// stopped: someone stopped it early (Stop); discard: and its recording is deleted.
	stopped, discard bool
}

// ActiveRecording is a recording in progress: what it is, and where its parts are written
// (Work, "" until it starts writing).
type ActiveRecording struct {
	ID, ChannelID, UserID    int64
	SourceID, Title, Channel string
	Start, Stop              time.Time // the programme's
	Began                    time.Time
	Work                     string
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
	// Rating is the guide's content rating ("" = unknown); OwnerName is who scheduled it.
	Rating, OwnerName string
	// StoppedEarly: someone stopped it before the programme ended, keeping what was recorded.
	StoppedEarly bool
}

// Rule records every airing of a title.
type Rule struct {
	ID, UserID  int64
	Title       string
	ChannelID   *int64
	ChannelName string
	CreatedAt   time.Time
	Upcoming    int
	OwnerName   string
	// Paused: its owner may no longer record, so nothing new is scheduled.
	Paused bool
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
	if r == nil || r.dir() == "" {
		return false
	}
	dir := r.dir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false
	}
	f, err := os.CreateTemp(dir, ".probe-*")
	if err != nil {
		return false
	}
	f.Close()
	os.Remove(f.Name())
	return true
}

// dir is the folder new recordings go in.
func (r *Recorder) dir() string {
	if r.Folder != nil {
		if f := strings.TrimSpace(r.Folder()); f != "" {
			return filepath.Clean(f)
		}
	}
	return r.Dir
}

// Roots are the folders that hold recordings: the one new recordings go in, the server's
// default, and earlier ones a recordings library still covers.
func (r *Recorder) Roots(ctx context.Context) []string {
	var out []string
	add := func(d string) {
		if d = filepath.Clean(d); d != "." && d != "" && !slices.Contains(out, d) {
			out = append(out, d)
		}
	}
	add(r.dir())
	if r.Dir != "" {
		add(r.Dir)
	}
	rows, err := r.Live.DB.QueryContext(ctx, `SELECT p.path FROM library_paths p JOIN libraries l ON l.id = p.library_id WHERE l.recordings = 1`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var p string
			if rows.Scan(&p) == nil {
				if b := filepath.Base(p); b == "TV" || b == "Movies" {
					add(filepath.Dir(p))
				}
			}
		}
	}
	return out
}

// rootOf is the recordings folder holding p ("" when none does).
func (r *Recorder) rootOf(ctx context.Context, p string) string {
	for _, root := range r.Roots(ctx) {
		if rel, err := filepath.Rel(root, p); err == nil && rel != "." && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel) {
			return root
		}
	}
	return ""
}

// workDir is where a recording's parts are written: where they already are (a recording
// resumed after a restart, even if the folder setting changed since), else the folder new
// recordings go in.
func (r *Recorder) workDir(ctx context.Context, id int64) string {
	name := strconv.FormatInt(id, 10)
	for _, root := range r.Roots(ctx) {
		w := filepath.Join(root, ".recording", name)
		if fi, err := os.Stat(w); err == nil && fi.IsDir() {
			return w
		}
	}
	return filepath.Join(r.dir(), ".recording", name)
}

// Wake makes the scheduler look again now.
func (r *Recorder) Wake() {
	r.mu.Lock()
	w := r.wake // set by Run, perhaps while a request schedules
	r.mu.Unlock()
	if w == nil {
		return
	}
	select {
	case w <- struct{}{}:
	default:
	}
}

// Run schedules and starts recordings until ctx ends.
func (r *Recorder) Run(ctx context.Context) {
	r.mu.Lock()
	if r.wake == nil {
		r.wake = make(chan struct{}, 1)
	}
	r.active = map[int64]*activeRec{}
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

// Tick follows guide changes, expands series rules, starts recordings that are due and
// marks missed ones.
func (r *Recorder) Tick(ctx context.Context) {
	if err := r.followGuide(ctx); err != nil {
		slog.Warn("dvr: guide changes", "err", err)
	}
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
		if msg := r.mayStart(ctx, d.id); msg != "" {
			r.finish(ctx, d.id, nil, msg)
			continue
		}
		r.start(ctx, d.id, end)
	}
}

// followGuide keeps upcoming recordings in step with the guide: a programme that changed
// name, details or end time is updated; a series airing whose slot the guide now fills
// with something else (another title, or a programme running over the time it started,
// when the airing moved) is dropped, and expandRules schedules it where it is now
// (one-off recordings record whatever airs in their slot).
func (r *Recorder) followGuide(ctx context.Context) error {
	if _, err := r.Live.DB.ExecContext(ctx, `DELETE FROM dvr_recordings WHERE status = 'scheduled' AND rule_id IS NOT NULL
		AND NOT EXISTS (SELECT 1 FROM live_programmes p JOIN dvr_rules d ON d.id = dvr_recordings.rule_id
			WHERE p.channel_id = dvr_recordings.channel_id AND p.start = dvr_recordings.start AND lower(p.title) = lower(d.title))
		AND EXISTS (SELECT 1 FROM live_programmes p
			WHERE p.channel_id = dvr_recordings.channel_id AND p.start <= dvr_recordings.start AND p.stop > dvr_recordings.start)`); err != nil {
		return err
	}
	_, err := r.Live.DB.ExecContext(ctx, `UPDATE dvr_recordings SET title = p.title, subtitle = p.subtitle, description = p.description,
			category = p.category, episode = p.episode, image_url = p.image_url, stop = p.stop, rating = p.rating
		FROM live_programmes p
		WHERE dvr_recordings.status = 'scheduled' AND p.channel_id = dvr_recordings.channel_id AND p.start = dvr_recordings.start
		  AND (p.title <> dvr_recordings.title OR p.stop <> dvr_recordings.stop OR p.subtitle <> dvr_recordings.subtitle OR p.episode <> dvr_recordings.episode
		       OR p.rating <> dvr_recordings.rating)`)
	return err
}

// expandRules schedules every guide airing that matches a series rule.
func (r *Recorder) expandRules(ctx context.Context) error {
	now := r.now().UTC().Format(time.RFC3339)
	paused, err := r.pausedOwners(ctx)
	if err != nil {
		return err
	}
	// A paused series records nothing: airings it scheduled that haven't started are dropped
	// (they come back if its owner may record again).
	for uid := range paused {
		if _, err := r.Live.DB.ExecContext(ctx, `DELETE FROM dvr_recordings WHERE status = 'scheduled' AND rule_id IN (SELECT id FROM dvr_rules WHERE user_id = ?)`, uid); err != nil {
			return err
		}
	}
	rows, err := r.Live.DB.QueryContext(ctx, `SELECT d.id, d.user_id, p.channel_id, c.name, p.start, p.stop, p.title, p.subtitle, p.description, p.category, p.episode, p.image_url, p.rating
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
		rating                                            string
	}
	var list []cand
	for rows.Next() {
		var c cand
		if err := rows.Scan(&c.rule, &c.user, &c.channel, &c.channelName, &c.start, &c.stop, &c.title, &c.subtitle, &c.description, &c.category, &c.ep, &c.image, &c.rating); err != nil {
			rows.Close()
			return err
		}
		if !paused[c.user] {
			list = append(list, c)
		}
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
		if _, err := r.Live.DB.ExecContext(ctx, `INSERT OR IGNORE INTO dvr_recordings (rule_id, user_id, channel_id, channel_name, start, stop, title, subtitle, description, category, episode, image_url, rating, created_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			c.rule, c.user, c.channel, c.channelName, c.start, c.stop, c.title, c.subtitle, c.description, c.category, c.ep, c.image, c.rating, now); err != nil {
			return err
		}
	}
	return nil
}

// Schedule records the programme on channelID starting at start, or every airing of its
// title when series is set (on any channel when anyChannel is also set).
func (r *Recorder) Schedule(ctx context.Context, userID, channelID int64, start time.Time, series, anyChannel bool) (rec *Recording, rule *Rule, err error) {
	var title, chName, stop string
	err = r.Live.DB.QueryRowContext(ctx, `SELECT p.title, c.name, p.stop FROM live_programmes p JOIN live_channels c ON c.id = p.channel_id WHERE p.channel_id = ? AND p.start = ?`,
		channelID, start.UTC().Format(time.RFC3339)).Scan(&title, &chName, &stop)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrProgrammeNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	// A programme that has ended (the guide keeps a few hours back) can't be recorded; it
	// would only fail as missed. A series of it still records its later airings.
	_, after := r.pad()
	if end, err := time.Parse(time.RFC3339, stop); err == nil && !series && !end.Add(after).After(r.now()) {
		return nil, nil, ErrProgrammeNotFound
	}
	if r.atQuota(ctx, userID) {
		return nil, nil, ErrQuotaExceeded
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
	_, err = r.Live.DB.ExecContext(ctx, `INSERT INTO dvr_recordings (user_id, channel_id, channel_name, start, stop, title, subtitle, description, category, episode, image_url, rating, created_at)
		SELECT ?, p.channel_id, ?, p.start, p.stop, p.title, p.subtitle, p.description, p.category, p.episode, p.image_url, p.rating, ?
		FROM live_programmes p WHERE p.channel_id = ? AND p.start = ?
		ON CONFLICT (channel_id, start) DO UPDATE SET status = 'scheduled', error = '', user_id = excluded.user_id, rule_id = NULL,
			title = excluded.title, subtitle = excluded.subtitle, description = excluded.description, category = excluded.category,
			episode = excluded.episode, image_url = excluded.image_url, stop = excluded.stop, rating = excluded.rating
		WHERE status IN ('cancelled', 'failed')`,
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
	(SELECT v.item_id FROM media_files f JOIN media_versions v ON v.id = f.version_id WHERE r.path <> '' AND f.path = r.path),
	r.rating, COALESCE((SELECT u.display_name FROM users u WHERE u.id = r.user_id), ''), r.stopped_early`

func scanRec(row interface{ Scan(...any) error }) (Recording, error) {
	var x Recording
	var start, stop string
	err := row.Scan(&x.ID, &x.UserID, &x.RuleID, &x.ChannelID, &x.ChannelName, &start, &stop, &x.Title, &x.Subtitle, &x.Description, &x.Category,
		&x.Episode, &x.Image, &x.Status, &x.Path, &x.Error, &x.Size, &x.ItemID, &x.Rating, &x.OwnerName, &x.StoppedEarly)
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
		x.Size = r.sizeOf(x)
		out = append(out, x)
	}
	return out, rows.Err()
}

// Rules lists series rules with how many airings are scheduled.
func (r *Recorder) Rules(ctx context.Context) ([]Rule, error) {
	paused, err := r.pausedOwners(ctx)
	if err != nil {
		return nil, err
	}
	rows, err := r.Live.DB.QueryContext(ctx, `SELECT d.id, d.user_id, d.title, d.channel_id, COALESCE(c.name, ''), d.created_at,
		(SELECT COUNT(*) FROM dvr_recordings x WHERE x.rule_id = d.id AND x.status IN ('scheduled', 'recording')),
		COALESCE((SELECT u.display_name FROM users u WHERE u.id = d.user_id), '')
		FROM dvr_rules d LEFT JOIN live_channels c ON c.id = d.channel_id ORDER BY lower(d.title)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Rule
	for rows.Next() {
		var x Rule
		var created string
		if err := rows.Scan(&x.ID, &x.UserID, &x.Title, &x.ChannelID, &x.ChannelName, &created, &x.Upcoming, &x.OwnerName); err != nil {
			return nil, err
		}
		x.CreatedAt, _ = time.Parse(time.RFC3339, created)
		x.Paused = paused[x.UserID]
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

// Cancel cancels an upcoming recording, stops one in progress (keeping what was recorded,
// as Stop with keep does), or removes a finished one from the list, deleting its file too
// when deleteFile is set.
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
		if err := r.Stop(ctx, id, true); err != nil && !errors.Is(err, ErrNotRecording) {
			return err
		}
		return nil
	}
	if deleteFile && x.Path != "" && r.rootOf(ctx, x.Path) != "" {
		if err := os.Remove(x.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		os.Remove(filepath.Dir(x.Path)) // only when empty
		r.rescan(ctx, x.Path)
	}
	return r.forget(ctx, x)
}

// forget removes a finished recording from the list.
func (r *Recorder) forget(ctx context.Context, x Recording) error {
	if x.RuleID != nil {
		// Keep the row (hidden) so the series doesn't record this airing again.
		_, err := r.Live.DB.ExecContext(ctx, `UPDATE dvr_recordings SET status = 'cancelled', path = '' WHERE id = ?`, x.ID)
		return err
	}
	_, err := r.Live.DB.ExecContext(ctx, `DELETE FROM dvr_recordings WHERE id = ?`, x.ID)
	return err
}

// Stop stops a recording in progress now. With keep, what was recorded is filed as a
// finished recording (a playable file, marked StoppedEarly); without, the partial
// recording is deleted and it leaves the list. It returns once the recording has stopped
// and been filed or deleted (or ctx ends).
func (r *Recorder) Stop(ctx context.Context, id int64, keep bool) error {
	r.mu.Lock()
	a := r.active[id]
	if a == nil {
		r.mu.Unlock()
		return ErrNotRecording
	}
	a.stopped, a.discard = true, !keep
	cancel, done := a.cancel, a.done
	r.mu.Unlock()
	slog.Info("dvr: stopping a recording early", "id", id, "keep", keep)
	cancel()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// ActiveRecordings lists the recordings in progress.
func (r *Recorder) ActiveRecordings() []ActiveRecording {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]ActiveRecording, 0, len(r.active))
	for _, a := range r.active {
		out = append(out, a.info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Began.Before(out[j].Began) })
	return out
}

// Capturing reports whether a recording is still writing its parts.
func (r *Recorder) Capturing(id int64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	a := r.active[id]
	return a != nil && a.capturing
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
		r.active = map[int64]*activeRec{}
	}
	if _, running := r.active[id]; running {
		r.mu.Unlock()
		return
	}
	rctx, cancel := context.WithDeadline(ctx, end)
	a := &activeRec{cancel: cancel, done: make(chan struct{}), capturing: true, info: ActiveRecording{ID: id, Began: r.now()}}
	if x, err := r.Get(ctx, id); err == nil {
		a.info.UserID, a.info.Title, a.info.Channel, a.info.Start, a.info.Stop = x.UserID, x.Title, x.ChannelName, x.Start, x.Stop
		if x.ChannelID != nil {
			a.info.ChannelID = *x.ChannelID
			if ch, err := r.Live.Channel(ctx, *x.ChannelID); err == nil {
				a.info.SourceID = ch.SourceID
			}
		}
	}
	r.active[id] = a
	r.mu.Unlock()
	r.Live.DB.ExecContext(ctx, `UPDATE dvr_recordings SET status = 'recording', error = '', stopped_early = 0 WHERE id = ?`, id)
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		defer func() {
			r.mu.Lock()
			delete(r.active, id)
			r.mu.Unlock()
			cancel()
			close(a.done)
		}()
		// A recording takes priority over live streams for the source's connections.
		if a.info.SourceID != "" {
			r.Live.makeRoom(a.info.SourceID, a.info.ChannelID)
			// VOD downloads from the same provider give back theirs before it connects.
			r.Live.beforeConnect(a.info.SourceID)
		}
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
	work := r.workDir(ctx, id)
	root := filepath.Dir(filepath.Dir(work))
	if err := os.MkdirAll(work, 0o755); err != nil {
		r.finish(ctx, id, nil, err.Error())
		return
	}
	r.mu.Lock()
	a := r.active[id]
	if a != nil {
		a.info.Work = work
	}
	r.mu.Unlock()
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
		// Not -nostdin: FFmpeg is stopped by a "q" on its input (see below).
		args := []string{"-hide_banner", "-loglevel", "error"}
		if ua != "" {
			args = append(args, "-user_agent", ua)
		}
		if strings.HasPrefix(ch.StreamURL, "http") {
			args = append(args, "-reconnect", "1", "-reconnect_streamed", "1", "-reconnect_delay_max", "5", "-rw_timeout", "15000000")
		}
		args = append(args, "-i", ch.StreamURL, "-map", "0:v:0?", "-map", "0:a?", "-c", "copy", "-f", "mpegts",
			"-t", strconv.Itoa(int(left.Seconds())+1), "-y", out)
		cmd := exec.CommandContext(rctx, r.FFmpeg, args...)
		// Stopping (the programme's end, or Stop): "q" makes FFmpeg finish cleanly. An
		// interrupt cuts off the packet it's reading, leaving a damaged frame in the file,
		// so it's only the fallback if FFmpeg doesn't quit (stuck on the network); a kill
		// is the last resort.
		stdin, perr := cmd.StdinPipe()
		cmd.Cancel = func() error {
			if perr == nil {
				if _, err := io.WriteString(stdin, "q"); err == nil {
					p := cmd.Process
					time.AfterFunc(4*time.Second, func() { p.Signal(os.Interrupt) })
					return nil
				}
			}
			return cmd.Process.Signal(os.Interrupt)
		}
		cmd.WaitDelay = 10 * time.Second
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
	r.mu.Lock()
	var stopped, discard bool
	if a != nil {
		a.capturing = false
		stopped, discard = a.stopped, a.discard
	}
	r.mu.Unlock()
	if ctx.Err() != nil {
		return // shutting down: the parts stay and recording resumes after the restart
	}
	if discard {
		// Stop and delete: the partial recording goes, and so does its row.
		os.RemoveAll(work)
		if x, err := r.Get(ctx, id); err == nil {
			r.forget(ctx, x)
		}
		slog.Info("dvr: recording stopped and deleted", "id", id)
		return
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
	dest, movies := destination(root, x)
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
	if stopped {
		r.Live.DB.ExecContext(ctx, `UPDATE dvr_recordings SET stopped_early = 1 WHERE id = ?`, id)
	}
	if r.Library != nil {
		sub := "TV"
		if movies {
			sub = "Movies"
		}
		if lib, err := r.Library(ctx, movies, filepath.Join(root, sub)); err != nil {
			slog.Warn("dvr: library", "err", err)
		} else if r.Scan != nil {
			r.Scan(lib)
		}
	}
	slog.Info("dvr: recorded", "id", id, "path", dest)
	if r.OnRecorded != nil {
		r.OnRecorded(dest)
	}
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
	// Never over an earlier recording (destination only checked the .mkv name).
	base := strings.TrimSuffix(dest, filepath.Ext(dest))
	ts := base + ".ts"
	for i := 2; ; i++ {
		if _, err := os.Lstat(ts); errors.Is(err, os.ErrNotExist) {
			break
		}
		ts = fmt.Sprintf("%s (%d).ts", base, i)
	}
	return ts, os.Rename(longest, ts)
}

func (r *Recorder) finish(ctx context.Context, id int64, path *string, errMsg string) {
	if path == nil {
		r.Live.DB.ExecContext(ctx, `UPDATE dvr_recordings SET status = 'failed', error = ? WHERE id = ?`, errMsg, id)
		slog.Warn("dvr: recording failed", "id", id, "err", errMsg)
		if r.OnFailed != nil {
			var title string
			r.Live.DB.QueryRowContext(ctx, `SELECT title FROM dvr_recordings WHERE id = ?`, id).Scan(&title)
			r.OnFailed(id, title, errMsg)
		}
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
	root := r.rootOf(ctx, path)
	if root == "" {
		return
	}
	movies := strings.HasPrefix(path, filepath.Join(root, "Movies")+string(filepath.Separator))
	sub := "TV"
	if movies {
		sub = "Movies"
	}
	if lib, err := r.Library(ctx, movies, filepath.Join(root, sub)); err == nil {
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
func destination(root string, x Recording) (string, bool) {
	title := clean(x.Title)
	var p string
	movie := isMovie(x)
	switch m := reEpisode.FindStringSubmatch(x.Episode); {
	case movie:
		p = filepath.Join(root, "Movies", title, title+".mkv")
	case m != nil:
		s, _ := strconv.Atoi(m[1])
		e, _ := strconv.Atoi(m[2])
		name := fmt.Sprintf("%s - S%02dE%02d", title, s, e)
		if x.Subtitle != "" {
			name += " - " + clean(x.Subtitle)
		}
		p = filepath.Join(root, "TV", title, fmt.Sprintf("Season %d", s), name+".mkv")
	default:
		name := title + " - " + x.Start.Local().Format("2006-01-02")
		if x.Subtitle != "" {
			name += " - " + clean(x.Subtitle)
		}
		p = filepath.Join(root, "TV", title, name+".mkv")
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

// NewFolder is the folder new recordings go in.
func (r *Recorder) NewFolder() string { return r.dir() }
