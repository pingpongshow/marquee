package vod

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"marquee/internal/metadata/tmdb"
	"marquee/internal/settings"
)

// TV series on demand from Xtream Codes sources (LIVE-6). Each refresh lists the series
// (one request for the categories, one for every series); a series' seasons and episodes
// are fetched from the server when someone opens it, and again once EpisodesMaxAge old.
// An episode is a file at {base}/series/{user}/{pass}/{id}.{ext}, played like a film.

// EpisodesMaxAge is how long a series' episodes are kept before they're fetched again.
const EpisodesMaxAge = 12 * time.Hour

// TVMatcher looks up series on TMDB by the id the source gives (metadata.Service). A
// Matcher that is also a TVMatcher adds TMDB's details to series.
type TVMatcher interface {
	TVByID(ctx context.Context, id int) (*tmdb.TVShow, error)
	TVSeason(ctx context.Context, showID, season int) (*tmdb.Season, error)
}

func (s *Service) tv() TVMatcher {
	t, _ := s.Matcher.(TVMatcher)
	return t
}

// ---- Xtream Codes API

type xtreamSeries struct {
	ID           flexString `json:"series_id"`
	Name         flexString `json:"name"`
	Title        flexString `json:"title"`
	Cover        flexString `json:"cover"`
	Plot         flexString `json:"plot"`
	Cast         flexString `json:"cast"`
	Director     flexString `json:"director"`
	Genre        flexString `json:"genre"`
	ReleaseDate  flexString `json:"releaseDate"`
	ReleaseDate2 flexString `json:"release_date"`
	Year         flexString `json:"year"`
	Rating       flexString `json:"rating"`
	Backdrop     flexList   `json:"backdrop_path"`
	TMDB         flexString `json:"tmdb"`
	TMDBID       flexString `json:"tmdb_id"`
	Category     flexString `json:"category_id"`
	LastModified flexString `json:"last_modified"`
}

type xtreamSeason struct {
	Number   flexString `json:"season_number"`
	Name     flexString `json:"name"`
	Cover    flexString `json:"cover"`
	CoverBig flexString `json:"cover_big"`
	Overview flexString `json:"overview"`
	AirDate  flexString `json:"air_date"`
}

type xtreamEpisode struct {
	ID        flexString `json:"id"`
	Episode   flexString `json:"episode_num"`
	Title     flexString `json:"title"`
	Extension flexString `json:"container_extension"`
	Season    flexString `json:"season"`
	Added     flexString `json:"added"`
	Info      json.RawMessage
}

func (e *xtreamEpisode) UnmarshalJSON(b []byte) error {
	type plain xtreamEpisode
	var p struct {
		plain
		Info json.RawMessage `json:"info"`
	}
	if err := json.Unmarshal(b, &p); err != nil {
		return err
	}
	*e = xtreamEpisode(p.plain)
	e.Info = p.Info
	return nil
}

type xtreamEpisodeInfo struct {
	Plot         flexString `json:"plot"`
	DurationSecs flexString `json:"duration_secs"`
	Duration     flexString `json:"duration"`
	Image        flexString `json:"movie_image"`
	ReleaseDate  flexString `json:"releasedate"`
	ReleaseDate2 flexString `json:"release_date"`
	AirDate      flexString `json:"air_date"`
	Rating       flexString `json:"rating"`
}

type xtreamSeriesInfo struct {
	Info     xtreamSeries
	Seasons  []xtreamSeason
	Episodes []xtreamEpisode
}

func (x *xtream) seriesCategories(ctx context.Context) (map[string]string, error) {
	var list []xtreamCategory
	if err := x.call(ctx, "get_series_categories", nil, &list); err != nil {
		return nil, err
	}
	out := make(map[string]string, len(list))
	for _, c := range list {
		out[c.ID.String()] = c.Name.String()
	}
	return out, nil
}

func (x *xtream) seriesList(ctx context.Context) ([]xtreamSeries, error) {
	var raw json.RawMessage
	if err := x.call(ctx, "get_series", nil, &raw); err != nil {
		return nil, err
	}
	var list []xtreamSeries
	if b := bytes.TrimSpace(raw); len(b) > 0 && b[0] == '[' { // none can come as {} or null
		if err := json.Unmarshal(b, &list); err != nil {
			return nil, fmt.Errorf("the server's series weren't understood: %v", x.redactErr(err))
		}
	}
	return list, nil
}

// seriesInfo is a series' details, seasons and episodes. Episodes come as an object of
// lists by season ("1": [...]) or as one list; seasons as a list or an object.
func (x *xtream) seriesInfo(ctx context.Context, id string) (xtreamSeriesInfo, error) {
	var r struct {
		Info     json.RawMessage `json:"info"`
		Seasons  json.RawMessage `json:"seasons"`
		Episodes json.RawMessage `json:"episodes"`
	}
	var out xtreamSeriesInfo
	if err := x.call(ctx, "get_series_info", url.Values{"series_id": {id}}, &r); err != nil {
		return out, err
	}
	bad := func(err error) error {
		return fmt.Errorf("the server's episodes weren't understood: %v", x.redactErr(err))
	}
	if b := bytes.TrimSpace(r.Info); len(b) > 0 && b[0] == '{' {
		json.Unmarshal(b, &out.Info) // details are a bonus; the episodes matter
	}
	switch b := bytes.TrimSpace(r.Seasons); {
	case len(b) > 0 && b[0] == '[':
		json.Unmarshal(b, &out.Seasons)
	case len(b) > 0 && b[0] == '{':
		var m map[string]xtreamSeason
		json.Unmarshal(b, &m)
		for k, s := range m {
			if s.Number.String() == "" {
				s.Number = flexString(k)
			}
			out.Seasons = append(out.Seasons, s)
		}
	}
	switch b := bytes.TrimSpace(r.Episodes); {
	case len(b) > 0 && b[0] == '[':
		if err := json.Unmarshal(b, &out.Episodes); err != nil {
			return out, bad(err)
		}
	case len(b) > 0 && b[0] == '{':
		var m map[string][]xtreamEpisode
		if err := json.Unmarshal(b, &m); err != nil {
			return out, bad(err)
		}
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			for _, e := range m[k] {
				if e.Season.String() == "" {
					e.Season = flexString(k)
				}
				out.Episodes = append(out.Episodes, e)
			}
		}
	}
	return out, nil
}

