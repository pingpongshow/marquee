// Package webhooks tells other services what Marquee is doing (ADM-5): playback starting,
// pausing, resuming, stopping and finishing, and new titles in libraries. Each webhook gets
// a JSON POST signed with HMAC-SHA256 (X-Marquee-Signature) when it has a secret, and
// failed deliveries are retried twice.
package webhooks

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"time"

	"marquee/internal/settings"
)

// Event kinds.
const (
	PlaybackStarted = "playback.started"
	PlaybackPaused  = "playback.paused"
	PlaybackResumed = "playback.resumed"
	PlaybackStopped = "playback.stopped"
	PlaybackWatched = "playback.watched"
	LibraryAdded    = "library.added"
	Test            = "test"
)

// Kinds lists the events a webhook can subscribe to.
var Kinds = []string{PlaybackStarted, PlaybackPaused, PlaybackResumed, PlaybackStopped, PlaybackWatched, LibraryAdded}

type Server struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Version string `json:"version"`
}

type User struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type Device struct {
	Name     string `json:"name,omitempty"`
	Platform string `json:"platform,omitempty"`
}

type Item struct {
	ID          int64             `json:"id"`
	LibraryID   int64             `json:"libraryId"`
	Type        string            `json:"type"`
	Title       string            `json:"title"`
	Year        int               `json:"year,omitempty"`
	Show        string            `json:"show,omitempty"`   // episodes: the show; tracks: the artist
	Parent      string            `json:"parent,omitempty"` // episodes: the season; tracks: the album
	Season      *int              `json:"season,omitempty"`
	Episode     *int              `json:"episode,omitempty"`
	DurationMS  int64             `json:"durationMs,omitempty"`
	ExternalIDs map[string]string `json:"externalIds,omitempty"`
}

type Playback struct {
	SessionID  string `json:"sessionId"`
	PositionMS int64  `json:"positionMs"`
	Method     string `json:"method"` // direct_play, direct_stream, transcode
	Remote     bool   `json:"remote"`
}

// Event is one notification. Item is set for playback events; Items for library.added.
type Event struct {
	Event    string    `json:"event"`
	Time     time.Time `json:"time"`
	Server   Server    `json:"server"`
	User     *User     `json:"user,omitempty"`
	Device   *Device   `json:"device,omitempty"`
	Item     *Item     `json:"item,omitempty"`
	Items    []Item    `json:"items,omitempty"`
	Playback *Playback `json:"playback,omitempty"`
}

type delivery struct {
	hook settings.Webhook
	body []byte
}

// Dispatcher sends events to the configured webhooks in the background.
type Dispatcher struct {
	DB       *sql.DB
	Settings *settings.Store
	Server   func() Server
	HTTP     *http.Client

	queue chan delivery
}

func (d *Dispatcher) client() *http.Client {
	if d.HTTP != nil {
		return d.HTTP
	}
	return &http.Client{Timeout: 10 * time.Second}
}

// Run delivers queued events until ctx ends.
func (d *Dispatcher) Run(ctx context.Context) {
	d.init()
	for {
		select {
		case <-ctx.Done():
			return
		case job := <-d.queue:
			for attempt, wait := range []time.Duration{0, 2 * time.Second, 15 * time.Second} {
				if wait > 0 {
					select {
					case <-ctx.Done():
						return
					case <-time.After(wait):
					}
				}
				status, err := d.post(ctx, job.hook, job.body)
				if err == nil && status < 300 {
					break
				}
				if attempt == 2 || (status >= 400 && status < 500 && status != 429) {
					slog.Warn("webhook delivery failed", "webhook", job.hook.Name, "status", status, "err", err)
					break
				}
			}
		}
	}
}

func (d *Dispatcher) init() {
	if d.queue == nil {
		d.queue = make(chan delivery, 256)
	}
}

