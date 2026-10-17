package vodsave

import (
	"context"
	"slices"
	"strconv"
	"sync"
	"time"

	"marquee/internal/scanner/naming"
)

// "In library" marks on the films and series on demand, for everyone: which titles the
// library already has, matched as the save comparisons match (TMDB id first, else the
// normalised title and year), worked out for every title at once and kept for a while so
// the lists, which page through tens of thousands of titles, never look items up one by one.

// LibraryIndex is, for each film and series on demand, the libraries that have it.
type LibraryIndex struct {
	movies, series map[int64][]int64 // VOD id → library ids (sorted, distinct)
	recordings     map[int64]bool    // libraries that hold Live TV recordings
	built          time.Time
}

// Visibility says which libraries a user can see: nil LibraryIDs = all; HideRecordings
// leaves out the Live TV recordings libraries.
type Visibility struct {
	LibraryIDs     []int64
	HideRecordings bool
}

func (v Visibility) sees(idx *LibraryIndex, lib int64) bool {
	if v.HideRecordings && idx.recordings[lib] {
		return false
	}
	return v.LibraryIDs == nil || slices.Contains(v.LibraryIDs, lib)
}

func (idx *LibraryIndex) table(series bool) map[int64][]int64 {
	if series {
		return idx.series
	}
	return idx.movies
}

// Has reports whether a film (or series) is in a library the user can see.
func (idx *LibraryIndex) Has(series bool, id int64, v Visibility) bool {
	if idx == nil {
		return false
	}
	for _, lib := range idx.table(series)[id] {
		if v.sees(idx, lib) {
			return true
		}
	}
	return false
}

