// Package bazarrtest is a fake Bazarr for tests: the API shapes Marquee uses, over a few
// movies and episodes. Downloads write a small .srt next to the video.
package bazarrtest

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"marquee/internal/bazarr"
)

const APIKey = "test-key"

// Fake holds Bazarr's view of the library. Paths are Bazarr's (e.g. /movies/…); Local maps
// them to where the files really are, for writing downloaded subtitles.
type Fake struct {
	Movies   []bazarr.Movie
	Series   []bazarr.Series
	Episodes []bazarr.Episode
	// Local turns a Bazarr path into a local one.
	Local func(string) string
	// Fail makes every call answer 500.
	Fail bool

	mu        sync.Mutex
	Downloads []string // "movie 1 en", "episode 7 fr", "pick movie 1 opensubtitles"
	URL       string
}

// Start serves the fake until the test ends.
func (f *Fake) Start(t *testing.T) *Fake {
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	f.URL = srv.URL
	return f
}

// Calls returns the downloads asked for so far.
func (f *Fake) Calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.Downloads...)
}

func write(w http.ResponseWriter, data any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"data": data, "total": 0})
}

func idsOf(r *http.Request, key string) map[int64]bool {
	out := map[int64]bool{}
	for _, v := range r.URL.Query()[key] {
		id, _ := strconv.ParseInt(v, 10, 64)
		out[id] = true
	}
	return out
}

func qid(r *http.Request, key string) int64 {
	id, _ := strconv.ParseInt(r.URL.Query().Get(key), 10, 64)
	return id
}

func (f *Fake) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("X-API-KEY") != APIKey {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if f.Fail {
		http.Error(w, "boom", http.StatusInternalServerError)
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	q := r.URL.Query()
	switch r.Method + " " + strings.TrimPrefix(r.URL.Path, "/api") {
	case "GET /system/status":
		write(w, map[string]any{"bazarr_version": "1.6.0"})
	case "GET /movies":
		want := idsOf(r, "radarrid[]")
		out := []bazarr.Movie{}
		for _, m := range f.Movies {
			if len(want) == 0 || want[m.RadarrID] {
				out = append(out, m)
			}
		}
		write(w, out)
	case "GET /series":
		write(w, f.Series)
	case "GET /episodes":
		series, eps := idsOf(r, "seriesid[]"), idsOf(r, "episodeid[]")
		out := []bazarr.Episode{}
		for _, e := range f.Episodes {
			if series[e.SeriesID] || eps[e.EpisodeID] {
				out = append(out, e)
			}
		}
		write(w, out)
	case "GET /movies/wanted":
		out := []map[string]any{}
		for _, m := range f.Movies {
			if len(m.Missing) > 0 {
				out = append(out, map[string]any{"radarrId": m.RadarrID, "title": m.Title, "missing_subtitles": m.Missing})
			}
		}
		write(w, out)
	case "GET /episodes/wanted":
		out := []map[string]any{}
		for _, e := range f.Episodes {
			if len(e.Missing) > 0 {
				out = append(out, map[string]any{"sonarrSeriesId": e.SeriesID, "sonarrEpisodeId": e.EpisodeID, "missing_subtitles": e.Missing})
			}
		}
		write(w, out)
	case "PATCH /movies/subtitles":
		m := f.movie(qid(r, "radarrid"))
		if m == nil {
			http.Error(w, "Movie not found", http.StatusNotFound)
			return
		}
		f.Downloads = append(f.Downloads, "movie "+q.Get("radarrid")+" "+q.Get("language"))
		f.save(&m.Path, &m.Subtitles, &m.Missing, q.Get("language"), q.Get("forced") == "True", q.Get("hi") == "True")
		w.WriteHeader(http.StatusNoContent)
	case "PATCH /episodes/subtitles":
		e := f.episode(qid(r, "episodeid"))
		if e == nil {
			http.Error(w, "Episode not found", http.StatusNotFound)
			return
		}
		f.Downloads = append(f.Downloads, "episode "+q.Get("episodeid")+" "+q.Get("language"))
		f.save(&e.Path, &e.Subtitles, &e.Missing, q.Get("language"), q.Get("forced") == "True", q.Get("hi") == "True")
		w.WriteHeader(http.StatusNoContent)
	case "GET /providers/movies", "GET /providers/episodes":
		write(w, []map[string]any{
			{"provider": "podnapisi", "subtitle": "b64:low", "language": "en", "release_info": []string{"Some.Release.720p"}, "score": 60,
				"hearing_impaired": "False", "forced": "False", "uploader": "", "original_format": "False"},
			{"provider": "opensubtitlescom", "subtitle": "b64:best", "language": "en", "release_info": []string{"Some.Release.1080p", "Other"}, "score": 95.4,
				"hearing_impaired": "True", "forced": "False", "uploader": "someone", "original_format": "True"},
		})
	case "POST /providers/movies":
		m := f.movie(qid(r, "radarrid"))
		if m == nil {
			http.Error(w, "Movie not found", http.StatusNotFound)
			return
		}
		f.Downloads = append(f.Downloads, "pick movie "+q.Get("radarrid")+" "+q.Get("provider")+" "+q.Get("subtitle"))
		f.save(&m.Path, &m.Subtitles, &m.Missing, "en", false, q.Get("hi") == "True")
		w.WriteHeader(http.StatusNoContent)
	case "POST /providers/episodes":
		e := f.episode(qid(r, "episodeid"))
		if e == nil {
			http.Error(w, "Episode not found", http.StatusNotFound)
			return
		}
		f.Downloads = append(f.Downloads, "pick episode "+q.Get("episodeid")+" "+q.Get("provider"))
		f.save(&e.Path, &e.Subtitles, &e.Missing, "en", false, q.Get("hi") == "True")
		w.WriteHeader(http.StatusNoContent)
	default:
		http.NotFound(w, r)
	}
}

func (f *Fake) movie(id int64) *bazarr.Movie {
	for i := range f.Movies {
		if f.Movies[i].RadarrID == id {
			return &f.Movies[i]
		}
	}
	return nil
}

func (f *Fake) episode(id int64) *bazarr.Episode {
	for i := range f.Episodes {
		if f.Episodes[i].EpisodeID == id {
			return &f.Episodes[i]
		}
	}
	return nil
}

// save writes "<video>.<lang>[.forced|.hi].srt" and moves the language from missing to have.
func (f *Fake) save(video *string, have, missing *[]bazarr.Language, lang string, forced, hi bool) {
	name := strings.TrimSuffix(*video, filepath.Ext(*video)) + "." + lang
	if forced {
		name += ".forced"
	} else if hi {
		name += ".hi"
	}
	name += ".srt"
	if f.Local != nil {
		os.WriteFile(f.Local(name), []byte("1\n00:00:00,500 --> 00:00:01,500\nHello from Bazarr\n"), 0o644)
	}
	*have = append(*have, bazarr.Language{Name: lang, Code2: lang, Path: name, Forced: bazarr.Flag(forced), HI: bazarr.Flag(hi)})
	kept := (*missing)[:0]
	for _, l := range *missing {
		if l.Code2 != lang || bool(l.Forced) != forced {
			kept = append(kept, l)
		}
	}
	*missing = kept
}
