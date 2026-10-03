package server

import (
	"fmt"
	"path/filepath"
	"testing"

	"marquee/internal/api"
)

func TestSmartCollectionsHomeLayoutCinema(t *testing.T) {
	h := newHarness(t)
	var res api.AuthResult
	h.do("POST", "/setup", map[string]any{"serverName": "T", "username": "admin", "password": "correct horse", "device": device}, &res)
	h.token = res.Token
	var lib api.Library
	h.do("POST", "/libraries", map[string]any{"name": "Movies", "type": "movies", "paths": []string{filepath.Join(h.media, "video/Movies")}}, &lib)
	exec := func(q string, args ...any) int64 {
		r, err := h.db.Exec(q, args...)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := r.LastInsertId()
		return id
	}
	matrix := exec(`INSERT INTO items(library_id, type, title, sort_title, year, audience_rating) VALUES (?, 'movie', 'The Matrix', 'Matrix', 1999, 8.7)`, lib.Id)
	monkeys := exec(`INSERT INTO items(library_id, type, title, sort_title, year, audience_rating) VALUES (?, 'movie', '12 Monkeys', '12 Monkeys', 1995, 7.6)`, lib.Id)
	alien := exec(`INSERT INTO items(library_id, type, title, sort_title, year) VALUES (?, 'movie', 'Alien', 'Alien', 1979)`, lib.Id)

	// Smart collections (META-7).
	var sc api.ItemSummary
	if code := h.do("POST", fmt.Sprintf("/libraries/%d/smart-collections", lib.Id), map[string]any{"title": "90s",
		"rules": map[string]any{"itemType": "movie", "yearFrom": 1990, "yearTo": 1999, "sort": "-year"}}, &sc); code != 201 || sc.Type != "collection" {
		t.Fatalf("create: %d %+v", code, sc)
	}
	var page api.ItemPage
	h.do("GET", fmt.Sprintf("/items/%d/children", sc.Id), nil, &page)
	if page.Total != 2 || page.Items[0].Id != matrix || page.Items[1].Id != monkeys {
		t.Fatalf("members: %+v", page)
	}
	var d api.ItemDetail
	h.do("GET", fmt.Sprintf("/items/%d", sc.Id), nil, &d)
	if d.SmartRules == nil || *d.SmartRules.YearFrom != 1990 || d.ChildCount != 2 {
		t.Fatalf("detail: %+v", d.SmartRules)
	}
	h.do("PUT", fmt.Sprintf("/collections/%d/rules", sc.Id), map[string]any{"rules": map[string]any{"itemType": "movie", "minRating": 8}}, nil)
	h.do("GET", fmt.Sprintf("/items/%d/children", sc.Id), nil, &page)
	if page.Total != 1 || page.Items[0].Id != matrix {
		t.Fatalf("after rule change: %+v", page)
	}
	if code := h.do("POST", fmt.Sprintf("/libraries/%d/smart-collections", lib.Id), map[string]any{"title": "x",
		"rules": map[string]any{"itemType": "track"}}, nil); code != 400 {
		t.Errorf("tracks accepted: %d", code)
	}

	// Home layout (USER-12): reorder, hide, pin; unknown rows dropped; new rows appended.
	recent := fmt.Sprintf("recent-%d", lib.Id)
	pin := fmt.Sprintf("collection-%d", sc.Id)
	var layout api.HomeLayout
	h.do("GET", "/me/home-layout", nil, &layout)
	if len(layout.Rows) != 5 || layout.Rows[0].Id != "continue-watching" || layout.Rows[2].Id != "recommended" {
		t.Fatalf("default layout: %+v", layout)
	}
	h.do("PUT", "/me/home-layout", map[string]any{"rows": []map[string]any{{"id": recent}, {"id": pin}, {"id": "watchlist", "hidden": true}, {"id": "nope"}}}, &layout)
	var ids []string
	for _, r := range layout.Rows {
		ids = append(ids, r.Id)
	}
	if fmt.Sprint(ids) != fmt.Sprintf("[%s %s watchlist continue-watching recommended because-you-watched]", recent, pin) || !*layout.Rows[1].Pinned || *layout.Rows[1].Title != "90s" || !*layout.Rows[2].Hidden {
		t.Fatalf("saved layout: %v %+v", ids, layout.Rows)
	}
	var hubs []api.Hub
	h.do("GET", "/hubs/home", nil, &hubs)
	if len(hubs) != 2 || hubs[0].Id != recent || hubs[1].Id != pin || len(hubs[1].Items) != 1 {
		t.Fatalf("hubs: %+v", hubs)
	}
	h.do("PUT", "/me/home-layout", map[string]any{"rows": []any{}}, &layout)
	if layout.Rows[0].Id != "continue-watching" || len(layout.Rows) != 5 {
		t.Fatalf("reset: %+v", layout)
	}

	// Cinema trailers (PLAY-18): other movies' trailers, then the pre-roll.
	exec(`INSERT INTO items(library_id, type, title, sort_title, parent_id, extra_type) VALUES (?, 'movie', 'Matrix Trailer', 'Matrix Trailer', ?, 'trailer')`, lib.Id, matrix)
	exec(`INSERT INTO items(library_id, type, title, sort_title, parent_id, extra_type) VALUES (?, 'movie', 'Alien Trailer', 'Alien Trailer', ?, 'trailer')`, lib.Id, alien)
	exec(`INSERT INTO items(library_id, type, title, sort_title, parent_id, extra_type) VALUES (?, 'movie', 'Monkeys Trailer', 'Monkeys Trailer', ?, 'trailer')`, lib.Id, monkeys)
	var rolls []api.ItemSummary
	h.do("GET", fmt.Sprintf("/items/%d/prerolls", matrix), nil, &rolls)
	if len(rolls) != 0 {
		t.Fatalf("off by default: %+v", rolls)
	}
	intro := exec(`INSERT INTO items(library_id, type, title, sort_title) VALUES (?, 'movie', 'Intro', 'Intro')`, lib.Id)
	h.do("PATCH", "/settings", map[string]any{"cinema": map[string]any{"trailers": 2, "prerollItemId": intro}}, nil)
	h.do("GET", fmt.Sprintf("/items/%d/prerolls", matrix), nil, &rolls)
	if len(rolls) != 3 || rolls[2].Id != intro || rolls[0].Title == "Matrix Trailer" || rolls[1].Title == "Matrix Trailer" {
		t.Fatalf("prerolls: %+v", rolls)
	}
	// A profile limited to PG gets only trailers of films it may watch.
	exec(`UPDATE items SET content_rating = 'R' WHERE id = ?`, alien)
	exec(`UPDATE items SET content_rating = 'PG' WHERE id = ?`, monkeys)
	var kid api.User
	h.do("POST", "/users", map[string]any{"username": "kid", "isManaged": true, "restrictions": map[string]any{"maxContentRating": "PG"}}, &kid)
	var sw api.AuthResult
	h.do("POST", fmt.Sprintf("/profiles/%d/switch", kid.Id), map[string]any{"device": map[string]any{"clientId": "kid-client", "name": "Kid", "platform": "web"}}, &sw)
	h.token = sw.Token
	exec(`UPDATE items SET content_rating = 'PG' WHERE id = ?`, matrix)
	h.do("GET", fmt.Sprintf("/items/%d/prerolls", matrix), nil, &rolls)
	for _, r := range rolls {
		if r.Title == "Alien Trailer" {
			t.Errorf("R-rated trailer for a PG profile: %+v", rolls)
		}
	}
	if len(rolls) == 0 || rolls[0].Title != "Monkeys Trailer" {
		t.Errorf("kid prerolls: %+v", rolls)
	}
	// Switching signed this device out of the admin profile.
	h.token = ""
	h.do("POST", "/auth/login", map[string]any{"username": "admin", "password": "correct horse", "device": device}, &sw)
	h.token = sw.Token
	h.do("PATCH", "/me", map[string]any{"preferences": map[string]any{"cinemaTrailers": false}}, nil)
	h.do("GET", fmt.Sprintf("/items/%d/prerolls", matrix), nil, &rolls)
	if len(rolls) != 0 {
		t.Fatalf("opted out: %+v", rolls)
	}
}

