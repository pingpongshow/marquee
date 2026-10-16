// Package plexsync keeps watch state, plays, resume positions and ratings in step between
// Marquee and a Plex Media Server, both ways, through Plex's HTTP API (client.go lists the
// calls). It never writes to Plex's database files, never deletes anything in Plex and never
// touches Plex libraries, metadata or files.
//
// Items pair through their files, with the Plex import's folder mappings (plex.Remap);
// Plex accounts pair with Marquee users through plex_sync_users (seeded by the import,
// editable). For each pair the sync ledger (plex_sync_ledger) holds the values both sides
// had after the last sync, so a side whose value moved since has changed; when only one side
// changed it wins, when both did the newer change wins (ties go to Marquee). Marquee's change
// times come from user_item_state.*_changed_at (kept by triggers), Plex's from lastViewedAt
// and lastRatedAt.
package plexsync

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"marquee/internal/library"
	"marquee/internal/notify"
	"marquee/internal/plex"
	"marquee/internal/settings"
	"marquee/internal/tasks"
)

// TaskID is the scheduled task that runs the sync.
const TaskID = "plex-sync"

const (
	reportKey = "plex_sync_report"
	stateKey  = "plex_sync_state"
	// maxScrobbles caps the plays pushed for one item in one run (each is one Plex request
	// and one Plex history entry).
	maxScrobbles = 10
	// Change lists keep this many entries.
	maxChangesRun     = 200
	maxChangesPreview = 2000
	maxErrors         = 50
	// unreachableAlertAfter: runs in a row that couldn't reach Plex before the alert.
	unreachableAlertAfter = 3
)

var (
	ErrNotConfigured = errors.New("set the Plex server address and token first")
	ErrRunning       = errors.New("a Plex sync is already running")
)

// Alerter raises admin alerts (notify.Alerts).
type Alerter interface {
	Raise(ctx context.Context, a notify.Alert) bool
}

// Service runs syncs. Only one sync or preview runs at a time.
type Service struct {
	DB       *sql.DB
	Settings *settings.Store
	Alerts   Alerter
	// PlexTV is plex.tv's address (tests use a fake); default https://plex.tv.
	PlexTV string
	HTTP   *http.Client
	// RateEvery is the minimum gap between Plex requests (default 100 ms: 10 a second).
	RateEvery time.Duration
	// Backoff is the first delay before retrying after HTTP 429/5xx or a dropped
	// connection (default 2 s, doubling, 3 retries).
	Backoff  time.Duration
	PageSize int // items per listing request (default 500)
	Now      func() time.Time

	mu       sync.Mutex
	running  bool
	progress Progress
	reach    struct {
		at  time.Time
		key string
		ok  bool
	}
}

// Progress is a running sync's progress.
type Progress struct {
	Step        string
	Done, Total int
}

// Change is one value copied one way.
type Change struct {
	Direction string `json:"direction"` // toPlex, toMarquee
	User      string `json:"user"`
	Title     string `json:"title"`
	Kind      string `json:"kind"` // watched, unwatched, plays, lastPlayed, position, rating, history
	From      string `json:"from,omitempty"`
	To        string `json:"to,omitempty"`
	Conflict  bool   `json:"conflict,omitempty"`
}

// Report sums up a sync or a preview.
type Report struct {
	StartedAt  time.Time `json:"startedAt"`
	FinishedAt time.Time `json:"finishedAt"`
	DryRun     bool      `json:"dryRun"`
	// Waiting: Plex wasn't reachable, so the sync waits for the next run.
	Waiting          bool     `json:"waiting"`
	ToPlex           int      `json:"toPlex"`
	ToMarquee        int      `json:"toMarquee"`
	HistoryPulled    int      `json:"historyPulled"`
	Conflicts        int      `json:"conflicts"`
	Skipped          int      `json:"skipped"`
	Failed           int      `json:"failed"`
	Unchanged        int      `json:"unchanged"`
	NotInMarquee     int      `json:"notInMarquee"`
	UsersSynced      int      `json:"usersSynced"`
	Changes          []Change `json:"changes"`
	ChangesTruncated bool     `json:"changesTruncated"`
	Errors           []string `json:"errors"`

	maxChanges int
}

