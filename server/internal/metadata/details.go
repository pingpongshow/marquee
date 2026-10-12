package metadata

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"marquee/internal/metadata/tmdb"
)

// Movie and show extras for Muse and recommendations: original language, production
// countries (tags of kind 'country', ISO codes), TMDB keywords (tags of kind 'keyword') and
// TMDB's recommendations. They come with every match and refresh; FetchVideoDetails fills
// them in for items matched before they were stored.

// videoExtras is what a TMDB movie or show adds beyond the basic details.
type videoExtras struct {
	language  string
	countries []string
	keywords  []string
	recs      []int
}

func movieExtras(m *tmdb.Movie) videoExtras {
	return videoExtras{language: m.OriginalLanguage, countries: m.CountryCodes(), keywords: m.KeywordNames(), recs: m.Recommendations.IDs()}
}

func showExtras(t *tmdb.TVShow) videoExtras {
	return videoExtras{language: t.OriginalLanguage, countries: t.CountryCodes(), keywords: t.KeywordNames(), recs: t.Recommendations.IDs()}
}

// keywordsPerItem bounds how many TMDB keywords are kept (TMDB lists the most relevant first).
const keywordsPerItem = 25

// setVideoExtras stores x for itemID, replacing what was there, and records the fetch.
func setVideoExtras(ctx context.Context, tx *sql.Tx, itemID int64, x videoExtras) error {
	if x.language != "" {
		if _, err := tx.ExecContext(ctx, `UPDATE items SET original_language = ? WHERE id = ?`, x.language, itemID); err != nil {
			return err
		}
	}
	if len(x.keywords) > keywordsPerItem {
		x.keywords = x.keywords[:keywordsPerItem]
	}
	if err := replaceTags(ctx, tx, itemID, "country", x.countries); err != nil {
		return err
	}
	if err := replaceTags(ctx, tx, itemID, "keyword", x.keywords); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM tmdb_recommendations WHERE item_id = ?`, itemID); err != nil {
		return err
	}
	for i, id := range x.recs {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO tmdb_recommendations (item_id, tmdb_id, rank) VALUES (?, ?, ?)`, itemID, id, i+1); err != nil {
			return err
		}
	}
	_, err := tx.ExecContext(ctx, `INSERT INTO tmdb_details (item_id, fetched_at) VALUES (?, ?)
		ON CONFLICT (item_id) DO UPDATE SET fetched_at = excluded.fetched_at`, itemID, time.Now().UTC().Format(time.RFC3339))
	return err
}

// replaceTags sets an item's tags of one kind to names (an empty list leaves them alone:
// TMDB sometimes answers without them).
func replaceTags(ctx context.Context, tx *sql.Tx, itemID int64, kind string, names []string) error {
	if len(names) == 0 {
		return nil
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM item_tags WHERE item_id = ? AND tag_id IN (SELECT id FROM tags WHERE kind = ?)`, itemID, kind); err != nil {
		return err
	}
	for _, n := range names {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO tags(kind, name) VALUES (?, ?)`, kind, n); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO item_tags(item_id, tag_id) SELECT ?, id FROM tags WHERE kind = ? AND name = ?`, itemID, kind, n); err != nil {
			return err
		}
	}
	return nil
}

// detailsProgress is the backfill's progress for the task list.
type detailsProgress struct {
	mu          sync.Mutex
	done, total int
}

var videoDetails detailsProgress

// VideoDetailsProgress reports FetchVideoDetails' progress.
func (s *Service) VideoDetailsProgress() (int, int) {
	videoDetails.mu.Lock()
	defer videoDetails.mu.Unlock()
	return videoDetails.done, videoDetails.total
}

func setDetailsProgress(done, total int) {
	videoDetails.mu.Lock()
	videoDetails.done, videoDetails.total = done, total
	videoDetails.mu.Unlock()
}

// DetailsPace is the wait between backfill requests (TMDB allows far more; this keeps the
// task in the background).
var DetailsPace = 250 * time.Millisecond

// FetchVideoDetails fetches the extras of matched movies and shows that don't have them
// yet, one request each, paced. It's resumable: each item is recorded as it's done.
func (s *Service) FetchVideoDetails(ctx context.Context) (string, error) {
	c, err := s.tmdb(s.Settings.Get().General.MetadataLanguage) // the matcher's client
	if err != nil {
		return "TMDB isn't set up", nil
	}
	type todo struct {
		id     int64
		typ    string
		tmdbID int
	}
	var list []todo
	rows, err := s.DB.QueryContext(ctx, `SELECT i.id, i.type, x.value FROM items i
		JOIN external_ids x ON x.item_id = i.id AND x.provider = 'tmdb'
		WHERE i.type IN ('movie', 'show') AND i.extra_type IS NULL AND i.match_state = 'matched'
		  AND NOT EXISTS (SELECT 1 FROM tmdb_details d WHERE d.item_id = i.id)
		ORDER BY i.id`)
	if err != nil {
		return "", err
	}
	for rows.Next() {
		var t todo
		var v string
		if rows.Scan(&t.id, &t.typ, &v) == nil {
			if t.tmdbID, _ = strconv.Atoi(v); t.tmdbID > 0 {
				list = append(list, t)
			}
		}
	}
	rows.Close()
	if len(list) == 0 {
		return "Every matched movie and show has its details", nil
	}
	defer setDetailsProgress(0, 0)
	done, failed := 0, 0
	for i, t := range list {
		setDetailsProgress(i, len(list))
		if i > 0 && DetailsPace > 0 {
			select {
			case <-ctx.Done():
			case <-time.After(DetailsPace):
			}
		}
		if ctx.Err() != nil {
			break
		}
		var x videoExtras
		if t.typ == "movie" {
			m, err := c.MovieExtras(ctx, t.tmdbID)
			if err == nil {
				x = movieExtras(m)
			}
			err = s.saveExtras(ctx, t.id, x, err)
			if err != nil {
				if errors.Is(err, tmdb.ErrInvalidKey) {
					return "", err
				}
				failed++
				continue
			}
		} else {
			sh, err := c.TVExtras(ctx, t.tmdbID)
			if err == nil {
				x = showExtras(sh)
			}
			if err := s.saveExtras(ctx, t.id, x, err); err != nil {
				if errors.Is(err, tmdb.ErrInvalidKey) {
					return "", err
				}
				failed++
				continue
			}
		}
		done++
	}
	msg := fmt.Sprintf("Fetched details for %d of %d movies and shows", done, len(list))
	if failed > 0 {
		msg += fmt.Sprintf(" (%d failed; tried again next time)", failed)
	}
	return msg, nil
}

// saveExtras stores a fetch's result; a title TMDB no longer has is recorded as done.
func (s *Service) saveExtras(ctx context.Context, itemID int64, x videoExtras, fetchErr error) error {
	if fetchErr != nil && !errors.Is(fetchErr, tmdb.ErrNotFound) {
		slog.Debug("tmdb details", "item", itemID, "err", fetchErr)
		return fetchErr
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := setVideoExtras(ctx, tx, itemID, x); err != nil {
		return err
	}
	return tx.Commit()
}
