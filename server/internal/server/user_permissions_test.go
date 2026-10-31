package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"marquee/internal/api"
	"marquee/internal/requests"
	"marquee/internal/settings"
	"marquee/internal/vod"
	"marquee/internal/vodsave"
)

// Per user: requests approved automatically, and VOD downloads. A user set to have their
// requests approved automatically sends them straight to Seerr; others' wait for an admin.
// A user allowed VOD downloads can queue films (not upgrades, not the queue itself); others
// can't, and don't see the save state.
func TestUserRequestAndDownloadPermissions(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	var res api.AuthResult
	h.do("POST", "/setup", map[string]any{"serverName": "T", "username": "admin", "password": "correct horse", "device": device}, &res)
	admin := res.Token
	h.token = admin

	seerr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasPrefix(r.URL.Path, "/api/v1/movie/"):
			id := strings.TrimPrefix(r.URL.Path, "/api/v1/movie/")
			fmt.Fprintf(w, `{"id":%s,"title":"Film %s","releaseDate":"2021-01-01"}`, id, id)
		case r.URL.Path == "/api/v1/request" && r.Method == http.MethodPost:
			w.WriteHeader(201)
			w.Write([]byte(`{"id":77}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(seerr.Close)
	movies := h.media + "/video/Movies"
	h.store.Update(ctx, func(s *settings.Settings) error {
		s.Integrations.SeerrURL, s.Integrations.SeerrAPIKey = seerr.URL, "key"
		s.Integrations.VODEnabled = true
		s.Integrations.VODSources = []settings.VODSource{{ID: "x", Name: "Films", URL: "http://127.0.0.1:1", Kind: settings.VODKindXtream,
			Username: "u", Password: "p", Enabled: true}}
		s.VODSave.MoviesFolder = movies
		return nil
	})
	h.handlers.Requests = &requests.Service{DB: h.db, Settings: h.store}
	h.handlers.VOD = &vod.Service{DB: h.db, Settings: h.store, Streams: &vod.Streams{}}
	h.handlers.VODSaves = &vodsave.Service{DB: h.db, Settings: h.store, VOD: h.handlers.VOD}
	r, err := h.db.Exec(`INSERT INTO vod_movies(source_id, source_key, title, stream_url, added_at, tmdb_id, match_state)
		VALUES ('x', 'xtream:1', 'Ronin', 'x', '2026-01-01T00:00:00Z', 100, 'matched')`)
	if err != nil {
		t.Fatal(err)
	}
	film, _ := r.LastInsertId()

	users := map[string]map[string]any{
		"ann": {"canRequest": true, "autoApproveRequests": true},
		"bo":  {"canRequest": true},
		"cy":  {},
		"di":  {"canDownloadVod": true},
	}
	tokens := map[string]string{}
	for name, rs := range users {
		var u api.User
		if code := h.do("POST", "/users", map[string]any{"username": name, "password": name + "-password-1", "restrictions": rs}, &u); code != 201 {
			t.Fatalf("create %s: %d", name, code)
		}
		if name == "ann" && (u.Restrictions.AutoApproveRequests == nil || !*u.Restrictions.AutoApproveRequests) {
			t.Errorf("ann's restrictions: %+v", u.Restrictions)
		}
		if name == "di" && (u.Restrictions.CanDownloadVod == nil || !*u.Restrictions.CanDownloadVod) {
			t.Errorf("di's restrictions: %+v", u.Restrictions)
		}
		h.token = ""
		var login api.AuthResult
		h.do("POST", "/auth/login", map[string]any{"username": name, "password": name + "-password-1", "device": device}, &login)
		tokens[name] = login.Token
		h.token = admin
	}
	as := func(name string) { h.token = tokens[name] }

	// Requests.
	var req api.MediaRequest
	as("ann")
	if code := h.do("POST", "/requests", map[string]any{"mediaType": "movie", "tmdbId": 11}, &req); code != 201 || req.Status != "approved" {
		t.Errorf("ann's request: %d %s", code, req.Status)
	}
	as("bo")
	if code := h.do("POST", "/requests", map[string]any{"mediaType": "movie", "tmdbId": 12}, &req); code != 201 || req.Status != "pending" {
		t.Errorf("bo's request: %d %s", code, req.Status)
	}

	// VOD downloads.
	save := fmt.Sprintf("/livetv/vod/movies/%d/save", film)
	var detail api.VodMovieDetail
	as("cy")
	if code := h.do("POST", save, nil, nil); code != 403 {
		t.Errorf("cy saved: %d", code)
	}
	h.do("GET", fmt.Sprintf("/livetv/vod/movies/%d", film), nil, &detail)
	if detail.CanSave != nil && *detail.CanSave {
		t.Error("cy is offered saving")
	}
	as("di")
	if code := h.do("GET", fmt.Sprintf("/livetv/vod/movies/%d", film), nil, &detail); code != 200 || detail.CanSave == nil || !*detail.CanSave {
		t.Errorf("di isn't offered saving: %d %v", code, detail.CanSave)
	}
	if code := h.do("POST", save+"?upgrade=true", nil, nil); code != 403 {
		t.Errorf("di upgraded: %d", code)
	}
	if code := h.do("POST", save, nil, nil); code != 200 {
		t.Errorf("di's save: %d", code)
	}
	if code := h.do("GET", "/livetv/vod/saves", nil, nil); code != 403 {
		t.Errorf("di sees the queue: %d", code)
	}
	var again api.VodMovieDetail
	h.do("GET", fmt.Sprintf("/livetv/vod/movies/%d", film), nil, &again)
	if again.Save == nil || again.Save.State != "queued" {
		t.Errorf("di's save state: %+v", again.Save)
	}
}
