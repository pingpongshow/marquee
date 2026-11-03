package server

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"marquee/internal/api"
	"marquee/internal/intown"
	"marquee/internal/settings"
)

const inTownTestKey = "secret-consumer-key"

// body is do, for tests about what's in the response's bytes.
func (h *harness) body(path string) (int, string) {
	h.t.Helper()
	req, _ := http.NewRequest("GET", h.srv.URL+"/api/v1"+path, nil)
	if h.token != "" {
		req.Header.Set("Authorization", "Bearer "+h.token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

// In Town (MUSIC-27) through the API: off by default and saying so, the key never comes back to
// anyone, and a location is its owner's alone.
func TestInTownAPI(t *testing.T) {
	h := newHarness(t)
	tm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"_embedded":{"events":[{"id":"E1","name":"Night Out","url":"https://www.ticketmaster.com/e/1",
			"dates":{"start":{"localDate":"2099-11-20","localTime":"19:30:00","dateTime":"2099-11-20T19:30:00Z"},"timezone":"Europe/London","status":{"code":"onsale"}},
			"_embedded":{"venues":[{"name":"O2","city":{"name":"London"},"country":{"countryCode":"GB"},"location":{"latitude":"51.5","longitude":"0.0"}}],
			"attractions":[{"id":"K1","name":"Test Band","classifications":[{"segment":{"name":"Music"}}]}]}}]},"page":{"totalPages":1}}`)
	}))
	t.Cleanup(tm.Close)
	h.handlers.InTown = &intown.Service{Path: filepath.Join(t.TempDir(), "cache", "in-town.db"), Items: h.handlers.Items, Base: tm.URL,
		Settings: func() settings.InTown { return h.store.Get().InTown }}

	var res api.AuthResult
	h.do("POST", "/setup", map[string]any{"serverName": "T", "username": "admin", "password": "correct horse", "device": device}, &res)
	admin := res.Token
	h.token = admin
	var kid api.User
	if code := h.do("POST", "/users", map[string]any{"username": "kiddo", "password": "correct horse"}, &kid); code >= 300 {
		t.Fatalf("create user: %d", code)
	}
	h.token = ""
	h.do("POST", "/auth/login", map[string]any{"username": "kiddo", "password": "correct horse", "device": device}, &res)
	kiddo := res.Token

	// Off by default, and saying so.
	h.token = kiddo
	var page api.InTownPage
	if code := h.do("GET", "/music/in-town", nil, &page); code != 200 || page.Enabled || !page.NeedsLocation || len(page.Near) != 0 {
		t.Fatalf("off: %d %+v", code, page)
	}
	var info api.SystemInfo
	h.do("GET", "/system/info", nil, &info)
	if info.InTown == nil || *info.InTown {
		t.Errorf("system info while off: %+v", info.InTown)
	}

	// Turning it on needs a key; only admins may.
	if code := h.do("PATCH", "/settings", map[string]any{"inTown": map[string]any{"enabled": true, "ticketmasterKey": inTownTestKey}}, nil); code != 403 {
		t.Errorf("a non-admin changed settings: %d", code)
	}
	h.token = admin
	if code := h.do("PATCH", "/settings", map[string]any{"inTown": map[string]any{"enabled": true}}, nil); code != 400 {
		t.Errorf("on without a key: %d", code)
	}
	var saved api.ServerSettings
	if code := h.do("PATCH", "/settings", map[string]any{"inTown": map[string]any{"enabled": true, "ticketmasterKey": " " + inTownTestKey + " "}}, &saved); code != 200 {
		t.Fatalf("turn on: %d", code)
	}
	if !*saved.InTown.Enabled || !*saved.InTown.TicketmasterKeySet || *saved.InTown.DailyBudget != 2000 {
		t.Errorf("saved: %+v", saved.InTown)
	}
	if h.store.Get().InTown.TicketmasterKey != inTownTestKey {
		t.Error("the key wasn't trimmed")
	}
	if code := h.do("PATCH", "/settings", map[string]any{"inTown": map[string]any{"dailyBudget": 50000}}, nil); code != 400 {
		t.Errorf("a budget past the quota: %d", code)
	}

	// The key: not to admins beyond whether it's set, not to anyone else at all.
	for _, path := range []string{"/settings", "/music/in-town/status", "/music/in-town", "/me", "/system/info", "/users"} {
		if _, body := h.body(path); strings.Contains(body, inTownTestKey) {
			t.Errorf("the key is in %s, for an admin", path)
		}
	}
	h.token = kiddo
	if code, _ := h.body("/settings"); code != 403 {
		t.Errorf("settings for a non-admin: %d", code)
	}
	if code, _ := h.body("/music/in-town/status"); code != 403 {
		t.Errorf("status for a non-admin: %d", code)
	}
	for _, path := range []string{"/music/in-town", "/me", "/system/info"} {
		if _, body := h.body(path); strings.Contains(body, inTownTestKey) {
			t.Errorf("the key is in %s, for a non-admin", path)
		}
	}

	// A location: checked, kept and shown only to its owner.
	if code := h.do("PUT", "/me/location", map[string]any{"countryCode": "GB"}, nil); code != 400 {
		t.Errorf("no postcode or city: %d", code)
	}
	if code := h.do("PUT", "/me/location", map[string]any{"countryCode": "GB", "postalCode": "SW1A", "city": "London"}, nil); code != 400 {
		t.Errorf("a postcode and a city: %d", code)
	}
	var loc api.InTownLocation
	if code := h.do("PUT", "/me/location", map[string]any{"postalCode": "sw1a 1aa", "countryCode": "gb"}, &loc); code != 200 {
		t.Fatalf("set: %d", code)
	}
	if *loc.PostalCode != "SW1A" || loc.CountryCode != "GB" || *loc.Distance != 50 || *loc.Unit != api.InTownLocationUnitMiles {
		t.Errorf("location: %+v", loc)
	}
	// Apps that save the preferences they know about mustn't wipe it.
	if code := h.do("PATCH", "/me", map[string]any{"preferences": map[string]any{"audioLanguage": "eng"}}, nil); code != 200 {
		t.Fatalf("preferences: %d", code)
	}
	var state api.InTownLocationState
	h.do("GET", "/me/location", nil, &state)
	if state.Location == nil || *state.Location.PostalCode != "SW1A" {
		t.Errorf("after saving preferences: %+v", state.Location)
	}
	h.token = admin
	if _, body := h.body("/users"); strings.Contains(body, "SW1A") {
		t.Error("someone's location is in the user list")
	}
	h.token = kiddo

	// The page, on: shows near them, with the times an app needs and no web formatting.
	if code := h.do("GET", "/music/in-town", nil, &page); code != 200 || !page.Enabled || page.NeedsLocation {
		t.Fatalf("on: %d %+v", code, page)
	}
	if len(page.Near) != 1 {
		t.Fatalf("near: %+v", page.Near)
	}
	s := page.Near[0]
	if s.ArtistName != "Test Band" || s.LocalDate != "2099-11-20" || *s.LocalTime != "19:30" || *s.TimeZone != "Europe/London" || s.StartsAt == nil ||
		s.VenueName != "O2" || s.Source != "ticketmaster" || s.TicketsUrl == "" || s.Reason == nil || s.ReasonKind == nil {
		t.Errorf("show: %+v", s)
	}
	if page.Attribution.Name != "Ticketmaster" {
		t.Errorf("attribution: %+v", page.Attribution)
	}

	// An artist's shows: 404 outside what they can see, like any item.
	h.token = admin
	var lib, dad api.Library
	h.do("POST", "/libraries", map[string]any{"name": "Music", "type": "music", "paths": []string{filepath.Join(h.media, "music")}}, &lib)
	h.do("POST", "/libraries", map[string]any{"name": "Dad", "type": "music", "paths": []string{filepath.Join(h.media, "video/Movies")}}, &dad)
	r, err := h.db.Exec(`INSERT INTO items(library_id, type, title, sort_title) VALUES (?, 'artist', 'Dads Band', 'Dads Band')`, dad.Id)
	if err != nil {
		t.Fatal(err)
	}
	dadsBand, _ := r.LastInsertId()
	if code := h.do("PATCH", fmt.Sprintf("/users/%d", kid.Id), map[string]any{"restrictions": map[string]any{"libraryIds": []int64{lib.Id}}}, nil); code != 200 {
		t.Fatalf("restrict: %d", code)
	}
	var shows api.ItemShows
	if code := h.do("GET", fmt.Sprintf("/items/%d/shows", dadsBand), nil, &shows); code != 200 || !shows.Enabled || len(shows.Shows) != 0 {
		t.Errorf("an admin's look: %d %+v", code, shows)
	}
	h.token = kiddo
	if code := h.do("GET", fmt.Sprintf("/items/%d/shows", dadsBand), nil, nil); code != 404 {
		t.Errorf("a hidden artist's shows: %d", code)
	}

	// Removing a location, and turning it off.
	if code := h.do("DELETE", "/me/location", nil, nil); code != 204 {
		t.Errorf("remove: %d", code)
	}
	h.token = admin
	if code := h.do("PATCH", "/settings", map[string]any{"inTown": map[string]any{"enabled": false, "ticketmasterKey": ""}}, &saved); code != 200 || *saved.InTown.TicketmasterKeySet {
		t.Errorf("off: %d %+v", code, saved.InTown)
	}
	h.token = kiddo
	if code := h.do("GET", fmt.Sprintf("/items/%d/shows", dadsBand), nil, nil); code != 404 {
		t.Errorf("still hidden when off: %d", code)
	}
	if h.do("GET", "/music/in-town", nil, &page); page.Enabled {
		t.Error("the page says on")
	}
}

// Kids don't get In Town, enforced here and not just hidden; a managed profile that isn't a kid
// does, and only an admin says which is which.
func TestInTownNotForKids(t *testing.T) {
	h := newHarness(t)
	calls := 0
	tm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		fmt.Fprint(w, `{"_embedded":{"events":[{"id":"E1","name":"Night Out","url":"https://www.ticketmaster.com/e/1",
			"dates":{"start":{"localDate":"2099-11-20","localTime":"19:30:00"},"status":{"code":"onsale"}},
			"_embedded":{"venues":[{"name":"O2","city":{"name":"London"},"country":{"countryCode":"GB"},"location":{"latitude":"51.5","longitude":"0.0"}}],
			"attractions":[{"id":"K1","name":"Test Band"}]}}]},"page":{"totalPages":1}}`)
	}))
	t.Cleanup(tm.Close)
	h.handlers.InTown = &intown.Service{Path: filepath.Join(t.TempDir(), "cache", "in-town.db"), Items: h.handlers.Items, Base: tm.URL,
		Settings: func() settings.InTown { return h.store.Get().InTown }}
	var res api.AuthResult
	h.do("POST", "/setup", map[string]any{"serverName": "T", "username": "admin", "password": "correct horse", "device": device}, &res)
	admin := res.Token
	me := res.User
	h.token = admin
	h.do("PATCH", "/settings", map[string]any{"inTown": map[string]any{"enabled": true, "ticketmasterKey": inTownTestKey}}, nil)
	var lib api.Library
	h.do("POST", "/libraries", map[string]any{"name": "Music", "type": "music", "paths": []string{filepath.Join(h.media, "music")}}, &lib)
	r, err := h.db.Exec(`INSERT INTO items(library_id, type, title, sort_title) VALUES (?, 'artist', 'Test Band', 'Test Band')`, lib.Id)
	if err != nil {
		t.Fatal(err)
	}
	artist, _ := r.LastInsertId()

	profile := func(name string, restrictions map[string]any) (api.User, string) {
		t.Helper()
		var u api.User
		h.token = admin
		if code := h.do("POST", "/users", map[string]any{"username": name, "isManaged": true, "restrictions": restrictions}, &u); code != 201 {
			t.Fatalf("create %s: %d", name, code)
		}
		var sw api.AuthResult
		if code := h.do("POST", fmt.Sprintf("/profiles/%d/switch", u.Id), map[string]any{"device": device}, &sw); code != 200 || sw.Token == "" {
			t.Fatalf("switch to %s: %d", name, code)
		}
		// Switching ends the session it was asked from, so sign in again.
		var back api.AuthResult
		h.token = ""
		h.do("POST", "/auth/login", map[string]any{"username": "admin", "password": "correct horse", "device": device}, &back)
		admin = back.Token
		return u, sw.Token
	}
	_, adult := profile("grandad", map[string]any{})
	child, kid := profile("kiddo", map[string]any{"isKid": true})

	// A managed profile that isn't a kid has it all.
	h.token = adult
	if code := h.do("PUT", "/me/location", map[string]any{"city": "London", "countryCode": "GB"}, nil); code != 200 {
		t.Fatalf("a managed adult's location: %d", code)
	}
	var page api.InTownPage
	if h.do("GET", "/music/in-town", nil, &page); !page.Enabled || page.NeedsLocation || len(page.Near) != 1 {
		t.Errorf("a managed adult's page: %+v", page)
	}
	var shows api.ItemShows
	if h.do("GET", fmt.Sprintf("/items/%d/shows", artist), nil, &shows); !shows.Enabled {
		t.Errorf("a managed adult's shows: %+v", shows)
	}

	// A kid has none of it, with those shows there for others.
	check := func(who, token string) {
		t.Helper()
		h.token = token
		var page api.InTownPage
		if code := h.do("GET", "/music/in-town", nil, &page); code != 200 || page.Enabled || page.NeedsLocation || page.Location != nil || len(page.Near) != 0 || len(page.Artists) != 0 {
			t.Errorf("%s's page: %d %+v", who, code, page)
		}
		var shows api.ItemShows
		if code := h.do("GET", fmt.Sprintf("/items/%d/shows", artist), nil, &shows); code != 200 || shows.Enabled || len(shows.Shows) != 0 || shows.NearCount != 0 {
			t.Errorf("%s's shows: %d %+v", who, code, shows)
		}
		if code := h.do("GET", "/me/location", nil, nil); code != 403 {
			t.Errorf("%s reading a location: %d", who, code)
		}
		if code := h.do("PUT", "/me/location", map[string]any{"city": "Paris", "countryCode": "FR"}, nil); code != 403 {
			t.Errorf("%s setting a location: %d", who, code)
		}
		if code := h.do("DELETE", "/me/location", nil, nil); code != 403 {
			t.Errorf("%s removing a location: %d", who, code)
		}
	}
	check("a kid", kid)

	// Someone made a kid later: a location set before isn't used or sent, and comes to nothing
	// even for an account that isn't managed.
	h.token = admin
	var friend api.User
	if code := h.do("POST", "/users", map[string]any{"username": "teen", "password": "correct horse"}, &friend); code != 201 {
		t.Fatalf("create: %d", code)
	}
	h.token = ""
	var login api.AuthResult
	h.do("POST", "/auth/login", map[string]any{"username": "teen", "password": "correct horse", "device": device}, &login)
	h.token = login.Token
	if code := h.do("PUT", "/me/location", map[string]any{"city": "Berlin", "countryCode": "DE"}, nil); code != 200 {
		t.Fatalf("location before: %d", code)
	}
	h.token = admin
	if code := h.do("PATCH", fmt.Sprintf("/users/%d", friend.Id), map[string]any{"restrictions": map[string]any{"isKid": true}}, nil); code != 200 {
		t.Fatalf("make a kid: %d", code)
	}
	// Whatever the background lookup of the earlier location was doing is over or doesn't
	// count: only what follows matters.
	time.Sleep(1500 * time.Millisecond)
	before := calls
	check("someone made a kid", login.Token)
	if calls != before {
		t.Errorf("%d calls for a kid", calls-before)
	}

	// Saving another restriction doesn't lose it, for an app that doesn't know about it.
	h.token = admin
	var got api.User
	h.do("PATCH", fmt.Sprintf("/users/%d", child.Id), map[string]any{"restrictions": map[string]any{"allowRemote": false}}, &got)
	if got.Restrictions.IsKid == nil || !*got.Restrictions.IsKid {
		t.Errorf("isKid was dropped: %+v", got.Restrictions)
	}
	// A kid can't undo it themselves.
	h.token = kid
	if code := h.do("PATCH", fmt.Sprintf("/users/%d", child.Id), map[string]any{"restrictions": map[string]any{"isKid": false}}, nil); code != 403 {
		t.Errorf("a kid changing their restrictions: %d", code)
	}
	h.do("PATCH", "/me", map[string]any{"restrictions": map[string]any{"isKid": false}}, nil)
	var self api.User
	if h.do("GET", "/me", nil, &self); self.Restrictions.IsKid == nil || !*self.Restrictions.IsKid {
		t.Errorf("/me: %+v", self.Restrictions)
	}

	// An administrator can't be one, however it's asked for.
	h.token = admin
	if code := h.do("POST", "/users", map[string]any{"username": "boss", "password": "correct horse", "isAdmin": true, "restrictions": map[string]any{"isKid": true}}, nil); code != 400 {
		t.Errorf("a new admin kid: %d", code)
	}
	if code := h.do("PATCH", fmt.Sprintf("/users/%d", me.Id), map[string]any{"restrictions": map[string]any{"isKid": true}}, nil); code != 400 {
		t.Errorf("an admin made a kid: %d", code)
	}
	if code := h.do("PATCH", fmt.Sprintf("/users/%d", friend.Id), map[string]any{"isAdmin": true}, nil); code != 400 {
		t.Errorf("a kid made an admin: %d", code)
	}
	// Existing users aren't kids until an admin says so.
	h.token = adult
	if h.do("GET", "/me", nil, &self); self.Restrictions.IsKid == nil || *self.Restrictions.IsKid {
		t.Errorf("a managed adult: %+v", self.Restrictions)
	}
}
