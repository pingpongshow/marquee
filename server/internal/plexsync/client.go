package plexsync

// The Plex HTTP API calls the sync makes. Tokens always travel in the X-Plex-Token header,
// never in a URL, and are redacted from every error message.
//
// Plex Media Server (with the token of the user whose state is read or written):
//
//	GET /identity                               machineIdentifier; the reachability check
//	GET /accounts                               the server's accounts (the owner is account 1)
//	GET /library/sections                       sections and their folders (Location)
//	GET /library/sections/{id}/all?type=1|4|10  movies, episodes or tracks with the token's
//	                                            user's viewCount, lastViewedAt, viewOffset,
//	                                            userRating, lastRatedAt and Media/Part files
//	GET /library/metadata/{ratingKey}           one item's state, re-read after pushing
//	GET /status/sessions/history/all            plays since a time (viewedAt>=, owner token:
//	                                            every account's, with accountID)
//	GET /:/scrobble?key=&identifier=com.plexapp.plugins.library    mark played (one more play;
//	                                            Plex records the play at this moment)
//	GET /:/unscrobble?key=&identifier=…         mark unplayed
//	PUT /:/rate?key=&identifier=…&rating=       set the user's 0–10 rating (-1 clears it)
//	GET /:/timeline?ratingKey=&key=/library/metadata/…&state=stopped&time=&duration=
//	                                            set the resume position (milliseconds)
//
// plex.tv (with the owner's plex.tv token):
//
//	POST /api/v2/pins?strong=true, GET /api/v2/pins/{id}   Sign in with Plex (plex.tv/link)
//	GET  /api/v2/home/users                                Plex Home users
//	POST /api/v2/home/users/{uuid}/switch                  a Home user's plex.tv token
//	GET  /api/v2/resources?includeHttps=1                  (that user's token) their token
//	                                                       for this server
//	GET  /api/servers/{machineId}/shared_servers           friends' tokens for this server,
//	                                                       when the owner's token may see them

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

const libraryIdentifier = "com.plexapp.plugins.library"

var (
	// ErrUnreachable: Plex (or plex.tv) couldn't be reached; the sync waits for the next run.
	ErrUnreachable = errors.New("Plex isn't reachable")
	// ErrUnauthorized: Plex rejected the token.
	ErrUnauthorized = errors.New("Plex rejected the token")
)

// limiter spaces requests at least every apart (shared by every client of a run).
type limiter struct {
	mu    sync.Mutex
	next  time.Time
	every time.Duration
}

func (l *limiter) wait(ctx context.Context) error {
	if l == nil || l.every <= 0 {
		return ctx.Err()
	}
	l.mu.Lock()
	now := time.Now()
	at := l.next
	if at.Before(now) {
		at = now
	}
	l.next = at.Add(l.every)
	l.mu.Unlock()
	if d := time.Until(at); d > 0 {
		t := time.NewTimer(d)
		defer t.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
		}
	}
	return nil
}

// client talks to one Plex server or plex.tv.
type client struct {
	base     string
	clientID string
	http     *http.Client
	lim      *limiter
	backoff  time.Duration // first retry delay after a 429/5xx; doubles
	retries  int
	secrets  *secrets
}

// secrets are the tokens seen in a run, scrubbed from every error message.
type secrets struct {
	mu   sync.Mutex
	list []string
}

func (s *secrets) add(tok string) {
	if s == nil || tok == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, t := range s.list {
		if t == tok {
			return
		}
	}
	s.list = append(s.list, tok)
}

var tokenParam = regexp.MustCompile(`(?i)(x-plex-token|authToken|accessToken|token)(=|":"|=")[^&"\s]+`)

// redact removes tokens from a message.
func (s *secrets) redact(msg string) string {
	if s != nil {
		s.mu.Lock()
		for _, t := range s.list {
			msg = strings.ReplaceAll(msg, t, "[redacted]")
		}
		s.mu.Unlock()
	}
	return tokenParam.ReplaceAllString(msg, "$1$2[redacted]")
}

type statusError struct {
	method, path string
	code         int
}

