package muse

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"marquee/internal/ai"
	"marquee/internal/db"
	"marquee/internal/items"
)

// fakeLLM answers every chat with a fixed JSON (or an error) and counts calls.
type fakeLLM struct {
	answer string
	err    error
	calls  int
	user   string
}

func (f *fakeLLM) Ready() bool { return true }
func (f *fakeLLM) Chat(_ context.Context, _, user string, _ any, out any) error {
	f.calls++
	f.user = user
	if f.err != nil {
		return f.err
	}
	return json.Unmarshal([]byte(f.answer), out)
}

type fakeLookup struct{}

func (fakeLookup) VideoPerson(_ context.Context, name string) (string, error) {
	if strings.EqualFold(name, "ridley scott") {
		return "Ridley Scott", nil
	}
	return "", nil
}

func (fakeLookup) VideoByTitle(_ context.Context, _ items.Access, title string, year int) (int64, string, bool, error) {
	if strings.EqualFold(title, "alien") && (year == 0 || year == 1979) {
		return 7, "Alien", true, nil
	}
	return 0, "", false, nil
}

var genres = []string{"Horror", "Science Fiction", "Sci-Fi & Fantasy", "Comedy", "Thriller", "Animation"}

func TestReadVideoMapsAndValidates(t *testing.T) {
	llm := &fakeLLM{answer: `{"types":["movie"],"yearFrom":1999,"yearTo":1980,"genres":["science fiction","Space Opera"],
		"excludeGenres":["comedy"],"unwatched":true,"maxMinutes":5,"minMinutes":0,"highlyRated":true,"kidFriendly":false,
		"people":["ridley scott","Unknown Person"],"like":[{"title":"Alien","year":1979},{"title":"Not In Library","year":0}],
		"meaning":"isolation in space","title":"\"Lonely Space Terror\""}`}
	r := ReadVideo(context.Background(), llm, fakeLookup{}, items.Access{}, "80s sci-fi like Alien by Ridley Scott, no comedy", items.VideoVocab{Genres: genres})
	q := r.Query
	if !r.AI || r.Title != "Lonely Space Terror" {
		t.Fatalf("reading %+v", r)
	}
	if !strings.Contains(llm.user, "Science Fiction") {
		t.Error("the prompt should list the library's genres")
	}
	want := items.VideoQuery{Types: []string{"movie"}, YearFrom: 1980, YearTo: 1999, Genres: [][]string{{"Science Fiction", "Sci-Fi & Fantasy"}},
		ExcludeGenres: []string{"Comedy"}, Unwatched: true, MinRating: 7.5, People: []string{"Ridley Scott"}, Seeds: []int64{7}, SeedTitles: []string{"Alien"},
		Rest: "isolation in space Unknown Person Not In Library"}
	got := q
	got.Relabel()
	want.Relabel()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("query\n got %+v\nwant %+v", got, want)
	}
	if q.MaxMinutes != 0 {
		t.Error("an absurd length should be dropped")
	}
}

func TestReadVideoFallsBack(t *testing.T) {
	kw := items.ParseVideoPrompt("90s horror movies", genres)
	for name, llm := range map[string]*fakeLLM{
		"error":   {err: errors.New("down")},
		"empty":   {answer: `{"types":[],"yearFrom":0,"yearTo":0,"genres":["Nope"],"excludeGenres":[],"people":[],"like":[],"meaning":"","title":""}`},
		"garbage": {answer: `{"types":"movie"}`},
	} {
		r := ReadVideo(context.Background(), llm, fakeLookup{}, items.Access{}, "90s horror movies", items.VideoVocab{Genres: genres})
		if r.AI || r.Query.Understood() != kw.Understood() {
			t.Errorf("%s: got %q (AI %v), want the keyword reading %q", name, r.Query.Understood(), r.AI, kw.Understood())
		}
	}
	// No AI at all.
	if r := ReadVideo(context.Background(), nil, fakeLookup{}, items.Access{}, "90s horror movies", items.VideoVocab{Genres: genres}); r.AI || r.Query.Understood() != kw.Understood() {
		t.Errorf("nil llm: %+v", r)
	}
}

