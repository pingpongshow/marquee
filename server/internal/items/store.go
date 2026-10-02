// Package items reads the metadata tree for browsing APIs.
package items

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode"
)

var ErrNotFound = errors.New("item not found")

type Summary struct {
	ID, LibraryID                 int64
	Type, Title, OriginalTitle    string
	Year, Index, AbsIndex, Disc   int
	ParentID, GrandparentID       int64
	ParentTitle, GrandparentTitle string
	ArtistCredit                  string
	ChildCount, LeafCount         int
	DurationMS                    int64
	ReleaseDate                   string
	Available                     bool
	MatchState                    string
	AddedAt                       time.Time
	Poster, Backdrop, Thumb, Logo int64 // selected artwork ids (0 = none)
	// The requesting user's watch state.
	ViewOffsetMS  int64
	ViewCount     int
	LastViewedAt  string
	WatchedLeaves int
}

type Detail struct {
	Summary
	Plot, Tagline, ContentRating, Studio string
	AudienceRating                       float64
	IMDbRating                           float64
	IMDbVotes, RTCritic, Metacritic      int
	Genres                               []string
	Credits                              []Credit
	LockedFields                         []string
	ExternalIDs                          map[string]string
	Versions                             []Version
	Chapters                             []Chapter
}

type Version struct {
	ID    int64
	Label string
	Files []File
}

type File struct {
	ID                                               int64
	Path                                             string
	Size, DurationMS                                 int64
	Container, VideoCodec, AudioCodec, HDRFormat     string
	BitrateKbps, Width, Height, DVProfile, PartIndex int
	Available                                        bool
	Streams                                          []Stream
}

type Stream struct {
	ID                                                      int64
	Kind, Codec, Profile, Language, Title, ChannelLayout    string
	Default, Forced, HearingImpaired, External              bool
	Channels, SampleRate, BitrateKbps, Width, Height, Depth int
	FrameRate                                               float64
}

type Credit struct {
	PersonID              int64
	Name, Role, Character string
	HasPhoto              bool
}

type Chapter struct {
	Title          string
	StartMS, EndMS int64
}

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// TopLevelType is the type listed by default for a library type.
func TopLevelType(libraryType string) string {
	switch libraryType {
	case "movies":
		return "movie"
	case "shows", "anime":
		return "show"
	case "music":
		return "artist"
	default:
		return "video"
	}
}

var sorts = map[string]string{
	"title":     "i.sort_title COLLATE NOCASE, i.year",
	"-title":    "i.sort_title COLLATE NOCASE DESC, i.year DESC",
	"added":     "i.added_at, i.id",
	"-added":    "i.added_at DESC, i.id DESC",
	"year":      "i.year, i.sort_title COLLATE NOCASE",
	"-year":     "i.year DESC, i.sort_title COLLATE NOCASE",
	"released":  "COALESCE(i.originally_available_at, i.year), i.sort_title",
	"-released": "COALESCE(i.originally_available_at, i.year) DESC, i.sort_title",
	"rating":    "COALESCE(i.audience_rating, i.critic_rating, 0), i.sort_title COLLATE NOCASE",
	"-rating":   "COALESCE(i.audience_rating, i.critic_rating, 0) DESC, i.sort_title COLLATE NOCASE",
	"duration":  "COALESCE(i.duration_ms, 0), i.sort_title COLLATE NOCASE",
	"-duration": "COALESCE(i.duration_ms, 0) DESC, i.sort_title COLLATE NOCASE",
	"-viewed":   "(SELECT MAX(x.last_viewed_at) FROM user_item_state x JOIN items l ON l.id = x.item_id WHERE x.user_id = %UID% AND (l.id = i.id OR l.parent_id = i.id OR l.grandparent_id = i.id)) DESC NULLS LAST, i.sort_title COLLATE NOCASE",
	"random":    "random()",
}

var baseCols = `i.id, i.library_id, i.type, i.title, COALESCE(i.original_title, ''), COALESCE(i.year, 0),
	COALESCE(i.idx, 0), COALESCE(i.absolute_idx, 0), COALESCE(i.disc, 0),
	COALESCE(i.parent_id, 0), COALESCE(i.grandparent_id, 0), COALESCE(p.title, ''), COALESCE(g.title, ''),
	COALESCE(i.artist_credit, ''), i.child_count, i.leaf_count, COALESCE(i.duration_ms, 0),
	COALESCE(i.originally_available_at, ''), i.available, i.match_state, i.added_at,
	COALESCE(` + art("poster", "i", "p", "g") + `, 0), COALESCE(` + art("backdrop", "i", "p", "g") + `, 0),
	COALESCE(` + art("thumb", "i") + `, 0), COALESCE(` + art("logo", "i", "p", "g") + `, 0)`

