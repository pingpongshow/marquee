package metadata

import (
	"context"
	"log/slog"
	"strings"

	"marquee/internal/metadata/deezer"
	"marquee/internal/scanner/naming"
)

// Deezer's placeholder image for artists without a photo.
func isPlaceholder(u string) bool {
	return u == "" || strings.Contains(u, "/artist//") || strings.Contains(u, "/images/artist/d41d8cd98f00b204e9800998ecf8427e")
}

// MusicArtwork fetches artist photos, and covers for albums without one, from Deezer.
// Only exact (normalized) name matches are used. Each item is tried once; it is retried
// after 30 days or when an admin refreshes it.
func (s *Service) MusicArtwork(ctx context.Context, libID int64, report func(Progress)) error {
	if s.deezer == nil {
		s.deezer = deezer.New()
	}
	type todo struct {
		id         int64
		typ, title string
		artist     string
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT i.id, i.type, i.title, COALESCE(a.title, '') FROM items i LEFT JOIN items a ON a.id = i.parent_id
		WHERE i.library_id = ? AND i.type IN ('artist', 'album')
		  AND NOT EXISTS (SELECT 1 FROM artwork w WHERE w.item_id = i.id AND w.kind = 'poster')
		  AND (i.metadata_refreshed_at IS NULL OR i.metadata_refreshed_at < strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '-30 days'))
		  AND i.title NOT IN ('Unknown Artist', 'Unknown Album', 'Various Artists')`, libID)
	if err != nil {
		return err
	}
	var list []todo
	for rows.Next() {
		var t todo
		rows.Scan(&t.id, &t.typ, &t.title, &t.artist)
		list = append(list, t)
	}
	rows.Close()

	found := 0
	for i, t := range list {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if report != nil && i%10 == 0 {
			report(Progress{Done: i, Total: len(list)})
		}
		img := ""
		if t.typ == "artist" {
			res, err := s.deezer.SearchArtists(ctx, t.title)
			if err != nil {
				slog.Debug("deezer artist", "artist", t.title, "err", err)
				continue
			}
			want := naming.Normalize(t.title)
			best := 0
			for _, a := range res {
				if naming.Normalize(a.Name) == want && !isPlaceholder(a.PictureXL) && a.NbFan >= best {
					img, best = a.PictureXL, a.NbFan
				}
			}
		} else {
			res, err := s.deezer.SearchAlbums(ctx, t.artist, t.title)
			if err != nil {
				continue
			}
			for _, a := range res {
				if naming.Normalize(a.Title) == naming.Normalize(t.title) && naming.Normalize(a.Artist.Name) == naming.Normalize(t.artist) && a.CoverXL != "" {
					img = a.CoverXL
					break
				}
			}
		}
		if img != "" {
			s.DB.ExecContext(ctx, `INSERT INTO artwork(item_id, kind, source, remote_url, selected)
				SELECT ?, 'poster', 'deezer', ?, NOT EXISTS (SELECT 1 FROM artwork WHERE item_id = ? AND kind = 'poster' AND selected = 1)`,
				t.id, img, t.id)
			found++
		}
		s.DB.ExecContext(ctx, `UPDATE items SET metadata_refreshed_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = ?`, t.id)
	}
	if len(list) > 0 {
		slog.Info("music artwork updated", "library", libID, "checked", len(list), "found", found)
	}
	return nil
}
