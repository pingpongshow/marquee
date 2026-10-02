package musicbrainz

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Bios and popularity from services MusicBrainz links to (MUSIC-15): Wikipedia summaries via
// Wikidata, and ListenBrainz listen counts. Neither needs a key.

// Web reaches Wikidata, Wikipedia and ListenBrainz; base URLs are settable for tests.
type Web struct {
	Wikidata, Wikipedia, ListenBrainz string // e.g. https://www.wikidata.org, https://%s.wikipedia.org, https://api.listenbrainz.org
	UserAgent                         string
	HTTP                              *http.Client
}

func NewWeb(userAgent string) *Web {
	return &Web{Wikidata: "https://www.wikidata.org", Wikipedia: "https://%s.wikipedia.org", ListenBrainz: "https://api.listenbrainz.org",
		UserAgent: userAgent, HTTP: &http.Client{Timeout: 20 * time.Second}}
}

func (w *Web) get(ctx context.Context, u string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", w.UserAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := w.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: HTTP %d", u, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// Bio is the opening of the artist's Wikipedia article in lang (falling back to English).
func (w *Web) Bio(ctx context.Context, wikidataID, lang string) (string, error) {
	var ent struct {
		Entities map[string]struct {
			Sitelinks map[string]struct {
				Title string `json:"title"`
			} `json:"sitelinks"`
		} `json:"entities"`
	}
	if err := w.get(ctx, w.Wikidata+"/wiki/Special:EntityData/"+url.PathEscape(wikidataID)+".json", &ent); err != nil {
		return "", err
	}
	links := ent.Entities[wikidataID].Sitelinks
	if lang == "" {
		lang = "en"
	}
	for _, l := range []string{lang, "en"} {
		site, ok := links[l+"wiki"]
		if !ok {
			continue
		}
		var sum struct {
			Type    string `json:"type"`
			Extract string `json:"extract"`
		}
		title := strings.ReplaceAll(site.Title, " ", "_")
		if err := w.get(ctx, fmt.Sprintf(w.Wikipedia, l)+"/api/rest_v1/page/summary/"+url.PathEscape(title), &sum); err != nil {
			return "", err
		}
		if sum.Type != "disambiguation" && sum.Extract != "" {
			return sum.Extract, nil
		}
	}
	return "", ErrNotFound
}

// Recording is one of an artist's most-listened recordings.
type Recording struct {
	MBID    string `json:"recording_mbid"`
	Name    string `json:"recording_name"`
	Listens int    `json:"total_listen_count"`
}

// TopRecordings are the artist's most-listened recordings on ListenBrainz, most first.
func (w *Web) TopRecordings(ctx context.Context, artistMBID string) ([]Recording, error) {
	var out []Recording
	err := w.get(ctx, w.ListenBrainz+"/1/popularity/top-recordings-for-artist/"+url.PathEscape(artistMBID), &out)
	return out, err
}