// Publish queues ev for every enabled webhook subscribed to it. It never blocks: when the
// queue is full (a webhook is down and events pile up) the event is dropped.
func (d *Dispatcher) Publish(ev Event) {
	if d == nil {
		return
	}
	d.init()
	hooks := d.Settings.Get().Webhooks
	if len(hooks) == 0 {
		return
	}
	ev.Time = time.Now().UTC()
	if d.Server != nil {
		ev.Server = d.Server()
	}
	body, err := json.Marshal(ev)
	if err != nil {
		return
	}
	for _, h := range hooks {
		if !h.Enabled || !slices.Contains(h.Events, ev.Event) {
			continue
		}
		select {
		case d.queue <- delivery{h, body}:
		default:
			slog.Warn("webhook queue full; dropping event", "webhook", h.Name, "event", ev.Event)
		}
	}
}

// SendTest posts a test event to a webhook now and reports the HTTP status.
func (d *Dispatcher) SendTest(ctx context.Context, h settings.Webhook) (int, error) {
	ev := Event{Event: Test, Time: time.Now().UTC()}
	if d.Server != nil {
		ev.Server = d.Server()
	}
	body, _ := json.Marshal(ev)
	return d.post(ctx, h, body)
}

// Sign is the X-Marquee-Signature value for body.
func Sign(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func (d *Dispatcher) post(ctx context.Context, h settings.Webhook, body []byte) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.URL, bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Marquee-Webhook/1")
	if h.Secret != "" {
		req.Header.Set("X-Marquee-Signature", Sign(h.Secret, body))
	}
	resp, err := d.client().Do(req)
	if err != nil {
		return 0, err
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		return resp.StatusCode, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return resp.StatusCode, nil
}

// LookupItem describes an item for a payload.
func (d *Dispatcher) LookupItem(ctx context.Context, id int64) *Item {
	var it Item
	var year, idx, pidx sql.NullInt64
	var show, parent sql.NullString
	var dur sql.NullInt64
	err := d.DB.QueryRowContext(ctx, `SELECT i.id, i.library_id, i.type, i.title, i.year, i.idx, p.idx, p.title, g.title, i.duration_ms
		FROM items i LEFT JOIN items p ON p.id = i.parent_id LEFT JOIN items g ON g.id = i.grandparent_id WHERE i.id = ?`, id).
		Scan(&it.ID, &it.LibraryID, &it.Type, &it.Title, &year, &idx, &pidx, &parent, &show, &dur)
	if err != nil {
		return nil
	}
	it.Year, it.Show, it.Parent, it.DurationMS = int(year.Int64), show.String, parent.String, dur.Int64
	if it.Type == "episode" {
		s, e := int(pidx.Int64), int(idx.Int64)
		it.Season, it.Episode = &s, &e
	}
	rows, err := d.DB.QueryContext(ctx, `SELECT provider, value FROM external_ids WHERE item_id = ?`, id)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var p, v string
			if rows.Scan(&p, &v) == nil {
				if it.ExternalIDs == nil {
					it.ExternalIDs = map[string]string{}
				}
				it.ExternalIDs[p] = v
			}
		}
	}
	return &it
}

// AnnounceAdded publishes library.added for titles added to a library since a time
// (movies, episodes, albums and videos; at most 100).
func (d *Dispatcher) AnnounceAdded(ctx context.Context, libID int64, since time.Time) {
	if d == nil || len(d.Settings.Get().Webhooks) == 0 {
		return
	}
	rows, err := d.DB.QueryContext(ctx, `SELECT id FROM items WHERE library_id = ? AND added_at > ?
		AND type IN ('movie', 'episode', 'album', 'video') ORDER BY added_at LIMIT 100`, libID, since.UTC().Format("2006-01-02T15:04:05.000Z"))
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
	if len(ids) == 0 {
		return
	}
	ev := Event{Event: LibraryAdded}
	for _, id := range ids {
		if it := d.LookupItem(ctx, id); it != nil {
			ev.Items = append(ev.Items, *it)
		}
	}
	d.Publish(ev)
}

// DevicePlatform names a device's platform for payloads.
func (d *Dispatcher) DevicePlatform(ctx context.Context, id int64) string {
	var p string
	d.DB.QueryRowContext(ctx, `SELECT platform FROM devices WHERE id = ?`, id).Scan(&p)
	return p
}