// seriesStreamURL is an episode's link (it holds the password: never log or store it).
func (x *xtream) seriesStreamURL(file string) string {
	return x.base + "/series/" + url.PathEscape(x.user) + "/" + url.PathEscape(x.pass) + "/" + file
}

// ---- refresh

type xtreamShow struct {
	key, title, category, logo, backdrop, overview, director, lastModified, added string
	genres                                                                        []string
	cast                                                                          []CastMember
	year                                                                          int
	rating                                                                        float64
	tmdb                                                                          int64
}

func splitList(s string, max int) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" && (max <= 0 || len(out) < max) {
			out = append(out, p)
		}
	}
	return out
}

func yearOf(dates ...string) int {
	for _, d := range dates {
		if len(d) >= 4 {
			if y, err := strconv.Atoi(d[:4]); err == nil && y > 1880 && y < 2200 {
				return y
			}
		}
	}
	return 0
}

func (x *xtream) show(in xtreamSeries, cats map[string]string) (xtreamShow, bool) {
	id := in.ID.Int()
	title := or(in.Name.String(), in.Title.String())
	if id <= 0 || title == "" {
		return xtreamShow{}, false
	}
	sh := xtreamShow{key: "xtream:" + strconv.FormatInt(id, 10), title: title, category: cats[in.Category.String()],
		logo: x.absolute(in.Cover.String()), overview: in.Plot.String(), director: in.Director.String(),
		genres: splitList(in.Genre.String(), 0), lastModified: in.LastModified.String()}
	for _, c := range splitList(in.Cast.String(), 12) {
		sh.cast = append(sh.cast, CastMember{Name: c})
	}
	if len(in.Backdrop) > 0 {
		sh.backdrop = x.absolute(in.Backdrop[0])
	}
	if y := int(in.Year.Int()); y > 1880 && y < 2200 {
		sh.year = y
	} else {
		sh.year = yearOf(in.ReleaseDate.String(), in.ReleaseDate2.String())
	}
	if sh.year == 0 {
		if m := titleYear.FindStringSubmatch(title); m != nil {
			sh.year, _ = strconv.Atoi(m[2])
		}
	}
	if r := in.Rating.Float(); r > 0 && r <= 10 {
		sh.rating = r
	}
	sh.tmdb = in.TMDB.Int()
	if sh.tmdb <= 0 {
		sh.tmdb = in.TMDBID.Int()
	}
	if n := in.LastModified.Int(); n > 946684800 {
		sh.added = time.Unix(n, 0).UTC().Format(time.RFC3339)
	}
	return sh, true
}

func jsonList[T any](v []T) string {
	if len(v) == 0 {
		return "[]"
	}
	b, _ := json.Marshal(v)
	return string(b)
}

