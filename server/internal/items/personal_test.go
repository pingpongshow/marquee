package items

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"
)

func init() { jitterScale = 0 } // a fixed order in tests; TestDailyTurn turns it on

// homeFixture is a small library in two tastes (space and romance, by vector) for Ann.
type homeFixture struct {
	d   *sql.DB
	s   *Store
	idx *VideoIndex
	uid int64
	lib int64
	ids map[string]int64
	t   *testing.T
}

func newHomeFixture(t *testing.T) *homeFixture {
	t.Helper()
	d := testDB(t)
	f := &homeFixture{d: d, s: NewStore(d), idx: NewVideoIndex(), t: t, ids: map[string]int64{}}
	f.uid = mustExec(t, d, `INSERT INTO users(username, display_name) VALUES ('ann', 'Ann')`)
	f.lib = mustExec(t, d, `INSERT INTO libraries(name, type) VALUES ('Movies', 'movies')`)
	return f
}

// add puts a movie in the library with a vector (x: space, y: romance, z: a little apart)
// and an audience rating.
func (f *homeFixture) add(title string, x, y, z float32, rating float64, year int) int64 {
	id := mustExec(f.t, f.d, `INSERT INTO items(library_id, type, title, sort_title, year, audience_rating, duration_ms, added_at)
		VALUES (?, 'movie', ?, ?, ?, ?, 6000000, '2020-01-01T00:00:00.000Z')`, f.lib, title, title, year, rating)
	f.ids[title] = id
	f.idx.Put("m", &VideoVec{ItemID: id, LibraryID: f.lib, Type: "movie", Vec: normalizeVec([]float32{x, y, z})})
	return id
}

func (f *homeFixture) watched(title string, rating float64) {
	now := isoUTC(time.Now().Add(-time.Hour))
	if rating > 0 {
		mustExec(f.t, f.d, `INSERT INTO user_item_state(user_id, item_id, play_count, last_viewed_at, rating) VALUES (?, ?, 1, ?, ?)`, f.uid, f.ids[title], now, rating)
	} else {
		mustExec(f.t, f.d, `INSERT INTO user_item_state(user_id, item_id, play_count, last_viewed_at) VALUES (?, ?, 1, ?)`, f.uid, f.ids[title], now)
	}
}

func (f *homeFixture) model() *TasteModel {
	f.t.Helper()
	m, err := f.s.TasteModel(context.Background(), f.uid, f.idx, VODTaste{})
	if err != nil {
		f.t.Fatal(err)
	}
	return m
}

func titlesOf(list []Summary) string {
	out := make([]string, len(list))
	for i, it := range list {
		out[i] = it.Title
	}
	return strings.Join(out, ",")
}

// Someone who loves both space films and romances gets both, in their own clusters, rather
// than an average that fits neither; each pick says which film it's there for.
func TestTasteClustersAndRecommended(t *testing.T) {
	f := newHomeFixture(t)
	ctx := context.Background()
	for i, n := range []string{"S1", "S2", "S3", "S4", "S5", "S6"} {
		f.add(n, 1, 0.05*float32(i), 0, 8, 2000)
		f.watched(n, 0)
	}
	for i, n := range []string{"R1", "R2", "R3", "R4", "R5", "R6"} {
		f.add(n, 0.05*float32(i), 1, 0, 8, 2000)
		f.watched(n, 0)
	}
	mustExec(t, f.d, `UPDATE user_item_state SET rating = 10 WHERE item_id = ?`, f.ids["R1"])
	f.add("Space new", 1, 0.02, 0.1, 7, 2020)
	f.add("Romance new", 0.02, 1, 0.1, 7, 2020)
	f.add("Between", 0.7, 0.7, 0, 7, 2020) // the average of both, and neither
	m := f.model()
	if len(m.clusters) != 2 {
		t.Fatalf("clusters: %d", len(m.clusters))
	}
	list, err := f.s.RecommendedFor(ctx, Access{UserID: f.uid}, m, nil, 2)
	if err != nil {
		t.Fatal(err)
	}
	got := titlesOf(list)
	if !strings.Contains(got, "Space new") || !strings.Contains(got, "Romance new") {
		t.Fatalf("both tastes: %s", got)
	}
	for _, it := range list {
		if it.Title == "Romance new" && it.Reason != "Because you loved R1" {
			t.Errorf("reason: %q", it.Reason)
		}
		if it.Title == "Space new" && !strings.HasPrefix(it.Reason, "Because you watched S") {
			t.Errorf("reason: %q", it.Reason)
		}
	}
}

