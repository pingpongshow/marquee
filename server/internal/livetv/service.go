package livetv

import (
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"marquee/internal/settings"
)

// GuideDays is how far ahead the guide is kept.
const GuideDays = 2

var ErrNotFound = errors.New("channel not found")

// Service keeps channels and the guide, and runs live streams.
type Service struct {
	DB       *sql.DB
	Settings *settings.Store
	HTTP     *http.Client
	// Sessions plays channels; set by the caller.
	Sessions *Sessions

	refreshing sync.Mutex
}

func (s *Service) client() *http.Client {
	if s.HTTP != nil {
		return s.HTTP
	}
	return &http.Client{Timeout: 2 * time.Minute}
}

// Sources are the enabled sources, with Dispatcharr's addresses filled in.
func (s *Service) Sources() []settings.LiveTVSource {
	var out []settings.LiveTVSource
	for _, src := range s.Settings.Get().Integrations.LiveTVSources {
		if !src.Enabled || src.URL == "" {
			continue
		}
		if src.Kind == "dispatcharr" {
			base := strings.TrimRight(src.URL, "/")
			src.URL = base + "/output/m3u"
			if src.EPGURL == "" {
				src.EPGURL = base + "/output/epg"
			}
		}
		out = append(out, src)
	}
	return out
}

func (s *Service) Enabled() bool { return len(s.Sources()) > 0 }

func (s *Service) get(ctx context.Context, url, ua string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	if ua != "" {
		req.Header.Set("User-Agent", ua)
	}
	resp, err := s.client().Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		resp.Body.Close()
		return nil, fmt.Errorf("%s answered %d", url, resp.StatusCode)
	}
	return resp.Body, nil
}

// sortKey orders channel numbers like 4 < 4.1 < 10; channels without one go last.
func sortKey(number string, fallback int) float64 {
	if f, err := strconv.ParseFloat(strings.TrimSpace(number), 64); err == nil {
		return f
	}
	return 1e6 + float64(fallback)
}

// Refresh reloads every source: channels from the playlist and the next GuideDays of guide.
func (s *Service) Refresh(ctx context.Context) {
	s.refreshing.Lock()
	defer s.refreshing.Unlock()
	sources := s.Sources()
	ids := map[string]bool{}
	for _, src := range sources {
		ids[src.ID] = true
		ch, pr, err := s.refreshSource(ctx, src)
		msg := ""
		if err != nil {
			msg = err.Error()
			slog.Warn("live TV source failed", "source", src.Name, "err", err)
		} else {
			slog.Info("live TV source loaded", "source", src.Name, "channels", ch, "programmes", pr)
		}
		s.DB.ExecContext(ctx, `INSERT INTO live_sources (id, refreshed_at, channels, programmes, error) VALUES (?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), ?, ?, ?)
			ON CONFLICT(id) DO UPDATE SET refreshed_at = excluded.refreshed_at, channels = excluded.channels, programmes = excluded.programmes, error = excluded.error`,
			src.ID, ch, pr, msg)
	}
	// Channels of sources that were removed or turned off disappear.
	rows, err := s.DB.QueryContext(ctx, `SELECT DISTINCT source_id FROM live_channels`)
	if err == nil {
		var gone []string
		for rows.Next() {
			var id string
			if rows.Scan(&id) == nil && !ids[id] {
				gone = append(gone, id)
			}
		}
		rows.Close()
		for _, id := range gone {
			s.DB.ExecContext(ctx, `DELETE FROM live_channels WHERE source_id = ?`, id)
			s.DB.ExecContext(ctx, `DELETE FROM live_sources WHERE id = ?`, id)
		}
	}
	s.DB.ExecContext(ctx, `DELETE FROM live_programmes WHERE stop < ?`, time.Now().UTC().Add(-6*time.Hour).Format(time.RFC3339))
}

