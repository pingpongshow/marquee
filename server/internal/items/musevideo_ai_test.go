package items

import (
	"context"
	"strings"
	"testing"
	"time"
)

// The AI reading adds people, "like" seeds and exclusions to Muse for movies, and
// recommendations learn from abandoned titles.
func TestMuseVideoAIFieldsAndDislikes(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	s := NewStore(d)
	uid := mustExec(t, d, `INSERT INTO users(username, display_name) VALUES ('ann', 'Ann')`)
	lib := mustExec(t, d, `INSERT INTO libraries(name, type) VALUES ('Movies', 'movies')`)
	add := func(title, summary string, year int, score float64) int64 {
		return mustExec(t, d, `INSERT INTO items(library_id, type, title, sort_title, summary, year, audience_rating, duration_ms)
			VALUES (?, 'movie', ?, ?, ?, ?, ?, 6000000)`, lib, title, title, summary, year, score)
	}
	alien := add("Alien", "Terror in deep space.", 1979, 8.5)
	aliens := add("Aliens", "More terror in space, with marines.", 1986, 8.4)
	gravity := add("Gravity", "Lost in space.", 2013, 7.7)
	notebook := add("The Notebook", "A love story.", 2004, 7.8)
	heat := add("Heat", "Cops and robbers.", 1995, 8.3)
	titanic := add("Titanic", "A love story at sea.", 1997, 7.9)
	horror := mustExec(t, d, `INSERT INTO tags(kind, name) VALUES ('genre', 'Horror')`)
	mustExec(t, d, `INSERT INTO item_tags(item_id, tag_id) VALUES (?, ?), (?, ?)`, alien, horror, aliens, horror)
	scott := mustExec(t, d, `INSERT INTO people(name) VALUES ('Ridley Scott')`)
	mustExec(t, d, `INSERT INTO credits(item_id, person_id, role) VALUES (?, ?, 'director')`, alien, scott)

	idx := NewVideoIndex()
	e := &Embedder{DB: d, Index: idx, Client: &fakeEmbedder{model: "m"}}
	if _, err := e.Run(ctx); err != nil {
		t.Fatal(err)
	}
	acc := Access{UserID: uid}

	// People must be credited; names are matched case-insensitively.
	if name, _ := s.VideoPerson(ctx, "ridley scott"); name != "Ridley Scott" {
		t.Fatalf("person %q", name)
	}
	if name, _ := s.VideoPerson(ctx, "Nobody Here"); name != "" {
		t.Fatalf("unknown person %q", name)
	}
	res, err := s.MuseVideo(ctx, acc, idx, VideoQuery{People: []string{"Ridley Scott"}}, nil, 0, 10)
	if err != nil || ids(res.Items) != "Alien" {
		t.Fatalf("people: %s %v", ids(res.Items), err)
	}
	// Excluded genres.
	res, _ = s.MuseVideo(ctx, acc, idx, VideoQuery{ExcludeGenres: []string{"horror"}}, nil, 0, 10)
	if strings.Contains(ids(res.Items), "Alien") || len(res.Items) != 4 {
		t.Fatalf("not horror: %s", ids(res.Items))
	}
	// "Like Alien": the seed's meaning ranks, and the seed itself isn't suggested.
	id, title, ok, err := s.VideoByTitle(ctx, acc, "alien", 0)
	if err != nil || !ok || id != alien || title != "Alien" {
		t.Fatalf("by title: %d %q %v %v", id, title, ok, err)
	}
	if _, _, ok, _ := s.VideoByTitle(ctx, acc, "Alien", 1990); ok {
		t.Fatal("year should narrow the title")
	}
	q := VideoQuery{Seeds: []int64{alien}, SeedTitles: []string{"Alien"}}
	q.Relabel()
	res, _ = s.MuseVideo(ctx, acc, idx, q, nil, 0, 10)
	if len(res.Items) != 5 || res.Items[0].ID == alien || (res.Items[0].ID != aliens && res.Items[0].ID != gravity) || q.Understood() != "Like Alien" {
		t.Fatalf("like alien: %s (%s)", ids(res.Items), q.Understood())
	}
	// Synopses for re-ranking.
	syn, err := s.Synopses(ctx, []int64{heat})
	if err != nil || syn[heat] != "Cops and robbers." {
		t.Fatalf("synopsis %q %v", syn[heat], err)
	}

	// Recommendations: Ann finished Gravity (space) and The Notebook (love), so Alien and
	// Titanic are equally close; she abandoned Aliens ten minutes in three weeks ago.
	mustExec(t, d, `INSERT INTO user_item_state(user_id, item_id, play_count, last_viewed_at) VALUES (?, ?, 1, ?)`, uid, gravity, isoUTC(time.Now().Add(-time.Hour)))
	mustExec(t, d, `INSERT INTO user_item_state(user_id, item_id, play_count, last_viewed_at) VALUES (?, ?, 1, ?)`, uid, notebook, isoUTC(time.Now().Add(-2*time.Hour)))
	mustExec(t, d, `INSERT INTO user_item_state(user_id, item_id, view_offset_ms, last_viewed_at) VALUES (?, ?, 600000, ?)`, uid, aliens, isoUTC(time.Now().AddDate(0, 0, -21)))
	before, _ := s.Recommended(ctx, acc, idx, nil, 10)
	if len(before) == 0 || before[0].ID != alien {
		t.Fatalf("without learning: %s", ids(before))
	}
	s.Learn = func() bool { return true }
	neg, err := s.disliked(ctx, uid)
	if err != nil || len(neg) != 1 || neg[0] != aliens {
		t.Fatalf("disliked %v %v", neg, err)
	}
	after, _ := s.Recommended(ctx, acc, idx, nil, 10)
	if len(after) == 0 || after[0].ID != titanic {
		t.Fatalf("learning should push the abandoned film's near-copy down: %s", ids(after))
	}
}

