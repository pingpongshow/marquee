// Package deezer looks up artist photos and album covers from Deezer's public API
// (no key required; rate limit ~50 requests per 5 seconds).
package deezer

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"time"
)

type Client struct {
	BaseURL string
	HTTP    *http.Client
	tick    *time.Ticker
}

func New() *Client {
	return &Client{BaseURL: "https://api.deezer.com", HTTP: &http.Client{Timeout: 15 * time.Second}, tick: time.NewTicker(150 * time.Millisecond)}
}

func (c *Client) get(ctx context.Context, path string, q url.Values, out any) error {
	select {
	case <-c.tick.C:
	case <-ctx.Done():
		return ctx.Err()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+path+"?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("deezer %s: HTTP %d", path, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

type Artist struct {
	Name      string `json:"name"`
	PictureXL string `json:"picture_xl"`
	NbFan     int    `json:"nb_fan"`
}

type Album struct {
	Title   string `json:"title"`
	CoverXL string `json:"cover_xl"`
	Artist  struct {
		Name string `json:"name"`
	} `json:"artist"`
}

func (c *Client) SearchArtists(ctx context.Context, name string) ([]Artist, error) {
	var r struct {
		Data  []Artist `json:"data"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := c.get(ctx, "/search/artist", url.Values{"q": {name}, "limit": {"10"}}, &r); err != nil {
		return nil, err
	}
	if r.Error != nil {
		return nil, fmt.Errorf("deezer: %s", r.Error.Message)
	}
	return r.Data, nil
}

func (c *Client) SearchAlbums(ctx context.Context, artist, album string) ([]Album, error) {
	var r struct {
		Data []Album `json:"data"`
	}
	q := fmt.Sprintf(`artist:"%s" album:"%s"`, artist, album)
	if err := c.get(ctx, "/search/album", url.Values{"q": {q}, "limit": {"10"}}, &r); err != nil {
		return nil, err
	}
	return r.Data, nil
}