func (e *statusError) Error() string { return fmt.Sprintf("%s %s: HTTP %d", e.method, e.path, e.code) }

// do sends a request with the token and decodes a JSON (or, with xmlOut, XML) response.
func (c *client) do(ctx context.Context, method, path string, q url.Values, token string, out any) error {
	c.secrets.add(token)
	u := strings.TrimRight(c.base, "/") + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	delay := c.backoff
	for attempt := 0; ; attempt++ {
		if err := c.lim.wait(ctx); err != nil {
			return err
		}
		err := c.once(ctx, method, u, path, token, out)
		if err == nil {
			return nil
		}
		var se *statusError
		retry := errors.As(err, &se) && (se.code == http.StatusTooManyRequests || se.code >= 500)
		retry = retry || errors.Is(err, ErrUnreachable)
		if !retry || attempt >= c.retries || ctx.Err() != nil {
			return err
		}
		t := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			t.Stop()
			return ctx.Err()
		case <-t.C:
		}
		delay *= 2
	}
}

func (c *client) once(ctx context.Context, method, u, path, token string, out any) error {
	req, err := http.NewRequestWithContext(ctx, method, u, nil)
	if err != nil {
		return errors.New(c.secrets.redact(err.Error()))
	}
	if _, isXML := out.(*xmlBody); isXML {
		req.Header.Set("Accept", "application/xml")
	} else {
		req.Header.Set("Accept", "application/json")
	}
	req.Header.Set("X-Plex-Product", "Marquee")
	req.Header.Set("X-Plex-Client-Identifier", c.clientID)
	if token != "" {
		req.Header.Set("X-Plex-Token", token)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// Connection refused, DNS, timeouts: Plex isn't there (yet).
		return fmt.Errorf("%w: %s", ErrUnreachable, c.secrets.redact(err.Error()))
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		io.Copy(io.Discard, resp.Body)
		return fmt.Errorf("%w (%s %s)", ErrUnauthorized, method, path)
	case resp.StatusCode >= 300:
		io.Copy(io.Discard, resp.Body)
		return &statusError{method: method, path: path, code: resp.StatusCode}
	}
	if out == nil {
		io.Copy(io.Discard, resp.Body)
		return nil
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256<<20))
	if err != nil {
		return fmt.Errorf("%w: %s", ErrUnreachable, c.secrets.redact(err.Error()))
	}
	if x, ok := out.(*xmlBody); ok {
		err = xml.Unmarshal(body, x.v)
	} else {
		err = json.Unmarshal(body, out)
	}
	if err != nil {
		return fmt.Errorf("%s %s: unexpected response: %s", method, path, c.secrets.redact(err.Error()))
	}
	return nil
}

type xmlBody struct{ v any }

// ---------- Plex Media Server ----------

// Metadata is an item as Plex returns it, with the requesting user's state.
type Metadata struct {
	RatingKey        string   `json:"ratingKey"`
	Type             string   `json:"type"`
	Title            string   `json:"title"`
	GrandparentTitle string   `json:"grandparentTitle"`
	ViewCount        int64    `json:"viewCount"`
	LastViewedAt     int64    `json:"lastViewedAt"`
	ViewOffset       int64    `json:"viewOffset"`
	UserRating       *float64 `json:"userRating"`
	LastRatedAt      int64    `json:"lastRatedAt"`
	Duration         int64    `json:"duration"`
	// History entries.
	ViewedAt  int64 `json:"viewedAt"`
	AccountID int64 `json:"accountID"`
	Media     []struct {
		Part []struct {
			File string `json:"file"`
		} `json:"Part"`
	} `json:"Media"`
}

// DisplayTitle is "Show – Episode" / "Artist – Track" or the title.
func (m Metadata) DisplayTitle() string {
	if m.GrandparentTitle != "" {
		return m.GrandparentTitle + " – " + m.Title
	}
	return m.Title
}

type directory struct {
	Key      string `json:"key"`
	Type     string `json:"type"` // movie, show, artist, photo
	Title    string `json:"title"`
	Location []struct {
		Path string `json:"path"`
	} `json:"Location"`
}

