// Package library manages library definitions (name, type, folders, options).
// Scanning and metadata live in their own packages from M1.
package library

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

var ErrNotFound = errors.New("library not found")

type Type string

const (
	Movies Type = "movies"
	Shows  Type = "shows"
	Anime  Type = "anime"
	Music  Type = "music"
	Videos Type = "videos"
	Photos Type = "photos"
)

func (t Type) Valid() bool {
	switch t {
	case Movies, Shows, Anime, Music, Videos, Photos:
		return true
	}
	return false
}

// Options mirrors the API LibraryOptions; nil fields mean "use the default".
type Options struct {
	Language          *string   `json:"language,omitempty"`
	EpisodeOrdering   *string   `json:"episodeOrdering,omitempty"`
	IgnorePatterns    *[]string `json:"ignorePatterns,omitempty"`
	IncludeInHome     *bool     `json:"includeInHome,omitempty"`
	IncludeInSearch   *bool     `json:"includeInSearch,omitempty"`
	ScanIntervalHours *int      `json:"scanIntervalHours,omitempty"`
}

// WithDefaults fills unset options with type-appropriate defaults.
func (o Options) WithDefaults(t Type, metadataLanguage string) Options {
	str := func(s string) *string { return &s }
	b := func(v bool) *bool { return &v }
	if o.Language == nil {
		o.Language = str(metadataLanguage)
	}
	if o.EpisodeOrdering == nil && (t == Shows || t == Anime) {
		o.EpisodeOrdering = str("aired")
	}
	if o.IgnorePatterns == nil {
		o.IgnorePatterns = &[]string{}
	}
	if o.IncludeInHome == nil {
		o.IncludeInHome = b(true)
	}
	if o.IncludeInSearch == nil {
		o.IncludeInSearch = b(true)
	}
	if o.ScanIntervalHours == nil {
		h := 24
		o.ScanIntervalHours = &h
	}
	return o
}

type Library struct {
	ID            int64
	Name          string
	Type          Type
	Paths         []string
	Options       Options
	ItemCount     int
	LastScannedAt *time.Time
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

func (s *Store) List(ctx context.Context) ([]Library, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id FROM libraries ORDER BY sort_order, name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	libs := make([]Library, 0, len(ids))
	for _, id := range ids {
		l, err := s.Get(ctx, id)
		if err != nil {
			return nil, err
		}
		libs = append(libs, l)
	}
	return libs, nil
}

func (s *Store) Get(ctx context.Context, id int64) (Library, error) {
	var l Library
	var opts, created, updated string
	var scanned sql.NullString
	err := s.db.QueryRowContext(ctx, `
		SELECT id, name, type, options, last_scanned_at, created_at, updated_at,
		       (SELECT COUNT(*) FROM items i WHERE i.library_id = l.id AND i.parent_id IS NULL AND i.extra_type IS NULL)
		FROM libraries l WHERE id = ?`, id).
		Scan(&l.ID, &l.Name, &l.Type, &opts, &scanned, &created, &updated, &l.ItemCount)
	if errors.Is(err, sql.ErrNoRows) {
		return Library{}, ErrNotFound
	}
	if err != nil {
		return Library{}, err
	}
	if err := json.Unmarshal([]byte(opts), &l.Options); err != nil {
		return Library{}, fmt.Errorf("decode library options: %w", err)
	}
	l.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	l.UpdatedAt, _ = time.Parse(time.RFC3339Nano, updated)
	if scanned.Valid {
		t, _ := time.Parse(time.RFC3339Nano, scanned.String)
		l.LastScannedAt = &t
	}
	rows, err := s.db.QueryContext(ctx, `SELECT path FROM library_paths WHERE library_id = ? ORDER BY path`, id)
	if err != nil {
		return Library{}, err
	}
	defer rows.Close()
	l.Paths = []string{}
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err != nil {
			return Library{}, err
		}
		l.Paths = append(l.Paths, p)
	}
	return l, rows.Err()
}

func (s *Store) Create(ctx context.Context, name string, t Type, paths []string, opts Options) (Library, error) {
	if !t.Valid() {
		return Library{}, &ValidationError{"unknown library type"}
	}
	if err := validateName(name); err != nil {
		return Library{}, err
	}
	paths, err := validatePaths(paths)
	if err != nil {
		return Library{}, err
	}
	if err := validateOptions(t, opts); err != nil {
		return Library{}, err
	}
	rawOpts, _ := json.Marshal(opts)
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Library{}, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `INSERT INTO libraries(name, type, options, sort_order)
		VALUES (?, ?, ?, (SELECT COALESCE(MAX(sort_order), 0) + 1 FROM libraries))`, name, t, string(rawOpts))
	if err != nil {
		return Library{}, err
	}
	id, _ := res.LastInsertId()
	if err := insertPaths(ctx, tx, id, paths); err != nil {
		return Library{}, err
	}
	if err := tx.Commit(); err != nil {
		return Library{}, err
	}
	return s.Get(ctx, id)
}

