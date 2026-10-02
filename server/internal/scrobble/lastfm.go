package scrobble

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
)

// Last.fm (MUSIC-12, D81): the owner registers an API account and enters its key and shared
// secret in Settings; each person then approves Marquee on last.fm, which hands back a token
// that's exchanged for a permanent session key.

var (
	ErrLastFMOff = errors.New("Last.fm isn't set up: an admin needs to add its API key and secret")
	ErrLastFMKey = errors.New("Last.fm didn't accept this server's API key or secret: check them in Settings → Music")
)

// LastFMKeys returns the server's Last.fm API key and secret ("" when not set).
type LastFMKeys func() (key, secret string)

func (s *Service) lastfmBase() string {
	if s.LastFMBase != "" {
		return s.LastFMBase
	}
	return "https://ws.audioscrobbler.com/2.0/"
}

// LastFMAvailable reports whether the admin has entered Last.fm's keys.
func (s *Service) LastFMAvailable() bool {
	if s.LastFM == nil {
		return false
	}
	k, sec := s.LastFM()
	return k != "" && sec != ""
}

// LastFMAuthURL is where a person approves Marquee on last.fm; last.fm then sends them to
// callback with ?token=….
func (s *Service) LastFMAuthURL(callback string) (string, error) {
	if !s.LastFMAvailable() {
		return "", ErrLastFMOff
	}
	key, _ := s.LastFM()
	u := "https://www.last.fm/api/auth/?api_key=" + url.QueryEscape(key)
	if callback != "" {
		u += "&cb=" + url.QueryEscape(callback)
	}
	return u, nil
}

// sign adds api_key and api_sig (MD5 of the sorted parameters and the secret) and format=json.
func (s *Service) sign(p url.Values) url.Values {
	key, secret := s.LastFM()
	p.Set("api_key", key)
	names := make([]string, 0, len(p))
	for k := range p {
		names = append(names, k)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, k := range names {
		b.WriteString(k)
		b.WriteString(p.Get(k))
	}
	b.WriteString(secret)
	sum := md5.Sum([]byte(b.String()))
	p.Set("api_sig", hex.EncodeToString(sum[:]))
	p.Set("format", "json")
	return p
}

func (s *Service) lastfm(ctx context.Context, p url.Values, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.lastfmBase(), strings.NewReader(s.sign(p).Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := s.client().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var body struct {
		Error   int    `json:"error"`
		Message string `json:"message"`
	}
	raw := json.NewDecoder(resp.Body)
	var m json.RawMessage
	if err := raw.Decode(&m); err != nil {
		return fmt.Errorf("last.fm: HTTP %d", resp.StatusCode)
	}
	json.Unmarshal(m, &body)
	if body.Error != 0 {
		if body.Error == 10 || body.Error == 13 || body.Error == 26 {
			return ErrLastFMKey // invalid or suspended API key, or a wrong secret (bad signature)
		}
		if body.Error == 4 || body.Error == 9 || body.Error == 14 || body.Error == 15 {
			return fmt.Errorf("%w: %s", ErrBadToken, body.Message)
		}
		return fmt.Errorf("last.fm: %s", body.Message)
	}
	if out != nil {
		return json.Unmarshal(m, out)
	}
	return nil
}

// ConnectLastFM exchanges the token last.fm returned for a session key and keeps it.
func (s *Service) ConnectLastFM(ctx context.Context, userID int64, token string) (Status, error) {
	if !s.LastFMAvailable() {
		return Status{}, ErrLastFMOff
	}
	var out struct {
		Session struct {
			Name string `json:"name"`
			Key  string `json:"key"`
		} `json:"session"`
	}
	if err := s.lastfm(ctx, url.Values{"method": {"auth.getSession"}, "token": {token}}, &out); err != nil {
		return Status{}, err
	}
	if out.Session.Key == "" {
		return Status{}, ErrBadToken
	}
	if _, err := s.DB.ExecContext(ctx, `INSERT INTO user_scrobble (user_id, service, token, username) VALUES (?, 'lastfm', ?, ?)
		ON CONFLICT (user_id, service) DO UPDATE SET token = excluded.token, username = excluded.username, error = ''`,
		userID, out.Session.Key, out.Session.Name); err != nil {
		return Status{}, err
	}
	return Status{Connected: true, Username: out.Session.Name}, nil
}

func (s *Service) sendLastFM(ctx context.Context, sessionKey string, t track, nowPlaying bool, startedAt int64) error {
	p := url.Values{"sk": {sessionKey}, "artist": {t.artist}, "track": {t.title}}
	if t.album != "" {
		p.Set("album", t.album)
	}
	if t.durationMS > 0 {
		p.Set("duration", strconv.FormatInt(t.durationMS/1000, 10))
	}
	if t.mbid != "" {
		p.Set("mbid", t.mbid)
	}
	if t.index > 0 {
		p.Set("trackNumber", strconv.FormatInt(t.index, 10))
	}
	if nowPlaying {
		p.Set("method", "track.updateNowPlaying")
	} else {
		p.Set("method", "track.scrobble")
		p.Set("timestamp", strconv.FormatInt(startedAt, 10))
	}
	return s.lastfm(ctx, p, nil)
}
