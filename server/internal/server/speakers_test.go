package server

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"marquee/internal/api"
	"marquee/internal/netclass"
	"marquee/internal/speakers"
	"marquee/internal/speakers/speakerstest"
)

const speakerRoom = "RINCON_ROOM01400"

// speakerHarness is a signed-in admin, an album of three songs and a room on a fake player,
// behind the whole HTTP stack, the speakers' own routes included.
func speakerHarness(t *testing.T) (h *harness, p *speakerstest.Player, album int64, songs []int64) {
	t.Helper()
	h = newHarness(t)
	var res api.AuthResult
	h.do("POST", "/setup", map[string]any{"serverName": "T", "username": "admin", "password": "correct horse", "device": device}, &res)
	h.token = res.Token
	var lib api.Library
	h.do("POST", "/libraries", map[string]any{"name": "Music", "type": "music", "paths": []string{filepath.Join(h.media, "music")}}, &lib)
	exec := func(q string, args ...any) int64 {
		t.Helper()
		r, err := h.db.Exec(q, args...)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := r.LastInsertId()
		return id
	}
	artist := exec(`INSERT INTO items(library_id, type, title, sort_title) VALUES (?, 'artist', 'Artist', 'Artist')`, lib.Id)
	album = exec(`INSERT INTO items(library_id, type, title, sort_title, parent_id) VALUES (?, 'album', 'Album', 'Album', ?)`, lib.Id, artist)
	for i := range 3 {
		title := fmt.Sprintf("Song %d", i+1)
		id := exec(`INSERT INTO items(library_id, type, title, sort_title, parent_id, grandparent_id, idx, duration_ms) VALUES (?, 'track', ?, ?, ?, ?, ?, 60000)`,
			lib.Id, title, title, album, artist, i+1)
		v := exec(`INSERT INTO media_versions(item_id) VALUES (?)`, id)
		path := filepath.Join(h.media, "music", fmt.Sprintf("%d.mp3", i+1))
		if err := os.WriteFile(path, []byte("audio"), 0o644); err != nil {
			t.Fatal(err)
		}
		f := exec(`INSERT INTO media_files(version_id, library_id, path, size, mtime, duration_ms, available) VALUES (?, ?, ?, 5, 1, 60000, 1)`, v, lib.Id, path)
		exec(`INSERT INTO streams(file_id, stream_index, kind, codec, sample_rate, channels) VALUES (?, 0, 'audio', 'mp3', 44100, 2)`, f)
		songs = append(songs, id)
	}
	p = speakerstest.New(t, speakerRoom)
	p.Set(func(p *speakerstest.Player) { p.Topology = p.RoomTopology("Den") })
	grace := time.Duration(0)
	svc := &speakers.Service{DB: h.db, Dir: t.TempDir(), Port: 32500, Hosts: []string{p.Addr}, PollEvery: time.Hour, Grace: &grace}
	h.handlers.Speakers = svc
	srv := httptest.NewServer(New(Deps{Handlers: h.handlers, Speakers: svc.Handler(), Auth: h.handlers.Auth,
		Classifier: netclass.New([]string{"127.0.0.0/8"}, "")}))
	t.Cleanup(srv.Close)
	h.srv = srv
	return h, p, album, songs
}

