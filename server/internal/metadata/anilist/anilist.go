// Package anilist looks anime up on AniList (GraphQL, no key; rate limited) and maps TMDB ids
// to AniList ids with the community anime-lists mapping (github.com/Fribb/anime-lists).
package anilist

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

var ErrNotFound = errors.New("not found on AniList")

type Client struct {
	URL, MappingURL string
	UserAgent       string
	HTTP            *http.Client
	// Interval between AniList requests (its limit is 90 a minute, 30 while degraded).
	Interval time.Duration

	mu   sync.Mutex
	last time.Time
}

func New(userAgent string) *Client {
	return &Client{URL: "https://graphql.anilist.co", MappingURL: "https://raw.githubusercontent.com/Fribb/anime-lists/master/anime-list-full.json",
		UserAgent: userAgent, HTTP: &http.Client{Timeout: 60 * time.Second}, Interval: 2100 * time.Millisecond}
}

type Media struct {
	ID    int64 `json:"id"`
	IDMal int64 `json:"idMal"`
	Title struct {
		Romaji  string `json:"romaji"`
		English string `json:"english"`
		Native  string `json:"native"`
	} `json:"title"`
	Synonyms     []string `json:"synonyms"`
	Format       string   `json:"format"`
	Genres       []string `json:"genres"`
	AverageScore int      `json:"averageScore"`
	Description  string   `json:"description"`
	StartDate    struct {
		Year int `json:"year"`
	} `json:"startDate"`
	Studios struct {
		Nodes []struct {
			Name string `json:"name"`
		} `json:"nodes"`
	} `json:"studios"`
	Tags []struct {
		Name     string `json:"name"`
		Rank     int    `json:"rank"`
		IsAdult  bool   `json:"isAdult"`
		IsSpoil  bool   `json:"isMediaSpoiler"`
		Category string `json:"category"`
	} `json:"tags"`
}

const fields = `id idMal title { romaji english native } synonyms format genres averageScore description(asHtml: false)
	startDate { year } studios(isMain: true) { nodes { name } } tags { name rank isAdult isMediaSpoiler category }`

func (c *Client) query(ctx context.Context, q string, vars map[string]any, out any) error {
	for attempt := 0; ; attempt++ {
		c.mu.Lock()
		if d := time.Until(c.last.Add(c.Interval)); d > 0 {
			c.mu.Unlock()
			select {
			case <-time.After(d):
			case <-ctx.Done():
				return ctx.Err()
			}
			c.mu.Lock()
		}
		c.last = time.Now()
		c.mu.Unlock()
		body, _ := json.Marshal(map[string]any{"query": q, "variables": vars})
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", c.UserAgent)
		resp, err := c.HTTP.Do(req)
		if err != nil {
			return err
		}
		if resp.StatusCode == http.StatusTooManyRequests && attempt < 3 {
			resp.Body.Close()
			select {
			case <-time.After(time.Duration(30*(attempt+1)) * time.Second):
			case <-ctx.Done():
				return ctx.Err()
			}
			continue
		}
		var res struct {
			Data   json.RawMessage `json:"data"`
			Errors []struct {
				Message string `json:"message"`
				Status  int    `json:"status"`
			} `json:"errors"`
		}
		err = json.NewDecoder(resp.Body).Decode(&res)
		resp.Body.Close()
		if err != nil {
			return fmt.Errorf("anilist: HTTP %d", resp.StatusCode)
		}
		if len(res.Errors) > 0 {
			if res.Errors[0].Status == http.StatusNotFound {
				return ErrNotFound
			}
			return fmt.Errorf("anilist: %s", res.Errors[0].Message)
		}
		return json.Unmarshal(res.Data, out)
	}
}

// Media looks one anime up by AniList id.
func (c *Client) Media(ctx context.Context, id int64) (Media, error) {
	var out struct {
		Media Media `json:"Media"`
	}
	err := c.query(ctx, `query($id: Int) { Media(id: $id, type: ANIME) { `+fields+` } }`, map[string]any{"id": id}, &out)
	return out.Media, err
}

// Search finds anime by title.
func (c *Client) Search(ctx context.Context, title string) ([]Media, error) {
	var out struct {
		Page struct {
			Media []Media `json:"media"`
		} `json:"Page"`
	}
	err := c.query(ctx, `query($s: String) { Page(perPage: 10) { media(search: $s, type: ANIME) { `+fields+` } } }`, map[string]any{"s": title}, &out)
	return out.Page.Media, err
}

var tags = regexp.MustCompile(`<[^>]+>`)

// PlainDescription is the description without markup or source notes.
func (m Media) PlainDescription() string {
	d := strings.ReplaceAll(m.Description, "<br>", "\n")
	d = tags.ReplaceAllString(d, "")
	if i := strings.Index(d, "(Source:"); i > 0 {
		d = d[:i]
	}
	return strings.TrimSpace(d)
}

// Mapping is the TMDB → AniList part of the anime-lists mapping.
type Mapping struct {
	TV     map[int64]int64 // TMDB show → AniList id of its first season
	Movies map[int64]int64
}

// LoadMapping reads the mapping, downloading it into cacheFile when it's missing or over a
// week old (a stale copy is used if the download fails).
func (c *Client) LoadMapping(ctx context.Context, cacheFile string) (Mapping, error) {
	st, err := os.Stat(cacheFile)
	if err != nil || time.Since(st.ModTime()) > 7*24*time.Hour {
		if derr := c.download(ctx, cacheFile); derr != nil && err != nil {
			return Mapping{}, derr
		}
	}
	f, err := os.Open(cacheFile)
	if err != nil {
		return Mapping{}, err
	}
	defer f.Close()
	var entries []struct {
		AniList int64           `json:"anilist_id"`
		TMDB    json.RawMessage `json:"themoviedb_id"`
		Season  struct {
			TMDB int `json:"tmdb"`
		} `json:"season"`
	}
	if err := json.NewDecoder(f).Decode(&entries); err != nil {
		return Mapping{}, err
	}
	m := Mapping{TV: map[int64]int64{}, Movies: map[int64]int64{}}
	season := map[int64]int{}
	for _, e := range entries {
		if e.AniList == 0 || len(e.TMDB) == 0 {
			continue
		}
		var ids struct {
			TV    int64 `json:"tv"`
			Movie int64 `json:"movie"`
		}
		if json.Unmarshal(e.TMDB, &ids) != nil {
			continue
		}
		if ids.Movie > 0 {
			if _, ok := m.Movies[ids.Movie]; !ok {
				m.Movies[ids.Movie] = e.AniList
			}
		}
		if ids.TV > 0 {
			// The show is described by its earliest season's entry.
			s := e.Season.TMDB
			if s <= 0 {
				s = 1000
			}
			if cur, ok := season[ids.TV]; !ok || s < cur {
				season[ids.TV] = s
				m.TV[ids.TV] = e.AniList
			}
		}
	}
	return m, nil
}

func (c *Client) download(ctx context.Context, dest string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.MappingURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("anime-lists: HTTP %d", resp.StatusCode)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	tmp := dest + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, io.LimitReader(resp.Body, 64<<20)); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	f.Close()
	return os.Rename(tmp, dest)
}
