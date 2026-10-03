// Package bazarr talks to Bazarr, which finds and downloads subtitles for the movies and
// episodes Radarr and Sonarr manage (META-12). Bazarr saves subtitles next to the video
// files; Marquee matches its titles to library files by path tail (folder + file name) and
// picks the new subtitle files up when Bazarr is done.
package bazarr

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ErrNotConfigured means no Bazarr address or API key has been set.
var ErrNotConfigured = errors.New("Bazarr isn't set up")

// Timeouts: status checks and lookups are quick; whole-library lists take a few seconds;
// downloads and provider searches run Bazarr's providers synchronously and can take minutes.
var (
	StatusTimeout = 5 * time.Second
	ListTimeout   = 60 * time.Second
	LongTimeout   = 3 * time.Minute
)

// Client is a client for Bazarr's API (version 1.x). Auth is the X-API-KEY header.
type Client struct {
	URL, APIKey string
	HTTP        *http.Client
}

// Flag is a boolean Bazarr sends either as JSON true/false or as "True"/"False".
type Flag bool

func (f *Flag) UnmarshalJSON(b []byte) error {
	s := strings.Trim(strings.ToLower(string(b)), `"`)
	*f = Flag(s == "true" || s == "1")
	return nil
}

// Language is a subtitle Bazarr has (Path set, or empty for an embedded track) or wants.
type Language struct {
	Name   string `json:"name"`
	Code2  string `json:"code2"`
	Code3  string `json:"code3"`
	Path   string `json:"path"`
	Forced Flag   `json:"forced"`
	HI     Flag   `json:"hi"`
}

// Movie is one of Radarr's movies as Bazarr lists it.
type Movie struct {
	RadarrID  int64      `json:"radarrId"`
	Title     string     `json:"title"`
	Path      string     `json:"path"`
	IMDbID    string     `json:"imdbId"`
	Subtitles []Language `json:"subtitles"`
	Missing   []Language `json:"missing_subtitles"`
}

// Series is one of Sonarr's shows; Path is its folder.
type Series struct {
	SeriesID int64  `json:"sonarrSeriesId"`
	Title    string `json:"title"`
	Path     string `json:"path"`
	TVDbID   int64  `json:"tvdbId"`
	IMDbID   string `json:"imdbId"`
}

// Episode is one of Sonarr's episodes.
type Episode struct {
	SeriesID  int64      `json:"sonarrSeriesId"`
	EpisodeID int64      `json:"sonarrEpisodeId"`
	Season    int        `json:"season"`
	Episode   int        `json:"episode"`
	Title     string     `json:"title"`
	Path      string     `json:"path"`
	Subtitles []Language `json:"subtitles"`
	Missing   []Language `json:"missing_subtitles"`
}

// Wanted is a movie or episode still missing subtitles its language profile asks for.
type Wanted struct {
	RadarrID  int64      `json:"radarrId"`
	SeriesID  int64      `json:"sonarrSeriesId"`
	EpisodeID int64      `json:"sonarrEpisodeId"`
	Missing   []Language `json:"missing_subtitles"`
}

// Candidate is one result of a manual provider search.
type Candidate struct {
	Provider         string   `json:"provider"`
	Subtitle         string   `json:"subtitle"`
	Language         string   `json:"language"`
	ReleaseInfo      []string `json:"release_info"`
	Score            float64  `json:"score"`
	HearingImpaired  Flag     `json:"hearing_impaired"`
	Forced           Flag     `json:"forced"`
	Uploader         string   `json:"uploader"`
	OriginalFormat   Flag     `json:"original_format"`
	ScoreWithoutHash float64  `json:"score_without_hash"`
}

// Options are what to download: a language (ISO 639-1, as Bazarr's code2) and its kind.
type Options struct {
	Language   string
	Forced, HI bool
}

// Pick is a manual search result to download.
type Pick struct {
	Provider, Subtitle         string
	Forced, HI, OriginalFormat bool
}

type page[T any] struct {
	Data  []T `json:"data"`
	Total int `json:"total"`
}

func (c *Client) do(ctx context.Context, timeout time.Duration, method, path string, q url.Values, out any) error {
	if c.URL == "" || c.APIKey == "" {
		return ErrNotConfigured
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	u := strings.TrimRight(c.URL, "/") + "/api" + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-API-KEY", c.APIKey)
	req.Header.Set("Accept", "application/json")
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("Bazarr didn't answer within %s", timeout)
		}
		return fmt.Errorf("Bazarr didn't answer: %w", err)
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return errors.New("Bazarr refused the API key")
	case resp.StatusCode >= 300:
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
		msg := strings.TrimSpace(string(b))
		if msg == "" {
			msg = resp.Status
		}
		return fmt.Errorf("Bazarr answered %d: %s", resp.StatusCode, msg)
	}
	if out == nil {
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 256<<20)).Decode(out); err != nil {
		return fmt.Errorf("Bazarr sent an unexpected answer: %w", err)
	}
	return nil
}

func all() url.Values { return url.Values{"start": {"0"}, "length": {"-1"}} }

func ids(key string, list []int64) url.Values {
	q := url.Values{}
	for _, id := range list {
		q.Add(key, strconv.FormatInt(id, 10))
	}
	return q
}

