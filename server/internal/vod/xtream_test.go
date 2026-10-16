package vod

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"

	"marquee/internal/settings"
)

const xcPass = "p@ss/w0rd&x"

// Field names and types vary between Xtream Codes servers: ids and ratings as numbers or
// strings, "tmdb" or "tmdb_id", empty strings for missing values.
const xcStreams = `[
{"num":1,"name":"The Matrix (1999)","stream_type":"movie","stream_id":"101","stream_icon":"/api/logo/101.png","rating":"8.1",
 "added":"1700000000","category_id":"1","container_extension":"mkv","tmdb":"603"},
{"num":2,"name":"Other Film","stream_id":102,"stream_icon":"","rating":7,"added":1700000001,"category_id":2,
 "container_extension":"mp4","tmdb_id":"","year":"2004"},
{"num":3,"name":"","stream_id":103},
{"num":4,"name":"Third","stream_id":"104","rating":"","category_id":null,"container_extension":"../x","tmdb":null}
]`

type fakeXtream struct {
	mu         sync.Mutex
	streams    string
	series     string
	seriesInfo map[string]string
	calls      []string
	files      []string
}

func (f *fakeXtream) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if strings.HasPrefix(r.URL.Path, "/series/") || strings.HasPrefix(r.URL.Path, "/movie/") {
			f.mu.Lock()
			f.files = append(f.files, r.URL.Path)
			f.mu.Unlock()
			w.Header().Set("Content-Type", "video/mp4")
			io.WriteString(w, "not really a video")
			return
		}
		if r.URL.Path != "/player_api.php" {
			http.NotFound(w, r)
			return
		}
		if q.Get("username") != "marquee" || q.Get("password") != xcPass {
			io.WriteString(w, `{"user_info":{"auth":0}}`)
			return
		}
		f.mu.Lock()
		f.calls = append(f.calls, q.Get("action"))
		streams, series, info := f.streams, f.series, f.seriesInfo[q.Get("series_id")]
		f.mu.Unlock()
		switch q.Get("action") {
		case "":
			io.WriteString(w, `{"user_info":{"username":"marquee","auth":1,"status":"Active","exp_date":"1893456000","max_connections":"1"},"server_info":{}}`)
		case "get_vod_categories":
			io.WriteString(w, `[{"category_id":"1","category_name":"Action","parent_id":0},{"category_id":2,"category_name":"Drama"}]`)
		case "get_vod_streams":
			io.WriteString(w, streams)
		case "get_vod_info":
			if q.Get("vod_id") != "102" {
				io.WriteString(w, `{"info":[],"movie_data":{}}`)
				return
			}
			io.WriteString(w, `{"info":{"plot":"A plot.","cast":"Ann One, Bob Two","director":"Dee","genre":"Drama, Thriller",
				"releasedate":"2004-05-01","duration_secs":"5400","duration":"01:30:00","backdrop_path":["/bd/102.jpg"],"tmdb_id":""},
				"movie_data":{"stream_id":102,"container_extension":"mp4"}}`)
		case "get_series_categories":
			io.WriteString(w, `[{"category_id":"10","category_name":"Drama Series"},{"category_id":11,"category_name":"Comedy Series"}]`)
		case "get_series":
			io.WriteString(w, series)
		case "get_series_info":
			if info == "" {
				http.Error(w, "no such series", http.StatusInternalServerError)
				return
			}
			io.WriteString(w, info)
		default:
			http.Error(w, "unknown action", http.StatusBadRequest)
		}
	}
}

func xtreamSetup(t *testing.T) (*Service, *fakeXtream, *httptest.Server, *fakeMatcher, int64) {
	t.Helper()
	s, _, m, uid := setup(t)
	f := &fakeXtream{streams: xcStreams, series: xcSeries, seriesInfo: map[string]string{"201": xcInfoByMap, "202": xcInfoByList}}
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)
	s.Settings.Update(context.Background(), func(st *settings.Settings) error {
		st.Integrations.VODSources = []settings.VODSource{{ID: "xc", Name: "Dispatcharr", Kind: settings.VODKindXtream,
			URL: srv.URL, Username: "marquee", Password: xcPass, Enabled: true}}
		return nil
	})
	return s, f, srv, m, uid
}

