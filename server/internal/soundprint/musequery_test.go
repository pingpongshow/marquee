package soundprint

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"marquee/internal/db"
)

// museLibrary: two artists. Rock (artist tag) has loud tracks 1–10 from the 1990s, with
// 1–3 calm; Jazz (album tag) has tracks 11–20 from the 1960s. Sound axes: 0 rock, 1 jazz,
// 2 calm.
func museLibrary(t *testing.T) (*Service, *sql.DB) {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	d, err := db.Open(ctx, filepath.Join(dir, "t.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := d.Exec(q, args...); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	exec(`INSERT INTO libraries (id, name, type) VALUES (1, 'Music', 'music')`)
	exec(`INSERT INTO users (id, username, display_name) VALUES (1, 'u', 'U')`)
	exec(`INSERT INTO items (id, library_id, type, title, sort_title) VALUES (1001, 1, 'artist', 'The Rockers', 'r'), (1002, 1, 'artist', 'Jazz Cats', 'j')`)
	exec(`INSERT INTO items (id, library_id, type, parent_id, title, sort_title) VALUES (2001, 1, 'album', 1001, 'Loud', 'l'), (2002, 1, 'album', 1002, 'Cool', 'c')`)
	exec(`INSERT INTO tags (id, kind, name) VALUES (1, 'genre', 'Rock'), (2, 'genre', 'Jazz')`)
	exec(`INSERT INTO item_tags (item_id, tag_id) VALUES (1001, 1), (2002, 2)`)
	ix := NewIndex()
	for i := 1; i <= 20; i++ {
		artist, album, year, v := int64(1001), int64(2001), 1990+i%10, []float32{1, 0, 0.05 * float32(i), 0}
		if i <= 3 {
			v[2] = 0.9
		}
		if i > 10 {
			artist, album, year, v = 1002, 2002, 1960+i%10, []float32{0, 1, 0.05 * float32(i-10), 0}
		}
		exec(`INSERT INTO items (id, library_id, type, parent_id, grandparent_id, title, sort_title, duration_ms) VALUES (?, 1, 'track', ?, ?, 't', 't', ?)`,
			i, album, artist, 200000+i*1000)
		ix.Put(&Track{ItemID: int64(i), AlbumID: int64(i), ArtistID: artist, LibraryID: 1, Year: year, Vec: normalize(v)})
	}
	return &Service{DB: d, Index: ix, Client: &Client{BaseURL: "http://127.0.0.1:1"}}, d
}

func contains(ids []int64, lo, hi int64) bool {
	for _, id := range ids {
		if id < lo || id > hi {
			return false
		}
	}
	return len(ids) > 0
}

func TestMuseQueryFilters(t *testing.T) {
	s, _ := museLibrary(t)
	ctx := context.Background()
	// Genre from the artist's tag; the sidecar is down but filters alone still make a list.
	st, err := s.MuseQuery(ctx, 1, MusicQuery{Genres: []string{"rock"}, Title: "Rock"}, 5, Options{})
	if err != nil || len(st.IDs) != 5 || !contains(st.IDs, 1, 10) || st.Title != "Rock" {
		t.Fatalf("rock: %v %v", st, err)
	}
	// Genre from the album's tag, and decades.
	st, _ = s.MuseQuery(ctx, 1, MusicQuery{Genres: []string{"Jazz"}, YearFrom: 1960, YearTo: 1969}, 20, Options{})
	if !contains(st.IDs, 11, 20) || len(st.IDs) < 5 {
		t.Fatalf("jazz 60s: %v", st.IDs)
	}
	st, _ = s.MuseQuery(ctx, 1, MusicQuery{YearFrom: 1990, YearTo: 1999}, 20, Options{})
	if !contains(st.IDs, 1, 10) {
		t.Fatalf("90s: %v", st.IDs)
	}
	// Only an artist.
	st, _ = s.MuseQuery(ctx, 1, MusicQuery{ArtistIDs: []int64{1002}}, 20, Options{})
	if !contains(st.IDs, 11, 20) {
		t.Fatalf("by artist: %v", st.IDs)
	}
	// Track length: tracks 1–5 are under 3:26.
	st, _ = s.MuseQuery(ctx, 1, MusicQuery{MaxTrackMinutes: 3, Genres: []string{"Rock"}}, 20, Options{})
	if len(st.IDs) != 0 {
		// every track is over 3 minutes, so nothing passes and the genre relax doesn't help
		t.Fatalf("length: %v", st.IDs)
	}
	// Playlist length: about 10 minutes of ~3.5-minute tracks is 3 tracks (the first may run over).
	st, _ = s.MuseQuery(ctx, 1, MusicQuery{Genres: []string{"Rock"}, TotalMinutes: 10}, 30, Options{})
	if len(st.IDs) < 2 || len(st.IDs) > 3 {
		t.Fatalf("total minutes: %v", st.IDs)
	}
	// Unplayed: tracks 1–8 have been played.
	for i := 1; i <= 8; i++ {
		s.DB.Exec(`INSERT INTO user_item_state (user_id, item_id, play_count) VALUES (1, ?, 1)`, i)
	}
	st, _ = s.MuseQuery(ctx, 1, MusicQuery{Unplayed: true, Genres: []string{"Rock"}}, 20, Options{})
	if len(st.IDs) == 0 || !contains(st.IDs, 9, 20) {
		t.Fatalf("unplayed: %v", st.IDs)
	}
}

func TestMuseQueryRelaxesGenre(t *testing.T) {
	s, _ := museLibrary(t)
	s.DB.Exec(`INSERT INTO tags (id, kind, name) VALUES (3, 'genre', 'Lounge')`)
	s.DB.Exec(`INSERT INTO item_tags (item_id, tag_id) VALUES (11, 3)`)
	// One Lounge track: the filter is relaxed into a hint, so the list fills with what's
	// closest to it (the jazz).
	st, err := s.MuseQuery(context.Background(), 1, MusicQuery{Genres: []string{"Lounge"}}, 6, Options{})
	if err != nil || len(st.IDs) != 6 || st.IDs[0] != 11 || !contains(st.IDs, 11, 20) {
		t.Fatalf("relaxed: %v %v", st.IDs, err)
	}
}

func TestMuseQueryBlendsSoundSeedsAndBoost(t *testing.T) {
	var texts []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct{ Texts []string }
		json.NewDecoder(r.Body).Decode(&in)
		texts = append(texts, in.Texts...)
		out := make([][]float32, len(in.Texts))
		for i, t := range in.Texts {
			out[i] = []float32{0, 0, 0, 1}
			if strings.Contains(t, "calm") {
				out[i] = []float32{0, 0, 1, 0}
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"model": "clap", "embeddings": out})
	}))
	defer srv.Close()
	s, _ := museLibrary(t)
	s.Client.BaseURL = srv.URL
	ctx := context.Background()

	// Calm rock: the calm rock tracks come first, and CLAP heard three phrasings.
	st, err := s.MuseQuery(ctx, 1, MusicQuery{Genres: []string{"Rock"}, Sound: "calm"}, 3, Options{})
	if err != nil || !contains(st.IDs, 1, 3) {
		t.Fatalf("calm rock: %v %v", st.IDs, err)
	}
	if len(texts) != 3 || texts[0] != "music that sounds calm" {
		t.Fatalf("phrasings %q", texts)
	}
	// Like Jazz Cats with no other filter: jazz first.
	st, _ = s.MuseQuery(ctx, 1, MusicQuery{LikeArtistIDs: []int64{1002}}, 5, Options{})
	if !contains(st.IDs, 11, 20) {
		t.Fatalf("like jazz: %v", st.IDs)
	}
	// A lyric theme's boost lifts a rock track into a jazz-like list.
	st, _ = s.MuseQuery(ctx, 1, MusicQuery{LikeArtistIDs: []int64{1002}}, 5, Options{Boost: map[int64]float64{7: 2}})
	found := false
	for _, id := range st.IDs {
		found = found || id == 7
	}
	if !found {
		t.Fatalf("boosted track missing: %v", st.IDs)
	}
}

func TestMuseQueryNeedsSoundWithoutFilters(t *testing.T) {
	s, _ := museLibrary(t)
	_, err := s.MuseQuery(context.Background(), 1, MusicQuery{Sound: "rainy sunday"}, 5, Options{})
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("want ErrUnavailable without the sidecar, got %v", err)
	}
}

func TestBlendVectors(t *testing.T) {
	v := BlendVectors([][]float32{{1, 0}, nil, {0, 1}}, []float64{0.5, 0.3, 0.2})
	if v[0] <= v[1] || v[1] <= 0 {
		t.Fatalf("blend %v", v)
	}
	if BlendVectors([][]float32{nil, nil}, []float64{1, 1}) != nil {
		t.Fatal("all nil should be nil")
	}
}
