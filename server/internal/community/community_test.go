package community

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"marquee/internal/db"
)

// Similar artists come from ListenBrainz (artists with a MusicBrainz id) and Last.fm (by id or
// name), matched to the library's artists by id and then by name; tags from both, artists'
// and tracks'. Everything is looked up once until it's due again, and a bad Last.fm key stops
// the Last.fm part.
func TestRun(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	d, err := db.Open(ctx, filepath.Join(dir, "t.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := d.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	exec(`INSERT INTO libraries (id, name, type) VALUES (1, 'Music', 'music')`)
	for _, a := range []struct {
		id         int64
		name, mbid string
	}{{1, "Portishead", "mb-portishead"}, {2, "Massive Attack", "mb-massive"}, {3, "Björk", ""}, {4, "Tricky", ""}} {
		exec(`INSERT INTO items (id, library_id, type, title, sort_title) VALUES (?, 1, 'artist', ?, ?)`, a.id, a.name, a.name)
		if a.mbid != "" {
			exec(`INSERT INTO external_ids (item_id, provider, value) VALUES (?, 'musicbrainz', ?)`, a.id, a.mbid)
		}
	}
	exec(`INSERT INTO items (id, library_id, type, title, sort_title) VALUES (10, 1, 'album', 'Dummy', 'Dummy')`)
	exec(`INSERT INTO items (id, library_id, type, parent_id, grandparent_id, title, sort_title) VALUES (11, 1, 'track', 10, 1, 'Roads', 'Roads')`)
	exec(`INSERT INTO external_ids (item_id, provider, value) VALUES (11, 'musicbrainz', 'rec-roads')`)

	var lbCalls, lfmCalls atomic.Int32
	lb := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lbCalls.Add(1)
		switch {
		case r.URL.Path == "/similar-artists/json":
			if r.URL.Query().Get("artist_mbids") != "mb-portishead" {
				json.NewEncoder(w).Encode([]any{})
				return
			}
			json.NewEncoder(w).Encode([]map[string]any{
				{"artist_mbid": "mb-massive", "name": "Massive Attack", "score": 7781},
				{"artist_mbid": "mb-unknown", "name": "Björk", "score": 4936}, // matched by name
				{"artist_mbid": "mb-x", "name": "Not In The Library", "score": 9000},
			})
		case r.URL.Path == "/1/metadata/artist/":
			json.NewEncoder(w).Encode([]map[string]any{
				{"artist_mbid": "mb-portishead", "tag": map[string]any{"artist": []map[string]any{{"tag": "trip-hop", "count": 11}, {"tag": "Downtempo", "count": 7}}}},
			})
		case r.URL.Path == "/1/metadata/recording/":
			json.NewEncoder(w).Encode(map[string]any{
				"rec-roads": map[string]any{"tag": map[string]any{"recording": []map[string]any{{"tag": "melancholic", "count": 3}}}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer lb.Close()
	key := "good"
	lfm := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lfmCalls.Add(1)
		q := r.URL.Query()
		if q.Get("api_key") != "good" {
			json.NewEncoder(w).Encode(map[string]any{"error": 10, "message": "Invalid API key"})
			return
		}
		switch q.Get("method") {
		case "artist.getSimilar":
			if q.Get("artist") == "Tricky" || q.Get("mbid") == "mb-massive" {
				json.NewEncoder(w).Encode(map[string]any{"similarartists": map[string]any{"artist": []map[string]any{
					{"name": "Portishead", "mbid": "mb-portishead", "match": "1"}, {"name": "Bjork", "mbid": "", "match": "0.5"}}}})
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"error": 6, "message": "The artist you supplied could not be found"})
		case "artist.getTopTags":
			json.NewEncoder(w).Encode(map[string]any{"toptags": map[string]any{"tag": []map[string]any{{"name": "trip hop", "count": 100}, {"name": "seen live", "count": 5}}}})
		case "track.getTopTags":
			json.NewEncoder(w).Encode(map[string]any{"toptags": map[string]any{"tag": []map[string]any{{"name": "sad", "count": 100}, {"name": "chill", "count": 40}}}})
		}
	}))
	defer lfm.Close()
	loaded := 0
	s := &Service{DB: d, ListenBrainz: lb.URL, Labs: lb.URL, LastFM: lfm.URL, LastFMKey: func() string { return key },
		Loaded: func(context.Context) { loaded++ }}
	msg, err := s.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg, "ListenBrainz") || !strings.Contains(msg, "Last.fm") || loaded != 1 {
		t.Errorf("message %q, loaded %d", msg, loaded)
	}
	similar := func(a int64, source string) map[int64]float64 {
		out := map[int64]float64{}
		rows, _ := d.Query(`SELECT similar_id, score FROM music_similar_artists WHERE artist_id = ? AND source = ?`, a, source)
		defer rows.Close()
		for rows.Next() {
			var id int64
			var sc float64
			rows.Scan(&id, &sc)
			out[id] = sc
		}
		return out
	}
	if got := similar(1, SourceListenBrainz); got[2] != 1 || got[3] < 0.6 || got[3] > 0.65 || len(got) != 2 {
		t.Errorf("Portishead's similar artists (ListenBrainz): %v", got)
	}
	if got := similar(4, SourceLastFM); got[1] != 1 || got[3] != 0.5 {
		t.Errorf("Tricky's similar artists (Last.fm, by name): %v", got)
	}
	if got := similar(2, SourceLastFM); got[1] != 1 {
		t.Errorf("Massive Attack's similar artists (Last.fm, by id): %v", got)
	}
	tags := func(item int64) map[string]float64 {
		out := map[string]float64{}
		rows, _ := d.Query(`SELECT tag, MAX(weight) FROM music_tags WHERE item_id = ? GROUP BY tag`, item)
		defer rows.Close()
		for rows.Next() {
			var tag string
			var w float64
			rows.Scan(&tag, &w)
			out[tag] = w
		}
		return out
	}
	if got := tags(1); got["trip hop"] != 1 || got["downtempo"] == 0 || got["seen live"] != 0 {
		t.Errorf("Portishead's tags: %v", got)
	}
	if got := tags(11); got["melancholic"] != 1 || got["sad"] != 1 || got["chill"] != 0.4 {
		t.Errorf("Roads' tags: %v", got)
	}

	// A second run looks nothing up.
	lb0, lfm0 := lbCalls.Load(), lfmCalls.Load()
	if msg, err := s.Run(ctx); err != nil || msg != "Nothing new to look up" {
		t.Errorf("second run: %q %v", msg, err)
	}
	if lbCalls.Load() != lb0 || lfmCalls.Load() != lfm0 {
		t.Error("looked things up again")
	}
	// A bad key stops Last.fm with a clear error.
	d.Exec(`DELETE FROM music_community_checked WHERE source LIKE 'lastfm%'`)
	key = "bad"
	if _, err := s.Run(ctx); err == nil || !strings.Contains(err.Error(), "API key") {
		t.Errorf("bad key: %v", err)
	}
	// Turned off: nothing.
	s.Enabled = func() bool { return false }
	if msg, _ := s.Run(ctx); !strings.Contains(msg, "off") {
		t.Errorf("off: %q", msg)
	}
	var _ *sql.DB = d
}
