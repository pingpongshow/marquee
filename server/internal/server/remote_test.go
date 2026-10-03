package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"testing"
	"time"

	"marquee/internal/api"
)

// call is h.do with an explicit token and context, for concurrent long polls.
func (h *harness) call(ctx context.Context, token, method, path string, body, out any) int {
	h.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req, _ := http.NewRequestWithContext(ctx, method, h.srv.URL+"/api/v1"+path, &buf)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0 // cancelled
	}
	defer resp.Body.Close()
	if out != nil {
		json.NewDecoder(resp.Body).Decode(out)
	}
	return resp.StatusCode
}

func TestRemoteControl(t *testing.T) {
	h := newHarness(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // ends the long polls still waiting, so the server can close

	var res api.AuthResult
	h.do("POST", "/setup", map[string]any{"serverName": "T", "username": "admin", "password": "correct horse", "device": device}, &res)
	h.token = res.Token
	var lib api.Library
	h.do("POST", "/libraries", map[string]any{"name": "Movies", "type": "movies", "paths": []string{filepath.Join(h.media, "video/Movies")}}, &lib)
	r, _ := h.db.Exec(`INSERT INTO items(library_id, type, title, sort_title) VALUES (?, 'movie', 'Alien', 'Alien')`, lib.Id)
	alien, _ := r.LastInsertId()

	var bob, fr api.User
	h.do("POST", "/users", map[string]any{"username": "bob", "password": "bob-password", "restrictions": map[string]any{"libraryIds": []int64{}}}, &bob)
	h.do("POST", "/users", map[string]any{"username": "fred", "password": "fred-password", "restrictions": map[string]any{"friend": true}}, &fr)
	login := func(user, pass, client, name string) string {
		var res api.AuthResult
		if code := h.do("POST", "/auth/login", map[string]any{"username": user, "password": pass,
			"device": map[string]any{"clientId": client, "name": name, "platform": "tvos"}}, &res); code != 200 {
			t.Fatalf("login %s: %d", user, code)
		}
		return res.Token
	}
	adminPhone := h.token
	adminTV := login("admin", "correct horse", "admin-tv", "Den TV")
	bobPhone := login("bob", "bob-password", "bob-phone", "Bob's phone")
	bobTV := login("bob", "bob-password", "bob-tv", "Bob's TV")
	fredPhone := login("fred", "fred-password", "fred-phone", "Fred's phone")

	type inboxResult struct {
		code  int
		inbox api.RemoteInbox
	}
	poll := func(token string, cursor int64, state *api.RemotePlayerState) chan inboxResult {
		ch := make(chan inboxResult, 1)
		go func() {
			var r inboxResult
			r.code = h.call(ctx, token, "POST", "/remote/inbox", api.RemoteInboxRequest{Cursor: &cursor,
				Capabilities: []api.RemoteCapability{"video", "music"}, State: state}, &r.inbox)
			ch <- r
		}()
		return ch
	}
	players := func(token string) []api.RemotePlayer {
		var out []api.RemotePlayer
		if code := h.call(ctx, token, "GET", "/remote/players", nil, &out); code != 200 {
			t.Fatalf("players: %d", code)
		}
		return out
	}
	waitFor := func(token string, n int) []api.RemotePlayer {
		t.Helper()
		for i := 0; i < 100; i++ {
			if p := players(token); len(p) >= n {
				return p
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Fatalf("expected %d players", n)
		return nil
	}

	// Bob's TV and the admin's TV become players by polling.
	bobInbox := poll(bobTV, 0, &api.RemotePlayerState{State: "idle"})
	poll(adminTV, 0, nil)

	// Bob sees his TV only (not his phone, not the admin's TV).
	list := waitFor(bobPhone, 1)
	if len(list) != 1 || list[0].Name != "Bob's TV" || list[0].UserId != bob.Id || list[0].State == nil || list[0].State.State != "idle" ||
		len(list[0].Capabilities) != 2 {
		t.Fatalf("bob's players: %+v", list)
	}
	tv := list[0].DeviceId
	// Bob's TV doesn't list itself.
	if p := players(bobTV); len(p) != 0 {
		t.Errorf("own device listed: %+v", p)
	}
	// The admin sees everyone's; a friend sees nobody else's.
	if p := waitFor(adminPhone, 2); len(p) != 2 {
		t.Errorf("admin's players: %+v", p)
	}
	if p := players(fredPhone); len(p) != 0 {
		t.Errorf("friend sees: %+v", p)
	}
	if code := h.call(ctx, fredPhone, "GET", fmt.Sprintf("/remote/players/%d", tv), nil, nil); code != 404 {
		t.Errorf("friend got bob's player: %d", code)
	}
	if code := h.call(ctx, fredPhone, "POST", fmt.Sprintf("/remote/players/%d/commands", tv), map[string]any{"type": "pause"}, nil); code != 404 {
		t.Errorf("friend commanded bob's player: %d", code)
	}
	if code := h.call(ctx, bobPhone, "GET", "/remote/players/99999", nil, nil); code != 404 {
		t.Errorf("unknown player: %d", code)
	}

	// Validation.
	cmdPath := fmt.Sprintf("/remote/players/%d/commands", tv)
	for _, bad := range []map[string]any{
		{"type": "seek"},
		{"type": "play"},
		{"type": "play", "itemIds": []int64{alien}, "index": 3},
		{"type": "setVolume", "volume": 2},
		{"type": "setSubtitle"},
		{"type": "dance"},
	} {
		if code := h.call(ctx, bobPhone, "POST", cmdPath, bad, nil); code != 400 {
			t.Errorf("%v: %d", bad, code)
		}
	}
	// Play checks what the TV's user may see: the admin can't send Bob a film from a
	// library Bob has no access to.
	if code := h.call(ctx, adminPhone, "POST", cmdPath, map[string]any{"type": "play", "itemIds": []int64{alien}}, nil); code != 400 {
		t.Errorf("play of a hidden item: %d", code)
	}

	// A command wakes the TV's poll at once.
	start := time.Now()
	if code := h.call(ctx, bobPhone, "POST", cmdPath, map[string]any{"type": "seek", "positionMs": 90_000}, nil); code != 204 {
		t.Fatalf("seek: %d", code)
	}
	var got inboxResult
	select {
	case got = <-bobInbox:
	case <-time.After(5 * time.Second):
		t.Fatal("inbox not woken")
	}
	if got.code != 200 || len(got.inbox.Commands) != 1 || time.Since(start) > 3*time.Second {
		t.Fatalf("inbox: %+v", got)
	}
	c := got.inbox.Commands[0]
	if c.Type != "seek" || *c.PositionMs != 90_000 || c.From == nil || *c.From != "Bob's phone" || got.inbox.Cursor != 1 {
		t.Errorf("command: %+v cursor %d", c, got.inbox.Cursor)
	}

	// The admin may play something on its own TV.
	var adminTVID int64
	for _, p := range players(adminPhone) {
		if p.Name == "Den TV" {
			adminTVID = p.DeviceId
		}
	}
	if code := h.call(ctx, adminPhone, "POST", fmt.Sprintf("/remote/players/%d/commands", adminTVID),
		map[string]any{"type": "play", "itemIds": []int64{alien, alien}, "index": 1, "startMs": 0}, nil); code != 204 {
		t.Errorf("admin play: %d", code)
	}

	// The next poll acknowledges with the cursor and waits again; state reports wake
	// controllers waiting with ?since=.
	bobInbox = poll(bobTV, got.inbox.Cursor, nil)
	var p api.RemotePlayer
	h.call(ctx, bobPhone, "GET", fmt.Sprintf("/remote/players/%d", tv), nil, &p)
	waiter := make(chan api.RemotePlayer, 1)
	go func() {
		var w api.RemotePlayer
		h.call(ctx, bobPhone, "GET", fmt.Sprintf("/remote/players/%d?since=%d", tv, p.Version), nil, &w)
		waiter <- w
	}()
	time.Sleep(100 * time.Millisecond)
	title := "Alien"
	if code := h.call(ctx, bobTV, "PUT", "/remote/state", api.RemotePlayerState{State: "playing", PositionMs: 91_000, ItemId: &alien, Title: &title}, nil); code != 204 {
		t.Fatalf("state: %d", code)
	}
	select {
	case w := <-waiter:
		if w.Version <= p.Version || w.State == nil || w.State.State != "playing" || *w.State.Title != "Alien" {
			t.Errorf("state wait: %+v", w)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("state waiter not woken")
	}
	select {
	case r := <-bobInbox:
		t.Errorf("acknowledged command redelivered: %+v", r)
	default:
	}
}

func TestSubtitleStylePreferences(t *testing.T) {
	h := newHarness(t)
	var res api.AuthResult
	h.do("POST", "/setup", map[string]any{"serverName": "T", "username": "admin", "password": "correct horse", "device": device}, &res)
	h.token = res.Token

	var u api.User
	if code := h.do("PATCH", "/me", map[string]any{"preferences": map[string]any{"audioLanguage": "jpn",
		"subtitleStyle": map[string]any{"size": "large", "color": "#ffcc00", "background": "translucent", "position": "raised"}}}, &u); code != 200 {
		t.Fatalf("patch: %d", code)
	}
	st := u.Preferences.SubtitleStyle
	if st == nil || *st.Size != "large" || *st.Color != "#FFCC00" || *st.Background != "translucent" || *st.Position != "raised" ||
		*u.Preferences.AudioLanguage != "jpn" {
		t.Fatalf("saved: %+v %+v", u.Preferences, st)
	}
	h.do("GET", "/me", nil, &u)
	if st = u.Preferences.SubtitleStyle; st == nil || *st.Size != "large" {
		t.Fatalf("read back: %+v", st)
	}
	// Partial styles keep only what's set; the rest are defaults on the apps.
	u = api.User{}
	h.do("PATCH", "/me", map[string]any{"preferences": map[string]any{"subtitleStyle": map[string]any{"color": "#00FFFF"}}}, &u)
	if st = u.Preferences.SubtitleStyle; st == nil || st.Size != nil || *st.Color != "#00FFFF" {
		t.Errorf("partial: %+v", st)
	}
	// Invalid values are refused and change nothing.
	for _, bad := range []map[string]any{
		{"color": "yellow"}, {"color": "#FFF"}, {"color": "#GG0000"}, {"size": "tiny"}, {"background": "glow"}, {"position": "top"},
	} {
		if code := h.do("PATCH", "/me", map[string]any{"preferences": map[string]any{"subtitleStyle": bad}}, nil); code != 400 {
			t.Errorf("%v: %d", bad, code)
		}
	}
	u = api.User{}
	h.do("GET", "/me", nil, &u)
	if st = u.Preferences.SubtitleStyle; st == nil || *st.Color != "#00FFFF" || st.Size != nil {
		t.Errorf("after refused updates: %+v", st)
	}
	// Sending preferences without a style clears it, as the whole object is replaced.
	u = api.User{}
	h.do("PATCH", "/me", map[string]any{"preferences": map[string]any{"audioLanguage": "eng"}}, &u)
	if u.Preferences.SubtitleStyle != nil {
		t.Errorf("not cleared: %+v", u.Preferences.SubtitleStyle)
	}
}
