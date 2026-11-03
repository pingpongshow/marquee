package intown

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// schemaVersion is the cache file's schema. Everything in it can be fetched again, so another
// version isn't migrated: the file is deleted and made afresh.
const schemaVersion = 2

const schema = `
CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
-- Acts the source knows, with the ones of one name findable, to spot two of them.
CREATE TABLE attractions (
    id    TEXT PRIMARY KEY,
    name  TEXT NOT NULL,
    norm  TEXT NOT NULL,
    mbids TEXT NOT NULL DEFAULT '',
    genre TEXT NOT NULL DEFAULT ''
);
CREATE INDEX attractions_norm ON attractions(norm);
-- Library artists looked up, by artist key (see artistKey). attraction_id is '' for none.
CREATE TABLE artist_matches (
    artist_key    TEXT PRIMARY KEY,
    attraction_id TEXT NOT NULL DEFAULT '',
    checked_at    TEXT NOT NULL DEFAULT '',
    shows_at      TEXT NOT NULL DEFAULT '',
    opened_at     TEXT NOT NULL DEFAULT '',
    retry_at      TEXT NOT NULL DEFAULT ''
);
CREATE INDEX artist_matches_attraction ON artist_matches(attraction_id);
CREATE TABLE shows (
    id         TEXT PRIMARY KEY,
    source     TEXT NOT NULL,
    title      TEXT NOT NULL,
    url        TEXT NOT NULL,
    local_date TEXT NOT NULL,
    local_time TEXT NOT NULL DEFAULT '',
    time_zone  TEXT NOT NULL DEFAULT '',
    starts_at  TEXT NOT NULL DEFAULT '',
    status     TEXT NOT NULL,
    genre      TEXT NOT NULL DEFAULT '',
    sub_genre  TEXT NOT NULL DEFAULT '',
    venue      TEXT NOT NULL DEFAULT '',
    city       TEXT NOT NULL DEFAULT '',
    region     TEXT NOT NULL DEFAULT '',
    country    TEXT NOT NULL DEFAULT '',
    lat        REAL,
    lon        REAL
);
CREATE INDEX shows_date ON shows(local_date);
CREATE TABLE show_acts (
    show_id       TEXT NOT NULL REFERENCES shows(id) ON DELETE CASCADE,
    attraction_id TEXT NOT NULL,
    position      INTEGER NOT NULL,
    PRIMARY KEY (show_id, attraction_id)
);
CREATE INDEX show_acts_attraction ON show_acts(attraction_id);
-- Where a postcode or city is, worked out from the venues there.
CREATE TABLE places (
    key        TEXT PRIMARY KEY,
    found      INTEGER NOT NULL,
    lat        REAL NOT NULL DEFAULT 0,
    lon        REAL NOT NULL DEFAULT 0,
    checked_at TEXT NOT NULL
);
-- Searches by rounded location, and what they found.
CREATE TABLE near_searches (key TEXT PRIMARY KEY, fetched_at TEXT NOT NULL);
CREATE TABLE near_shows (
    search_key TEXT NOT NULL REFERENCES near_searches(key) ON DELETE CASCADE,
    show_id    TEXT NOT NULL REFERENCES shows(id) ON DELETE CASCADE,
    PRIMARY KEY (search_key, show_id)
);
`

// cache is the cache file: its own small SQLite database, left out of backups (they copy
// only the main one) and deleted when In Town is turned off.
type cache struct {
	db   *sql.DB
	path string
}

func removeCacheFiles(path string) {
	for _, p := range []string{path, path + "-wal", path + "-shm"} {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			slog.Warn("in town: remove cache", "err", err)
		}
	}
}

// errRebuild is a file that can't be used and won't get better: damaged, or another version.
var errRebuild = errors.New("can't be used")

// shouldRebuild reports an error that's about the file itself. A locked file or a full disk
// isn't: those pass, and the file is what we'd want afterwards.
func shouldRebuild(err error) bool { return errors.Is(err, errRebuild) || isCorrupt(err) }

