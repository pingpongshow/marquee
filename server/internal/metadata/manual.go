package metadata

import (
	"context"
	"database/sql"
	"errors"
	"regexp"
	"strconv"
	"strings"

	"marquee/internal/metadata/tmdb"
)

var (
	ErrNotMatchable = errors.New("only movies and shows can be matched")
	ErrNotMatched   = errors.New("item is not matched to TMDB")
)

// MatchRefError is a TMDB or IMDb id that can't be used for this item (a show's link pasted
// into a movie's Fix Match); its text is shown to the admin.
type MatchRefError string

func (e MatchRefError) Error() string { return string(e) }

type Candidate struct {
	Provider, ID, Title, Original, Overview, PosterURL string
	Year                                               int
	Current                                            bool
}

func (s *Service) itemInfo(ctx context.Context, itemID int64) (typ, title string, year int, lang string, tmdbID int, err error) {
	var libLang sql.NullString
	err = s.DB.QueryRowContext(ctx, `SELECT i.type, i.title, COALESCE(i.year, 0), json_extract(l.options, '$.language'),
		COALESCE((SELECT CAST(value AS INTEGER) FROM external_ids WHERE item_id = i.id AND provider = 'tmdb'), 0)
		FROM items i JOIN libraries l ON l.id = i.library_id WHERE i.id = ?`, itemID).Scan(&typ, &title, &year, &libLang, &tmdbID)
	lang = libLang.String
	if lang == "" {
		lang = s.Settings.Get().General.MetadataLanguage
	}
	if err == nil && typ != "movie" && typ != "show" {
		err = ErrNotMatchable
	}
	return
}

// Candidates searches TMDB for Fix Match. Empty title/zero year default to the item's own.
func (s *Service) Candidates(ctx context.Context, itemID int64, title string, year int) ([]Candidate, error) {
	typ, curTitle, curYear, lang, current, err := s.itemInfo(ctx, itemID)
	if err != nil {
		return nil, err
	}
	if title == "" {
		title, year = curTitle, curYear
	}
	c, err := s.tmdb(lang)
	if err != nil {
		return nil, err
	}
	if ref, ok := parseMatchRef(title); ok {
		r, err := lookupRef(ctx, c, typ, ref)
		if err != nil || r == nil {
			return []Candidate{}, err
		}
		return []Candidate{candidate(*r, current)}, nil
	}
	var res []tmdb.SearchResult
	if typ == "movie" {
		res, err = c.SearchMovie(ctx, title, year)
		if err == nil && len(res) == 0 && year > 0 {
			res, err = c.SearchMovie(ctx, title, 0)
		}
	} else {
		res, err = c.SearchTV(ctx, title, year)
		if err == nil && len(res) == 0 && year > 0 {
			res, err = c.SearchTV(ctx, title, 0)
		}
	}
	if err != nil {
		return nil, err
	}
	out := make([]Candidate, 0, len(res))
	for _, r := range res {
		out = append(out, candidate(r, current))
	}
	return out, nil
}

func candidate(r tmdb.SearchResult, current int) Candidate {
	cand := Candidate{Provider: "tmdb", ID: strconv.Itoa(r.ID), Title: r.DisplayTitle(), Year: r.Year(),
		Overview: r.Overview, Current: r.ID == current}
	if o := r.Original(); o != cand.Title {
		cand.Original = o
	}
	if r.PosterPath != "" {
		cand.PosterURL = "https://image.tmdb.org/t/p/w154" + r.PosterPath
	}
	return cand
}

// matchRef is a TMDB or IMDb id typed into Fix Match's search box instead of a title.
type matchRef struct {
	tmdb int
	kind string // movie or tv when a TMDB link says which
	imdb string
}

var (
	tmdbLinkRe = regexp.MustCompile(`(?i)themoviedb\.org/(?:[a-z]{2}(?:-[a-z]{2})?/)?(movie|tv)/(\d+)`)
	tmdbIDRe   = regexp.MustCompile(`(?i)^tmdb\s*[:#-]?\s*(\d+)$`)
	imdbLinkRe = regexp.MustCompile(`(?i)imdb\.com/(?:[a-z]{2}/)?title/(tt\d+)`)
	imdbIDRe   = regexp.MustCompile(`(?i)^(?:imdb\s*[:#-]?\s*)?(tt\d{6,})$`)
)

