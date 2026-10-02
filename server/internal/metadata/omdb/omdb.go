// Package omdb fetches IMDb, Rotten Tomatoes and Metacritic ratings from the OMDb API.
package omdb

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

var (
	ErrLimit      = errors.New("OMDb daily request limit reached")
	ErrInvalidKey = errors.New("OMDb rejected the API key")
	ErrNotFound   = errors.New("not found on OMDb")
)

type Client struct {
	Key     string
	BaseURL string
	HTTP    *http.Client
}

func New(key string) *Client {
	return &Client{Key: key, BaseURL: "https://www.omdbapi.com/", HTTP: &http.Client{Timeout: 15 * time.Second}}
}

type Ratings struct {
	IMDb       float64 // 0–10, 0 = unknown
	IMDbVotes  int
	RTCritic   int // 0–100, -1 = unknown
	Metacritic int // 0–100, -1 = unknown
}

// ByIMDbID returns ratings for an IMDb id (tt…).
func (c *Client) ByIMDbID(ctx context.Context, imdbID string) (Ratings, error) {
	r := Ratings{RTCritic: -1, Metacritic: -1}
	u := c.BaseURL + "?" + url.Values{"apikey": {c.Key}, "i": {imdbID}, "tomatoes": {"true"}}.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return r, err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return r, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return r, err
	}
	var out struct {
		Response   string `json:"Response"`
		Error      string `json:"Error"`
		IMDbRating string `json:"imdbRating"`
		IMDbVotes  string `json:"imdbVotes"`
		Metascore  string `json:"Metascore"`
		Ratings    []struct {
			Source string `json:"Source"`
			Value  string `json:"Value"`
		} `json:"Ratings"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return r, fmt.Errorf("omdb: HTTP %d: %w", resp.StatusCode, err)
	}
	if out.Response != "True" {
		e := strings.ToLower(out.Error)
		switch {
		case strings.Contains(e, "limit"):
			return r, ErrLimit
		case strings.Contains(e, "invalid api key"), strings.Contains(e, "no api key"):
			return r, ErrInvalidKey
		case strings.Contains(e, "not found"), strings.Contains(e, "incorrect imdb id"):
			return r, ErrNotFound
		}
		return r, fmt.Errorf("omdb: %s", out.Error)
	}
	r.IMDb, _ = strconv.ParseFloat(out.IMDbRating, 64)
	r.IMDbVotes, _ = strconv.Atoi(strings.ReplaceAll(out.IMDbVotes, ",", ""))
	if n, err := strconv.Atoi(out.Metascore); err == nil {
		r.Metacritic = n
	}
	for _, x := range out.Ratings {
		if x.Source == "Rotten Tomatoes" {
			if n, err := strconv.Atoi(strings.TrimSuffix(x.Value, "%")); err == nil {
				r.RTCritic = n
			}
		}
	}
	return r, nil
}
