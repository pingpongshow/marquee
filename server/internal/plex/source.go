// Package plex imports libraries, matches, artwork, users, watch state, history, markers and
// playlists from a Plex Media Server database (M2, docs/04-plex-import.md).
//
// The importer never touches Plex's live database: it copies the database and its WAL into
// Marquee's config folder, checks the copy's integrity, and reads the copy.
package plex

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"modernc.org/sqlite"
)

const dbRel = "Library/Application Support/Plex Media Server/Plug-in Support/Databases/com.plexapp.plugins.library.db"

// Metadata bundle folders under "Plex Media Server/Metadata", by Plex metadata type.
var bundleDirs = map[int]string{1: "Movies", 2: "TV Shows", 8: "Artists", 9: "Albums"}

// Plex metadata types.
const (
	typeMovie    = 1
	typeShow     = 2
	typeSeason   = 3
	typeEpisode  = 4
	typeArtist   = 8
	typeAlbum    = 9
	typeTrack    = 10
	typePlaylist = 15
)

var ErrNoDatabase = errors.New("Plex database not found")

// DatabasePath returns the Plex library database under a Plex data folder (the folder that
// contains "Library"), or ErrNoDatabase.
func DatabasePath(plexRoot string) (string, error) {
	p := filepath.Join(plexRoot, dbRel)
	if fi, err := os.Stat(p); err != nil || fi.IsDir() {
		return "", ErrNoDatabase
	}
	return p, nil
}

// Source is an open snapshot of a Plex database.
type Source struct {
	db       *sql.DB
	plexRoot string
	TakenAt  time.Time
}

// Snapshot copies the Plex database (plus WAL) into workDir and opens the copy. Plex may be
// writing while we copy, so the copy is integrity-checked and retried.
func Snapshot(ctx context.Context, plexRoot, workDir string) (*Source, error) {
	src, err := DatabasePath(plexRoot)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(workDir, 0o755); err != nil {
		return nil, err
	}
	dst := filepath.Join(workDir, "plex-snapshot.db")
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		for _, suffix := range []string{"", "-wal", "-shm"} {
			os.Remove(dst + suffix)
		}
		// WAL first: pages referenced by a slightly newer WAL are a smaller risk than a
		// database file that has moved past the WAL we copied.
		if err := copyIfExists(src+"-wal", dst+"-wal"); err != nil {
			return nil, err
		}
		if err := copyIfExists(src, dst); err != nil {
			return nil, err
		}
		db, err := sql.Open("sqlite", "file:"+dst+"?_pragma=busy_timeout(5000)")
		if err != nil {
			return nil, err
		}
		// PRAGMA quick_check can't be used: Plex's indexes use its custom ICU collation.
		// Reading every table the importer uses through a full scan catches a torn copy.
		if err := verify(ctx, db); err == nil {
			return &Source{db: db, plexRoot: plexRoot, TakenAt: time.Now()}, nil
		} else {
			lastErr = err
		}
		db.Close()
		time.Sleep(2 * time.Second)
	}
	return nil, fmt.Errorf("could not take a consistent copy of the Plex database: %w", lastErr)
}

func verify(ctx context.Context, db *sql.DB) error {
	for _, t := range []string{"library_sections", "section_locations", "accounts", "metadata_items", "media_items",
		"media_parts", "metadata_item_settings", "metadata_item_views", "taggings", "tags", "play_queue_generators"} {
		var n int
		if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM (SELECT * FROM `+t+`)`).Scan(&n); err != nil {
			return fmt.Errorf("%s: %w", t, err)
		}
	}
	return nil
}

func init() {
	// Plex declares text columns with its ICU "icu_root" collation. A case-insensitive
	// comparison is close enough for reading (we never write to the snapshot).
	sqlite.RegisterCollationUtf8("icu_root", func(a, b string) int {
		return strings.Compare(strings.ToLower(a), strings.ToLower(b))
	})
}

func copyIfExists(src, dst string) error {
	in, err := os.Open(src)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

func (s *Source) Close() error { return s.db.Close() }

type Section struct {
	ID    int64
	Name  string
	Type  int // 1 movie, 2 show, 8 artist
	Agent string
	Roots []string
}

func (s *Source) Sections(ctx context.Context) ([]Section, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, section_type, COALESCE(agent, '') FROM library_sections ORDER BY id`)
	if err != nil {
		return nil, err
	}
	var out []Section
	for rows.Next() {
		var sec Section
		if err := rows.Scan(&sec.ID, &sec.Name, &sec.Type, &sec.Agent); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, sec)
	}
	rows.Close()
	for i := range out {
		r, err := s.db.QueryContext(ctx, `SELECT root_path FROM section_locations WHERE library_section_id = ? ORDER BY root_path`, out[i].ID)
		if err != nil {
			return nil, err
		}
		for r.Next() {
			var p string
			r.Scan(&p)
			out[i].Roots = append(out[i].Roots, p)
		}
		r.Close()
	}
	return out, nil
}

