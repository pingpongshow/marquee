package requests

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"sync"
	"time"

	"marquee/internal/settings"
)

var (
	ErrNotFound  = errors.New("request not found")
	ErrDecided   = errors.New("this request has already been decided")
	ErrDuplicate = errors.New("already requested")
	ErrAvailable = errors.New("already in the library")
)

const tmdbImages = "https://image.tmdb.org/t/p/"

// Service keeps requests and talks to Seerr.
type Service struct {
	DB       *sql.DB
	Settings *settings.Store

	approveMu sync.Mutex
	ratingsMu sync.Mutex
	ratings   map[string]cachedRatings
	genres    [][]Genre
	genresAt  time.Time
}

type cachedRatings struct {
	r   RatingsRaw
	at  time.Time
	ttl time.Duration
}

// Ratings are a title's public critic and audience ratings (REQ-2), kept for a day. Titles
// Seerr has no ratings for return empty ratings, not an error.
func (s *Service) Ratings(ctx context.Context, mediaType string, tmdbID int64) (RatingsRaw, error) {
	key := fmt.Sprintf("%s/%d", mediaType, tmdbID)
	s.ratingsMu.Lock()
	if c, ok := s.ratings[key]; ok && time.Since(c.at) < c.ttl {
		s.ratingsMu.Unlock()
		return c.r, nil
	}
	s.ratingsMu.Unlock()
	if !s.Enabled() {
		return RatingsRaw{}, ErrNotConfigured
	}
	r, err := s.seerr().Ratings(ctx, mediaType, tmdbID)
	ttl := 24 * time.Hour
	if err != nil {
		ttl = time.Hour
		// Seerr answers 404 (or 500) when RT or IMDb has nothing for the title.
		slog.Debug("seerr ratings", "title", key, "err", err)
		r = RatingsRaw{}
	}
	s.ratingsMu.Lock()
	if s.ratings == nil {
		s.ratings = map[string]cachedRatings{}
	}
	if len(s.ratings) > 2000 {
		for k, c := range s.ratings {
			if time.Since(c.at) > c.ttl {
				delete(s.ratings, k)
			}
		}
	}
	s.ratings[key] = cachedRatings{r, time.Now(), ttl}
	s.ratingsMu.Unlock()
	return r, nil
}

func (s *Service) seerr() *Seerr {
	c := s.Settings.Get().Integrations
	return &Seerr{URL: c.SeerrURL, APIKey: c.SeerrAPIKey}
}

// Enabled reports whether Seerr is configured.
func (s *Service) Enabled() bool {
	c := s.Settings.Get().Integrations
	return c.SeerrURL != "" && c.SeerrAPIKey != ""
}

// Test checks an address and key; an empty key uses the saved one.
func (s *Service) Test(ctx context.Context, u, key string) (string, error) {
	if key == "" {
		key = s.Settings.Get().Integrations.SeerrAPIKey
	}
	c := &Seerr{URL: u, APIKey: key}
	return c.Status(ctx)
}

func (s *Service) Users(ctx context.Context) ([]User, error) { return s.seerr().Users(ctx) }

// Availability values.
const (
	None       = "none"
	Pending    = "pending"
	Requested  = "requested"
	Processing = "processing"
	Partial    = "partial"
	Available  = "available"
)

// Item is a title for the Discover and search screens.
type Item struct {
	TMDBID       int64
	MediaType    string
	Title        string
	Year         int
	Overview     string
	PosterURL    string
	BackdropURL  string
	TMDBRating   float64 // TMDB's user score, 0–10 (0 = too few votes)
	Availability string
	ItemID       int64 // in this library
	RequestID    int64 // the caller's open request
}

func image(size, path string) string {
	if path == "" {
		return ""
	}
	return tmdbImages + size + path
}