type container struct {
	MediaContainer struct {
		Size              int         `json:"size"`
		TotalSize         int         `json:"totalSize"`
		MachineIdentifier string      `json:"machineIdentifier"`
		Metadata          []Metadata  `json:"Metadata"`
		Directory         []directory `json:"Directory"`
		Account           []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"Account"`
	} `json:"MediaContainer"`
}

func (c *client) identity(ctx context.Context, token string) (string, error) {
	var out container
	if err := c.do(ctx, http.MethodGet, "/identity", nil, token, &out); err != nil {
		return "", err
	}
	return out.MediaContainer.MachineIdentifier, nil
}

type account struct {
	ID   int64
	Name string
}

func (c *client) accounts(ctx context.Context, token string) ([]account, error) {
	var out container
	if err := c.do(ctx, http.MethodGet, "/accounts", nil, token, &out); err != nil {
		return nil, err
	}
	var list []account
	for _, a := range out.MediaContainer.Account {
		list = append(list, account{ID: a.ID, Name: a.Name})
	}
	return list, nil
}

func (c *client) sections(ctx context.Context, token string) ([]directory, error) {
	var out container
	if err := c.do(ctx, http.MethodGet, "/library/sections", nil, token, &out); err != nil {
		return nil, err
	}
	return out.MediaContainer.Directory, nil
}

// list pages through a listing, pageSize items at a time.
func (c *client) list(ctx context.Context, path string, q url.Values, token string, pageSize int, each func(Metadata) error) error {
	for start := 0; ; {
		qq := url.Values{}
		for k, v := range q {
			qq[k] = v
		}
		qq.Set("X-Plex-Container-Start", strconv.Itoa(start))
		qq.Set("X-Plex-Container-Size", strconv.Itoa(pageSize))
		var out container
		if err := c.do(ctx, http.MethodGet, path, qq, token, &out); err != nil {
			return err
		}
		for _, m := range out.MediaContainer.Metadata {
			if err := each(m); err != nil {
				return err
			}
		}
		n := len(out.MediaContainer.Metadata)
		start += n
		total := out.MediaContainer.TotalSize
		if n == 0 || n < pageSize || (total > 0 && start >= total) {
			return nil
		}
	}
}

func (c *client) metadata(ctx context.Context, token, ratingKey string) (Metadata, error) {
	var out container
	if err := c.do(ctx, http.MethodGet, "/library/metadata/"+url.PathEscape(ratingKey), nil, token, &out); err != nil {
		return Metadata{}, err
	}
	if len(out.MediaContainer.Metadata) == 0 {
		return Metadata{}, fmt.Errorf("item %s not found in Plex", ratingKey)
	}
	return out.MediaContainer.Metadata[0], nil
}

func (c *client) scrobble(ctx context.Context, token, ratingKey string) error {
	return c.do(ctx, http.MethodGet, "/:/scrobble", url.Values{"key": {ratingKey}, "identifier": {libraryIdentifier}}, token, nil)
}

func (c *client) unscrobble(ctx context.Context, token, ratingKey string) error {
	return c.do(ctx, http.MethodGet, "/:/unscrobble", url.Values{"key": {ratingKey}, "identifier": {libraryIdentifier}}, token, nil)
}

// rate sets the user's 0–10 rating; nil clears it (-1).
func (c *client) rate(ctx context.Context, token, ratingKey string, rating *float64) error {
	r := "-1"
	if rating != nil {
		r = strconv.FormatFloat(*rating, 'f', -1, 64)
	}
	return c.do(ctx, http.MethodPut, "/:/rate", url.Values{"key": {ratingKey}, "identifier": {libraryIdentifier}, "rating": {r}}, token, nil)
}

// timeline reports a stopped playback at positionMS, which Plex keeps as the resume point.
func (c *client) timeline(ctx context.Context, token, ratingKey string, positionMS, durationMS int64) error {
	return c.do(ctx, http.MethodGet, "/:/timeline", url.Values{"ratingKey": {ratingKey}, "key": {"/library/metadata/" + ratingKey},
		"identifier": {libraryIdentifier}, "state": {"stopped"}, "time": {strconv.FormatInt(positionMS, 10)},
		"duration": {strconv.FormatInt(durationMS, 10)}}, token, nil)
}

// ---------- plex.tv ----------

type homeUser struct {
	ID         int64  `json:"id"`
	UUID       string `json:"uuid"`
	Title      string `json:"title"`
	Username   string `json:"username"`
	Admin      bool   `json:"admin"`
	Restricted bool   `json:"restricted"`
	Protected  bool   `json:"protected"`
}

func (c *client) homeUsers(ctx context.Context, token string) ([]homeUser, error) {
	var raw json.RawMessage
	if err := c.do(ctx, http.MethodGet, "/api/v2/home/users", nil, token, &raw); err != nil {
		return nil, err
	}
	var wrapped struct {
		Users []homeUser `json:"users"`
	}
	if err := json.Unmarshal(raw, &wrapped); err == nil && wrapped.Users != nil {
		return wrapped.Users, nil
	}
	var list []homeUser
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, fmt.Errorf("plex.tv home users: unexpected response")
	}
	return list, nil
}

// switchHomeUser returns a Plex Home user's plex.tv token.
func (c *client) switchHomeUser(ctx context.Context, token, uuid string) (string, error) {
	var out struct {
		AuthToken string `json:"authToken"`
	}
	if err := c.do(ctx, http.MethodPost, "/api/v2/home/users/"+url.PathEscape(uuid)+"/switch", nil, token, &out); err != nil {
		return "", err
	}
	if out.AuthToken == "" {
		return "", errors.New("plex.tv didn't return a token for the Home user")
	}
	c.secrets.add(out.AuthToken)
	return out.AuthToken, nil
}

// serverToken returns the token a plex.tv user uses for the server machineID.
func (c *client) serverToken(ctx context.Context, userToken, machineID string) (string, error) {
	var out []struct {
		ClientIdentifier string `json:"clientIdentifier"`
		AccessToken      string `json:"accessToken"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/v2/resources", url.Values{"includeHttps": {"1"}}, userToken, &out); err != nil {
		return "", err
	}
	for _, r := range out {
		if r.ClientIdentifier == machineID && r.AccessToken != "" {
			c.secrets.add(r.AccessToken)
			return r.AccessToken, nil
		}
	}
	return "", errors.New("this Plex server isn't shared with the user")
}

