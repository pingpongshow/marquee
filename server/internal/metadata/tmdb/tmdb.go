// Package tmdb is a small client for The Movie Database API v3.
package tmdb

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

const (
	defaultBase = "https://api.themoviedb.org/3"
	ImageBase   = "https://image.tmdb.org/t/p/original"
)

var (
	ErrNoKey      = errors.New("TMDB API key not configured")
	ErrNotFound   = errors.New("not found on TMDB")
	ErrInvalidKey = errors.New("TMDB rejected the API key")
)

type Client struct {
	// Key is either a v3 API key or a v4 read access token (JWT, starts with "eyJ").
	Key      string
	Language string
	BaseURL  string
	HTTP     *http.Client
	limiter  chan struct{}
}

// New returns a client limited to ~20 requests/second (TMDB allows ~50).
func New(key, language string) *Client {
	c := &Client{Key: key, Language: language, BaseURL: defaultBase, HTTP: &http.Client{Timeout: 20 * time.Second},
		limiter: make(chan struct{}, 1)}
	go func() {
		t := time.NewTicker(50 * time.Millisecond)
		defer t.Stop()
		for range t.C {
			select {
			case c.limiter <- struct{}{}:
			default:
			}
		}
	}()
	return c
}

func (c *Client) get(ctx context.Context, path string, q url.Values, out any) error {
	if c.Key == "" {
		return ErrNoKey
	}
	if q == nil {
		q = url.Values{}
	}
	if c.Language != "" && q.Get("language") == "" {
		q.Set("language", c.Language)
	}
	bearer := strings.HasPrefix(c.Key, "eyJ")
	if !bearer {
		q.Set("api_key", c.Key)
	}
	u := c.BaseURL + path + "?" + q.Encode()

	for attempt := 0; ; attempt++ {
		if c.limiter != nil {
			select {
			case <-c.limiter:
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Accept", "application/json")
		if bearer {
			req.Header.Set("Authorization", "Bearer "+c.Key)
		}
		resp, err := c.HTTP.Do(req)
		if err != nil {
			if attempt < 2 && ctx.Err() == nil {
				time.Sleep(time.Second << attempt)
				continue
			}
			return err
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		resp.Body.Close()
		if err != nil {
			return err
		}
		switch {
		case resp.StatusCode == http.StatusOK:
			return json.Unmarshal(body, out)
		case resp.StatusCode == http.StatusNotFound:
			return ErrNotFound
		case resp.StatusCode == http.StatusUnauthorized:
			return ErrInvalidKey
		case (resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500) && attempt < 3:
			wait := time.Second << attempt
			if ra, err := strconv.Atoi(resp.Header.Get("Retry-After")); err == nil && ra > 0 {
				wait = time.Duration(ra) * time.Second
			}
			select {
			case <-time.After(wait):
			case <-ctx.Done():
				return ctx.Err()
			}
			continue
		default:
			return fmt.Errorf("tmdb %s: HTTP %d: %.200s", path, resp.StatusCode, body)
		}
	}
}

// ---- search ----

type SearchResult struct {
	ID            int     `json:"id"`
	Title         string  `json:"title"`          // movies
	Name          string  `json:"name"`           // tv
	OriginalTitle string  `json:"original_title"` // movies
	OriginalName  string  `json:"original_name"`  // tv
	ReleaseDate   string  `json:"release_date"`
	FirstAirDate  string  `json:"first_air_date"`
	Popularity    float64 `json:"popularity"`
	VoteCount     int     `json:"vote_count"`
	Overview      string  `json:"overview"`
	PosterPath    string  `json:"poster_path"`
}

func (r SearchResult) DisplayTitle() string {
	if r.Title != "" {
		return r.Title
	}
	return r.Name
}

func (r SearchResult) Original() string {
	if r.OriginalTitle != "" {
		return r.OriginalTitle
	}
	return r.OriginalName
}

func (r SearchResult) Year() int {
	d := r.ReleaseDate
	if d == "" {
		d = r.FirstAirDate
	}
	if len(d) >= 4 {
		y, _ := strconv.Atoi(d[:4])
		return y
	}
	return 0
}

type searchResponse struct {
	Results []SearchResult `json:"results"`
}

func (c *Client) SearchMovie(ctx context.Context, query string, year int) ([]SearchResult, error) {
	q := url.Values{"query": {query}, "include_adult": {"false"}}
	if year > 0 {
		q.Set("year", strconv.Itoa(year))
	}
	var r searchResponse
	err := c.get(ctx, "/search/movie", q, &r)
	return r.Results, err
}

func (c *Client) SearchTV(ctx context.Context, query string, year int) ([]SearchResult, error) {
	q := url.Values{"query": {query}, "include_adult": {"false"}}
	if year > 0 {
		q.Set("first_air_date_year", strconv.Itoa(year))
	}
	var r searchResponse
	err := c.get(ctx, "/search/tv", q, &r)
	return r.Results, err
}

// FindByExternalID resolves an IMDb or TVDB id to TMDB ids. source: imdb_id, tvdb_id.
func (c *Client) FindByExternalID(ctx context.Context, id, source string) (movieIDs, tvIDs []int, err error) {
	var r struct {
		Movies []SearchResult `json:"movie_results"`
		TV     []SearchResult `json:"tv_results"`
	}
	if err := c.get(ctx, "/find/"+url.PathEscape(id), url.Values{"external_source": {source}}, &r); err != nil {
		return nil, nil, err
	}
	for _, m := range r.Movies {
		movieIDs = append(movieIDs, m.ID)
	}
	for _, t := range r.TV {
		tvIDs = append(tvIDs, t.ID)
	}
	return movieIDs, tvIDs, nil
}

// ---- details ----

type Genre struct {
	Name string `json:"name"`
}

type Company struct {
	Name string `json:"name"`
}

type Cast struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Character   string `json:"character"`
	Order       int    `json:"order"`
	ProfilePath string `json:"profile_path"`
}

type Crew struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Job         string `json:"job"`
	Department  string `json:"department"`
	ProfilePath string `json:"profile_path"`
}

type Credits struct {
	Cast []Cast `json:"cast"`
	Crew []Crew `json:"crew"`
}

type Image struct {
	FilePath    string  `json:"file_path"`
	Width       int     `json:"width"`
	Height      int     `json:"height"`
	Language    *string `json:"iso_639_1"`
	VoteAverage float64 `json:"vote_average"`
	VoteCount   int     `json:"vote_count"`
}

type Images struct {
	Posters   []Image `json:"posters"`
	Backdrops []Image `json:"backdrops"`
	Logos     []Image `json:"logos"`
	Stills    []Image `json:"stills"`
}

type ExternalIDs struct {
	IMDB string `json:"imdb_id"`
	TVDB int    `json:"tvdb_id"`
}

type Movie struct {
	ID            int            `json:"id"`
	Title         string         `json:"title"`
	OriginalTitle string         `json:"original_title"`
	Overview      string         `json:"overview"`
	Tagline       string         `json:"tagline"`
	ReleaseDate   string         `json:"release_date"`
	Runtime       int            `json:"runtime"`
	VoteAverage   float64        `json:"vote_average"`
	Genres        []Genre        `json:"genres"`
	Companies     []Company      `json:"production_companies"`
	PosterPath    string         `json:"poster_path"`
	BackdropPath  string         `json:"backdrop_path"`
	IMDBID        string         `json:"imdb_id"`
	Collection    *CollectionRef `json:"belongs_to_collection"`
	Credits       Credits        `json:"credits"`
	Images        Images         `json:"images"`
	ReleaseDates  struct {
		Results []struct {
			Country string `json:"iso_3166_1"`
			Dates   []struct {
				Certification string `json:"certification"`
				Type          int    `json:"type"`
			} `json:"release_dates"`
		} `json:"results"`
	} `json:"release_dates"`
}

// Certification returns the content rating for country (e.g. "US" → "PG-13").
func (m *Movie) Certification(country string) string {
	for _, r := range m.ReleaseDates.Results {
		if r.Country != country {
			continue
		}
		for _, d := range r.Dates {
			if d.Certification != "" {
				return d.Certification
			}
		}
	}
	return ""
}

func (c *Client) imageLangs() string {
	lang := c.Language
	if i := strings.IndexByte(lang, '-'); i > 0 {
		lang = lang[:i]
	}
	if lang == "" || lang == "en" {
		return "en,null"
	}
	return lang + ",en,null"
}

// CollectionRef is the TMDB collection (film series) a movie belongs to.
type CollectionRef struct {
	ID           int    `json:"id"`
	Name         string `json:"name"`
	PosterPath   string `json:"poster_path"`
	BackdropPath string `json:"backdrop_path"`
}

// MovieBasic fetches a movie without credits and images (enough for its collection).
func (c *Client) MovieBasic(ctx context.Context, id int) (*Movie, error) {
	var m Movie
	err := c.get(ctx, "/movie/"+strconv.Itoa(id), nil, &m)
	return &m, err
}

func (c *Client) Movie(ctx context.Context, id int) (*Movie, error) {
	var m Movie
	err := c.get(ctx, "/movie/"+strconv.Itoa(id), url.Values{
		"append_to_response":     {"credits,images,release_dates"},
		"include_image_language": {c.imageLangs()},
	}, &m)
	return &m, err
}

type SeasonSummary struct {
	SeasonNumber int    `json:"season_number"`
	Name         string `json:"name"`
	Overview     string `json:"overview"`
	PosterPath   string `json:"poster_path"`
	AirDate      string `json:"air_date"`
	EpisodeCount int    `json:"episode_count"`
}

type TVShow struct {
	ID             int             `json:"id"`
	Name           string          `json:"name"`
	OriginalName   string          `json:"original_name"`
	Overview       string          `json:"overview"`
	Tagline        string          `json:"tagline"`
	FirstAirDate   string          `json:"first_air_date"`
	VoteAverage    float64         `json:"vote_average"`
	Genres         []Genre         `json:"genres"`
	Networks       []Company       `json:"networks"`
	PosterPath     string          `json:"poster_path"`
	BackdropPath   string          `json:"backdrop_path"`
	EpisodeRunTime []int           `json:"episode_run_time"`
	Seasons        []SeasonSummary `json:"seasons"`
	Credits        Credits         `json:"credits"`
	Images         Images          `json:"images"`
	ExternalIDs    ExternalIDs     `json:"external_ids"`
	ContentRatings struct {
		Results []struct {
			Country string `json:"iso_3166_1"`
			Rating  string `json:"rating"`
		} `json:"results"`
	} `json:"content_ratings"`
}

func (t *TVShow) Certification(country string) string {
	for _, r := range t.ContentRatings.Results {
		if r.Country == country {
			return r.Rating
		}
	}
	return ""
}

func (c *Client) TV(ctx context.Context, id int) (*TVShow, error) {
	var t TVShow
	err := c.get(ctx, "/tv/"+strconv.Itoa(id), url.Values{
		"append_to_response":     {"credits,images,external_ids,content_ratings"},
		"include_image_language": {c.imageLangs()},
	}, &t)
	return &t, err
}

type Episode struct {
	EpisodeNumber int     `json:"episode_number"`
	SeasonNumber  int     `json:"season_number"`
	Name          string  `json:"name"`
	Overview      string  `json:"overview"`
	AirDate       string  `json:"air_date"`
	Runtime       int     `json:"runtime"`
	StillPath     string  `json:"still_path"`
	VoteAverage   float64 `json:"vote_average"`
	Crew          []Crew  `json:"crew"`
}

type Season struct {
	SeasonNumber int       `json:"season_number"`
	Name         string    `json:"name"`
	Overview     string    `json:"overview"`
	AirDate      string    `json:"air_date"`
	PosterPath   string    `json:"poster_path"`
	Episodes     []Episode `json:"episodes"`
}

func (c *Client) Season(ctx context.Context, showID, season int) (*Season, error) {
	var s Season
	err := c.get(ctx, fmt.Sprintf("/tv/%d/season/%d", showID, season), nil, &s)
	return &s, err
}

// Validate checks the key with a cheap request.
func (c *Client) Validate(ctx context.Context) error {
	var v struct{}
	return c.get(ctx, "/configuration", nil, &v)
}