type Account struct {
	ID                                               int64
	Name                                             string
	Watched, InProgress, Ratings, History, Playlists int
}

// Accounts returns named accounts that have watch state, history or playlists.
func (s *Source) Accounts(ctx context.Context) ([]Account, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT a.id, a.name,
		(SELECT COUNT(*) FROM metadata_item_settings x WHERE x.account_id = a.id AND x.view_count > 0),
		(SELECT COUNT(*) FROM metadata_item_settings x WHERE x.account_id = a.id AND x.view_offset > 0),
		(SELECT COUNT(*) FROM metadata_item_settings x WHERE x.account_id = a.id AND x.rating > 0),
		(SELECT COUNT(*) FROM metadata_item_views x WHERE x.account_id = a.id),
		(SELECT COUNT(*) FROM metadata_item_accounts x JOIN metadata_items m ON m.id = x.metadata_item_id
		 WHERE x.account_id = a.id AND m.metadata_type = 15)
		FROM accounts a WHERE COALESCE(a.name, '') != '' ORDER BY a.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Account
	for rows.Next() {
		var a Account
		if err := rows.Scan(&a.ID, &a.Name, &a.Watched, &a.InProgress, &a.Ratings, &a.History, &a.Playlists); err != nil {
			return nil, err
		}
		if a.Watched+a.InProgress+a.Ratings+a.History+a.Playlists > 0 || a.ID == 1 {
			out = append(out, a)
		}
	}
	return out, rows.Err()
}

// Part is a media file Plex knows about, with the leaf item (movie/episode/track) it belongs to.
type Part struct {
	File      string
	ItemID    int64
	ItemType  int
	SectionID int64
}