func TestSpeakersAreForSignedInUsersAtHome(t *testing.T) {
	h, p, _, songs := speakerHarness(t)
	request := func(method, path, token, body string, remote bool) int {
		req, _ := http.NewRequest(method, h.srv.URL+"/api/v1"+path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if remote {
			req.Header.Set("X-Forwarded-For", "203.0.113.9") // from the loopback proxy: away from home
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}
	play := fmt.Sprintf(`{"itemIds":[%d]}`, songs[0])
	for _, c := range []struct{ method, path, body string }{
		{"GET", "/speakers", ""},
		{"GET", "/speakers/" + speakerRoom, ""},
		{"POST", "/speakers/" + speakerRoom + "/play", play},
		{"POST", "/speakers/" + speakerRoom + "/queue", play},
		{"POST", "/speakers/" + speakerRoom + "/control", `{"action":"pause"}`},
	} {
		if code := request(c.method, c.path, "", c.body, false); code != 401 {
			t.Errorf("%s %s signed out: %d", c.method, c.path, code)
		}
		if code := request(c.method, c.path, h.token, c.body, true); code != 403 {
			t.Errorf("%s %s away from home: %d", c.method, c.path, code)
		}
	}
	if calls := p.Actions(); len(calls) != 0 {
		t.Fatalf("the speaker heard %v", calls)
	}
	if code := request("GET", "/speakers", h.token, "", false); code != 200 {
		t.Fatalf("at home: %d", code)
	}
}

func TestPlayingOnASpeaker(t *testing.T) {
	h, p, album, songs := speakerHarness(t)

	var rooms []api.Speaker
	if code := h.do("GET", "/speakers", nil, &rooms); code != 200 || len(rooms) != 1 || rooms[0].Id != speakerRoom || rooms[0].Name != "Den" || rooms[0].Marquee {
		t.Fatalf("%d %+v", code, rooms)
	}
	var none api.Error
	if code := h.do("GET", "/speakers/RINCON_NOBODY", nil, &none); code != 404 {
		t.Fatalf("no such room: %d", code)
	}
	if code := h.do("POST", "/speakers/RINCON_NOBODY/control", map[string]any{"action": "pause"}, nil); code != 404 {
		t.Fatalf("no such room: %d", code)
	}
	path := "/speakers/" + speakerRoom

	// Bad requests never reach the speaker.
	for _, body := range []map[string]any{{"itemIds": []int64{}}, {"itemIds": []int64{1}, "index": 5}, {"itemIds": []int64{1}, "startMs": -1}} {
		if code := h.do("POST", path+"/play", body, nil); code != 400 {
			t.Errorf("%v: %d", body, code)
		}
	}
	for _, body := range []map[string]any{{"action": "seek"}, {"action": "setVolume"}, {"action": "setVolume", "volume": 101}, {"action": "dance"}} {
		if code := h.do("POST", path+"/control", body, nil); code != 400 {
			t.Errorf("%v: %d", body, code)
		}
	}
	if code := h.do("POST", path+"/control", map[string]any{"action": "next"}, nil); code != 409 {
		t.Fatalf("next with nothing of ours: %d", code)
	}
	if code := h.do("POST", path+"/queue", map[string]any{"itemIds": songs[:1]}, nil); code != 409 {
		t.Fatalf("queue with nothing of ours: %d", code)
	}
	if code := h.do("POST", path+"/play", map[string]any{"itemIds": []int64{album + 999}}, nil); code != 404 {
		t.Fatalf("nothing playable: %d", code)
	}

	// The thumbs-down song stays out of an album...
	if _, err := h.db.Exec(`INSERT INTO user_item_state(user_id, item_id, disliked_at) VALUES (1, ?, '2026-01-01T00:00:00Z')`, songs[1]); err != nil {
		t.Fatal(err)
	}
	var st api.Speaker
	if code := h.do("POST", path+"/play", map[string]any{"itemIds": []int64{album}, "context": "Album:1"}, &st); code != 200 {
		t.Fatalf("play: %d", code)
	}
	if !st.Marquee || st.QueueLength == nil || *st.QueueLength != 2 || *st.QueueIndex != 0 || st.Current == nil || st.Current.Id != songs[0] {
		t.Fatalf("%+v", st)
	}
	uri := p.Get().URI
	if strings.Contains(uri, h.token) || strings.Contains(p.Get().Meta, h.token) {
		t.Fatal("a sign-in token in what the speaker gets")
	}

	// The speaker fetches with no sign-in, through the whole stack; nothing else opens up.
	u, err := url.Parse(uri)
	if err != nil {
		t.Fatal(err)
	}
	fetch := func(path string, headers ...string) (int, string) {
		req, _ := http.NewRequest("GET", h.srv.URL+path, nil)
		if len(headers) == 2 {
			req.Header.Set(headers[0], headers[1])
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, string(b)
	}
	if code, body := fetch(u.Path); code != 200 || body != "audio" {
		t.Fatalf("stream: %d %q", code, body)
	}
	if code, body := fetch(u.Path, "Range", "bytes=1-2"); code != 206 || body != "ud" {
		t.Fatalf("range: %d %q", code, body)
	}
	token := strings.Split(u.Path, "/")[5]
	for _, other := range []string{
		fmt.Sprintf("/api/v1/speakers/stream/%s/%d.mp3", token, songs[1]), // left out of the queue
		fmt.Sprintf("/api/v1/speakers/stream/%s/%d.mp3", strings.Repeat("a", 32), songs[0]),
		fmt.Sprintf("/api/v1/items/%d", songs[0]),
		"/api/v1/speakers",
		"/api/v1/speakers/" + speakerRoom,
	} {
		if code, _ := fetch(other); code != 404 && code != 401 {
			t.Errorf("%s with no sign-in: %d", other, code)
		}
	}

	// ... but one named directly plays, there and when added.
	if code := h.do("POST", path+"/queue", map[string]any{"itemIds": songs[1:2]}, &st); code != 200 || *st.QueueLength != 3 {
		t.Fatalf("queue: %d %+v", code, st)
	}
	if code := h.do("POST", path+"/queue", map[string]any{"itemIds": []int64{album}}, &st); code != 200 || *st.QueueLength != 5 {
		t.Fatalf("queue an album: %d %+v", code, st)
	}
	if code := h.do("POST", path+"/play", map[string]any{"itemIds": songs, "index": 1}, &st); code != 200 || st.Current.Id != songs[1] || *st.QueueIndex != 1 || *st.QueueLength != 3 {
		t.Fatalf("named song: %d %+v", code, st)
	}
	if code := h.do("POST", path+"/play", map[string]any{"itemIds": songs, "index": 2}, &st); code != 200 || st.Current.Id != songs[2] || *st.QueueIndex != 1 || *st.QueueLength != 2 {
		t.Fatalf("other song named: %d %+v", code, st)
	}

	// Controls answer with the room.
	p.Set(func(p *speakerstest.Player) { p.Rel, p.Duration = "0:00:10", "0:01:00" })
	if code := h.do("POST", path+"/control", map[string]any{"action": "setVolume", "volume": 12}, &st); code != 200 || st.Volume != 12 {
		t.Fatalf("volume: %d %+v", code, st)
	}
	if code := h.do("POST", path+"/control", map[string]any{"action": "seek", "positionMs": 20000}, &st); code != 200 || *st.PositionMs != 20000 {
		t.Fatalf("seek: %d %+v", code, st)
	}
	// Someone who can't see the song doesn't see what's playing.
	var movies api.Library
	h.do("POST", "/libraries", map[string]any{"name": "Movies", "type": "movies", "paths": []string{filepath.Join(h.media, "video/Movies")}}, &movies)
	var kid api.User
	h.do("POST", "/users", map[string]any{"username": "kid", "password": "kid-password", "restrictions": map[string]any{"libraryIds": []int64{movies.Id}}}, &kid)
	var login api.AuthResult
	if code := h.do("POST", "/auth/login", map[string]any{"username": "kid", "password": "kid-password", "device": map[string]any{"clientId": "kid", "name": "K", "platform": "web"}}, &login); code != 200 {
		t.Fatalf("login: %d", code)
	}
	admin := h.token
	h.token = login.Token
	var asKid api.Speaker
	if code := h.do("GET", path, nil, &asKid); code != 200 || !asKid.Marquee || asKid.Current != nil {
		t.Fatalf("as kid: %d %+v", code, asKid)
	}
	if code := h.do("POST", path+"/play", map[string]any{"itemIds": songs}, nil); code != 404 {
		t.Fatalf("kid playing what they can't see: %d", code)
	}
	h.token = admin

	var stopped api.Speaker
	if code := h.do("POST", path+"/control", map[string]any{"action": "stop"}, &stopped); code != 200 || stopped.Marquee || stopped.Current != nil {
		t.Fatalf("stop: %d %+v", code, stopped)
	}
}