// openCache opens the cache file. One that's corrupt or of another schema version is
// deleted and made afresh, since everything in it can be downloaded again.
func openCache(ctx context.Context, path string) (*cache, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	c, err := tryOpenCache(ctx, path)
	if err == nil {
		return c, nil
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if !shouldRebuild(err) {
		return nil, err
	}
	slog.Info("in town: making a new cache", "reason", err)
	removeCacheFiles(path)
	return tryOpenCache(ctx, path)
}

func tryOpenCache(ctx context.Context, path string) (*cache, error) {
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(10000)" +
		"&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*cache, error) {
		db.Close()
		return nil, err
	}
	if err := db.PingContext(ctx); err != nil {
		return fail(fmt.Errorf("open: %w", err))
	}
	var check string
	if err := db.QueryRowContext(ctx, `PRAGMA quick_check(1)`).Scan(&check); err != nil {
		return fail(fmt.Errorf("check: %w", err))
	}
	if check != "ok" {
		return fail(fmt.Errorf("%w: damaged", errRebuild))
	}
	var version, tables int
	if err := db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return fail(fmt.Errorf("version: %w", err))
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master`).Scan(&tables); err != nil {
		return fail(fmt.Errorf("tables: %w", err))
	}
	switch {
	case tables == 0:
		if _, err := db.ExecContext(ctx, schema+fmt.Sprintf(`PRAGMA user_version = %d;`, schemaVersion)); err != nil {
			return fail(fmt.Errorf("schema: %w", err))
		}
	case version != schemaVersion:
		return fail(fmt.Errorf("%w: schema version %d", errRebuild, version))
	}
	return &cache{db: db, path: path}, nil
}

// isCorrupt reports an error that means the file itself is bad.
func isCorrupt(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, s := range []string{"malformed", "not a database", "no such table", "no such column"} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

func (c *cache) close() { c.db.Close() }

// ---------- meta ----------

func (c *cache) setMeta(ctx context.Context, key, value string) {
	if _, err := c.db.ExecContext(ctx, `INSERT INTO meta(key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, value); err != nil {
		slog.Debug("in town: save", "key", key, "err", err)
	}
}

func (c *cache) meta(ctx context.Context, key string) string {
	var v string
	c.db.QueryRowContext(ctx, `SELECT value FROM meta WHERE key = ?`, key).Scan(&v)
	return v
}

func (c *cache) saveBudget(ctx context.Context, st budgetState) {
	raw, _ := json.Marshal(st)
	c.setMeta(ctx, "budget", string(raw))
}

func (c *cache) loadBudget(ctx context.Context) (budgetState, bool) {
	var st budgetState
	raw := c.meta(ctx, "budget")
	return st, raw != "" && json.Unmarshal([]byte(raw), &st) == nil
}

// ---------- attractions and matches ----------

func (c *cache) saveAttraction(ctx context.Context, q execer, a Attraction) error {
	_, err := q.ExecContext(ctx, `INSERT INTO attractions(id, name, norm, mbids, genre) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET name = excluded.name, norm = excluded.norm,
			mbids = CASE WHEN excluded.mbids <> '' THEN excluded.mbids ELSE mbids END,
			genre = CASE WHEN excluded.genre <> '' THEN excluded.genre ELSE genre END`,
		a.ID, a.Name, normalizeName(a.Name), strings.Join(a.MBIDs, ","), a.Genre)
	return err
}

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// matchRow is what's known of a library artist.
type matchRow struct {
	Key, AttractionID          string
	CheckedAt, ShowsAt, Opened time.Time
	// Retry: this artist's lookup failed, and isn't tried again before then.
	Retry time.Time
}

func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

func stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func (c *cache) matches(ctx context.Context) (map[string]matchRow, error) {
	rows, err := c.db.QueryContext(ctx, `SELECT artist_key, attraction_id, checked_at, shows_at, opened_at, retry_at FROM artist_matches`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]matchRow{}
	for rows.Next() {
		var m matchRow
		var checked, shows, opened, retry string
		if err := rows.Scan(&m.Key, &m.AttractionID, &checked, &shows, &opened, &retry); err != nil {
			return nil, err
		}
		m.CheckedAt, m.ShowsAt, m.Opened, m.Retry = parseTime(checked), parseTime(shows), parseTime(opened), parseTime(retry)
		out[m.Key] = m
	}
	return out, rows.Err()
}

func (c *cache) match(ctx context.Context, key string) (matchRow, error) {
	var m matchRow
	var checked, shows, opened, retry string
	err := c.db.QueryRowContext(ctx, `SELECT artist_key, attraction_id, checked_at, shows_at, opened_at, retry_at FROM artist_matches WHERE artist_key = ?`, key).
		Scan(&m.Key, &m.AttractionID, &checked, &shows, &opened, &retry)
	if errors.Is(err, sql.ErrNoRows) {
		return matchRow{Key: key}, nil
	}
	if err != nil {
		return m, err
	}
	m.CheckedAt, m.ShowsAt, m.Opened, m.Retry = parseTime(checked), parseTime(shows), parseTime(opened), parseTime(retry)
	return m, nil
}

// saveMatch stores a lookup's result and the attractions of that exact name, which is what
// refuses a same-named act seen later.
func (c *cache) saveMatch(ctx context.Context, key, attractionID string, seen []Attraction, at time.Time) error {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, a := range seen {
		if err := c.saveAttraction(ctx, tx, a); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO artist_matches(artist_key, attraction_id, checked_at) VALUES (?, ?, ?)
		ON CONFLICT(artist_key) DO UPDATE SET attraction_id = excluded.attraction_id, checked_at = excluded.checked_at, retry_at = ''`, key, attractionID, stamp(at)); err != nil {
		return err
	}
	return tx.Commit()
}

// touchOpened notes an artist's page being opened, which moves them up the background queue.
func (c *cache) touchOpened(ctx context.Context, key string, at time.Time) error {
	_, err := c.db.ExecContext(ctx, `INSERT INTO artist_matches(artist_key, opened_at) VALUES (?, ?)
		ON CONFLICT(artist_key) DO UPDATE SET opened_at = excluded.opened_at`, key, stamp(at))
	return err
}

// setRetry holds an artist's lookups until at, after one failed.
func (c *cache) setRetry(ctx context.Context, key string, at time.Time) error {
	_, err := c.db.ExecContext(ctx, `INSERT INTO artist_matches(artist_key, retry_at) VALUES (?, ?)
		ON CONFLICT(artist_key) DO UPDATE SET retry_at = excluded.retry_at`, key, stamp(at))
	return err
}

// dropMatches forgets artists no longer in the library.
func (c *cache) dropMatches(ctx context.Context, keep map[string]bool) error {
	have, err := c.matches(ctx)
	if err != nil {
		return err
	}
	for key := range have {
		if !keep[key] {
			if _, err := c.db.ExecContext(ctx, `DELETE FROM artist_matches WHERE artist_key = ?`, key); err != nil {
				return err
			}
		}
	}
	return nil
}

// attractionNames counts the attractions of each normalised name.
func (c *cache) attractionNames(ctx context.Context) (map[string]int, error) {
	rows, err := c.db.QueryContext(ctx, `SELECT norm, COUNT(*) FROM attractions GROUP BY norm`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var n string
		var count int
		if err := rows.Scan(&n, &count); err != nil {
			return nil, err
		}
		out[n] = count
	}
	return out, rows.Err()
}

// ---------- shows ----------

func (c *cache) saveShow(ctx context.Context, tx *sql.Tx, s Show) error {
	var lat, lon any
	if s.Venue.HasPoint {
		lat, lon = s.Venue.Lat, s.Venue.Lon
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO shows(id, source, title, url, local_date, local_time, time_zone, starts_at, status, genre, sub_genre, venue, city, region, country, lat, lon)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET source = excluded.source, title = excluded.title, url = excluded.url, local_date = excluded.local_date, local_time = excluded.local_time,
			time_zone = excluded.time_zone, starts_at = excluded.starts_at, status = excluded.status, genre = excluded.genre, sub_genre = excluded.sub_genre,
			venue = excluded.venue, city = excluded.city, region = excluded.region, country = excluded.country, lat = excluded.lat, lon = excluded.lon`,
		s.ID, s.Source, s.Title, s.URL, s.LocalDate, s.LocalTime, s.TimeZone, stamp(s.StartsAt), s.Status, s.Genre, s.SubGenre,
		s.Venue.Name, s.Venue.City, s.Venue.Region, s.Venue.Country, lat, lon); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM show_acts WHERE show_id = ?`, s.ID); err != nil {
		return err
	}
	for i, a := range s.Acts {
		if err := c.saveAttraction(ctx, tx, a); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO show_acts(show_id, attraction_id, position) VALUES (?, ?, ?)`, s.ID, a.ID, i); err != nil {
			return err
		}
	}
	return nil
}