// The real client against a fake Ollama: the reading travels through /api/chat.
func TestReadVideoThroughOllama(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"message": map[string]any{"role": "assistant",
			"content": `{"types":["show"],"yearFrom":0,"yearTo":0,"genres":["Animation"],"excludeGenres":[],"unwatched":false,"maxMinutes":0,"minMinutes":0,"highlyRated":false,"kidFriendly":true,"people":[],"like":[],"meaning":"dinosaurs","title":"Dino Toons"}`}})
	}))
	defer srv.Close()
	c := &ai.Client{Config: func() ai.Config {
		return ai.Config{Enabled: true, URL: srv.URL, ChatModel: "qwen3:8b", EmbedModel: "mxbai-embed-large"}
	}}
	r := ReadVideo(context.Background(), c, fakeLookup{}, items.Access{}, "cartoons about dinosaurs for the kids", items.VideoVocab{Genres: genres})
	if !r.AI || r.Query.Understood() != "Shows · Animation · Family-friendly · like 'dinosaurs'" || r.Title != "Dino Toons" {
		t.Fatalf("reading %q %+v", r.Query.Understood(), r)
	}
}

func TestRerank(t *testing.T) {
	cands := []Candidate{{ID: 10, Title: "A"}, {ID: 20, Title: "B"}, {ID: 30, Title: "C"}, {ID: 40, Title: "D"}}
	llm := &fakeLLM{answer: `{"results":[{"n":3,"reason":"Best fit."},{"n":9,"reason":"invented"},{"n":1,"reason":"Close."},{"n":3,"reason":"again"}]}`}
	order, why, err := Rerank(context.Background(), llm, "prompt", nil, cands)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(order, []int64{30, 10, 20, 40}) {
		t.Fatalf("order %v", order)
	}
	if why[30] != "Best fit." || why[10] != "Close." || len(why) != 2 {
		t.Fatalf("reasons %v", why)
	}
	if !strings.Contains(llm.user, "3. C") {
		t.Errorf("candidates not numbered: %q", llm.user)
	}
	if _, _, err := Rerank(context.Background(), &fakeLLM{answer: `{"results":[]}`}, "p", nil, cands); err == nil {
		t.Fatal("an empty answer should fail so the ranking is kept")
	}
	if _, _, err := Rerank(context.Background(), &fakeLLM{err: errors.New("down")}, "p", nil, cands); err == nil {
		t.Fatal("want error")
	}
}

func TestReasonsCached(t *testing.T) {
	var r Reasons
	llm := &fakeLLM{answer: `{"results":[{"n":1,"reason":"Same director."},{"n":2,"reason":"Similar mood."}]}`}
	cands := []Candidate{{ID: 1, Title: "A"}, {ID: 2, Title: "B"}}
	got := r.Get(context.Background(), llm, 5, []string{"Alien"}, cands)
	if got[1] != "Same director." || got[2] != "Similar mood." {
		t.Fatalf("reasons %v", got)
	}
	got = r.Get(context.Background(), llm, 5, []string{"Alien"}, cands)
	if llm.calls != 1 || len(got) != 2 {
		t.Fatalf("cache missed: %d calls", llm.calls)
	}
	// Another person isn't served the first one's reasons.
	r.Get(context.Background(), llm, 6, []string{"Heat"}, cands)
	if llm.calls != 2 {
		t.Fatalf("calls %d", llm.calls)
	}
	// Nothing liked yet: no reasons, no call.
	if got := r.Get(context.Background(), llm, 7, nil, cands); len(got) != 0 || llm.calls != 2 {
		t.Fatal("no history should give no reasons")
	}
}

var musicLib = MusicLibrary{
	Genres:  []string{"Rock", "Jazz", "Electronic", "Hip-Hop"},
	Artists: map[int64]string{1: "Radiohead", 2: "The Beatles", 3: "Air", 4: "Simon & Garfunkel", 5: "Simon"},
}

func TestReadMusicAI(t *testing.T) {
	llm := &fakeLLM{answer: `{"genres":["rock","Shoegaze"],"yearFrom":1999,"yearTo":1990,"artists":["the beatles"],"likeArtists":["Radiohead","Unknown Band"],
		"favourites":false,"unplayed":true,"recentlyAdded":false,"rediscover":false,"maxTrackMinutes":0,"totalMinutes":60,
		"sound":"melancholy guitars","theme":"the ocean","name":"Sad Sea Songs"}`}
	r := ReadMusic(context.Background(), llm, musicLib, "an hour of unheard 90s rock like radiohead, songs about the ocean")
	q := r.Query
	if !r.AI || q.Title != "Sad Sea Songs" || q.Theme != "the ocean" || q.TotalMinutes != 60 || !q.Unplayed {
		t.Fatalf("reading %+v", r)
	}
	if !reflect.DeepEqual(q.Genres, []string{"Rock"}) || q.YearFrom != 1990 || q.YearTo != 1999 {
		t.Fatalf("filters %+v", q)
	}
	if !reflect.DeepEqual(q.ArtistIDs, []int64{2}) || !reflect.DeepEqual(q.LikeArtistIDs, []int64{1}) {
		t.Fatalf("artists %+v", q)
	}
	if q.Sound != "melancholy guitars Unknown Band" {
		t.Fatalf("sound %q", q.Sound)
	}
	// A failed or empty answer falls back to keywords.
	for _, bad := range []*fakeLLM{{err: errors.New("down")}, {answer: `{"genres":[],"artists":[],"likeArtists":[],"sound":"","theme":"","name":"x"}`}} {
		if r := ReadMusic(context.Background(), bad, musicLib, "90s rock"); r.AI || !reflect.DeepEqual(r.Query.Genres, []string{"Rock"}) {
			t.Fatalf("fallback %+v", r)
		}
	}
}

