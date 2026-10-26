package vod

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"marquee/internal/livetv"
	"marquee/internal/settings"
)

// Xtream Codes sources (e.g. Dispatcharr): the VOD categories and movies of an account,
// read from player_api.php. A film is a plain file at {base}/movie/{user}/{pass}/{id}.{ext},
// which the server turns into HLS when someone watches it (see Player).
//
// The password is in every request, so it never reaches the database (films keep only
// "{id}.{ext}"; the link is built when playing), logs or error messages (redact).

type xtream struct {
	base, user, pass, ua string
	client               *http.Client
}

func (s *Service) xtream(src settings.VODSource) *xtream {
	return &xtream{base: strings.TrimRight(src.URL, "/"), user: src.Username, pass: src.Password, ua: src.UserAgent, client: s.client()}
}

// redact removes the password (as written and as escaped in links) from a message.
func (x *xtream) redact(msg string) string {
	if x.pass == "" {
		return msg
	}
	for _, p := range []string{x.pass, url.QueryEscape(x.pass), url.PathEscape(x.pass)} {
		if p != "" {
			msg = strings.ReplaceAll(msg, p, "***")
		}
	}
	return msg
}

func (x *xtream) redactErr(err error) error {
	if err == nil {
		return nil
	}
	return errors.New(x.redact(err.Error()))
}

// streamURL is a film's link (it holds the password: never log or store it).
func (x *xtream) streamURL(file string) string {
	return x.base + "/movie/" + url.PathEscape(x.user) + "/" + url.PathEscape(x.pass) + "/" + file
}

// call fetches player_api.php with an action (none = the account) and decodes the answer.
func (x *xtream) call(ctx context.Context, action string, q url.Values, out any) error {
	if q == nil {
		q = url.Values{}
	}
	q.Set("username", x.user)
	q.Set("password", x.pass)
	if action != "" {
		q.Set("action", action)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, x.base+"/player_api.php?"+q.Encode(), nil)
	if err != nil {
		return x.redactErr(err)
	}
	if x.ua != "" {
		req.Header.Set("User-Agent", x.ua)
	}
	resp, err := x.client.Do(req)
	if err != nil {
		return x.redactErr(err)
	}
	defer resp.Body.Close()
	switch {
	// Dispatcharr answers 404 for a username it doesn't have (an exact, case-sensitive
	// match) and 401 for a wrong password or a user with no XC password.
	case resp.StatusCode == http.StatusNotFound:
		return errors.New("the server doesn't know that username (check the address too; Dispatcharr usernames are case-sensitive)")
	case resp.StatusCode == http.StatusUnauthorized:
		return errors.New("wrong password, or the user has no XC password set in Dispatcharr")
	case resp.StatusCode == http.StatusForbidden:
		return errors.New("the server refused the username or password")
	case resp.StatusCode >= 300:
		return fmt.Errorf("the server answered %s", resp.Status)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 512<<20)).Decode(out); err != nil {
		return fmt.Errorf("the server's answer to %s wasn't understood: %v", or(action, "sign-in"), x.redactErr(err))
	}
	return nil
}

func or(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// ---- lenient values: providers send numbers as strings, "" for missing, null…

type flexString string

func (f *flexString) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	switch {
	case len(b) == 0 || string(b) == "null":
		*f = ""
	case b[0] == '"':
		var s string
		if err := json.Unmarshal(b, &s); err != nil {
			return err
		}
		*f = flexString(s)
	case b[0] == '[' || b[0] == '{':
		*f = "" // not a value
	default:
		*f = flexString(b) // a number or boolean as written
	}
	return nil
}

func (f flexString) String() string { return strings.TrimSpace(string(f)) }

func (f flexString) Int() int64 {
	s := f.String()
	if n, err := strconv.ParseInt(s, 10, 64); err == nil {
		return n
	}
	if v, err := strconv.ParseFloat(s, 64); err == nil {
		return int64(v)
	}
	return 0
}

func (f flexString) Float() float64 {
	v, _ := strconv.ParseFloat(strings.ReplaceAll(f.String(), ",", "."), 64)
	return v
}

// flexList is a list of strings sent as an array or as one string.
type flexList []string