// refreshXtreamSeries lists the account's series into the source, in one transaction.
// Series gone from the server go, with their episodes and everyone's progress in them.
func (s *Service) refreshXtreamSeries(ctx context.Context, src settings.VODSource) (int, error) {
	x := s.xtream(src)
	x.client = s.listClient()
	cats, err := x.seriesCategories(ctx)
	if err != nil {
		return 0, err
	}
	list, err := x.seriesList(ctx)
	if err != nil {
		return 0, err
	}
	shows := make([]xtreamShow, 0, len(list))
	seen := make(map[string]bool, len(list))
	for _, in := range list {
		sh, ok := x.show(in, cats)
		if !ok || seen[sh.key] {
			continue
		}
		seen[sh.key] = true
		shows = append(shows, sh)
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	existing := map[string]int64{}
	rows, err := tx.QueryContext(ctx, "SELECT id, source_key FROM vod_series WHERE source_id=?", src.ID)
	if err != nil {
		return 0, err
	}
	for rows.Next() {
		var id int64
		var k string
		rows.Scan(&id, &k)
		existing[k] = id
	}
	rows.Close()
	// An empty list from a server that listed series before is taken as a glitch (some send
	// {} or null when overloaded), not as every series gone: removing them would take
	// everyone's progress in them with them.
	if len(shows) == 0 && len(existing) > 0 {
		return 0, fmt.Errorf("the server lists no series (keeping the %d listed before)", len(existing))
	}
	// Series renumbered by the source keep their rows and follows (see carry.go).
	listing := make([]listed, len(shows))
	for i, sh := range shows {
		listing[i] = listed{key: sh.key, title: sh.title, category: sh.category, year: sh.year, tmdb: sh.tmdb}
	}
	if _, err := carrySeries(ctx, tx, src.ID, existing, listing); err != nil {
		return 0, err
	}
	// The source's details stand until TMDB's replace them (matched). A changed TMDB id is
	// looked up again; a changed last_modified fetches the episodes again when next opened.
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO vod_series(source_id, source_key, title, category, logo_url, year,
		overview, genres, rating, director, cast_json, backdrop_url, tmdb_id, match_state, last_modified, added_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(source_id, source_key) DO UPDATE SET title=excluded.title, category=excluded.category,
		logo_url=excluded.logo_url, last_modified=excluded.last_modified,
		episodes_at=CASE WHEN vod_series.last_modified<>excluded.last_modified THEN NULL ELSE vod_series.episodes_at END,
		year=CASE WHEN vod_series.match_state='matched' THEN vod_series.year ELSE excluded.year END,
		overview=CASE WHEN vod_series.match_state='matched' THEN vod_series.overview ELSE excluded.overview END,
		genres=CASE WHEN vod_series.match_state='matched' THEN vod_series.genres ELSE excluded.genres END,
		rating=CASE WHEN vod_series.match_state='matched' THEN vod_series.rating ELSE excluded.rating END,
		director=CASE WHEN vod_series.match_state='matched' THEN vod_series.director ELSE excluded.director END,
		cast_json=CASE WHEN vod_series.match_state='matched' THEN vod_series.cast_json ELSE excluded.cast_json END,
		backdrop_url=CASE WHEN vod_series.match_state='matched' THEN vod_series.backdrop_url ELSE excluded.backdrop_url END,
		match_state=CASE
			WHEN excluded.tmdb_id IS NULL THEN CASE WHEN vod_series.match_state='matched' THEN 'matched' ELSE 'none' END
			WHEN vod_series.tmdb_id IS excluded.tmdb_id THEN vod_series.match_state
			ELSE 'pending' END,
		tmdb_id=COALESCE(excluded.tmdb_id, vod_series.tmdb_id)`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()
	added := now()
	for _, sh := range shows {
		var year, rating, tmdbID any
		state := "none"
		if sh.year > 0 {
			year = sh.year
		}
		if sh.rating > 0 {
			rating = sh.rating
		}
		if sh.tmdb > 0 {
			tmdbID, state = sh.tmdb, "pending"
		}
		if _, err := stmt.ExecContext(ctx, src.ID, sh.key, sh.title, sh.category, sh.logo, year, sh.overview,
			jsonList(sh.genres), rating, sh.director, jsonList(sh.cast), sh.backdrop, tmdbID, state, sh.lastModified,
			or(sh.added, added)); err != nil {
			return 0, err
		}
	}
	for k, id := range existing {
		if !seen[k] {
			if _, err := tx.ExecContext(ctx, "DELETE FROM vod_series WHERE id=?", id); err != nil {
				return 0, err
			}
		}
	}
	return len(shows), tx.Commit()
}

// matchSeries adds TMDB's details to series the source named by TMDB id. Series without
// one keep the source's details (no guessing by title).
func (s *Service) matchSeries(ctx context.Context, retryFailed bool) {
	tv := s.tv()
	if tv == nil {
		return
	}
	q := "SELECT id, tmdb_id FROM vod_series WHERE tmdb_id IS NOT NULL AND match_state='pending'"
	if retryFailed {
		q = "SELECT id, tmdb_id FROM vod_series WHERE tmdb_id IS NOT NULL AND match_state IN ('pending','failed')"
	}
	rows, err := s.DB.QueryContext(ctx, q+" ORDER BY id")
	if err != nil {
		return
	}
	type todo struct{ id, tmdb int64 }
	var list []todo
	for rows.Next() {
		var t todo
		rows.Scan(&t.id, &t.tmdb)
		list = append(list, t)
	}
	rows.Close()
	matched := 0
	for _, t := range list {
		if ctx.Err() != nil || !s.Enabled() {
			return
		}
		show, err := tv.TVByID(ctx, int(t.tmdb))
		if errors.Is(err, tmdb.ErrNoKey) || errors.Is(err, tmdb.ErrInvalidKey) {
			return // series stay pending until a key is set
		}
		if err != nil || show == nil || show.ID == 0 {
			slog.Debug("series by TMDB id", "tmdb", t.tmdb, "err", err)
			s.DB.ExecContext(ctx, "UPDATE vod_series SET match_state='failed' WHERE id=?", t.id)
			continue
		}
		if err := s.applyTMDBTV(ctx, t.id, show); err != nil {
			slog.Warn("series match", "tmdb", t.tmdb, "err", err)
			continue
		}
		matched++
	}
	if len(list) > 0 {
		slog.Info("series matched to TMDB", "matched", matched, "tried", len(list))
	}
}

// applyTMDBTV stores TMDB's details for a series; the source's title stays.
func (s *Service) applyTMDBTV(ctx context.Context, id int64, t *tmdb.TVShow) error {
	var year any
	if y := yearOf(t.FirstAirDate); y > 0 {
		year = y
	}
	genres := make([]string, 0, len(t.Genres))
	for _, g := range t.Genres {
		genres = append(genres, g.Name)
	}
	cast := make([]CastMember, 0, 12)
	for _, c := range t.Credits.Cast {
		if len(cast) == 12 {
			break
		}
		cast = append(cast, CastMember{Name: c.Name, Character: c.Character})
	}
	poster, backdrop := "", ""
	if t.PosterPath != "" {
		poster = tmdbImages + "w500" + t.PosterPath
	}
	if t.BackdropPath != "" {
		backdrop = tmdbImages + "w1280" + t.BackdropPath
	}
	var rating any
	if t.VoteAverage > 0 {
		rating = t.VoteAverage
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE vod_series SET match_state='matched', year=COALESCE(?, year),
		overview=CASE WHEN ?<>'' THEN ? ELSE overview END, genres=CASE WHEN ?<>'[]' THEN ? ELSE genres END,
		content_rating=CASE WHEN ?<>'' THEN ? ELSE content_rating END, rating=COALESCE(?, rating),
		cast_json=CASE WHEN ?<>'[]' THEN ? ELSE cast_json END,
		poster_url=CASE WHEN ?<>'' THEN ? ELSE poster_url END,
		backdrop_url=CASE WHEN ?<>'' THEN ? ELSE backdrop_url END, tmdb_recs=?, keywords=?, votes=? WHERE id=?`,
		year, t.Overview, t.Overview, jsonList(genres), jsonList(genres), t.Certification("US"), t.Certification("US"), rating,
		jsonList(cast), jsonList(cast), poster, poster, backdrop, backdrop, jsonList(t.Recommendations.IDs()), jsonList(t.KeywordNames()), t.VoteCount, id)
	return err
}

// ---- episodes, fetched when a series is opened

var episodeLocks sync.Map // series id -> *sync.Mutex

// sxe is a "Show - S01E02 - " prefix many servers put before an episode's name.
var sxe = regexp.MustCompile(`(?i)^.*?\bS\d{1,3}\s*E\d{1,4}\b\s*[-:–|]*\s*`)

func episodeTitle(raw string, season, episode int) string {
	raw = strings.TrimSpace(raw)
	if t := strings.TrimSpace(sxe.ReplaceAllString(raw, "")); t != "" {
		return t
	}
	if raw != "" && !sxe.MatchString(raw) {
		return raw
	}
	return fmt.Sprintf("Episode %d", episode)
}

func runtimeOf(secs, hhmmss flexString) int64 {
	if n := secs.Int(); n > 0 {
		return n * 1000
	}
	if m := hms.FindStringSubmatch(hhmmss.String()); m != nil {
		h, _ := strconv.Atoi(m[1])
		mi, _ := strconv.Atoi(m[2])
		sec, _ := strconv.Atoi(m[3])
		return int64(h*3600+mi*60+sec) * 1000
	}
	return 0
}

// LoadEpisodes fetches a series' seasons and episodes from its server unless they were
// fetched within EpisodesMaxAge (or force). With some already kept, a failure leaves
// them and returns nil; with none, it returns the (redacted) error.
func (s *Service) LoadEpisodes(ctx context.Context, seriesID int64, force bool) error {
	l, _ := episodeLocks.LoadOrStore(seriesID, &sync.Mutex{})
	mu := l.(*sync.Mutex)
	mu.Lock()
	defer mu.Unlock()
	var srcID, key string
	var at sql.NullString
	var have int
	if err := s.DB.QueryRowContext(ctx, `SELECT source_id, source_key, episodes_at,
		(SELECT COUNT(*) FROM vod_episodes WHERE series_id=vod_series.id) FROM vod_series WHERE id=?`, seriesID).
		Scan(&srcID, &key, &at, &have); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrNotFound
		}
		return err
	}
	if t, err := time.Parse(time.RFC3339, at.String); !force && at.Valid && err == nil && time.Since(t) < EpisodesMaxAge {
		return nil
	}
	src, ok := s.source(srcID)
	if !ok || !src.Xtream() || !strings.HasPrefix(key, "xtream:") {
		return nil
	}
	fctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	x := s.xtream(src)
	info, err := x.seriesInfo(fctx, strings.TrimPrefix(key, "xtream:"))
	if err == nil {
		err = s.storeEpisodes(ctx, x, seriesID, info)
	}
	if err != nil {
		slog.Warn("series episodes", "source", src.Name, "err", err)
		if have > 0 {
			return nil
		}
		return err
	}
	if tv := s.tv(); tv != nil {
		go s.episodesFromTMDB(context.WithoutCancel(ctx), tv, seriesID)
	}
	if s.OnUpdated != nil {
		s.OnUpdated()
	}
	return nil
}

