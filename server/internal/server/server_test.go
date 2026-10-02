package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"marquee/internal/api"
	"marquee/internal/auth"
	"marquee/internal/avatars"
	"marquee/internal/db"
	"marquee/internal/items"
	"marquee/internal/library"
	"marquee/internal/netclass"
	"marquee/internal/settings"
)

type harness struct {
	t     *testing.T
	db    *sql.DB
	srv   *httptest.Server
	token string
	media string
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	dir := t.TempDir()
	ctx := context.Background()
	database, err := db.Open(ctx, filepath.Join(dir, "test.db"), filepath.Join(dir, "backups"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	store, err := settings.Open(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	media := filepath.Join(dir, "media")
	for _, d := range []string{"video/Movies", "video/Shows", "music"} {
		os.MkdirAll(filepath.Join(media, d), 0o755)
	}
	if _, err := store.Update(ctx, func(s *settings.Settings) error {
		s.Library.BrowseRoots = []string{media}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	authSvc := auth.NewService(database)
	h := New(Deps{
		Handlers: &api.Handlers{DB: database, Auth: authSvc, Settings: store,
			Libraries: library.NewStore(database), Items: items.NewStore(database), Version: "test",
			Avatars: &avatars.Store{DB: database, Dir: filepath.Join(dir, "avatars")}},
		Auth:       authSvc,
		Classifier: netclass.New([]string{"127.0.0.0/8"}, ""),
	})
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return &harness{t: t, srv: srv, media: media, db: database}
}

func (h *harness) do(method, path string, body any, out any) int {
	h.t.Helper()
	var buf bytes.Buffer
	if body != nil {
		json.NewEncoder(&buf).Encode(body)
	}
	req, _ := http.NewRequest(method, h.srv.URL+"/api/v1"+path, &buf)
	req.Header.Set("Content-Type", "application/json")
	if h.token != "" {
		req.Header.Set("Authorization", "Bearer "+h.token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	if out != nil {
		json.NewDecoder(resp.Body).Decode(out)
	}
	return resp.StatusCode
}

var device = map[string]any{"clientId": "test-client", "name": "Test", "platform": "web"}

func TestSetupLoginSettingsLibraries(t *testing.T) {
	h := newHarness(t)

	var info api.SystemInfo
	if code := h.do("GET", "/system/info", nil, &info); code != 200 || !info.SetupRequired || info.NetworkClass != "local" {
		t.Fatalf("info: %d %+v", code, info)
	}
	if code := h.do("GET", "/settings", nil, nil); code != 401 {
		t.Fatalf("settings unauthenticated: got %d", code)
	}
	if code := h.do("POST", "/setup", map[string]any{"serverName": "Tower", "username": "admin", "password": "short", "device": device}, nil); code != 400 {
		t.Fatalf("weak password accepted: %d", code)
	}

	var res api.AuthResult
	if code := h.do("POST", "/setup", map[string]any{"serverName": "Tower", "username": "admin", "password": "correct horse", "device": device}, &res); code != 200 || !res.User.IsAdmin {
		t.Fatalf("setup: %d %+v", code, res)
	}
	if code := h.do("POST", "/setup", map[string]any{"serverName": "x", "username": "evil", "password": "correct horse", "device": device}, nil); code != 409 {
		t.Fatalf("second setup: got %d", code)
	}
	if code := h.do("POST", "/auth/login", map[string]any{"username": "admin", "password": "wrong password", "device": device}, nil); code != 401 {
		t.Fatalf("bad login: got %d", code)
	}
	if code := h.do("POST", "/auth/login", map[string]any{"username": "ADMIN", "password": "correct horse", "device": device}, &res); code != 200 {
		t.Fatalf("login: got %d", code)
	}
	h.token = res.Token

	var s api.ServerSettings
	if code := h.do("GET", "/settings", nil, &s); code != 200 || *s.General.ServerName != "Tower" {
		t.Fatalf("settings: %d %+v", code, s.General)
	}
	if code := h.do("PATCH", "/settings", map[string]any{"network": map[string]any{"lanSubnets": []string{"not-a-cidr"}}}, nil); code != 400 {
		t.Fatalf("invalid subnet accepted: %d", code)
	}
	if code := h.do("PATCH", "/settings", map[string]any{
		"remoteAccess": map[string]any{"uploadSpeedKbps": 40000, "remoteUrl": "https://tower.tail1234.ts.net"},
		"metadata":     map[string]any{"tmdbApiKey": "secret"},
	}, &s); code != 200 || *s.RemoteAccess.UploadSpeedKbps != 40000 || !*s.Metadata.TmdbApiKeySet {
		t.Fatalf("patch settings: %d %+v", code, s.RemoteAccess)
	}
	if *s.Transcoder.MaxConcurrentTranscodes != 12 {
		t.Fatalf("untouched section changed: %+v", s.Transcoder)
	}

	var listing api.DirectoryListing
	if code := h.do("GET", "/filesystem/browse", nil, &listing); code != 200 || len(listing.Entries) != 1 {
		t.Fatalf("browse roots: %d %+v", code, listing)
	}
	if code := h.do("GET", "/filesystem/browse?path="+filepath.Join(h.media, "video"), nil, &listing); code != 200 || len(listing.Entries) != 2 {
		t.Fatalf("browse video: %d %+v", code, listing)
	}
	if code := h.do("GET", "/filesystem/browse?path=/etc", nil, nil); code != 403 {
		t.Fatalf("browse outside roots: got %d", code)
	}

	movies := filepath.Join(h.media, "video/Movies")
	var lib api.Library
	if code := h.do("POST", "/libraries", map[string]any{"name": "Movies", "type": "movies", "paths": []string{movies}}, &lib); code != 201 {
		t.Fatalf("create library: got %d", code)
	}
	if lib.Options.IncludeInHome == nil || !*lib.Options.IncludeInHome || lib.Options.EpisodeOrdering != nil {
		t.Fatalf("library defaults wrong: %+v", lib.Options)
	}
	if code := h.do("POST", "/libraries", map[string]any{"name": "Bad", "type": "movies", "paths": []string{"/does/not/exist"}}, nil); code != 400 {
		t.Fatalf("missing folder accepted: %d", code)
	}
	if code := h.do("POST", "/libraries", map[string]any{"name": "Bad", "type": "movies", "paths": []string{movies},
		"options": map[string]any{"episodeOrdering": "absolute"}}, nil); code != 400 {
		t.Fatalf("episode ordering on movies accepted: %d", code)
	}
	if code := h.do("PATCH", "/libraries/"+itoa(lib.Id), map[string]any{"name": "Films", "options": map[string]any{"includeInHome": false}}, &lib); code != 200 || lib.Name != "Films" || *lib.Options.IncludeInHome {
		t.Fatalf("update library: %d %+v", code, lib)
	}
	var libs []api.Library
	if code := h.do("GET", "/libraries", nil, &libs); code != 200 || len(libs) != 1 {
		t.Fatalf("list: %d %d", code, len(libs))
	}
	if code := h.do("DELETE", "/libraries/"+itoa(lib.Id), nil, nil); code != 204 {
		t.Fatalf("delete: got %d", code)
	}
	if code := h.do("GET", "/libraries/"+itoa(lib.Id), nil, nil); code != 404 {
		t.Fatalf("deleted library still found: %d", code)
	}

	if code := h.do("POST", "/auth/logout", nil, nil); code != 204 {
		t.Fatalf("logout: got %d", code)
	}
	if code := h.do("GET", "/me", nil, nil); code != 401 {
		t.Fatalf("revoked token still works: %d", code)
	}
}

func itoa(i int64) string { return strconv.FormatInt(i, 10) }

func TestUsersProfilesAccessSearch(t *testing.T) {
	h := newHarness(t)
	var res api.AuthResult
	h.do("POST", "/setup", map[string]any{"serverName": "T", "username": "admin", "password": "correct horse", "device": device}, &res)
	h.token = res.Token
	adminToken := res.Token

	// Two libraries with a few items.
	movies := filepath.Join(h.media, "video/Movies")
	music := filepath.Join(h.media, "music")
	var mlib, alib api.Library
	h.do("POST", "/libraries", map[string]any{"name": "Movies", "type": "movies", "paths": []string{movies}}, &mlib)
	h.do("POST", "/libraries", map[string]any{"name": "Music", "type": "music", "paths": []string{music}}, &alib)
	h.db.Exec(`INSERT INTO items(library_id, type, title, sort_title, content_rating) VALUES (?, 'movie', 'The Matrix', 'Matrix', 'R'), (?, 'movie', 'Toy Story', 'Toy Story', 'G'), (?, 'movie', 'Unrated Thing', 'Unrated Thing', NULL)`, mlib.Id, mlib.Id, mlib.Id)
	h.db.Exec(`INSERT INTO items(library_id, type, title, sort_title) VALUES (?, 'artist', 'Matrix Band', 'Matrix Band')`, alib.Id)

	// Managed kid profile: PIN only, PG max, movies library only.
	var kid api.User
	if code := h.do("POST", "/users", map[string]any{"username": "kid", "displayName": "Kid", "isManaged": true, "pin": "1234",
		"restrictions": map[string]any{"maxContentRating": "PG", "libraryIds": []int64{mlib.Id}}}, &kid); code != 201 {
		t.Fatalf("create kid: %d", code)
	}
	if code := h.do("POST", "/users", map[string]any{"username": "nopw"}, nil); code != 201 {
		t.Fatalf("non-admin user without password should be allowed: %d", code)
	}
	if code := h.do("POST", "/users", map[string]any{"username": "adminnopw", "isAdmin": true}, nil); code != 400 {
		t.Fatalf("admin without password accepted: %d", code)
	}
	if code := h.do("POST", "/users", map[string]any{"username": "KID", "isManaged": true}, nil); code != 409 {
		t.Fatalf("duplicate username: %d", code)
	}
	var friend api.User
	h.do("POST", "/users", map[string]any{"username": "friend", "password": "friend-password"}, &friend)

	// Search as admin sees everything, grouped.
	var sr api.SearchResults
	h.do("GET", "/search?q=matr", nil, &sr)
	if len(sr.Groups) != 2 {
		t.Fatalf("admin search groups: %+v", sr.Groups)
	}

	// Kid can't log in with a password; switching needs the PIN.
	var profiles []api.Profile
	h.do("GET", "/profiles", nil, &profiles)
	if len(profiles) != 4 {
		t.Fatalf("profiles: %d", len(profiles))
	}
	h.token = "" // switching from the friend's session, not admin
	var fr api.AuthResult
	h.do("POST", "/auth/login", map[string]any{"username": "friend", "password": "friend-password", "device": device}, &fr)
	h.token = fr.Token
	if code := h.do("POST", fmt.Sprintf("/profiles/%d/switch", kid.Id), map[string]any{"pin": "0000", "device": device}, nil); code != 401 {
		t.Fatalf("wrong PIN accepted: %d", code)
	}
	var kr api.AuthResult
	if code := h.do("POST", fmt.Sprintf("/profiles/%d/switch", kid.Id), map[string]any{"pin": "1234", "device": device}, &kr); code != 200 || kr.User.Id != kid.Id {
		t.Fatalf("switch: %d", code)
	}
	if code := h.do("GET", "/me", nil, nil); code != 401 {
		t.Fatalf("old token should be revoked after switching: %d", code)
	}
	h.token = kr.Token

	// Restrictions: only the movies library, only G/PG titles, unrated hidden.
	var libs []api.Library
	h.do("GET", "/libraries", nil, &libs)
	if len(libs) != 1 || libs[0].Id != mlib.Id {
		t.Fatalf("kid libraries: %+v", libs)
	}
	var page api.ItemPage
	h.do("GET", fmt.Sprintf("/libraries/%d/items", mlib.Id), nil, &page)
	if page.Total != 1 || page.Items[0].Title != "Toy Story" {
		t.Fatalf("kid items: %+v", page)
	}
	if code := h.do("GET", fmt.Sprintf("/libraries/%d/items", alib.Id), nil, nil); code != 404 {
		t.Fatalf("hidden library visible: %d", code)
	}
	h.do("GET", "/search?q=matr", nil, &sr)
	if len(sr.Groups) != 0 {
		t.Fatalf("kid search should find nothing: %+v", sr.Groups)
	}
	if code := h.do("GET", "/users", nil, nil); code != 403 {
		t.Fatalf("kid listing users: %d", code)
	}
	if code := h.do("PATCH", "/me", map[string]any{"pin": ""}, nil); code != 403 {
		t.Fatalf("kid removed own PIN: %d", code)
	}

	// Admin can't remove the last admin.
	h.token = adminToken
	var me api.User
	h.do("GET", "/me", nil, &me)
	if code := h.do("PATCH", fmt.Sprintf("/users/%d", me.Id), map[string]any{"isAdmin": false}, nil); code != 409 {
		t.Fatalf("demoted last admin: %d", code)
	}
	if code := h.do("DELETE", fmt.Sprintf("/users/%d", me.Id), nil, nil); code != 409 {
		t.Fatalf("deleted last admin: %d", code)
	}
	var devices []api.Device
	h.do("GET", "/devices", nil, &devices)
	if len(devices) < 2 {
		t.Fatalf("devices: %d", len(devices))
	}
	if code := h.do("DELETE", fmt.Sprintf("/users/%d", kid.Id), nil, nil); code != 204 {
		t.Fatalf("delete kid: %d", code)
	}
}

func TestPinSignIn(t *testing.T) {
	h := newHarness(t)
	var res api.AuthResult
	h.do("POST", "/setup", map[string]any{"serverName": "T", "username": "admin", "password": "correct horse", "device": device}, &res)
	h.token = res.Token
	var kid, tot, adult, guest api.User
	h.do("POST", "/users", map[string]any{"username": "kid", "isManaged": true, "pin": "4321"}, &kid)
	h.do("POST", "/users", map[string]any{"username": "toddler", "isManaged": true}, &tot)
	h.do("POST", "/users", map[string]any{"username": "adult", "password": "adult-password"}, &adult)
	h.do("POST", "/users", map[string]any{"username": "guest"}, &guest)
	h.token = ""

	var info api.SystemInfo
	h.do("GET", "/system/info", nil, &info)
	if info.PinSignIn == nil || !*info.PinSignIn {
		t.Fatal("PIN sign-in should be offered on the local network by default")
	}
	var profiles []api.Profile
	h.do("GET", "/auth/profiles", nil, &profiles)
	if len(profiles) != 5 {
		t.Fatalf("profiles: %d", len(profiles))
	}
	pin := func(id int64, p string) int {
		return h.do("POST", "/auth/pin", map[string]any{"userId": id, "pin": p, "device": device}, &res)
	}
	if code := pin(kid.Id, "4321"); code != 200 || res.User.Id != kid.Id {
		t.Fatalf("correct PIN: %d", code)
	}
	if code := pin(tot.Id, ""); code != 200 {
		t.Fatalf("managed profile without PIN: %d", code)
	}
	if code := pin(adult.Id, ""); code != 401 {
		t.Fatalf("user with a password but no PIN must use the password: %d", code)
	}
	if code := h.do("POST", "/auth/pin", map[string]any{"userId": adult.Id, "password": "adult-password", "device": device}, &res); code != 200 {
		t.Fatalf("password via profile picker: %d", code)
	}
	if code := pin(guest.Id, ""); code != 200 {
		t.Fatalf("user with neither PIN nor password opens with a tap: %d", code)
	}
	for i := 0; i < 5; i++ {
		if code := pin(kid.Id, "0000"); code != 401 {
			t.Fatalf("wrong PIN %d: %d", i, code)
		}
	}
	if code := pin(kid.Id, "4321"); code != 429 {
		t.Fatalf("lockout after 5 wrong PINs: %d", code)
	}

	// Turning PIN sign-in off hides profiles and refuses PINs.
	h.do("POST", "/auth/login", map[string]any{"username": "admin", "password": "correct horse", "device": device}, &res)
	h.token = res.Token
	if code := h.do("PATCH", "/settings", map[string]any{"security": map[string]any{"pinSignIn": "off"}}, nil); code != 200 {
		t.Fatalf("disable: %d", code)
	}
	h.token = ""
	h.do("GET", "/auth/profiles", nil, &profiles)
	if len(profiles) != 0 {
		t.Fatalf("profiles listed while disabled: %d", len(profiles))
	}
	if code := pin(tot.Id, ""); code != 403 {
		t.Fatalf("PIN accepted while disabled: %d", code)
	}
}

func (h *harness) raw(method, path string, body []byte) (*http.Response, []byte) {
	h.t.Helper()
	req, _ := http.NewRequest(method, h.srv.URL+path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/octet-stream")
	if h.token != "" {
		req.Header.Set("Authorization", "Bearer "+h.token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp, b
}

func TestAvatars(t *testing.T) {
	h := newHarness(t)
	var res api.AuthResult
	h.do("POST", "/setup", map[string]any{"serverName": "T", "username": "admin", "password": "correct horse", "device": device}, &res)
	adminToken := res.Token
	h.token = adminToken
	var me, kid api.User
	h.do("GET", "/me", nil, &me)
	h.do("POST", "/users", map[string]any{"username": "kid", "isManaged": true}, &kid)

	var pic bytes.Buffer
	png.Encode(&pic, image.NewRGBA(image.Rect(0, 0, 200, 100)))
	resp, body := h.raw("PUT", fmt.Sprintf("/api/v1/users/%d/avatar", kid.Id), pic.Bytes())
	var updated api.User
	json.Unmarshal(body, &updated)
	if resp.StatusCode != 200 || updated.AvatarUrl == nil {
		t.Fatalf("admin upload for kid: %d %s", resp.StatusCode, body)
	}
	if resp, _ := h.raw("PUT", fmt.Sprintf("/api/v1/users/%d/avatar", kid.Id), []byte("not an image")); resp.StatusCode != 400 {
		t.Fatalf("garbage upload: %d", resp.StatusCode)
	}

	// The sign-in picker shows it without a token.
	h.token = ""
	var profiles []api.Profile
	h.do("GET", "/auth/profiles", nil, &profiles)
	var url string
	for _, p := range profiles {
		if p.Id == kid.Id && p.AvatarUrl != nil {
			url = *p.AvatarUrl
		}
	}
	if url != *updated.AvatarUrl {
		t.Fatalf("profile avatarUrl %q, user %q", url, *updated.AvatarUrl)
	}
	resp, body = h.raw("GET", url, nil)
	if resp.StatusCode != 200 || resp.Header.Get("Content-Type") != "image/jpeg" || !strings.Contains(resp.Header.Get("Cache-Control"), "immutable") {
		t.Fatalf("get avatar: %d %v", resp.StatusCode, resp.Header)
	}
	if img, err := jpeg.Decode(bytes.NewReader(body)); err != nil || img.Bounds().Dx() != avatars.Size || img.Bounds().Dy() != avatars.Size {
		t.Fatalf("stored avatar: %v", err)
	}

	// Users can change only their own.
	h.do("POST", "/auth/pin", map[string]any{"userId": kid.Id, "device": device}, &res)
	h.token = res.Token
	if resp, _ := h.raw("PUT", fmt.Sprintf("/api/v1/users/%d/avatar", me.Id), pic.Bytes()); resp.StatusCode != 403 {
		t.Fatalf("kid changed admin's picture: %d", resp.StatusCode)
	}
	if resp, body := h.raw("DELETE", fmt.Sprintf("/api/v1/users/%d/avatar", kid.Id), nil); resp.StatusCode != 200 || strings.Contains(string(body), "avatarUrl") {
		t.Fatalf("kid removed own picture: %d %s", resp.StatusCode, body)
	}
	if resp, _ := h.raw("GET", url, nil); resp.StatusCode != 404 {
		t.Fatalf("removed avatar still served: %d", resp.StatusCode)
	}
}
