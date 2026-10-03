package bazarr

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"marquee/internal/scanner"
)

// ErrNotManaged means Bazarr doesn't know the item: it isn't a movie or an episode, or
// Radarr/Sonarr don't manage its file.
var ErrNotManaged = errors.New("Bazarr doesn't manage this item")

// CacheTTL is how long the movie and series lists (used to match files) are kept.
var CacheTTL = 10 * time.Minute

// Service resolves Marquee items to Bazarr's movies and episodes and runs downloads.
type Service struct {
	DB *sql.DB
	// Config returns Bazarr's address and API key ("" when not set up).
	Config func() (url, apiKey string)
	HTTP   *http.Client
	// RetryDelay is how long to wait before looking for Bazarr's new file again.
	RetryDelay time.Duration

	mu       sync.Mutex
	lists    *lists
	episodes map[int64]cachedEpisodes
	wanted   *cachedWanted
	inflight map[string]bool
	wg       sync.WaitGroup
}

type lists struct {
	url     string
	at      time.Time
	movies  map[string]int64 // path tail → Radarr id
	byIMDb  map[string]int64
	paths   map[int64]string // Radarr id → path
	series  []Series
	refresh time.Time // last forced refresh, so a miss doesn't hammer Bazarr
}

type cachedEpisodes struct {
	at   time.Time
	list []Episode
}

type cachedWanted struct {
	url  string
	at   time.Time
	list []WantedItem
}

// Target is an item as Bazarr knows it.
type Target struct {
	ItemID, FileID int64
	Path           string
	Kind           string // movie, episode
	RadarrID       int64
	SeriesID       int64
	EpisodeID      int64
}

// Configured reports whether Bazarr's address and key are set.
func (s *Service) Configured() bool {
	if s == nil || s.Config == nil {
		return false
	}
	u, k := s.Config()
	return u != "" && k != ""
}

func (s *Service) client() *Client {
	u, k := s.Config()
	return &Client{URL: u, APIKey: k, HTTP: s.HTTP}
}

// Tail is the part of a path Bazarr and Marquee share whatever their mounts: the parent
// folder and the file name, lower-cased.
func Tail(p string) string { return tailN(p, 2) }