// parseMatchRef recognises a TMDB link (themoviedb.org/movie/603-the-matrix), "tmdb:603",
// an IMDb id (tt0133093) or an IMDb link. Plain numbers stay a title search: films are
// called "1917" and "2012".
func parseMatchRef(q string) (matchRef, bool) {
	q = strings.TrimSpace(q)
	if m := tmdbLinkRe.FindStringSubmatch(q); m != nil {
		id, _ := strconv.Atoi(m[2])
		return matchRef{tmdb: id, kind: strings.ToLower(m[1])}, id > 0
	}
	if m := tmdbIDRe.FindStringSubmatch(q); m != nil {
		id, _ := strconv.Atoi(m[1])
		return matchRef{tmdb: id}, id > 0
	}
	if m := imdbLinkRe.FindStringSubmatch(q); m != nil {
		return matchRef{imdb: strings.ToLower(m[1])}, true
	}
	if m := imdbIDRe.FindStringSubmatch(q); m != nil {
		return matchRef{imdb: strings.ToLower(m[1])}, true
	}
	return matchRef{}, false
}

// lookupRef fetches the entry a matchRef names, as a search result for an item of type typ
// (movie or show). nil when TMDB has no such entry.
func lookupRef(ctx context.Context, c *tmdb.Client, typ string, ref matchRef) (*tmdb.SearchResult, error) {
	want := "movie"
	if typ == "show" {
		want = "tv"
	}
	wrongKind := func(what string) error {
		if want == "movie" {
			return MatchRefError("that " + what + " is for a TV show, but this is a movie")
		}
		return MatchRefError("that " + what + " is for a movie, but this is a TV show")
	}
	if ref.imdb != "" {
		movies, tv, err := c.Find(ctx, ref.imdb, "imdb_id")
		if errors.Is(err, tmdb.ErrNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		mine, other := movies, tv
		if want == "tv" {
			mine, other = tv, movies
		}
		if len(mine) > 0 {
			return &mine[0], nil
		}
		if len(other) > 0 {
			return nil, wrongKind("IMDb id")
		}
		return nil, nil
	}
	if ref.kind != "" && ref.kind != want {
		return nil, wrongKind("TMDB link")
	}
	var r tmdb.SearchResult
	if want == "movie" {
		m, err := c.MovieBasic(ctx, ref.tmdb)
		if errors.Is(err, tmdb.ErrNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		r = tmdb.SearchResult{ID: m.ID, Title: m.Title, OriginalTitle: m.OriginalTitle, ReleaseDate: m.ReleaseDate, Overview: m.Overview, PosterPath: m.PosterPath}
	} else {
		t, err := c.TVBasic(ctx, ref.tmdb)
		if errors.Is(err, tmdb.ErrNotFound) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		r = tmdb.SearchResult{ID: t.ID, Name: t.Name, OriginalName: t.OriginalName, FirstAirDate: t.FirstAirDate, Overview: t.Overview, PosterPath: t.PosterPath}
	}
	if r.ID == 0 {
		return nil, nil
	}
	return &r, nil
}

// MatchTo matches an item to a specific TMDB entry and applies its metadata.
func (s *Service) MatchTo(ctx context.Context, itemID int64, tmdbID int) error {
	typ, _, _, lang, _, err := s.itemInfo(ctx, itemID)
	if err != nil {
		return err
	}
	c, err := s.tmdb(lang)
	if err != nil {
		return err
	}
	// A new match replaces the old identity (and its provider ids) entirely.
	if _, err := s.DB.ExecContext(ctx, `DELETE FROM external_ids WHERE item_id = ? AND provider IN ('tmdb', 'imdb', 'tvdb')`, itemID); err != nil {
		return err
	}
	if typ == "movie" {
		m, err := c.Movie(ctx, tmdbID)
		if err != nil {
			return err
		}
		if err := s.applyMovie(ctx, itemID, m); err != nil {
			return err
		}
	} else {
		t, err := c.TV(ctx, tmdbID)
		if err != nil {
			return err
		}
		if err := s.applyShow(ctx, itemID, t); err != nil {
			return err
		}
		if err := s.applySeasons(ctx, c, itemID, t); err != nil {
			return err
		}
	}
	s.DB.ExecContext(ctx, `UPDATE items SET ratings_refreshed_at = NULL WHERE id = ?`, itemID)
	return nil
}

// Refresh re-downloads metadata for an already matched item.
func (s *Service) Refresh(ctx context.Context, itemID int64) error {
	_, _, _, _, tmdbID, err := s.itemInfo(ctx, itemID)
	if err != nil {
		return err
	}
	if tmdbID == 0 {
		return ErrNotMatched
	}
	return s.MatchTo(ctx, itemID, tmdbID)
}
