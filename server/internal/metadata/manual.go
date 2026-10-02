package metadata

import (
	"context"
	"database/sql"
	"errors"
	"strconv"

	"marquee/internal/metadata/tmdb"
)

var (
	ErrNotMatchable = errors.New("only movies and shows can be matched")
	ErrNotMatched   = errors.New("item is not matched to TMDB")
)

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
		cand := Candidate{Provider: "tmdb", ID: strconv.Itoa(r.ID), Title: r.DisplayTitle(), Year: r.Year(),
			Overview: r.Overview, Current: r.ID == current}
		if o := r.Original(); o != cand.Title {
			cand.Original = o
		}
		if r.PosterPath != "" {
			cand.PosterURL = "https://image.tmdb.org/t/p/w154" + r.PosterPath
		}
		out = append(out, cand)
	}
	return out, nil
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