// cols adds the user's watch state to the summary columns (uid 0 = no user).
func cols(uid int64) string {
	u := strconv.FormatInt(uid, 10)
	return baseCols + `,
	COALESCE((SELECT view_offset_ms FROM user_item_state WHERE user_id = ` + u + ` AND item_id = i.id), 0),
	COALESCE((SELECT play_count FROM user_item_state WHERE user_id = ` + u + ` AND item_id = i.id), 0),
	COALESCE((SELECT last_viewed_at FROM user_item_state WHERE user_id = ` + u + ` AND item_id = i.id), ''),
	CASE WHEN i.type IN ('show', 'season', 'artist', 'album') THEN (SELECT COUNT(*) FROM items l
		JOIN user_item_state x ON x.item_id = l.id AND x.user_id = ` + u + ` AND x.play_count > 0
		WHERE (l.parent_id = i.id OR l.grandparent_id = i.id) AND l.type IN ('episode', 'track')) ELSE 0 END`
}

// art builds a COALESCE over the selected artwork of kind for each aliased item, so
// episodes fall back to the season's/show's art and tracks to the album's.
func art(kind string, aliases ...string) string {
	parts := make([]string, len(aliases))
	for i, a := range aliases {
		parts[i] = "(SELECT id FROM artwork WHERE item_id = " + a + ".id AND kind = '" + kind + "' AND selected = 1 LIMIT 1)"
	}
	if len(parts) == 1 {
		return parts[0]
	}
	return "COALESCE(" + strings.Join(parts, ", ") + ")"
}

const summaryFrom = ` FROM items i LEFT JOIN items p ON p.id = i.parent_id LEFT JOIN items g ON g.id = i.grandparent_id`

func scanSummary(row interface{ Scan(...any) error }, extra ...any) (Summary, error) {
	var s Summary
	var added string
	err := row.Scan(append([]any{&s.ID, &s.LibraryID, &s.Type, &s.Title, &s.OriginalTitle, &s.Year, &s.Index, &s.AbsIndex, &s.Disc,
		&s.ParentID, &s.GrandparentID, &s.ParentTitle, &s.GrandparentTitle, &s.ArtistCredit, &s.ChildCount, &s.LeafCount,
		&s.DurationMS, &s.ReleaseDate, &s.Available, &s.MatchState, &added, &s.Poster, &s.Backdrop, &s.Thumb, &s.Logo,
		&s.ViewOffsetMS, &s.ViewCount, &s.LastViewedAt, &s.WatchedLeaves}, extra...)...)
	s.AddedAt, _ = time.Parse(time.RFC3339Nano, added)
	return s, err
}