// replaceArtistShows makes shows the attraction's whole list, and notes when that was, which
// drops those it no longer has (cancelled, sold on elsewhere).
func (c *cache) replaceArtistShows(ctx context.Context, key, attractionID string, shows []Show, at time.Time) error {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM show_acts WHERE attraction_id = ?`, attractionID); err != nil {
		return err
	}
	for _, s := range shows {
		if err := c.saveShow(ctx, tx, s); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM shows WHERE id NOT IN (SELECT show_id FROM show_acts) AND id NOT IN (SELECT show_id FROM near_shows)`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE artist_matches SET shows_at = ?, retry_at = '' WHERE artist_key = ?`, stamp(at), key); err != nil {
		return err
	}
	return tx.Commit()
}

func (c *cache) saveNear(ctx context.Context, key string, shows []Show, at time.Time) error {
	tx, err := c.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO near_searches(key, fetched_at) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET fetched_at = excluded.fetched_at`, key, stamp(at)); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM near_shows WHERE search_key = ?`, key); err != nil {
		return err
	}
	for _, s := range shows {
		if err := c.saveShow(ctx, tx, s); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO near_shows(search_key, show_id) VALUES (?, ?)`, key, s.ID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (c *cache) nearFetchedAt(ctx context.Context, key string) time.Time {
	var at string
	c.db.QueryRowContext(ctx, `SELECT fetched_at FROM near_searches WHERE key = ?`, key).Scan(&at)
	return parseTime(at)
}

const showCols = `s.id, s.source, s.title, s.url, s.local_date, s.local_time, s.time_zone, s.starts_at, s.status, s.genre, s.sub_genre, s.venue, s.city, s.region, s.country, s.lat, s.lon`

