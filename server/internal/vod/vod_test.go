package vod

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"marquee/internal/db"
	"marquee/internal/metadata/tmdb"
	"marquee/internal/settings"
)

const (
	alphaID = "5f7631c876c54900134c6126"
	betaID  = "61f98773635aeb001acd5b95"
)

// films lists two Pluto-style films and one plain link; {base} is the fake server.
const films = `#EXTM3U
#EXTINF:-1 group-title="'00s Movies" tvg-name="Alpha",Alpha
{base}/v2/stitch/hls/episode/` + alphaID + `/master.m3u8?jwt=one
#EXTINF:-1 group-title="Anime Movies" tvg-name="Beta",Beta
{base}/v2/stitch/hls/episode/` + betaID + `/master.m3u8?jwt=one
#EXTINF:-1 group-title="'00s Movies" tvg-logo="{base}/logo.png",Gamma (1999)
{base}/films/gamma.m3u8?token=abc
`

const plutoItems = `[{"_id":"` + alphaID + `","description":"Pluto's plot.","rating":"R","genre":"Romance","duration":7200000,
"covers":[{"aspectRatio":"347:500","url":"{base}/poster.jpg"},{"aspectRatio":"16:9","url":"{base}/backdrop.jpg"}],
"clip":{"actors":["A One","B Two"],"directors":["D Rector"],"originalReleaseDate":"2002-03-01T00:00:00Z"}},
{"_id":"` + betaID + `","description":"Beta plot.","rating":"PG","genre":"Anime","duration":5400000,"covers":[],
"clip":{"originalReleaseDate":"2010-01-01T00:00:00Z"}}]`

type fakeMatcher struct {
	mu    sync.Mutex
	calls map[string]int // title -> year asked
	byID  []int
}

func (f *fakeMatcher) MatchMovie(_ context.Context, title string, year int) (*tmdb.Movie, error) {
	f.mu.Lock()
	f.calls[title] = year
	f.mu.Unlock()
	if title != "Alpha" {
		return nil, nil
	}
	m := &tmdb.Movie{ID: 42, Title: "Alpha", Overview: "TMDB's plot.", ReleaseDate: "2002-03-01", Runtime: 96,
		VoteAverage: 6.4, Genres: []tmdb.Genre{{Name: "Comedy"}, {Name: "Romance"}}, PosterPath: "/p.jpg", IMDBID: "tt0243736"}
	m.Credits.Cast = []tmdb.Cast{{Name: "Josh", Character: "Matt"}}
	m.Credits.Crew = []tmdb.Crew{{Name: "Michael", Job: "Director"}}
	m.Recommendations.Results = []tmdb.SearchResult{{ID: 7}, {ID: 9}}
	m.Keywords.Keywords = []tmdb.Keyword{{Name: "Wedding"}}
	return m, nil
}

func (f *fakeMatcher) MovieByID(_ context.Context, id int) (*tmdb.Movie, error) {
	f.mu.Lock()
	f.byID = append(f.byID, id)
	f.mu.Unlock()
	if id != 603 {
		return nil, errors.New("not found")
	}
	return &tmdb.Movie{ID: 603, Title: "The Matrix", Overview: "TMDB's Matrix.", ReleaseDate: "1999-03-31", Runtime: 136,
		VoteAverage: 8.2, PosterPath: "/m.jpg"}, nil
}

