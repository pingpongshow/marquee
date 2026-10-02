// Package subtitles finds and downloads subtitles from OpenSubtitles.com (PLAY-7). Media
// folders are read-only, so downloads are kept in the config directory and attached to
// the file as external subtitle streams.
package subtitles

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"slices"
	"strconv"
	"sync"
	"time"
)

const apiBase = "https://api.opensubtitles.com/api/v1"

// ErrNotConfigured means there's no API key.
var ErrNotConfigured = errors.New("Add an OpenSubtitles API key in Settings → Metadata.") //nolint:staticcheck // shown to people as is

// Client talks to the OpenSubtitles REST API. Username and password are optional; logging
// in raises the daily download limit.
type Client struct {
	Base      string
	APIKey    string
	Username  string
	Password  string
	UserAgent string
	HTTP      *http.Client

	mu      sync.Mutex
	token   string
	tokenAt time.Time
}

func (c *Client) base() string {
	if c.Base != "" {
		return c.Base
	}
	return apiBase
}

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 20 * time.Second}
}

func (c *Client) do(ctx context.Context, method, path string, q url.Values, body, out any) error {
	if c.APIKey == "" {
		return ErrNotConfigured
	}
	u := c.base() + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Api-Key", c.APIKey)
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if path != "/login" {
		if tok := c.login(ctx); tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
	}
	resp, err := c.http().Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		var e struct {
			Message string   `json:"message"`
			Errors  []string `json:"errors"`
		}
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		json.Unmarshal(b, &e)
		msg := e.Message
		if msg == "" && len(e.Errors) > 0 {
			msg = e.Errors[0]
		}
		if msg == "" {
			msg = http.StatusText(resp.StatusCode)
		}
		return fmt.Errorf("OpenSubtitles: %s", msg)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// login returns a session token when an account is configured (cached for a day).
func (c *Client) login(ctx context.Context) string {
	if c.Username == "" || c.Password == "" {
		return ""
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.token != "" && time.Since(c.tokenAt) < 23*time.Hour {
		return c.token
	}
	var out struct {
		Token string `json:"token"`
	}
	if err := c.do(ctx, http.MethodPost, "/login", nil, map[string]string{"username": c.Username, "password": c.Password}, &out); err != nil {
		return ""
	}
	c.token, c.tokenAt = out.Token, time.Now()
	return c.token
}

// Query identifies what to search for.
type Query struct {
	IMDbID, TMDBID  string // the movie, or for episodes the show
	IsEpisode       bool
	Season, Episode int
	Languages       string // comma-separated ISO 639-1, e.g. "en,es"
	MovieHash       string
	Title           string
	Year            int
}

// Result is one subtitle file found.
type Result struct {
	FileID          int64
	Language        string
	Release         string
	FileName        string
	Downloads       int
	HearingImpaired bool
	ForeignOnly     bool
	AITranslated    bool
	HashMatch       bool
	FPS             float64
}

// Search finds subtitles, best matches first (hash matches, then most downloaded).
func (c *Client) Search(ctx context.Context, q Query) ([]Result, error) {
	v := url.Values{}
	if q.Languages != "" {
		v.Set("languages", q.Languages)
	}
	if q.MovieHash != "" {
		v.Set("moviehash", q.MovieHash)
	}
	switch {
	case q.IsEpisode && q.IMDbID != "":
		v.Set("parent_imdb_id", trimTT(q.IMDbID))
	case q.IsEpisode && q.TMDBID != "":
		v.Set("parent_tmdb_id", q.TMDBID)
	case q.IMDbID != "":
		v.Set("imdb_id", trimTT(q.IMDbID))
	case q.TMDBID != "":
		v.Set("tmdb_id", q.TMDBID)
	default:
		v.Set("query", q.Title)
		if q.Year > 0 {
			v.Set("year", strconv.Itoa(q.Year))
		}
	}
	if q.IsEpisode {
		v.Set("season_number", strconv.Itoa(q.Season))
		v.Set("episode_number", strconv.Itoa(q.Episode))
	}
	var out struct {
		Data []struct {
			Attributes struct {
				Language          string  `json:"language"`
				Release           string  `json:"release"`
				DownloadCount     int     `json:"download_count"`
				HearingImpaired   bool    `json:"hearing_impaired"`
				ForeignPartsOnly  bool    `json:"foreign_parts_only"`
				AITranslated      bool    `json:"ai_translated"`
				MachineTranslated bool    `json:"machine_translated"`
				MovieHashMatch    bool    `json:"moviehash_match"`
				FPS               float64 `json:"fps"`
				Files             []struct {
					FileID   int64  `json:"file_id"`
					FileName string `json:"file_name"`
				} `json:"files"`
			} `json:"attributes"`
		} `json:"data"`
	}
	if err := c.do(ctx, http.MethodGet, "/subtitles", v, nil, &out); err != nil {
		return nil, err
	}
	var res []Result
	for _, d := range out.Data {
		a := d.Attributes
		if len(a.Files) == 0 {
			continue
		}
		res = append(res, Result{FileID: a.Files[0].FileID, FileName: a.Files[0].FileName, Language: a.Language, Release: a.Release,
			Downloads: a.DownloadCount, HearingImpaired: a.HearingImpaired, ForeignOnly: a.ForeignPartsOnly,
			AITranslated: a.AITranslated || a.MachineTranslated, HashMatch: a.MovieHashMatch, FPS: a.FPS})
	}
	sortResults(res)
	return res, nil
}

// Download fetches a subtitle file's contents and reports downloads left today.
func (c *Client) Download(ctx context.Context, fileID int64) (data []byte, fileName string, remaining int, err error) {
	var out struct {
		Link      string `json:"link"`
		FileName  string `json:"file_name"`
		Remaining int    `json:"remaining"`
	}
	if err := c.do(ctx, http.MethodPost, "/download", nil, map[string]any{"file_id": fileID, "sub_format": "srt"}, &out); err != nil {
		return nil, "", 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, out.Link, nil)
	if err != nil {
		return nil, "", 0, err
	}
	resp, err := c.http().Do(req)
	if err != nil {
		return nil, "", 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", 0, fmt.Errorf("OpenSubtitles download: HTTP %d", resp.StatusCode)
	}
	data, err = io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	return data, out.FileName, out.Remaining, err
}

func trimTT(id string) string {
	for len(id) > 0 && (id[0] < '0' || id[0] > '9') {
		id = id[1:]
	}
	for len(id) > 1 && id[0] == '0' {
		id = id[1:]
	}
	return id
}

// Hash is OpenSubtitles' file hash: the size plus the 64-bit little-endian sums of the
// first and last 64 KiB.
func Hash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return "", err
	}
	const chunk = 64 << 10
	size := st.Size()
	if size < chunk*2 {
		return "", errors.New("file too small to hash")
	}
	sum := uint64(size)
	buf := make([]byte, chunk)
	for _, off := range []int64{0, size - chunk} {
		if _, err := f.ReadAt(buf, off); err != nil {
			return "", err
		}
		for i := 0; i < chunk; i += 8 {
			sum += binary.LittleEndian.Uint64(buf[i:])
		}
	}
	return fmt.Sprintf("%016x", sum), nil
}

func sortResults(rs []Result) {
	score := func(r Result) int {
		n := r.Downloads
		if r.HashMatch {
			n += 1 << 30 // made for this exact file
		}
		if r.AITranslated || r.ForeignOnly {
			n -= 1 << 29
		}
		return n
	}
	slices.SortStableFunc(rs, func(a, b Result) int { return score(b) - score(a) })
}
