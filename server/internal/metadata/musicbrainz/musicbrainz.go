// Package musicbrainz looks up artists and release groups in the MusicBrainz web service
// (no key; at most one request per second, as MusicBrainz asks).
package musicbrainz

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

var ErrNotFound = errors.New("not found in MusicBrainz")

type Client struct {
	BaseURL   string
	UserAgent string
	HTTP      *http.Client
	// Interval between requests (default one second).
	Interval time.Duration

	mu   sync.Mutex
	last time.Time
}

func New(userAgent string) *Client {
	return &Client{BaseURL: "https://musicbrainz.org/ws/2", UserAgent: userAgent, HTTP: &http.Client{Timeout: 20 * time.Second}}
}

type Genre struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type Artist struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	SortName string  `json:"sort-name"`
	Score    int     `json:"score"`
	Genres   []Genre `json:"genres"`
}

type ReleaseGroup struct {
	ID               string   `json:"id"`
	Title            string   `json:"title"`
	Score            int      `json:"score"`
	PrimaryType      string   `json:"primary-type"`
	SecondaryTypes   []string `json:"secondary-types"`
	FirstReleaseDate string   `json:"first-release-date"`
	Genres           []Genre  `json:"genres"`
	ArtistCredit     []struct {
		Name   string `json:"name"`
		Artist struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"artist"`
	} `json:"artist-credit"`
}

func (c *Client) wait(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	gap := c.Interval
	if gap == 0 {
		gap = time.Second
	}
	if d := time.Until(c.last.Add(gap)); d > 0 {
		select {
		case <-time.After(d):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	c.last = time.Now()
	return nil
}

func (c *Client) get(ctx context.Context, path string, q url.Values, out any) error {
	q.Set("fmt", "json")
	for attempt := 0; ; attempt++ {
		if err := c.wait(ctx); err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path+"?"+q.Encode(), nil)
		if err != nil {
			return err
		}
		req.Header.Set("User-Agent", c.UserAgent)
		req.Header.Set("Accept", "application/json")
		resp, err := c.HTTP.Do(req)
		if err != nil {
			return err
		}
		switch {
		case resp.StatusCode == http.StatusNotFound:
			resp.Body.Close()
			return ErrNotFound
		case resp.StatusCode == http.StatusServiceUnavailable && attempt < 3:
			// Rate limited: back off and try again.
			resp.Body.Close()
			select {
			case <-time.After(time.Duration(attempt+2) * time.Second):
			case <-ctx.Done():
				return ctx.Err()
			}
			continue
		case resp.StatusCode != http.StatusOK:
			resp.Body.Close()
			return fmt.Errorf("musicbrainz %s: HTTP %d", path, resp.StatusCode)
		}
		err = json.NewDecoder(resp.Body).Decode(out)
		resp.Body.Close()
		return err
	}
}

// quote makes a Lucene phrase.
func quote(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`)
	return `"` + r.Replace(s) + `"`
}

// Artist looks an artist up by MBID, with genres.
func (c *Client) Artist(ctx context.Context, mbid string) (Artist, error) {
	var a Artist
	err := c.get(ctx, "/artist/"+url.PathEscape(mbid), url.Values{"inc": {"genres"}}, &a)
	return a, err
}

// SearchArtists finds artists by name.
func (c *Client) SearchArtists(ctx context.Context, name string) ([]Artist, error) {
	var out struct {
		Artists []Artist `json:"artists"`
	}
	err := c.get(ctx, "/artist", url.Values{"query": {"artist:" + quote(name)}, "limit": {"10"}}, &out)
	return out.Artists, err
}

// ReleaseGroupOf returns the release group of a release (an album's MBID in tags).
func (c *Client) ReleaseGroupOf(ctx context.Context, releaseID string) (string, error) {
	var out struct {
		ReleaseGroup struct {
			ID string `json:"id"`
		} `json:"release-group"`
	}
	if err := c.get(ctx, "/release/"+url.PathEscape(releaseID), url.Values{"inc": {"release-groups"}}, &out); err != nil {
		return "", err
	}
	if out.ReleaseGroup.ID == "" {
		return "", ErrNotFound
	}
	return out.ReleaseGroup.ID, nil
}

// ReleaseGroup looks a release group up, with genres.
func (c *Client) ReleaseGroup(ctx context.Context, id string) (ReleaseGroup, error) {
	var g ReleaseGroup
	err := c.get(ctx, "/release-group/"+url.PathEscape(id), url.Values{"inc": {"genres"}}, &g)
	return g, err
}

// SearchReleaseGroups finds an artist's release groups by title.
func (c *Client) SearchReleaseGroups(ctx context.Context, artist, title string) ([]ReleaseGroup, error) {
	var out struct {
		ReleaseGroups []ReleaseGroup `json:"release-groups"`
	}
	q := "releasegroup:" + quote(title) + " AND artist:" + quote(artist)
	err := c.get(ctx, "/release-group", url.Values{"query": {q}, "limit": {"10"}}, &out)
	return out.ReleaseGroups, err
}

// ReleaseType maps MusicBrainz's primary and secondary types to Marquee's release types.
func (g ReleaseGroup) ReleaseType() string {
	for _, s := range g.SecondaryTypes {
		switch strings.ToLower(s) {
		case "live":
			return "live"
		case "compilation", "dj-mix", "mixtape/street":
			return "compilation"
		case "soundtrack":
			return "soundtrack"
		case "remix":
			return "remix"
		case "demo":
			return "demo"
		}
	}
	switch strings.ToLower(g.PrimaryType) {
	case "album":
		return "album"
	case "ep":
		return "ep"
	case "single":
		return "single"
	case "":
		return ""
	}
	return "other"
}