// loadShows reads shows by id, soonest first, with their acts.
func (c *cache) loadShows(ctx context.Context, from string, args []any) ([]Show, error) {
	rows, err := c.db.QueryContext(ctx, `SELECT DISTINCT `+showCols+` FROM shows s `+from+` ORDER BY s.local_date, s.local_time, s.id`, args...)
	if err != nil {
		return nil, err
	}
	var out []Show
	for rows.Next() {
		var s Show
		var starts string
		var lat, lon sql.NullFloat64
		if err := rows.Scan(&s.ID, &s.Source, &s.Title, &s.URL, &s.LocalDate, &s.LocalTime, &s.TimeZone, &starts, &s.Status, &s.Genre, &s.SubGenre,
			&s.Venue.Name, &s.Venue.City, &s.Venue.Region, &s.Venue.Country, &lat, &lon); err != nil {
			rows.Close()
			return nil, err
		}
		s.StartsAt = parseTime(starts)
		if lat.Valid && lon.Valid {
			s.Venue.Lat, s.Venue.Lon, s.Venue.HasPoint = lat.Float64, lon.Float64, true
		}
		out = append(out, s)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, c.fillActs(ctx, out)
}

// sqlChunk bounds the ids in one IN list.
const sqlChunk = 500

func placeholders(n int) string { return "?" + strings.Repeat(",?", n-1) }

func anyOf(ids []string) []any {
	out := make([]any, len(ids))
	for i, id := range ids {
		out[i] = id
	}
	return out
}

func (c *cache) fillActs(ctx context.Context, shows []Show) error {
	index := make(map[string]int, len(shows))
	ids := make([]string, len(shows))
	for i, s := range shows {
		index[s.ID], ids[i] = i, s.ID
	}
	for part := range slices.Chunk(ids, sqlChunk) {
		rows, err := c.db.QueryContext(ctx, `SELECT sa.show_id, a.id, a.name, a.mbids, a.genre FROM show_acts sa JOIN attractions a ON a.id = sa.attraction_id
			WHERE sa.show_id IN (`+placeholders(len(part))+`) ORDER BY sa.show_id, sa.position`, anyOf(part)...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var showID, mbids string
			a := Attraction{Music: true, Upcoming: -1}
			if err := rows.Scan(&showID, &a.ID, &a.Name, &mbids, &a.Genre); err != nil {
				rows.Close()
				return err
			}
			if mbids != "" {
				a.MBIDs = strings.Split(mbids, ",")
			}
			shows[index[showID]].Acts = append(shows[index[showID]].Acts, a)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
	}
	return nil
}

// showsOfAttractions lists the shows these attractions are on.
func (c *cache) showsOfAttractions(ctx context.Context, ids []string) ([]Show, error) {
	var out []Show
	seen := map[string]bool{}
	for part := range slices.Chunk(ids, sqlChunk) {
		list, err := c.loadShows(ctx, `JOIN show_acts sa ON sa.show_id = s.id WHERE sa.attraction_id IN (`+placeholders(len(part))+`)`, anyOf(part))
		if err != nil {
			return nil, err
		}
		// A bill can have acts in two chunks, so it comes back once from each.
		for _, s := range list {
			if !seen[s.ID] {
				seen[s.ID] = true
				out = append(out, s)
			}
		}
	}
	return out, nil
}

func (c *cache) showsOfSearch(ctx context.Context, key string) ([]Show, error) {
	return c.loadShows(ctx, `JOIN near_shows n ON n.show_id = s.id WHERE n.search_key = ?`, []any{key})
}

// ---------- places ----------

type place struct {
	Found     bool
	Lat, Lon  float64
	CheckedAt time.Time
}

func (c *cache) place(ctx context.Context, key string) (place, bool) {
	var p place
	var at string
	err := c.db.QueryRowContext(ctx, `SELECT found, lat, lon, checked_at FROM places WHERE key = ?`, key).Scan(&p.Found, &p.Lat, &p.Lon, &at)
	if err != nil {
		return p, false
	}
	p.CheckedAt = parseTime(at)
	return p, true
}

func (c *cache) savePlace(ctx context.Context, key string, p place) error {
	_, err := c.db.ExecContext(ctx, `INSERT INTO places(key, found, lat, lon, checked_at) VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET found = excluded.found, lat = excluded.lat, lon = excluded.lon, checked_at = excluded.checked_at`,
		key, p.Found, p.Lat, p.Lon, stamp(p.CheckedAt))
	return err
}

// ---------- past shows ----------

// isPast reports whether a show's date has gone, grace days ago, at its venue. Without a known
// zone it goes by the last place on Earth still on that date, so it's never early.
func isPast(localDate, zone string, now time.Time, grace int) bool {
	day, err := time.Parse(time.DateOnly, localDate)
	if err != nil {
		return true
	}
	loc := time.FixedZone("", -12*3600)
	if zone != "" {
		if l, err := time.LoadLocation(zone); err == nil {
			loc = l
		}
	}
	y, m, d := now.In(loc).Date()
	return day.Before(time.Date(y, m, d, 0, 0, 0, 0, time.UTC).AddDate(0, 0, -grace))
}

// pruneGraceDays keeps a show in the file for a day after its date.
const pruneGraceDays = 1

// compactEvery is how often the file is compacted, when something was removed.
const compactEvery = 7 * 24 * time.Hour

// prune deletes shows whose date has passed, and what only they needed.
func (c *cache) prune(ctx context.Context, now time.Time) (int, error) {
	// No date later than today in UTC+14 can have passed anywhere.
	limit := now.UTC().AddDate(0, 0, 1).Format(time.DateOnly)
	rows, err := c.db.QueryContext(ctx, `SELECT id, local_date, time_zone FROM shows WHERE local_date <= ?`, limit)
	if err != nil {
		return 0, err
	}
	var gone []string
	for rows.Next() {
		var id, date, zone string
		if err := rows.Scan(&id, &date, &zone); err != nil {
			rows.Close()
			return 0, err
		}
		if isPast(date, zone, now, pruneGraceDays) {
			gone = append(gone, id)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	for part := range slices.Chunk(gone, sqlChunk) {
		if _, err := c.db.ExecContext(ctx, `DELETE FROM shows WHERE id IN (`+placeholders(len(part))+`)`, anyOf(part)...); err != nil {
			return 0, err
		}
	}
	// Searches nobody has repeated, and the shows that were only theirs.
	old := stamp(now.Add(-7 * 24 * time.Hour))
	if _, err := c.db.ExecContext(ctx, `DELETE FROM near_searches WHERE fetched_at < ?`, old); err != nil {
		return 0, err
	}
	if _, err := c.db.ExecContext(ctx, `DELETE FROM shows WHERE id NOT IN (SELECT show_id FROM show_acts) AND id NOT IN (SELECT show_id FROM near_shows)`); err != nil {
		return 0, err
	}
	// Where postcodes and cities are: a point nobody has used for a while, and a miss once it's
	// due again. A point outlives its refresh so that a failed refresh can fall back on it.
	if _, err := c.db.ExecContext(ctx, `DELETE FROM places WHERE (found = 1 AND checked_at < ?) OR (found = 0 AND checked_at < ?)`,
		stamp(now.Add(-placeKeep)), stamp(now.Add(-placeMissTTL))); err != nil {
		return 0, err
	}
	// Attractions nothing refers to, keeping those of a library artist's name: those are
	// what refuses a same-named act. A rematch brings them back in any case.
	if _, err := c.db.ExecContext(ctx, `DELETE FROM attractions WHERE id NOT IN (SELECT attraction_id FROM show_acts)
		AND id NOT IN (SELECT attraction_id FROM artist_matches)
		AND norm NOT IN (SELECT SUBSTR(artist_key, 6) FROM artist_matches WHERE artist_key LIKE 'name:%')`); err != nil {
		return 0, err
	}
	if len(gone) > 0 && now.Sub(parseTime(c.meta(ctx, "compacted_at"))) >= compactEvery {
		if _, err := c.db.ExecContext(ctx, `VACUUM`); err != nil {
			return len(gone), err
		}
		c.setMeta(ctx, "compacted_at", stamp(now))
	}
	return len(gone), nil
}

// counts is for the status line.
func (c *cache) counts(ctx context.Context) (checked, matched, shows int) {
	c.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM artist_matches WHERE checked_at <> ''`).Scan(&checked)
	c.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM artist_matches WHERE attraction_id <> ''`).Scan(&matched)
	c.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM shows`).Scan(&shows)
	return checked, matched, shows
}