// Summary is a one-line account of the run.
func (r *Report) Summary() string {
	if r.Waiting {
		return "Plex isn't reachable; the sync waits for the next run"
	}
	verb := "Pushed"
	if r.DryRun {
		verb = "Would push"
	}
	s := fmt.Sprintf("%s %d to Plex, pulled %d, %d conflicts, %d skipped", verb, r.ToPlex, r.ToMarquee+r.HistoryPulled, r.Conflicts, r.Skipped)
	if r.Failed > 0 {
		s += fmt.Sprintf(", %d failed", r.Failed)
	}
	return s
}

func (r *Report) add(c Change) {
	if len(r.Changes) >= r.maxChanges {
		r.ChangesTruncated = true
		return
	}
	r.Changes = append(r.Changes, c)
}

func (r *Report) fail(msg string) {
	r.Failed++
	if len(r.Errors) < maxErrors {
		r.Errors = append(r.Errors, msg)
	}
}

// state is kept between runs.
type state struct {
	HistorySince int64     `json:"historySince"` // unix seconds: newest Plex play pulled
	LastRunAt    time.Time `json:"lastRunAt"`
	// LastSyncAt is when the last complete sync started: Plex changes Plex doesn't date
	// (an unwatch, a rating without lastRatedAt) are taken to be from then.
	LastSyncAt  time.Time `json:"lastSyncAt"`
	Unreachable int       `json:"unreachable"` // runs in a row that couldn't reach Plex
	LastError   string    `json:"lastError"`
	Alerted     bool      `json:"alerted"`
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) loadJSON(ctx context.Context, key string, v any) bool {
	var raw string
	if err := s.DB.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, key).Scan(&raw); err != nil {
		return false
	}
	return json.Unmarshal([]byte(raw), v) == nil
}

func (s *Service) saveJSON(ctx context.Context, key string, v any) {
	raw, _ := json.Marshal(v)
	s.DB.ExecContext(ctx, `INSERT INTO settings(key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')`, key, string(raw))
}

// LastReport is the last real (not preview) run's report.
func (s *Service) LastReport(ctx context.Context) *Report {
	var r Report
	if !s.loadJSON(ctx, reportKey, &r) {
		return nil
	}
	return &r
}

func (s *Service) setProgress(step string, done, total int) {
	s.mu.Lock()
	s.progress = Progress{Step: step, Done: done, Total: total}
	s.mu.Unlock()
}

// Running reports whether a sync or preview is running, and its progress.
func (s *Service) Running() (bool, Progress) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running, s.progress
}

// Task is the scheduled "Plex sync" task: every IntervalMinutes while automatic sync is
// on; Run Now (and Sync now) work either way.
func (s *Service) Task() tasks.Task {
	return tasks.Task{ID: TaskID, Name: "Plex sync",
		Description: "Syncs watch status, plays, resume positions and ratings with Plex, both ways (Settings → Plex). Runs on its interval while automatic sync is on.",
		EveryFunc: func() time.Duration {
			c := s.Settings.Get().PlexSync
			if !c.Enabled || !c.Configured() {
				return 0
			}
			return time.Duration(c.IntervalMinutes) * time.Minute
		},
		Run: func(ctx context.Context) (string, error) {
			rep, err := s.Run(ctx, false)
			if err != nil {
				return "", err
			}
			return rep.Summary(), nil
		}}
}

