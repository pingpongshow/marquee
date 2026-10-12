package metadata

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"marquee/internal/metadata/musicbrainz"
	"marquee/internal/scanner/naming"
)

// Artist and album details (META-3, D77, MUSIC-15): for artists, sort names without a
// leading article in any language ("Los Lobos" sorts as "Lobos"), genres, styles (MusicBrainz
// tags), type (person or group), gender, country and area, active years and a Wikipedia bio;
// for albums, the release type (album, EP, single, live…), the original release date, styles
// and the label. MBIDs from tags are used when present; otherwise only an unambiguous
// exact-name match is accepted. Each item is looked up once (again after 180 days when nothing
// was found, and once more when mbVersion rises). Genres only ever add to those from the files.

// MBUserAgent identifies Marquee to MusicBrainz; set by the caller.
var MBUserAgent = "Marquee (self-hosted media server)"

const (
	mbGenresPerItem = 3
	mbStylesPerItem = 8
	// mbVersion is the enrichment's version: items looked up by an older one are revisited.
	mbVersion = 2
)

type mbTodo struct {
	id                  int64
	typ, title, artist  string
	mbid                string
	locked              string
	sortTitle, released string
	year                int
}

// mbInfo is what's stored in music_info for a found item.
type mbInfo struct {
	artistType, gender, country, area, beginArea string
	beginYear, endYear                           int
	primaryType, label                           string
}

type mbProgress struct {
	mu          sync.Mutex
	done, total int
}

var musicDetails mbProgress

// MusicDetailsProgress reports EnrichMusic's progress for the task list.
func (s *Service) MusicDetailsProgress() (int, int) {
	musicDetails.mu.Lock()
	defer musicDetails.mu.Unlock()
	return musicDetails.done, musicDetails.total
}

func setMBProgress(done, total int) {
	musicDetails.mu.Lock()
	musicDetails.done, musicDetails.total = done, total
	musicDetails.mu.Unlock()
}

// EnrichMusic looks up artists and albums not yet checked, until done or ctx ends.
func (s *Service) EnrichMusic(ctx context.Context) (string, error) {
	if s.Settings != nil && !s.Settings.Get().Music.ArtistDetails {
		return "Artist and album details are turned off", nil
	}
	s.mu.Lock()
	if s.mb == nil {
		s.mb = musicbrainz.New(MBUserAgent)
	}
	mb := s.mb
	s.mu.Unlock()
	retry := time.Now().UTC().Add(-180 * 24 * time.Hour).Format(time.RFC3339)
	rows, err := s.DB.QueryContext(ctx, `SELECT i.id, i.type, i.title, COALESCE(a.title, ''), COALESCE(x.value, ''), i.locked_fields, i.sort_title,
			COALESCE(i.originally_available_at, ''), COALESCE(i.year, 0)
		FROM items i
		JOIN libraries l ON l.id = i.library_id AND l.type = 'music'
		LEFT JOIN items a ON a.id = i.parent_id
		LEFT JOIN external_ids x ON x.item_id = i.id AND x.provider = 'musicbrainz'
		LEFT JOIN musicbrainz_lookups m ON m.item_id = i.id
		LEFT JOIN music_info mi ON mi.item_id = i.id
		WHERE i.type IN ('artist', 'album')
		  AND i.title NOT IN ('Unknown Artist', 'Unknown Album', 'Various Artists', '')
		  AND (mi.item_id IS NULL OR (mi.found = 1 AND mi.version < ?) OR (mi.found = 0 AND mi.checked_at < ?))
		  AND NOT (mi.item_id IS NULL AND COALESCE(m.found, 1) = 0 AND m.checked_at >= ?)
		ORDER BY i.type DESC, i.id`, mbVersion, retry, retry)
	if err != nil {
		return "", err
	}
	var list []mbTodo
	for rows.Next() {
		var t mbTodo
		if err := rows.Scan(&t.id, &t.typ, &t.title, &t.artist, &t.mbid, &t.locked, &t.sortTitle, &t.released, &t.year); err != nil {
			rows.Close()
			return "", err
		}
		list = append(list, t)
	}
	rows.Close()
	if len(list) == 0 {
		return "Every artist and album has been looked up", nil
	}
	defer setMBProgress(0, 0)
	found, checked := 0, 0
	for i, t := range list {
		setMBProgress(i, len(list))
		if ctx.Err() != nil {
			break
		}
		if s.Settings != nil && !s.Settings.Get().Music.ArtistDetails {
			break
		}
		var ok bool
		var err error
		if t.typ == "artist" {
			ok, err = s.enrichArtist(ctx, mb, t)
		} else {
			ok, err = s.enrichAlbum(ctx, mb, t)
		}
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			// Network trouble: stop for now and try the rest next time.
			if !errors.Is(err, musicbrainz.ErrNotFound) {
				slog.Warn("musicbrainz", "item", t.title, "err", err)
				return fmt.Sprintf("Looked up %d of %d (%d found); stopped: %v", checked, len(list), found, err), nil
			}
		}
		checked++
		now := time.Now().UTC().Format(time.RFC3339)
		if ok {
			found++
		} else {
			s.DB.ExecContext(ctx, `INSERT INTO music_info (item_id, kind, found, version, checked_at) VALUES (?, ?, 0, ?, ?)
				ON CONFLICT (item_id) DO UPDATE SET found = 0, version = excluded.version, checked_at = excluded.checked_at`, t.id, t.typ, mbVersion, now)
		}
		s.DB.ExecContext(ctx, `INSERT INTO musicbrainz_lookups (item_id, checked_at, found) VALUES (?, ?, ?)
			ON CONFLICT (item_id) DO UPDATE SET checked_at = excluded.checked_at, found = excluded.found`, t.id, now, ok)
	}
	if found > 0 && s.AfterMusicDetails != nil {
		s.AfterMusicDetails()
	}
	if checked < len(list) {
		return fmt.Sprintf("Looked up %d of %d artists and albums (%d found); continuing next time", checked, len(list), found), nil
	}
	return fmt.Sprintf("Looked up %d artists and albums (%d found)", checked, found), nil
}

