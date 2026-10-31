package vodsuggest

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"marquee/internal/db"
	"marquee/internal/items"
	"marquee/internal/vod"
	"marquee/internal/vodsave"
)

type env struct {
	t     *testing.T
	db    *sql.DB
	svc   *Service
	store *items.Store
	uid   int64
	movie int64 // the films library
	shows int64
	n     int
}

func newEnv(t *testing.T) *env {
	t.Helper()
	dir := t.TempDir()
	d, err := db.Open(context.Background(), filepath.Join(dir, "t.db"), filepath.Join(dir, "b"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	e := &env{t: t, db: d, store: items.NewStore(d)}
	saves := &vodsave.Service{DB: d}
	e.svc = &Service{DB: d, Items: e.store, InLibrary: func(ctx context.Context) (*vodsave.LibraryIndex, error) {
		saves.ForgetLibraryIndex()
		return saves.InLibrary(ctx)
	}, CheckEvery: time.Nanosecond}
	e.uid = e.exec(`INSERT INTO users(username, display_name) VALUES ('ann', 'Ann')`)
	e.movie = e.exec(`INSERT INTO libraries(name, type) VALUES ('Movies', 'movies')`)
	e.shows = e.exec(`INSERT INTO libraries(name, type) VALUES ('Shows', 'shows')`)
	return e
}

func (e *env) exec(q string, args ...any) int64 {
	e.t.Helper()
	r, err := e.db.Exec(q, args...)
	if err != nil {
		e.t.Fatalf("%s: %v", q, err)
	}
	id, _ := r.LastInsertId()
	return id
}

// item adds a library movie or show with its TMDB id, genres and TMDB recommendations.
func (e *env) item(typ, title string, tmdb int, genres []string, recs ...int) int64 {
	lib := e.movie
	if typ == "show" {
		lib = e.shows
	}
	id := e.exec(`INSERT INTO items(library_id, type, title, sort_title, year) VALUES (?, ?, ?, ?, 2000)`, lib, typ, title, title)
	e.exec(`INSERT INTO external_ids(item_id, provider, value) VALUES (?, 'tmdb', ?)`, id, tmdb)
	for _, g := range genres {
		e.exec(`INSERT OR IGNORE INTO tags(kind, name) VALUES ('genre', ?)`, g)
		e.exec(`INSERT INTO item_tags(item_id, tag_id) SELECT ?, id FROM tags WHERE kind='genre' AND name=?`, id, g)
	}
	for i, r := range recs {
		e.exec(`INSERT INTO tmdb_recommendations(item_id, tmdb_id, rank) VALUES (?, ?, ?)`, id, r, i+1)
	}
	return id
}

// withFile gives a library movie an available file (so it's "in the library").
func (e *env) withFile(item int64) {
	v := e.exec(`INSERT INTO media_versions(item_id) VALUES (?)`, item)
	e.exec(`INSERT INTO media_files(version_id, library_id, path, size, mtime) VALUES (?, ?, ?, 1, 1)`, v, e.movie, "/m/"+itoa(item)+".mkv")
}

// vodMovie adds a matched film on demand.
func (e *env) vodMovie(title string, tmdb int, genres, rating string) int64 {
	if genres == "" {
		genres = "[]"
	}
	e.n++
	return e.exec(`INSERT INTO vod_movies(source_id, source_key, title, stream_url, added_at, tmdb_id, match_state, genres, content_rating, rating, year)
		VALUES ('s', ?, ?, 'x', '2026-01-01T00:00:00Z', ?, 'matched', ?, ?, 7, 2001)`, itoa(int64(e.n)), title, tmdb, genres, rating)
}

func (e *env) user() User {
	return User{ID: e.uid, Access: items.Access{UserID: e.uid}}
}

func titles(t *testing.T, e *env, list []Suggestion) []string {
	t.Helper()
	var out []string
	for _, s := range list {
		var name string
		table := "vod_movies"
		if s.Kind == Series {
			table = "vod_series"
		}
		e.db.QueryRow("SELECT title FROM "+table+" WHERE id=?", s.ID).Scan(&name)
		out = append(out, name)
	}
	return out
}

func indexOf(list []string, s string) int {
	for i, x := range list {
		if x == s {
			return i
		}
	}
	return -1
}

// Suggestions come from TMDB's recommendations of what the person loves (a favourite above
// a 10/10 rating) and their genres; titles the library has, ones they watched or hid, ones
// rated above their limit aren't suggested; one franchise doesn't fill the list; a new
// favourite changes the kept suggestions; series come from shows.
func TestSuggestions(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	now := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
	heat := e.item("movie", "Heat", 949, []string{"Crime"}, 100, 101, 102, 600, 601, 602, 300)
	arrival := e.item("movie", "Arrival", 329865, []string{"Science Fiction"}, 200, 201, 202, 203)
	owned := e.item("movie", "Owned", 300, nil)
	e.withFile(owned)
	insomnia := e.item("movie", "Insomnia", 320, nil, 800)
	dark := e.item("show", "Dark", 70523, []string{"Mystery"}, 700)
	if err := e.store.SetFavorite(ctx, e.uid, heat, true); err != nil {
		t.Fatal(err)
	}
	e.store.SetFavorite(ctx, e.uid, dark, true)
	e.exec(`INSERT INTO user_item_state(user_id, item_id, play_count, last_viewed_at, rating) VALUES (?, ?, 1, ?, 10)`, e.uid, arrival, now)

	e.vodMovie("Ronin", 100, `["Action","Crime"]`, "R")
	e.vodMovie("Contact", 200, `["Science Fiction"]`, "PG")
	e.vodMovie("Owned", 300, `["Crime"]`, "")
	watched := e.vodMovie("Seen It", 101, `["Crime"]`, "")
	e.exec(`INSERT INTO vod_progress(user_id, movie_id, watched, updated_at) VALUES (?, ?, 1, ?)`, e.uid, watched, now)
	hidden := e.vodMovie("Not For Me", 102, `["Crime"]`, "")
	e.vodMovie("Gattaca", 201, `["Science Fiction"]`, "R")
	e.vodMovie("Crime Only", 500, `["Crime"]`, "")
	e.vodMovie("Nothing In Common", 501, `["Romance"]`, "")
	e.vodMovie("Saga", 600, `["Crime"]`, "")
	e.vodMovie("Saga 2", 601, `["Crime"]`, "")
	e.vodMovie("Saga III", 602, `["Crime"]`, "")
	e.vodMovie("Memento", 800, "", "")
	// A second copy of Contact (another source) is suggested once.
	e.vodMovie("Contact", 200, `["Science Fiction"]`, "PG")
	e.exec(`INSERT INTO vod_series(source_id, source_key, title, added_at, tmdb_id, match_state, genres) VALUES ('s', 'x1', '1899', '2026-01-01T00:00:00Z', 700, 'matched', '["Mystery"]')`)

	if ok, err := e.svc.Hide(ctx, e.uid, Movie, hidden); !ok || err != nil {
		t.Fatalf("hide: %v %v", ok, err)
	}
	list, err := e.svc.For(ctx, e.user(), "", 50)
	if err != nil {
		t.Fatal(err)
	}
	got := titles(t, e, list)
	for _, gone := range []string{"Owned", "Seen It", "Not For Me", "Nothing In Common", "Memento"} {
		if indexOf(got, gone) >= 0 {
			t.Errorf("%s suggested: %v", gone, got)
		}
	}
	if n := strings.Count(strings.Join(got, "|"), "Contact"); n != 1 {
		t.Errorf("Contact %d times: %v", n, got)
	}
	ronin, contact := indexOf(got, "Ronin"), indexOf(got, "Contact")
	if ronin != 0 || contact < 0 || contact < ronin {
		t.Fatalf("a favourite's recommendation should come before a 10/10's: %v", got)
	}
	if list[0].Reason != "Because you love Heat" {
		t.Errorf("reason: %q", list[0].Reason)
	}
	if r := list[contact].Reason; r != "Because you loved Arrival" {
		t.Errorf("Contact's reason: %q", r)
	}
	if i := indexOf(got, "Crime Only"); i < 0 || list[i].Reason != "Because you like Crime" {
		t.Errorf("genre suggestion: %v", got)
	}
	// Diversity: one Saga among the first four, though all three outscore Contact.
	if n := strings.Count(strings.Join(got[:4], "|"), "Saga"); n != 1 {
		t.Errorf("sagas in the top four: %v", got[:4])
	}
	if i := indexOf(got, "1899"); i < 0 || list[i].Kind != Series || list[i].Reason != "Because you love Dark" {
		t.Errorf("series: %v", got)
	}
	only, _ := e.svc.For(ctx, e.user(), Series, 10)
	if len(only) != 1 || only[0].Kind != Series {
		t.Errorf("series only: %+v", only)
	}

	// A profile limited to PG-13 doesn't get R-rated (or unrated) films.
	kid := e.user()
	kid.MaxRating = "PG-13"
	list, _ = e.svc.For(ctx, kid, Movie, 50)
	if got := titles(t, e, list); len(got) != 1 || got[0] != "Contact" {
		t.Errorf("PG-13: %v", got)
	}

	// A new favourite: the kept suggestions are worked out again (in the background).
	if err := e.store.SetFavorite(ctx, e.uid, insomnia, true); err != nil {
		t.Fatal(err)
	}
	e.svc.For(ctx, e.user(), "", 50) // notices, rebuilds in the background
	e.svc.Wait()
	list, _ = e.svc.For(ctx, e.user(), "", 50)
	got = titles(t, e, list)
	if i := indexOf(got, "Memento"); i < 0 || list[i].Reason != "Because you love Insomnia" {
		t.Errorf("after a new favourite: %v", got)
	}

	// Watching a suggestion on demand takes it out at once.
	var ronID int64
	e.db.QueryRow(`SELECT id FROM vod_movies WHERE title='Ronin'`).Scan(&ronID)
	e.exec(`INSERT INTO vod_progress(user_id, movie_id, position_ms, updated_at) VALUES (?, ?, 600000, ?)`, e.uid, ronID, now)
	list, _ = e.svc.For(ctx, e.user(), "", 50)
	if indexOf(titles(t, e, list), "Ronin") >= 0 {
		t.Error("a started film is still suggested")
	}
}

// Also on VOD: titles TMDB recommends for the item, else ones sharing its genres, of its kind.
func TestSimilar(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	alien := e.item("movie", "Alien", 348, []string{"Horror", "Science Fiction"}, 679, 1091)
	e.vodMovie("Aliens", 679, `["Action","Science Fiction"]`, "")
	e.vodMovie("The Thing", 1091, `["Horror"]`, "")
	e.vodMovie("Event Horizon", 8413, `["Horror","Science Fiction"]`, "")
	e.vodMovie("Notting Hill", 509, `["Romance","Comedy"]`, "")
	it, ok, err := e.store.TasteOf(ctx, items.Access{UserID: e.uid}, alien)
	if err != nil || !ok {
		t.Fatalf("taste of: %v %v", ok, err)
	}
	list, err := e.svc.Similar(ctx, e.user(), it, 10)
	if err != nil {
		t.Fatal(err)
	}
	got := titles(t, e, list)
	if len(got) != 3 || got[2] != "Event Horizon" || indexOf(got, "Notting Hill") >= 0 {
		t.Fatalf("similar: %v", got)
	}
	if list[0].Reason != "Similar to Alien" {
		t.Errorf("reason: %q", list[0].Reason)
	}
}

// Muse: on-demand titles pass the prompt's filters and rank by its "like" titles and words.
func TestMuse(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	heat := e.item("movie", "Heat", 949, []string{"Crime"}, 100)
	e.vodMovie("Ronin", 100, `["Action","Crime"]`, "")
	e.vodMovie("Time Travel Caper", 900, `["Comedy"]`, "")
	e.vodMovie("Space Romance", 901, `["Romance"]`, "")
	seed, _, _ := e.store.TasteOf(ctx, items.Access{UserID: e.uid}, heat)
	list, err := e.svc.Muse(ctx, e.user(), MuseQuery{Seeds: []items.TasteTitle{seed}}, 10)
	if err != nil {
		t.Fatal(err)
	}
	if got := titles(t, e, list); len(got) != 1 || got[0] != "Ronin" || list[0].Reason != "Like Heat" {
		t.Errorf("like Heat: %v %+v", got, list)
	}
	list, _ = e.svc.Muse(ctx, e.user(), MuseQuery{Words: "time travel"}, 10)
	if got := titles(t, e, list); len(got) != 1 || got[0] != "Time Travel Caper" {
		t.Errorf("time travel: %v", got)
	}
	list, _ = e.svc.Muse(ctx, e.user(), MuseQuery{Genres: [][]string{{"Romance"}}}, 10)
	if got := titles(t, e, list); len(got) != 1 || got[0] != "Space Romance" {
		t.Errorf("romance: %v", got)
	}
}

func TestFranchiseKey(t *testing.T) {
	for in, want := range map[string]string{"Rocky II": "rocky", "Rocky (1976)": "rocky", "The Matrix Reloaded": "matrix reloaded",
		"Mission: Impossible - Fallout": "mission", "Toy Story 3": "toy story", "Saga III": "saga"} {
		if got := franchiseKey(in); got != want {
			t.Errorf("%q: %q, want %q", in, got, want)
		}
	}
}

// A favourite's recommendation outranks a 10/10 rating's, all else equal.
func TestFavouriteOutranksRating(t *testing.T) {
	fav := seed{name: "Fav", kind: Movie, weight: 2, favorite: true, genres: []string{"drama"}, recs: []int{1}}
	rated := seed{name: "Rated", kind: Movie, weight: 10.0 / 7, rating: 10, genres: []string{"drama"}, recs: []int{2}}
	p := buildProfile([]seed{rated, fav}, nil)
	a, wa, _ := p.score(&title{kind: Movie, tmdb: 1, genres: []string{"drama"}})
	b, wb, _ := p.score(&title{kind: Movie, tmdb: 2, genres: []string{"drama"}})
	if a <= b || wa != whyFavorite || wb != whyLoved {
		t.Errorf("favourite %v (%v), rated %v (%v)", a, wa, b, wb)
	}
}

// Films watched on demand count too, through their TMDB recommendations, and keep counting
// once they've left their source.
func TestWatchedOnDemand(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	v := &vod.Service{DB: e.db}
	seen := e.vodMovie("Seen", 10, `["Western"]`, "")
	e.exec(`UPDATE vod_movies SET tmdb_recs='[11]' WHERE id=?`, seen)
	if err := v.SetWatched(ctx, e.uid, seen, true); err != nil {
		t.Fatal(err)
	}
	e.vodMovie("Next", 11, `["Drama"]`, "")
	list, err := e.svc.For(ctx, e.user(), "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if got := titles(t, e, list); len(got) != 1 || got[0] != "Next" || list[0].Reason != "Because you watched Seen" {
		t.Errorf("from a film watched on demand: %v %+v", got, list)
	}
	e.exec(`DELETE FROM vod_movies WHERE id=?`, seen) // gone from the source, and its watched mark with it
	e.svc.Forget(e.uid)
	if list, err = e.svc.For(ctx, e.user(), "", 10); err != nil {
		t.Fatal(err)
	}
	if got := titles(t, e, list); len(got) != 1 || got[0] != "Next" || list[0].Reason != "Because you watched Seen" {
		t.Errorf("after it left the source: %v %+v", got, list)
	}
	// Back under another id: still watched, so not suggested.
	e.vodMovie("Seen", 10, `["Western"]`, "")
	e.svc.Forget(e.uid)
	if list, err = e.svc.For(ctx, e.user(), "", 10); err != nil {
		t.Fatal(err)
	}
	if got := titles(t, e, list); len(got) != 1 || got[0] != "Next" {
		t.Errorf("after it came back: %v", got)
	}
}

// A rating on demand is kept with the title: a high one is a strong like, a low one a dislike
// whose recommendations are steered away from; it's there again when the title comes back.
func TestRatedOnDemand(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	v := &vod.Service{DB: e.db}
	loved := e.vodMovie("Loved", 20, `["Drama"]`, "")
	hated := e.vodMovie("Hated", 30, `["Drama"]`, "")
	e.exec(`UPDATE vod_movies SET tmdb_recs='[21]' WHERE id=?`, loved)
	e.exec(`UPDATE vod_movies SET tmdb_recs='[31]' WHERE id=?`, hated)
	e.vodMovie("Liked's pick", 21, `["Drama"]`, "")
	e.vodMovie("Disliked's pick", 31, `["Drama"]`, "")
	nine, two := 9.0, 2.0
	if err := v.SetRating(ctx, e.uid, "movie", loved, &nine); err != nil {
		t.Fatal(err)
	}
	if err := v.SetRating(ctx, e.uid, "movie", hated, &two); err != nil {
		t.Fatal(err)
	}
	if err := v.SetRating(ctx, e.uid, "movie", loved, ptrTo(11.0)); err == nil {
		t.Error("a rating of 11 was taken")
	}
	list, err := e.svc.For(ctx, e.user(), "", 10)
	if err != nil {
		t.Fatal(err)
	}
	got := titles(t, e, list)
	if len(got) == 0 || got[0] != "Liked's pick" || list[0].Reason != "Because you loved Loved" {
		t.Errorf("from ratings: %v %+v", got, list)
	}
	for i, g := range got {
		if g == "Disliked's pick" && i == 0 {
			t.Errorf("a disliked film's recommendation came first: %v", got)
		}
	}
	// The rating outlives the film's row and comes back with it.
	e.exec(`DELETE FROM vod_movies WHERE id=?`, loved)
	again := e.vodMovie("Loved", 20, `["Drama"]`, "")
	if r, err := v.Rating(ctx, e.uid, "movie", again); err != nil || r != 9 {
		t.Errorf("the rating after it came back: %v %v", r, err)
	}
	// Cleared.
	if err := v.SetRating(ctx, e.uid, "movie", again, nil); err != nil {
		t.Fatal(err)
	}
	if r, _ := v.Rating(ctx, e.uid, "movie", again); r != 0 {
		t.Errorf("cleared, but %v", r)
	}
}

func ptrTo[T any](v T) *T { return &v }

// Someone with no taste yet gets well-rated, popular titles they may see ("Popular on
// VOD"), spread over genres; library-owned, watched, hidden and poorly rated titles stay out.
func TestPopularForNewUsers(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	add := func(title string, tmdb int, genres string, rating float64, votes int) int64 {
		id := e.vodMovie(title, tmdb, genres, "")
		e.exec(`UPDATE vod_movies SET rating=?, votes=? WHERE id=?`, rating, votes, id)
		return id
	}
	add("Blockbuster", 1, `["Action"]`, 8.0, 20000)
	add("Cult Gem", 2, `["Drama"]`, 8.6, 40)
	add("Action Two", 3, `["Action"]`, 7.9, 18000)
	add("Action Three", 4, `["Action"]`, 7.8, 17000)
	add("Comedy Hit", 5, `["Comedy"]`, 7.4, 9000)
	add("Bad", 6, `["Action"]`, 4.0, 50000)
	// Started (a film watched to the end would already be taste).
	seen := add("Seen", 7, `["Drama"]`, 8.5, 30000)
	e.exec(`INSERT INTO vod_progress(user_id, movie_id, position_ms, updated_at) VALUES (?, ?, 600000, '2026-01-01T00:00:00Z')`, e.uid, seen)
	hidden := add("Hidden", 8, `["Drama"]`, 8.4, 30000)
	e.svc.Hide(ctx, e.uid, Movie, hidden)
	owned := e.item("movie", "Owned", 9, nil)
	e.withFile(owned)
	add("Owned", 9, `["Drama"]`, 9.0, 90000)

	list, err := e.svc.For(ctx, e.user(), "", 10)
	if err != nil {
		t.Fatal(err)
	}
	got := titles(t, e, list)
	if len(got) != 5 || got[0] != "Blockbuster" {
		t.Fatalf("popular: %v", got)
	}
	for _, s := range list {
		if s.Reason != "Popular on VOD" {
			t.Errorf("reason %q", s.Reason)
		}
	}
	for _, gone := range []string{"Bad", "Seen", "Hidden", "Owned"} {
		if indexOf(got, gone) >= 0 {
			t.Errorf("%s offered: %v", gone, got)
		}
	}
	// Votes count: the blockbuster leads the less-voted gem; genres are spread (Comedy Hit
	// comes before the third action film).
	if indexOf(got, "Cult Gem") < indexOf(got, "Blockbuster") || indexOf(got, "Comedy Hit") > indexOf(got, "Action Three") {
		t.Errorf("order: %v", got)
	}
	// Once they have taste, it's their suggestions instead.
	heat := e.item("movie", "Heat", 949, []string{"Crime"}, 5)
	e.store.SetFavorite(ctx, e.uid, heat, true)
	e.svc.For(ctx, e.user(), "", 10)
	e.svc.Wait()
	list, _ = e.svc.For(ctx, e.user(), "", 10)
	if len(list) == 0 || list[0].Reason != "Because you love Heat" {
		t.Errorf("with taste: %+v", list)
	}
}
