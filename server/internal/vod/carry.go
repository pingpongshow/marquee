package vod

import (
	"context"
	"database/sql"
	"log/slog"
	"strings"
)

// Sources renumber their catalogues: Dispatcharr and IPTV providers hand a film, series or
// episode a new id from one refresh to the next. Left alone, the row under the old id is
// removed (and with it everyone's progress, watched marks and the rest kept by the row's id)
// and the title comes back as new, to be matched to TMDB again. Instead, a title listed under
// a new id takes over the row it had: a film or series found by its TMDB id (else by its
// title and year), an episode by its season and number. The row keeps its id, so its
// progress, watched marks, hidden-from-Continue-Watching and Not-interested marks, TMDB match
// and quality stay; follows and saves, kept by the source's ids, move to the new ones.

// listed is a title in a refresh, as the source lists it now.
type listed struct {
	key, title, category, file string // file: the stream ("{id}.{ext}" or a link)
	year                       int
	tmdb                       int64
}

// stored is a row whose key the refresh no longer lists.
type stored struct {
	id                   int64
	key, title, category string
	year                 int
	tmdb                 int64
}

// pair matches titles listed under keys no row has to rows whose keys are gone, one row each:
// by TMDB id first (across the whole list, so a title match can't take a row another title
// matches exactly), then by title, with years at most one apart when both are known and
// never across two different TMDB ids. A row from the same category is preferred. It returns
// the new key → the row it takes over.
func pair(gone []stored, fresh []listed) map[string]stored {
	out := map[string]stored{}
	if len(gone) == 0 || len(fresh) == 0 {
		return out
	}
	byTMDB := map[int64][]int{}
	byTitle := map[string][]int{}
	for i, g := range gone {
		if g.tmdb > 0 {
			byTMDB[g.tmdb] = append(byTMDB[g.tmdb], i)
		}
		byTitle[titleKey(g.title)] = append(byTitle[titleKey(g.title)], i)
	}
	taken := make([]bool, len(gone))
	take := func(cands []int, f listed, fits func(stored) bool) bool {
		best := -1
		for _, i := range cands {
			if taken[i] || !fits(gone[i]) {
				continue
			}
			if best < 0 || (gone[i].category == f.category && gone[best].category != f.category) {
				best = i
			}
		}
		if best < 0 {
			return false
		}
		taken[best] = true
		out[f.key] = gone[best]
		return true
	}
	var rest []listed
	for _, f := range fresh {
		if f.tmdb > 0 && take(byTMDB[f.tmdb], f, func(stored) bool { return true }) {
			continue
		}
		rest = append(rest, f)
	}
	for _, f := range rest {
		take(byTitle[titleKey(f.title)], f, func(g stored) bool {
			if f.tmdb > 0 && g.tmdb > 0 && f.tmdb != g.tmdb {
				return false
			}
			return f.year == 0 || g.year == 0 || (f.year-g.year <= 1 && g.year-f.year <= 1)
		})
	}
	return out
}

// titleKey compares titles regardless of case and spacing.
func titleKey(t string) string { return strings.Join(strings.Fields(strings.ToLower(t)), " ") }

// fresh is the listed titles under keys no row has yet; listedKeys every key listed.
func fresh(existing map[string]int64, list []listed) (out []listed, listedKeys map[string]bool) {
	listedKeys = make(map[string]bool, len(list))
	for _, l := range list {
		listedKeys[l.key] = true
		if _, ok := existing[l.key]; !ok {
			out = append(out, l)
		}
	}
	return out, listedKeys
}

// goneRows reads rows (id, source_key, title, category, year, tmdb_id) and keeps those whose
// key isn't listed.
func goneRows(ctx context.Context, tx *sql.Tx, listedKeys map[string]bool, q string, args ...any) ([]stored, error) {
	rows, err := tx.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []stored
	for rows.Next() {
		var g stored
		if err := rows.Scan(&g.id, &g.key, &g.title, &g.category, &g.year, &g.tmdb); err != nil {
			return nil, err
		}
		if !listedKeys[g.key] {
			out = append(out, g)
		}
	}
	return out, rows.Err()
}

// saveKey is how a save names a film: its source key without "xtream:".
func saveKey(k string) string { return strings.TrimPrefix(k, "xtream:") }