func seerrAvailability(media int) string {
	switch media {
	case mediaAvailable:
		return Available
	case mediaPartial:
		return Partial
	case mediaProcessing:
		return Processing
	case mediaPending:
		return Requested
	}
	return None
}

// annotate turns Seerr results into Items, marking what's here or already asked for.
func (s *Service) annotate(ctx context.Context, userID int64, results []Result) []Item {
	out := make([]Item, 0, len(results))
	for _, r := range results {
		if r.MediaType != "movie" && r.MediaType != "tv" {
			continue
		}
		it := Item{TMDBID: r.ID, MediaType: r.MediaType, Title: r.DisplayTitle(), Year: r.Year(), Overview: r.Overview,
			PosterURL: image("w342", r.PosterPath), BackdropURL: image("w780", r.BackdropPath), Availability: seerrAvailability(r.MediaStatus())}
		if r.VoteCount >= 5 {
			it.TMDBRating = r.VoteAverage
		}
		it.ItemID = s.libraryItem(ctx, r.MediaType, r.ID)
		if it.ItemID != 0 && it.Availability != Available {
			if r.MediaType == "movie" {
				it.Availability = Available
			} else if it.Availability == None || it.Availability == Requested {
				it.Availability = Partial
			}
		}
		var status string
		s.DB.QueryRowContext(ctx, `SELECT id, status FROM media_requests WHERE media_type = ? AND tmdb_id = ? AND status IN ('pending', 'approved')
			ORDER BY user_id = ? DESC, created_at DESC LIMIT 1`, r.MediaType, r.ID, userID).Scan(&it.RequestID, &status)
		if status == "pending" && it.Availability == None {
			it.Availability = Pending
		}
		if status == "approved" && it.Availability == None {
			it.Availability = Requested
		}
		out = append(out, it)
	}
	return out
}

// libraryItem finds the movie or show with this TMDB id in the library.
func (s *Service) libraryItem(ctx context.Context, mediaType string, tmdbID int64) int64 {
	typ := "movie"
	if mediaType == "tv" {
		typ = "show"
	}
	var id int64
	s.DB.QueryRowContext(ctx, `SELECT i.id FROM external_ids e JOIN items i ON i.id = e.item_id
		WHERE e.provider = 'tmdb' AND e.value = ? AND i.type = ? LIMIT 1`, strconv.FormatInt(tmdbID, 10), typ).Scan(&id)
	return id
}

func (s *Service) Search(ctx context.Context, userID int64, q string, page int) ([]Item, int, int, error) {
	p, err := s.seerr().Search(ctx, q, page)
	if err != nil {
		return nil, 0, 0, err
	}
	return s.annotate(ctx, userID, p.Results), p.Page, p.TotalPages, nil
}

func (s *Service) Discover(ctx context.Context, userID int64, category string, b Browse, page int) ([]Item, int, int, error) {
	p, err := s.seerr().Discover(ctx, category, b, page)
	if err != nil {
		return nil, 0, 0, err
	}
	return s.annotate(ctx, userID, p.Results), p.Page, p.TotalPages, nil
}

// Networks and Studios are the ones offered as Discover filters (TMDB ids, as Seerr uses).
var (
	Networks = []Genre{{213, "Netflix"}, {2739, "Disney+"}, {1024, "Prime Video"}, {2552, "Apple TV+"}, {453, "Hulu"},
		{49, "HBO"}, {3186, "Max"}, {4330, "Paramount+"}, {3353, "Peacock"}, {67, "Showtime"}, {318, "Starz"},
		{174, "AMC"}, {88, "FX"}, {2, "ABC"}, {16, "CBS"}, {6, "NBC"}, {19, "FOX"}, {71, "The CW"}, {4, "BBC One"},
		{80, "Adult Swim"}, {1112, "Crunchyroll"}, {64, "Discovery"}}
	Studios = []Genre{{2, "Disney"}, {127928, "20th Century"}, {34, "Sony Pictures"}, {174, "Warner Bros."},
		{33, "Universal"}, {4, "Paramount"}, {3, "Pixar"}, {521, "DreamWorks"}, {420, "Marvel Studios"}, {1, "Lucasfilm"},
		{41077, "A24"}, {1632, "Lionsgate"}, {3172, "Blumhouse"}, {6704, "Illumination"}, {10342, "Studio Ghibli"}}
)