func TestXtreamRefreshMatchingAndDetails(t *testing.T) {
	s, f, srv, m, uid := xtreamSetup(t)
	ctx := context.Background()
	s.Refresh(ctx, false)
	settle(t, s)

	st, _ := s.Status(ctx)
	if st.Movies != 3 || len(st.Sources) != 1 || st.Sources[0].Movies != 3 || st.Sources[0].Error != "" {
		t.Fatalf("status: %+v", st)
	}
	// One request for the categories and one for every film: no per-film requests.
	if strings.Join(f.calls, ",") != "get_vod_categories,get_vod_streams,get_series_categories,get_series" {
		t.Errorf("requests: %v", f.calls)
	}
	// The film the server named by TMDB id was looked up by it, not searched for.
	// (Series lookups are recorded as negative ids.)
	if len(m.byID) == 0 || m.byID[0] != 603 || (len(m.byID) > 1 && m.byID[1] > 0) {
		t.Errorf("by id: %v", m.byID)
	}
	if _, searched := m.calls["The Matrix"]; searched {
		t.Error("a film with a TMDB id was searched for by title")
	}
	if m.calls["Other Film"] != 2004 {
		t.Errorf("searches: %v", m.calls)
	}

	list, total, err := s.Movies(ctx, Filter{UserID: uid, Sort: "title"})
	if err != nil || total != 3 {
		t.Fatalf("movies: %d %v", total, err)
	}
	byTitle := map[string]Movie{}
	for _, mv := range list {
		byTitle[mv.Title] = mv
	}
	matrix, other, third := byTitle["The Matrix (1999)"], byTitle["Other Film"], byTitle["Third"]
	if !matrix.Matched || matrix.TMDBID != 603 || matrix.Category != "Action" || matrix.Year != 1999 || !matrix.HasPoster {
		t.Errorf("matrix: %+v", matrix)
	}
	if other.Category != "Drama" || other.Year != 2004 || other.Rating != 7 || other.Matched {
		t.Errorf("other: %+v", other)
	}
	if third.Category != "" || third.StreamURL != "104.mp4" {
		t.Errorf("third: %+v", third)
	}
	// Nothing stored holds the password; the logo's relative link is the server's.
	var stored string
	s.DB.QueryRow("SELECT group_concat(stream_url || ' ' || logo_url, ' ') FROM vod_movies").Scan(&stored)
	if strings.Contains(stored, "w0rd") || !strings.Contains(stored, srv.URL+"/api/logo/101.png") || !strings.Contains(stored, "101.mkv") {
		t.Errorf("stored: %q", stored)
	}

	// Details come from the server the first time the film is opened.
	s.Details(ctx, other.ID)
	d, _ := s.Movie(ctx, other.ID, uid)
	if d.Overview != "A plot." || d.RuntimeMS != 5_400_000 || d.Director != "Dee" || len(d.Cast) != 2 || len(d.Genres) != 2 || !d.HasBackdrop {
		t.Errorf("details: %+v", d)
	}
	n := len(f.calls)
	s.Details(ctx, other.ID)
	if len(f.calls) != n {
		t.Error("details are fetched once")
	}

	// The film's link is built when it plays, with the password escaped.
	link, _, hls := s.PlayLink(matrix)
	if hls || link != srv.URL+"/movie/marquee/"+url.PathEscape(xcPass)+"/101.mkv" {
		t.Errorf("link %q hls=%v", link, hls)
	}

	// A film gone from the server goes from the list, and the rest keep their match.
	f.mu.Lock()
	f.streams = strings.Replace(xcStreams, `{"num":2,"name":"Other Film"`, `{"num":2,"name":""`, 1)
	f.mu.Unlock()
	s.Refresh(ctx, false)
	settle(t, s)
	if _, total, _ := s.Movies(ctx, Filter{}); total != 2 {
		t.Errorf("after removal: %d", total)
	}
	if mv, _ := s.Movie(ctx, matrix.ID, uid); !mv.Matched || mv.TMDBID != 603 {
		t.Errorf("matrix after refresh: %+v", mv)
	}
}

// The password never shows: not in errors (the source's status, admin alerts) or logs.
func TestXtreamPasswordRedacted(t *testing.T) {
	var logs bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(old)

	s, _, srv, _, _ := xtreamSetup(t)
	ctx := context.Background()
	var alerted error
	s.OnSourceFailed = func(_ settings.VODSource, err error) { alerted = err }
	srv.Close() // the server is down: the client's error names the request's link
	s.Refresh(ctx, false)
	st, _ := s.Status(ctx)
	if len(st.Sources) != 1 || st.Sources[0].Error == "" || alerted == nil {
		t.Fatalf("status: %+v", st)
	}
	for _, msg := range []string{st.Sources[0].Error, alerted.Error(), logs.String()} {
		for _, p := range []string{xcPass, url.QueryEscape(xcPass), url.PathEscape(xcPass)} {
			if strings.Contains(msg, p) {
				t.Errorf("password in %q", msg)
			}
		}
	}
	if !strings.Contains(st.Sources[0].Error, "***") {
		t.Errorf("error: %q", st.Sources[0].Error)
	}
	if got := s.Redact("xc", "ffmpeg: "+srv.URL+"/movie/marquee/"+url.PathEscape(xcPass)+"/1.mkv: refused"); strings.Contains(got, "w0rd") {
		t.Errorf("redact: %q", got)
	}
}

func TestXtreamTest(t *testing.T) {
	s, _, srv, _, _ := xtreamSetup(t)
	ctx := context.Background()
	src := settings.VODSource{Kind: settings.VODKindXtream, URL: srv.URL, Username: "marquee", Password: xcPass}
	r, err := s.Test(ctx, src)
	if err != nil || r.Movies != 3 || r.Series != 3 || r.Categories != 2 || r.Account != "Active" || r.ExpiresAt.Year() != 2030 {
		t.Fatalf("test: %+v %v", r, err)
	}
	src.Password = "wrong"
	if _, err := s.Test(ctx, src); err == nil || !strings.Contains(err.Error(), "username or password") {
		t.Errorf("wrong password: %v", err)
	}
}
