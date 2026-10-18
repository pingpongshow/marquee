package notify

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"

	"marquee/internal/settings"
	"marquee/internal/webhooks"
)

// DedupWindow: the same alert (type and subject) is raised at most once in this time.
const DedupWindow = time.Hour

// Keep is how long notifications stay in the list.
const Keep = 30 * 24 * time.Hour

// Alert is something an administrator may want to know about.
type Alert struct {
	Type    string // settings.Alert*
	Subject string // what it is about: a task id, a volume, a service; used to de-duplicate
	Title   string
	Message string
	// Resolved: the problem has gone away (a volume has room again, a service is back).
	Resolved bool
}

// Notification is an alert in the in-app list.
type Notification struct {
	ID                            int64
	Type, Subject, Title, Message string
	CreatedAt                     time.Time
	Read                          bool
}

// Alerts raises alerts: every alert goes to the bell and to webhooks subscribed to "alert";
// the alert types chosen in Settings → Notifications are also mailed to the administrators.
type Alerts struct {
	DB       *sql.DB
	Settings *settings.Store
	Mail     *Mailer
	Webhooks *webhooks.Dispatcher
	Now      func() time.Time
	// Sync delivers mail before Raise returns (tests).
	Sync bool

	mu   sync.Mutex
	last map[string]time.Time
}

func (a *Alerts) now() time.Time {
	if a.Now != nil {
		return a.Now()
	}
	return time.Now()
}

// Raise records and delivers an alert, reporting whether it was raised (false when the
// same alert was raised within DedupWindow). It always reaches the bell; it's mailed when
// its type is chosen for email.
func (a *Alerts) Raise(ctx context.Context, al Alert) bool {
	if a == nil || a.Settings == nil {
		return false
	}
	cfg := a.Settings.Get()
	key := al.Type + "\x00" + al.Subject + "\x00" + fmt.Sprint(al.Resolved)
	now := a.now()
	a.mu.Lock()
	if a.last == nil {
		a.last = map[string]time.Time{}
	}
	if t, ok := a.last[key]; ok && now.Sub(t) < DedupWindow {
		a.mu.Unlock()
		return false
	}
	a.last[key] = now
	for k, t := range a.last {
		if now.Sub(t) >= DedupWindow {
			delete(a.last, k)
		}
	}
	a.mu.Unlock()

	ctx = context.WithoutCancel(ctx)
	if _, err := a.DB.ExecContext(ctx, `INSERT INTO notifications (type, subject, title, message, created_at) VALUES (?, ?, ?, ?, ?)`,
		al.Type, al.Subject, al.Title, al.Message, now.UTC().Format("2006-01-02T15:04:05.000Z")); err != nil {
		slog.Warn("store notification", "err", err)
	}
	slog.Info("alert", "type", al.Type, "subject", al.Subject, "title", al.Title)
	a.Webhooks.Publish(webhooks.Event{Event: webhooks.AlertRaised, Alert: &webhooks.Alert{
		Type: al.Type, Subject: al.Subject, Title: al.Title, Message: al.Message, Resolved: al.Resolved}})
	if a.Mail.Configured() && cfg.Notifications.AlertOn(al.Type) {
		deliver := func() {
			to, err := a.Recipients(ctx)
			if err != nil || len(to) == 0 {
				return
			}
			server := cfg.General.ServerName
			text := al.Message
			if text != "" {
				text += "\n\n"
			}
			text += "— " + server + "\n\nYou get this because you're an administrator of " + server +
				". Choose which alerts are mailed in Settings → Notifications."
			msg := Message{To: to, Subject: "[" + server + "] " + al.Title, Text: text,
				HTML: htmlMail(al.Title, []string{al.Message}, "", "", "You get this because you're an administrator of "+server+
					". Choose which alerts are mailed in Settings → Notifications.")}
			if err := a.Mail.Send(ctx, msg); err != nil {
				slog.Warn("alert mail", "err", err)
			}
		}
		if a.Sync {
			deliver()
		} else {
			go deliver()
		}
	}
	return true
}