// carryFilms moves films the source now lists under new keys onto the rows they had, with
// their saves, and updates existing to match (so the refresh updates those rows and doesn't
// remove them). It returns how many moved.
func carryFilms(ctx context.Context, tx *sql.Tx, sourceID string, existing map[string]int64, list []listed) (int, error) {
	newOnes, listedKeys := fresh(existing, list)
	if len(newOnes) == 0 || len(existing) == 0 {
		return 0, nil
	}
	gone, err := goneRows(ctx, tx, listedKeys, `SELECT id, source_key, title, category, COALESCE(year, 0), COALESCE(tmdb_id, 0)
		FROM vod_movies WHERE source_id=?`, sourceID)
	if err != nil {
		return 0, err
	}
	files := map[string]string{}
	for _, l := range newOnes {
		files[l.key] = l.file
	}
	moves := pair(gone, newOnes)
	for key, g := range moves {
		if _, err := tx.ExecContext(ctx, `UPDATE vod_movies SET source_key=? WHERE id=?`, key, g.id); err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE vod_saves SET source_key=?, file=? WHERE source_id=? AND kind='movie' AND source_key=?`,
			saveKey(key), files[key], sourceID, saveKey(g.key)); err != nil {
			return 0, err
		}
		delete(existing, g.key)
		existing[key] = g.id
	}
	if len(moves) > 0 {
		slog.InfoContext(ctx, "vod: films kept under their new ids", "source", sourceID, "films", len(moves))
	}
	return len(moves), nil
}

// carrySeries moves series the source now lists under new keys onto the rows they had, with
// their follows, and updates existing to match. Their episodes are fetched again when next
// needed (their ids have likely changed too; carryEpisodes keeps them). It returns how many
// moved.
func carrySeries(ctx context.Context, tx *sql.Tx, sourceID string, existing map[string]int64, list []listed) (int, error) {
	newOnes, listedKeys := fresh(existing, list)
	if len(newOnes) == 0 || len(existing) == 0 {
		return 0, nil
	}
	gone, err := goneRows(ctx, tx, listedKeys, `SELECT id, source_key, title, category, COALESCE(year, 0), COALESCE(tmdb_id, 0)
		FROM vod_series WHERE source_id=?`, sourceID)
	if err != nil {
		return 0, err
	}
	moves := pair(gone, newOnes)
	for key, g := range moves {
		if _, err := tx.ExecContext(ctx, `UPDATE vod_series SET source_key=?, episodes_at=NULL WHERE id=?`, key, g.id); err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE vod_follows SET series_key=?, series_id=? WHERE source_id=? AND series_key=?`,
			key, g.id, sourceID, g.key); err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE vod_follow_episodes SET series_key=? WHERE source_id=? AND series_key=?`,
			key, sourceID, g.key); err != nil {
			return 0, err
		}
		delete(existing, g.key)
		existing[key] = g.id
	}
	if len(moves) > 0 {
		slog.InfoContext(ctx, "vod: series kept under their new ids", "source", sourceID, "series", len(moves))
	}
	return len(moves), nil
}

// listedEpisode is an episode as the source lists it now.
type listedEpisode struct {
	key, file       string
	season, episode int
}

// carryEpisodes moves a series' episodes the source now lists under new keys onto the rows
// with the same season and number whose keys are gone, with their saves and follow records,
// and updates existing to match. It returns how many moved.
func carryEpisodes(ctx context.Context, tx *sql.Tx, seriesID int64, existing map[string]int64, list []listedEpisode) (int, error) {
	if len(existing) == 0 {
		return 0, nil
	}
	listedKeys := make(map[string]bool, len(list))
	for _, e := range list {
		listedKeys[e.key] = true
	}
	var sourceID string
	if err := tx.QueryRowContext(ctx, `SELECT source_id FROM vod_series WHERE id=?`, seriesID).Scan(&sourceID); err != nil {
		return 0, err
	}
	type slot struct{ season, episode int }
	gone := map[slot][]stored{}
	rows, err := tx.QueryContext(ctx, `SELECT id, source_key, season, episode FROM vod_episodes WHERE series_id=? ORDER BY id`, seriesID)
	if err != nil {
		return 0, err
	}
	for rows.Next() {
		var g stored
		var sl slot
		if err := rows.Scan(&g.id, &g.key, &sl.season, &sl.episode); err != nil {
			rows.Close()
			return 0, err
		}
		if !listedKeys[g.key] && sl.episode > 0 {
			gone[sl] = append(gone[sl], g)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	n := 0
	for _, e := range list {
		sl := slot{e.season, e.episode}
		if _, ok := existing[e.key]; ok || len(gone[sl]) == 0 {
			continue
		}
		g := gone[sl][0]
		gone[sl] = gone[sl][1:]
		if _, err := tx.ExecContext(ctx, `UPDATE vod_episodes SET source_key=? WHERE id=?`, e.key, g.id); err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE vod_saves SET source_key=?, file=? WHERE source_id=? AND kind='episode' AND source_key=?`,
			e.key, e.file, sourceID, g.key); err != nil {
			return 0, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE vod_follow_episodes SET episode_key=? WHERE source_id=? AND episode_key=?`,
			e.key, sourceID, g.key); err != nil {
			return 0, err
		}
		delete(existing, g.key)
		existing[e.key] = g.id
		n++
	}
	if n > 0 {
		slog.InfoContext(ctx, "vod: episodes kept under their new ids", "series", seriesID, "episodes", n)
	}
	return n, nil
}