// Update changes the name, paths and/or options. Nil arguments are left unchanged.
// Options are merged field by field so clients can send only what changed.
func (s *Store) Update(ctx context.Context, id int64, name *string, paths *[]string, opts *Options) (Library, error) {
	cur, err := s.Get(ctx, id)
	if err != nil {
		return Library{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return Library{}, err
	}
	defer tx.Rollback()
	if name != nil {
		if err := validateName(*name); err != nil {
			return Library{}, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE libraries SET name = ? WHERE id = ?`, *name, id); err != nil {
			return Library{}, err
		}
	}
	if paths != nil {
		clean, err := validatePaths(*paths)
		if err != nil {
			return Library{}, err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM library_paths WHERE library_id = ?`, id); err != nil {
			return Library{}, err
		}
		if err := insertPaths(ctx, tx, id, clean); err != nil {
			return Library{}, err
		}
	}
	if opts != nil {
		merged := mergeOptions(cur.Options, *opts)
		if err := validateOptions(cur.Type, merged); err != nil {
			return Library{}, err
		}
		raw, _ := json.Marshal(merged)
		if _, err := tx.ExecContext(ctx, `UPDATE libraries SET options = ? WHERE id = ?`, string(raw), id); err != nil {
			return Library{}, err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE libraries SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = ?`, id); err != nil {
		return Library{}, err
	}
	if err := tx.Commit(); err != nil {
		return Library{}, err
	}
	return s.Get(ctx, id)
}

// Delete removes the library and all its metadata (cascades). Media files are never touched.
func (s *Store) Delete(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM libraries WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func insertPaths(ctx context.Context, tx *sql.Tx, id int64, paths []string) error {
	for _, p := range paths {
		if _, err := tx.ExecContext(ctx, `INSERT INTO library_paths(library_id, path) VALUES (?, ?)`, id, p); err != nil {
			return err
		}
	}
	return nil
}

func validateName(name string) error {
	if l := len([]rune(name)); l == 0 || l > 64 {
		return &ValidationError{"library name must be 1–64 characters"}
	}
	return nil
}

func validatePaths(paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, &ValidationError{"at least one folder is required"}
	}
	seen := map[string]bool{}
	var out []string
	for _, p := range paths {
		if !filepath.IsAbs(p) {
			return nil, &ValidationError{fmt.Sprintf("folder %q must be an absolute path", p)}
		}
		p = filepath.Clean(p)
		fi, err := os.Stat(p)
		if err != nil || !fi.IsDir() {
			return nil, &ValidationError{fmt.Sprintf("folder %q does not exist or is not a directory", p)}
		}
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out, nil
}

func validateOptions(t Type, o Options) error {
	if o.EpisodeOrdering != nil {
		if t != Shows && t != Anime {
			return &ValidationError{"episode ordering only applies to TV and anime libraries"}
		}
		switch *o.EpisodeOrdering {
		case "aired", "absolute", "dvd":
		default:
			return &ValidationError{"episode ordering must be aired, absolute or dvd"}
		}
	}
	if o.ScanIntervalHours != nil && *o.ScanIntervalHours < 0 {
		return &ValidationError{"scan interval cannot be negative"}
	}
	if o.IgnorePatterns != nil {
		for _, p := range *o.IgnorePatterns {
			if _, err := filepath.Match(p, ""); err != nil {
				return &ValidationError{fmt.Sprintf("invalid ignore pattern %q", p)}
			}
		}
	}
	return nil
}

func mergeOptions(cur, patch Options) Options {
	if patch.Language != nil {
		cur.Language = patch.Language
	}
	if patch.EpisodeOrdering != nil {
		cur.EpisodeOrdering = patch.EpisodeOrdering
	}
	if patch.IgnorePatterns != nil {
		cur.IgnorePatterns = patch.IgnorePatterns
	}
	if patch.IncludeInHome != nil {
		cur.IncludeInHome = patch.IncludeInHome
	}
	if patch.IncludeInSearch != nil {
		cur.IncludeInSearch = patch.IncludeInSearch
	}
	if patch.ScanIntervalHours != nil {
		cur.ScanIntervalHours = patch.ScanIntervalHours
	}
	return cur
}
