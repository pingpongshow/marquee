package server

import (
	"fmt"
	"path/filepath"
	"testing"

	"marquee/internal/api"
)

// Each person rates for themselves; everyone's ratings make the community rating, shown with comments.
func TestCommunityRatingsAndSongFilters(t *testing.T) {
	h := newHarness(t)
	var res api.AuthResult
	h.do("POST", "/setup", map[string]any{"serverName": "T", "username": "admin", "password": "correct horse", "device": device}, &res)
	admin := res.Token
	h.token = admin
	var lib api.Library
	h.do("POST", "/libraries", map[string]any{"name": "Music", "type": "music", "paths": []string{filepath.Join(h.media, "music")}}, &lib)
	exec := func(q string, args ...any) int64 {
		r, err := h.db.Exec(q, args...)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := r.LastInsertId()
		return id
	}
	artist := exec(`INSERT INTO items(library_id, type, title, sort_title) VALUES (?, 'artist', 'Band', 'Band')`, lib.Id)
	album := exec(`INSERT INTO items(library_id, type, title, sort_title, parent_id, year) VALUES (?, 'album', 'Record', 'Record', ?, 1994)`, lib.Id, artist)
	track := exec(`INSERT INTO items(library_id, type, title, sort_title, parent_id, grandparent_id) VALUES (?, 'track', 'Song', 'Song', ?, ?)`, lib.Id, album, artist)
	jazz := exec(`INSERT INTO tags(kind, name) VALUES ('genre', 'Jazz')`)
	exec(`INSERT INTO item_tags(item_id, tag_id) VALUES (?, ?)`, album, jazz)

	// Songs take their album's genre and year for filtering.
	var f api.LibraryFilters
	h.do("GET", fmt.Sprintf("/libraries/%d/filters?type=track", lib.Id), nil, &f)
	if len(f.Genres) != 1 || f.Genres[0].Value != "Jazz" || len(f.Decades) != 1 || f.Decades[0].Value != "1990" {
		t.Fatalf("song facets: %+v", f)
	}
	var page api.ItemPage
	h.do("GET", fmt.Sprintf("/libraries/%d/items?type=track&genre=Jazz&decade=1990", lib.Id), nil, &page)
	if page.Total != 1 {
		t.Fatalf("song filter: %+v", page)
	}

	// Two people rate; one comments.
	var bo api.User
	h.do("POST", "/users", map[string]any{"username": "bo", "password": "bo's password"}, &bo)
	h.do("PUT", fmt.Sprintf("/items/%d/rating", track), map[string]any{"rating": 8}, nil)
	h.do("PUT", fmt.Sprintf("/items/%d/reviews", track), map[string]any{"comment": "Great on vinyl"}, nil)
	h.token = ""
	h.do("POST", "/auth/login", map[string]any{"username": "bo", "password": "bo's password", "device": device}, &res)
	h.token = res.Token
	h.do("PUT", fmt.Sprintf("/items/%d/rating", track), map[string]any{"rating": 6}, nil)

	var it api.ItemDetail
	h.do("GET", fmt.Sprintf("/items/%d", track), nil, &it)
	if it.UserRating == nil || *it.UserRating != 6 || it.CommunityRating == nil || it.CommunityRating.Average != 7 || it.CommunityRating.Count != 2 {
		t.Fatalf("bo's view: mine %v community %+v", it.UserRating, it.CommunityRating)
	}
	var rv api.ItemReviews
	h.do("GET", fmt.Sprintf("/items/%d/reviews", track), nil, &rv)
	if rv.Count != 2 || len(rv.Reviews) != 2 || rv.Reviews[0].Comment == nil || *rv.Reviews[0].Comment != "Great on vinyl" || rv.Reviews[0].Mine || !rv.Reviews[1].Mine {
		t.Fatalf("reviews: %+v", rv)
	}
	// Only the author or an admin removes a comment.
	if code := h.do("DELETE", fmt.Sprintf("/items/%d/reviews/%d", track, rv.Reviews[0].UserId), nil, nil); code != 403 {
		t.Errorf("bo deleted the admin's comment: %d", code)
	}
	h.token = admin
	if code := h.do("DELETE", fmt.Sprintf("/items/%d/reviews/%d", track, rv.Reviews[0].UserId), nil, nil); code != 204 {
		t.Errorf("admin delete: %d", code)
	}
	var after api.ItemReviews
	h.do("GET", fmt.Sprintf("/items/%d/reviews", track), nil, &after)
	if after.Count != 2 || after.Reviews[0].Comment != nil || after.Reviews[1].Comment != nil {
		t.Errorf("after delete: %+v (ratings stay)", after)
	}
}
