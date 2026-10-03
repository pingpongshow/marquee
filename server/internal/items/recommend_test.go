package items

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"
)

// fakeEmbedder embeds by keyword: space, love, or neither, in three dimensions.
type fakeEmbedder struct {
	model string
	texts int
	kinds []string
}

func (f *fakeEmbedder) EmbedDocs(_ context.Context, texts []string, kind string) (string, [][]float32, error) {
	f.kinds = append(f.kinds, kind)
	out := make([][]float32, len(texts))
	for i, t := range texts {
		f.texts++
		t = strings.ToLower(t)
		v := []float32{0.05, 0.05, 0.05}
		if strings.Contains(t, "space") {
			v[0] += 1
		}
		if strings.Contains(t, "love") {
			v[1] += 1
		}
		if strings.Contains(t, "ship") {
			v[0] += 0.3 // a little like space
		}
		if !strings.Contains(t, "space") && !strings.Contains(t, "love") {
			v[2] += 1
		}
		out[i] = v
	}
	return f.model, out, nil
}

func ids(list []Summary) string {
	var out []string
	for _, s := range list {
		out = append(out, s.Title)
	}
	return strings.Join(out, ",")
}

func TestEmbeddingsAndRecommendations(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	s := NewStore(d)
	uid := mustExec(t, d, `INSERT INTO users(username, display_name) VALUES ('ann', 'Ann')`)
	movies := mustExec(t, d, `INSERT INTO libraries(name, type) VALUES ('Movies', 'movies')`)
	shows := mustExec(t, d, `INSERT INTO libraries(name, type) VALUES ('Shows', 'shows')`)
	add := func(lib int64, typ, title, summary, rating string, year int, score float64) int64 {
		return mustExec(t, d, `INSERT INTO items(library_id, type, title, sort_title, summary, content_rating, year, audience_rating, duration_ms)
			VALUES (?, ?, ?, ?, ?, NULLIF(?, ''), ?, ?, 6000000)`, lib, typ, title, title, summary, rating, year, score)
	}
	alien := add(movies, "movie", "Alien", "Terror in deep space.", "R", 1979, 8.5)
	gravity := add(movies, "movie", "Gravity", "Lost in space.", "PG-13", 2013, 7.7)
	moon := add(movies, "movie", "Moon", "Alone in space on a moon base.", "R", 2009, 7.8)
	wall := add(movies, "movie", "WALL-E", "A robot's love story in space.", "G", 2008, 8.4)
	notebook := add(movies, "movie", "The Notebook", "A love story.", "PG-13", 2004, 7.8)
	heat := add(movies, "movie", "Heat", "Cops and robbers.", "R", 1995, 8.3)
	expanse := add(shows, "show", "The Expanse", "Politics in space.", "TV-14", 2015, 8.5)
	trailer := mustExec(t, d, `INSERT INTO items(library_id, type, title, sort_title, summary, parent_id, extra_type) VALUES (?, 'movie', 'Alien Trailer', 'x', 'space', ?, 'trailer')`, movies, alien)
	genre := mustExec(t, d, `INSERT INTO tags(kind, name) VALUES ('genre', 'Science Fiction')`)
	for _, id := range []int64{alien, gravity, moon, wall, expanse} {
		mustExec(t, d, `INSERT INTO item_tags(item_id, tag_id) VALUES (?, ?)`, id, genre)
	}

	// The task embeds movies and shows (not extras), and only what changed after that.
	fe := &fakeEmbedder{model: "m1"}
	e := &Embedder{DB: d, Index: NewVideoIndex(), Client: fe}
	msg, err := e.Run(ctx)
	if err != nil || e.Index.Len() != 7 || fe.texts != 7 || e.Index.Get(trailer) != nil {
		t.Fatalf("first run: %q %v len=%d texts=%d", msg, err, e.Index.Len(), fe.texts)
	}
	if msg, _ := e.Run(ctx); fe.texts != 7 || !strings.Contains(msg, "All 7") {
		t.Fatalf("second run re-embedded: %q %d", msg, fe.texts)
	}
	mustExec(t, d, `UPDATE items SET summary = 'Cops, robbers and a ship in space.' WHERE id = ?`, heat)
	if _, err := e.Run(ctx); err != nil || fe.texts != 8 {
		t.Fatalf("changed summary: %d %v", fe.texts, err)
	}
	mustExec(t, d, `UPDATE items SET summary = 'Cops and robbers.' WHERE id = ?`, heat)
	fe.model = "m2"
	if _, err := e.Run(ctx); err != nil || fe.texts != 15 || e.Index.Model() != "m2" || e.Index.Len() != 7 {
		t.Fatalf("new model: %d %v %s", fe.texts, err, e.Index.Model())
	}
	// A fresh index loads the newest model's vectors.
	idx := NewVideoIndex()
	if err := idx.Load(ctx, d); err != nil || idx.Len() != 7 || idx.Model() != "m2" {
		t.Fatalf("load: %v %d", err, idx.Len())
	}

	// Related: same library and type, most similar first.
	acc := Access{UserID: uid}
	rel, ok, err := s.RelatedByEmbedding(ctx, acc, idx, alien, 3)
	if err != nil || !ok || len(rel) != 3 || rel[2].ID == expanse || (rel[0].ID != gravity && rel[0].ID != moon) {
		t.Fatalf("related: %s %v", ids(rel), err)
	}
	if _, ok, _ := s.RelatedByEmbedding(ctx, acc, idx, trailer, 3); ok {
		t.Fatal("trailer embedded")
	}

	// Nothing watched: no recommendations.
	if rows, err := s.BecauseYouWatched(ctx, acc, idx, nil, 2, 20); err != nil || len(rows) != 0 {
		t.Fatalf("no history: %+v %v", rows, err)
	}
	now := isoUTC(time.Now().Add(-time.Hour))
	mustExec(t, d, `INSERT INTO user_item_state(user_id, item_id, play_count, last_viewed_at, rating) VALUES (?, ?, 1, ?, 9)`, uid, alien, now)
	mustExec(t, d, `INSERT INTO user_item_state(user_id, item_id, play_count, last_viewed_at) VALUES (?, ?, 1, ?)`, uid, gravity, isoUTC(time.Now().Add(-48*time.Hour)))
	mustExec(t, d, `INSERT INTO user_item_state(user_id, item_id, view_offset_ms, last_viewed_at) VALUES (?, ?, 60000, ?)`, uid, moon, now) // started
	rows, err := s.BecauseYouWatched(ctx, acc, idx, nil, 2, 20)
	if err != nil || len(rows) != 2 || rows[0].Seed.ID != alien || rows[1].Seed.ID != gravity {
		t.Fatalf("because: %+v %v", rows, err)
	}
	// Unstarted only, movies before the show, space before the rest.
	if got := ids(rows[0].Items); got != "WALL-E,The Notebook,Heat,The Expanse" && got != "WALL-E,Heat,The Notebook,The Expanse" {
		t.Fatalf("because alien: %s", got)
	}
	if strings.Contains(ids(rows[1].Items), "Alien") {
		t.Fatalf("watched seed suggested: %s", ids(rows[1].Items))
	}
	// Libraries limit the suggestions; a profile limited to PG doesn't get R-rated seeds or items.
	rows, _ = s.BecauseYouWatched(ctx, acc, idx, []int64{shows}, 2, 20)
	if len(rows) != 2 || ids(rows[0].Items) != "The Expanse" {
		t.Fatalf("shows only: %+v", rows)
	}
	kid := Access{UserID: uid, MaxRating: "PG-13"}
	rows, _ = s.BecauseYouWatched(ctx, kid, idx, nil, 2, 20)
	if len(rows) != 1 || rows[0].Seed.ID != gravity || strings.Contains(ids(rows[0].Items), "Heat") {
		t.Fatalf("PG-13: %+v", rows)
	}
	// A show counts via its episodes.
	season := mustExec(t, d, `INSERT INTO items(library_id, type, title, sort_title, parent_id) VALUES (?, 'season', 'S1', 'S1', ?)`, shows, expanse)
	ep := mustExec(t, d, `INSERT INTO items(library_id, type, title, sort_title, parent_id, grandparent_id) VALUES (?, 'episode', 'E1', 'E1', ?, ?)`, shows, season, expanse)
	mustExec(t, d, `UPDATE items SET leaf_count = 1 WHERE id = ?`, expanse)
	mustExec(t, d, `INSERT INTO user_item_state(user_id, item_id, play_count, last_viewed_at) VALUES (?, ?, 1, ?)`, uid, ep, isoUTC(time.Now()))
	rows, _ = s.BecauseYouWatched(ctx, acc, idx, nil, 2, 20)
	if len(rows) != 2 || rows[0].Seed.ID != expanse || strings.Contains(ids(rows[0].Items), "Expanse") {
		t.Fatalf("show seed: %+v", rows)
	}

	// Recommended: near the (rating-weighted) centroid, nothing started.
	rec, err := s.Recommended(ctx, acc, idx, nil, 20)
	if err != nil || len(rec) != 3 || rec[0].ID != wall {
		t.Fatalf("recommended: %s %v", ids(rec), err)
	}

	// Muse for movies: constraints filter, the vector ranks.
	q := ParseVideoPrompt("sci-fi movies with love", []string{"Science Fiction"})
	qv, _, _ := (&Embedder{Client: fe}).Query(ctx, q.Rest)
	res, err := s.MuseVideo(ctx, acc, idx, q, qv, 0, 10)
	if err != nil || ids(res.Items) == "" || res.Items[0].ID != wall || strings.Contains(ids(res.Items), "Notebook") || strings.Contains(ids(res.Items), "Expanse") {
		t.Fatalf("muse: %s %v", ids(res.Items), err)
	}
	if res.Analysed != 1 || fe.kinds[len(fe.kinds)-1] != "query" {
		t.Fatalf("analysed %v kinds %v", res.Analysed, fe.kinds)
	}
	// Unwatched, by rating when nothing's left to rank by.
	res, _ = s.MuseVideo(ctx, acc, idx, ParseVideoPrompt("unwatched sci-fi", []string{"Science Fiction"}), nil, 0, 10)
	if ids(res.Items) != "WALL-E" {
		t.Fatalf("unwatched: %s", ids(res.Items))
	}
	res, _ = s.MuseVideo(ctx, acc, idx, ParseVideoPrompt("family films", nil), nil, 0, 10)
	if ids(res.Items) != "WALL-E" {
		t.Fatalf("family: %s", ids(res.Items))
	}
	res, _ = s.MuseVideo(ctx, acc, idx, ParseVideoPrompt("2000s", nil), nil, movies, 10)
	if ids(res.Items) != "WALL-E,Moon,The Notebook" {
		t.Fatalf("2000s by rating: %s", ids(res.Items))
	}
	// Half the pool not embedded.
	mustExec(t, d, `DELETE FROM item_embeddings WHERE item_id IN (?, ?, ?)`, heat, notebook, wall)
	idx.Load(ctx, d)
	res, _ = s.MuseVideo(ctx, acc, idx, ParseVideoPrompt("anything", nil), qv, movies, 10)
	if fmt.Sprintf("%.2f", res.Analysed) != "0.50" || len(res.Items) != 6 || res.Items[5].Title == "Alien" {
		t.Fatalf("partial: %v %s", res.Analysed, ids(res.Items))
	}
}