func TestKeywordMusic(t *testing.T) {
	cases := []struct {
		prompt       string
		genres       []string
		from, to     int
		by, like     []int64
		theme        string
		unplayed     bool
		fav, rediscv bool
	}{
		{prompt: "90s rock", genres: []string{"Rock"}, from: 1990, to: 1999},
		{prompt: "chill electronic like Radiohead", genres: []string{"Electronic"}, like: []int64{1}},
		{prompt: "songs by the beatles", by: []int64{2}},
		{prompt: "Simon & Garfunkel for a rainy day", like: []int64{4}},
		{prompt: "fresh air on a summer morning"}, // "air" is just a word here
		{prompt: "jazz i haven't heard", genres: []string{"Jazz"}, unplayed: true},
		{prompt: "rediscover my old favourites", rediscv: true},
		{prompt: "my favourites from the 1970s", from: 1970, to: 1979, fav: true},
		{prompt: "songs about the ocean", theme: "the ocean"},
		{prompt: "breakup songs", theme: "breakup"},
	}
	for _, c := range cases {
		q := KeywordMusic(musicLib, c.prompt).Query
		if !reflect.DeepEqual(q.Genres, c.genres) || q.YearFrom != c.from || q.YearTo != c.to || !reflect.DeepEqual(q.ArtistIDs, c.by) ||
			!reflect.DeepEqual(q.LikeArtistIDs, c.like) || q.Theme != c.theme || q.Unplayed != c.unplayed || q.Favourites != c.fav || q.Rediscover != c.rediscv {
			t.Errorf("%q: got %+v", c.prompt, q)
		}
		if q.Sound != c.prompt || q.Title != c.prompt {
			t.Errorf("%q: the whole prompt should still describe the sound", c.prompt)
		}
	}
}

func TestThemeBoost(t *testing.T) {
	sims := map[int64]float64{}
	for i := int64(1); i <= 100; i++ {
		sims[i] = float64(i) / 100
	}
	b := ThemeBoost(sims)
	if b[100] < themeWeight-1e-9 || b[100] > themeWeight+1e-9 {
		t.Fatalf("best match %v", b[100])
	}
	if _, ok := b[50]; ok {
		t.Fatal("an average match got a bonus")
	}
	if len(b) < 10 || len(b) > 16 {
		t.Fatalf("%d boosted, want about 15%%", len(b))
	}
	if b[95] <= b[90] {
		t.Fatal("bonus should grow with similarity")
	}
	if ThemeBoost(nil) != nil || ThemeBoost(map[int64]float64{1: 0.5, 2: 0.5}) != nil {
		t.Fatal("no spread, no bonus")
	}
}

func TestLyricText(t *testing.T) {
	if got := LyricText("[00:01.00]Hello\n[00:02.00]\n[00:03.00]World"); got != "Hello\nWorld" {
		t.Fatalf("got %q", got)
	}
	long := strings.Repeat("a line of lyrics here\n", 200)
	if got := LyricText(long); len(got) > lyricChars || !strings.HasSuffix(got, "here") {
		t.Fatalf("long lyrics cut badly: %d", len(got))
	}
}