func TestPenaliseSimilar(t *testing.T) {
	idx := NewVideoIndex()
	idx.Replace("m", []*VideoVec{
		{ItemID: 1, Vec: normalizeVec([]float32{1, 0.05, 0})}, // a near-copy of 9
		{ItemID: 2, Vec: normalizeVec([]float32{0.5, 1, 0})},  // somewhat like 9
		{ItemID: 3, Vec: normalizeVec([]float32{0, 0, 1})},    // unlike 9
		{ItemID: 9, Vec: normalizeVec([]float32{1, 0, 0})},    // abandoned
	})
	ranked := []Scored{{1, 0.9}, {2, 0.85}, {3, 0.8}}
	got := PenaliseSimilar(ranked, idx, []int64{9})
	if got[0].ItemID != 2 || got[1].ItemID != 3 || got[2].ItemID != 1 {
		t.Fatalf("order %+v", got)
	}
	if got[1].Score != 0.8 {
		t.Fatalf("an unrelated title was penalised: %+v", got)
	}
	if same := PenaliseSimilar(ranked, idx, nil); same[0].ItemID != 1 {
		t.Fatal("no dislikes should keep the order")
	}
}

func TestGenreGroupAndRelabel(t *testing.T) {
	if g := GenreGroup("science fiction", libraryGenres); len(g) != 2 || g[0] != "Science Fiction" || g[1] != "Sci-Fi & Fantasy" {
		t.Fatalf("sci-fi group %v", g)
	}
	if g := GenreGroup("sci-fi", libraryGenres); len(g) != 2 {
		t.Fatalf("synonym group %v", g)
	}
	if g := GenreGroup("Space Opera", libraryGenres); g != nil {
		t.Fatalf("unknown genre %v", g)
	}
	q := VideoQuery{Types: []string{"movie"}, YearFrom: 1990, YearTo: 1999, Genres: [][]string{{"Horror"}}, ExcludeGenres: []string{"Comedy"},
		People: []string{"John Carpenter"}, MinRating: 7.5, Rest: "isolation"}
	q.Relabel()
	if u := q.Understood(); u != "Movies · 1990s · Horror · Not Comedy · With John Carpenter · Highly rated · like 'isolation'" {
		t.Fatalf("understood %q", u)
	}
}
