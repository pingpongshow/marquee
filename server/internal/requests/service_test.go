package requests

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"marquee/internal/db"
	"marquee/internal/settings"
)

// fakeSeerr serves just enough of the Seerr API, and records requests made to it.
type fakeSeerr struct {
	mu       sync.Mutex
	made     []map[string]any
	declined bool
}

func (f *fakeSeerr) handler(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "key" {
			http.Error(w, `{"message":"bad key"}`, 403)
			return
		}
		switch {
		case r.URL.Path == "/api/v1/status":
			w.Write([]byte(`{"version":"3.4.1"}`))
		case r.URL.Path == "/api/v1/search":
			if strings.Contains(r.URL.RawQuery, "+") {
				http.Error(w, `{"message":"bad query"}`, 400) // Seerr wants %20
				return
			}
			w.Write([]byte(`{"page":1,"totalPages":1,"results":[
				{"id":10,"mediaType":"movie","title":"Here Already","releaseDate":"2020-01-01","posterPath":"/a.jpg"},
				{"id":11,"mediaType":"movie","title":"Wanted","releaseDate":"2021-05-01"},
				{"id":12,"mediaType":"tv","name":"A Show","firstAirDate":"2019-02-02","mediaInfo":{"status":4}},
				{"id":13,"mediaType":"person","name":"Someone"}]}`))
		case r.URL.Path == "/api/v1/movie/11":
			w.Write([]byte(`{"id":11,"title":"Wanted","releaseDate":"2021-05-01","posterPath":"/w.jpg"}`))
		case r.URL.Path == "/api/v1/movie/10":
			w.Write([]byte(`{"id":10,"title":"Here Already","releaseDate":"2020-01-01"}`))
		case r.URL.Path == "/api/v1/tv/12":
			w.Write([]byte(`{"id":12,"name":"A Show","firstAirDate":"2019-02-02","mediaInfo":{"status":4,"seasons":[{"seasonNumber":1,"status":5}]},
				"seasons":[{"seasonNumber":0,"name":"Specials"},{"seasonNumber":1,"name":"Season 1","episodeCount":8},{"seasonNumber":2,"name":"Season 2","episodeCount":8}]}`))
		case r.URL.Path == "/api/v1/request" && r.Method == http.MethodPost:
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			f.mu.Lock()
			f.made = append(f.made, body)
			f.mu.Unlock()
			w.WriteHeader(201)
			w.Write([]byte(`{"id":77}`))
		case r.URL.Path == "/api/v1/request/77":
			if f.declined {
				w.Write([]byte(`{"status":3,"media":{"status":1}}`))
			} else {
				w.Write([]byte(`{"status":2,"media":{"status":5}}`))
			}
		default:
			t.Logf("unexpected %s %s", r.Method, r.URL)
			http.NotFound(w, r)
		}
	})
}

func setup(t *testing.T) (*Service, *fakeSeerr, int64, int64) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	d, err := db.Open(ctx, filepath.Join(dir, "t.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	f := &fakeSeerr{}
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)
	store, _ := settings.Open(ctx, d)
	store.Update(ctx, func(s *settings.Settings) error {
		s.Integrations.SeerrURL, s.Integrations.SeerrAPIKey = srv.URL, "key"
		return nil
	})
	res, _ := d.Exec(`INSERT INTO users (username, display_name, is_admin) VALUES ('admin', 'Admin', 1)`)
	admin, _ := res.LastInsertId()
	res, _ = d.Exec(`INSERT INTO users (username, display_name) VALUES ('kid', 'Kid')`)
	kid, _ := res.LastInsertId()
	// "Here Already" (TMDB 10) is in the library.
	d.Exec(`INSERT INTO libraries (id, name, type) VALUES (1, 'Movies', 'movies')`)
	res, err = d.Exec(`INSERT INTO items (library_id, type, title, sort_title) VALUES (1, 'movie', 'Here Already', 'here already')`)
	if err != nil {
		t.Fatal(err)
	}
	item, _ := res.LastInsertId()
	d.Exec(`INSERT INTO external_ids (item_id, provider, value) VALUES (?, 'tmdb', '10')`, item)
	return &Service{DB: d, Settings: store}, f, admin, kid
}