// A row ordered for the person: what fits them first; one film of a series; a title passed
// over on Home day after day gives way; it turns each day, the same all day.
func TestOrderFitVarietyAndTurn(t *testing.T) {
	f := newHomeFixture(t)
	for _, n := range []string{"S1", "S2", "S3"} {
		f.add(n, 1, 0, 0, 8, 2000)
		f.watched(n, 0)
	}
	a := f.add("Space A", 1, 0.1, 0, 7, 2010)
	b := f.add("Space B (sequel)", 1, 0.12, 0.05, 7, 2012)
	c := f.add("Romance", 0, 1, 0, 9, 2010)
	d := f.add("Space D", 0.8, 0.5, 0.2, 7, 2011)
	col := mustExec(t, f.d, `INSERT INTO items(library_id, type, title, sort_title) VALUES (?, 'collection', 'Space Saga', 'x')`, f.lib)
	mustExec(t, f.d, `INSERT INTO collection_items(collection_id, item_id, ord) VALUES (?, ?, 1), (?, ?, 2)`, col, a, col, b)
	cands := []Cand{{ID: a, Rating: 7}, {ID: b, Rating: 7}, {ID: c, Rating: 9}, {ID: d, Rating: 7}}
	m := f.model()
	picks := m.Order(cands, 3)
	if len(picks) != 3 || picks[0].ID != a || picks[1].ID != d || picks[2].ID != c {
		t.Fatalf("order: %+v", picks)
	}
	if picks[0].Reason == "" || picks[2].Reason != "" {
		t.Errorf("reasons: %+v", picks)
	}
	// Space A shown on eight days without being opened: it gives way.
	mustExec(t, f.d, `INSERT INTO home_seen(user_id, item_id, days, last_day) VALUES (?, ?, 8, '2026-01-01')`, f.uid, a)
	if picks := f.model().Order(cands, 3); picks[0].ID == a {
		t.Errorf("passed over, still first: %+v", picks)
	}
	// Opening it ends that.
	f.s.Opened(context.Background(), f.uid, a)
	if picks := f.model().Order(cands, 3); picks[0].ID != a {
		t.Errorf("opened, but: %+v", picks)
	}

	// The daily turn: the same order all day, different on another day.
	jitterScale = 1
	defer func() { jitterScale = 0 }()
	var even []Cand
	for i := 0; i < 30; i++ {
		even = append(even, Cand{ID: f.add("E"+string(rune('a'+i)), 0, 0, 1, 7, 2000), Rating: 7})
	}
	m = f.model()
	first, again := m.Order(even, 10), m.Order(even, 10)
	m.day = "2001-01-01"
	other := m.Order(even, 10)
	same := func(x, y []Pick) bool {
		for i := range x {
			if x[i].ID != y[i].ID {
				return false
			}
		}
		return true
	}
	if !same(first, again) || same(first, other) {
		t.Errorf("turn: %v %v %v", first, again, other)
	}
}

// Not interested: gone from every suggestion row, and a dislike even with learning off; undone,
// it's back.
func TestNotInterested(t *testing.T) {
	f := newHomeFixture(t)
	ctx := context.Background()
	f.add("Liked", 1, 0, 0, 8, 2000)
	f.watched("Liked", 0)
	nope := f.add("Nope", 1, 0.1, 0, 9, 2001)
	f.add("Other", 0.8, 0.3, 0, 8, 2002)
	acc := Access{UserID: f.uid}
	if err := f.s.SetNotInterested(ctx, f.uid, nope, true); err != nil {
		t.Fatal(err)
	}
	if !f.s.NotInterested(ctx, f.uid, nope) {
		t.Fatal("not marked")
	}
	top, _ := f.s.Discover(ctx, acc, []int64{f.lib}, DiscoverQuery{MinRating: 7}, 10)
	rec, _ := f.s.RecommendedFor(ctx, acc, f.model(), nil, 10)
	if strings.Contains(titlesOf(top), "Nope") || strings.Contains(titlesOf(rec), "Nope") {
		t.Fatalf("still suggested: %s / %s", titlesOf(top), titlesOf(rec))
	}
	if neg, _ := f.s.disliked(ctx, f.uid); len(neg) != 1 || neg[0] != nope {
		t.Errorf("disliked: %v", neg)
	}
	f.s.SetNotInterested(ctx, f.uid, nope, false)
	top, _ = f.s.Discover(ctx, acc, []int64{f.lib}, DiscoverQuery{MinRating: 7}, 10)
	if !strings.Contains(titlesOf(top), "Nope") {
		t.Errorf("undone, but: %s", titlesOf(top))
	}
}

