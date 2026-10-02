// Package requests lets people ask for movies and shows that aren't in the library
// (REQ-1). Requests wait in Marquee for an admin's approval and are then sent to Seerr,
// which hands them to Radarr and Sonarr.
package requests

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ErrNotConfigured means no Seerr address or API key has been set.
var ErrNotConfigured = errors.New("Seerr isn't set up")

// Seerr is a client for the Seerr (Overseerr/Jellyseerr) API.
type Seerr struct {
	URL, APIKey string
	HTTP        *http.Client
}

// Seerr media statuses (MediaStatus) and request statuses.
const (
	mediaPending    = 2
	mediaProcessing = 3
	mediaPartial    = 4
	mediaAvailable  = 5

	requestPending  = 1
	requestApproved = 2
	requestDeclined = 3
	requestFailed   = 4
)

// Result is a movie or show as Seerr lists it.
type Result struct {
	ID           int64   `json:"id"`
	MediaType    string  `json:"mediaType"`
	Title        string  `json:"title"`
	Name         string  `json:"name"`
	ReleaseDate  string  `json:"releaseDate"`
	FirstAirDate string  `json:"firstAirDate"`
	Overview     string  `json:"overview"`
	PosterPath   string  `json:"posterPath"`
	BackdropPath string  `json:"backdropPath"`
	VoteAverage  float64 `json:"voteAverage"`
	VoteCount    int     `json:"voteCount"`
	MediaInfo    *struct {
		Status  int `json:"status"`
		Seasons []struct {
			SeasonNumber int `json:"seasonNumber"`
			Status       int `json:"status"`
		} `json:"seasons"`
	} `json:"mediaInfo"`
}

func (r Result) DisplayTitle() string {
	if r.Title != "" {
		return r.Title
	}
	return r.Name
}

func (r Result) Year() int {
	d := r.ReleaseDate
	if d == "" {
		d = r.FirstAirDate
	}
	var y int
	if len(d) >= 4 {
		fmt.Sscanf(d[:4], "%d", &y)
	}
	return y
}

func (r Result) MediaStatus() int {
	if r.MediaInfo == nil {
		return 0
	}
	return r.MediaInfo.Status
}

// Page is one page of results.
type Page struct {
	Page       int      `json:"page"`
	TotalPages int      `json:"totalPages"`
	Results    []Result `json:"results"`
}

// Show is a show's details with its seasons.
type Show struct {
	Result
	Seasons []struct {
		SeasonNumber int    `json:"seasonNumber"`
		Name         string `json:"name"`
		EpisodeCount int    `json:"episodeCount"`
	} `json:"seasons"`
}

type User struct {
	ID          int64  `json:"id"`
	DisplayName string `json:"displayName"`
}

func (s *Seerr) client() *http.Client {
	if s.HTTP != nil {
		return s.HTTP
	}
	return &http.Client{Timeout: 15 * time.Second}
}