// NextRun is when automatic sync runs next (nil while it's off).
func (s *Service) NextRun(ctx context.Context) *time.Time {
	c := s.Settings.Get().PlexSync
	if !c.Enabled || !c.Configured() {
		return nil
	}
	var st state
	s.loadJSON(ctx, stateKey, &st)
	next := st.LastRunAt.Add(time.Duration(c.IntervalMinutes) * time.Minute)
	if now := s.now(); next.Before(now) {
		next = now
	}
	return &next
}

// LastError is the last run's error ("" when it worked).
func (s *Service) LastError(ctx context.Context) string {
	var st state
	s.loadJSON(ctx, stateKey, &st)
	return st.LastError
}

func (s *Service) clients(cfg settings.PlexSync) (srv, tv *client) {
	hc := s.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 60 * time.Second}
	}
	every := s.RateEvery
	if every == 0 {
		every = 100 * time.Millisecond
	}
	backoff := s.Backoff
	if backoff == 0 {
		backoff = 2 * time.Second
	}
	lim := &limiter{every: every}
	sec := &secrets{}
	sec.add(cfg.Token)
	id := "marquee"
	if s.Settings != nil {
		id = "marquee-" + s.Settings.ServerID()
	}
	tvBase := s.PlexTV
	if tvBase == "" {
		tvBase = "https://plex.tv"
	}
	srv = &client{base: cfg.URL, clientID: id, http: hc, lim: lim, backoff: backoff, retries: 3, secrets: sec}
	tv = &client{base: tvBase, clientID: id, http: hc, lim: lim, backoff: backoff, retries: 3, secrets: sec}
	return srv, tv
}

// Reachable checks (at most once a minute) whether Plex answers with the token; nil when
// sync isn't configured.
func (s *Service) Reachable(ctx context.Context) *bool {
	cfg := s.Settings.Get().PlexSync
	if !cfg.Configured() {
		return nil
	}
	key := cfg.URL + "\x00" + cfg.Token
	s.mu.Lock()
	if s.reach.key == key && (s.running || s.now().Sub(s.reach.at) < time.Minute) {
		ok := s.reach.ok
		s.mu.Unlock()
		return &ok
	}
	s.mu.Unlock()
	srv, _ := s.clients(cfg)
	srv.retries = 0
	cctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	_, err := srv.identity(cctx, cfg.Token)
	ok := err == nil
	s.setReach(key, ok)
	return &ok
}

func (s *Service) setReach(key string, ok bool) {
	s.mu.Lock()
	s.reach.at, s.reach.key, s.reach.ok = s.now(), key, ok
	s.mu.Unlock()
}

// Run syncs once (dry: only work out what would change). Plex being unreachable isn't an
// error: the report says Waiting and nothing changes.
func (s *Service) Run(ctx context.Context, dry bool) (*Report, error) {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return nil, ErrRunning
	}
	s.running, s.progress = true, Progress{Step: "Connecting to Plex"}
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
	}()
	cfg := s.Settings.Get().PlexSync
	if !cfg.Configured() {
		return nil, ErrNotConfigured
	}
	rep := &Report{StartedAt: s.now().UTC(), DryRun: dry, Changes: []Change{}, Errors: []string{}, maxChanges: maxChangesRun}
	if dry {
		rep.maxChanges = maxChangesPreview
	}
	var st state
	s.loadJSON(ctx, stateKey, &st)
	srv, tv := s.clients(cfg)
	err := s.run(ctx, cfg, srv, tv, &st, rep, dry)
	rep.FinishedAt = s.now().UTC()
	key := cfg.URL + "\x00" + cfg.Token
	if errors.Is(err, ErrUnreachable) {
		rep.Waiting, err = true, nil
		s.setReach(key, false)
		slog.Info("Plex sync: Plex isn't reachable; waiting for the next run")
	} else if err == nil || !errors.Is(err, ErrUnauthorized) {
		s.setReach(key, true)
	}
	if err != nil {
		err = errors.New(srv.secrets.redact(err.Error()))
	}
	if dry {
		return rep, err
	}
	st.LastRunAt = rep.StartedAt // the interval counts from every attempt
	if err == nil && !rep.Waiting {
		st.LastSyncAt = rep.StartedAt
	}
	s.alert(ctx, &st, rep, err)
	s.saveJSON(ctx, stateKey, st)
	s.saveJSON(ctx, reportKey, rep)
	s.DB.ExecContext(ctx, `DELETE FROM plex_sync_scrobbles WHERE pushed_at < ?`, s.now().Add(-30*24*time.Hour).Unix())
	if err == nil {
		slog.Info("Plex sync finished", "result", rep.Summary())
	}
	return rep, err
}

