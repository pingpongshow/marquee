package metadata

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"marquee/internal/metadata/anilist"
	"marquee/internal/scanner/naming"
)

// AniList enrichment (META-2, D84) for shows and films in anime libraries: matched through the
// anime-lists TMDB → AniList mapping, else an exact title and year search. Adds the romaji,
// English and Japanese titles and synonyms (searchable), the Japanese title as the original
// title, the main studio, AniList's genres and top tags, its score and AniList/MAL ids, and
// AniList's description when TMDB had none. TMDB stays the source of episodes and artwork.
// Items are looked up again after 30 days, for scores of shows still airing.

type aniTodo struct {
	id, tmdb                  int64
	typ, title, locked        string
	year                      int
	original, studio, summary string
}

// EnrichAnime looks up anime not checked in the last 30 days, until done or ctx ends.
func (s *Service) EnrichAnime(ctx context.Context) (string, error) {
	if !s.aniRun.TryLock() {
		return "Already running", nil
	}
	defer s.aniRun.Unlock()
	s.mu.Lock()
	if s.ani == nil {
		s.ani = anilist.New(MBUserAgent)
	}
	c := s.ani
	s.mu.Unlock()
	rows, err := s.DB.QueryContext(ctx, `SELECT i.id, i.type, i.title, COALESCE(i.year, 0), i.locked_fields, COALESCE(i.original_title, ''),
			COALESCE(i.studio, ''), COALESCE(i.summary, ''),
			COALESCE((SELECT CAST(value AS INTEGER) FROM external_ids WHERE item_id = i.id AND provider = 'tmdb'), 0)
		FROM items i JOIN libraries l ON l.id = i.library_id AND l.type = 'anime'
		LEFT JOIN anilist_lookups a ON a.item_id = i.id
		WHERE i.type IN ('show', 'movie') AND i.extra_type IS NULL AND (a.item_id IS NULL OR a.checked_at < ?)
		ORDER BY i.id`, time.Now().UTC().Add(-30*24*time.Hour).Format(time.RFC3339))
	if err != nil {
		return "", err
	}
	var list []aniTodo
	for rows.Next() {
		var t aniTodo
		if err := rows.Scan(&t.id, &t.typ, &t.title, &t.year, &t.locked, &t.original, &t.studio, &t.summary, &t.tmdb); err != nil {
			rows.Close()
			return "", err
		}
		list = append(list, t)
	}
	rows.Close()
	if len(list) == 0 {
		return "Nothing to look up", nil
	}
	mapping, err := c.LoadMapping(ctx, filepath.Join(s.CacheDir, "anime-lists.json"))
	if err != nil {
		slog.Warn("anime-lists mapping", "err", err) // fall back to searching by title
	}
	found, checked := 0, 0
	for _, t := range list {
		if ctx.Err() != nil {
			break
		}
		m, ok, err := s.findAnime(ctx, c, mapping, t)
		if err != nil && !errors.Is(err, anilist.ErrNotFound) {
			slog.Warn("anilist", "item", t.title, "err", err)
			return fmt.Sprintf("Looked up %d of %d (%d found); stopped: %v", checked, len(list), found, err), nil
		}
		if ok {
			if err := s.applyAnime(ctx, t, m); err != nil {
				return "", err
			}
			found++
		}
		checked++
		s.DB.ExecContext(ctx, `INSERT INTO anilist_lookups (item_id, checked_at, found) VALUES (?, ?, ?)
			ON CONFLICT (item_id) DO UPDATE SET checked_at = excluded.checked_at, found = excluded.found`,
			t.id, time.Now().UTC().Format(time.RFC3339), ok)
	}
	return fmt.Sprintf("Looked up %d anime (%d found on AniList)", checked, found), nil
}

