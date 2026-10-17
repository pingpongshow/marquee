package server

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"marquee/internal/api"
	"marquee/internal/soundprint"
)

// Favourites (the heart): set and clear, per user, only on what the user can see; the
// favorite field in item and list responses; the favourites filter on every browse list and
// search; the Home row; the Favourites mix.
func TestFavorites(t *testing.T) {
	h := newHarness(t)
	h.handlers.Soundprint = &soundprint.Service{DB: h.db, Index: soundprint.NewIndex()}
	var res api.AuthResult
	h.do("POST", "/setup", map[string]any{"serverName": "T", "username": "admin", "password": "correct horse", "device": device}, &res)
	h.token = res.Token
	admin := res.Token

	var mlib, slib, alib api.Library
	h.do("POST", "/libraries", map[string]any{"name": "Movies", "type": "movies", "paths": []string{filepath.Join(h.media, "video/Movies")}}, &mlib)
	h.do("POST", "/libraries", map[string]any{"name": "Shows", "type": "shows", "paths": []string{filepath.Join(h.media, "video/Shows")}}, &slib)
	h.do("POST", "/libraries", map[string]any{"name": "Music", "type": "music", "paths": []string{filepath.Join(h.media, "music")}}, &alib)
	add := func(lib int64, typ, title string, parent, grandparent int64) int64 {
		t.Helper()
		var p, g any
		if parent != 0 {
			p = parent
		}
		if grandparent != 0 {
			g = grandparent
		}
		r, err := h.db.Exec(`INSERT INTO items(library_id, type, title, sort_title, parent_id, grandparent_id, idx) VALUES (?, ?, ?, ?, ?, ?, 1)`,
			lib, typ, title, title, p, g)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := r.LastInsertId()
		return id
	}
	alien := add(mlib.Id, "movie", "Alien", 0, 0)
	brazil := add(mlib.Id, "movie", "Brazil", 0, 0)
	add(mlib.Id, "movie", "Casablanca", 0, 0)
	show := add(slib.Id, "show", "Dark", 0, 0)
	season := add(slib.Id, "season", "Season 1", show, 0)
	season2 := add(slib.Id, "season", "Season 2", show, 0)
	add(slib.Id, "episode", "Secrets", season, show)
	lies := add(slib.Id, "episode", "Lies", season, show)
	add(slib.Id, "episode", "Past and Present", season2, show)
	artist := add(alib.Id, "artist", "Echo Band", 0, 0)
	album := add(alib.Id, "album", "First Album", artist, 0)
	add(alib.Id, "album", "Second Album", artist, 0)
	var tracks []int64
	for _, n := range []string{"Song One", "Song Two", "Song Three", "Song Four"} {
		tracks = append(tracks, add(alib.Id, "track", n, album, artist))
	}

	fav := func(id int64, on bool) int {
		t.Helper()
		method := "PUT"
		if !on {
			method = "DELETE"
		}
		return h.do(method, fmt.Sprintf("/items/%d/favorite", id), nil, nil)
	}
	isFav := func(id int64) bool {
		t.Helper()
		var d api.ItemDetail
		if code := h.do("GET", fmt.Sprintf("/items/%d", id), nil, &d); code != 200 {
			t.Fatalf("get %d: %d", id, code)
		}
		return d.Favorite != nil && *d.Favorite
	}
	titles := func(path string) string {
		t.Helper()
		var page api.ItemPage
		if code := h.do("GET", path, nil, &page); code != 200 {
			t.Fatalf("%s: %d", path, code)
		}
		var out []string
		for _, it := range page.Items {
			if it.Favorite == nil || !*it.Favorite {
				t.Errorf("%s: %s listed without favorite set", path, it.Title)
			}
			out = append(out, it.Title)
		}
		return strings.Join(out, ",")
	}

	// Set and clear.
	if code := fav(alien, true); code != 204 {
		t.Fatalf("favourite: %d", code)
	}
	if !isFav(alien) {
		t.Fatal("favorite not set on the item")
	}
	if code := fav(alien, false); code != 204 || isFav(alien) {
		t.Fatalf("clear: %d", code)
	}
	if code := fav(999999, true); code != 404 {
		t.Fatalf("unknown item: %d", code)
	}
	for _, id := range append([]int64{brazil, alien, show, lies, album, artist}, tracks[:3]...) {
		if code := fav(id, true); code != 204 {
			t.Fatalf("favourite %d: %d", id, code)
		}
	}
	fav(brazil, true) // again: keeps the original time
	h.db.Exec(`UPDATE user_item_state SET favorite_at = '2026-01-01T00:00:00.000Z' WHERE item_id = ?`, alien)

	// Browse: each type, combined with filters and the date-favourited sort.
	if got := titles(fmt.Sprintf("/libraries/%d/items?favorites=true", mlib.Id)); got != "Alien,Brazil" {
		t.Errorf("movies: %s", got)
	}
	if got := titles(fmt.Sprintf("/libraries/%d/items?favorites=true&sort=-favorited", mlib.Id)); got != "Brazil,Alien" {
		t.Errorf("movies, newest favourite first: %s", got)
	}
	if got := titles(fmt.Sprintf("/libraries/%d/items?favorites=true&letter=B", mlib.Id)); got != "Brazil" {
		t.Errorf("movies with a letter: %s", got)
	}
	if got := titles(fmt.Sprintf("/libraries/%d/items?favorites=true", slib.Id)); got != "Dark" {
		t.Errorf("shows: %s", got)
	}
	if got := titles(fmt.Sprintf("/libraries/%d/items?favorites=true&type=episode", slib.Id)); got != "Lies" {
		t.Errorf("episodes: %s", got)
	}
	if got := titles(fmt.Sprintf("/items/%d/children?favorites=true", season)); got != "Lies" {
		t.Errorf("season's episodes: %s", got)
	}
	var page api.ItemPage
	h.do("GET", fmt.Sprintf("/items/%d/children?favorites=true", show), nil, &page)
	if page.Total != 1 || page.Items[0].Id != season {
		t.Errorf("show's seasons with a favourite episode: %+v", page)
	}
	var leaves []api.ItemSummary
	h.do("GET", fmt.Sprintf("/items/%d/leaves?favorites=true", show), nil, &leaves)
	if len(leaves) != 1 || leaves[0].Id != lies {
		t.Errorf("show's favourite episodes: %+v", leaves)
	}
	if got := titles(fmt.Sprintf("/libraries/%d/items?favorites=true&type=track", alib.Id)); got != "Song One,Song Three,Song Two" {
		t.Errorf("tracks: %s", got)
	}
	if got := titles(fmt.Sprintf("/libraries/%d/items?favorites=true&type=album", alib.Id)); got != "First Album" {
		t.Errorf("albums: %s", got)
	}
	if got := titles(fmt.Sprintf("/libraries/%d/items?favorites=true", alib.Id)); got != "Echo Band" {
		t.Errorf("artists: %s", got)
	}
	// Unfiltered lists carry the field too.
	h.do("GET", fmt.Sprintf("/items/%d/children", album), nil, &page)
	n := 0
	for _, it := range page.Items {
		if it.Favorite != nil && *it.Favorite {
			n++
		}
	}
	if page.Total != 4 || n != 3 {
		t.Errorf("album tracks: %d of %d favourites", n, page.Total)
	}

	// Search.
	var sr api.SearchResults
	h.do("GET", "/search?q=song&favorites=true", nil, &sr)
	if len(sr.Groups) != 1 || len(sr.Groups[0].Items) != 3 {
		t.Errorf("favourite search: %+v", sr.Groups)
	}
	h.do("GET", "/search?q=song", nil, &sr)
	if len(sr.Groups) != 1 || len(sr.Groups[0].Items) != 4 {
		t.Errorf("search: %+v", sr.Groups)
	}

	// Home: the Favourites row is hidden until shown, then lists movies and shows, newest first.
	hubs := func() map[string][]string {
		var list []api.Hub
		h.do("GET", "/hubs/home", nil, &list)
		out := map[string][]string{}
		for _, hb := range list {
			for _, it := range hb.Items {
				out[hb.Id] = append(out[hb.Id], it.Title)
			}
		}
		return out
	}
	if _, ok := hubs()["favorites"]; ok {
		t.Error("Favourites row shown before the person turned it on")
	}
	var layout api.HomeLayout
	h.do("GET", "/me/home-layout", nil, &layout)
	found := false
	for i, r := range layout.Rows {
		if r.Id == "favorites" {
			found = r.Hidden != nil && *r.Hidden
			layout.Rows[i].Hidden = ptrBool(false)
		}
	}
	if !found {
		t.Fatalf("layout has no hidden favorites row: %+v", layout.Rows)
	}
	if code := h.do("PUT", "/me/home-layout", layout, nil); code != 200 && code != 204 {
		t.Fatalf("save layout: %d", code)
	}
	if got := strings.Join(hubs()["favorites"], ","); got != "Dark,Brazil,Alien" {
		t.Errorf("Favourites row: %s", got)
	}

	// The Favourites mix: favourite songs, shuffled, without any analysis.
	var mixes []api.Station
	h.do("GET", fmt.Sprintf("/music/mixes?libraryId=%d", alib.Id), nil, &mixes)
	if len(mixes) == 0 || mixes[0].Id == nil || *mixes[0].Id != "favorites" || len(mixes[0].Items) != 3 {
		t.Fatalf("favourites mix: %+v", mixes)
	}
	for _, it := range mixes[0].Items {
		if it.Favorite == nil || !*it.Favorite {
			t.Errorf("mix has a non-favourite: %s", it.Title)
		}
	}

	// Another user: their own favourites, and nothing they can't see.
	var friend api.User
	h.do("POST", "/users", map[string]any{"username": "friend", "password": "friend-password",
		"restrictions": map[string]any{"libraryIds": []int64{mlib.Id}}}, &friend)
	h.token = ""
	var fr api.AuthResult
	h.do("POST", "/auth/login", map[string]any{"username": "friend", "password": "friend-password", "device": device}, &fr)
	h.token = fr.Token
	if isFav(alien) {
		t.Error("the admin's favourite shows as the friend's")
	}
	if code := fav(tracks[3], true); code != 404 {
		t.Errorf("favourite in a hidden library: %d", code)
	}
	if code := fav(tracks[0], false); code != 404 {
		t.Errorf("clear in a hidden library: %d", code)
	}
	if code := fav(brazil, true); code != 204 {
		t.Fatalf("friend favourite: %d", code)
	}
	if got := titles(fmt.Sprintf("/libraries/%d/items?favorites=true", mlib.Id)); got != "Brazil" {
		t.Errorf("friend's favourites: %s", got)
	}
	h.token = admin
	if got := titles(fmt.Sprintf("/libraries/%d/items?favorites=true", mlib.Id)); got != "Alien,Brazil" {
		t.Errorf("admin's favourites after the friend's: %s", got)
	}
}

func ptrBool(b bool) *bool { return &b }
