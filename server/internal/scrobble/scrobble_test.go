package scrobble

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"marquee/internal/db"
)

func TestListenBrainz(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	d, err := db.Open(ctx, filepath.Join(dir, "t.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	var got []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Token good" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/1/validate-token":
			w.Write([]byte(`{"valid":true,"user_name":"lister"}`))
		case "/1/submit-listens":
			var b map[string]any
			json.NewDecoder(r.Body).Decode(&b)
			got = append(got, b)
			w.Write([]byte(`{"status":"ok"}`))
		}
	}))
	defer srv.Close()
	d.Exec(`INSERT INTO users (id, username, display_name) VALUES (1, 'u', 'U')`)
	d.Exec(`INSERT INTO libraries (id, name, type) VALUES (1, 'Music', 'music')`)
	d.Exec(`INSERT INTO items (id, library_id, type, title, sort_title) VALUES (10, 1, 'artist', 'Los Lobos', 'Lobos')`)
	d.Exec(`INSERT INTO items (id, library_id, type, title, sort_title, parent_id) VALUES (11, 1, 'album', 'Kiko', 'Kiko', 10)`)
	d.Exec(`INSERT INTO items (id, library_id, type, title, sort_title, parent_id, grandparent_id, idx, duration_ms) VALUES (12, 1, 'track', 'Kiko', 'Kiko', 11, 10, 3, 180000)`)
	d.Exec(`INSERT INTO external_ids (item_id, provider, value) VALUES (12, 'musicbrainz', 'rec-1')`)

	s := &Service{DB: d, Base: srv.URL}
	if _, err := s.Connect(ctx, 1, "bad"); err != ErrBadToken {
		t.Errorf("bad token: %v", err)
	}
	st, err := s.Connect(ctx, 1, "good")
	if err != nil || st.Username != "lister" || !s.Status(ctx, 1, "listenbrainz").Connected {
		t.Fatalf("connect: %+v %v", st, err)
	}
	start := time.Unix(1_700_000_000, 0)
	s.Played(ctx, 1, 12, true, start)
	s.Played(ctx, 1, 12, false, start)
	s.Played(ctx, 1, 11, false, start) // not a track
	if len(got) != 2 || got[0]["listen_type"] != "playing_now" || got[1]["listen_type"] != "single" {
		t.Fatalf("submissions: %v", got)
	}
	l := got[1]["payload"].([]any)[0].(map[string]any)
	meta := l["track_metadata"].(map[string]any)
	if l["listened_at"].(float64) != 1_700_000_000 || meta["artist_name"] != "Los Lobos" || meta["release_name"] != "Kiko" ||
		meta["additional_info"].(map[string]any)["recording_mbid"] != "rec-1" {
		t.Errorf("listen: %v", l)
	}
	s.Disconnect(ctx, 1, "listenbrainz")
	s.Played(ctx, 1, 12, false, start)
	if len(got) != 2 || s.Status(ctx, 1, "listenbrainz").Connected {
		t.Error("still scrobbling after disconnecting")
	}
}

func TestLastFM(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	d, err := db.Open(ctx, filepath.Join(dir, "t.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	d.Exec(`INSERT INTO users (id, username, display_name) VALUES (1, 'u', 'U')`)
	d.Exec(`INSERT INTO libraries (id, name, type) VALUES (1, 'Music', 'music')`)
	d.Exec(`INSERT INTO items (id, library_id, type, title, sort_title) VALUES (10, 1, 'artist', 'Los Lobos', 'Lobos')`)
	d.Exec(`INSERT INTO items (id, library_id, type, title, sort_title, parent_id) VALUES (11, 1, 'album', 'Kiko', 'Kiko', 10)`)
	d.Exec(`INSERT INTO items (id, library_id, type, title, sort_title, parent_id, grandparent_id, duration_ms) VALUES (12, 1, 'track', 'Kiko', 'Kiko', 11, 10, 180000)`)
	var calls []url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		p := r.PostForm
		// Check the signature the way Last.fm does.
		names := []string{}
		for k := range p {
			if k != "api_sig" && k != "format" {
				names = append(names, k)
			}
		}
		sort.Strings(names)
		var b strings.Builder
		for _, k := range names {
			b.WriteString(k + p.Get(k))
		}
		sum := md5.Sum([]byte(b.String() + "s3cret"))
		if hex.EncodeToString(sum[:]) != p.Get("api_sig") || p.Get("api_key") != "key" {
			w.Write([]byte(`{"error":13,"message":"Invalid method signature supplied"}`))
			return
		}
		calls = append(calls, p)
		switch p.Get("method") {
		case "auth.getSession":
			if p.Get("token") != "tok" {
				w.Write([]byte(`{"error":4,"message":"Invalid authentication token supplied"}`))
				return
			}
			w.Write([]byte(`{"session":{"name":"fm-user","key":"sess"}}`))
		default:
			w.Write([]byte(`{}`))
		}
	}))
	defer srv.Close()
	var key, secret string
	s := &Service{DB: d, LastFMBase: srv.URL, LastFM: func() (string, string) { return key, secret }}
	if _, err := s.LastFMAuthURL("http://x/cb"); err != ErrLastFMOff {
		t.Errorf("without keys: %v", err)
	}
	key, secret = "key", "s3cret"
	u, _ := s.LastFMAuthURL("http://x/cb")
	if u != "https://www.last.fm/api/auth/?api_key=key&cb=http%3A%2F%2Fx%2Fcb" {
		t.Errorf("auth url: %s", u)
	}
	if _, err := s.ConnectLastFM(ctx, 1, "wrong"); !errors.Is(err, ErrBadToken) {
		t.Errorf("bad token: %v", err)
	}
	st, err := s.ConnectLastFM(ctx, 1, "tok")
	if err != nil || st.Username != "fm-user" {
		t.Fatalf("connect: %+v %v", st, err)
	}
	s.Played(ctx, 1, 12, true, time.Unix(1_700_000_000, 0))
	s.Played(ctx, 1, 12, false, time.Unix(1_700_000_000, 0))
	// The rejected token, the session, then playing now and the scrobble.
	if len(calls) != 4 || calls[2].Get("method") != "track.updateNowPlaying" || calls[3].Get("method") != "track.scrobble" ||
		calls[3].Get("sk") != "sess" || calls[3].Get("timestamp") != "1700000000" || calls[3].Get("artist") != "Los Lobos" || calls[3].Get("duration") != "180" {
		t.Errorf("calls: %v", calls)
	}
	if st := s.Status(ctx, 1, "lastfm"); !st.Connected || st.Error != "" {
		t.Errorf("status: %+v", st)
	}
}