func (s *Service) findAnime(ctx context.Context, c *anilist.Client, mapping anilist.Mapping, t aniTodo) (anilist.Media, bool, error) {
	if t.tmdb > 0 {
		id := mapping.TV[t.tmdb]
		if t.typ == "movie" {
			id = mapping.Movies[t.tmdb]
		}
		if id > 0 {
			m, err := c.Media(ctx, id)
			return m, err == nil, err
		}
	}
	// No mapping: an exact title match of the right kind, from the right year.
	res, err := c.Search(ctx, t.title)
	if err != nil {
		return anilist.Media{}, false, err
	}
	want := naming.Normalize(t.title)
	for _, m := range res {
		if (t.typ == "movie") != (m.Format == "MOVIE") {
			continue
		}
		if t.year > 0 && m.StartDate.Year > 0 && (m.StartDate.Year < t.year-1 || m.StartDate.Year > t.year+1) {
			continue
		}
		for _, name := range append([]string{m.Title.Romaji, m.Title.English, m.Title.Native}, m.Synonyms...) {
			if name != "" && naming.Normalize(name) == want {
				return m, true, nil
			}
		}
	}
	return anilist.Media{}, false, nil
}

func (s *Service) applyAnime(ctx context.Context, t aniTodo, m anilist.Media) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	tx.ExecContext(ctx, `INSERT OR REPLACE INTO external_ids(item_id, provider, value) VALUES (?, 'anilist', ?)`, t.id, strconv.FormatInt(m.ID, 10))
	if m.IDMal > 0 {
		tx.ExecContext(ctx, `INSERT OR REPLACE INTO external_ids(item_id, provider, value) VALUES (?, 'mal', ?)`, t.id, strconv.FormatInt(m.IDMal, 10))
	}
	// Other names it's known by, so a search for "Shingeki no Kyojin" finds "Attack on Titan".
	seen := map[string]bool{naming.Normalize(t.title): true}
	var alts []string
	for _, n := range append([]string{m.Title.Romaji, m.Title.English, m.Title.Native}, m.Synonyms...) {
		k := naming.Normalize(n)
		if n == "" || k == "" || seen[k] {
			continue
		}
		seen[k] = true
		alts = append(alts, n)
	}
	sets := []string{"alt_titles = ?"}
	args := []any{strings.Join(alts, "\n")}
	if m.AverageScore > 0 {
		sets, args = append(sets, "anilist_score = ?"), append(args, m.AverageScore)
	}
	if t.original == "" && m.Title.Native != "" && !lockedIn(t.locked, "originalTitle") {
		sets, args = append(sets, "original_title = ?"), append(args, m.Title.Native)
	}
	if t.studio == "" && len(m.Studios.Nodes) > 0 && !lockedIn(t.locked, "studio") {
		sets, args = append(sets, "studio = ?"), append(args, m.Studios.Nodes[0].Name)
	}
	if strings.TrimSpace(t.summary) == "" && !lockedIn(t.locked, "summary") {
		if d := m.PlainDescription(); d != "" {
			sets, args = append(sets, "summary = ?"), append(args, d)
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE items SET `+strings.Join(sets, ", ")+` WHERE id = ?`, append(args, t.id)...); err != nil {
		return err
	}
	// AniList's genres, and its two strongest non-spoiler tags (e.g. Isekai, Mecha).
	names := append([]string{}, m.Genres...)
	tags := m.Tags[:0:0]
	for _, tg := range m.Tags {
		if !tg.IsAdult && !tg.IsSpoil && tg.Rank >= 80 && tg.Category != "Sexual Content" {
			tags = append(tags, tg)
		}
	}
	sort.SliceStable(tags, func(i, j int) bool { return tags[i].Rank > tags[j].Rank })
	for i, tg := range tags {
		if i == 2 {
			break
		}
		names = append(names, tg.Name)
	}
	if !lockedIn(t.locked, "genres") {
		for _, n := range names {
			name := genreName(ctx, tx, n)
			tx.ExecContext(ctx, `INSERT OR IGNORE INTO tags(kind, name) VALUES ('genre', ?)`, name)
			tx.ExecContext(ctx, `INSERT OR IGNORE INTO item_tags(item_id, tag_id) SELECT ?, id FROM tags WHERE kind = 'genre' AND name = ?`, t.id, name)
		}
	}
	return tx.Commit()
}