// saveInfo writes a found item's music_info row and styles.
func saveInfo(ctx context.Context, tx *sql.Tx, t mbTodo, info mbInfo, styles []string) error {
	nz := func(n int) any {
		if n == 0 {
			return nil
		}
		return n
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO music_info (item_id, kind, found, version, checked_at, artist_type, gender, country, area, begin_area,
			begin_year, end_year, primary_type, label) VALUES (?, ?, 1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (item_id) DO UPDATE SET found = 1, version = excluded.version, checked_at = excluded.checked_at, artist_type = excluded.artist_type,
			gender = excluded.gender, country = excluded.country, area = excluded.area, begin_area = excluded.begin_area, begin_year = excluded.begin_year,
			end_year = excluded.end_year, primary_type = excluded.primary_type, label = excluded.label`,
		t.id, t.typ, mbVersion, time.Now().UTC().Format(time.RFC3339), nullEmpty(info.artistType), nullEmpty(info.gender), nullEmpty(info.country),
		nullEmpty(info.area), nullEmpty(info.beginArea), nz(info.beginYear), nz(info.endYear), nullEmpty(info.primaryType), nullEmpty(info.label)); err != nil {
		return err
	}
	if lockedIn(t.locked, "genres", "styles") {
		return nil
	}
	return replaceTags(ctx, tx, t.id, "style", styles)
}

func (s *Service) enrichArtist(ctx context.Context, mb *musicbrainz.Client, t mbTodo) (bool, error) {
	var a musicbrainz.Artist
	if t.mbid != "" {
		var err error
		if a, err = mb.Artist(ctx, t.mbid); err != nil {
			return false, err
		}
	} else {
		res, err := mb.SearchArtists(ctx, t.title)
		if err != nil {
			return false, err
		}
		want := naming.Normalize(t.title)
		var match []musicbrainz.Artist
		for _, r := range res {
			if r.Score >= 90 && naming.Normalize(r.Name) == want {
				match = append(match, r)
			}
		}
		if len(match) != 1 {
			return false, nil // none, or several artists with this name
		}
		// Search results don't carry genres.
		if a, err = mb.Artist(ctx, match[0].ID); err != nil {
			return false, err
		}
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	tx.ExecContext(ctx, `INSERT OR IGNORE INTO external_ids(item_id, provider, value) VALUES (?, 'musicbrainz', ?)`, t.id, a.ID)
	if sortName := articleSort(t.title, a.SortName); sortName != "" && sortName != t.sortTitle && !lockedIn(t.locked, "sortTitle", "sort_title", "title") {
		tx.ExecContext(ctx, `UPDATE items SET sort_title = ? WHERE id = ?`, sortName, t.id)
	}
	if err := addGenres(ctx, tx, t.id, a.Genres, t.locked); err != nil {
		return false, err
	}
	info := mbInfo{artistType: strings.ToLower(a.Type), gender: strings.ToLower(a.Gender), country: a.CountryCode()}
	if a.Area != nil {
		info.area = a.Area.Name
	}
	if a.Begin != nil {
		info.beginArea = a.Begin.Name
	}
	info.beginYear, info.endYear = a.Years()
	if err := saveInfo(ctx, tx, t, info, musicbrainz.Styles(a.Genres, a.Tags, mbStylesPerItem)); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	s.artistExtras(ctx, t, a)
	return true, nil
}

// artistExtras adds a Wikipedia bio when the artist has none, and the artist's most-listened
// tracks that are in the library (ListenBrainz). Failures are only logged: these are extras.
func (s *Service) artistExtras(ctx context.Context, t mbTodo, a musicbrainz.Artist) {
	s.mu.Lock()
	if s.web == nil {
		s.web = musicbrainz.NewWeb(MBUserAgent)
	}
	web := s.web
	s.mu.Unlock()
	if q := a.Wikidata(); q != "" && !lockedIn(t.locked, "summary") {
		// Only when the artist has no summary (tags, Plex import or an earlier lookup may have one).
		var cur sql.NullString
		s.DB.QueryRowContext(ctx, `SELECT summary FROM items WHERE id = ?`, t.id).Scan(&cur)
		if strings.TrimSpace(cur.String) == "" {
			lang := "en"
			if s.Settings != nil {
				lang, _, _ = strings.Cut(s.Settings.Get().General.MetadataLanguage, "-")
			}
			if bio, err := web.Bio(ctx, q, lang); err == nil {
				s.DB.ExecContext(ctx, `UPDATE items SET summary = ? WHERE id = ?`, bio, t.id)
			} else if !errors.Is(err, musicbrainz.ErrNotFound) {
				slog.Debug("wikipedia bio", "artist", t.title, "err", err)
			}
		}
	}
	top, err := web.TopRecordings(ctx, a.ID)
	if err != nil {
		if !errors.Is(err, musicbrainz.ErrNotFound) {
			slog.Debug("listenbrainz", "artist", t.title, "err", err)
		}
		return
	}
	s.setPopular(ctx, t.id, top)
}

// popularCount is how many popular tracks are kept per artist.
const popularCount = 10

// setPopular matches an artist's top recordings to their tracks in the library, by recording
// MBID and then by title, and keeps the first popularCount.
func (s *Service) setPopular(ctx context.Context, artistID int64, top []musicbrainz.Recording) {
	rows, err := s.DB.QueryContext(ctx, `SELECT t.id, t.title, COALESCE(x.value, '') FROM items t
		LEFT JOIN external_ids x ON x.item_id = t.id AND x.provider = 'musicbrainz'
		WHERE t.type = 'track' AND t.grandparent_id = ? ORDER BY t.id`, artistID)
	if err != nil {
		return
	}
	byMBID, byTitle := map[string]int64{}, map[string]int64{}
	for rows.Next() {
		var id int64
		var title, mbid string
		if rows.Scan(&id, &title, &mbid) == nil {
			if mbid != "" {
				byMBID[mbid] = id
			}
			if k := naming.Normalize(title); byTitle[k] == 0 {
				byTitle[k] = id
			}
		}
	}
	rows.Close()
	var picked []int64
	seen := map[int64]bool{}
	for _, r := range top {
		id := byMBID[r.MBID]
		if id == 0 {
			id = byTitle[naming.Normalize(r.Name)]
		}
		if id != 0 && !seen[id] {
			seen[id] = true
			picked = append(picked, id)
			if len(picked) == popularCount {
				break
			}
		}
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return
	}
	defer tx.Rollback()
	tx.ExecContext(ctx, `DELETE FROM music_popular WHERE artist_id = ?`, artistID)
	for i, id := range picked {
		tx.ExecContext(ctx, `INSERT INTO music_popular (artist_id, track_id, rank) VALUES (?, ?, ?)`, artistID, id, i+1)
	}
	tx.Commit()
}

func (s *Service) enrichAlbum(ctx context.Context, mb *musicbrainz.Client, t mbTodo) (bool, error) {
	var g musicbrainz.ReleaseGroup
	var label string
	if t.mbid != "" {
		id, l, err := mb.ReleaseGroupOf(ctx, t.mbid)
		if err != nil {
			return false, err
		}
		label = l
		if g, err = mb.ReleaseGroup(ctx, id); err != nil {
			return false, err
		}
	} else {
		if t.artist == "" || t.artist == "Various Artists" {
			return false, nil
		}
		res, err := mb.SearchReleaseGroups(ctx, t.artist, t.title)
		if err != nil {
			return false, err
		}
		want, artist := naming.Normalize(t.title), naming.Normalize(t.artist)
		var match []musicbrainz.ReleaseGroup
		for _, r := range res {
			if r.Score < 90 || naming.Normalize(r.Title) != want {
				continue
			}
			for _, c := range r.ArtistCredit {
				if naming.Normalize(c.Name) == artist || naming.Normalize(c.Artist.Name) == artist {
					match = append(match, r)
					break
				}
			}
		}
		if len(match) != 1 {
			return false, nil
		}
		if g, err = mb.ReleaseGroup(ctx, match[0].ID); err != nil {
			return false, err
		}
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	tx.ExecContext(ctx, `INSERT OR IGNORE INTO external_ids(item_id, provider, value) VALUES (?, 'musicbrainz_releasegroup', ?)`, t.id, g.ID)
	if rt := g.ReleaseType(); rt != "" {
		tx.ExecContext(ctx, `UPDATE items SET release_type = ? WHERE id = ?`, rt, t.id)
	}
	// The original release date, when the files only gave a year (or nothing).
	if d := g.FirstReleaseDate; len(d) == 10 && len(t.released) < 10 && !lockedIn(t.locked, "originallyAvailableAt", "originally_available_at") {
		tx.ExecContext(ctx, `UPDATE items SET originally_available_at = ? WHERE id = ?`, d, t.id)
	}
	if y := year(g.FirstReleaseDate); y > 0 && t.year == 0 && !lockedIn(t.locked, "year") {
		tx.ExecContext(ctx, `UPDATE items SET year = ? WHERE id = ?`, y, t.id)
	}
	if err := addGenres(ctx, tx, t.id, g.Genres, t.locked); err != nil {
		return false, err
	}
	info := mbInfo{primaryType: strings.ToLower(g.PrimaryType), label: label}
	if err := saveInfo(ctx, tx, t, info, musicbrainz.Styles(g.Genres, g.Tags, mbStylesPerItem)); err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func lockedIn(lockedJSON string, fields ...string) bool {
	var locked []string
	json.Unmarshal([]byte(lockedJSON), &locked)
	for _, l := range locked {
		for _, f := range fields {
			if l == f {
				return true
			}
		}
	}
	return false
}

// articleSort uses MusicBrainz's sort name to drop a leading article the scanner doesn't know
// ("Los Lobos" sorts as "Lobos", as "The Beatles" already sorts as "Beatles"). Sort names that
// reorder people's names ("Cash, Johnny") aren't used: people expect artists sorted by the
// name they see. Returns "" when there's nothing to change.
func articleSort(title, sortName string) string {
	i := strings.LastIndex(sortName, ", ")
	if i < 0 {
		return ""
	}
	rest, article := sortName[:i], sortName[i+2:]
	if !strings.EqualFold(title, article+" "+rest) {
		return ""
	}
	switch strings.ToLower(article) {
	case "the", "a", "an", "los", "las", "la", "le", "les", "el", "il", "die", "der", "das", "de", "het":
		return rest
	}
	return ""
}

// addGenres adds an item's top MusicBrainz genres, reusing existing genre names that differ
// only in case or punctuation.
func addGenres(ctx context.Context, tx *sql.Tx, itemID int64, genres []musicbrainz.Genre, locked string) error {
	if len(genres) == 0 || lockedIn(locked, "genres") {
		return nil
	}
	sort.SliceStable(genres, func(i, j int) bool { return genres[i].Count > genres[j].Count })
	n := 0
	for _, g := range genres {
		if n == mbGenresPerItem || g.Count <= 0 {
			break
		}
		n++
		name := genreName(ctx, tx, g.Name)
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO tags(kind, name) VALUES ('genre', ?)`, name); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO item_tags(item_id, tag_id) SELECT ?, id FROM tags WHERE kind = 'genre' AND name = ?`, itemID, name); err != nil {
			return err
		}
	}
	return nil
}

// genreName is an existing genre spelled the same way ("Hip-Hop" for "hip hop"), else the
// MusicBrainz name in title case.
func genreName(ctx context.Context, tx *sql.Tx, mbName string) string {
	key := naming.Normalize(mbName)
	rows, err := tx.QueryContext(ctx, `SELECT name FROM tags WHERE kind = 'genre'`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var n string
			if rows.Scan(&n) == nil && naming.Normalize(n) == key {
				return n
			}
		}
	}
	return titleCase(mbName)
}

func titleCase(s string) string {
	r := []rune(s)
	up := true
	for i, c := range r {
		if up && unicode.IsLetter(c) {
			r[i] = unicode.ToUpper(c)
		}
		up = c == ' ' || c == '-' || c == '/' || c == '&'
	}
	return string(r)
}