func TestInvites(t *testing.T) {
	h := newHarness(t)
	var res api.AuthResult
	h.do("POST", "/setup", map[string]any{"serverName": "Home", "username": "admin", "password": "correct horse", "device": device}, &res)
	admin := res.Token
	h.token = admin
	h.do("POST", "/users", map[string]any{"username": "kid", "isManaged": true}, nil)
	var lib api.Library
	h.do("POST", "/libraries", map[string]any{"name": "Movies", "type": "movies", "paths": []string{filepath.Join(h.media, "video/Movies")}}, &lib)
	var inv api.InviteCreated
	if code := h.do("POST", "/invites", map[string]any{"note": "Sam", "restrictions": map[string]any{"libraryIds": []int64{lib.Id}}}, &inv); code != 201 || inv.Path != "/join/"+inv.Token {
		t.Fatalf("create: %d %+v", code, inv)
	}
	h.token = ""
	var info api.InviteInfo
	if code := h.do("GET", "/invitations/"+inv.Token, nil, &info); code != 200 || info.ServerName != "Home" || *info.InvitedBy != "admin" {
		t.Fatalf("info: %d %+v", code, info)
	}
	join := map[string]any{"username": "sam", "password": "sam's password", "device": device}
	if code := h.do("POST", "/invitations/"+inv.Token+"/accept", join, &res); code != 200 || !*res.User.Restrictions.Friend {
		t.Fatalf("accept: %d %+v", code, res.User)
	}
	sam := res.Token
	if code := h.do("POST", "/invitations/"+inv.Token+"/accept", map[string]any{"username": "sam2", "password": "sam's password", "device": device}, nil); code != 404 {
		t.Errorf("reused: %d", code)
	}
	// Not on the household's sign-in picker or profile switcher; can't switch into the household.
	var profiles []api.Profile
	h.do("GET", "/auth/profiles", nil, &profiles)
	for _, p := range profiles {
		if p.DisplayName == "sam" {
			t.Error("friend on the sign-in picker")
		}
	}
	h.token = sam
	h.do("GET", "/profiles", nil, &profiles)
	if len(profiles) != 1 || profiles[0].DisplayName != "sam" {
		t.Errorf("friend's profiles: %+v", profiles)
	}
	var kid int64
	h.db.QueryRow(`SELECT id FROM users WHERE username = 'kid'`).Scan(&kid)
	if code := h.do("POST", fmt.Sprintf("/profiles/%d/switch", kid), map[string]any{"device": device}, nil); code != 404 {
		t.Errorf("friend switched to a household profile: %d", code)
	}
	h.token = admin
	h.do("GET", "/profiles", nil, &profiles)
	for _, p := range profiles {
		if p.DisplayName == "sam" {
			t.Error("friend on the household's switcher")
		}
	}
	var list []api.Invite
	h.do("GET", "/invites", nil, &list)
	if len(list) != 1 || list[0].UsedBy == nil || *list[0].UsedBy != "sam" {
		t.Fatalf("list: %+v", list)
	}
	// An admin editing the friend from an older app keeps them a friend.
	var samID int64
	h.db.QueryRow(`SELECT id FROM users WHERE username = 'sam'`).Scan(&samID)
	var u api.User
	h.do("PATCH", fmt.Sprintf("/users/%d", samID), map[string]any{"restrictions": map[string]any{"canRequest": true}}, &u)
	if u.Restrictions.Friend == nil || !*u.Restrictions.Friend {
		t.Error("friend flag dropped by an edit")
	}
	// Guessing tokens is rate limited.
	h.token = ""
	code := 0
	for i := 0; i < 12; i++ {
		code = h.do("GET", "/invitations/nope", nil, nil)
	}
	if code != 429 {
		t.Errorf("guessing not limited: %d", code)
	}
}