func (s *Source) Parts(ctx context.Context) ([]Part, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT p.file, m.id, m.metadata_type, COALESCE(m.library_section_id, 0)
		FROM media_parts p JOIN media_items mi ON mi.id = p.media_item_id JOIN metadata_items m ON m.id = mi.metadata_item_id
		WHERE p.file != '' AND p.deleted_at IS NULL AND m.metadata_type IN (1, 4, 10) AND m.deleted_at IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Part
	for rows.Next() {
		var p Part
		if err := rows.Scan(&p.File, &p.ItemID, &p.ItemType, &p.SectionID); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Item is a Plex metadata item (any type) with the fields the importer needs.
type Item struct {
	ID, ParentID      int64
	Type, Year        int
	GUID, Title, Hash string
	UserFields        string
	UserThumb         string
	UserArt           string
}

func (s *Source) Items(ctx context.Context) (map[int64]Item, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, COALESCE(parent_id, 0), metadata_type, COALESCE(year, 0), COALESCE(guid, ''), COALESCE(title, ''),
		COALESCE(hash, ''), COALESCE(user_fields, ''), COALESCE(user_thumb_url, ''), COALESCE(user_art_url, '')
		FROM metadata_items WHERE metadata_type IN (1, 2, 3, 4, 8, 9, 10) AND deleted_at IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]Item{}
	for rows.Next() {
		var it Item
		if err := rows.Scan(&it.ID, &it.ParentID, &it.Type, &it.Year, &it.GUID, &it.Title, &it.Hash, &it.UserFields, &it.UserThumb, &it.UserArt); err != nil {
			return nil, err
		}
		out[it.ID] = it
	}
	return out, rows.Err()
}

// ExternalIDs returns provider ids (tmdb, imdb, tvdb) per Plex item.
func (s *Source) ExternalIDs(ctx context.Context) (map[int64]map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT g.metadata_item_id, t.tag FROM taggings g JOIN tags t ON t.id = g.tag_id WHERE t.tag_type = 314`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]map[string]string{}
	for rows.Next() {
		var id int64
		var tag string
		if err := rows.Scan(&id, &tag); err != nil {
			return nil, err
		}
		provider, value, ok := strings.Cut(tag, "://")
		if !ok || value == "" {
			continue
		}
		if out[id] == nil {
			out[id] = map[string]string{}
		}
		out[id][provider] = value
	}
	return out, rows.Err()
}

// Setting is per-account watch state for a Plex guid.
type Setting struct {
	AccountID               int64
	GUID                    string
	Rating                  float64
	ViewOffsetMS, ViewCount int64
	LastViewedAt            int64 // unix seconds, 0 = never
}

func (s *Source) Settings(ctx context.Context) ([]Setting, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT account_id, guid, COALESCE(rating, 0), COALESCE(view_offset, 0), COALESCE(view_count, 0),
		COALESCE(last_viewed_at, 0) FROM metadata_item_settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Setting
	for rows.Next() {
		var st Setting
		if err := rows.Scan(&st.AccountID, &st.GUID, &st.Rating, &st.ViewOffsetMS, &st.ViewCount, &st.LastViewedAt); err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

type View struct {
	AccountID        int64
	GUID, Title      string
	GrandparentTitle string
	ViewedAt         int64
}

func (s *Source) Views(ctx context.Context) ([]View, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT account_id, COALESCE(guid, ''), COALESCE(title, ''), COALESCE(grandparent_title, ''), viewed_at
		FROM metadata_item_views WHERE viewed_at IS NOT NULL ORDER BY viewed_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []View
	for rows.Next() {
		var v View
		if err := rows.Scan(&v.AccountID, &v.GUID, &v.Title, &v.GrandparentTitle, &v.ViewedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

type Marker struct {
	ItemID         int64
	Kind           string // intro, credits
	StartMS, EndMS int64
}

func (s *Source) Markers(ctx context.Context) ([]Marker, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT g.metadata_item_id, LOWER(t.tag), g.time_offset, g.end_time_offset
		FROM taggings g JOIN tags t ON t.id = g.tag_id
		WHERE LOWER(t.tag) IN ('intro', 'credits') AND g.time_offset IS NOT NULL AND g.end_time_offset > g.time_offset`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Marker
	for rows.Next() {
		var m Marker
		if err := rows.Scan(&m.ItemID, &m.Kind, &m.StartMS, &m.EndMS); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

type Playlist struct {
	ID      int64
	Title   string
	OwnerID int64
	ItemIDs []int64 // in playlist order
}

func (s *Source) Playlists(ctx context.Context) ([]Playlist, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT m.id, m.title, COALESCE((SELECT account_id FROM metadata_item_accounts a WHERE a.metadata_item_id = m.id LIMIT 1), 1)
		FROM metadata_items m WHERE m.metadata_type = 15 AND m.deleted_at IS NULL ORDER BY m.id`)
	if err != nil {
		return nil, err
	}
	var out []Playlist
	for rows.Next() {
		var p Playlist
		if err := rows.Scan(&p.ID, &p.Title, &p.OwnerID); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, p)
	}
	rows.Close()
	for i := range out {
		r, err := s.db.QueryContext(ctx, `SELECT metadata_item_id FROM play_queue_generators
			WHERE playlist_id = ? AND metadata_item_id IS NOT NULL ORDER BY "order", id`, out[i].ID)
		if err != nil {
			return nil, err
		}
		for r.Next() {
			var id int64
			r.Scan(&id)
			out[i].ItemIDs = append(out[i].ItemIDs, id)
		}
		r.Close()
	}
	return out, nil
}

// ArtworkFile finds the image file behind a user_thumb_url / user_art_url such as
// "metadata://posters/tv.plex.agents.movie_ab12…" or "upload://posters/862c…".
func (s *Source) ArtworkFile(it Item, ref string) string {
	scheme, rest, ok := strings.Cut(ref, "://")
	if !ok || it.Hash == "" || len(it.Hash) < 2 {
		return ""
	}
	kind, name, ok := strings.Cut(rest, "/")
	if !ok || strings.Contains(name, "/") || strings.Contains(name, "..") {
		return ""
	}
	folder, ok := bundleDirs[it.Type]
	if !ok {
		return ""
	}
	bundle := filepath.Join(s.plexRoot, "Library/Application Support/Plex Media Server/Metadata", folder,
		it.Hash[:1], it.Hash[1:]+".bundle")
	var candidates []string
	switch scheme {
	case "upload":
		candidates = []string{filepath.Join(bundle, "Uploads", kind, name)}
	case "metadata":
		candidates = []string{
			filepath.Join(bundle, "Contents", "_combined", kind, name),
			filepath.Join(bundle, "Uploads", kind, name),
		}
	default:
		return ""
	}
	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() && fi.Size() > 0 {
			return c
		}
	}
	return ""
}