// List returns items of one type in a library.
func (s *Store) List(ctx context.Context, acc Access, libID int64, typ, sort string, f Filter, offset, limit int) ([]Summary, int, error) {
	order, ok := sorts[sort]
	if !ok {
		order = sorts["title"]
	}
	order = strings.ReplaceAll(order, "%UID%", strconv.FormatInt(acc.UserID, 10))
	var total int
	ac, aargs := acc.clause()
	fc, fargs := f.clause(acc.UserID)
	args := append(append([]any{libID, typ}, aargs...), fargs...)
	where := ` WHERE i.library_id = ? AND i.type = ? AND i.extra_type IS NULL AND ` + ac + ` AND ` + fc
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*)`+summaryFrom+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+cols(acc.UserID)+summaryFrom+where+` ORDER BY `+order+` LIMIT ? OFFSET ?`,
		append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	out, err := collect(rows)
	return out, total, err
}

// Children returns an item's direct children in natural order (season/episode/disc/track number).
func (s *Store) Children(ctx context.Context, acc Access, parentID int64, offset, limit int) ([]Summary, int, error) {
	var total int
	ac, aargs := acc.clause()
	args := append([]any{parentID}, aargs...)
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*)`+summaryFrom+` WHERE i.parent_id = ? AND i.extra_type IS NULL AND `+ac, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+cols(acc.UserID)+summaryFrom+`
		WHERE i.parent_id = ? AND i.extra_type IS NULL AND `+ac+`
		ORDER BY CASE WHEN i.type = 'album' THEN COALESCE(i.year, 9999) END DESC, COALESCE(i.disc, 0), COALESCE(i.idx, 0), i.sort_title COLLATE NOCASE
		LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	out, err := collect(rows)
	return out, total, err
}

func collect(rows *sql.Rows) ([]Summary, error) {
	defer rows.Close()
	out := []Summary{}
	for rows.Next() {
		s, err := scanSummary(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Visible returns ErrNotFound unless the item exists and acc may see it.
func (s *Store) Visible(ctx context.Context, acc Access, id int64) error {
	ac, aargs := acc.clause()
	var one int
	err := s.db.QueryRowContext(ctx, `SELECT 1`+summaryFrom+` WHERE i.id = ? AND `+ac, append([]any{id}, aargs...)...).Scan(&one)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

var searchTypes = []string{"movie", "show", "episode", "artist", "album", "track", "video"}

// SearchGroup is one item type's results.
type SearchGroup struct {
	Type  string
	Items []Summary
}

// Search does a prefix full-text search over titles in the given libraries.
func (s *Store) Search(ctx context.Context, acc Access, query string, perType int) ([]SearchGroup, error) {
	match := ftsQuery(query)
	if match == "" {
		return []SearchGroup{}, nil
	}
	ac, aargs := acc.clause()
	rows, err := s.db.QueryContext(ctx, `SELECT `+cols(acc.UserID)+` FROM items_fts f JOIN items i ON i.id = f.rowid
		LEFT JOIN items p ON p.id = i.parent_id LEFT JOIN items g ON g.id = i.grandparent_id
		WHERE items_fts MATCH ? AND i.extra_type IS NULL AND i.type IN ('movie','show','episode','artist','album','track','video') AND `+ac+`
		ORDER BY bm25(items_fts, 10.0, 5.0, 1.0) LIMIT 400`, append([]any{match}, aargs...)...)
	if err != nil {
		return nil, err
	}
	all, err := collect(rows)
	if err != nil {
		return nil, err
	}
	byType := map[string][]Summary{}
	for _, it := range all {
		if len(byType[it.Type]) < perType {
			byType[it.Type] = append(byType[it.Type], it)
		}
	}
	out := []SearchGroup{}
	for _, t := range searchTypes {
		if len(byType[t]) > 0 {
			out = append(out, SearchGroup{Type: t, Items: byType[t]})
		}
	}
	return out, nil
}

// ftsQuery turns user input into an FTS5 prefix query: every word must match the start
// of a word in the title ("matr reload" → "matr"* "reload"*).
func ftsQuery(q string) string {
	var terms []string
	for _, w := range strings.FieldsFunc(q, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		terms = append(terms, `"`+w+`"*`)
	}
	return strings.Join(terms, " ")
}

// Get returns full item detail. File paths are included only when withPaths is set (admins).
func (s *Store) Get(ctx context.Context, acc Access, id int64, withPaths bool) (Detail, error) {
	var d Detail
	ac, aargs := acc.clause()
	sum, err := scanSummary(s.db.QueryRowContext(ctx, `SELECT `+cols(acc.UserID)+summaryFrom+` WHERE i.id = ? AND `+ac, append([]any{id}, aargs...)...))
	if errors.Is(err, sql.ErrNoRows) {
		return d, ErrNotFound
	}
	if err != nil {
		return d, err
	}
	d.Summary = sum
	var lockedJSON string
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(summary, ''), COALESCE(tagline, ''), COALESCE(content_rating, ''), COALESCE(studio, ''),
		COALESCE(audience_rating, 0), COALESCE(imdb_rating, 0), COALESCE(imdb_votes, 0), COALESCE(rt_critic, -1), COALESCE(metacritic, -1),
		locked_fields FROM items WHERE id = ?`, id).Scan(&d.Plot, &d.Tagline, &d.ContentRating, &d.Studio, &d.AudienceRating,
		&d.IMDbRating, &d.IMDbVotes, &d.RTCritic, &d.Metacritic, &lockedJSON); err != nil {
		return d, err
	}
	var lockedCols []string
	json.Unmarshal([]byte(lockedJSON), &lockedCols)
	d.LockedFields = LockedAPIFields(lockedCols)
	d.Credits = []Credit{}
	if err := eachRow(ctx, s.db, `SELECT p.id, p.name, c.role, COALESCE(c.character, ''), p.photo_path IS NOT NULL
		FROM credits c JOIN people p ON p.id = c.person_id WHERE c.item_id = ?
		ORDER BY CASE c.role WHEN 'actor' THEN 1 ELSE 0 END, c.ord`, []any{id}, func(r *sql.Rows) error {
		var c Credit
		err := r.Scan(&c.PersonID, &c.Name, &c.Role, &c.Character, &c.HasPhoto)
		d.Credits = append(d.Credits, c)
		return err
	}); err != nil {
		return d, err
	}

	d.Genres = []string{}
	if err := eachRow(ctx, s.db, `SELECT t.name FROM item_tags it JOIN tags t ON t.id = it.tag_id WHERE it.item_id = ? AND t.kind = 'genre' ORDER BY t.name`,
		[]any{id}, func(r *sql.Rows) error {
			var g string
			err := r.Scan(&g)
			d.Genres = append(d.Genres, g)
			return err
		}); err != nil {
		return d, err
	}
	d.ExternalIDs = map[string]string{}
	if err := eachRow(ctx, s.db, `SELECT provider, value FROM external_ids WHERE item_id = ?`, []any{id}, func(r *sql.Rows) error {
		var p, v string
		err := r.Scan(&p, &v)
		d.ExternalIDs[p] = v
		return err
	}); err != nil {
		return d, err
	}

	d.Versions = []Version{}
	fileIdx := map[int64]*File{}
	if err := eachRow(ctx, s.db, `SELECT v.id, v.label, f.id, f.path, f.size, COALESCE(f.duration_ms, 0), COALESCE(f.container, ''),
			COALESCE(f.video_codec, ''), COALESCE(f.audio_codec, ''), COALESCE(f.hdr_format, ''), COALESCE(f.bitrate_kbps, 0),
			COALESCE(f.width, 0), COALESCE(f.height, 0), COALESCE(f.dv_profile, 0), f.part_index, f.available
		FROM media_versions v JOIN media_files f ON f.version_id = v.id WHERE v.item_id = ? ORDER BY v.id, f.part_index`,
		[]any{id}, func(r *sql.Rows) error {
			var vid int64
			var label string
			var f File
			if err := r.Scan(&vid, &label, &f.ID, &f.Path, &f.Size, &f.DurationMS, &f.Container, &f.VideoCodec, &f.AudioCodec,
				&f.HDRFormat, &f.BitrateKbps, &f.Width, &f.Height, &f.DVProfile, &f.PartIndex, &f.Available); err != nil {
				return err
			}
			if !withPaths {
				f.Path = ""
			}
			if n := len(d.Versions); n == 0 || d.Versions[n-1].ID != vid {
				d.Versions = append(d.Versions, Version{ID: vid, Label: label})
			}
			v := &d.Versions[len(d.Versions)-1]
			f.Streams = []Stream{}
			v.Files = append(v.Files, f)
			return nil
		}); err != nil {
		return d, err
	}
	for vi := range d.Versions {
		for fi := range d.Versions[vi].Files {
			f := &d.Versions[vi].Files[fi]
			fileIdx[f.ID] = f
		}
	}
	if len(fileIdx) > 0 {
		ids := make([]any, 0, len(fileIdx))
		for id := range fileIdx {
			ids = append(ids, id)
		}
		in := "?" + strings.Repeat(",?", len(ids)-1)
		if err := eachRow(ctx, s.db, `SELECT id, file_id, kind, codec, COALESCE(profile, ''), COALESCE(language, ''), COALESCE(title, ''),
				is_default, is_forced, is_hearing_impaired, external_path IS NOT NULL, COALESCE(channels, 0), COALESCE(channel_layout, ''),
				COALESCE(sample_rate, 0), COALESCE(bitrate_kbps, 0), COALESCE(width, 0), COALESCE(height, 0), COALESCE(frame_rate, 0),
				COALESCE(bit_depth, 0)
			FROM streams WHERE file_id IN (`+in+`)
			ORDER BY CASE kind WHEN 'video' THEN 0 WHEN 'audio' THEN 1 ELSE 2 END, external_path IS NOT NULL, stream_index, id`,
			ids, func(r *sql.Rows) error {
				var st Stream
				var fileID int64
				if err := r.Scan(&st.ID, &fileID, &st.Kind, &st.Codec, &st.Profile, &st.Language, &st.Title, &st.Default, &st.Forced,
					&st.HearingImpaired, &st.External, &st.Channels, &st.ChannelLayout, &st.SampleRate, &st.BitrateKbps,
					&st.Width, &st.Height, &st.FrameRate, &st.Depth); err != nil {
					return err
				}
				f := fileIdx[fileID]
				f.Streams = append(f.Streams, st)
				return nil
			}); err != nil {
			return d, err
		}
	}

	d.Chapters = []Chapter{}
	if len(d.Versions) > 0 && len(d.Versions[0].Files) > 0 {
		if err := eachRow(ctx, s.db, `SELECT COALESCE(title, ''), start_ms, end_ms FROM markers WHERE file_id = ? AND kind = 'chapter' ORDER BY start_ms`,
			[]any{d.Versions[0].Files[0].ID}, func(r *sql.Rows) error {
				var c Chapter
				err := r.Scan(&c.Title, &c.StartMS, &c.EndMS)
				d.Chapters = append(d.Chapters, c)
				return err
			}); err != nil {
			return d, err
		}
	}
	return d, nil
}

func eachRow(ctx context.Context, db *sql.DB, q string, args []any, fn func(*sql.Rows) error) error {
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := fn(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}