func (f *flexList) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) > 0 && b[0] == '[' {
		var raw []flexString
		if err := json.Unmarshal(b, &raw); err != nil {
			return nil
		}
		*f = nil
		for _, r := range raw {
			if r.String() != "" {
				*f = append(*f, r.String())
			}
		}
		return nil
	}
	var s flexString
	if err := s.UnmarshalJSON(b); err != nil {
		return nil
	}
	*f = nil
	if s.String() != "" {
		*f = flexList{s.String()}
	}
	return nil
}

// ---- API shapes

type xtreamAccount struct {
	UserInfo struct {
		Auth   flexString `json:"auth"`
		Status flexString `json:"status"`
		Exp    flexString `json:"exp_date"`
	} `json:"user_info"`
}

type xtreamCategory struct {
	ID   flexString `json:"category_id"`
	Name flexString `json:"category_name"`
}

type xtreamStream struct {
	ID        flexString `json:"stream_id"`
	Name      flexString `json:"name"`
	Title     flexString `json:"title"`
	Icon      flexString `json:"stream_icon"`
	Rating    flexString `json:"rating"`
	Added     flexString `json:"added"`
	Category  flexString `json:"category_id"`
	Extension flexString `json:"container_extension"`
	TMDB      flexString `json:"tmdb"`
	TMDBID    flexString `json:"tmdb_id"`
	Year      flexString `json:"year"`
}

type xtreamInfo struct {
	Plot         flexString `json:"plot"`
	Description  flexString `json:"description"`
	Cast         flexString `json:"cast"`
	Actors       flexString `json:"actors"`
	Director     flexString `json:"director"`
	Genre        flexString `json:"genre"`
	ReleaseDate  flexString `json:"releasedate"`
	ReleaseDate2 flexString `json:"release_date"`
	DurationSecs flexString `json:"duration_secs"`
	Duration     flexString `json:"duration"`
	Backdrop     flexList   `json:"backdrop_path"`
	Cover        flexString `json:"movie_image"`
	CoverBig     flexString `json:"cover_big"`
	TMDBID       flexString `json:"tmdb_id"`
	MPAA         flexString `json:"mpaa_rating"`
}

// account signs in and returns the account's status and expiry (zero = none).
func (x *xtream) account(ctx context.Context) (string, time.Time, error) {
	var a xtreamAccount
	if err := x.call(ctx, "", nil, &a); err != nil {
		return "", time.Time{}, err
	}
	if a.UserInfo.Auth.Int() != 1 {
		return "", time.Time{}, errors.New("the server refused the username or password")
	}
	var exp time.Time
	if n := a.UserInfo.Exp.Int(); n > 0 {
		exp = time.Unix(n, 0).UTC()
	}
	return a.UserInfo.Status.String(), exp, nil
}

func (x *xtream) categories(ctx context.Context) (map[string]string, error) {
	var list []xtreamCategory
	if err := x.call(ctx, "get_vod_categories", nil, &list); err != nil {
		return nil, err
	}
	out := make(map[string]string, len(list))
	for _, c := range list {
		out[c.ID.String()] = c.Name.String()
	}
	return out, nil
}

func (x *xtream) streams(ctx context.Context) ([]xtreamStream, error) {
	var list []xtreamStream
	err := x.call(ctx, "get_vod_streams", nil, &list)
	return list, err
}

// rawInfo is a film's "info" as the server sends it.
func (x *xtream) rawInfo(ctx context.Context, id string) (json.RawMessage, error) {
	var r struct {
		Info json.RawMessage `json:"info"`
	}
	if err := x.call(ctx, "get_vod_info", url.Values{"vod_id": {id}}, &r); err != nil {
		return nil, err
	}
	return r.Info, nil
}

func (x *xtream) info(ctx context.Context, id string) (xtreamInfo, error) {
	var out xtreamInfo
	raw, err := x.rawInfo(ctx, id)
	if err != nil {
		return out, err
	}
	if b := bytes.TrimSpace(raw); len(b) > 0 && b[0] == '{' { // some send [] when there's nothing
		if err := json.Unmarshal(b, &out); err != nil {
			return out, fmt.Errorf("the server's details weren't understood: %v", x.redactErr(err))
		}
	}
	return out, nil
}