// alert raises (or resolves) the "Plex sync failing" alert.
func (s *Service) alert(ctx context.Context, st *state, rep *Report, err error) {
	failing, msg := false, ""
	switch {
	case err != nil:
		st.LastError = err.Error()
		failing, msg = true, err.Error()
	case rep.Waiting:
		st.Unreachable++
		st.LastError = ""
		if st.Unreachable >= unreachableAlertAfter {
			failing, msg = true, fmt.Sprintf("Plex hasn't been reachable for %d sync runs in a row. The sync waits and carries on when Plex is back.", st.Unreachable)
		}
	default:
		st.Unreachable, st.LastError = 0, ""
	}
	if s.Alerts == nil {
		return
	}
	if failing {
		if s.Alerts.Raise(ctx, notify.Alert{Type: settings.AlertPlexSync, Subject: "plex-sync", Title: "Plex sync is failing", Message: msg}) {
			st.Alerted = true
		}
	} else if st.Alerted && !rep.Waiting {
		st.Alerted = false
		s.Alerts.Raise(ctx, notify.Alert{Type: settings.AlertPlexSync, Subject: "plex-sync", Title: "Plex sync is working again",
			Message: rep.Summary(), Resolved: true})
	}
}

var sectionLeaf = map[string]struct {
	plexType int    // plex.Section.Type
	leafType string // type= for /all: movies, episodes, tracks
}{"movie": {1, "1"}, "show": {2, "4"}, "artist": {8, "10"}}