// episodeExt is an episode's file extension (mp4 unless the source names a safe one).
func episodeExt(e xtreamEpisode) string {
	ext := strings.ToLower(strings.TrimPrefix(e.Extension.String(), "."))
	if !safeFileExt.MatchString(ext) {
		ext = "mp4"
	}
	return ext
}

func (s *Service) storeEpisodes(ctx context.Context, x *xtream, seriesID int64, info xtreamSeriesInfo) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	existing := map[string]int64{}
	rows, err := tx.QueryContext(ctx, "SELECT id, source_key FROM vod_episodes WHERE series_id=?", seriesID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id int64
		var k string
		rows.Scan(&id, &k)
		existing[k] = id
	}
	rows.Close()
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO vod_episodes(series_id, source_key, season, episode, title, overview,
		still_url, air_date, runtime_ms, rating, stream_url) VALUES(?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(series_id, source_key) DO UPDATE SET season=excluded.season, episode=excluded.episode,
		stream_url=excluded.stream_url, rating=COALESCE(excluded.rating, vod_episodes.rating),
		runtime_ms=CASE WHEN excluded.runtime_ms>0 THEN excluded.runtime_ms ELSE vod_episodes.runtime_ms END,
		title=CASE WHEN vod_episodes.tmdb THEN vod_episodes.title ELSE excluded.title END,
		overview=CASE WHEN vod_episodes.tmdb AND vod_episodes.overview<>'' THEN vod_episodes.overview ELSE excluded.overview END,
		still_url=CASE WHEN vod_episodes.tmdb AND vod_episodes.still_url<>'' THEN vod_episodes.still_url ELSE excluded.still_url END,
		air_date=CASE WHEN excluded.air_date<>'' THEN excluded.air_date ELSE vod_episodes.air_date END`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	// No episodes where there were some is a glitch, not every episode gone (that would take
	// everyone's progress in them): keep them.
	if len(existing) > 0 && !anyEpisode(info.Episodes) {
		return errors.New("the server gave no episodes for this series")
	}
	// Episodes renumbered by the source keep their rows, by season and number (see carry.go).
	var list []listedEpisode
	for _, e := range info.Episodes {
		if id := e.ID.Int(); id > 0 {
			k := strconv.FormatInt(id, 10)
			list = append(list, listedEpisode{key: k, file: k + "." + episodeExt(e), season: int(e.Season.Int()), episode: int(e.Episode.Int())})
		}
	}
	if _, err := carryEpisodes(ctx, tx, seriesID, existing, list); err != nil {
		return err
	}
	seen := map[string]bool{}
	seasons := map[int]bool{}
	quality := map[string]Quality{} // what the server says the files are
	for _, e := range info.Episodes {
		id := e.ID.Int()
		if id <= 0 {
			continue
		}
		k := strconv.FormatInt(id, 10)
		if seen[k] {
			continue
		}
		seen[k] = true
		season, num := int(e.Season.Int()), int(e.Episode.Int())
		seasons[season] = true
		ext := episodeExt(e)
		var ei xtreamEpisodeInfo
		if b := bytes.TrimSpace(e.Info); len(b) > 0 && b[0] == '{' {
			json.Unmarshal(b, &ei)
		}
		if q, ok := qualityFromInfo(e.Info); ok {
			quality[k] = q
		}
		var rating any
		if r := ei.Rating.Float(); r > 0 && r <= 10 {
			rating = r
		}
		air := or(ei.AirDate.String(), or(ei.ReleaseDate.String(), ei.ReleaseDate2.String()))
		if len(air) > 10 {
			air = air[:10]
		}
		if _, err := stmt.ExecContext(ctx, seriesID, k, season, num, episodeTitle(e.Title.String(), season, num),
			ei.Plot.String(), x.absolute(ei.Image.String()), air, runtimeOf(ei.DurationSecs, ei.Duration), rating,
			k+"."+ext); err != nil {
			return err
		}
	}
	for k, id := range existing {
		if !seen[k] {
			if _, err := tx.ExecContext(ctx, "DELETE FROM vod_episodes WHERE id=?", id); err != nil {
				return err
			}
		}
	}
	if len(quality) > 0 {
		ids := map[string]int64{}
		rows, err := tx.QueryContext(ctx, "SELECT id, source_key FROM vod_episodes WHERE series_id=?", seriesID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id int64
			var k string
			rows.Scan(&id, &k)
			ids[k] = id
		}
		rows.Close()
		for k, q := range quality {
			if id := ids[k]; id > 0 {
				if err := s.storeQuality(ctx, tx, "episode_id", id, q); err != nil {
					return err
				}
			}
		}
	}
	// Seasons: the server's list where it gives one, else those the episodes name.
	if _, err := tx.ExecContext(ctx, "DELETE FROM vod_seasons WHERE series_id=?", seriesID); err != nil {
		return err
	}
	for _, sn := range info.Seasons {
		n := int(sn.Number.Int())
		if !seasons[n] {
			continue // a season with no episodes here
		}
		delete(seasons, n)
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO vod_seasons(series_id, season, name, overview, poster_url, air_date)
			VALUES(?,?,?,?,?,?)`, seriesID, n, sn.Name.String(), sn.Overview.String(),
			x.absolute(or(sn.CoverBig.String(), sn.Cover.String())), sn.AirDate.String()); err != nil {
			return err
		}
	}
	for n := range seasons {
		if _, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO vod_seasons(series_id, season) VALUES(?,?)", seriesID, n); err != nil {
			return err
		}
	}
	// The details the list didn't have (some servers only send them here).
	in := info.Info
	bd := ""
	if len(in.Backdrop) > 0 {
		bd = x.absolute(in.Backdrop[0])
	}
	if _, err := tx.ExecContext(ctx, `UPDATE vod_series SET episodes_at=?,
		overview=CASE WHEN overview='' THEN ? ELSE overview END,
		backdrop_url=CASE WHEN backdrop_url='' THEN ? ELSE backdrop_url END,
		director=CASE WHEN director='' THEN ? ELSE director END WHERE id=?`,
		now(), in.Plot.String(), bd, in.Director.String(), seriesID); err != nil {
		return err
	}
	return tx.Commit()
}

