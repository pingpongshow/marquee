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
	"sync"
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
	limiter  *limiter
}

// limiter spaces requests every apart. It needs no goroutine: a client used to start a
// ticker goroutine that never stopped, one more each time a client was made.
type limiter struct {
	every time.Duration
	mu    sync.Mutex
	next  time.Time
}

func (l *limiter) wait(ctx context.Context) error {
	l.mu.Lock()
	now := time.Now()
	at := l.next
	if at.Before(now) {
		at = now
	}
	l.next = at.Add(l.every)
	l.mu.Unlock()
	d := time.Until(at)
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// New returns a client limited to ~20 requests/second (TMDB allows ~50).
func New(key, language string) *Client {
	return &Client{Key: key, Language: language, BaseURL: defaultBase, HTTP: &http.Client{Timeout: 20 * time.Second},
		limiter: &limiter{every: 50 * time.Millisecond}}
}

// WithLanguage returns a client for another language sharing this one's rate limit.
func (c *Client) WithLanguage(language string) *Client {
	cp := *c
	cp.Language = language
	return &cp
}

// redact keeps the API key out of a request error (a *url.Error carries the whole URL,
// api_key included, into logs and task messages).
func (c *Client) redact(err error) error {
	var ue *url.Error
	if errors.As(err, &ue) && c.Key != "" && strings.Contains(ue.URL, c.Key) {
		return &url.Error{Op: ue.Op, URL: strings.ReplaceAll(ue.URL, url.QueryEscape(c.Key), "REDACTED"), Err: ue.Err}
	}
	return err
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
			if err := c.limiter.wait(ctx); err != nil {
				return err
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
			return c.redact(err)
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

// Find resolves an IMDb or TVDB id to TMDB movies and shows. source: imdb_id, tvdb_id.
func (c *Client) Find(ctx context.Context, id, source string) (movies, tv []SearchResult, err error) {
	var r struct {
		Movies []SearchResult `json:"movie_results"`
		TV     []SearchResult `json:"tv_results"`
	}
	if err := c.get(ctx, "/find/"+url.PathEscape(id), url.Values{"external_source": {source}}, &r); err != nil {
		return nil, nil, err
	}
	return r.Movies, r.TV, nil
}

// FindByExternalID resolves an IMDb or TVDB id to TMDB ids. source: imdb_id, tvdb_id.
func (c *Client) FindByExternalID(ctx context.Context, id, source string) (movieIDs, tvIDs []int, err error) {
	movies, tv, err := c.Find(ctx, id, source)
	if err != nil {
		return nil, nil, err
	}
	for _, m := range movies {
		movieIDs = append(movieIDs, m.ID)
	}
	for _, t := range tv {
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
	VoteCount     int            `json:"vote_count"`
	Genres        []Genre        `json:"genres"`
	Companies     []Company      `json:"production_companies"`
	PosterPath    string         `json:"poster_path"`
	BackdropPath  string         `json:"backdrop_path"`
	IMDBID        string         `json:"imdb_id"`
	Collection    *CollectionRef `json:"belongs_to_collection"`
	// Extras for Muse: original language, production countries, keywords, recommendations.
	OriginalLanguage string    `json:"original_language"`
	Countries        []Country `json:"production_countries"`
	Keywords         struct {
		Keywords []Keyword `json:"keywords"`
	} `json:"keywords"`
	Recommendations Page    `json:"recommendations"`
	Credits         Credits `json:"credits"`
	Images          Images  `json:"images"`
	ReleaseDates    struct {
		Results []struct {
			Country string `json:"iso_3166_1"`
			Dates   []struct {
				Certification string `json:"certification"`
				Type          int    `json:"type"`
			} `json:"release_dates"`
		} `json:"results"`
	} `json:"release_dates"`
}

// Country is a production country.
type Country struct {
	ISO  string `json:"iso_3166_1"`
	Name string `json:"name"`
}

// Keyword is one of TMDB's plot keywords ("time loop", "heist").
type Keyword struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

// Page is the first page of a list of movies or shows (recommendations).
type Page struct {
	Results []SearchResult `json:"results"`
}

// IDs are the TMDB ids on the page, in order.
func (p Page) IDs() []int {
	out := make([]int, 0, len(p.Results))
	for _, r := range p.Results {
		if r.ID > 0 {
			out = append(out, r.ID)
		}
	}
	return out
}

// KeywordNames are the movie's keywords.
func (m *Movie) KeywordNames() []string { return keywordNames(m.Keywords.Keywords) }

// CountryCodes are the movie's production countries (ISO 3166-1).
func (m *Movie) CountryCodes() []string { return countryCodes(m.Countries, nil) }

// KeywordNames are the show's keywords.
func (t *TVShow) KeywordNames() []string { return keywordNames(t.Keywords.Results) }

// CountryCodes are the show's origin and production countries (ISO 3166-1).
func (t *TVShow) CountryCodes() []string { return countryCodes(t.Countries, t.OriginCountry) }

func keywordNames(ks []Keyword) []string {
	var out []string
	seen := map[string]bool{}
	for _, k := range ks {
		n := strings.ToLower(strings.TrimSpace(k.Name))
		if n != "" && !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	return out
}

func countryCodes(cs []Country, extra []string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(c string) {
		c = strings.ToUpper(strings.TrimSpace(c))
		if len(c) == 2 && !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	for _, c := range extra {
		add(c)
	}
	for _, c := range cs {
		add(c.ISO)
	}
	return out
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
		"append_to_response":     {"credits,images,release_dates,keywords,recommendations"},
		"include_image_language": {c.imageLangs()},
	}, &m)
	return &m, err
}

// MovieExtras fetches a movie's language, countries, keywords and recommendations (and its
// basic details), for items matched before these were stored.
func (c *Client) MovieExtras(ctx context.Context, id int) (*Movie, error) {
	var m Movie
	err := c.get(ctx, "/movie/"+strconv.Itoa(id), url.Values{"append_to_response": {"keywords,recommendations"}}, &m)
	return &m, err
}

// TVExtras is MovieExtras for a show.
func (c *Client) TVExtras(ctx context.Context, id int) (*TVShow, error) {
	var t TVShow
	err := c.get(ctx, "/tv/"+strconv.Itoa(id), url.Values{"append_to_response": {"keywords,recommendations"}}, &t)
	return &t, err
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
	VoteCount      int             `json:"vote_count"`
	Genres         []Genre         `json:"genres"`
	Networks       []Company       `json:"networks"`
	PosterPath     string          `json:"poster_path"`
	BackdropPath   string          `json:"backdrop_path"`
	EpisodeRunTime []int           `json:"episode_run_time"`
	Seasons        []SeasonSummary `json:"seasons"`
	Credits        Credits         `json:"credits"`
	Images         Images          `json:"images"`
	ExternalIDs    ExternalIDs     `json:"external_ids"`
	// Extras for Muse: original language, countries, keywords, recommendations.
	OriginalLanguage string    `json:"original_language"`
	OriginCountry    []string  `json:"origin_country"`
	Countries        []Country `json:"production_countries"`
	Keywords         struct {
		Results []Keyword `json:"results"`
	} `json:"keywords"`
	Recommendations Page `json:"recommendations"`
	ContentRatings  struct {
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

// TVBasic fetches a show without credits, images or seasons' details.
func (c *Client) TVBasic(ctx context.Context, id int) (*TVShow, error) {
	var t TVShow
	err := c.get(ctx, "/tv/"+strconv.Itoa(id), nil, &t)
	return &t, err
}

func (c *Client) TV(ctx context.Context, id int) (*TVShow, error) {
	var t TVShow
	err := c.get(ctx, "/tv/"+strconv.Itoa(id), url.Values{
		"append_to_response":     {"credits,images,external_ids,content_ratings,keywords,recommendations"},
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

// Video is one of a movie's or show's videos (trailers, teasers, clips) on YouTube or Vimeo.
type Video struct {
	Key         string `json:"key"`
	Name        string `json:"name"`
	Site        string `json:"site"`
	Type        string `json:"type"`
	Official    bool   `json:"official"`
	Language    string `json:"iso_639_1"`
	PublishedAt string `json:"published_at"`
}

// Videos lists a movie's ("movie") or show's ("tv") videos in the client's language and
// English, plus those with no language.
func (c *Client) Videos(ctx context.Context, kind string, id int) ([]Video, error) {
	langs := "en,null"
	if l := strings.SplitN(c.Language, "-", 2)[0]; l != "" && l != "en" {
		langs = l + "," + langs
	}
	var out struct {
		Results []Video `json:"results"`
	}
	err := c.get(ctx, "/"+kind+"/"+strconv.Itoa(id)+"/videos", url.Values{"include_video_language": {langs}}, &out)
	return out.Results, err
}