func (s *Service) run(ctx context.Context, cfg settings.PlexSync, srv, tv *client, st *state, rep *Report, dry bool) error {
	machineID, err := srv.identity(ctx, cfg.Token)
	if err != nil {
		return err
	}
	s.setProgress("Finding Plex users", 0, 0)
	users, err := s.resolveUsers(ctx, srv, tv, cfg.Token, machineID)
	if err != nil {
		return err
	}
	dirs, err := srv.sections(ctx, cfg.Token)
	if err != nil {
		return err
	}
	var sections []plex.Section
	for _, d := range dirs {
		if t, ok := sectionLeaf[d.Type]; ok {
			sec := plex.Section{Name: d.Title, Type: t.plexType}
			for _, l := range d.Location {
				sec.Roots = append(sec.Roots, l.Path)
			}
			sections = append(sections, sec)
		}
	}
	var mappings []plex.PathMapping
	for _, m := range cfg.PathMappings {
		mappings = append(mappings, plex.PathMapping{Plex: m.Plex, Marquee: m.Marquee})
	}
	if len(mappings) == 0 {
		libs, err := library.NewStore(s.DB).List(ctx)
		if err != nil {
			return err
		}
		mappings = plex.DefaultMappings(sections, libs)
	}
	files, err := s.marqueeFiles(ctx)
	if err != nil {
		return err
	}

	// The owner's listing pairs items for the history pull even when the owner doesn't sync.
	var order []*User
	for i := range users {
		if users[i].PlexAccountID == ownerAccount {
			order = append([]*User{&users[i]}, order...)
		} else if users[i].CanSync {
			order = append(order, &users[i])
		}
	}
	keyToItem := map[string]int64{}
	unmatched := map[string]bool{}
	lastRun := st.LastSyncAt
	for _, u := range order {
		syncing := u.CanSync && u.UserID != 0 && (cfg.WatchState || cfg.Ratings || cfg.ResumePositions)
		if !syncing && (u.PlexAccountID != ownerAccount || !cfg.PlayHistory) {
			continue
		}
		if syncing {
			rep.UsersSynced++
		}
		var mstates map[int64]mstate
		var ledger map[string]ledgerRow
		if syncing {
			if mstates, err = s.marqueeState(ctx, u.UserID); err != nil {
				return err
			}
			if ledger, err = s.loadLedger(ctx, u.UserID); err != nil {
				return err
			}
		}
		done := 0
		for _, d := range dirs {
			leaf, ok := sectionLeaf[d.Type]
			if !ok {
				continue
			}
			step := fmt.Sprintf("Syncing %s: %s", u.Name, d.Title)
			s.setProgress(step, done, 0)
			pageSize := s.PageSize
			if pageSize <= 0 {
				pageSize = 500
			}
			err := srv.list(ctx, "/library/sections/"+url.PathEscape(d.Key)+"/all", url.Values{"type": {leaf.leafType}}, u.token, pageSize, func(md Metadata) error {
				done++
				if done%200 == 0 {
					s.setProgress(step, done, 0)
				}
				item := pairItem(md, mappings, files)
				if item == 0 {
					if !unmatched[md.RatingKey] {
						unmatched[md.RatingKey] = true
						rep.NotInMarquee++
					}
					return nil
				}
				keyToItem[md.RatingKey] = item
				if !syncing {
					return nil
				}
				return s.syncItem(ctx, srv, cfg, u, item, md, mstates[item], ledger[md.RatingKey], lastRun, rep, dry)
			})
			if errors.Is(err, ErrUnreachable) || errors.Is(err, ErrUnauthorized) && u.PlexAccountID == ownerAccount || ctx.Err() != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				return err
			}
			if err != nil {
				rep.fail(fmt.Sprintf("%s, %s: %s", u.Name, d.Title, srv.secrets.redact(err.Error())))
			}
		}
	}
	if cfg.PlayHistory {
		s.setProgress("Pulling Plex play history", 0, 0)
		if err := s.pullHistory(ctx, srv, cfg.Token, users, keyToItem, st, rep, dry); err != nil {
			return err
		}
	}
	return nil
}

// pairItem finds the Marquee item behind a Plex item's files.
func pairItem(md Metadata, mappings []plex.PathMapping, files map[string]int64) int64 {
	for _, m := range md.Media {
		for _, p := range m.Part {
			if mp, ok := plex.Remap(p.File, mappings); ok {
				if id, found := files[mp]; found {
					return id
				}
			}
		}
	}
	return 0
}

func (s *Service) marqueeFiles(ctx context.Context) (map[string]int64, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT f.path, v.item_id FROM media_files f JOIN media_versions v ON v.id = f.version_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var p string
		var id int64
		if err := rows.Scan(&p, &id); err != nil {
			return nil, err
		}
		out[p] = id
	}
	return out, rows.Err()
}

// ---------- the two sides' state ----------

type mstate struct {
	count, offset               int64
	rating                      *float64
	lastViewed                  time.Time
	countAt, offsetAt, ratingAt time.Time
}

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339Nano, s)
	return t
}

