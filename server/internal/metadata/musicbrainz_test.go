package metadata

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"marquee/internal/db"
	"marquee/internal/metadata/musicbrainz"
)

func fakeMusicBrainz(t *testing.T) (*httptest.Server, *int) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.Header.Get("User-Agent") == "" {
			t.Error("no user agent")
		}
		q := r.URL.Query().Get("query")
		switch {
		case r.URL.Path == "/artist" && strings.Contains(q, `"Los Lobos"`):
			io.WriteString(w, `{"artists":[{"id":"lobos","name":"Los Lobos","sort-name":"Lobos, Los","score":100},{"id":"x","name":"Los Lobos Tribute","score":80}]}`)
		case r.URL.Path == "/artist" && strings.Contains(q, `"Twins"`):
			io.WriteString(w, `{"artists":[{"id":"t1","name":"Twins","score":100},{"id":"t2","name":"Twins","score":100}]}`)
		case r.URL.Path == "/artist":
			io.WriteString(w, `{"artists":[]}`)
		case r.URL.Path == "/artist/lobos":
			io.WriteString(w, `{"id":"lobos","name":"Los Lobos","sort-name":"Lobos, Los","genres":[{"name":"roots rock","count":5},{"name":"rock","count":9},{"name":"tex-mex","count":2},{"name":"chicano","count":1}],
				"relations":[{"type":"wikidata","url":{"resource":"https://www.wikidata.org/wiki/Q1"}}]}`)
		case r.URL.Path == "/wiki/Special:EntityData/Q1.json":
			io.WriteString(w, `{"entities":{"Q1":{"sitelinks":{"enwiki":{"title":"Los Lobos"}}}}}`)
		case r.URL.Path == "/en/api/rest_v1/page/summary/Los_Lobos":
			io.WriteString(w, `{"type":"standard","extract":"Los Lobos is an American rock band from East Los Angeles."}`)
		case r.URL.Path == "/1/popularity/top-recordings-for-artist/lobos":
			io.WriteString(w, `[{"recording_mbid":"r-missing","recording_name":"Not Here","total_listen_count":900},
				{"recording_mbid":"r-kiko","recording_name":"Kiko and the Lavender Moon","total_listen_count":500},
				{"recording_mbid":"r-other","recording_name":"Angels With Dirty Faces","total_listen_count":300}]`)
		case strings.HasPrefix(r.URL.Path, "/1/popularity/"):
			http.NotFound(w, r)
		case r.URL.Path == "/artist/tagged":
			io.WriteString(w, `{"id":"tagged","name":"Johnny Cash","sort-name":"Cash, Johnny","genres":[{"name":"country","count":7}]}`)
		case r.URL.Path == "/release/rel1":
			io.WriteString(w, `{"id":"rel1","release-group":{"id":"rg1"}}`)
		case r.URL.Path == "/release-group/rg1":
			io.WriteString(w, `{"id":"rg1","title":"Live at Folsom","primary-type":"Album","secondary-types":["Live"],"first-release-date":"1968-05-06","genres":[{"name":"country","count":3}]}`)
		case r.URL.Path == "/release-group" && strings.Contains(q, `"Kiko"`):
			io.WriteString(w, `{"release-groups":[{"id":"rg2","title":"Kiko","score":100,"primary-type":"Album","artist-credit":[{"name":"Los Lobos","artist":{"id":"lobos","name":"Los Lobos"}}]}]}`)
		case r.URL.Path == "/release-group/rg2":
			io.WriteString(w, `{"id":"rg2","title":"Kiko","primary-type":"Album","first-release-date":"1992-05-26","genres":[{"name":"Rock","count":2}]}`)
		case r.URL.Path == "/release-group":
			io.WriteString(w, `{"release-groups":[]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	return srv, &hits
}

func TestEnrichMusic(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(dir, "t.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	srv, hits := fakeMusicBrainz(t)
	defer srv.Close()
	mustExec(t, d, `INSERT INTO libraries(id, name, type) VALUES (1, 'Music', 'music')`)
	mustExec(t, d, `INSERT INTO items(id, library_id, type, title, sort_title) VALUES (1, 1, 'artist', 'Los Lobos', 'Los Lobos')`)
	mustExec(t, d, `INSERT INTO items(id, library_id, type, title, sort_title, parent_id, originally_available_at) VALUES (2, 1, 'album', 'Kiko', 'Kiko', 1, '1992')`)
	mustExec(t, d, `INSERT INTO items(id, library_id, type, title, sort_title) VALUES (3, 1, 'artist', 'Johnny Cash', 'Johnny Cash')`)
	mustExec(t, d, `INSERT INTO external_ids(item_id, provider, value) VALUES (3, 'musicbrainz', 'tagged')`)
	mustExec(t, d, `INSERT INTO items(id, library_id, type, title, sort_title, parent_id) VALUES (4, 1, 'album', 'At Folsom Prison', 'At Folsom Prison', 3)`)
	mustExec(t, d, `INSERT INTO external_ids(item_id, provider, value) VALUES (4, 'musicbrainz', 'rel1')`)
	mustExec(t, d, `INSERT INTO items(id, library_id, type, title, sort_title) VALUES (5, 1, 'artist', 'Twins', 'Twins')`)
	mustExec(t, d, `INSERT INTO tags(kind, name) VALUES ('genre', 'Roots-Rock')`)
	mustExec(t, d, `INSERT INTO items(id, library_id, type, title, sort_title, parent_id, grandparent_id) VALUES (20, 1, 'track', 'Angels with Dirty Faces', 'a', 2, 1)`)
	mustExec(t, d, `INSERT INTO items(id, library_id, type, title, sort_title, parent_id, grandparent_id) VALUES (21, 1, 'track', 'Kiko & the Lavender Moon (live)', 'k', 2, 1)`)
	mustExec(t, d, `INSERT INTO external_ids(item_id, provider, value) VALUES (21, 'musicbrainz', 'r-kiko')`)

	mb := musicbrainz.New("test")
	mb.BaseURL, mb.Interval = srv.URL, time.Millisecond
	s := &Service{DB: d, mb: mb, web: &musicbrainz.Web{Wikidata: srv.URL, Wikipedia: srv.URL + "/%s", ListenBrainz: srv.URL, UserAgent: "test", HTTP: srv.Client()}}
	msg, err := s.EnrichMusic(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg, "5 artists and albums (4 found)") {
		t.Errorf("summary: %s", msg)
	}
	str := func(q string, args ...any) string {
		var v string
		d.QueryRow(q, args...).Scan(&v)
		return v
	}
	genres := func(id int) string {
		return str(`SELECT group_concat(name, ',') FROM (SELECT t.name FROM item_tags it JOIN tags t ON t.id = it.tag_id WHERE it.item_id = ? ORDER BY t.name)`, id)
	}
	if v := str(`SELECT sort_title FROM items WHERE id = 1`); v != "Lobos" {
		t.Errorf("article dropped: %q", v)
	}
	if v := str(`SELECT sort_title FROM items WHERE id = 3`); v != "Johnny Cash" {
		t.Errorf("people keep their names: %q", v)
	}
	// Top three by votes, reusing an existing spelling.
	if v := genres(1); v != "Rock,Roots-Rock,Tex-Mex" {
		t.Errorf("artist genres: %q", v)
	}
	if v := str(`SELECT release_type || ' ' || originally_available_at FROM items WHERE id = 4`); v != "live 1968-05-06" {
		t.Errorf("tagged album: %q", v)
	}
	if v := str(`SELECT release_type || ' ' || originally_available_at FROM items WHERE id = 2`); v != "album 1992-05-26" {
		t.Errorf("searched album: %q", v)
	}
	if v := str(`SELECT value FROM external_ids WHERE item_id = 1 AND provider = 'musicbrainz'`); v != "lobos" {
		t.Errorf("mbid stored: %q", v)
	}
	if v := str(`SELECT COUNT(*) FROM external_ids WHERE item_id = 5`); v != "0" {
		t.Errorf("ambiguous artist matched")
	}
	// A bio from Wikipedia, and popular tracks matched by recording id, then title.
	if v := str(`SELECT summary FROM items WHERE id = 1`); !strings.HasPrefix(v, "Los Lobos is an American rock band") {
		t.Errorf("bio: %q", v)
	}
	if v := str(`SELECT group_concat(track_id, ',') FROM (SELECT track_id FROM music_popular WHERE artist_id = 1 ORDER BY rank)`); v != "21,20" {
		t.Errorf("popular: %q", v)
	}
	// Each item is looked up once.
	before := *hits
	if msg, _ := s.EnrichMusic(ctx); *hits != before || !strings.Contains(msg, "0 artists") {
		t.Errorf("second run: %s (%d requests)", msg, *hits-before)
	}
}

func TestArticleSort(t *testing.T) {
	for _, c := range [][3]string{
		{"Los Lobos", "Lobos, Los", "Lobos"},
		{"Die Ärzte", "Ärzte, Die", "Ärzte"},
		{"The Beatles", "Beatles, The", "Beatles"},
		{"Johnny Cash", "Cash, Johnny", ""},
		{"Crosby, Stills & Nash", "Crosby, Stills & Nash", ""},
	} {
		if got := articleSort(c[0], c[1]); got != c[2] {
			t.Errorf("%s: %q, want %q", c[0], got, c[2])
		}
	}
}