// IDs are the films (or series) in a library the user can see.
func (idx *LibraryIndex) IDs(series bool, v Visibility) []int64 {
	if idx == nil {
		return nil
	}
	out := []int64{}
	for id := range idx.table(series) {
		if idx.Has(series, id, v) {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return out
}

// libraryIndexTTL is how long an index is used before it's rebuilt (in the background,
// the old one serving meanwhile).
const libraryIndexTTL = 2 * time.Minute

type indexCache struct {
	mu       sync.Mutex
	idx      *LibraryIndex
	building bool
}

func (s *Service) indexCache() *indexCache { return &s.inLibrary }

// InLibrary is the current index: built now the first time, then rebuilt in the
// background once it's older than a couple of minutes.
func (s *Service) InLibrary(ctx context.Context) (*LibraryIndex, error) {
	c := s.indexCache()
	c.mu.Lock()
	idx := c.idx
	if idx != nil {
		if time.Since(idx.built) > libraryIndexTTL && !c.building {
			c.building = true
			go func() {
				fresh, err := s.buildLibraryIndex(context.Background())
				c.mu.Lock()
				if err == nil {
					c.idx = fresh
				}
				c.building = false
				c.mu.Unlock()
			}()
		}
		c.mu.Unlock()
		return idx, nil
	}
	defer c.mu.Unlock()
	idx, err := s.buildLibraryIndex(ctx)
	if err != nil {
		return nil, err
	}
	c.idx = idx
	return idx, nil
}

// ForgetLibraryIndex drops the index so the next use rebuilds it (tests, and after a save).
func (s *Service) ForgetLibraryIndex() {
	c := s.indexCache()
	c.mu.Lock()
	c.idx = nil
	c.mu.Unlock()
}

type libTitle struct {
	lib  int64
	tmdb string
}

// libSide is the library's films or shows: by TMDB id, and by normalised title and year.
type libSide struct {
	byTMDB  map[string][]int64
	byTitle map[string][]libTitle
}

func titleKey(norm string, year int) string { return norm + "|" + strconv.Itoa(year) }

func (s *Service) loadLibSide(ctx context.Context, typ, hasFiles string) (libSide, error) {
	side := libSide{byTMDB: map[string][]int64{}, byTitle: map[string][]libTitle{}}
	rows, err := s.DB.QueryContext(ctx, `SELECT i.library_id, i.title, COALESCE(i.original_title,''), COALESCE(i.year,0),
		COALESCE((SELECT x.value FROM external_ids x WHERE x.item_id=i.id AND x.provider='tmdb' LIMIT 1),'')
		FROM items i WHERE i.type=? AND i.extra_type IS NULL AND `+hasFiles, typ)
	if err != nil {
		return side, err
	}
	defer rows.Close()
	for rows.Next() {
		var lib int64
		var title, orig, tmdb string
		var year int
		if err := rows.Scan(&lib, &title, &orig, &year, &tmdb); err != nil {
			return side, err
		}
		if tmdb != "" {
			side.byTMDB[tmdb] = append(side.byTMDB[tmdb], lib)
		}
		if year <= 0 {
			continue
		}
		seen := ""
		for _, t := range []string{title, orig} {
			if n := naming.Normalize(t); n != "" && n != seen {
				k := titleKey(n, year)
				side.byTitle[k] = append(side.byTitle[k], libTitle{lib, tmdb})
				seen = n
			}
		}
	}
	return side, rows.Err()
}

// match is the libraries that have a title, as candidates() finds them.
func (side libSide) match(tmdbID int, title string, year int) []int64 {
	if tmdbID > 0 {
		if libs := side.byTMDB[strconv.Itoa(tmdbID)]; len(libs) > 0 {
			return distinct(libs)
		}
	}
	name, year := libraryTitle(title, year)
	want := naming.Normalize(name)
	if year <= 0 || want == "" {
		return nil
	}
	var libs []int64
	for _, c := range side.byTitle[titleKey(want, year)] {
		if c.tmdb != "" && tmdbID > 0 && c.tmdb != strconv.Itoa(tmdbID) {
			continue // another film or show of the same name
		}
		libs = append(libs, c.lib)
	}
	return distinct(libs)
}

func distinct(ids []int64) []int64 {
	if len(ids) == 0 {
		return nil
	}
	out := slices.Clone(ids)
	slices.Sort(out)
	return slices.Compact(out)
}

func (s *Service) matchVOD(ctx context.Context, table string, side libSide) (map[int64][]int64, error) {
	out := map[int64][]int64{}
	if len(side.byTMDB) == 0 && len(side.byTitle) == 0 {
		return out, nil
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT id, title, COALESCE(year,0), COALESCE(tmdb_id,0), match_state='matched' FROM `+table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var title string
		var year, tmdbID int
		var matched bool
		if err := rows.Scan(&id, &title, &year, &tmdbID, &matched); err != nil {
			return nil, err
		}
		if !matched {
			tmdbID = 0 // the source's guess isn't trusted until TMDB confirmed it
		}
		if libs := side.match(tmdbID, title, year); len(libs) > 0 {
			out[id] = libs
		}
	}
	return out, rows.Err()
}

func (s *Service) buildLibraryIndex(ctx context.Context) (*LibraryIndex, error) {
	idx := &LibraryIndex{recordings: map[int64]bool{}, built: time.Now()}
	movies, err := s.loadLibSide(ctx, "movie", `EXISTS (SELECT 1 FROM media_versions v JOIN media_files f ON f.version_id=v.id
		WHERE v.item_id=i.id AND f.available=1)`)
	if err != nil {
		return nil, err
	}
	shows, err := s.loadLibSide(ctx, "show", `EXISTS (SELECT 1 FROM items e JOIN media_versions v ON v.item_id=e.id
		JOIN media_files f ON f.version_id=v.id WHERE e.grandparent_id=i.id AND e.type='episode' AND f.available=1)`)
	if err != nil {
		return nil, err
	}
	if idx.movies, err = s.matchVOD(ctx, "vod_movies", movies); err != nil {
		return nil, err
	}
	if idx.series, err = s.matchVOD(ctx, "vod_series", shows); err != nil {
		return nil, err
	}
	rows, err := s.DB.QueryContext(ctx, "SELECT id FROM libraries WHERE recordings=1")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		idx.recordings[id] = true
	}
	return idx, rows.Err()
}