// absolute resolves a link the server gave relative to itself.
func (x *xtream) absolute(link string) string {
	if link == "" || strings.HasPrefix(link, "http://") || strings.HasPrefix(link, "https://") {
		return link
	}
	b, err := url.Parse(x.base + "/")
	if err != nil {
		return ""
	}
	u, err := b.Parse(link)
	if err != nil {
		return ""
	}
	return u.String()
}

var safeFileExt = regexp.MustCompile(`^[A-Za-z0-9]{1,8}$`)

// xtreamFilm is one listed film, ready to store.
type xtreamFilm struct {
	key, title, category, file, logo, added string
	year                                    int
	rating                                  float64
	tmdb                                    int64
}

func (x *xtream) film(st xtreamStream, cats map[string]string) (xtreamFilm, bool) {
	id := st.ID.Int()
	title := or(st.Name.String(), st.Title.String())
	if id <= 0 || title == "" {
		return xtreamFilm{}, false
	}
	ext := strings.TrimPrefix(st.Extension.String(), ".")
	if !safeFileExt.MatchString(ext) {
		ext = "mp4"
	}
	f := xtreamFilm{key: "xtream:" + strconv.FormatInt(id, 10), title: title, category: cats[st.Category.String()],
		file: strconv.FormatInt(id, 10) + "." + strings.ToLower(ext), logo: x.absolute(st.Icon.String())}
	if y := int(st.Year.Int()); y > 1880 && y < 2200 {
		f.year = y
	} else if m := titleYear.FindStringSubmatch(title); m != nil {
		f.year, _ = strconv.Atoi(m[2])
	}
	if r := st.Rating.Float(); r > 0 && r <= 10 {
		f.rating = r
	}
	f.tmdb = st.TMDB.Int()
	if f.tmdb <= 0 {
		f.tmdb = st.TMDBID.Int()
	}
	if n := st.Added.Int(); n > 946684800 { // a Unix time after 2000
		f.added = time.Unix(n, 0).UTC().Format(time.RFC3339)
	}
	return f, true
}

