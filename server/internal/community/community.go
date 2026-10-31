// Package community fetches what listeners elsewhere say about the library's music
// (MUSIC-15): similar artists and community tags from ListenBrainz (free and open; artists
// by their MusicBrainz id) and, with the owner's API key, Last.fm (similar artists, and
// artist and track tags). Only artist and track names and MusicBrainz ids are sent. Soundprint
// uses the results for radios, mixes, moods and Muse.
package community

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"marquee/internal/scanner/naming"
)

// Sources, as stored in music_similar_artists and music_tags.
const (
	SourceListenBrainz = "listenbrainz"
	SourceLastFM       = "lastfm"
)

// What music_community_checked marks as looked up.
const (
	checkLBSimilar  = "listenbrainz-similar"
	checkLBTags     = "listenbrainz-tags"
	checkLFMArtist  = "lastfm-artist"
	checkLFMTrack   = "lastfm-track"
	artistRefresh   = "-90 days"
	trackRefresh    = "-180 days"
	similarKept     = 50 // similar artists kept per artist (those in the library)
	tagsKept        = 20 // tags kept per artist or track
	lbTagBatch      = 25 // artists or recordings per ListenBrainz metadata request
	minLastFMTagPct = 10 // Last.fm tag counts are 0–100 (the strongest 100)
)

// similarAlgorithm is the ListenBrainz similar-artists dataset: sessions of the last 7,500
// days, artists played together in a listening session.
const similarAlgorithm = "session_based_days_7500_session_300_contribution_5_threshold_10_limit_100_filter_True_skip_30"

// Service runs the fetch task.
type Service struct {
	DB *sql.DB
	// Enabled reports the Music → similar artists and tags setting.
	Enabled func() bool
	// LastFMKey is the owner's Last.fm API key ("" = Last.fm isn't used).
	LastFMKey func() string
	// Loaded is called after new results are stored (Soundprint reloads them).
	Loaded    func(ctx context.Context)
	UserAgent string
	HTTP      *http.Client
	// Bases, for tests.
	ListenBrainz, Labs, LastFM string
	// Pace is the pause between requests to one service.
	Pace time.Duration

	mu          sync.Mutex
	done, total int
}

func (s *Service) client() *http.Client {
	if s.HTTP != nil {
		return s.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func or(v, def string) string {
	if v == "" {
		return def
	}
	return v
}

// Progress reports the running task's progress.
func (s *Service) Progress() (int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.done, s.total
}

func (s *Service) step(total int) {
	s.mu.Lock()
	s.done, s.total = 0, total
	s.mu.Unlock()
}

func (s *Service) advance() {
	s.mu.Lock()
	s.done++
	s.mu.Unlock()
}

// errNotFound is a service that has nothing on an artist or track.
var errNotFound = errors.New("not found")

func (s *Service) get(ctx context.Context, u string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", or(s.UserAgent, "Marquee (self-hosted media server)"))
	resp, err := s.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return errNotFound
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return fmt.Errorf("rate limited")
	}
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (s *Service) pause(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(s.Pace):
		return nil
	}
}

// Run looks up the artists and tracks not looked up recently. It continues where it stopped.
func (s *Service) Run(ctx context.Context) (string, error) {
	if s.Enabled != nil && !s.Enabled() {
		return "Similar artists and tags are turned off", nil
	}
	lib, err := s.loadLibrary(ctx)
	if err != nil {
		return "", err
	}
	var parts []string
	stored := false
	report := func(n int, what string, err error) error {
		if n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, what))
			stored = true
		}
		return err
	}
	defer func() {
		if stored && s.Loaded != nil {
			s.Loaded(context.WithoutCancel(ctx))
		}
	}()
	if err := report(s.lbSimilar(ctx, lib)); err != nil {
		return strings.Join(parts, ", "), err
	}
	if err := report(s.lbArtistTags(ctx, lib)); err != nil {
		return strings.Join(parts, ", "), err
	}
	if err := report(s.lbRecordingTags(ctx)); err != nil {
		return strings.Join(parts, ", "), err
	}
	if key := s.key(); key != "" {
		if err := report(s.lfmArtists(ctx, lib, key)); err != nil {
			return strings.Join(parts, ", "), err
		}
		if err := report(s.lfmTracks(ctx, key)); err != nil {
			return strings.Join(parts, ", "), err
		}
	}
	if len(parts) == 0 {
		return "Nothing new to look up", nil
	}
	return "Looked up " + strings.Join(parts, ", "), nil
}