func (s *Service) marqueeState(ctx context.Context, uid int64) (map[int64]mstate, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT item_id, play_count, view_offset_ms, rating, COALESCE(last_viewed_at, ''),
		COALESCE(play_count_changed_at, last_viewed_at, ''), COALESCE(view_offset_changed_at, last_viewed_at, ''),
		COALESCE(rating_changed_at, updated_at, '') FROM user_item_state WHERE user_id = ?`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]mstate{}
	for rows.Next() {
		var id int64
		var m mstate
		var rating sql.NullFloat64
		var last, cAt, oAt, rAt string
		if err := rows.Scan(&id, &m.count, &m.offset, &rating, &last, &cAt, &oAt, &rAt); err != nil {
			return nil, err
		}
		if rating.Valid {
			m.rating = &rating.Float64
		}
		m.lastViewed, m.countAt, m.offsetAt, m.ratingAt = parseTime(last), parseTime(cAt), parseTime(oAt), parseTime(rAt)
		out[id] = m
	}
	return out, rows.Err()
}

// ledgerRow is what both sides had after the last sync of a (user, item).
type ledgerRow struct {
	watchSynced      bool
	mCount, pCount   int64
	offsetSynced     bool
	mOffset, pOffset int64
	ratingSynced     bool
	mRating, pRating *float64
}

func (s *Service) loadLedger(ctx context.Context, uid int64) (map[string]ledgerRow, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT rating_key, watch_synced, m_play_count, p_view_count, offset_synced, m_offset_ms, p_offset_ms,
		rating_synced, m_rating, p_rating FROM plex_sync_ledger WHERE user_id = ?`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]ledgerRow{}
	for rows.Next() {
		var k string
		var l ledgerRow
		var mr, pr sql.NullFloat64
		if err := rows.Scan(&k, &l.watchSynced, &l.mCount, &l.pCount, &l.offsetSynced, &l.mOffset, &l.pOffset, &l.ratingSynced, &mr, &pr); err != nil {
			return nil, err
		}
		if mr.Valid {
			l.mRating = &mr.Float64
		}
		if pr.Valid {
			l.pRating = &pr.Float64
		}
		out[k] = l
	}
	return out, rows.Err()
}

func (s *Service) saveLedger(ctx context.Context, uid int64, key string, item int64, l ledgerRow) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO plex_sync_ledger(user_id, rating_key, item_id, watch_synced, m_play_count, p_view_count,
		offset_synced, m_offset_ms, p_offset_ms, rating_synced, m_rating, p_rating) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id, rating_key) DO UPDATE SET item_id = excluded.item_id, watch_synced = excluded.watch_synced,
		m_play_count = excluded.m_play_count, p_view_count = excluded.p_view_count, offset_synced = excluded.offset_synced,
		m_offset_ms = excluded.m_offset_ms, p_offset_ms = excluded.p_offset_ms, rating_synced = excluded.rating_synced,
		m_rating = excluded.m_rating, p_rating = excluded.p_rating, synced_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')`,
		uid, key, item, l.watchSynced, l.mCount, l.pCount, l.offsetSynced, l.mOffset, l.pOffset, l.ratingSynced, l.mRating, l.pRating)
	return err
}

// plexRating is Plex's userRating on Marquee's scale. Both are 0–10, so (as in the Plex
// import) the value carries over unchanged; Plex has no 0 rating, so ≤ 0 is no rating.
func plexRating(r *float64) *float64 {
	if r == nil || *r <= 0 {
		return nil
	}
	v := math.Min(*r, 10)
	return &v
}

func sameRating(a, b *float64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return math.Abs(*a-*b) < 0.05
}

func unix(sec int64) time.Time {
	if sec <= 0 {
		return time.Time{}
	}
	return time.Unix(sec, 0).UTC()
}

// ---------- deciding ----------

// plan is what to change for one (user, item).
type plan struct {
	// Marquee.
	setCount      *int64
	countAt       time.Time
	setLastViewed *time.Time
	setOffset     *int64
	offsetAt      time.Time
	setRating     bool
	newRating     *float64
	ratingAt      time.Time
	// Plex.
	scrobbles  int
	unscrobble bool
	pushOffset *int64
	pushRating bool
	plexRating *float64

	changes   []Change
	conflicts int
	skipped   int
	// Kinds looked at (their ledger values are updated once applied).
	watchKind, offsetKind, ratingKind bool
}