// Recipients are the email addresses alert mail goes to: administrators with an email
// address, all of them or those chosen in the settings.
func (a *Alerts) Recipients(ctx context.Context) ([]string, error) {
	n := a.Settings.Get().Notifications
	rows, err := a.DB.QueryContext(ctx, `SELECT id, email FROM users WHERE is_admin = 1 AND email IS NOT NULL AND email != '' ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id int64
		var email string
		if err := rows.Scan(&id, &email); err != nil {
			return nil, err
		}
		if n.RecipientsAllAdmins || slices.Contains(n.Recipients, id) {
			out = append(out, email)
		}
	}
	return out, rows.Err()
}

// ---------- the in-app list ----------

// List returns the newest notifications (at most limit) and how many are unread.
func (a *Alerts) List(ctx context.Context, limit int) ([]Notification, int, error) {
	var unread int
	if err := a.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM notifications WHERE read_at IS NULL`).Scan(&unread); err != nil {
		return nil, 0, err
	}
	rows, err := a.DB.QueryContext(ctx, `SELECT id, type, subject, title, message, created_at, read_at IS NOT NULL
		FROM notifications ORDER BY id DESC LIMIT ?`, limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Notification{}
	for rows.Next() {
		var n Notification
		var created string
		if err := rows.Scan(&n.ID, &n.Type, &n.Subject, &n.Title, &n.Message, &created, &n.Read); err != nil {
			return nil, 0, err
		}
		n.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		out = append(out, n)
	}
	return out, unread, rows.Err()
}

// ErrNotFound: no such notification.
var ErrNotFound = errors.New("notification not found")

// MarkRead marks one notification read (id > 0) or all of them (id == 0).
func (a *Alerts) MarkRead(ctx context.Context, id int64) error {
	stamp := a.now().UTC().Format("2006-01-02T15:04:05.000Z")
	if id == 0 {
		_, err := a.DB.ExecContext(ctx, `UPDATE notifications SET read_at = ? WHERE read_at IS NULL`, stamp)
		return err
	}
	r, err := a.DB.ExecContext(ctx, `UPDATE notifications SET read_at = COALESCE(read_at, ?) WHERE id = ?`, stamp, id)
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Dismiss removes a notification.
func (a *Alerts) Dismiss(ctx context.Context, id int64) error {
	r, err := a.DB.ExecContext(ctx, `DELETE FROM notifications WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := r.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Prune deletes notifications older than Keep.
func (a *Alerts) Prune(ctx context.Context) {
	a.DB.ExecContext(ctx, `DELETE FROM notifications WHERE created_at < ?`, a.now().UTC().Add(-Keep).Format("2006-01-02T15:04:05.000Z"))
}

// ---------- periodic checks ----------

// Probe checks that a service Marquee uses answers.
type Probe struct {
	Name string
	// Enabled reports whether the service is set up (unconfigured services aren't checked).
	Enabled func() bool
	Check   func(ctx context.Context) error
}

// Volume is a folder whose free space is watched.
type Volume struct {
	Name, Path string
}

// Watcher runs the periodic checks: free disk space and services, every Every.
type Watcher struct {
	Alerts *Alerts
	// Volumes returns the folders to watch (config, transcode, recordings, libraries).
	Volumes func(ctx context.Context) []Volume
	Probes  []Probe
	Every   time.Duration
	// FreePercent reports a folder's free space (tests); defaults to the file system's.
	FreePercent func(path string) (float64, error)

	diskLow  map[string]bool // by device: below the threshold at the last check
	failures map[string]int  // by probe: consecutive failed checks
}

// Run checks now and then every Every until ctx ends.
func (w *Watcher) Run(ctx context.Context) {
	every := w.Every
	if every <= 0 {
		every = 15 * time.Minute
	}
	// Give services started alongside Marquee a moment.
	select {
	case <-ctx.Done():
		return
	case <-time.After(time.Minute):
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		w.Check(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// Check runs every check once.
func (w *Watcher) Check(ctx context.Context) {
	w.Alerts.Prune(ctx)
	n := w.Alerts.Settings.Get().Notifications
	// Every alert reaches the bell, so the checks always run (email follows the settings).
	if w.Volumes != nil {
		w.checkDisks(ctx, w.Volumes(ctx), float64(n.DiskThresholdPercent))
	}
	w.checkProbes(ctx)
}

func (w *Watcher) checkDisks(ctx context.Context, vols []Volume, threshold float64) {
	if w.diskLow == nil {
		w.diskLow = map[string]bool{}
	}
	free := w.FreePercent
	if free == nil {
		free = freePercent
	}
	// Several folders often share a volume: report each volume once, by its first name.
	seen := map[string]bool{}
	for _, v := range vols {
		if v.Path == "" {
			continue
		}
		dev := deviceOf(v.Path)
		if seen[dev] {
			continue
		}
		seen[dev] = true
		pct, err := free(v.Path)
		if err != nil {
			continue
		}
		low := pct < threshold
		subject := v.Name + " (" + v.Path + ")"
		switch {
		case low && !w.diskLow[dev]:
			w.Alerts.Raise(ctx, Alert{Type: settings.AlertDiskLow, Subject: subject,
				Title:   "Disk space low: " + v.Name,
				Message: fmt.Sprintf("%s has %.1f%% free space left (the alert is set at %.0f%%).", v.Path, pct, threshold)})
		case !low && w.diskLow[dev]:
			w.Alerts.Raise(ctx, Alert{Type: settings.AlertDiskLow, Subject: subject, Resolved: true,
				Title:   "Disk space OK again: " + v.Name,
				Message: fmt.Sprintf("%s has %.1f%% free space.", v.Path, pct)})
		}
		w.diskLow[dev] = low
	}
}

// A service must fail twice in a row (one check apart) before it's reported, so a restart
// doesn't raise an alert.
const probeFailures = 2

func (w *Watcher) checkProbes(ctx context.Context) {
	if w.failures == nil {
		w.failures = map[string]int{}
	}
	for _, p := range w.Probes {
		if p.Enabled != nil && !p.Enabled() {
			delete(w.failures, p.Name)
			continue
		}
		cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		err := p.Check(cctx)
		cancel()
		if err != nil {
			w.failures[p.Name]++
			if w.failures[p.Name] == probeFailures {
				w.Alerts.Raise(ctx, Alert{Type: settings.AlertIntegrationDown, Subject: p.Name,
					Title:   p.Name + " isn't answering",
					Message: fmt.Sprintf("Marquee couldn't reach %s: %s", p.Name, shorten(err.Error(), 300))})
			}
			continue
		}
		if w.failures[p.Name] >= probeFailures {
			w.Alerts.Raise(ctx, Alert{Type: settings.AlertIntegrationDown, Subject: p.Name, Resolved: true,
				Title: p.Name + " is answering again", Message: "Marquee can reach " + p.Name + " again."})
		}
		delete(w.failures, p.Name)
	}
}

func shorten(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "…"
}