// Continue the Saga, Hidden Gems and More From.
func TestSagaGemsAndPeople(t *testing.T) {
	f := newHomeFixture(t)
	ctx := context.Background()
	acc := Access{UserID: f.uid}
	one := f.add("Saga One", 1, 0, 0, 8, 2001)
	two := f.add("Saga Two", 1, 0.1, 0, 8, 2003)
	three := f.add("Saga Three", 1, 0.2, 0, 8, 2005)
	col := mustExec(t, f.d, `INSERT INTO items(library_id, type, title, sort_title) VALUES (?, 'collection', 'The Saga Collection', 'x')`, f.lib)
	mustExec(t, f.d, `INSERT INTO collection_items(collection_id, item_id, ord) VALUES (?, ?, 1), (?, ?, 2), (?, ?, 3)`, col, one, col, two, col, three)
	f.watched("Saga One", 9)
	saga, err := f.s.NextInCollections(ctx, acc, []int64{f.lib}, 10)
	if err != nil || titlesOf(saga) != "Saga Two" || saga[0].Reason != "Next in The Saga Collection" {
		t.Fatalf("saga: %s %+v %v", titlesOf(saga), saga, err)
	}

	// Hidden gems: well rated, nobody here has played it, in the library a month.
	gem := f.add("Gem", 0, 1, 0, 8.5, 1999)
	played := f.add("Played by Bo", 0, 1, 0.1, 8.5, 1999)
	f.add("Too new", 0, 1, 0.2, 8.5, 1999)
	mustExec(t, f.d, `UPDATE items SET added_at = ? WHERE title = 'Too new'`, isoUTC(time.Now()))
	bo := mustExec(t, f.d, `INSERT INTO users(username, display_name) VALUES ('bo', 'Bo')`)
	mustExec(t, f.d, `INSERT INTO user_item_state(user_id, item_id, play_count) VALUES (?, ?, 1)`, bo, played)
	gems, _ := f.s.Discover(ctx, acc, []int64{f.lib}, DiscoverQuery{MinRating: 7.3, Unplayed: true}, 10)
	if got := titlesOf(gems); !strings.Contains(got, "Gem") || strings.Contains(got, "Played by Bo") || strings.Contains(got, "Too new") {
		t.Fatalf("gems: %s", got)
	}
	_ = gem

	// More From: a director of two films Ann loved, with three more to offer.
	nolan := mustExec(t, f.d, `INSERT INTO people(name) VALUES ('Chris Director')`)
	extra := mustExec(t, f.d, `INSERT INTO people(name) VALUES ('Once Only')`)
	f.watched("Saga Two", 9)
	for _, id := range []int64{one, two} {
		mustExec(t, f.d, `INSERT INTO credits(item_id, person_id, role) VALUES (?, ?, 'director')`, id, nolan)
	}
	mustExec(t, f.d, `INSERT INTO credits(item_id, person_id, role, ord) VALUES (?, ?, 'actor', 0)`, one, extra)
	for _, n := range []string{"Dir A", "Dir B", "Dir C"} {
		id := f.add(n, 0.5, 0.5, 0, 7, 2010)
		mustExec(t, f.d, `INSERT INTO credits(item_id, person_id, role) VALUES (?, ?, 'director')`, id, nolan)
	}
	rows, err := f.s.PeopleYouLove(ctx, acc, f.model(), []int64{f.lib}, 2, 10)
	if err != nil || len(rows) != 1 || rows[0].Name != "Chris Director" || !rows[0].Director || len(rows[0].Items) != 3 {
		t.Fatalf("people: %+v %v", rows, err)
	}
}

// What Home showed and what was played from it.
func TestHomeShownAndContext(t *testing.T) {
	f := newHomeFixture(t)
	ctx := context.Background()
	a := f.add("A", 1, 0, 0, 8, 2000)
	b := f.add("B", 0, 1, 0, 8, 2000)
	if err := f.s.HomeShown(ctx, f.uid, map[string][]int64{"recommended": {a}, "top-rated": {a, b}, "continue-watching": {b}}); err != nil {
		t.Fatal(err)
	}
	f.s.HomeShown(ctx, f.uid, map[string][]int64{"top-rated": {a}}) // again the same day: still one day
	var days int
	f.d.QueryRow(`SELECT days FROM home_seen WHERE user_id = ? AND item_id = ?`, f.uid, a).Scan(&days)
	if days != 1 {
		t.Errorf("days: %d", days)
	}
	if ctxRow := f.s.HomeContext(ctx, f.uid, a); ctxRow != "home:top-rated" && ctxRow != "home:recommended" {
		t.Errorf("context: %q", ctxRow)
	}
	if got := f.s.HomeContext(ctx, f.uid, 99999); got != "" {
		t.Errorf("never shown: %q", got)
	}
	mustExec(t, f.d, `INSERT INTO play_history(user_id, item_id, item_title, started_at, context) VALUES (?, ?, 'A', ?, 'home:top-rated')`,
		f.uid, a, isoUTC(time.Now()))
	use, err := f.s.HomeRowUse(ctx, f.uid)
	if err != nil || use["top-rated"].Shown != 2 || use["top-rated"].Plays != 1 || use["recommended"].Plays != 0 {
		t.Errorf("use: %+v %v", use, err)
	}
}