// anyEpisode reports whether a server's episodes include one that can be stored.
func anyEpisode(list []xtreamEpisode) bool {
	for _, e := range list {
		if e.ID.Int() > 0 {
			return true
		}
	}
	return false
}

// episodesFromTMDB adds TMDB's names, overviews and stills to a matched series' episodes,
// one request per season that still needs them.
func (s *Service) episodesFromTMDB(ctx context.Context, tv TVMatcher, seriesID int64) {
	var showID int64
	if err := s.DB.QueryRowContext(ctx, "SELECT tmdb_id FROM vod_series WHERE id=? AND match_state='matched'", seriesID).
		Scan(&showID); err != nil || showID <= 0 {
		return
	}
	rows, err := s.DB.QueryContext(ctx, "SELECT DISTINCT season FROM vod_episodes WHERE series_id=? AND tmdb=0 ORDER BY season", seriesID)
	if err != nil {
		return
	}
	var seasons []int
	for rows.Next() {
		var n int
		rows.Scan(&n)
		seasons = append(seasons, n)
	}
	rows.Close()
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	for _, n := range seasons {
		se, err := tv.TVSeason(ctx, int(showID), n)
		if err != nil || se == nil {
			slog.Debug("series season from TMDB", "tmdb", showID, "season", n, "err", err)
			continue
		}
		poster := ""
		if se.PosterPath != "" {
			poster = tmdbImages + "w500" + se.PosterPath
		}
		s.DB.ExecContext(ctx, `UPDATE vod_seasons SET name=CASE WHEN ?<>'' THEN ? ELSE name END,
			overview=CASE WHEN ?<>'' THEN ? ELSE overview END, poster_url=CASE WHEN ?<>'' THEN ? ELSE poster_url END
			WHERE series_id=? AND season=?`, se.Name, se.Name, se.Overview, se.Overview, poster, poster, seriesID, n)
		for _, e := range se.Episodes {
			still := ""
			if e.StillPath != "" {
				still = tmdbImages + "w780" + e.StillPath
			}
			s.DB.ExecContext(ctx, `UPDATE vod_episodes SET tmdb=1, title=CASE WHEN ?<>'' THEN ? ELSE title END,
				overview=CASE WHEN ?<>'' THEN ? ELSE overview END, still_url=CASE WHEN ?<>'' THEN ? ELSE still_url END,
				air_date=CASE WHEN air_date='' THEN ? ELSE air_date END,
				runtime_ms=CASE WHEN runtime_ms=0 THEN ? ELSE runtime_ms END
				WHERE series_id=? AND season=? AND episode=?`, e.Name, e.Name, e.Overview, e.Overview, still, still,
				e.AirDate, int64(e.Runtime)*60000, seriesID, n, e.EpisodeNumber)
		}
	}
}

