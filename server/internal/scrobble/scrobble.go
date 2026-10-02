// Package scrobble sends what people play to ListenBrainz and Last.fm (MUSIC-12, D80, D81):
// "playing now" when a track starts and a listen once it counts as played (half way), for
// every client, from the server. Each user connects their own accounts.
package scrobble

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

var ErrBadToken = errors.New("ListenBrainz didn't accept that token")

type Service struct {
	DB   *sql.DB
	Base string // default https://api.listenbrainz.org
	HTTP *http.Client
	// LastFM returns the server's Last.fm API key and secret; LastFMBase overrides the
	// Last.fm API address (tests).
	LastFM     LastFMKeys
	LastFMBase string
}

func (s *Service) base() string {
	if s.Base != "" {
		return s.Base
	}
	return "https://api.listenbrainz.org"
}

func (s *Service) client() *http.Client {
	if s.HTTP != nil {
		return s.HTTP
	}
	return &http.Client{Timeout: 15 * time.Second}
}

func (s *Service) call(ctx context.Context, method, path, token string, body any, out any) error {
	var rd *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	} else {
		rd = bytes.NewReader(nil)
	}
	req, err := http.NewRequestWithContext(ctx, method, s.base()+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Token "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		return ErrBadToken
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("listenbrainz %s: HTTP %d", path, resp.StatusCode)
	}
	if out != nil {
		return json.NewDecoder(resp.Body).Decode(out)
	}
	return nil
}

// Status is a user's ListenBrainz connection.
type Status struct {
	Connected bool
	Username  string
	Error     string // the last submission problem, if any
}

func (s *Service) Status(ctx context.Context, userID int64, service string) Status {
	var st Status
	if s.DB.QueryRowContext(ctx, `SELECT username, error FROM user_scrobble WHERE user_id = ? AND service = ?`, userID, service).
		Scan(&st.Username, &st.Error) == nil {
		st.Connected = true
	}
	return st
}

// Connect checks a token with ListenBrainz and keeps it.
func (s *Service) Connect(ctx context.Context, userID int64, token string) (Status, error) {
	var v struct {
		Valid    bool   `json:"valid"`
		UserName string `json:"user_name"`
	}
	if err := s.call(ctx, http.MethodGet, "/1/validate-token", token, nil, &v); err != nil {
		return Status{}, err
	}
	if !v.Valid {
		return Status{}, ErrBadToken
	}
	if _, err := s.DB.ExecContext(ctx, `INSERT INTO user_scrobble (user_id, service, token, username) VALUES (?, 'listenbrainz', ?, ?)
		ON CONFLICT (user_id, service) DO UPDATE SET token = excluded.token, username = excluded.username, error = ''`, userID, token, v.UserName); err != nil {
		return Status{}, err
	}
	return Status{Connected: true, Username: v.UserName}, nil
}

func (s *Service) Disconnect(ctx context.Context, userID int64, service string) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM user_scrobble WHERE user_id = ? AND service = ?`, userID, service)
	return err
}

type track struct {
	title, album, artist, mbid, albumMBID string
	durationMS, index                     int64
}

type listen struct {
	ListenedAt int64          `json:"listened_at,omitempty"`
	Meta       map[string]any `json:"track_metadata"`
}

// Played reports playback of a track to every service the user connected: started (playing
// now) or played (a listen, at startedAt).
func (s *Service) Played(ctx context.Context, userID, trackID int64, nowPlaying bool, startedAt time.Time) {
	rows, err := s.DB.QueryContext(ctx, `SELECT service, token FROM user_scrobble WHERE user_id = ?`, userID)
	if err != nil {
		return
	}
	tokens := map[string]string{}
	for rows.Next() {
		var svc, tok string
		if rows.Scan(&svc, &tok) == nil {
			tokens[svc] = tok
		}
	}
	rows.Close()
	if len(tokens) == 0 {
		return
	}
	var typ, credit string
	var t track
	var durMS, idx sql.NullInt64
	err = s.DB.QueryRowContext(ctx, `SELECT t.type, t.title, COALESCE(a.title, ''), COALESCE(g.title, ''), COALESCE(t.artist_credit, ''),
			COALESCE((SELECT value FROM external_ids WHERE item_id = t.id AND provider = 'musicbrainz'), ''),
			COALESCE((SELECT value FROM external_ids WHERE item_id = a.id AND provider = 'musicbrainz'), ''),
			t.duration_ms, t.idx
		FROM items t LEFT JOIN items a ON a.id = t.parent_id LEFT JOIN items g ON g.id = t.grandparent_id WHERE t.id = ?`, trackID).
		Scan(&typ, &t.title, &t.album, &t.artist, &credit, &t.mbid, &t.albumMBID, &durMS, &idx)
	if err != nil || typ != "track" {
		return
	}
	if credit != "" {
		t.artist = credit // the track's own credit, e.g. with featured artists
	}
	t.durationMS, t.index = durMS.Int64, idx.Int64
	for svc, tok := range tokens {
		var err error
		switch svc {
		case "listenbrainz":
			err = s.sendListenBrainz(ctx, tok, t, nowPlaying, startedAt.Unix())
		case "lastfm":
			if !s.LastFMAvailable() {
				continue
			}
			err = s.sendLastFM(ctx, tok, t, nowPlaying, startedAt.Unix())
		default:
			continue
		}
		msg := ""
		if err != nil {
			msg = err.Error()
			slog.Warn("scrobble", "service", svc, "user", userID, "err", err)
		}
		s.DB.ExecContext(ctx, `UPDATE user_scrobble SET error = ? WHERE user_id = ? AND service = ?`, msg, userID, svc)
	}
}

func (s *Service) sendListenBrainz(ctx context.Context, token string, t track, nowPlaying bool, startedAt int64) error {
	info := map[string]any{"media_player": "Marquee", "submission_client": "Marquee"}
	if t.durationMS > 0 {
		info["duration_ms"] = t.durationMS
	}
	if t.mbid != "" {
		info["recording_mbid"] = t.mbid
	}
	if t.albumMBID != "" {
		info["release_mbid"] = t.albumMBID
	}
	if t.index > 0 {
		info["tracknumber"] = t.index
	}
	meta := map[string]any{"artist_name": t.artist, "track_name": t.title, "additional_info": info}
	if t.album != "" {
		meta["release_name"] = t.album
	}
	body := map[string]any{"listen_type": "single", "payload": []listen{{ListenedAt: startedAt, Meta: meta}}}
	if nowPlaying {
		body = map[string]any{"listen_type": "playing_now", "payload": []listen{{Meta: meta}}}
	}
	return s.call(ctx, http.MethodPost, "/1/submit-listens", token, body, nil)
}