func TestSearchMarksLibraryAndRequests(t *testing.T) {
	s, _, _, kid := setup(t)
	ctx := context.Background()
	items, _, _, err := s.Search(ctx, kid, "two words", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("people should be left out: %+v", items)
	}
	if items[0].Availability != Available || items[0].ItemID == 0 || items[0].PosterURL != "https://image.tmdb.org/t/p/w342/a.jpg" {
		t.Errorf("library movie: %+v", items[0])
	}
	if items[1].Availability != None || items[2].Availability != Partial {
		t.Errorf("availability: %+v %+v", items[1], items[2])
	}
	if _, err := s.Create(ctx, kid, "movie", 11, nil); err != nil {
		t.Fatal(err)
	}
	items, _, _, _ = s.Search(ctx, kid, "two words", 1)
	if items[1].Availability != Pending || items[1].RequestID == 0 {
		t.Errorf("after requesting: %+v", items[1])
	}
}

func TestRequestApprovalFlow(t *testing.T) {
	s, f, admin, kid := setup(t)
	ctx := context.Background()
	if _, err := s.Create(ctx, kid, "movie", 10, nil); !errors.Is(err, ErrAvailable) {
		t.Errorf("in the library: %v", err)
	}
	r, err := s.Create(ctx, kid, "movie", 11, nil)
	if err != nil || r.Status != "pending" || r.Title != "Wanted" || r.Year != 2021 {
		t.Fatalf("create: %+v %v", r, err)
	}
	if _, err := s.Create(ctx, admin, "movie", 11, nil); !errors.Is(err, ErrDuplicate) {
		t.Errorf("duplicate: %v", err)
	}
	if len(f.made) != 0 {
		t.Fatal("nothing goes to Seerr before approval")
	}
	seerrUser := int64(2)
	r, err = s.Approve(ctx, r.ID, admin, func(int64) *int64 { return &seerrUser })
	if err != nil || r.Status != "approved" || r.SeerrRequestID != 77 || r.DecidedBy != "Admin" {
		t.Fatalf("approve: %+v %v", r, err)
	}
	if got := f.made[0]; got["mediaType"] != "movie" || got["mediaId"] != float64(11) || got["userId"] != float64(2) {
		t.Errorf("sent to Seerr: %v", got)
	}
	if _, err := s.Approve(ctx, r.ID, admin, func(int64) *int64 { return nil }); !errors.Is(err, ErrDecided) {
		t.Errorf("approve twice: %v", err)
	}
	// Seerr reports it arrived.
	list, _ := s.List(ctx, kid, "")
	if len(list) != 1 || list[0].Status != "available" {
		t.Errorf("after arrival: %+v", list)
	}
}

func TestShowSeasonsAndDecline(t *testing.T) {
	s, f, admin, kid := setup(t)
	ctx := context.Background()
	if _, err := s.Create(ctx, kid, "tv", 12, []int{9}); err == nil {
		t.Error("unknown season accepted")
	}
	r, err := s.Create(ctx, kid, "tv", 12, []int{2, 2})
	if err != nil || len(r.Seasons) != 1 {
		t.Fatalf("create show: %+v %v", r, err)
	}
	title, seasons, err := s.Show(ctx, 12)
	if err != nil || title != "A Show" || len(seasons) != 2 {
		t.Fatalf("show: %s %+v %v", title, seasons, err)
	}
	if seasons[0].Availability != Available || seasons[1].Availability != Pending {
		t.Errorf("seasons: %+v", seasons)
	}
	r, err = s.Decline(ctx, r.ID, admin, "Not this one")
	if err != nil || r.Status != "declined" || r.Reason != "Not this one" {
		t.Fatalf("decline: %+v %v", r, err)
	}
	if len(f.made) != 0 {
		t.Error("declined requests never reach Seerr")
	}
	if err := s.Cancel(ctx, r.ID); !errors.Is(err, ErrDecided) {
		t.Errorf("cancel decided: %v", err)
	}
}

func TestNotConfigured(t *testing.T) {
	s, _, _, kid := setup(t)
	ctx := context.Background()
	s.Settings.Update(ctx, func(x *settings.Settings) error { x.Integrations.SeerrURL = ""; return nil })
	if _, _, _, err := s.Search(ctx, kid, "x", 1); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("unconfigured: %v", err)
	}
}
