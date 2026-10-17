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
	"sort"
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
	Tags     []Genre `json:"tags"`
	Type     string  `json:"type"`   // Person, Group, Orchestra, Choir, Character, Other
	Gender   string  `json:"gender"` // Male, Female, Non-binary, Other (people only)
	Country  string  `json:"country"`
	Area     *Area   `json:"area"`
	Begin    *Area   `json:"begin-area"`
	LifeSpan struct {
		Begin string `json:"begin"`
		End   string `json:"end"`
		Ended bool   `json:"ended"`
	} `json:"life-span"`
	Disambiguation string `json:"disambiguation"`
	Rels           []struct {
		Type string `json:"type"`
		URL  struct {
			Resource string `json:"resource"`
		} `json:"url"`
	} `json:"relations"`
}

// Area is a country, city or region.
type Area struct {
	Name string `json:"name"`
	// ISO codes, present on country areas.
	Codes []string `json:"iso-3166-1-codes"`
}

// Years are the artist's begin and end years (0 when unknown).
func (a Artist) Years() (begin, end int) {
	return yearOf(a.LifeSpan.Begin), yearOf(a.LifeSpan.End)
}

// CountryCode is the artist's country (ISO 3166-1), from the country field or a country area.
func (a Artist) CountryCode() string {
	if len(a.Country) == 2 {
		return strings.ToUpper(a.Country)
	}
	if a.Area != nil && len(a.Area.Codes) > 0 {
		return strings.ToUpper(a.Area.Codes[0])
	}
	return ""
}

func yearOf(date string) int {
	if len(date) < 4 {
		return 0
	}
	y := 0
	for _, c := range date[:4] {
		if c < '0' || c > '9' {
			return 0
		}
		y = y*10 + int(c-'0')
	}
	return y
}

// Styles merges MusicBrainz genres and tags: the n most-voted names (lower case), votes > 0.
func Styles(genres, tags []Genre, n int) []string {
	votes := map[string]int{}
	var order []string
	for _, list := range [][]Genre{genres, tags} {
		for _, g := range list {
			k := strings.ToLower(strings.TrimSpace(g.Name))
			if k == "" || g.Count <= 0 {
				continue
			}
			if _, ok := votes[k]; !ok {
				order = append(order, k)
			}
			votes[k] = max(votes[k], g.Count)
		}
	}
	sort.SliceStable(order, func(i, j int) bool { return votes[order[i]] > votes[order[j]] })
	if len(order) > n {
		order = order[:n]
	}
	return order
}

// Wikidata is the artist's Wikidata id (e.g. Q44190), from its links.
func (a Artist) Wikidata() string {
	for _, r := range a.Rels {
		if r.Type == "wikidata" {
			if i := strings.LastIndex(r.URL.Resource, "/"); i >= 0 {
				return r.URL.Resource[i+1:]
			}
		}
	}
	return ""
}