func (s *Service) refreshSource(ctx context.Context, src settings.LiveTVSource) (channels, programmes int, err error) {
	body, err := s.get(ctx, src.URL, src.UserAgent)
	if err != nil {
		return 0, 0, err
	}
	pl, err := ParseM3U(body)
	body.Close()
	if err != nil {
		return 0, 0, fmt.Errorf("playlist: %w", err)
	}
	if len(pl.Entries) == 0 {
		return 0, 0, errors.New("the playlist has no channels")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback()
	tx.ExecContext(ctx, `UPDATE live_channels SET present = 0 WHERE source_id = ?`, src.ID)
	byEPG := map[string][]int64{} // guide channel id -> channels
	for i, e := range pl.Entries {
		key := e.TvgID
		if key == "" {
			h := sha1.Sum([]byte(e.URL))
			key = "url:" + hex.EncodeToString(h[:8])
		}
		var id int64
		err := tx.QueryRowContext(ctx, `INSERT INTO live_channels (source_id, source_key, epg_id, number, sort_key, name, group_name, logo_url, stream_url, present)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 1)
			ON CONFLICT(source_id, source_key) DO UPDATE SET epg_id = excluded.epg_id, number = excluded.number, sort_key = excluded.sort_key,
				name = excluded.name, group_name = excluded.group_name, logo_url = excluded.logo_url, stream_url = excluded.stream_url, present = 1
			RETURNING id`,
			src.ID, key, e.TvgID, e.Number, sortKey(e.Number, i), e.Name, e.Group, e.Logo, e.URL).Scan(&id)
		if err != nil {
			return 0, 0, err
		}
		if e.TvgID != "" {
			byEPG[e.TvgID] = append(byEPG[e.TvgID], id)
		}
		channels++
	}
	if err := tx.Commit(); err != nil {
		return 0, 0, err
	}

	epg := src.EPGURL
	if epg == "" {
		epg = pl.EPG
	}
	if epg == "" {
		return channels, 0, nil
	}
	body, err = s.get(ctx, epg, src.UserAgent)
	if err != nil {
		return channels, 0, fmt.Errorf("guide: %w", err)
	}
	defer body.Close()
	now := time.Now().UTC()
	from, until := now.Add(-3*time.Hour), now.Add(GuideDays*24*time.Hour)
	tx, err = s.DB.BeginTx(ctx, nil)
	if err != nil {
		return channels, 0, err
	}
	defer tx.Rollback()
	// Replace the guide for this source's channels.
	tx.ExecContext(ctx, `DELETE FROM live_programmes WHERE channel_id IN (SELECT id FROM live_channels WHERE source_id = ?)`, src.ID)
	ins, err := tx.PrepareContext(ctx, `INSERT INTO live_programmes (channel_id, start, stop, title, subtitle, description, category, episode, image_url) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return channels, 0, err
	}
	defer ins.Close()
	var insErr error
	guideChans, err := ParseXMLTV(body, from, until, func(p Programme) {
		for _, id := range byEPG[p.Channel] {
			if _, e := ins.ExecContext(ctx, id, p.Start.Format(time.RFC3339), p.Stop.Format(time.RFC3339), p.Title, p.Subtitle, p.Description, p.Category, p.Episode, p.Image); e != nil && insErr == nil {
				insErr = e
			}
			programmes++
		}
	})
	if err != nil {
		return channels, programmes, fmt.Errorf("guide: %w", err)
	}
	if insErr != nil {
		return channels, programmes, insErr
	}
	// Logos from the guide fill in channels whose playlist entry had none.
	for _, g := range guideChans {
		if g.Icon == "" {
			continue
		}
		for _, id := range byEPG[g.ID] {
			tx.ExecContext(ctx, `UPDATE live_channels SET logo_url = ? WHERE id = ? AND logo_url = ''`, g.Icon, id)
		}
	}
	return channels, programmes, tx.Commit()
}

// Run refreshes at start and every few hours until ctx ends; wake asks for a refresh now.
func (s *Service) Run(ctx context.Context, wake <-chan struct{}) {
	t := time.NewTicker(4 * time.Hour)
	defer t.Stop()
	for {
		if s.Enabled() {
			s.Refresh(ctx)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-wake:
		}
	}
}

// Channel is a channel for listing.
type Channel struct {
	ID         int64
	Number     string
	Name       string
	Group      string
	HasLogo    bool
	Favorite   bool
	Now, Next  *Prog
	StreamURL  string
	SourceID   string
	LogoSource string
}

// Prog is a programme for listing.
type Prog struct {
	ID                                                     int64
	Start, Stop                                            time.Time
	Title, Subtitle, Description, Category, Episode, Image string
}

// Filter narrows channel lists.
type Filter struct {
	Group     string
	Favorites bool
}

func (s *Service) channels(ctx context.Context, userID int64, f Filter) ([]Channel, error) {
	q := `SELECT c.id, c.number, c.name, c.group_name, c.logo_url, c.stream_url, c.source_id, f.user_id IS NOT NULL
		FROM live_channels c LEFT JOIN live_favorites f ON f.channel_id = c.id AND f.user_id = ?
		WHERE c.present = 1`
	args := []any{userID}
	if f.Group != "" {
		q += ` AND c.group_name = ?`
		args = append(args, f.Group)
	}
	if f.Favorites {
		q += ` AND f.user_id IS NOT NULL`
	}
	rows, err := s.DB.QueryContext(ctx, q+` ORDER BY c.sort_key, c.name`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Channel
	for rows.Next() {
		var c Channel
		if err := rows.Scan(&c.ID, &c.Number, &c.Name, &c.Group, &c.LogoSource, &c.StreamURL, &c.SourceID, &c.Favorite); err != nil {
			return nil, err
		}
		c.HasLogo = c.LogoSource != ""
		out = append(out, c)
	}
	return out, rows.Err()
}

func scanProg(rows *sql.Rows) (int64, Prog, error) {
	var ch int64
	var p Prog
	var start, stop string
	err := rows.Scan(&ch, &p.ID, &start, &stop, &p.Title, &p.Subtitle, &p.Description, &p.Category, &p.Episode, &p.Image)
	p.Start, _ = time.Parse(time.RFC3339, start)
	p.Stop, _ = time.Parse(time.RFC3339, stop)
	return ch, p, err
}

const progCols = `channel_id, id, start, stop, title, subtitle, description, category, episode, image_url`

// Channels lists channels with what's on now and next.
func (s *Service) Channels(ctx context.Context, userID int64, f Filter) ([]Channel, error) {
	list, err := s.channels(ctx, userID, f)
	if err != nil || len(list) == 0 {
		return list, err
	}
	now := time.Now().UTC()
	idx := map[int64]int{}
	for i, c := range list {
		idx[c.ID] = i
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+progCols+` FROM live_programmes WHERE stop > ? AND start < ? ORDER BY channel_id, start`,
		now.Format(time.RFC3339), now.Add(12*time.Hour).Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		ch, p, err := scanProg(rows)
		if err != nil {
			return nil, err
		}
		i, ok := idx[ch]
		if !ok {
			continue
		}
		c := &list[i]
		switch {
		case !p.Start.After(now) && c.Now == nil:
			pp := p
			c.Now = &pp
		case p.Start.After(now) && c.Next == nil:
			pp := p
			c.Next = &pp
		}
	}
	return list, rows.Err()
}

// Row is one channel's programmes in a guide window.
type Row struct {
	ChannelID  int64
	Programmes []Prog
}

// Guide returns the programmes overlapping [from, to) for the channels the filter picks.
func (s *Service) Guide(ctx context.Context, userID int64, f Filter, from, to time.Time) ([]Row, error) {
	list, err := s.channels(ctx, userID, f)
	if err != nil {
		return nil, err
	}
	out := make([]Row, len(list))
	idx := map[int64]int{}
	for i, c := range list {
		out[i] = Row{ChannelID: c.ID, Programmes: []Prog{}}
		idx[c.ID] = i
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+progCols+` FROM live_programmes WHERE stop > ? AND start < ? ORDER BY channel_id, start`,
		from.UTC().Format(time.RFC3339), to.UTC().Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		ch, p, err := scanProg(rows)
		if err != nil {
			return nil, err
		}
		if i, ok := idx[ch]; ok {
			out[i].Programmes = append(out[i].Programmes, p)
		}
	}
	return out, rows.Err()
}

// Groups lists channel groups with counts.
func (s *Service) Groups(ctx context.Context) (map[string]int, []string, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT group_name, COUNT(*) FROM live_channels WHERE present = 1 AND group_name != '' GROUP BY group_name ORDER BY MIN(sort_key)`)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	counts := map[string]int{}
	var order []string
	for rows.Next() {
		var g string
		var n int
		if rows.Scan(&g, &n) == nil {
			counts[g] = n
			order = append(order, g)
		}
	}
	return counts, order, rows.Err()
}

// Channel looks one channel up.
func (s *Service) Channel(ctx context.Context, id int64) (Channel, error) {
	var c Channel
	err := s.DB.QueryRowContext(ctx, `SELECT id, number, name, group_name, logo_url, stream_url, source_id FROM live_channels WHERE id = ?`, id).
		Scan(&c.ID, &c.Number, &c.Name, &c.Group, &c.LogoSource, &c.StreamURL, &c.SourceID)
	if errors.Is(err, sql.ErrNoRows) {
		return c, ErrNotFound
	}
	c.HasLogo = c.LogoSource != ""
	return c, err
}

// UserAgent is the source's user agent for a channel.
func (s *Service) UserAgent(sourceID string) string {
	for _, src := range s.Settings.Get().Integrations.LiveTVSources {
		if src.ID == sourceID {
			return src.UserAgent
		}
	}
	return ""
}

func (s *Service) SetFavorite(ctx context.Context, userID, channelID int64, on bool) error {
	if !on {
		_, err := s.DB.ExecContext(ctx, `DELETE FROM live_favorites WHERE user_id = ? AND channel_id = ?`, userID, channelID)
		return err
	}
	if _, err := s.Channel(ctx, channelID); err != nil {
		return err
	}
	_, err := s.DB.ExecContext(ctx, `INSERT OR IGNORE INTO live_favorites (user_id, channel_id) VALUES (?, ?)`, userID, channelID)
	return err
}

// SourceStatus is what the last refresh of a source found.
type SourceStatus struct {
	ID, Name             string
	Channels, Programmes int
	RefreshedAt          *time.Time
	Error                string
}

// Status reports the channel count, the end of the guide and each source.
func (s *Service) Status(ctx context.Context) (channels int, guideUntil *time.Time, sources []SourceStatus) {
	s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM live_channels WHERE present = 1`).Scan(&channels)
	var until sql.NullString
	s.DB.QueryRowContext(ctx, `SELECT MAX(stop) FROM live_programmes`).Scan(&until)
	if t, err := time.Parse(time.RFC3339, until.String); err == nil {
		guideUntil = &t
	}
	for _, src := range s.Sources() {
		st := SourceStatus{ID: src.ID, Name: src.Name}
		var at sql.NullString
		s.DB.QueryRowContext(ctx, `SELECT refreshed_at, channels, programmes, error FROM live_sources WHERE id = ?`, src.ID).Scan(&at, &st.Channels, &st.Programmes, &st.Error)
		if t, err := time.Parse(time.RFC3339Nano, at.String); err == nil {
			st.RefreshedAt = &t
		}
		sources = append(sources, st)
	}
	return
}

// LogoDir caches channel logos; set by the caller.
var LogoDir string

// Logo returns a channel's logo, fetched from its source once and kept on disk.
func (s *Service) Logo(ctx context.Context, id int64) ([]byte, string, error) {
	c, err := s.Channel(ctx, id)
	if err != nil {
		return nil, "", err
	}
	if c.LogoSource == "" {
		return nil, "", ErrNotFound
	}
	h := sha1.Sum([]byte(c.LogoSource))
	path := fmt.Sprintf("%s/%d-%s", LogoDir, id, hex.EncodeToString(h[:6]))
	if b, err := os.ReadFile(path); err == nil {
		return b, imageType(b), nil
	}
	body, err := s.get(ctx, c.LogoSource, s.UserAgent(c.SourceID))
	if err != nil {
		return nil, "", err
	}
	defer body.Close()
	b, err := io.ReadAll(io.LimitReader(body, 4<<20))
	if err != nil {
		return nil, "", err
	}
	ct := imageType(b)
	if !strings.HasPrefix(ct, "image/") {
		return nil, "", ErrNotFound
	}
	os.MkdirAll(LogoDir, 0o755)
	os.WriteFile(path, b, 0o644)
	return b, ct, nil
}

// imageType sniffs an image's type, recognising SVG (which sniffing calls text).
func imageType(b []byte) string {
	if strings.Contains(string(b[:min(len(b), 512)]), "<svg") {
		return "image/svg+xml"
	}
	return http.DetectContentType(b)
}
