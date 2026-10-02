package scrobble

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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
	if err != nil || st.Username != "lister" || !s.Status(ctx, 1).Connected {
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
	s.Disconnect(ctx, 1)
	s.Played(ctx, 1, 12, false, start)
	if len(got) != 2 || s.Status(ctx, 1).Connected {
		t.Error("still scrobbling after disconnecting")
	}
}
