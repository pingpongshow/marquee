package vod

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Pluto TV's catalogue gives each film's year, plot, runtime, rating, cast and artwork.
// It needs no account: a session token from Pluto's public start endpoint is enough.
// The year sharpens TMDB matching, and the details stand in when TMDB has no match or no
// key is set.

var (
	plutoBoot = "https://boot.pluto.tv/v4/start"
	plutoVOD  = "https://service-vod.clusters.pluto.tv/v4/vod/items"
)

type plutoItem struct {
	ID          string `json:"_id"`
	Description string `json:"description"`
	Summary     string `json:"summary"`
	Rating      string `json:"rating"`
	Genre       string `json:"genre"`
	Duration    int64  `json:"duration"` // ms
	Covers      []struct {
		AspectRatio string `json:"aspectRatio"`
		URL         string `json:"url"`
	} `json:"covers"`
	Clip struct {
		Actors              []string `json:"actors"`
		Directors           []string `json:"directors"`
		OriginalReleaseDate string   `json:"originalReleaseDate"`
	} `json:"clip"`
}

func (s *Service) plutoToken(ctx context.Context) (string, error) {
	id := make([]byte, 16)
	rand.Read(id)
	q := url.Values{"appName": {"web"}, "appVersion": {"8.1.0"}, "deviceVersion": {"133.0.0"}, "deviceModel": {"web"},
		"deviceMake": {"chrome"}, "deviceType": {"web"}, "clientID": {hex.EncodeToString(id)},
		"clientModelNumber": {"1.0.0"}, "serverSideAds": {"false"}}
	body, err := s.get(ctx, plutoBoot+"?"+q.Encode(), "")
	if err != nil {
		return "", err
	}
	defer body.Close()
	var r struct {
		SessionToken string `json:"sessionToken"`
	}
	if err := json.NewDecoder(io.LimitReader(body, 1<<20)).Decode(&r); err != nil {
		return "", err
	}
	if r.SessionToken == "" {
		return "", fmt.Errorf("pluto gave no session token")
	}
	return r.SessionToken, nil
}

func (s *Service) plutoItems(ctx context.Context, token string, ids []string) ([]plutoItem, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, plutoVOD+"?ids="+strings.Join(ids, ","), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := s.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("pluto catalogue answered %d", resp.StatusCode)
	}
	var items []plutoItem
	err = json.NewDecoder(io.LimitReader(resp.Body, 32<<20)).Decode(&items)
	return items, err
}

// fetchPlutoDetails fills in films from Pluto that haven't had their details yet.
func (s *Service) fetchPlutoDetails(ctx context.Context) error {
	rows, err := s.DB.QueryContext(ctx, "SELECT id, source_key FROM vod_movies WHERE details_at IS NULL AND source_key LIKE 'pluto:%'")
	if err != nil {
		return err
	}
	byKey := map[string]int64{}
	var ids []string
	for rows.Next() {
		var id int64
		var k string
		rows.Scan(&id, &k)
		byKey[strings.TrimPrefix(k, "pluto:")] = id
		ids = append(ids, strings.TrimPrefix(k, "pluto:"))
	}
	rows.Close()
	if len(ids) == 0 {
		return nil
	}
	token, err := s.plutoToken(ctx)
	if err != nil {
		return err
	}
	for i := 0; i < len(ids); i += 100 {
		batch := ids[i:min(i+100, len(ids))]
		items, err := s.plutoItems(ctx, token, batch)
		if err != nil {
			return err
		}
		for _, it := range items {
			if id, ok := byKey[it.ID]; ok {
				if err := s.applyPluto(ctx, id, it); err != nil {
					return err
				}
			}
		}
		// Films Pluto no longer describes are marked too, so they aren't asked about again.
		for _, k := range batch {
			s.DB.ExecContext(ctx, "UPDATE vod_movies SET details_at=? WHERE id=? AND details_at IS NULL", now(), byKey[k])
		}
	}
	return nil
}

// applyPluto stores Pluto's details where nothing better (TMDB's) is there yet.
func (s *Service) applyPluto(ctx context.Context, id int64, it plutoItem) error {
	var year any
	if d := it.Clip.OriginalReleaseDate; len(d) >= 4 {
		if y, err := strconv.Atoi(d[:4]); err == nil && y > 1880 {
			year = y
		}
	}
	plot := it.Description
	if plot == "" {
		plot = it.Summary
	}
	var genres []string
	if it.Genre != "" {
		genres = []string{it.Genre}
	}
	cast := make([]CastMember, 0, len(it.Clip.Actors))
	for _, a := range it.Clip.Actors {
		cast = append(cast, CastMember{Name: a})
	}
	poster, backdrop := "", ""
	for _, c := range it.Covers {
		switch c.AspectRatio {
		case "347:500":
			poster = c.URL
		case "16:9":
			backdrop = c.URL
		}
	}
	rating := it.Rating
	if rating == "NR" || rating == "Not Rated" {
		rating = ""
	}
	gj, _ := json.Marshal(genres)
	if genres == nil {
		gj = []byte("[]")
	}
	cj, _ := json.Marshal(cast)
	_, err := s.DB.ExecContext(ctx, `UPDATE vod_movies SET details_at=?, year=COALESCE(year, ?),
		overview=CASE WHEN overview='' THEN ? ELSE overview END,
		content_rating=CASE WHEN content_rating='' THEN ? ELSE content_rating END,
		genres=CASE WHEN genres='[]' THEN ? ELSE genres END,
		runtime_ms=CASE WHEN runtime_ms=0 THEN ? ELSE runtime_ms END,
		director=CASE WHEN director='' THEN ? ELSE director END,
		cast_json=CASE WHEN cast_json='[]' THEN ? ELSE cast_json END,
		poster_url=CASE WHEN poster_url='' THEN ? ELSE poster_url END,
		backdrop_url=CASE WHEN backdrop_url='' THEN ? ELSE backdrop_url END WHERE id=?`,
		now(), year, plot, rating, string(gj), it.Duration, strings.Join(it.Clip.Directors, ", "), string(cj), poster, backdrop, id)
	return err
}