func (p *plan) empty() bool {
	return p.setCount == nil && p.setLastViewed == nil && p.setOffset == nil && !p.setRating &&
		p.scrobbles == 0 && !p.unscrobble && p.pushOffset == nil && !p.pushRating
}

func plays(n int64) string {
	if n == 1 {
		return "1 play"
	}
	return fmt.Sprintf("%d plays", n)
}

func clock(ms int64) string {
	sec := ms / 1000
	if sec >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", sec/3600, sec/60%60, sec%60)
	}
	return fmt.Sprintf("%d:%02d", sec/60, sec%60)
}

func ratingText(r *float64) string {
	if r == nil {
		return "no rating"
	}
	return fmt.Sprintf("%s/10", strings.TrimSuffix(strings.TrimRight(fmt.Sprintf("%.1f", *r), "0"), "."))
}

// decide works out one (user, item): m is Marquee's state, p Plex's, l the ledger, lastRun
// when the previous sync ran (the time given to a Plex change Plex doesn't date).
func decide(cfg settings.PlexSync, m mstate, p Metadata, l ledgerRow, lastRun, now time.Time) plan {
	var pl plan
	track := p.Type == "track"
	pLast := unix(p.LastViewedAt)

	// Watched / unwatched and play counts: the count is the larger of the two, unless one
	// side was marked unwatched more recently than the other's last play.
	if cfg.WatchState {
		pl.watchKind = true
		first := !l.watchSynced
		mC, pC := m.count != l.mCount, p.ViewCount != l.pCount
		if first {
			mC, pC = m.count > 0, p.ViewCount > 0
		}
		if mC || pC {
			target := max(m.count, p.ViewCount)
			conflict := false
			if !first {
				marqueeWins, plexWins := mC && !pC, pC && !mC
				if mC && pC && m.count != p.ViewCount {
					conflict = true
					mT := m.countAt
					if mT.IsZero() {
						mT = m.lastViewed
					}
					pT := pLast
					if p.ViewCount == 0 || pT.IsZero() {
						pT = lastRun // Plex doesn't date an unwatch
					}
					plexWins = pT.After(mT)
					marqueeWins = !plexWins
				}
				if marqueeWins && m.count == 0 || plexWins && p.ViewCount == 0 {
					target = 0
				}
			}
			if conflict {
				pl.conflicts++
			}
			if target == 0 {
				if p.ViewCount > 0 {
					pl.unscrobble = true
					pl.changes = append(pl.changes, Change{Direction: "toPlex", Kind: "unwatched", From: plays(p.ViewCount), To: "unwatched", Conflict: conflict})
				}
				if m.count > 0 {
					zero := int64(0)
					pl.setCount, pl.countAt = &zero, lastRun
					if pl.countAt.IsZero() || pl.countAt.Before(m.countAt) {
						pl.countAt = now
					}
					pl.changes = append(pl.changes, Change{Direction: "toMarquee", Kind: "unwatched", From: plays(m.count), To: "unwatched", Conflict: conflict})
				}
			} else {
				if p.ViewCount < target {
					n := int(target - p.ViewCount)
					if n > maxScrobbles {
						n = maxScrobbles
					}
					pl.scrobbles = n
					kind := "plays"
					if p.ViewCount == 0 {
						kind = "watched"
					}
					pl.changes = append(pl.changes, Change{Direction: "toPlex", Kind: kind, From: plays(p.ViewCount), To: plays(p.ViewCount + int64(n)), Conflict: conflict})
				}
				if m.count < target {
					t := target
					pl.setCount, pl.countAt = &t, pLast
					if pl.countAt.IsZero() {
						pl.countAt = now
					}
					kind := "plays"
					if m.count == 0 {
						kind = "watched"
					}
					pl.changes = append(pl.changes, Change{Direction: "toMarquee", Kind: kind, From: plays(m.count), To: plays(target), Conflict: conflict})
				}
				// Last played: the newer of the two (Plex can't be given a play time; a play
				// pushed to Plex is dated when it's pushed).
				if pC && pLast.After(m.lastViewed) {
					t := pLast
					pl.setLastViewed = &t
					if m.count >= target {
						pl.changes = append(pl.changes, Change{Direction: "toMarquee", Kind: "lastPlayed", To: pLast.Format(time.RFC3339)})
					}
				}
			}
		}
	}

	// Resume position (not for music): from whichever side changed it last.
	if cfg.ResumePositions && !track {
		pl.offsetKind = true
		first := !l.offsetSynced
		mC, pC := m.offset != l.mOffset, p.ViewOffset != l.pOffset
		if first {
			mC, pC = m.offset > 0, p.ViewOffset > 0
		}
		diff := m.offset - p.ViewOffset
		if (mC || pC) && (diff > 2000 || diff < -2000) {
			mT := m.offsetAt
			if mT.IsZero() {
				mT = m.lastViewed
			}
			plexWins := pC && !mC
			if first || mC && pC {
				plexWins = pLast.After(mT)
				if !first {
					pl.conflicts++
				}
			}
			switch {
			case plexWins:
				// A watched/unwatched change on Marquee's side this run already clears it.
				if !(pl.setCount != nil && p.ViewOffset == 0) {
					o := p.ViewOffset
					pl.setOffset, pl.offsetAt = &o, pLast
					if pl.offsetAt.IsZero() {
						pl.offsetAt = now
					}
					pl.changes = append(pl.changes, Change{Direction: "toMarquee", Kind: "position", From: clock(m.offset), To: clock(o), Conflict: mC && pC})
					if pLast.After(m.lastViewed) && pl.setLastViewed == nil {
						t := pLast
						pl.setLastViewed = &t
					}
				}
			case m.offset == 0:
				// Nothing to send: a resume point can't be removed in Plex except by marking
				// the item played or unplayed, which the watch state does.
			case p.Duration <= 0 || m.offset*100 >= p.Duration*90:
				pl.skipped++ // Plex would count it as played
			case pl.scrobbles > 0 || pl.unscrobble:
				// The watch state change resets Plex's position; don't fight it.
			default:
				o := m.offset
				pl.pushOffset = &o
				pl.changes = append(pl.changes, Change{Direction: "toPlex", Kind: "position", From: clock(p.ViewOffset), To: clock(o), Conflict: mC && pC})
			}
		}
	}

	// Rating: the newer change wins.
	if cfg.Ratings {
		pl.ratingKind = true
		pr := plexRating(p.UserRating)
		if !sameRating(m.rating, pr) {
			first := !l.ratingSynced
			mC, pC := !sameRating(m.rating, l.mRating), !sameRating(pr, l.pRating)
			if first {
				mC, pC = m.rating != nil, pr != nil
			}
			if mC || pC {
				plexWins := pC && !mC
				conflict := false
				if mC && pC {
					switch {
					case first && m.rating == nil:
						plexWins = true
					case first && pr == nil:
						plexWins = false
					default:
						pT := unix(p.LastRatedAt)
						if pT.IsZero() && !first {
							pT = lastRun
						}
						plexWins = pT.After(m.ratingAt)
						conflict = !first
					}
				}
				if conflict {
					pl.conflicts++
				}
				if plexWins {
					pl.setRating, pl.newRating = true, pr
					pl.ratingAt = unix(p.LastRatedAt)
					if pl.ratingAt.IsZero() {
						pl.ratingAt = now
					}
					pl.changes = append(pl.changes, Change{Direction: "toMarquee", Kind: "rating", From: ratingText(m.rating), To: ratingText(pr), Conflict: conflict})
				} else {
					pl.pushRating, pl.plexRating = true, m.rating
					pl.changes = append(pl.changes, Change{Direction: "toPlex", Kind: "rating", From: ratingText(pr), To: ratingText(m.rating), Conflict: conflict})
				}
			}
		}
	}
	return pl
}