func pyBool(b bool) string {
	if b {
		return "True"
	}
	return "False"
}

// Status checks the address and key, returning Bazarr's version.
func (c *Client) Status(ctx context.Context) (string, error) {
	var out struct {
		Data struct {
			Version string `json:"bazarr_version"`
		} `json:"data"`
	}
	err := c.do(ctx, StatusTimeout, http.MethodGet, "/system/status", nil, &out)
	return out.Data.Version, err
}

// Movies lists movies: all of them, or only the given Radarr ids.
func (c *Client) Movies(ctx context.Context, radarrIDs ...int64) ([]Movie, error) {
	q, timeout := all(), ListTimeout
	if len(radarrIDs) > 0 {
		q, timeout = ids("radarrid[]", radarrIDs), StatusTimeout
	}
	var p page[Movie]
	return p.Data, c.do(ctx, timeout, http.MethodGet, "/movies", q, &p)
}

// Series lists every show.
func (c *Client) Series(ctx context.Context) ([]Series, error) {
	var p page[Series]
	return p.Data, c.do(ctx, ListTimeout, http.MethodGet, "/series", all(), &p)
}

// EpisodesOf lists a show's episodes.
func (c *Client) EpisodesOf(ctx context.Context, seriesID int64) ([]Episode, error) {
	var p page[Episode]
	return p.Data, c.do(ctx, ListTimeout, http.MethodGet, "/episodes", ids("seriesid[]", []int64{seriesID}), &p)
}

// Episodes looks episodes up by their Sonarr ids.
func (c *Client) Episodes(ctx context.Context, episodeIDs ...int64) ([]Episode, error) {
	var p page[Episode]
	return p.Data, c.do(ctx, ListTimeout, http.MethodGet, "/episodes", ids("episodeid[]", episodeIDs), &p)
}

// WantedMovies lists movies missing subtitles.
func (c *Client) WantedMovies(ctx context.Context) ([]Wanted, error) {
	var p page[Wanted]
	return p.Data, c.do(ctx, ListTimeout, http.MethodGet, "/movies/wanted", all(), &p)
}

// WantedEpisodes lists episodes missing subtitles.
func (c *Client) WantedEpisodes(ctx context.Context) ([]Wanted, error) {
	var p page[Wanted]
	return p.Data, c.do(ctx, ListTimeout, http.MethodGet, "/episodes/wanted", all(), &p)
}

func downloadQuery(o Options) url.Values {
	return url.Values{"language": {o.Language}, "forced": {pyBool(o.Forced)}, "hi": {pyBool(o.HI)}}
}

// DownloadMovie has Bazarr search its providers and save the best subtitle. It returns
// when Bazarr is done (it answers 204 whether or not it found one).
func (c *Client) DownloadMovie(ctx context.Context, radarrID int64, o Options) error {
	q := downloadQuery(o)
	q.Set("radarrid", strconv.FormatInt(radarrID, 10))
	return c.do(ctx, LongTimeout, http.MethodPatch, "/movies/subtitles", q, nil)
}

// DownloadEpisode is DownloadMovie for an episode.
func (c *Client) DownloadEpisode(ctx context.Context, seriesID, episodeID int64, o Options) error {
	q := downloadQuery(o)
	q.Set("seriesid", strconv.FormatInt(seriesID, 10))
	q.Set("episodeid", strconv.FormatInt(episodeID, 10))
	return c.do(ctx, LongTimeout, http.MethodPatch, "/episodes/subtitles", q, nil)
}

// SearchMovie runs a manual search of every provider.
func (c *Client) SearchMovie(ctx context.Context, radarrID int64) ([]Candidate, error) {
	var p page[Candidate]
	return p.Data, c.do(ctx, LongTimeout, http.MethodGet, "/providers/movies", url.Values{"radarrid": {strconv.FormatInt(radarrID, 10)}}, &p)
}

// SearchEpisode runs a manual search for an episode.
func (c *Client) SearchEpisode(ctx context.Context, episodeID int64) ([]Candidate, error) {
	var p page[Candidate]
	return p.Data, c.do(ctx, LongTimeout, http.MethodGet, "/providers/episodes", url.Values{"episodeid": {strconv.FormatInt(episodeID, 10)}}, &p)
}

func pickQuery(p Pick) url.Values {
	return url.Values{"provider": {p.Provider}, "subtitle": {p.Subtitle}, "hi": {pyBool(p.HI)},
		"forced": {pyBool(p.Forced)}, "original_format": {pyBool(p.OriginalFormat)}}
}

// PickMovie downloads one manual search result.
func (c *Client) PickMovie(ctx context.Context, radarrID int64, p Pick) error {
	q := pickQuery(p)
	q.Set("radarrid", strconv.FormatInt(radarrID, 10))
	return c.do(ctx, LongTimeout, http.MethodPost, "/providers/movies", q, nil)
}

// PickEpisode downloads one manual search result for an episode.
func (c *Client) PickEpisode(ctx context.Context, seriesID, episodeID int64, p Pick) error {
	q := pickQuery(p)
	q.Set("seriesid", strconv.FormatInt(seriesID, 10))
	q.Set("episodeid", strconv.FormatInt(episodeID, 10))
	return c.do(ctx, LongTimeout, http.MethodPost, "/providers/episodes", q, nil)
}