// ---- browsing

type Series struct {
	ID            int64
	Title         string
	Category      string
	Year          int
	Overview      string
	Genres        []string
	ContentRating string
	Rating        float64
	Director      string
	Cast          []CastMember
	HasPoster     bool
	HasBackdrop   bool
	TMDBID        int
	Matched       bool
	SourceID      string
	AddedAt       time.Time
	// Known once the episodes were fetched.
	Episodes, Watched, Seasons int
	EpisodesFetched            bool
}

type Season struct {
	Number          int
	Name, Overview  string
	AirDate         string
	HasPoster       bool
	Episodes        int
	WatchedEpisodes int
}

type Episode struct {
	ID, SeriesID    int64
	SeriesTitle     string
	Season, Episode int
	Title, Overview string
	AirDate         string
	RuntimeMS       int64
	Rating          float64
	HasStill        bool
	PositionMS      int64
	Watched         bool
	StreamURL       string
	SourceID        string
	ContentRating   string
}

// seriesCols reads m (vod_series) with the user's counts; the first argument is the user.
const seriesCols = `m.id, m.title, m.category, COALESCE(m.year,0), m.overview, m.genres, m.content_rating,
	COALESCE(m.rating,0), m.director, m.cast_json, (m.poster_url<>'' OR m.logo_url<>''), m.backdrop_url<>'',
	COALESCE(m.tmdb_id,0), m.match_state='matched', m.source_id, m.added_at, m.episodes_at IS NOT NULL,
	(SELECT COUNT(*) FROM vod_episodes e WHERE e.series_id=m.id),
	(SELECT COUNT(*) FROM vod_episode_progress p WHERE p.series_id=m.id AND p.user_id=? AND p.watched=1),
	(SELECT COUNT(DISTINCT e.season) FROM vod_episodes e WHERE e.series_id=m.id)`

func scanSeries(sc interface{ Scan(...any) error }) (Series, error) {
	var m Series
	var genres, cast, added string
	err := sc.Scan(&m.ID, &m.Title, &m.Category, &m.Year, &m.Overview, &genres, &m.ContentRating, &m.Rating,
		&m.Director, &cast, &m.HasPoster, &m.HasBackdrop, &m.TMDBID, &m.Matched, &m.SourceID, &added,
		&m.EpisodesFetched, &m.Episodes, &m.Watched, &m.Seasons)
	if err != nil {
		return m, err
	}
	json.Unmarshal([]byte(genres), &m.Genres)
	json.Unmarshal([]byte(cast), &m.Cast)
	m.AddedAt, _ = time.Parse(time.RFC3339, added)
	return m, nil
}

// AllWatched reports whether the user watched every episode known.
func (m Series) AllWatched() bool { return m.Episodes > 0 && m.Watched >= m.Episodes }

func likeArg(q string) string {
	return "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(q) + "%"
}

