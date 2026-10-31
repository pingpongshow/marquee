package server

import (
	"context"
	"fmt"
	"testing"
	"time"

	"marquee/internal/api"
)

// Home suggestions (USER-16) through the API: Not interested takes a title off Home and shows
// on its page; Home records what it showed, plays soon after count for the row in
// Statistics, and a row shown plenty without a play sinks below the others.
func TestHomeSuggestionsAPI(t *testing.T) {
	h := newHarness(t)
	var res api.AuthResult
	h.do("POST", "/setup", map[string]any{"serverName": "T", "username": "admin", "password": "correct horse", "device": device}, &res)
	h.token = res.Token
	var lib api.Library
	h.do("POST", "/libraries", map[string]any{"name": "Movies", "type": "movies", "paths": []string{h.media + "/video/Movies"}}, &lib)
	add := func(title string, rating float64) int64 {
		r, err := h.db.Exec(`INSERT INTO items(library_id, type, title, sort_title, year, audience_rating, added_at)
			VALUES (?, 'movie', ?, ?, 2000, ?, '2020-01-01T00:00:00.000Z')`, lib.Id, title, title, rating)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := r.LastInsertId()
		return id
	}
	great := add("Great", 9)
	good := add("Good", 8)

	topRated := func() []string {
		var hubs []api.Hub
		h.do("GET", "/hubs/home", nil, &hubs)
		for _, hub := range hubs {
			if hub.Id == "top-rated" {
				var out []string
				for _, it := range hub.Items {
					out = append(out, it.Title)
				}
				return out
			}
		}
		return nil
	}
	if got := fmt.Sprint(topRated()); got != "[Great Good]" {
		t.Fatalf("top rated: %s", got)
	}
	if code := h.do("PUT", fmt.Sprintf("/items/%d/not-interested", great), nil, nil); code != 204 {
		t.Fatalf("not interested: %d", code)
	}
	if code := h.do("PUT", "/items/99999/not-interested", nil, nil); code != 404 {
		t.Errorf("no such item: %d", code)
	}
	var d api.ItemDetail
	h.do("GET", fmt.Sprintf("/items/%d", great), nil, &d)
	if d.NotInterested == nil || !*d.NotInterested {
		t.Errorf("item page: %v", d.NotInterested)
	}
	if got := fmt.Sprint(topRated()); got != "[Good]" {
		t.Fatalf("after not interested: %s", got)
	}
	h.do("DELETE", fmt.Sprintf("/items/%d/not-interested", great), nil, nil)
	if got := fmt.Sprint(topRated()); got != "[Great Good]" {
		t.Fatalf("undone: %s", got)
	}

	// Home recorded what it showed (in the background).
	var uid int64
	h.db.QueryRow("SELECT id FROM users WHERE username='admin'").Scan(&uid)
	shown := 0
	for i := 0; i < 50 && shown == 0; i++ {
		h.db.QueryRow(`SELECT COUNT(*) FROM home_shown WHERE user_id = ? AND row_id = 'top-rated'`, uid).Scan(&shown)
		if shown == 0 {
			time.Sleep(20 * time.Millisecond)
		}
	}
	if shown != 2 {
		t.Fatalf("shown: %d", shown)
	}
	if got := h.handlers.Items.HomeContext(context.Background(), uid, good); got != "home:top-rated" && got != "home:hidden-gems" {
		t.Errorf("a play now counts for: %q", got) // it's in both rows
	}
	h.db.Exec(`INSERT INTO play_history(user_id, item_id, item_title, started_at, context) VALUES (?, ?, 'Good', ?, 'home:top-rated')`,
		uid, good, time.Now().UTC().Format("2006-01-02T15:04:05.000Z"))
	var st api.Stats
	h.do("GET", "/stats", nil, &st)
	found := false
	if st.HomeRows != nil {
		for _, r := range *st.HomeRows {
			if r.Row == "top-rated" && r.Label == "Top Rated" && r.Plays == 1 && r.Shown >= 2 {
				found = true
			}
		}
	}
	if !found {
		t.Errorf("stats: %+v", st.HomeRows)
	}

	// Classics shown 40 times in a month and never played from: it sinks below the other
	// suggestion rows (Recently Added stays last).
	for i := 0; i < 40; i++ {
		id := add(fmt.Sprintf("Classic %d", i), 5)
		h.db.Exec(`INSERT INTO home_shown(user_id, item_id, row_id, shown_at) VALUES (?, ?, 'classics', ?)`, uid, id,
			time.Now().UTC().Format("2006-01-02T15:04:05.000Z"))
	}
	var layout api.HomeLayout
	h.do("GET", "/me/home-layout", nil, &layout)
	pos := map[string]int{}
	for i, r := range layout.Rows {
		pos[r.Id] = i
	}
	recent := fmt.Sprintf("recent-%d", lib.Id)
	if pos["classics"] < pos["watch-again"] || pos["classics"] < pos["international"] || pos[recent] < pos["classics"] {
		t.Errorf("classics didn't sink: %v", pos)
	}
}
