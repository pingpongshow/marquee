package metadata

import (
	"context"
	"database/sql"
	"errors"
	"sort"
	"strings"

	"marquee/internal/metadata/tmdb"
)

// ArtworkAlternative is an image TMDB offers for an item (META-13).
type ArtworkAlternative struct {
	URL, PreviewURL string
	Width, Height   int
	Language        string
	Votes           float64
}

// tmdbPreview is TMDB's small rendition of an image, for choosing among them.
func tmdbPreview(path string, kind string) string {
	size := "w342"
	if kind == "backdrop" || kind == "thumb" {
		size = "w500"
	}
	return "https://image.tmdb.org/t/p/" + size + path
}

// ArtworkAlternatives are TMDB's images of a kind for a matched film, show, season or episode,
// best rated first; none for anything else, or without a TMDB key.
func (s *Service) ArtworkAlternatives(ctx context.Context, itemID int64, kind string) ([]ArtworkAlternative, error) {
	var typ string
	var idx, parentIdx sql.NullInt64
	var showID, ownID int
	var libLang sql.NullString
	err := s.DB.QueryRowContext(ctx, `SELECT i.type, i.idx, p.idx, json_extract(l.options, '$.language'),
			COALESCE((SELECT CAST(value AS INTEGER) FROM external_ids WHERE item_id = i.id AND provider = 'tmdb'), 0),
			COALESCE((SELECT CAST(value AS INTEGER) FROM external_ids WHERE provider = 'tmdb'
				AND item_id = CASE i.type WHEN 'season' THEN i.parent_id WHEN 'episode' THEN i.grandparent_id END), 0)
		FROM items i JOIN libraries l ON l.id = i.library_id LEFT JOIN items p ON p.id = i.parent_id WHERE i.id = ?`, itemID).
		Scan(&typ, &idx, &parentIdx, &libLang, &ownID, &showID)
	if err != nil {
		return nil, err
	}
	var images *tmdb.Images
	c, err := s.tmdb(firstNonEmpty(libLang.String, s.Settings.Get().General.MetadataLanguage))
	if errors.Is(err, tmdb.ErrNoKey) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	switch {
	case typ == "movie" && ownID > 0:
		images, err = c.ImagesOf(ctx, "movie", ownID, 0, 0)
	case typ == "show" && ownID > 0:
		images, err = c.ImagesOf(ctx, "tv", ownID, 0, 0)
	case typ == "season" && showID > 0 && idx.Valid:
		images, err = c.ImagesOf(ctx, "season", showID, int(idx.Int64), 0)
	case typ == "episode" && showID > 0 && idx.Valid && parentIdx.Valid:
		images, err = c.ImagesOf(ctx, "episode", showID, int(parentIdx.Int64), int(idx.Int64))
	default:
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var list []tmdb.Image
	switch kind {
	case "poster":
		list = images.Posters
	case "backdrop":
		list = images.Backdrops
	case "logo":
		list = images.Logos
	case "thumb":
		list = images.Stills
		if typ != "episode" {
			list = images.Backdrops // a film's or show's wide image
		}
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].VoteAverage > list[j].VoteAverage })
	out := make([]ArtworkAlternative, 0, len(list))
	for _, im := range list {
		if im.FilePath == "" {
			continue
		}
		out = append(out, ArtworkAlternative{URL: tmdb.ImageBase + im.FilePath, PreviewURL: tmdbPreview(im.FilePath, kind),
			Width: im.Width, Height: im.Height, Language: lang(im), Votes: im.VoteAverage})
		if len(out) == 60 {
			break
		}
	}
	return out, nil
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}