func setup(t *testing.T) (*Service, *httptest.Server, *fakeMatcher, int64) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	d, err := db.Open(ctx, filepath.Join(dir, "t.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		base := srv.URL
		switch r.URL.Path {
		case "/films.m3u":
			io.WriteString(w, strings.ReplaceAll(films, "{base}", base))
		case "/boot":
			io.WriteString(w, `{"sessionToken":"tok"}`)
		case "/items":
			if r.Header.Get("Authorization") != "Bearer tok" {
				http.Error(w, "no", http.StatusUnauthorized)
				return
			}
			io.WriteString(w, strings.ReplaceAll(plutoItems, "{base}", base))
		case "/poster.jpg", "/logo.png":
			w.Write([]byte("\xff\xd8\xff\xe0fake jpeg"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	plutoBoot, plutoVOD = srv.URL+"/boot", srv.URL+"/items"
	store, _ := settings.Open(ctx, d)
	store.Update(ctx, func(s *settings.Settings) error {
		s.Integrations.VODEnabled = true
		s.Integrations.VODSources = []settings.VODSource{{ID: "p", Name: "Pluto", URL: srv.URL + "/films.m3u", Enabled: true}}
		return nil
	})
	res, _ := d.Exec(`INSERT INTO users (username, display_name) VALUES ('u', 'U')`)
	uid, _ := res.LastInsertId()
	m := &fakeMatcher{calls: map[string]int{}}
	return &Service{DB: d, Settings: store, Matcher: m, Streams: &Streams{}, ImageDir: filepath.Join(dir, "img")}, srv, m, uid
}

// settle waits for the background details and matching to finish.
func settle(t *testing.T, s *Service) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var pending int
		s.DB.QueryRow("SELECT COUNT(*) FROM vod_movies WHERE match_state='pending'").Scan(&pending)
		if pending == 0 && !s.Matching() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("matching didn't finish")
}

func TestSourceKey(t *testing.T) {
	if k := sourceKey("https://x/v2/stitch/hls/episode/" + alphaID + "/master.m3u8?jwt=1"); k != "pluto:"+alphaID {
		t.Errorf("pluto key = %q", k)
	}
	a, b := sourceKey("https://cdn/films/a.m3u8?token=1"), sourceKey("https://cdn/films/a.m3u8?token=2")
	if a != b || !strings.HasPrefix(a, "url:") {
		t.Errorf("a link's token changing must keep its key: %q %q", a, b)
	}
}

func TestRefreshDetailsAndMatching(t *testing.T) {
	s, _, m, uid := setup(t)
	ctx := context.Background()
	s.Refresh(ctx, false)
	settle(t, s)

	st, _ := s.Status(ctx)
	if st.Movies != 3 || st.Matched != 1 || st.Unmatched != 2 || len(st.Sources) != 1 || st.Sources[0].Movies != 3 {
		t.Fatalf("status: %+v", st)
	}
	// Pluto's year sharpened the search; a "(1999)" title gives its own year.
	if m.calls["Alpha"] != 2002 || m.calls["Gamma"] != 1999 {
		t.Errorf("searches: %v", m.calls)
	}

	list, total, err := s.Movies(ctx, Filter{UserID: uid})
	if err != nil || total != 3 || list[0].Title != "Alpha" || list[2].Title != "Gamma (1999)" {
		t.Fatalf("movies: %d %+v %v", total, list, err)
	}
	a, _ := s.Movie(ctx, list[0].ID, uid)
	// TMDB's details win over Pluto's; the rating Pluto gave stays since TMDB gave none.
	if !a.Matched || a.TMDBID != 42 || a.Overview != "TMDB's plot." || a.RuntimeMS != 96*60000 || a.Year != 2002 ||
		a.ContentRating != "R" || a.Director != "Michael" || len(a.Cast) != 1 || a.Rating != 6.4 || len(a.Genres) != 2 || !a.HasBackdrop {
		t.Errorf("alpha: %+v", a)
	}
	// TMDB's recommendations and keywords are kept for suggestions.
	var recs, keywords string
	s.DB.QueryRow("SELECT tmdb_recs, keywords FROM vod_movies WHERE id=?", a.ID).Scan(&recs, &keywords)
	if recs != "[7,9]" || keywords != `["wedding"]` {
		t.Errorf("recommendations %s, keywords %s", recs, keywords)
	}
	b, _ := s.Movie(ctx, list[1].ID, uid)
	// Unmatched: Pluto's details stand in.
	if b.Matched || b.Overview != "Beta plot." || b.Year != 2010 || b.RuntimeMS != 5400000 || b.ContentRating != "PG" {
		t.Errorf("beta: %+v", b)
	}

	cats, _ := s.Categories(ctx, "")
	if len(cats) != 2 || cats[0].Name != "'00s Movies" || cats[0].Count != 2 {
		t.Errorf("categories: %+v", cats)
	}
	// A PG limit hides the R film and the unrated one, as the library does.
	if list, total, _ := s.Movies(ctx, Filter{UserID: uid, MaxRating: "PG"}); total != 1 || list[0].Title != "Beta" {
		t.Errorf("PG listing: %d %+v", total, list)
	}
	if !Allowed("PG", "PG-13") || Allowed("R", "PG-13") || Allowed("", "PG") || !Allowed("", "") {
		t.Error("Allowed")
	}
	if list, total, _ := s.Movies(ctx, Filter{UserID: uid, Query: "amm", Category: "'00s Movies"}); total != 1 || list[0].Title != "Gamma (1999)" {
		t.Errorf("search: %d %+v", total, list)
	}

	// Gamma has no poster, so the playlist's logo stands in (fetched once, then cached).
	if b, ct, err := s.Image(ctx, list[2].ID, "poster"); err != nil || ct != "image/jpeg" || len(b) == 0 {
		t.Errorf("poster: %q %v", ct, err)
	}
	if _, _, err := s.Image(ctx, b.ID, "poster"); err != ErrNotFound {
		t.Errorf("no artwork: %v", err)
	}
}

func TestRefreshRemovesAndKeepsWhileOff(t *testing.T) {
	s, _, _, uid := setup(t)
	ctx := context.Background()
	s.Refresh(ctx, false)
	settle(t, s)
	list, _, _ := s.Movies(ctx, Filter{UserID: uid})
	s.SetProgress(ctx, uid, list[0].ID, 60_000, 0)

	// Turning movies on demand off keeps everything (and everyone's progress).
	s.Settings.Update(ctx, func(st *settings.Settings) error { st.Integrations.VODEnabled = false; return nil })
	s.Refresh(ctx, false)
	if _, total, _ := s.Movies(ctx, Filter{}); total != 3 {
		t.Errorf("films while off = %d", total)
	}
	// Removing the source removes its films.
	s.Settings.Update(ctx, func(st *settings.Settings) error {
		st.Integrations.VODEnabled = true
		st.Integrations.VODSources = nil
		return nil
	})
	s.Refresh(ctx, false)
	if _, total, _ := s.Movies(ctx, Filter{}); total != 0 {
		t.Errorf("films after removing the source = %d", total)
	}
}

func TestProgress(t *testing.T) {
	s, _, _, uid := setup(t)
	ctx := context.Background()
	s.Refresh(ctx, false)
	settle(t, s)
	list, _, _ := s.Movies(ctx, Filter{UserID: uid})
	id := list[0].ID

	s.SetProgress(ctx, uid, id, 5_000, 6_000_000) // too early to resume
	if cw, _ := s.ContinueWatching(ctx, uid, ""); len(cw) != 0 {
		t.Errorf("continue after 5s: %+v", cw)
	}
	s.SetProgress(ctx, uid, id, 1_200_000, 6_000_000)
	cw, _ := s.ContinueWatching(ctx, uid, "")
	if len(cw) != 1 || cw[0].PositionMS != 1_200_000 || cw[0].Watched {
		t.Fatalf("continue: %+v", cw)
	}
	s.SetProgress(ctx, uid, id, 5_500_000, 6_000_000) // past 90%
	m, _ := s.Movie(ctx, id, uid)
	if !m.Watched || m.PositionMS != 0 {
		t.Errorf("after the end: %+v", m)
	}
	s.SetWatched(ctx, uid, id, false)
	if m, _ := s.Movie(ctx, id, uid); m.Watched {
		t.Error("still watched")
	}
}

func TestStreamRewritesAndProxies(t *testing.T) {
	var upstream *httptest.Server
	var gotUA string
	var methods []string
	upstream = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUA = r.Header.Get("User-Agent")
		methods = append(methods, r.Method)
		switch r.URL.Path {
		case "/ep/master.m3u8":
			w.Header().Set("Content-Type", "application/x-mpegURL")
			io.WriteString(w, "#EXTM3U\n#EXT-X-MEDIA:TYPE=AUDIO,GROUP-ID=\"a\",URI=\"audio/en.m3u8?sid=1\"\n"+
				"#EXT-X-STREAM-INF:BANDWIDTH=1000,AUDIO=\"a\"\n1000/playlist.m3u8?sid=1\n")
		case "/ep/1000/playlist.m3u8":
			io.WriteString(w, "#EXTM3U\n#EXT-X-KEY:METHOD=AES-128,URI=\""+upstream.URL+"/key\"\n#EXTINF:5,\nseg0.ts\n#EXTINF:5,\n"+upstream.URL+"/abs/seg1.ts\n#EXT-X-ENDLIST\n")
		case "/ep/1000/seg0.ts":
			w.Header().Set("Content-Type", "video/mp2t")
			io.WriteString(w, "segment-zero")
		default:
			http.NotFound(w, r)
		}
	}))
	defer upstream.Close()
	st := &Streams{}
	sess := st.Start(1, 7, upstream.URL+"/ep/master.m3u8?jwt=x", "Marquee-UA")
	h := httptest.NewServer(st.Handler())
	defer h.Close()
	get := func(name string) (string, string) {
		resp, err := http.Get(h.URL + StreamPrefix + sess.ID + "/" + name)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return string(b), resp.Header.Get("Content-Type")
	}
	master, ct := get("master.m3u8")
	if ct != "application/vnd.apple.mpegurl" || !strings.Contains(master, `URI="0.m3u8"`) || !strings.Contains(master, "\n1.m3u8") {
		t.Fatalf("master (%s):\n%s", ct, master)
	}
	if strings.Contains(master, upstream.URL) {
		t.Error("the master still links to the source")
	}
	variant, _ := get("1.m3u8")
	if !strings.Contains(variant, `URI="2.bin"`) || !strings.Contains(variant, "\n3.ts") || !strings.Contains(variant, "\n4.ts") {
		t.Fatalf("variant:\n%s", variant)
	}
	if seg, ct := get("3.ts"); seg != "segment-zero" || ct != "video/mp2t" {
		t.Errorf("segment = %q (%s)", seg, ct)
	}
	if gotUA != "Marquee-UA" {
		t.Errorf("user agent = %q", gotUA)
	}
	// Fetching the variant again doesn't grow the session's links.
	get("1.m3u8")
	if n := len(sess.urls); n != 5 {
		t.Errorf("links = %d", n)
	}
	// Only reads reach the source.
	methods = nil
	for _, m := range []string{http.MethodPost, http.MethodDelete, http.MethodPut} {
		req, _ := http.NewRequest(m, h.URL+StreamPrefix+sess.ID+"/master.m3u8", strings.NewReader("x"))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusMethodNotAllowed {
			t.Errorf("%s: %d", m, resp.StatusCode)
		}
	}
	if len(methods) != 0 {
		t.Errorf("the source got %v", methods)
	}
	if body, _ := get("99.ts"); !strings.Contains(body, "not found") {
		t.Errorf("unknown link: %q", body)
	}
	st.Stop(sess.ID)
	if body, _ := get("master.m3u8"); !strings.Contains(body, "not found") {
		t.Errorf("stopped session: %q", body)
	}
}