// Filters are Discover's choices: networks, studios and Seerr's genres (kept a day).
func (s *Service) Filters(ctx context.Context) (movieGenres, tvGenres []Genre, err error) {
	s.ratingsMu.Lock()
	if s.genres != nil && time.Since(s.genresAt) < 24*time.Hour {
		m, t := s.genres[0], s.genres[1]
		s.ratingsMu.Unlock()
		return m, t, nil
	}
	s.ratingsMu.Unlock()
	if !s.Enabled() {
		return nil, nil, ErrNotConfigured
	}
	if movieGenres, err = s.seerr().Genres(ctx, false); err != nil {
		return nil, nil, err
	}
	if tvGenres, err = s.seerr().Genres(ctx, true); err != nil {
		return nil, nil, err
	}
	s.ratingsMu.Lock()
	s.genres, s.genresAt = [][]Genre{movieGenres, tvGenres}, time.Now()
	s.ratingsMu.Unlock()
	return movieGenres, tvGenres, nil
}

// Season is a show's season and where it stands.
type Season struct {
	Number, Episodes   int
	Name, Availability string
}

func (s *Service) Show(ctx context.Context, tmdbID int64) (string, []Season, error) {
	sh, err := s.seerr().TV(ctx, tmdbID)
	if err != nil {
		return "", nil, err
	}
	states := map[int]int{}
	if sh.MediaInfo != nil {
		for _, x := range sh.MediaInfo.Seasons {
			states[x.SeasonNumber] = x.Status
		}
	}
	// Seasons waiting for approval in Marquee.
	pending := map[int]bool{}
	rows, err := s.DB.QueryContext(ctx, `SELECT seasons FROM media_requests WHERE media_type = 'tv' AND tmdb_id = ? AND status = 'pending'`, tmdbID)
	if err == nil {
		for rows.Next() {
			var raw string
			var ns []int
			if rows.Scan(&raw) == nil && json.Unmarshal([]byte(raw), &ns) == nil {
				if len(ns) == 0 {
					pending[-1] = true
				}
				for _, n := range ns {
					pending[n] = true
				}
			}
		}
		rows.Close()
	}
	var out []Season
	for _, x := range sh.Seasons {
		if x.SeasonNumber == 0 {
			continue // specials
		}
		a := seerrAvailability(states[x.SeasonNumber])
		if a == None && (pending[x.SeasonNumber] || pending[-1]) {
			a = Pending
		}
		out = append(out, Season{Number: x.SeasonNumber, Name: x.Name, Episodes: x.EpisodeCount, Availability: a})
	}
	return sh.DisplayTitle(), out, nil
}

// Request is a stored request.
type Request struct {
	ID             int64
	UserID         int64
	UserName       string
	TMDBID         int64
	MediaType      string
	Title          string
	Year           int
	PosterURL      string
	Seasons        []int
	Status         string
	Reason         string
	SeerrRequestID int64
	DecidedBy      string
	CreatedAt      time.Time
	DecidedAt      *time.Time
}

const cols = `r.id, r.user_id, u.display_name, r.tmdb_id, r.media_type, r.title, COALESCE(r.year, 0), COALESCE(r.poster_path, ''),
	r.seasons, r.status, COALESCE(r.reason, ''), COALESCE(r.seerr_request_id, 0), COALESCE(d.display_name, ''), r.created_at, r.decided_at`

const from = ` FROM media_requests r JOIN users u ON u.id = r.user_id LEFT JOIN users d ON d.id = r.decided_by`