func (s *Service) key() string {
	if s.LastFMKey == nil {
		return ""
	}
	return strings.TrimSpace(s.LastFMKey())
}

// library is the music artists, by MusicBrainz id and by name, for matching what the
// services return.
type library struct {
	byMBID map[string]int64
	byName map[string]int64
}

func (s *Service) loadLibrary(ctx context.Context) (library, error) {
	l := library{byMBID: map[string]int64{}, byName: map[string]int64{}}
	rows, err := s.DB.QueryContext(ctx, `SELECT a.id, a.title, COALESCE(x.value, '') FROM items a
		LEFT JOIN external_ids x ON x.item_id = a.id AND x.provider = 'musicbrainz' WHERE a.type = 'artist'`)
	if err != nil {
		return l, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var name, mbid string
		if rows.Scan(&id, &name, &mbid) != nil {
			continue
		}
		if mbid != "" {
			l.byMBID[mbid] = id
		}
		if k := nameKey(name); k != "" && l.byName[k] == 0 {
			l.byName[k] = id
		}
	}
	return l, rows.Err()
}

func (l library) match(mbid, name string) int64 {
	if id := l.byMBID[mbid]; id != 0 && mbid != "" {
		return id
	}
	return l.byName[nameKey(name)]
}

// nameKey compares artist names without case, punctuation or accents (Last.fm writes "Bjork"
// for Björk).
func nameKey(name string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(name) {
		if !unicode.Is(unicode.Mn, r) {
			b.WriteRune(r)
		}
	}
	return naming.Normalize(b.String())
}

type todoItem struct {
	id               int64
	name, mbid, more string // more: a track's artist name
}

// due lists the items of a type not looked up for this check within refresh.
func (s *Service) due(ctx context.Context, q string, args ...any) ([]todoItem, error) {
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []todoItem
	for rows.Next() {
		var t todoItem
		if rows.Scan(&t.id, &t.name, &t.mbid, &t.more) == nil {
			out = append(out, t)
		}
	}
	return out, rows.Err()
}

const dueArtists = `SELECT a.id, a.title, COALESCE(x.value, ''), '' FROM items a
	LEFT JOIN external_ids x ON x.item_id = a.id AND x.provider = 'musicbrainz'
	LEFT JOIN music_community_checked c ON c.item_id = a.id AND c.source = ?
	WHERE a.type = 'artist' AND (c.checked_at IS NULL OR c.checked_at < strftime('%Y-%m-%dT%H:%M:%fZ', 'now', ?))`

func (s *Service) checked(ctx context.Context, id int64, check string, err error) {
	msg := ""
	if err != nil && !errors.Is(err, errNotFound) {
		msg = err.Error()
	}
	s.DB.ExecContext(ctx, `INSERT INTO music_community_checked(item_id, source, checked_at, error) VALUES (?, ?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), ?)
		ON CONFLICT(item_id, source) DO UPDATE SET checked_at = excluded.checked_at, error = excluded.error`, id, check, msg)
}

type similar struct {
	id    int64
	score float64
}

