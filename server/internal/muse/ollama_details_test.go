package muse

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"marquee/internal/ai"
	"marquee/internal/db"
)

// A fake Ollama: chat answers a music reading (and checks the schema asks for the new
// fields); embed maps "iceland"/"icelandic" texts to one axis and everything else to another.
func fakeOllamaDetails(t *testing.T) *ai.Client {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in map[string]any
		json.NewDecoder(r.Body).Decode(&in)
		switch r.URL.Path {
		case "/api/chat":
			format, _ := json.Marshal(in["format"])
			for _, f := range []string{"tempoMin", "energy", "popularity", "origins", "artistType", "albumTypes", "describe"} {
				if !strings.Contains(string(format), `"`+f+`"`) {
					t.Errorf("the schema lacks %s", f)
				}
			}
			json.NewEncoder(w).Encode(map[string]any{"message": map[string]any{"role": "assistant", "content": `{"genres":[],"yearFrom":0,"yearTo":0,
				"artists":[],"likeArtists":[],"favourites":false,"unplayed":false,"recentlyAdded":false,"rediscover":false,"maxTrackMinutes":0,
				"totalMinutes":0,"sound":"dreamy","theme":"","tempoMin":0,"tempoMax":95,"energy":"low","mode":"","key":"","popularity":"hits",
				"origins":[],"artistType":"","gender":"","activeFrom":0,"activeTo":0,"albumTypes":[],"excludeAlbumTypes":[],
				"describe":"icelandic experimental artists","name":"Slow North"}`}})
		case "/api/embed":
			var out [][]float32
			for _, x := range in["input"].([]any) {
				s := strings.ToLower(x.(string))
				if strings.Contains(s, "iceland") {
					out = append(out, []float32{1, 0.1})
				} else {
					out = append(out, []float32{0.1, 1})
				}
			}
			json.NewEncoder(w).Encode(map[string]any{"embeddings": out})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return &ai.Client{Config: func() ai.Config {
		return ai.Config{Enabled: true, URL: srv.URL, ChatModel: "qwen3:8b", EmbedModel: "mxbai-embed-large"}
	}}
}

func TestMusicDetailsThroughOllama(t *testing.T) {
	c := fakeOllamaDetails(t)
	r := ReadMusic(context.Background(), c, detailLib, "slow dreamy hits by icelandic experimental artists")
	q := r.Query
	if !r.AI || q.TempoMax != 95 || q.Energy != "low" || q.Popular != 1 || q.Describe != "icelandic experimental artists" || q.Title != "Slow North" {
		t.Fatalf("reading %+v", r)
	}

	// The artist and album index: descriptions from music_info, tags and bios, then Rank.
	ctx := context.Background()
	dir := t.TempDir()
	d, err := db.Open(ctx, filepath.Join(dir, "t.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	for _, q := range []string{
		`INSERT INTO libraries(id, name, type) VALUES (1, 'Music', 'music')`,
		`INSERT INTO items(id, library_id, type, title, sort_title, summary) VALUES (1, 1, 'artist', 'Björk', 'b', 'A singer and songwriter.')`,
		`INSERT INTO items(id, library_id, type, title, sort_title) VALUES (2, 1, 'artist', 'The Smiths', 's')`,
		`INSERT INTO items(id, library_id, type, title, sort_title, parent_id, year) VALUES (3, 1, 'album', 'Homogenic', 'h', 1, 1997)`,
		`INSERT INTO music_info(item_id, kind, found, version, checked_at, artist_type, gender, country) VALUES (1, 'artist', 1, 2, 'x', 'person', 'female', 'IS')`,
		`INSERT INTO music_info(item_id, kind, found, version, checked_at, artist_type, country) VALUES (2, 'artist', 1, 2, 'x', 'group', 'GB')`,
	} {
		if _, err := d.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	m := &MusicDocs{DB: d, AI: c, Enabled: func() bool { return true }}
	if msg, err := m.Run(ctx); err != nil || !strings.Contains(msg, "Indexed 3") {
		t.Fatalf("run: %q %v", msg, err)
	}
	if msg, _ := m.Run(ctx); !strings.Contains(msg, "All 3") {
		t.Fatalf("second run should embed nothing: %q", msg)
	}
	artists, albums := m.Rank(ctx, "icelandic experimental artists")
	if len(artists) != 2 || artists[1] <= artists[2] || len(albums) != 1 {
		t.Fatalf("rank: %v %v", artists, albums)
	}
	// A changed description is embedded again.
	d.Exec(`UPDATE music_info SET country = 'SE' WHERE item_id = 2`)
	if msg, _ := m.Run(ctx); !strings.Contains(msg, "Indexed 1") {
		t.Fatalf("re-embed on change: %q", msg)
	}
	// AI off: no ranking.
	m.Enabled = func() bool { return false }
	if a, b := m.Rank(ctx, "anything"); a != nil || b != nil {
		t.Fatal("ranked with AI off")
	}
}