type ReleaseGroup struct {
	ID               string   `json:"id"`
	Title            string   `json:"title"`
	Score            int      `json:"score"`
	PrimaryType      string   `json:"primary-type"`
	SecondaryTypes   []string `json:"secondary-types"`
	FirstReleaseDate string   `json:"first-release-date"`
	Disambiguation   string   `json:"disambiguation"`
	Genres           []Genre  `json:"genres"`
	Tags             []Genre  `json:"tags"`
	ArtistCredit     []struct {
		Name       string `json:"name"`
		JoinPhrase string `json:"joinphrase"`
		Artist     struct {
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
		case resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusBadRequest:
			// 400: MusicBrainz rejected the id or query for this item (e.g. a malformed MBID
			// tag); treated like "not found" so one bad item doesn't stop a whole run.
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
	err := c.get(ctx, "/artist/"+url.PathEscape(mbid), url.Values{"inc": {"genres tags url-rels"}}, &a)
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

// ReleaseGroupOf returns the release group of a release (an album's MBID in tags), and the
// release's first label (free in the same request).
func (c *Client) ReleaseGroupOf(ctx context.Context, releaseID string) (string, string, error) {
	var out struct {
		ReleaseGroup struct {
			ID string `json:"id"`
		} `json:"release-group"`
		LabelInfo []struct {
			Label *struct {
				Name string `json:"name"`
			} `json:"label"`
		} `json:"label-info"`
	}
	if err := c.get(ctx, "/release/"+url.PathEscape(releaseID), url.Values{"inc": {"release-groups labels"}}, &out); err != nil {
		return "", "", err
	}
	if out.ReleaseGroup.ID == "" {
		return "", "", ErrNotFound
	}
	label := ""
	for _, l := range out.LabelInfo {
		if l.Label != nil && l.Label.Name != "" && !strings.EqualFold(l.Label.Name, "[no label]") {
			label = l.Label.Name
			break
		}
	}
	return out.ReleaseGroup.ID, label, nil
}

// ReleaseGroup looks a release group up, with genres and its artist credit.
func (c *Client) ReleaseGroup(ctx context.Context, id string) (ReleaseGroup, error) {
	var g ReleaseGroup
	err := c.get(ctx, "/release-group/"+url.PathEscape(id), url.Values{"inc": {"genres tags artist-credits"}}, &g)
	return g, err
}

// SearchReleaseGroups finds an artist's release groups by title (any artist's when artist
// is empty).
func (c *Client) SearchReleaseGroups(ctx context.Context, artist, title string) ([]ReleaseGroup, error) {
	var out struct {
		ReleaseGroups []ReleaseGroup `json:"release-groups"`
	}
	q := "releasegroup:" + quote(title)
	if artist != "" {
		q += " AND artist:" + quote(artist)
	}
	err := c.get(ctx, "/release-group", url.Values{"query": {q}, "limit": {"10"}}, &out)
	return out.ReleaseGroups, err
}

// ReleaseGroupLabel is the first label of a release group's earliest release ("" when none).
func (c *Client) ReleaseGroupLabel(ctx context.Context, id string) (string, error) {
	var out struct {
		Releases []struct {
			Date      string `json:"date"`
			LabelInfo []struct {
				Label *struct {
					Name string `json:"name"`
				} `json:"label"`
			} `json:"label-info"`
		} `json:"releases"`
	}
	if err := c.get(ctx, "/release", url.Values{"release-group": {id}, "inc": {"labels"}, "limit": {"25"}}, &out); err != nil {
		return "", err
	}
	sort.SliceStable(out.Releases, func(i, j int) bool {
		a, b := out.Releases[i].Date, out.Releases[j].Date
		return a != "" && (b == "" || a < b)
	})
	for _, r := range out.Releases {
		for _, l := range r.LabelInfo {
			if l.Label != nil && l.Label.Name != "" && !strings.EqualFold(l.Label.Name, "[no label]") {
				return l.Label.Name, nil
			}
		}
	}
	return "", nil
}

// Credit is the release group's artist credit as printed ("Simon & Garfunkel").
func (g ReleaseGroup) Credit() string {
	var b strings.Builder
	for _, c := range g.ArtistCredit {
		b.WriteString(c.Name)
		b.WriteString(c.JoinPhrase)
	}
	return strings.TrimSpace(b.String())
}

// DeezerID is the artist's Deezer id, from its links ("" when MusicBrainz has none).
func (a Artist) DeezerID() string {
	for _, r := range a.Rels {
		u := r.URL.Resource
		if i := strings.Index(u, "deezer.com/"); i >= 0 {
			rest := u[i+len("deezer.com/"):]
			// deezer.com/artist/27 or deezer.com/en/artist/27
			if j := strings.Index(rest, "artist/"); j >= 0 && j <= 3 {
				id := rest[j+len("artist/"):]
				if k := strings.IndexAny(id, "/?#"); k >= 0 {
					id = id[:k]
				}
				if id != "" && strings.Trim(id, "0123456789") == "" {
					return id
				}
			}
		}
	}
	return ""
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