func (s *Seerr) do(ctx context.Context, method, path string, body, out any) error {
	if s.URL == "" || s.APIKey == "" {
		return ErrNotConfigured
	}
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(s.URL, "/")+"/api/v1"+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("X-Api-Key", s.APIKey)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := s.client().Do(req)
	if err != nil {
		return fmt.Errorf("Seerr didn't answer: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		var e struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(msg, &e) == nil && e.Message != "" {
			return fmt.Errorf("Seerr: %s (%d)", e.Message, resp.StatusCode)
		}
		return fmt.Errorf("Seerr answered %d", resp.StatusCode)
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// Status returns Seerr's version (a connection check).
func (s *Seerr) Status(ctx context.Context) (string, error) {
	var v struct {
		Version string `json:"version"`
	}
	err := s.do(ctx, http.MethodGet, "/status", nil, &v)
	return v.Version, err
}

// queryEscape encodes like JavaScript's encodeURIComponent: Seerr rejects "+" for spaces.
func queryEscape(q string) string { return strings.ReplaceAll(url.QueryEscape(q), "+", "%20") }

// Search finds movies and shows (people are left out).
func (s *Seerr) Search(ctx context.Context, q string, page int) (Page, error) {
	var p Page
	err := s.do(ctx, http.MethodGet, fmt.Sprintf("/search?query=%s&page=%d", queryEscape(q), page), nil, &p)
	return p, err
}

// Discover lists trending, popular movies or shows, or upcoming movies.
func (s *Seerr) Discover(ctx context.Context, category string, page int) (Page, error) {
	path := map[string]string{"trending": "/discover/trending", "movies": "/discover/movies", "tv": "/discover/tv", "upcoming": "/discover/movies/upcoming"}[category]
	if path == "" {
		path = "/discover/trending"
	}
	var p Page
	err := s.do(ctx, http.MethodGet, fmt.Sprintf("%s?page=%d", path, page), nil, &p)
	for i := range p.Results {
		// Discover pages for one type leave mediaType out.
		if p.Results[i].MediaType == "" {
			if category == "tv" {
				p.Results[i].MediaType = "tv"
			} else {
				p.Results[i].MediaType = "movie"
			}
		}
	}
	return p, err
}

// Movie and TV fetch one title's details.
func (s *Seerr) Movie(ctx context.Context, id int64) (Result, error) {
	var r Result
	err := s.do(ctx, http.MethodGet, fmt.Sprintf("/movie/%d", id), nil, &r)
	r.MediaType = "movie"
	return r, err
}

func (s *Seerr) TV(ctx context.Context, id int64) (Show, error) {
	var r Show
	err := s.do(ctx, http.MethodGet, fmt.Sprintf("/tv/%d", id), nil, &r)
	r.MediaType = "tv"
	return r, err
}

// Request asks Seerr for a title, as a Seerr user when userID is set. Requests made with the
// API key are approved by Seerr straight away (Marquee has already approved them).
func (s *Seerr) Request(ctx context.Context, mediaType string, tmdbID int64, seasons []int, userID *int64) (int64, error) {
	body := map[string]any{"mediaType": mediaType, "mediaId": tmdbID}
	if mediaType == "tv" {
		if len(seasons) == 0 {
			body["seasons"] = "all"
		} else {
			body["seasons"] = seasons
		}
	}
	if userID != nil {
		body["userId"] = *userID
	}
	var out struct {
		ID int64 `json:"id"`
	}
	err := s.do(ctx, http.MethodPost, "/request", body, &out)
	return out.ID, err
}

// RequestState is how Seerr sees a request: its status and its media's status.
func (s *Seerr) RequestState(ctx context.Context, id int64) (status, media int, err error) {
	var r struct {
		Status int `json:"status"`
		Media  struct {
			Status int `json:"status"`
		} `json:"media"`
	}
	err = s.do(ctx, http.MethodGet, fmt.Sprintf("/request/%d", id), nil, &r)
	return r.Status, r.Media.Status, err
}

// RatingsRaw are a title's public ratings as Seerr gives them: Rotten Tomatoes for shows;
// Rotten Tomatoes and IMDb ("ratingscombined") for movies.
type RatingsRaw struct {
	RT *struct {
		URL            string `json:"url"`
		CriticsRating  string `json:"criticsRating"`
		CriticsScore   *int   `json:"criticsScore"`
		AudienceRating string `json:"audienceRating"`
		AudienceScore  *int   `json:"audienceScore"`
	} `json:"rt"`
	IMDb *struct {
		URL          string   `json:"url"`
		CriticsScore *float64 `json:"criticsScore"`
	} `json:"imdb"`
}

// Ratings fetches a title's public ratings.
func (s *Seerr) Ratings(ctx context.Context, mediaType string, tmdbID int64) (RatingsRaw, error) {
	var out RatingsRaw
	if mediaType == "movie" {
		err := s.do(ctx, http.MethodGet, fmt.Sprintf("/movie/%d/ratingscombined", tmdbID), nil, &out)
		return out, err
	}
	err := s.do(ctx, http.MethodGet, fmt.Sprintf("/tv/%d/ratings", tmdbID), nil, &out.RT)
	return out, err
}

func (s *Seerr) Users(ctx context.Context) ([]User, error) {
	var r struct {
		Results []User `json:"results"`
	}
	err := s.do(ctx, http.MethodGet, "/user?take=200", nil, &r)
	return r.Results, err
}