// SeriesList lists series with the user's counts, and how many match in total.
func (s *Service) SeriesList(ctx context.Context, f Filter) ([]Series, int, error) {
	where, args := f.where(true)
	var total int
	if err := s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM vod_series m WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	order := "m.title COLLATE NOCASE"
	switch f.Sort {
	case "added":
		order = "m.added_at DESC, m.id DESC"
	case "year":
		order = "m.year IS NULL, m.year DESC, m.title COLLATE NOCASE"
	case "rating":
		order = "m.rating IS NULL, m.rating DESC, m.title COLLATE NOCASE"
	}
	limit := f.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := s.DB.QueryContext(ctx, "SELECT "+seriesCols+" FROM vod_series m WHERE "+where+" ORDER BY "+order+" LIMIT ? OFFSET ?",
		append(append([]any{f.UserID}, args...), limit, max(f.Offset, 0))...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []Series
	for rows.Next() {
		m, err := scanSeries(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, m)
	}
	return out, total, rows.Err()
}

// SeriesCategories are the sources' series categories with how many series each holds.
func (s *Service) SeriesCategories(ctx context.Context, maxRating string) ([]Category, error) {
	where, args := ratingClause(maxRating)
	rows, err := s.DB.QueryContext(ctx, "SELECT m.category, COUNT(*) FROM vod_series m WHERE m.category<>'' AND "+where+
		" GROUP BY m.category ORDER BY m.category COLLATE NOCASE", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Category
	for rows.Next() {
		var c Category
		rows.Scan(&c.Name, &c.Count)
		out = append(out, c)
	}
	return out, rows.Err()
}

// OneSeries returns a series with the user's counts.
func (s *Service) OneSeries(ctx context.Context, id, userID int64) (Series, error) {
	m, err := scanSeries(s.DB.QueryRowContext(ctx, "SELECT "+seriesCols+" FROM vod_series m WHERE m.id=?", userID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return m, ErrNotFound
	}
	return m, err
}

// Seasons lists a series' seasons with the user's watched counts (specials, season 0, last).
func (s *Service) Seasons(ctx context.Context, seriesID, userID int64) ([]Season, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT z.season, z.name, z.overview, z.air_date, z.poster_url<>'',
		(SELECT COUNT(*) FROM vod_episodes e WHERE e.series_id=z.series_id AND e.season=z.season),
		(SELECT COUNT(*) FROM vod_episodes e JOIN vod_episode_progress p ON p.episode_id=e.id AND p.user_id=? AND p.watched=1
			WHERE e.series_id=z.series_id AND e.season=z.season)
		FROM vod_seasons z WHERE z.series_id=? ORDER BY z.season=0, z.season`, userID, seriesID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Season
	for rows.Next() {
		var z Season
		if err := rows.Scan(&z.Number, &z.Name, &z.Overview, &z.AirDate, &z.HasPoster, &z.Episodes, &z.WatchedEpisodes); err != nil {
			return nil, err
		}
		if z.Name == "" {
			z.Name = "Season " + strconv.Itoa(z.Number)
			if z.Number == 0 {
				z.Name = "Specials"
			}
		}
		out = append(out, z)
	}
	return out, rows.Err()
}

const episodeCols = `e.id, e.series_id, m.title, e.season, e.episode, e.title, e.overview, e.air_date, e.runtime_ms,
	COALESCE(e.rating,0), e.still_url<>'', COALESCE(p.position_ms,0), COALESCE(p.watched,0), e.stream_url, m.source_id,
	m.content_rating`

// episodeFrom joins an episode to its series and the user's progress (the first argument).
const episodeFrom = ` FROM vod_episodes e JOIN vod_series m ON m.id=e.series_id
	LEFT JOIN vod_episode_progress p ON p.episode_id=e.id AND p.user_id=?`

// episodeOrder is viewing order: seasons in turn, specials (season 0) last.
const episodeOrder = " ORDER BY e.season=0, e.season, e.episode, e.id"

func scanEpisode(sc interface{ Scan(...any) error }) (Episode, error) {
	var e Episode
	err := sc.Scan(&e.ID, &e.SeriesID, &e.SeriesTitle, &e.Season, &e.Episode, &e.Title, &e.Overview, &e.AirDate,
		&e.RuntimeMS, &e.Rating, &e.HasStill, &e.PositionMS, &e.Watched, &e.StreamURL, &e.SourceID, &e.ContentRating)
	return e, err
}

func (s *Service) episodes(ctx context.Context, q string, args ...any) ([]Episode, error) {
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Episode
	for rows.Next() {
		e, err := scanEpisode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Episodes lists a series' episodes in viewing order with the user's progress.
func (s *Service) Episodes(ctx context.Context, seriesID, userID int64) ([]Episode, error) {
	return s.episodes(ctx, "SELECT "+episodeCols+episodeFrom+" WHERE e.series_id=?"+episodeOrder, userID, seriesID)
}

// OneEpisode returns an episode with the user's progress.
func (s *Service) OneEpisode(ctx context.Context, id, userID int64) (Episode, error) {
	e, err := scanEpisode(s.DB.QueryRowContext(ctx, "SELECT "+episodeCols+episodeFrom+" WHERE e.id=?", userID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return e, ErrNotFound
	}
	return e, err
}

// NextUp is the episode the user would watch next: the one they're part-way through
// (the most recent), else the first unwatched after the last they finished, else the
// first unwatched. nil when they've seen every episode.
func (s *Service) NextUp(ctx context.Context, seriesID, userID int64) (*Episode, error) {
	list, err := s.Episodes(ctx, seriesID, userID)
	if err != nil || len(list) == 0 {
		return nil, err
	}
	var lastID int64
	var lastWatched bool
	err = s.DB.QueryRowContext(ctx, `SELECT episode_id, watched FROM vod_episode_progress WHERE user_id=? AND series_id=?
		ORDER BY updated_at DESC, watched ASC LIMIT 1`, userID, seriesID).Scan(&lastID, &lastWatched)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	for i, e := range list {
		if e.ID != lastID {
			continue
		}
		if !lastWatched && e.PositionMS > 0 {
			return &list[i], nil
		}
		for j := i + 1; j < len(list); j++ {
			if !list[j].Watched {
				return &list[j], nil
			}
		}
		break
	}
	for i, e := range list {
		if !e.Watched {
			return &list[i], nil
		}
	}
	return nil, nil
}

// NextEpisode is the episode after this one in viewing order (for autoplay), or nil.
func (s *Service) NextEpisode(ctx context.Context, episodeID, userID int64) (*Episode, error) {
	cur, err := s.OneEpisode(ctx, episodeID, userID)
	if err != nil {
		return nil, err
	}
	list, err := s.Episodes(ctx, cur.SeriesID, userID)
	if err != nil {
		return nil, err
	}
	for i, e := range list {
		if e.ID == episodeID && i+1 < len(list) {
			return &list[i+1], nil
		}
	}
	return nil, nil
}

// UpNext is a series with the episode the user would watch next.
type UpNext struct {
	Series  Series
	Episode Episode
}

// SeriesContinueWatching is the series the user is watching, most recent first, each with
// its next episode; series they've finished are left out.
func (s *Service) SeriesContinueWatching(ctx context.Context, userID int64, maxRating string) ([]UpNext, error) {
	where, args := ratingClause(maxRating)
	rows, err := s.DB.QueryContext(ctx, "SELECT "+seriesCols+` FROM vod_series m
		JOIN (SELECT series_id, user_id, MAX(updated_at) AS at FROM vod_episode_progress WHERE user_id=? GROUP BY series_id) r ON r.series_id=m.id
		WHERE `+where+`
		AND NOT EXISTS (SELECT 1 FROM vod_continue_hidden h WHERE h.user_id=r.user_id AND h.series_id=m.id AND h.hidden_at>=r.at)
		ORDER BY r.at DESC LIMIT 40`, append([]any{userID, userID}, args...)...)
	if err != nil {
		return nil, err
	}
	var list []Series
	for rows.Next() {
		m, err := scanSeries(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		list = append(list, m)
	}
	rows.Close()
	var out []UpNext
	for _, m := range list {
		e, err := s.NextUp(ctx, m.ID, userID)
		if err != nil {
			return nil, err
		}
		if e != nil {
			out = append(out, UpNext{Series: m, Episode: *e})
		}
		if len(out) == 20 {
			break
		}
	}
	return out, nil
}

// ---- progress

// SetEpisodeProgress records where the user is in an episode; past the watched threshold
// it counts as watched.
func (s *Service) SetEpisodeProgress(ctx context.Context, userID, episodeID, positionMS, durationMS int64) error {
	pct := int64(s.Settings.Get().Library.WatchedThresholdPercent)
	if pct <= 0 || pct > 100 {
		pct = 90
	}
	if durationMS > 0 && positionMS*100 >= durationMS*pct {
		return s.SetEpisodeWatched(ctx, userID, episodeID, true)
	}
	if positionMS < 10_000 {
		return nil
	}
	_, err := s.DB.ExecContext(ctx, `INSERT INTO vod_episode_progress(user_id, episode_id, series_id, position_ms, updated_at)
		SELECT ?, id, series_id, ?, ? FROM vod_episodes WHERE id=?
		ON CONFLICT(user_id, episode_id) DO UPDATE SET position_ms=excluded.position_ms, watched=0, updated_at=excluded.updated_at`,
		userID, positionMS, now(), episodeID)
	return err
}

// SetEpisodeWatched marks an episode watched, or unwatched (forgetting its progress).
func (s *Service) SetEpisodeWatched(ctx context.Context, userID, episodeID int64, watched bool) error {
	var err error
	if !watched {
		_, err = s.DB.ExecContext(ctx, "DELETE FROM vod_episode_progress WHERE user_id=? AND episode_id=?", userID, episodeID)
	} else {
		_, err = s.DB.ExecContext(ctx, `INSERT INTO vod_episode_progress(user_id, episode_id, series_id, position_ms, watched, updated_at)
			SELECT ?, id, series_id, 0, 1, ? FROM vod_episodes WHERE id=?
			ON CONFLICT(user_id, episode_id) DO UPDATE SET position_ms=0, watched=1, updated_at=excluded.updated_at`,
			userID, now(), episodeID)
	}
	if err == nil {
		s.remember(ctx, func() error {
			var series int64
			if err := s.DB.QueryRowContext(ctx, "SELECT series_id FROM vod_episodes WHERE id=?", episodeID).Scan(&series); err != nil {
				return err
			}
			return s.rememberSeries(ctx, userID, series)
		})
	}
	return err
}

// SetSeriesWatched marks every known episode of a series (or of one season) watched or not.
func (s *Service) SetSeriesWatched(ctx context.Context, userID, seriesID int64, season *int, watched bool) error {
	cond, args := "series_id=?", []any{seriesID}
	if season != nil {
		cond += " AND season=?"
		args = append(args, *season)
	}
	if !watched {
		_, err := s.DB.ExecContext(ctx, "DELETE FROM vod_episode_progress WHERE user_id=? AND episode_id IN (SELECT id FROM vod_episodes WHERE "+
			cond+")", append([]any{userID}, args...)...)
		if err == nil {
			s.remember(ctx, func() error { return s.rememberSeries(ctx, userID, seriesID) })
		}
		return err
	}
	// Later episodes get later times, so the last one counts as the most recently finished.
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	rows, err := tx.QueryContext(ctx, "SELECT e.id FROM vod_episodes e WHERE "+strings.ReplaceAll(cond, "series_id", "e.series_id")+
		episodeOrder, args...)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()
	base := time.Now().UTC().Add(-time.Duration(len(ids)) * time.Second)
	for i, id := range ids {
		if _, err := tx.ExecContext(ctx, `INSERT INTO vod_episode_progress(user_id, episode_id, series_id, position_ms, watched, updated_at)
			VALUES(?,?,?,0,1,?) ON CONFLICT(user_id, episode_id) DO UPDATE SET position_ms=0, watched=1, updated_at=excluded.updated_at`,
			userID, id, seriesID, base.Add(time.Duration(i)*time.Second).Format(time.RFC3339)); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.remember(ctx, func() error { return s.rememberSeries(ctx, userID, seriesID) })
	return nil
}

// EpisodePlayLink is the link an episode streams from (it holds the password: never
// store or log it), and the user agent the source asks for.
func (s *Service) EpisodePlayLink(e Episode) (link, userAgent string, ok bool) {
	src, found := s.source(e.SourceID)
	if !found || !src.Xtream() {
		return "", "", false
	}
	return s.xtream(src).seriesStreamURL(e.StreamURL), src.UserAgent, true
}

// SeriesByID loads series with the user's counts, by id (missing ones are left out).
func (s *Service) SeriesByID(ctx context.Context, ids []int64, userID int64) (map[int64]Series, error) {
	out := map[int64]Series{}
	if len(ids) == 0 {
		return out, nil
	}
	b, _ := json.Marshal(ids)
	rows, err := s.DB.QueryContext(ctx, "SELECT "+seriesCols+" FROM vod_series m WHERE m.id IN (SELECT value FROM json_each(?))", userID, string(b))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		m, err := scanSeries(rows)
		if err != nil {
			return nil, err
		}
		out[m.ID] = m
	}
	return out, rows.Err()
}
