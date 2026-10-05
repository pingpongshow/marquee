package server

import (
	"fmt"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"marquee/internal/api"
)

// Changes made offline arrive later with the time they were made; an older one never undoes
// a newer change made elsewhere (USER-18).
func TestOfflineChangesKeepTheNewest(t *testing.T) {
	h := newHarness(t)
	var res api.AuthResult
	h.do("POST", "/setup", map[string]any{"serverName": "T", "username": "admin", "password": "correct horse", "device": device}, &res)
	h.token = res.Token
	var lib api.Library
	h.do("POST", "/libraries", map[string]any{"name": "Movies", "type": "movies", "paths": []string{filepath.Join(h.media, "video/Movies")}}, &lib)
	r, err := h.db.Exec(`INSERT INTO items(library_id, type, title, sort_title) VALUES (?, 'movie', 'Heat', 'Heat')`, lib.Id)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := r.LastInsertId()
	ago := func(d time.Duration) string { return time.Now().Add(-d).UTC().Format(time.RFC3339Nano) }
	detail := func() api.ItemDetail {
		var d api.ItemDetail
		h.do("GET", fmt.Sprintf("/items/%d", id), nil, &d)
		return d
	}

	// Rated 9 just now on another device; a 4 made offline an hour ago arrives later: ignored.
	h.do("PUT", fmt.Sprintf("/items/%d/rating", id), map[string]any{"rating": 9}, nil)
	if code := h.do("PUT", fmt.Sprintf("/items/%d/rating", id), map[string]any{"rating": 4, "at": ago(time.Hour)}, nil); code != 204 {
		t.Fatalf("stale rating: %d", code)
	}
	if d := detail(); d.UserRating == nil || *d.UserRating != 9 {
		t.Fatalf("an older offline rating replaced a newer one: %v", d.UserRating)
	}
	// A newer offline change applies.
	h.do("PUT", fmt.Sprintf("/items/%d/rating", id), map[string]any{"rating": 6, "at": time.Now().UTC().Format(time.RFC3339Nano)}, nil)
	if d := detail(); *d.UserRating != 6 {
		t.Errorf("newer offline rating: %v", *d.UserRating)
	}

	// Watched marks and the watchlist work the same way.
	h.do("DELETE", fmt.Sprintf("/items/%d/watched", id), nil, nil)
	h.do("POST", fmt.Sprintf("/items/%d/watched?at=%s", id, url.QueryEscape(ago(time.Hour))), nil, nil)
	if d := detail(); d.ViewCount != nil && *d.ViewCount > 0 {
		t.Error("an older offline 'watched' undid a newer 'unwatched'")
	}
	h.do("POST", fmt.Sprintf("/items/%d/watched?at=%s", id, url.QueryEscape(time.Now().UTC().Format(time.RFC3339Nano))), nil, nil)
	if d := detail(); d.ViewCount == nil || *d.ViewCount == 0 {
		t.Error("a newer offline 'watched' was dropped")
	}
	h.do("PUT", fmt.Sprintf("/items/%d/watchlist", id), nil, nil)
	h.do("DELETE", fmt.Sprintf("/items/%d/watchlist?at=%s", id, url.QueryEscape(ago(time.Minute))), nil, nil)
	if d := detail(); d.Watchlisted == nil || !*d.Watchlisted {
		t.Error("an older offline watchlist removal applied")
	}
}