func tailN(p string, n int) string {
	parts := strings.Split(strings.Trim(strings.ReplaceAll(p, `\`, "/"), "/"), "/")
	if len(parts) > n {
		parts = parts[len(parts)-n:]
	}
	return strings.ToLower(strings.Join(parts, "/"))
}

// loadLists returns the cached movie and series lists, fetching them when stale (or when
// force is set and the last forced refresh was over a minute ago).
func (s *Service) loadLists(ctx context.Context, force bool) (*lists, error) {
	c := s.client()
	s.mu.Lock()
	l := s.lists
	s.mu.Unlock()
	if l != nil && l.url == c.URL && time.Since(l.at) < CacheTTL && (!force || time.Since(l.refresh) < time.Minute) {
		return l, nil
	}
	movies, err := c.Movies(ctx)
	if err != nil {
		return nil, err
	}
	series, err := c.Series(ctx)
	if err != nil {
		return nil, err
	}
	n := &lists{url: c.URL, at: time.Now(), refresh: time.Now(), movies: map[string]int64{}, byIMDb: map[string]int64{}, paths: map[int64]string{}, series: series}
	for _, m := range movies {
		if m.Path != "" {
			n.movies[Tail(m.Path)] = m.RadarrID
			n.paths[m.RadarrID] = m.Path
		}
		if m.IMDbID != "" {
			n.byIMDb[m.IMDbID] = m.RadarrID
		}
	}
	s.mu.Lock()
	s.lists = n
	s.mu.Unlock()
	return n, nil
}

func (s *Service) seriesEpisodes(ctx context.Context, seriesID int64, force bool) ([]Episode, error) {
	s.mu.Lock()
	c, ok := s.episodes[seriesID]
	s.mu.Unlock()
	if ok && time.Since(c.at) < CacheTTL && !force {
		return c.list, nil
	}
	list, err := s.client().EpisodesOf(ctx, seriesID)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	if s.episodes == nil || len(s.episodes) > 500 {
		s.episodes = map[int64]cachedEpisodes{}
	}
	s.episodes[seriesID] = cachedEpisodes{time.Now(), list}
	s.mu.Unlock()
	return list, nil
}

type itemInfo struct {
	typ           string
	season, epNum int
	files         []fileRef
	imdb          string // the movie's, or the episode's show's
	tvdb          int64
}

type fileRef struct {
	id   int64
	path string
}

func (s *Service) item(ctx context.Context, itemID int64) (itemInfo, error) {
	var it itemInfo
	var season, ep sql.NullInt64
	var showID sql.NullInt64
	err := s.DB.QueryRowContext(ctx, `SELECT i.type, p.idx, i.idx, i.grandparent_id FROM items i LEFT JOIN items p ON p.id = i.parent_id
		WHERE i.id = ?`, itemID).Scan(&it.typ, &season, &ep, &showID)
	if err != nil {
		return it, err
	}
	if it.typ != "movie" && it.typ != "episode" {
		return it, ErrNotManaged
	}
	it.season, it.epNum = int(season.Int64), int(ep.Int64)
	rows, err := s.DB.QueryContext(ctx, `SELECT f.id, f.path FROM media_versions v JOIN media_files f ON f.version_id = v.id
		WHERE v.item_id = ? AND f.part_index = 0 ORDER BY f.available DESC, v.id`, itemID)
	if err != nil {
		return it, err
	}
	defer rows.Close()
	for rows.Next() {
		var f fileRef
		if err := rows.Scan(&f.id, &f.path); err != nil {
			return it, err
		}
		it.files = append(it.files, f)
	}
	if err := rows.Err(); err != nil {
		return it, err
	}
	if len(it.files) == 0 {
		return it, ErrNotManaged
	}
	idsOf := itemID
	if it.typ == "episode" {
		idsOf = showID.Int64
	}
	var imdb, tvdb sql.NullString
	if err := s.DB.QueryRowContext(ctx, `SELECT (SELECT value FROM external_ids WHERE item_id = ? AND provider = 'imdb'),
		(SELECT value FROM external_ids WHERE item_id = ? AND provider = 'tvdb')`, idsOf, idsOf).Scan(&imdb, &tvdb); err != nil {
		return it, err
	}
	it.imdb = imdb.String
	it.tvdb, _ = strconv.ParseInt(tvdb.String, 10, 64)
	return it, nil
}

// Resolve finds the Bazarr movie or episode for an item: by the path tail of one of its
// files, else by IMDb id (movies) or TVDB id plus season and episode number (episodes).
func (s *Service) Resolve(ctx context.Context, itemID int64) (Target, error) {
	if !s.Configured() {
		return Target{}, ErrNotConfigured
	}
	it, err := s.item(ctx, itemID)
	if errors.Is(err, sql.ErrNoRows) {
		return Target{}, ErrNotManaged
	}
	if err != nil {
		return Target{}, err
	}
	t := Target{ItemID: itemID, FileID: it.files[0].id, Path: it.files[0].path, Kind: it.typ}
	for _, force := range []bool{false, true} {
		l, err := s.loadLists(ctx, force)
		if err != nil {
			return t, err
		}
		if it.typ == "movie" {
			if s.resolveMovie(l, it, &t) {
				return t, nil
			}
			continue
		}
		ok, err := s.resolveEpisode(ctx, l, it, &t, force)
		if err != nil || ok {
			return t, err
		}
	}
	return t, ErrNotManaged
}

func (s *Service) resolveMovie(l *lists, it itemInfo, t *Target) bool {
	for _, f := range it.files {
		if id, ok := l.movies[Tail(f.path)]; ok {
			t.RadarrID, t.FileID, t.Path = id, f.id, f.path
			return true
		}
	}
	if id, ok := l.byIMDb[it.imdb]; ok && it.imdb != "" {
		t.RadarrID = id
		return true
	}
	return false
}

func (s *Service) resolveEpisode(ctx context.Context, l *lists, it itemInfo, t *Target, force bool) (bool, error) {
	// The show: a series whose folder is the file's parent or grandparent folder.
	var cands []Series
	for _, f := range it.files {
		dir := path.Dir(strings.ReplaceAll(f.path, `\`, "/"))
		for _, d := range []string{path.Base(dir), path.Base(path.Dir(dir))} {
			for _, sr := range l.series {
				if strings.EqualFold(path.Base(strings.TrimRight(strings.ReplaceAll(sr.Path, `\`, "/"), "/")), d) {
					cands = append(cands, sr)
				}
			}
		}
	}
	for _, sr := range l.series {
		if (it.tvdb != 0 && sr.TVDbID == it.tvdb) || (it.imdb != "" && sr.IMDbID == it.imdb) {
			cands = append(cands, sr)
		}
	}
	seen := map[int64]bool{}
	for _, sr := range cands {
		if seen[sr.SeriesID] {
			continue
		}
		seen[sr.SeriesID] = true
		eps, err := s.seriesEpisodes(ctx, sr.SeriesID, force)
		if err != nil {
			return false, err
		}
		for _, f := range it.files {
			for _, e := range eps {
				if e.Path != "" && Tail(e.Path) == Tail(f.path) {
					t.SeriesID, t.EpisodeID, t.FileID, t.Path = sr.SeriesID, e.EpisodeID, f.id, f.path
					return true, nil
				}
			}
		}
		if it.epNum > 0 {
			for _, e := range eps {
				if e.Season == it.season && e.Episode == it.epNum {
					t.SeriesID, t.EpisodeID = sr.SeriesID, e.EpisodeID
					return true, nil
				}
			}
		}
	}
	return false, nil
}

// Languages are what an item has and what its Bazarr language profile still wants.
func (s *Service) Languages(ctx context.Context, t Target) (have, missing []Language, err error) {
	c := s.client()
	if t.Kind == "movie" {
		ms, err := c.Movies(ctx, t.RadarrID)
		if err != nil {
			return nil, nil, err
		}
		for _, m := range ms {
			if m.RadarrID == t.RadarrID {
				return m.Subtitles, m.Missing, nil
			}
		}
		return nil, nil, ErrNotManaged
	}
	es, err := c.Episodes(ctx, t.EpisodeID)
	if err != nil {
		return nil, nil, err
	}
	for _, e := range es {
		if e.EpisodeID == t.EpisodeID {
			return e.Subtitles, e.Missing, nil
		}
	}
	return nil, nil, ErrNotManaged
}

// Search runs a manual provider search, best results first.
func (s *Service) Search(ctx context.Context, t Target) ([]Candidate, error) {
	var res []Candidate
	var err error
	if t.Kind == "movie" {
		res, err = s.client().SearchMovie(ctx, t.RadarrID)
	} else {
		res, err = s.client().SearchEpisode(ctx, t.EpisodeID)
	}
	sort.SliceStable(res, func(i, j int) bool { return res[i].Score > res[j].Score })
	return res, err
}

// Download asks Bazarr for the best subtitle in a language, in the background.
func (s *Service) Download(t Target, o Options) {
	label := fmt.Sprintf("%s forced=%v hi=%v", o.Language, o.Forced, o.HI)
	s.start(t, label, func(ctx context.Context, c *Client) error {
		if t.Kind == "movie" {
			return c.DownloadMovie(ctx, t.RadarrID, o)
		}
		return c.DownloadEpisode(ctx, t.SeriesID, t.EpisodeID, o)
	})
}

// Pick downloads a manual search result in the background.
func (s *Service) Pick(t Target, p Pick) {
	s.start(t, p.Provider+" "+p.Subtitle, func(ctx context.Context, c *Client) error {
		if t.Kind == "movie" {
			return c.PickMovie(ctx, t.RadarrID, p)
		}
		return c.PickEpisode(ctx, t.SeriesID, t.EpisodeID, p)
	})
}

// Wait blocks until background downloads have finished (for tests and shutdown).
func (s *Service) Wait() { s.wg.Wait() }

// start runs a download unless the same one is already running, then attaches the new
// subtitle file to the item's media file.
func (s *Service) start(t Target, label string, fn func(context.Context, *Client) error) {
	key := fmt.Sprintf("%d %s", t.ItemID, label)
	s.mu.Lock()
	if s.inflight == nil {
		s.inflight = map[string]bool{}
	}
	if s.inflight[key] {
		s.mu.Unlock()
		return
	}
	s.inflight[key] = true
	s.mu.Unlock()
	c := s.client()
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer func() {
			s.mu.Lock()
			delete(s.inflight, key)
			s.wanted = nil // what's missing has changed
			s.mu.Unlock()
		}()
		ctx, cancel := context.WithTimeout(context.Background(), LongTimeout+time.Minute)
		defer cancel()
		started := time.Now()
		if err := fn(ctx, c); err != nil {
			slog.Warn("bazarr: subtitle download failed", "item", t.ItemID, "path", t.Path, "request", label, "err", err)
			return
		}
		added := s.attach(ctx, t)
		if added > 0 {
			slog.Info("bazarr: subtitle downloaded", "item", t.ItemID, "path", t.Path, "request", label, "added", added,
				"took", time.Since(started).Round(time.Second))
		} else {
			slog.Info("bazarr: no new subtitle", "item", t.ItemID, "path", t.Path, "request", label)
		}
	}()
}

// attach picks up subtitle files Bazarr saved next to the video. Bazarr has written the
// file by the time it answers, but a slow network share may show it a moment later.
func (s *Service) attach(ctx context.Context, t Target) int {
	delay := s.RetryDelay
	if delay == 0 {
		delay = 3 * time.Second
	}
	for try := 0; ; try++ {
		added, err := scanner.RefreshSubtitles(ctx, s.DB, t.FileID)
		if err != nil {
			slog.Warn("bazarr: refresh subtitles", "item", t.ItemID, "path", t.Path, "err", err)
			return 0
		}
		if added > 0 || try == 2 {
			return added
		}
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return 0
		}
	}
}

// WantedItem is a library movie or episode missing subtitles.
type WantedItem struct {
	ItemID, FileID int64
	Path           string
	Missing        []Language
}

// Wanted maps Bazarr's wanted lists to library files (cached for CacheTTL).
func (s *Service) Wanted(ctx context.Context) ([]WantedItem, error) {
	if !s.Configured() {
		return nil, ErrNotConfigured
	}
	c := s.client()
	s.mu.Lock()
	w := s.wanted
	s.mu.Unlock()
	if w != nil && w.url == c.URL && time.Since(w.at) < CacheTTL {
		return w.list, nil
	}
	l, err := s.loadLists(ctx, false)
	if err != nil {
		return nil, err
	}
	files, err := s.libraryFiles(ctx)
	if err != nil {
		return nil, err
	}
	var out []WantedItem
	movies, err := c.WantedMovies(ctx)
	if err != nil {
		return nil, err
	}
	for _, m := range movies {
		if f, ok := files.find(l.paths[m.RadarrID]); ok && len(m.Missing) > 0 {
			out = append(out, WantedItem{ItemID: f.itemID, FileID: f.fileID, Path: f.path, Missing: m.Missing})
		}
	}
	eps, err := c.WantedEpisodes(ctx)
	if err != nil {
		return nil, err
	}
	missing := map[int64][]Language{}
	var ids []int64
	for _, e := range eps {
		if len(e.Missing) > 0 {
			missing[e.EpisodeID] = e.Missing
			ids = append(ids, e.EpisodeID)
		}
	}
	// The wanted list has no paths: look the episodes up, a batch at a time.
	for len(ids) > 0 {
		n := min(len(ids), 200)
		list, err := c.Episodes(ctx, ids[:n]...)
		if err != nil {
			return nil, err
		}
		ids = ids[n:]
		for _, e := range list {
			if f, ok := files.find(e.Path); ok && missing[e.EpisodeID] != nil {
				out = append(out, WantedItem{ItemID: f.itemID, FileID: f.fileID, Path: f.path, Missing: missing[e.EpisodeID]})
				delete(missing, e.EpisodeID)
			}
		}
	}
	s.mu.Lock()
	s.wanted = &cachedWanted{url: c.URL, at: time.Now(), list: out}
	s.mu.Unlock()
	return out, nil
}

type libFile struct {
	itemID, fileID int64
	path           string
}

type fileIndex map[string][]libFile

// find matches a Bazarr path to a library file by tail, using the folder above when two
// files share a tail (e.g. "Season 01/Episode 1.mkv" in two shows).
func (ix fileIndex) find(p string) (libFile, bool) {
	if p == "" {
		return libFile{}, false
	}
	c := ix[Tail(p)]
	if len(c) == 1 {
		return c[0], true
	}
	for _, f := range c {
		if tailN(f.path, 3) == tailN(p, 3) {
			return f, true
		}
	}
	return libFile{}, false
}

func (s *Service) libraryFiles(ctx context.Context) (fileIndex, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT i.id, f.id, f.path FROM items i
		CROSS JOIN media_versions v ON v.item_id = i.id CROSS JOIN media_files f ON f.version_id = v.id AND f.part_index = 0
		WHERE i.type IN ('movie', 'episode') AND i.extra_type IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ix := fileIndex{}
	for rows.Next() {
		var f libFile
		if err := rows.Scan(&f.itemID, &f.fileID, &f.path); err != nil {
			return nil, err
		}
		ix[Tail(f.path)] = append(ix[Tail(f.path)], f)
	}
	return ix, rows.Err()
}

// Describe names a list of languages for people: "English, French (forced)".
func Describe(list []Language) string {
	var parts []string
	for _, l := range list {
		n := l.Name
		if n == "" {
			n = l.Code2
		}
		switch {
		case bool(l.Forced):
			n += " (forced)"
		case bool(l.HI):
			n += " (SDH)"
		}
		parts = append(parts, n)
	}
	return strings.Join(parts, ", ")
}
