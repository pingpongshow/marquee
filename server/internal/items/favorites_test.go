package items

import (
	"context"
	"strings"
	"testing"
	"time"
)

// A favourite is the strongest liking signal: it outweighs a 5-star rating in "Recommended
// for You", seeds "Because you watched" first, leads the titles that explain
// recommendations, is never counted as disliked, and steers Muse's "like my favourites".
func TestFavoritesInRecommendations(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	s := NewStore(d)
	s.Learn = func() bool { return true }
	uid := mustExec(t, d, `INSERT INTO users(username, display_name) VALUES ('ann', 'Ann')`)
	lib := mustExec(t, d, `INSERT INTO libraries(name, type) VALUES ('Movies', 'movies')`)
	add := func(title string) int64 {
		return mustExec(t, d, `INSERT INTO items(library_id, type, title, sort_title, duration_ms) VALUES (?, 'movie', ?, ?, 6000000)`, lib, title, title)
	}
	alien := add("Alien")         // space; a favourite with no rating
	notebook := add("Notebook")   // love; rated 5 stars
	gravity := add("Gravity")     // mostly space
	titanic := add("Titanic")     // mostly love
	abandoned := add("Abandoned") // a favourite they gave up on early and rated 1 star
	idx := NewVideoIndex()
	vec := func(id int64, v ...float32) *VideoVec {
		return &VideoVec{ItemID: id, LibraryID: lib, Type: "movie", Vec: normalizeVec(v)}
	}
	idx.Replace("m", []*VideoVec{vec(alien, 1, 0, 0), vec(notebook, 0, 1, 0), vec(gravity, 0.8, 0.6, 0),
		vec(titanic, 0.6, 0.8, 0), vec(abandoned, 0, 0, 1)})
	now := isoUTC(time.Now())
	mustExec(t, d, `INSERT INTO user_item_state(user_id, item_id, play_count, last_viewed_at, rating) VALUES (?, ?, 1, ?, 10)`, uid, notebook, now)
	mustExec(t, d, `INSERT INTO user_item_state(user_id, item_id, play_count, last_viewed_at) VALUES (?, ?, 1, ?)`, uid, alien, isoUTC(time.Now().Add(-time.Hour)))
	acc := Access{UserID: uid}

	// Without favourites the 5-star love story leads the taste.
	recs, err := s.Recommended(ctx, acc, idx, nil, 2)
	if err != nil || ids(recs) != "Titanic,Gravity" {
		t.Fatalf("before favourites: %s %v", ids(recs), err)
	}
	if err := s.SetFavorite(ctx, uid, alien, true); err != nil {
		t.Fatal(err)
	}
	recs, err = s.Recommended(ctx, acc, idx, nil, 2)
	if err != nil || ids(recs) != "Gravity,Titanic" {
		t.Fatalf("a favourite should outweigh a 5-star rating: %s %v", ids(recs), err)
	}

	// Because you watched: the favourite is the first seed, although the rated film is newer.
	rows, err := s.BecauseYouWatched(ctx, acc, idx, nil, 1, 3)
	if err != nil || len(rows) != 1 || rows[0].Seed.ID != alien || !rows[0].Favorite {
		t.Fatalf("because you watched: %+v %v", rows, err)
	}
	if !strings.HasPrefix(ids(rows[0].Items), "Gravity") {
		t.Errorf("because you love Alien: %s", ids(rows[0].Items))
	}
	liked, err := s.LikedTitles(ctx, acc, 5)
	if err != nil || len(liked) < 2 || liked[0] != "Alien" {
		t.Errorf("liked titles: %v %v", liked, err)
	}

	// Abandoned early and rated 1 star counts as disliked, unless it's a favourite.
	mustExec(t, d, `INSERT INTO user_item_state(user_id, item_id, view_offset_ms, last_viewed_at, rating) VALUES (?, ?, 60000, ?, 2)`,
		uid, abandoned, isoUTC(time.Now().AddDate(0, 0, -20)))
	if neg, _ := s.disliked(ctx, uid); len(neg) != 1 || neg[0] != abandoned {
		t.Fatalf("disliked: %v", neg)
	}
	s.SetFavorite(ctx, uid, abandoned, true)
	if neg, _ := s.disliked(ctx, uid); len(neg) != 0 {
		t.Errorf("a favourite counted as disliked: %v", neg)
	}
	s.SetFavorite(ctx, uid, abandoned, false)

	// Muse: "like my favourites" reads as such; the favourites filter keeps only favourites.
	q := ParseVideoPrompt("movies like my favourites", nil)
	if !q.LikeFavourites || q.Favourites || !strings.Contains(q.Understood(), "Like your favourites") {
		t.Errorf("like my favourites: %+v %s", q, q.Understood())
	}
	q = ParseVideoPrompt("my favourite movies", nil)
	if !q.Favourites || q.LikeFavourites {
		t.Errorf("my favourite movies: %+v", q)
	}
	res, err := s.MuseVideo(ctx, acc, idx, q, nil, 0, 10)
	if err != nil || ids(res.Items) != "Alien" {
		t.Errorf("favourites only: %s %v", ids(res.Items), err)
	}
	favs, _ := s.FavoriteVideos(ctx, uid, 10)
	res, err = s.MuseVideo(ctx, acc, idx, VideoQuery{Seeds: favs, LikeFavourites: true}, nil, 0, 2)
	if err != nil || ids(res.Items) != "Gravity,Titanic" {
		t.Errorf("like my favourites: %s %v", ids(res.Items), err)
	}
}