// Indexing lyrics through a fake Ollama, then scoring a theme.
func TestLyricThemesIndexAndBoost(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	d, err := db.Open(ctx, filepath.Join(dir, "t.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	d.Exec(`INSERT INTO libraries (id, name, type) VALUES (1, 'Music', 'music')`)
	lyricsFor := map[int64]string{}
	for i := int64(1); i <= 12; i++ {
		d.Exec(`INSERT INTO items (id, library_id, type, title, sort_title) VALUES (?, 1, 'track', 't', 't')`, i)
		text := "[00:01.00]dancing all night in the city"
		if i <= 2 {
			text = "waves crash on the ocean shore\nthe sea calls me home"
		}
		if i == 12 {
			text = "" // looked up, none found
		}
		lyricsFor[i] = text
		d.Exec(`INSERT INTO lyrics (item_id, synced, text, source) VALUES (?, 0, ?, 'embedded')`, i, text)
	}
	var embedded []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Input     []string `json:"input"`
			KeepAlive string   `json:"keep_alive"`
		}
		json.NewDecoder(r.Body).Decode(&in)
		if in.KeepAlive != "5m" {
			t.Errorf("keep_alive %q", in.KeepAlive)
		}
		out := make([][]float32, len(in.Input))
		for i, s := range in.Input {
			embedded = append(embedded, s)
			out[i] = []float32{0.1, 1, 0}
			if strings.Contains(s, "ocean") || strings.Contains(s, "sea") {
				out[i] = []float32{1, 0.1, 0}
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"embeddings": out})
	}))
	defer srv.Close()
	model := "mxbai-embed-large"
	c := &ai.Client{Config: func() ai.Config {
		return ai.Config{Enabled: true, URL: srv.URL, ChatModel: "qwen3:8b", EmbedModel: model, UnloadMinutes: 5}
	}}
	on := true
	l := &LyricThemes{DB: d, AI: c, Enabled: func() bool { return on }}
	msg, err := l.Run(ctx)
	if err != nil || !strings.Contains(msg, "11 tracks") {
		t.Fatalf("run: %q %v", msg, err)
	}
	st := l.Status(ctx)
	if st.Indexed != 11 || st.WithLyrics != 11 || st.Tracks != 12 {
		t.Fatalf("status %+v", st)
	}
	if strings.Contains(strings.Join(embedded, "|"), "[00:01") {
		t.Fatal("timestamps were embedded")
	}
	// Nothing changed: nothing re-embedded.
	n := len(embedded)
	if msg, _ := l.Run(ctx); len(embedded) != n || !strings.Contains(msg, "All 11") {
		t.Fatalf("second run re-embedded: %q", msg)
	}
	// The ocean theme lifts the two sea songs only.
	b := l.Boost(ctx, "the ocean")
	if len(b) != 2 || b[1] == 0 || b[2] == 0 {
		t.Fatalf("boost %v", b)
	}
	if q := embedded[len(embedded)-1]; !strings.HasPrefix(q, ai.QueryPrefix(model)) {
		t.Fatalf("theme query sent as %q", q)
	}
	// Turned off: no boost and no indexing.
	on = false
	if l.Boost(ctx, "the ocean") != nil {
		t.Fatal("boost while off")
	}
	if msg, _ := l.Run(ctx); !strings.Contains(msg, "off") {
		t.Fatalf("run while off: %q", msg)
	}
	// A new model re-embeds everything and the old vectors stop counting.
	on, model = true, "nomic-embed-text"
	if l.Boost(ctx, "the ocean") != nil {
		t.Fatal("vectors of another model were used")
	}
	if _, err := l.Run(ctx); err != nil || l.Status(ctx).Indexed != 11 || len(embedded) != n+1+11 {
		t.Fatalf("model change: %v %+v %d", err, l.Status(ctx), len(embedded)-n)
	}
	// Rebuild forgets everything.
	if err := l.Rebuild(ctx); err != nil || l.Status(ctx).Indexed != 0 {
		t.Fatal("rebuild")
	}
	_ = lyricsFor
}

// "More like my favourites" asks for music like the favourites (their sound steers), not
// only the favourites; "songs I love" alone still means only them.
func TestKeywordMusicLikeFavourites(t *testing.T) {
	cases := []struct {
		prompt     string
		fav, like  bool
		sound      string
		understood string
	}{
		{prompt: "more like my favourites", like: true, sound: "", understood: "Like your favourites"},
		{prompt: "mellow songs similar to songs I love", like: true, sound: "mellow", understood: "Like your favourites"},
		{prompt: "songs I love", fav: true, sound: "songs I love", understood: "Favourites"},
	}
	for _, c := range cases {
		r := KeywordMusic(musicLib, c.prompt)
		q := r.Query
		if q.Favourites != c.fav || q.LikeFavourites != c.like || q.Sound != c.sound || !slices.Contains(r.Understood, c.understood) {
			t.Errorf("%q: fav=%v like=%v sound=%q understood=%v", c.prompt, q.Favourites, q.LikeFavourites, q.Sound, r.Understood)
		}
	}
}
