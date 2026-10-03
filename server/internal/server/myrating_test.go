package server

import (
	"fmt"
	"path/filepath"
	"testing"

	"marquee/internal/api"
)

// Favourites: list a library by the person's own rating, or only what they rated highly.
func TestMyRatingSortAndFilter(t *testing.T) {
	h := newHarness(t)
	var res api.AuthResult
	h.do("POST", "/setup", map[string]any{"serverName": "T", "username": "admin", "password": "correct horse", "device": device}, &res)
	h.token = res.Token
	var lib api.Library
	h.do("POST", "/libraries", map[string]any{"name": "Music", "type": "music", "paths": []string{filepath.Join(h.media, "music")}}, &lib)
	ids := map[string]int64{}
	for _, title := range []string{"Alpha", "Bravo", "Charlie", "Delta"} {
		r, err := h.db.Exec(`INSERT INTO items(library_id, type, title, sort_title) VALUES (?, 'track', ?, ?)`, lib.Id, title, title)
		if err != nil {
			t.Fatal(err)
		}
		ids[title], _ = r.LastInsertId()
	}
	for title, rating := range map[string]int{"Alpha": 6, "Bravo": 10, "Charlie": 8} {
		h.do("PUT", fmt.Sprintf("/items/%d/rating", ids[title]), map[string]any{"rating": rating}, nil)
	}
	var page api.ItemPage
	h.do("GET", fmt.Sprintf("/libraries/%d/items?type=track&sort=-myRating", lib.Id), nil, &page)
	var order []string
	for _, it := range page.Items {
		order = append(order, it.Title)
	}
	if fmt.Sprint(order) != "[Bravo Charlie Alpha Delta]" {
		t.Errorf("by my rating: %v", order)
	}
	h.do("GET", fmt.Sprintf("/libraries/%d/items?type=track&sort=-myRating&minMyRating=8", lib.Id), nil, &page)
	if page.Total != 2 || page.Items[0].Title != "Bravo" {
		t.Errorf("favourites: %+v", page)
	}
}
