package metadata

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"marquee/internal/db"
	"marquee/internal/metadata/anilist"
)

func TestEnrichAnime(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(dir, "t.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	titan := `{"id":16498,"idMal":16498,"title":{"romaji":"Shingeki no Kyojin","english":"Attack on Titan","native":"進撃の巨人"},
		"synonyms":["AoT","SnK"],"format":"TV","genres":["Action","Drama"],"averageScore":85,"description":"Centuries ago…<br><br>(Source: Kodansha)",
		"startDate":{"year":2013},"studios":{"nodes":[{"name":"Wit Studio"}]},
		"tags":[{"name":"Military","rank":90,"category":"Theme"},{"name":"Spoiler","rank":99,"isMediaSpoiler":true},{"name":"Gore","rank":85},{"name":"Weak","rank":40}]}`
	frieren := `{"id":154587,"idMal":52991,"title":{"romaji":"Sousou no Frieren","english":"Frieren: Beyond Journey's End","native":"葬送のフリーレン"},
		"format":"TV","genres":["Adventure","Fantasy"],"averageScore":90,"startDate":{"year":2023},"studios":{"nodes":[{"name":"MADHOUSE"}]}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/mapping.json" {
			io.WriteString(w, `[{"anilist_id":16498,"themoviedb_id":{"tv":1429},"season":{"tmdb":1}},{"anilist_id":20958,"themoviedb_id":{"tv":1429},"season":{"tmdb":2}},
				{"anilist_id":99,"themoviedb_id":{"movie":5}}]`)
			return
		}
		var q struct {
			Query     string         `json:"query"`
			Variables map[string]any `json:"variables"`
		}
		json.NewDecoder(r.Body).Decode(&q)
		switch {
		case strings.Contains(q.Query, "Media(id"):
			if q.Variables["id"].(float64) != 16498 {
				t.Errorf("show mapped to %v, want its first season", q.Variables["id"])
			}
			io.WriteString(w, `{"data":{"Media":`+titan+`}}`)
		case q.Variables["s"] == "Frieren: Beyond Journey's End":
			io.WriteString(w, `{"data":{"Page":{"media":[`+frieren+`]}}}`)
		default:
			io.WriteString(w, `{"data":{"Page":{"media":[]}}}`)
		}
	}))
	defer srv.Close()
	mustExec(t, d, `INSERT INTO libraries(id, name, type) VALUES (1, 'Anime', 'anime')`)
	mustExec(t, d, `INSERT INTO items(id, library_id, type, title, sort_title, year) VALUES (1, 1, 'show', 'Attack on Titan', 'Attack on Titan', 2013)`)
	mustExec(t, d, `INSERT INTO external_ids(item_id, provider, value) VALUES (1, 'tmdb', '1429')`)
	mustExec(t, d, `INSERT INTO items(id, library_id, type, title, sort_title, year, summary) VALUES (2, 1, 'show', 'Frieren: Beyond Journey''s End', 'Frieren', 2023, 'TMDB text')`)
	mustExec(t, d, `INSERT INTO items(id, library_id, type, title, sort_title, year) VALUES (3, 1, 'show', 'Frieren: Beyond Journey''s End', 'Frieren', 1990)`) // wrong year
	mustExec(t, d, `INSERT INTO tags(kind, name) VALUES ('genre', 'Action')`)

	c := anilist.New("test")
	c.URL, c.MappingURL, c.Interval = srv.URL, srv.URL+"/mapping.json", time.Millisecond
	s := &Service{DB: d, ani: c, CacheDir: dir}
	msg, err := s.EnrichAnime(ctx)
	if err != nil || !strings.Contains(msg, "3 anime (2 found") {
		t.Fatalf("%s %v", msg, err)
	}
	str := func(q string, args ...any) string {
		var v string
		d.QueryRow(q, args...).Scan(&v)
		return v
	}
	if v := str(`SELECT original_title || '|' || studio || '|' || anilist_score FROM items WHERE id = 1`); v != "進撃の巨人|Wit Studio|85" {
		t.Errorf("details: %q", v)
	}
	if v := str(`SELECT summary FROM items WHERE id = 1`); v != "Centuries ago…" {
		t.Errorf("summary from AniList when TMDB had none: %q", v)
	}
	if v := str(`SELECT summary FROM items WHERE id = 2`); v != "TMDB text" {
		t.Errorf("TMDB summary kept: %q", v)
	}
	if v := str(`SELECT group_concat(name, ',') FROM (SELECT t.name FROM item_tags it JOIN tags t ON t.id = it.tag_id WHERE it.item_id = 1 ORDER BY t.name)`); v != "Action,Drama,Gore,Military" {
		t.Errorf("genres and top tags: %q", v)
	}
	if v := str(`SELECT value FROM external_ids WHERE item_id = 1 AND provider = 'mal'`); v != "16498" {
		t.Errorf("mal id: %q", v)
	}
	// Searchable by its other names.
	if v := str(`SELECT rowid FROM items_fts WHERE items_fts MATCH 'shingeki'`); v != "1" {
		t.Errorf("romaji search: %q", v)
	}
	if v := str(`SELECT anilist_score FROM items WHERE id = 3`); v != "" {
		t.Errorf("wrong year matched: %q", v)
	}
	if msg, _ := s.EnrichAnime(ctx); msg != "Nothing to look up" {
		t.Errorf("second run: %s", msg)
	}
}
