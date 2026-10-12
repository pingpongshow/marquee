package items

import (
	"context"
	"strings"
	"testing"

	"marquee/internal/origins"
)

// "Movies like Arrival": TMDB's recommendations for the seed come first when they pass the
// filters, ahead of a closer-by-vector decoy that only shares a lead actor; with nothing
// else in the prompt the seed alone steers.
func TestLikeSeedUsesTMDBRecommendations(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	s := NewStore(d)
	lib := mustExec(t, d, `INSERT INTO libraries(name, type) VALUES ('Movies', 'movies')`)
	add := func(title string, tmdbID int, score float64) int64 {
		id := mustExec(t, d, `INSERT INTO items(library_id, type, title, sort_title, year, audience_rating, match_state) VALUES (?, 'movie', ?, ?, 2016, ?, 'matched')`,
			lib, title, title, score)
		mustExec(t, d, `INSERT INTO external_ids(item_id, provider, value) VALUES (?, 'tmdb', ?)`, id, tmdbID)
		return id
	}
	arrival := add("Arrival", 329865, 7.6)
	contact := add("Contact", 686, 7.4)
	annihilation := add("Annihilation", 300668, 6.9)
	decoy := add("Shared Star Drama", 1111, 8.0) // same lead actor, nothing else in common
	other := add("Unrelated Comedy", 2222, 8.5)
	// A show with the same TMDB id as a recommended film must not be picked up.
	showLib := mustExec(t, d, `INSERT INTO libraries(name, type) VALUES ('Shows', 'shows')`)
	sameID := mustExec(t, d, `INSERT INTO items(library_id, type, title, sort_title) VALUES (?, 'show', 'Show 686', 's')`, showLib)
	mustExec(t, d, `INSERT INTO external_ids(item_id, provider, value) VALUES (?, 'tmdb', '686')`, sameID)
	mustExec(t, d, `INSERT INTO tmdb_recommendations(item_id, tmdb_id, rank) VALUES (?, 300668, 1), (?, 686, 2), (?, 99999, 3)`, arrival, arrival, arrival)

	// The decoy's description is the closest to Arrival's (shared cast); the recommended
	// films are further away.
	idx := NewVideoIndex()
	vec := func(id int64, v ...float32) *VideoVec {
		return &VideoVec{ItemID: id, LibraryID: lib, Type: "movie", Vec: normalizeVec(v)}
	}
	idx.Replace("m", []*VideoVec{vec(arrival, 1, 0, 0), vec(decoy, 0.9, 0.44, 0), vec(contact, 0.6, 0.8, 0),
		vec(annihilation, 0.5, 0, 0.87), vec(other, 0, 1, 0)})

	recs, err := s.TMDBRecommended(ctx, []int64{arrival}, likeRecWeight)
	if err != nil || len(recs) != 2 || recs[annihilation] <= recs[contact] || recs[sameID] != 0 {
		t.Fatalf("recommendations %v %v", recs, err)
	}
	acc := Access{}
	q := VideoQuery{Seeds: []int64{arrival}, SeedTitles: []string{"Arrival"}}
	res, err := s.MuseVideo(ctx, acc, idx, q, nil, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(res.Items); !strings.HasPrefix(got, "Contact,Annihilation,Shared Star Drama") && !strings.HasPrefix(got, "Annihilation,Contact,Shared Star Drama") {
		t.Fatalf("like Arrival: %s", got)
	}
	// Without recommendations the decoy would lead: the seed's vector alone is used.
	mustExec(t, d, `DELETE FROM tmdb_recommendations`)
	res, _ = s.MuseVideo(ctx, acc, idx, q, nil, 0, 10)
	if got := ids(res.Items); !strings.HasPrefix(got, "Shared Star Drama") {
		t.Fatalf("seed alone: %s", got)
	}
	// With meaning text the seeds still count for three quarters: a prompt vector pointing
	// at the comedy doesn't pull it above the seed's neighbours.
	res, _ = s.MuseVideo(ctx, acc, idx, VideoQuery{Seeds: []int64{arrival}, Rest: "funny"}, normalizeVec([]float32{0, 1, 0}), 0, 10)
	if got := ids(res.Items); strings.HasPrefix(got, "Unrelated Comedy") {
		t.Fatalf("seed weight: %s", got)
	}
	// "More like this" also prefers TMDB's recommendations.
	mustExec(t, d, `INSERT INTO tmdb_recommendations(item_id, tmdb_id, rank) VALUES (?, 686, 1)`, arrival)
	rel, ok, err := s.RelatedByEmbedding(ctx, acc, idx, arrival, 3)
	if err != nil || !ok || len(rel) == 0 || rel[0].ID != contact {
		t.Fatalf("related: %s %v", ids(rel), err)
	}
}

// Library details as filters and boosts: origin (country or language), not English,
// content rating limits, critics' scores and keywords.
func TestMuseVideoDetails(t *testing.T) {
	d := testDB(t)
	ctx := context.Background()
	s := NewStore(d)
	lib := mustExec(t, d, `INSERT INTO libraries(name, type) VALUES ('Movies', 'movies')`)
	add := func(title, lang, rating string, rt any, score float64) int64 {
		return mustExec(t, d, `INSERT INTO items(library_id, type, title, sort_title, original_language, content_rating, rt_critic, audience_rating)
			VALUES (?, 'movie', ?, ?, ?, ?, ?, ?)`, lib, title, title, lang, rating, rt, score)
	}
	parasite := add("Parasite", "ko", "R", 99, 8.5)
	oldboy := add("Oldboy", "ko", "R", 82, 8.4)
	brit := add("Hot Fuzz", "en", "R", 91, 7.8)
	heist := add("Ocean's Eleven", "en", "PG-13", 83, 7.7)
	kids := add("Paddington", "en", "PG", 99, 7.6)
	kr := mustExec(t, d, `INSERT INTO tags(kind, name) VALUES ('country', 'KR')`)
	gb := mustExec(t, d, `INSERT INTO tags(kind, name) VALUES ('country', 'GB')`)
	kw := mustExec(t, d, `INSERT INTO tags(kind, name) VALUES ('keyword', 'heist')`)
	mustExec(t, d, `INSERT INTO item_tags(item_id, tag_id) VALUES (?, ?), (?, ?), (?, ?), (?, ?)`, parasite, kr, brit, gb, kids, gb, heist, kw)
	_ = oldboy
	run := func(q VideoQuery) string {
		res, err := s.MuseVideo(ctx, Access{}, NewVideoIndex(), q, nil, 0, 10)
		if err != nil {
			t.Fatal(err)
		}
		return ids(res.Items)
	}
	// Korean: the KR country tag, or Korean as the original language (Oldboy has no tags yet).
	if got := run(VideoQuery{Origins: []origins.Origin{{Country: "KR", Language: "ko"}}}); got != "Parasite,Oldboy" {
		t.Errorf("korean: %s", got)
	}
	if got := run(VideoQuery{Origins: []origins.Origin{{Country: "GB"}}}); got != "Hot Fuzz,Paddington" {
		t.Errorf("british: %s", got)
	}
	if got := run(VideoQuery{Foreign: true}); got != "Parasite,Oldboy" {
		t.Errorf("foreign: %s", got)
	}
	if got := run(VideoQuery{MaxRating: "PG-13"}); got != "Ocean's Eleven,Paddington" {
		t.Errorf("PG-13 or under: %s", got)
	}
	if got := run(VideoQuery{MinCritic: 90}); got != "Parasite,Hot Fuzz,Paddington" {
		t.Errorf("critics 90+: %s", got)
	}
	// Keywords boost rather than filter.
	if got := run(VideoQuery{Keywords: []string{"heist"}}); !strings.HasPrefix(got, "Ocean's Eleven,") || strings.Count(got, ",") != 4 {
		t.Errorf("heist: %s", got)
	}
}

func TestParseVideoPromptDetails(t *testing.T) {
	vocab := VideoVocab{Genres: []string{"Thriller", "Comedy", "Drama"}, Keywords: map[string]bool{"time loop": true, "heist": true, "based on a true story": true, "love": true}}
	cases := []struct {
		prompt, understood string
		check              func(VideoQuery) bool
	}{
		{"Korean thrillers", "Thriller · From South Korea", func(q VideoQuery) bool {
			return len(q.Origins) == 1 && q.Origins[0] == origins.Origin{Country: "KR", Language: "ko"}
		}},
		{"British comedies", "Comedy · From United Kingdom", func(q VideoQuery) bool { return q.Origins[0].Language == "" }},
		{"foreign films", "Movies · Not in English", func(q VideoQuery) bool { return q.Foreign }},
		{"a time loop movie", "Movies · Themes: time loop · like 'time loop'", func(q VideoQuery) bool { return q.Rest == "time loop" }},
		{"heists", "Themes: heist · like 'heists'", nil},
		{"something PG or under", "PG or under", func(q VideoQuery) bool { return q.Rest == "" }},
		{"dramas rated pg-13 and below", "Drama · PG-13 or under", nil},
		{"critically acclaimed dramas", "Drama · Critics 85%+", nil},
		{"movies with rotten tomatoes over 90", "Movies · Critics 91%+", nil},
		{"comedies rt at least 80%", "Comedy · Critics 80%+", nil},
		{"best thrillers", "Thriller · Highly rated", nil},
	}
	for _, c := range cases {
		q := ParseVideoPromptWith(c.prompt, vocab)
		if q.Understood() != c.understood || c.check != nil && !c.check(q) {
			t.Errorf("%q: %q %+v", c.prompt, q.Understood(), q)
		}
	}
	if got := Meaningful("films similar to"); got != "" {
		t.Errorf("filler: %q", got)
	}
	if got := Meaningful("but funnier"); got != "but funnier" {
		t.Errorf("meaning: %q", got)
	}
}

// The description leads with what a title is about and keeps cast and studio short.
func TestDocText(t *testing.T) {
	m := docMeta{typ: "movie", title: "Arrival", year: 2016, genres: []string{"Drama", "Science Fiction"},
		keywords: []string{"alien contact", "linguistics"}, tagline: "Why are they here?", summary: "A linguist works with the military.",
		directors: []string{"Denis Villeneuve"}, cast: []string{"Amy Adams", "Jeremy Renner", "Forest Whitaker"}, studio: "Paramount",
		countries: []string{"US"}, language: "en", contentRating: "PG-13", rtCritic: 94, metacritic: 81, audience: 7.6}
	want := "Arrival (2016). A film. Drama, Science Fiction. Themes: alien contact, linguistics. Why are they here? A linguist works with the military. " +
		"Directed by Denis Villeneuve. Starring Amy Adams, Jeremy Renner, Forest Whitaker. From the United States. Rated PG-13. Critics 94%, audiences 7.6/10."
	if got := DocText(m); got != want {
		t.Errorf("doc:\n%s\nwant\n%s", got, want)
	}
	show := docMeta{typ: "show", title: "Kingdom", studio: "Netflix", countries: []string{"KR"}, language: "ko", rtCritic: -1, metacritic: -1}
	if got := DocText(show); got != "Kingdom. A TV series. From South Korea, in Korean. On Netflix." {
		t.Errorf("show doc: %s", got)
	}
}