// refreshXtream lists the account's films into the source. One request for the categories
// and one for every film; details come later (Details), so tens of thousands are quick.
func (s *Service) refreshXtream(ctx context.Context, src settings.VODSource) (int, error) {
	x := s.xtream(src)
	x.client = s.listClient()
	cats, err := x.categories(ctx)
	if err != nil {
		return 0, err
	}
	list, err := x.streams(ctx)
	if err != nil {
		return 0, err
	}
	films := make([]xtreamFilm, 0, len(list))
	seen := make(map[string]bool, len(list))
	for _, st := range list {
		f, ok := x.film(st, cats)
		if !ok || seen[f.key] {
			continue
		}
		seen[f.key] = true
		films = append(films, f)
	}
	if len(films) == 0 {
		return 0, errors.New("the server lists no movies")
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	existing := map[string]int64{}
	rows, err := tx.QueryContext(ctx, "SELECT id, source_key FROM vod_movies WHERE source_id=?", src.ID)
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
	// Films renumbered by the source keep their rows (see carry.go).
	listing := make([]listed, len(films))
	for i, f := range films {
		listing[i] = listed{key: f.key, title: f.title, category: f.category, file: f.file, year: f.year, tmdb: f.tmdb}
	}
	if _, err := carryFilms(ctx, tx, src.ID, existing, listing); err != nil {
		return 0, err
	}
	// The source's TMDB id is kept until the film is matched, so matching looks it up directly.
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO vod_movies(source_id, source_key, title, category, stream_url, logo_url,
		year, rating, tmdb_id, added_at) VALUES(?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(source_id, source_key) DO UPDATE SET title=excluded.title, category=excluded.category,
		stream_url=excluded.stream_url, logo_url=excluded.logo_url, year=COALESCE(vod_movies.year, excluded.year),
		rating=COALESCE(vod_movies.rating, excluded.rating),
		tmdb_id=CASE WHEN vod_movies.match_state='matched' THEN vod_movies.tmdb_id ELSE excluded.tmdb_id END`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()
	added := now()
	for _, f := range films {
		var year, rating, tmdbID any
		if f.year > 0 {
			year = f.year
		}
		if f.rating > 0 {
			rating = f.rating
		}
		if f.tmdb > 0 {
			tmdbID = f.tmdb
		}
		if _, err := stmt.ExecContext(ctx, src.ID, f.key, f.title, f.category, f.file, f.logo, year, rating, tmdbID,
			or(f.added, added)); err != nil {
			return 0, err
		}
	}
	for k, id := range existing {
		if !seen[k] {
			if _, err := tx.ExecContext(ctx, "DELETE FROM vod_movies WHERE id=?", id); err != nil {
				return 0, err
			}
		}
	}
	return len(films), tx.Commit()
}

// listClient allows for a long list from a slow server.
func (s *Service) listClient() *http.Client {
	c := *s.client()
	if c.Timeout > 0 && c.Timeout < 5*time.Minute {
		c.Timeout = 5 * time.Minute
	}
	return &c
}

func (s *Service) source(id string) (settings.VODSource, bool) {
	for _, src := range s.Settings.Get().Integrations.VODSources {
		if src.ID == id {
			return src, true
		}
	}
	return settings.VODSource{}, false
}

// Details fills in an Xtream Codes film's plot, runtime, cast and backdrop from the
// server the first time it is opened (TMDB's details, once matched, are kept).
func (s *Service) Details(ctx context.Context, id int64) {
	var srcID, key string
	var detailed sql.NullString
	if err := s.DB.QueryRowContext(ctx, "SELECT source_id, source_key, details_at FROM vod_movies WHERE id=?", id).
		Scan(&srcID, &key, &detailed); err != nil || detailed.Valid || !strings.HasPrefix(key, "xtream:") {
		return
	}
	src, ok := s.source(srcID)
	if !ok || !src.Xtream() {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	x := s.xtream(src)
	raw, err := x.rawInfo(ctx, strings.TrimPrefix(key, "xtream:"))
	if err != nil {
		slog.Warn("movie details", "source", src.Name, "err", err)
		return
	}
	var info xtreamInfo
	if b := bytes.TrimSpace(raw); len(b) > 0 && b[0] == '{' {
		if err := json.Unmarshal(b, &info); err != nil {
			slog.Warn("movie details", "source", src.Name, "err", x.redactErr(err))
			return
		}
	}
	// What the file is, for comparing it with the library's copy.
	if q, ok := qualityFromInfo(raw); ok {
		s.StoreMovieQuality(ctx, id, q)
	}
	if err := s.applyXtream(ctx, x, id, info); err != nil {
		slog.Warn("movie details", "source", src.Name, "err", err)
	}
}

var hms = regexp.MustCompile(`^(\d+):(\d{2}):(\d{2})$`)

// applyXtream stores the server's details where nothing better (TMDB's) is there yet.
func (s *Service) applyXtream(ctx context.Context, x *xtream, id int64, in xtreamInfo) error {
	var year any
	if d := or(in.ReleaseDate.String(), in.ReleaseDate2.String()); len(d) >= 4 {
		if y, err := strconv.Atoi(d[:4]); err == nil && y > 1880 {
			year = y
		}
	}
	runtime := in.DurationSecs.Int() * 1000
	if m := hms.FindStringSubmatch(in.Duration.String()); runtime <= 0 && m != nil {
		h, _ := strconv.Atoi(m[1])
		mi, _ := strconv.Atoi(m[2])
		sec, _ := strconv.Atoi(m[3])
		runtime = int64(h*3600+mi*60+sec) * 1000
	}
	var genres []string
	for _, g := range strings.Split(in.Genre.String(), ",") {
		if g = strings.TrimSpace(g); g != "" {
			genres = append(genres, g)
		}
	}
	cast := []CastMember{}
	for _, c := range strings.Split(or(in.Cast.String(), in.Actors.String()), ",") {
		if c = strings.TrimSpace(c); c != "" && len(cast) < 12 {
			cast = append(cast, CastMember{Name: c})
		}
	}
	backdrop := ""
	if len(in.Backdrop) > 0 {
		backdrop = x.absolute(in.Backdrop[0])
	}
	poster := x.absolute(or(in.CoverBig.String(), in.Cover.String()))
	gj, _ := json.Marshal(genres)
	if genres == nil {
		gj = []byte("[]")
	}
	cj, _ := json.Marshal(cast)
	var tmdbID any
	if n := in.TMDBID.Int(); n > 0 {
		tmdbID = n
	}
	_, err := s.DB.ExecContext(ctx, `UPDATE vod_movies SET details_at=?, year=COALESCE(year, ?),
		overview=CASE WHEN overview='' THEN ? ELSE overview END,
		content_rating=CASE WHEN content_rating='' THEN ? ELSE content_rating END,
		genres=CASE WHEN genres='[]' THEN ? ELSE genres END,
		runtime_ms=CASE WHEN runtime_ms=0 THEN ? ELSE runtime_ms END,
		director=CASE WHEN director='' THEN ? ELSE director END,
		cast_json=CASE WHEN cast_json='[]' THEN ? ELSE cast_json END,
		poster_url=CASE WHEN poster_url='' THEN ? ELSE poster_url END,
		backdrop_url=CASE WHEN backdrop_url='' THEN ? ELSE backdrop_url END,
		tmdb_id=COALESCE(tmdb_id, ?) WHERE id=?`,
		now(), year, or(in.Plot.String(), in.Description.String()), in.MPAA.String(), string(gj), runtime,
		in.Director.String(), string(cj), poster, backdrop, tmdbID, id)
	return err
}

// PlayLink is where a film streams from and how: hls is the source's own HLS (M3U sources),
// else a file the server makes into HLS. The link of an Xtream Codes film holds its
// password, so it is only built here, never stored, and never logged.
func (s *Service) PlayLink(m Movie) (link, userAgent string, hls bool) {
	src, ok := s.source(m.SourceID)
	if !ok || !src.Xtream() {
		// An M3U entry can be a plain file (an .mp4 or .mkv) as well as HLS: passed through
		// as a playlist, a file reached the player as one and couldn't play.
		return m.StreamURL, src.UserAgent, !isVideoFile(m.StreamURL)
	}
	return s.xtream(src).streamURL(m.StreamURL), src.UserAgent, false
}

// videoFileExts are the file types an M3U film can link to directly (anything else, such as
// .m3u8 or no extension, is taken to be HLS).
var videoFileExts = map[string]bool{".mp4": true, ".m4v": true, ".mkv": true, ".mov": true, ".avi": true,
	".webm": true, ".ts": true, ".mpg": true, ".mpeg": true, ".wmv": true, ".flv": true}

// isVideoFile is whether a link is a video file rather than an HLS playlist.
func isVideoFile(link string) bool {
	u, err := url.Parse(link)
	if err != nil {
		return false
	}
	return videoFileExts[strings.ToLower(path.Ext(u.Path))]
}

// Redact removes a film source's secrets from a message (FFmpeg's errors name the link).
func (s *Service) Redact(sourceID, msg string) string {
	if src, ok := s.source(sourceID); ok && src.Xtream() {
		return s.xtream(src).redact(msg)
	}
	return msg
}

// TestResult is what a source check found.
type TestResult struct {
	Movies, Categories int
	// Series is how many series an Xtream Codes server lists (-1: it didn't list them).
	Series    int
	Account   string
	ExpiresAt time.Time
}

// Test checks a source without saving it: it signs in to an Xtream Codes server and counts
// its films, or loads an M3U playlist and counts its entries.
func (s *Service) Test(ctx context.Context, src settings.VODSource) (TestResult, error) {
	var r TestResult
	if !src.Xtream() {
		body, err := s.get(ctx, src.URL, src.UserAgent)
		if err != nil {
			return r, err
		}
		defer body.Close()
		pl, err := livetv.ParseM3U(body)
		if err != nil {
			return r, err
		}
		r.Movies = len(pl.Entries)
		return r, nil
	}
	x := s.xtream(src)
	x.client = s.listClient()
	var err error
	if r.Account, r.ExpiresAt, err = x.account(ctx); err != nil {
		return r, err
	}
	cats, err := x.categories(ctx)
	if err != nil {
		return r, err
	}
	r.Categories = len(cats)
	list, err := x.streams(ctx)
	if err != nil {
		return r, err
	}
	for _, st := range list {
		if _, ok := x.film(st, cats); ok {
			r.Movies++
		}
	}
	r.Series = -1
	if scats, err := x.seriesCategories(ctx); err == nil {
		if shows, err := x.seriesList(ctx); err == nil {
			r.Series = 0
			for _, sh := range shows {
				if _, ok := x.show(sh, scats); ok {
					r.Series++
				}
			}
		}
	}
	return r, nil
}