func scan(row interface{ Scan(...any) error }) (Request, error) {
	var r Request
	var poster, seasons, created string
	var decided sql.NullString
	if err := row.Scan(&r.ID, &r.UserID, &r.UserName, &r.TMDBID, &r.MediaType, &r.Title, &r.Year, &poster, &seasons, &r.Status,
		&r.Reason, &r.SeerrRequestID, &r.DecidedBy, &created, &decided); err != nil {
		return r, err
	}
	r.PosterURL = image("w342", poster)
	json.Unmarshal([]byte(seasons), &r.Seasons)
	if r.Seasons == nil {
		r.Seasons = []int{}
	}
	r.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	if t, err := time.Parse(time.RFC3339Nano, decided.String); err == nil {
		r.DecidedAt = &t
	}
	return r, nil
}

func (s *Service) Get(ctx context.Context, id int64) (Request, error) {
	r, err := scan(s.DB.QueryRowContext(ctx, `SELECT `+cols+from+` WHERE r.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return r, ErrNotFound
	}
	return r, err
}

// List returns requests, newest first: one user's (userID > 0) or everyone's.
func (s *Service) List(ctx context.Context, userID int64, status string) ([]Request, error) {
	s.refresh(ctx)
	q := `SELECT ` + cols + from + ` WHERE 1 = 1`
	var args []any
	if userID > 0 {
		q += ` AND r.user_id = ?`
		args = append(args, userID)
	}
	if status != "" {
		q += ` AND r.status = ?`
		args = append(args, status)
	}
	rows, err := s.DB.QueryContext(ctx, q+` ORDER BY r.created_at DESC LIMIT 300`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Request{}
	for rows.Next() {
		r, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// PendingCount is how many requests wait for an admin.
func (s *Service) PendingCount(ctx context.Context) int {
	var n int
	s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM media_requests WHERE status = 'pending'`).Scan(&n)
	return n
}

// refresh updates approved requests from Seerr: arrived, or declined/failed there.
func (s *Service) refresh(ctx context.Context) {
	if !s.Enabled() {
		return
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT id, seerr_request_id FROM media_requests WHERE status = 'approved' AND seerr_request_id IS NOT NULL ORDER BY random() LIMIT 50`)
	if err != nil {
		return
	}
	type pair struct{ id, seerr int64 }
	var list []pair
	for rows.Next() {
		var p pair
		if rows.Scan(&p.id, &p.seerr) == nil {
			list = append(list, p)
		}
	}
	rows.Close()
	c := s.seerr()
	for _, p := range list {
		st, media, err := c.RequestState(ctx, p.seerr)
		if err != nil {
			continue
		}
		switch {
		case media == mediaAvailable:
			s.DB.ExecContext(ctx, `UPDATE media_requests SET status = 'available' WHERE id = ?`, p.id)
		case st == requestDeclined:
			s.DB.ExecContext(ctx, `UPDATE media_requests SET status = 'declined', reason = 'Declined in Seerr' WHERE id = ?`, p.id)
		case st == requestFailed:
			s.DB.ExecContext(ctx, `UPDATE media_requests SET status = 'failed', reason = 'Seerr couldn''t add it' WHERE id = ?`, p.id)
		}
	}
}

// Create records a request (pending approval). Details come from Seerr, not the client.
func (s *Service) Create(ctx context.Context, userID int64, mediaType string, tmdbID int64, seasons []int) (Request, error) {
	if mediaType != "movie" && mediaType != "tv" {
		return Request{}, fmt.Errorf("mediaType must be movie or tv")
	}
	c := s.seerr()
	var title, poster string
	var year, status int
	switch mediaType {
	case "movie":
		m, err := c.Movie(ctx, tmdbID)
		if err != nil {
			return Request{}, err
		}
		title, poster, year, status = m.DisplayTitle(), m.PosterPath, m.Year(), m.MediaStatus()
		if status == mediaAvailable || s.libraryItem(ctx, "movie", tmdbID) != 0 {
			return Request{}, ErrAvailable
		}
	case "tv":
		sh, err := c.TV(ctx, tmdbID)
		if err != nil {
			return Request{}, err
		}
		title, poster, year, status = sh.DisplayTitle(), sh.PosterPath, sh.Year(), sh.MediaStatus()
		if status == mediaAvailable {
			return Request{}, ErrAvailable
		}
		valid := map[int]bool{}
		for _, x := range sh.Seasons {
			valid[x.SeasonNumber] = true
		}
		for _, n := range seasons {
			if !valid[n] {
				return Request{}, fmt.Errorf("season %d doesn't exist", n)
			}
		}
	}
	if status == mediaPending || status == mediaProcessing {
		return Request{}, ErrDuplicate
	}
	slices.Sort(seasons)
	seasons = slices.Compact(seasons)
	var open int
	s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM media_requests WHERE media_type = ? AND tmdb_id = ? AND status = 'pending'`, mediaType, tmdbID).Scan(&open)
	if open > 0 && mediaType == "movie" {
		return Request{}, ErrDuplicate
	}
	if seasons == nil {
		seasons = []int{}
	}
	raw, _ := json.Marshal(seasons)
	res, err := s.DB.ExecContext(ctx, `INSERT INTO media_requests (user_id, tmdb_id, media_type, title, year, poster_path, seasons) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		userID, tmdbID, mediaType, title, nullInt(year), poster, string(raw))
	if err != nil {
		return Request{}, err
	}
	id, _ := res.LastInsertId()
	slog.Info("title requested", "title", title, "type", mediaType, "user", userID)
	return s.Get(ctx, id)
}

func nullInt(v int) any {
	if v == 0 {
		return nil
	}
	return v
}

// Approve sends a pending request to Seerr, as the requester's linked Seerr user.
func (s *Service) Approve(ctx context.Context, id, adminID int64, seerrUser func(userID int64) *int64) (Request, error) {
	// One approval at a time, so a double click or two admins can't send it to Seerr twice.
	s.approveMu.Lock()
	defer s.approveMu.Unlock()
	r, err := s.Get(ctx, id)
	if err != nil {
		return r, err
	}
	if r.Status != "pending" {
		return r, ErrDecided
	}
	sid, err := s.seerr().Request(ctx, r.MediaType, r.TMDBID, r.Seasons, seerrUser(r.UserID))
	if err != nil {
		s.DB.ExecContext(ctx, `UPDATE media_requests SET status = 'failed', reason = ?, decided_by = ?, decided_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = ?`,
			err.Error(), adminID, id)
		slog.Warn("request to Seerr failed", "title", r.Title, "err", err)
		return s.Get(ctx, id)
	}
	if _, err := s.DB.ExecContext(ctx, `UPDATE media_requests SET status = 'approved', seerr_request_id = ?, decided_by = ?, decided_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = ?`,
		sid, adminID, id); err != nil {
		return r, err
	}
	slog.Info("request approved", "title", r.Title, "seerr", sid)
	return s.Get(ctx, id)
}

func (s *Service) Decline(ctx context.Context, id, adminID int64, reason string) (Request, error) {
	r, err := s.Get(ctx, id)
	if err != nil {
		return r, err
	}
	if r.Status != "pending" {
		return r, ErrDecided
	}
	s.DB.ExecContext(ctx, `UPDATE media_requests SET status = 'declined', reason = ?, decided_by = ?, decided_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = ?`,
		reason, adminID, id)
	return s.Get(ctx, id)
}

// Cancel withdraws a pending request.
func (s *Service) Cancel(ctx context.Context, id int64) error {
	r, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if r.Status != "pending" {
		return ErrDecided
	}
	_, err = s.DB.ExecContext(ctx, `DELETE FROM media_requests WHERE id = ?`, id)
	return err
}