// setSimilar replaces an artist's similar artists from one source (scores scaled so the
// strongest is 1).
func (s *Service) setSimilar(ctx context.Context, artist int64, source string, list []similar) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM music_similar_artists WHERE artist_id = ? AND source = ?`, artist, source); err != nil {
		return err
	}
	top := 0.0
	for _, x := range list {
		top = max(top, x.score)
	}
	seen := map[int64]bool{}
	n := 0
	for _, x := range list {
		if x.id == artist || seen[x.id] || top <= 0 || n >= similarKept {
			continue
		}
		seen[x.id] = true
		n++
		if _, err := tx.ExecContext(ctx, `INSERT INTO music_similar_artists(artist_id, similar_id, source, score) VALUES (?, ?, ?, ?)`,
			artist, x.id, source, x.score/top); err != nil {
			return err
		}
	}
	return tx.Commit()
}

type tagCount struct {
	tag   string
	count float64
}

// setTags replaces an item's tags from one source: the strongest tagsKept, scaled so the
// strongest is 1.
func (s *Service) setTags(ctx context.Context, item int64, source string, tags []tagCount) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM music_tags WHERE item_id = ? AND source = ?`, item, source); err != nil {
		return err
	}
	merged := map[string]float64{}
	var order []string
	top := 0.0
	for _, t := range tags {
		tag := strings.Join(strings.Fields(strings.ToLower(strings.ReplaceAll(t.tag, "-", " "))), " ")
		if tag == "" || t.count <= 0 {
			continue
		}
		if _, ok := merged[tag]; !ok {
			order = append(order, tag)
		}
		merged[tag] += t.count
		top = max(top, merged[tag])
	}
	sortByCount(order, merged)
	for i, tag := range order {
		if i >= tagsKept {
			break
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO music_tags(item_id, tag, source, weight) VALUES (?, ?, ?, ?)`, item, tag, source, merged[tag]/top); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func sortByCount(order []string, counts map[string]float64) {
	for i := 1; i < len(order); i++ {
		for j := i; j > 0 && counts[order[j]] > counts[order[j-1]]; j-- {
			order[j], order[j-1] = order[j-1], order[j]
		}
	}
}

// ---------- ListenBrainz ----------

// lbSimilar fetches each artist's similar artists (artists with a MusicBrainz id).
func (s *Service) lbSimilar(ctx context.Context, lib library) (int, string, error) {
	todo, err := s.due(ctx, dueArtists+` AND x.value IS NOT NULL ORDER BY a.id`, checkLBSimilar, artistRefresh)
	if err != nil {
		return 0, "", err
	}
	s.step(len(todo))
	n := 0
	base := or(s.Labs, "https://labs.api.listenbrainz.org")
	for _, a := range todo {
		if err := s.pause(ctx); err != nil {
			return n, "artists' similar artists (ListenBrainz)", err
		}
		var out []struct {
			MBID  string  `json:"artist_mbid"`
			Name  string  `json:"name"`
			Score float64 `json:"score"`
		}
		err := s.get(ctx, base+"/similar-artists/json?artist_mbids="+url.QueryEscape(a.mbid)+"&algorithm="+similarAlgorithm, &out)
		if err == nil {
			var list []similar
			for _, x := range out {
				if id := lib.match(x.MBID, x.Name); id != 0 {
					list = append(list, similar{id, x.Score})
				}
			}
			err = s.setSimilar(ctx, a.id, SourceListenBrainz, list)
			n++
		} else if !errors.Is(err, errNotFound) {
			slog.Debug("listenbrainz similar artists", "artist", a.name, "err", err)
		}
		s.checked(ctx, a.id, checkLBSimilar, err)
		s.advance()
	}
	return n, "artists' similar artists (ListenBrainz)", nil
}

type lbTag struct {
	Tag   string  `json:"tag"`
	Count float64 `json:"count"`
}

func lbTags(in []lbTag) []tagCount {
	out := make([]tagCount, 0, len(in))
	for _, t := range in {
		out = append(out, tagCount{t.Tag, t.Count})
	}
	return out
}

// lbArtistTags fetches artists' tags, a batch at a time.
func (s *Service) lbArtistTags(ctx context.Context, lib library) (int, string, error) {
	todo, err := s.due(ctx, dueArtists+` AND x.value IS NOT NULL ORDER BY a.id`, checkLBTags, artistRefresh)
	if err != nil {
		return 0, "", err
	}
	s.step(len(todo))
	n := 0
	base := or(s.ListenBrainz, "https://api.listenbrainz.org")
	for i := 0; i < len(todo); i += lbTagBatch {
		if err := s.pause(ctx); err != nil {
			return n, "artists' tags (ListenBrainz)", err
		}
		batch := todo[i:min(i+lbTagBatch, len(todo))]
		ids := make([]string, len(batch))
		for j, a := range batch {
			ids[j] = a.mbid
		}
		var out []struct {
			MBID string `json:"artist_mbid"`
			Tag  struct {
				Artist []lbTag `json:"artist"`
			} `json:"tag"`
		}
		err := s.get(ctx, base+"/1/metadata/artist/?inc=tag&artist_mbids="+url.QueryEscape(strings.Join(ids, ",")), &out)
		byMBID := map[string][]lbTag{}
		for _, x := range out {
			byMBID[x.MBID] = x.Tag.Artist
		}
		for _, a := range batch {
			if err == nil {
				if tags := byMBID[a.mbid]; len(tags) > 0 {
					if serr := s.setTags(ctx, a.id, SourceListenBrainz, lbTags(tags)); serr == nil {
						n++
					}
				}
			}
			s.checked(ctx, a.id, checkLBTags, err)
			s.advance()
		}
		if err != nil && !errors.Is(err, errNotFound) {
			slog.Debug("listenbrainz artist tags", "err", err)
		}
	}
	return n, "artists' tags (ListenBrainz)", nil
}

// lbRecordingTags fetches the tags of tracks with a MusicBrainz recording id.
func (s *Service) lbRecordingTags(ctx context.Context) (int, string, error) {
	todo, err := s.due(ctx, `SELECT t.id, t.title, x.value, '' FROM items t
		JOIN external_ids x ON x.item_id = t.id AND x.provider = 'musicbrainz'
		LEFT JOIN music_community_checked c ON c.item_id = t.id AND c.source = ?
		WHERE t.type = 'track' AND (c.checked_at IS NULL OR c.checked_at < strftime('%Y-%m-%dT%H:%M:%fZ', 'now', ?)) ORDER BY t.id`, checkLBTags, trackRefresh)
	if err != nil {
		return 0, "", err
	}
	s.step(len(todo))
	n := 0
	base := or(s.ListenBrainz, "https://api.listenbrainz.org")
	for i := 0; i < len(todo); i += lbTagBatch {
		if err := s.pause(ctx); err != nil {
			return n, "tracks' tags (ListenBrainz)", err
		}
		batch := todo[i:min(i+lbTagBatch, len(todo))]
		ids := make([]string, len(batch))
		for j, t := range batch {
			ids[j] = t.mbid
		}
		out := map[string]struct {
			Tag struct {
				Recording []lbTag `json:"recording"`
			} `json:"tag"`
		}{}
		err := s.get(ctx, base+"/1/metadata/recording/?inc=tag&recording_mbids="+url.QueryEscape(strings.Join(ids, ",")), &out)
		for _, t := range batch {
			if err == nil {
				if tags := out[t.mbid].Tag.Recording; len(tags) > 0 {
					if serr := s.setTags(ctx, t.id, SourceListenBrainz, lbTags(tags)); serr == nil {
						n++
					}
				}
			}
			s.checked(ctx, t.id, checkLBTags, err)
			s.advance()
		}
	}
	return n, "tracks' tags (ListenBrainz)", nil
}

// ---------- Last.fm ----------

// lfmError is Last.fm's error body (HTTP 200 or 4xx with {"error": 6, "message": …}).
type lfmError struct {
	Error   int    `json:"error"`
	Message string `json:"message"`
}

func (s *Service) lastfm(ctx context.Context, key string, p url.Values, out any) error {
	p.Set("api_key", key)
	p.Set("format", "json")
	p.Set("autocorrect", "1")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, or(s.LastFM, "https://ws.audioscrobbler.com/2.0/")+"?"+p.Encode(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", or(s.UserAgent, "Marquee (self-hosted media server)"))
	resp, err := s.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	var e lfmError
	if json.Unmarshal(body, &e) == nil && e.Error != 0 {
		if e.Error == 6 { // not found
			return errNotFound
		}
		return fmt.Errorf("Last.fm error %d: %s", e.Error, e.Message)
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("Last.fm: HTTP %d", resp.StatusCode)
	}
	return json.Unmarshal(body, out)
}

// errBadKey stops the Last.fm part: every request would fail the same way.
func badKey(err error) bool {
	return err != nil && (strings.Contains(err.Error(), "error 10:") || strings.Contains(err.Error(), "error 26:"))
}

type lfmTags struct {
	TopTags struct {
		Tag []struct {
			Name  string  `json:"name"`
			Count float64 `json:"count"`
		} `json:"tag"`
	} `json:"toptags"`
}

func (t lfmTags) counts() []tagCount {
	var out []tagCount
	for _, x := range t.TopTags.Tag {
		if x.Count >= minLastFMTagPct {
			out = append(out, tagCount{x.Name, x.Count})
		}
	}
	return out
}

// lfmArtists fetches each artist's similar artists and tags from Last.fm.
func (s *Service) lfmArtists(ctx context.Context, lib library, key string) (int, string, error) {
	todo, err := s.due(ctx, dueArtists+` ORDER BY a.id`, checkLFMArtist, artistRefresh)
	if err != nil {
		return 0, "", err
	}
	s.step(len(todo))
	n := 0
	const what = "artists on Last.fm"
	for _, a := range todo {
		ask := func(method string) url.Values {
			p := url.Values{"method": {method}}
			if a.mbid != "" {
				p.Set("mbid", a.mbid)
			} else {
				p.Set("artist", a.name)
			}
			return p
		}
		if err := s.pause(ctx); err != nil {
			return n, what, err
		}
		var sim struct {
			Similar struct {
				Artist []struct {
					Name  string `json:"name"`
					MBID  string `json:"mbid"`
					Match string `json:"match"`
				} `json:"artist"`
			} `json:"similarartists"`
		}
		p := ask("artist.getSimilar")
		p.Set("limit", "100")
		err := s.lastfm(ctx, key, p, &sim)
		if errors.Is(err, errNotFound) && a.mbid != "" {
			// Last.fm doesn't know every MusicBrainz id: try the name.
			p.Del("mbid")
			p.Set("artist", a.name)
			err = s.lastfm(ctx, key, p, &sim)
			a.mbid = ""
		}
		if badKey(err) {
			return n, what, fmt.Errorf("the Last.fm API key isn't accepted: %w", err)
		}
		if err == nil {
			var list []similar
			for _, x := range sim.Similar.Artist {
				var m float64
				fmt.Sscan(x.Match, &m)
				if id := lib.match(x.MBID, x.Name); id != 0 {
					list = append(list, similar{id, m})
				}
			}
			err = s.setSimilar(ctx, a.id, SourceLastFM, list)
		}
		if err == nil || errors.Is(err, errNotFound) {
			if perr := s.pause(ctx); perr != nil {
				return n, what, perr
			}
			var tags lfmTags
			if terr := s.lastfm(ctx, key, ask("artist.getTopTags"), &tags); terr == nil {
				s.setTags(ctx, a.id, SourceLastFM, tags.counts())
			} else if badKey(terr) {
				return n, what, fmt.Errorf("the Last.fm API key isn't accepted: %w", terr)
			}
			if err == nil {
				n++
			}
		}
		s.checked(ctx, a.id, checkLFMArtist, err)
		s.advance()
	}
	return n, what, nil
}

// lfmTracks fetches each track's tags from Last.fm (by artist and title).
func (s *Service) lfmTracks(ctx context.Context, key string) (int, string, error) {
	todo, err := s.due(ctx, `SELECT t.id, t.title, '', COALESCE(ar.title, '') FROM items t
		LEFT JOIN items ar ON ar.id = t.grandparent_id
		LEFT JOIN music_community_checked c ON c.item_id = t.id AND c.source = ?
		WHERE t.type = 'track' AND ar.title IS NOT NULL
		  AND (c.checked_at IS NULL OR c.checked_at < strftime('%Y-%m-%dT%H:%M:%fZ', 'now', ?)) ORDER BY t.id`, checkLFMTrack, trackRefresh)
	if err != nil {
		return 0, "", err
	}
	s.step(len(todo))
	n := 0
	const what = "tracks' tags (Last.fm)"
	for _, t := range todo {
		if err := s.pause(ctx); err != nil {
			return n, what, err
		}
		var tags lfmTags
		err := s.lastfm(ctx, key, url.Values{"method": {"track.getTopTags"}, "artist": {t.more}, "track": {t.name}}, &tags)
		if badKey(err) {
			return n, what, fmt.Errorf("the Last.fm API key isn't accepted: %w", err)
		}
		if err == nil {
			if c := tags.counts(); len(c) > 0 {
				if s.setTags(ctx, t.id, SourceLastFM, c) == nil {
					n++
				}
			}
		}
		s.checked(ctx, t.id, checkLFMTrack, err)
		s.advance()
	}
	return n, what, nil
}
