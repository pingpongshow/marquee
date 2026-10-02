// Package scrobble sends what people play to ListenBrainz (MUSIC-12, D80): "playing now" when a
// track starts and a listen once it counts as played (half way), for every client, from the
// server. Each user connects with their own ListenBrainz token.
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

func (s *Service) Status(ctx context.Context, userID int64) Status {
	var st Status
	if s.DB.QueryRowContext(ctx, `SELECT username, error FROM user_scrobble WHERE user_id = ? AND service = 'listenbrainz'`, userID).
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

func (s *Service) Disconnect(ctx context.Context, userID int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM user_scrobble WHERE user_id = ? AND service = 'listenbrainz'`, userID)
	return err
}

type listen struct {
	ListenedAt int64          `json:"listened_at,omitempty"`
	Meta       map[string]any `json:"track_metadata"`
}

// Played reports playback of a track: started (playing now) or played (a listen, at startedAt).
func (s *Service) Played(ctx context.Context, userID, trackID int64, nowPlaying bool, startedAt time.Time) {
	var token string
	if s.DB.QueryRowContext(ctx, `SELECT token FROM user_scrobble WHERE user_id = ? AND service = 'listenbrainz'`, userID).Scan(&token) != nil {
		return
	}
	var typ, title, album, artist, credit, mbid, albumMBID string
	var durMS sql.NullInt64
	var idx sql.NullInt64
	err := s.DB.QueryRowContext(ctx, `SELECT t.type, t.title, COALESCE(a.title, ''), COALESCE(g.title, ''), COALESCE(t.artist_credit, ''),
			COALESCE((SELECT value FROM external_ids WHERE item_id = t.id AND provider = 'musicbrainz'), ''),
			COALESCE((SELECT value FROM external_ids WHERE item_id = a.id AND provider = 'musicbrainz'), ''),
			t.duration_ms, t.idx
		FROM items t LEFT JOIN items a ON a.id = t.parent_id LEFT JOIN items g ON g.id = t.grandparent_id WHERE t.id = ?`, trackID).
		Scan(&typ, &title, &album, &artist, &credit, &mbid, &albumMBID, &durMS, &idx)
	if err != nil || typ != "track" {
		return
	}
	if credit != "" {
		artist = credit // the track's own credit, e.g. with featured artists
	}
	info := map[string]any{"media_player": "Marquee", "submission_client": "Marquee"}
	if durMS.Valid && durMS.Int64 > 0 {
		info["duration_ms"] = durMS.Int64
	}
	if mbid != "" {
		info["recording_mbid"] = mbid
	}
	if albumMBID != "" {
		info["release_mbid"] = albumMBID
	}
	if idx.Valid {
		info["tracknumber"] = idx.Int64
	}
	meta := map[string]any{"artist_name": artist, "track_name": title, "additional_info": info}
	if album != "" {
		meta["release_name"] = album
	}
	body := map[string]any{"listen_type": "single", "payload": []listen{{ListenedAt: startedAt.Unix(), Meta: meta}}}
	if nowPlaying {
		body = map[string]any{"listen_type": "playing_now", "payload": []listen{{Meta: meta}}}
	}
	err = s.call(ctx, http.MethodPost, "/1/submit-listens", token, body, nil)
	msg := ""
	if err != nil {
		msg = err.Error()
		slog.Warn("listenbrainz", "user", userID, "err", err)
	}
	s.DB.ExecContext(ctx, `UPDATE user_scrobble SET error = ? WHERE user_id = ? AND service = 'listenbrainz'`, msg, userID)
}