// sharedTokens returns the server tokens of users the server is shared with, by plex.tv
// user id, when the owner's token may list them.
func (c *client) sharedTokens(ctx context.Context, token, machineID string) (map[int64]string, error) {
	var out struct {
		Servers []struct {
			UserID      int64  `xml:"userID,attr"`
			AccessToken string `xml:"accessToken,attr"`
		} `xml:"SharedServer"`
	}
	if err := c.do(ctx, http.MethodGet, "/api/servers/"+url.PathEscape(machineID)+"/shared_servers", nil, token, &xmlBody{&out}); err != nil {
		return nil, err
	}
	m := map[int64]string{}
	for _, s := range out.Servers {
		if s.AccessToken != "" {
			c.secrets.add(s.AccessToken)
			m[s.UserID] = s.AccessToken
		}
	}
	return m, nil
}

// Pin is a Sign in with Plex request.
type Pin struct {
	ID        int64  `json:"id"`
	Code      string `json:"code"`
	AuthToken string `json:"authToken"`
}

func (c *client) createPin(ctx context.Context) (Pin, error) {
	var p Pin
	err := c.do(ctx, http.MethodPost, "/api/v2/pins", url.Values{"strong": {"true"}}, "", &p)
	return p, err
}

func (c *client) checkPin(ctx context.Context, id int64) (Pin, error) {
	var p Pin
	err := c.do(ctx, http.MethodGet, "/api/v2/pins/"+strconv.FormatInt(id, 10), nil, "", &p)
	c.secrets.add(p.AuthToken)
	return p, err
}
