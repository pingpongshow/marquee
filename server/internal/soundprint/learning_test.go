package soundprint

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"marquee/internal/db"
)

// learnLibrary is a music library with tracks in the database and the index; artist n is
// item 100000+n, with one album (item 200000+n).
type learnLibrary struct {
	t  *testing.T
	db *sql.DB
	s  *Service
}

func newLearnLibrary(t *testing.T) *learnLibrary {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	d, err := db.Open(ctx, filepath.Join(dir, "t.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	d.Exec(`INSERT INTO libraries (id, name, type) VALUES (1, 'Music', 'music')`)
	d.Exec(`INSERT INTO users (id, username, display_name) VALUES (1, 'u', 'U'), (2, 'v', 'V')`)
	return &learnLibrary{t: t, db: d, s: &Service{DB: d, Index: NewIndex()}}
}

func (l *learnLibrary) exec(q string, args ...any) {
	l.t.Helper()
	if _, err := l.db.Exec(q, args...); err != nil {
		l.t.Fatalf("%s: %v", q, err)
	}
}

// track adds a track (and its artist and album) with a vector, traits and the like.
func (l *learnLibrary) track(id, artist int64, vec []float32, edit func(*Track)) *Track {
	l.t.Helper()
	artist, album := 100000+artist, 200000+artist
	l.db.Exec(`INSERT INTO items (id, library_id, type, title, sort_title) VALUES (?, 1, 'artist', ?, ?) ON CONFLICT DO NOTHING`, artist, fmt.Sprint("Artist ", artist), "a")
	l.db.Exec(`INSERT INTO items (id, library_id, type, parent_id, title, sort_title) VALUES (?, 1, 'album', ?, 'Album', 'b') ON CONFLICT DO NOTHING`, album, artist)
	title := fmt.Sprint("Song ", id)
	l.exec(`INSERT INTO items (id, library_id, type, parent_id, grandparent_id, title, sort_title, duration_ms) VALUES (?, 1, 'track', ?, ?, ?, ?, 200000)`,
		id, album, artist, title, title)
	tr := &Track{ItemID: id, ArtistID: artist, AlbumID: album, LibraryID: 1, Vec: normalize(vec), Copy: CopyKey(title, artist), DurationMS: 200000}
	if edit != nil {
		edit(tr)
	}
	l.s.Index.Put(tr)
	return tr
}

func (l *learnLibrary) play(user, item int64, at time.Time, positionMS int64) {
	l.exec(`INSERT INTO play_history (user_id, item_id, item_title, started_at, stopped_at, position_ms) VALUES (?, ?, 't', ?, ?, ?)`,
		user, item, at.UTC().Format("2006-01-02T15:04:05.000Z"), at.UTC().Add(time.Minute).Format("2006-01-02T15:04:05.000Z"), positionMS)
}

// Tracks played in one sitting pair up (nearer ones more), across listeners; a quick skip,
// another sitting or the same album doesn't pair; a playlist's neighbours do. A radio then
// lifts what's played with the track before.
func TestRebuildCoplays(t *testing.T) {
	l := newLearnLibrary(t)
	ctx := context.Background()
	for i := int64(1); i <= 6; i++ {
		l.track(i, i, []float32{1, float32(i) / 100}, nil)
	}
	l.track(7, 1, []float32{1, 0.07}, nil) // on artist 1's album
	start := time.Now().Add(-48 * time.Hour)
	for _, user := range []int64{1, 2} {
		at := start.Add(time.Duration(user) * time.Hour)
		l.play(user, 1, at, 190000)
		l.play(user, 2, at.Add(4*time.Minute), 190000)
		l.play(user, 3, at.Add(8*time.Minute), 5000) // skipped
		l.play(user, 7, at.Add(9*time.Minute), 190000)
		l.play(user, 6, at.Add(5*time.Hour), 190000) // another sitting
	}
	l.exec(`INSERT INTO playlists (id, user_id, title, kind) VALUES (1, 1, 'p', 'audio')`)
	for i, item := range []int64{4, 5, 4, 5} {
		l.exec(`INSERT INTO playlist_items (playlist_id, item_id, ord) VALUES (1, ?, ?)`, item, i)
	}
	msg, err := l.s.RebuildCoplays(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(msg, "tracks paired") {
		t.Errorf("message %q", msg)
	}
	if w := l.s.CoPlay(1, 2); w != 1 {
		t.Errorf("1 and 2 played together by two people: %v", w)
	}
	if l.s.CoPlay(1, 3) != 0 || l.s.CoPlay(2, 6) != 0 || l.s.CoPlay(1, 7) != 0 {
		t.Errorf("skip %v, other sitting %v, same album %v", l.s.CoPlay(1, 3), l.s.CoPlay(2, 6), l.s.CoPlay(1, 7))
	}
	if l.s.CoPlay(4, 5) == 0 {
		t.Error("playlist neighbours didn't pair")
	}
	// Stored and loaded back.
	l.s.coplays = nil
	if err := l.s.LoadLearned(ctx); err != nil || l.s.CoPlay(2, 1) != 1 {
		t.Errorf("loaded back: %v %v", l.s.CoPlay(2, 1), err)
	}
	// Flow from 1: 2 is lifted over 3 though 3 sounds as close.
	o := Options{Length: 2, CoPlay: l.s.CoPlay, Rand: nil}
	first := l.s.Index.Get(1)
	l.s.Index.update(3, func(t *Track) { t.Vec = normalize([]float32{1, 0.02}) })
	if got := l.s.Index.Flow(first.Vec, first, o); len(got) < 2 || got[1].ItemID != 2 {
		t.Errorf("flow after 1: %v", TrackIDs(got))
	}
}

// fakeSidecar answers /embed_text with a fixed vector per text (by "model|text", then by
// text) and /analyze with analyze's embeddings.
func fakeSidecar(t *testing.T, text map[string][]float32, analyze func(paths []string, model string) []Analysis) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Texts []string `json:"texts"`
			Paths []string `json:"paths"`
			Model string   `json:"model"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		switch r.URL.Path {
		case "/embed_text":
			var out [][]float32
			for _, tx := range req.Texts {
				v := text[req.Model+"|"+tx]
				if v == nil {
					v = text[tx]
				}
				if v == nil {
					v = []float32{1, 0, 0}
				}
				out = append(out, v)
			}
			json.NewEncoder(w).Encode(map[string]any{"model": "clap", "embeddings": out})
		case "/analyze":
			json.NewEncoder(w).Encode(map[string]any{"model": "laion/larger_clap_" + req.Model + "#2", "results": analyze(req.Paths, req.Model)})
		case "/health":
			json.NewEncoder(w).Encode(Health{Model: "laion/larger_clap_general#2", Models: map[string]string{
				"general": "laion/larger_clap_general#2", "music": "laion/larger_clap_music#2"}})
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// A mood radio plays what fits the mood (here: the sad, quiet, minor tracks tagged
// "melancholic") over what merely sounds like its words, and leaves out what fits least.
func TestMoodRadio(t *testing.T) {
	l := newLearnLibrary(t)
	ctx := context.Background()
	// The descriptions sound like "x"; half the tracks sit near x but are cheerful and loud,
	// the other half a little further off but sad, calm and in minor keys.
	var sad, happy []int64
	for i := int64(1); i <= 400; i++ {
		artist := 1 + i%40
		if i%2 == 0 {
			l.track(i, artist, []float32{1, 0.3, float32(i%7) / 50}, func(t *Track) {
				t.Traits = &Traits{Sad: 0.6, Happy: 0.05, Dance: 0.1}
				t.Energy, t.Mode = 0.2, "minor"
			})
			sad = append(sad, i)
		} else {
			l.track(i, artist, []float32{1, 0.05, float32(i%7) / 50}, func(t *Track) {
				t.Traits = &Traits{Sad: 0.05, Happy: 0.7, Dance: 0.6}
				t.Energy, t.Mode = 0.8, "major"
			})
			happy = append(happy, i)
		}
	}
	l.exec(`INSERT INTO music_tags (item_id, tag, source, weight) VALUES (?, 'melancholic', 'lastfm', 1)`, sad[0])
	if err := l.s.LoadCommunity(ctx); err != nil {
		t.Fatal(err)
	}
	srv := fakeSidecar(t, map[string][]float32{}, nil)
	l.s.Client = &Client{BaseURL: srv.URL}
	st, err := l.s.MoodRadio(ctx, "Melancholy", Options{Length: 30})
	if err != nil {
		t.Fatal(err)
	}
	if st.Title != "Melancholy Radio" || len(st.IDs) != 30 {
		t.Fatalf("%q: %d tracks", st.Title, len(st.IDs))
	}
	nSad := 0
	for _, id := range st.IDs {
		if id%2 == 0 {
			nSad++
		}
	}
	if nSad < 27 {
		t.Errorf("%d of 30 tracks are the sad ones", nSad)
	}
	if moodFor("Rainy afternoon").Phrases[0] == "" || len(Moods) != 8 {
		t.Error("moods")
	}
}

// Another model chosen in Settings is tried on the library (the index keeps the current
// embeddings), compared once it covers the library, and switched to only when asked; keeping
// the current one drops the trial.
func TestSoundModelTrial(t *testing.T) {
	l := newLearnLibrary(t)
	ctx := context.Background()
	for i := int64(1); i <= 12; i++ {
		l.track(i, 1+i%3, []float32{1, float32(i)}, nil)
		if i == 1 {
			l.exec(`INSERT INTO media_versions (id, item_id) VALUES (1, 1)`)
		}
		l.exec(`INSERT INTO media_files (id, version_id, library_id, path, size, mtime) VALUES (?, 1, 1, ?, 1, 1)`, i, fmt.Sprint("/m/", i, ".flac"))
		l.exec(`INSERT INTO soundprint (item_id, file_id, model, embedding) VALUES (?, ?, 'laion/larger_clap_general#2', ?)`, i, i, encode(normalize([]float32{1, float32(i)})))
	}
	srv := fakeSidecar(t, nil, func(paths []string, model string) []Analysis {
		var out []Analysis
		for _, p := range paths {
			out = append(out, Analysis{Path: p, Embedding: []float32{0, 1, float32(len(p))}})
		}
		return out
	})
	want := ModelGeneral
	l.s.Client = &Client{BaseURL: srv.URL}
	l.s.Enabled = func() bool { return true }
	l.s.Model = func() string { return want }
	if l.s.Trial(ctx) != nil {
		t.Fatal("a trial with the model in use")
	}
	want = ModelMusic
	h, _ := l.s.Client.Health(ctx)
	msg, err := l.s.trialPass(ctx, h)
	if err != nil {
		t.Fatal(err)
	}
	tr := l.s.Trial(ctx)
	if tr == nil || tr.Analyzed != 12 || tr.Total != 12 || tr.Tried == nil || tr.Current == nil || !strings.Contains(msg, "ready to compare") {
		t.Fatalf("trial %+v, %q", tr, msg)
	}
	if l.s.ActiveModel(ctx) != ModelGeneral || l.s.Index.Get(1).Vec[1] == 0 {
		t.Error("the trial replaced the model in use")
	}
	if err := l.s.SwitchModel(ctx); err != nil {
		t.Fatal(err)
	}
	if l.s.ActiveModel(ctx) != ModelMusic || l.s.Index.Get(1).Vec[0] != 0 || l.s.Trial(ctx) != nil {
		t.Errorf("after the switch: %s %v %+v", l.s.ActiveModel(ctx), l.s.Index.Get(1).Vec, l.s.Trial(ctx))
	}
	// Keeping: the trial is dropped.
	want = ModelGeneral
	l.s.trialPass(ctx, h)
	if err := l.s.EndTrial(ctx); err != nil {
		t.Fatal(err)
	}
	var left int
	l.db.QueryRow(`SELECT COUNT(*) FROM soundprint WHERE next_model IS NOT NULL`).Scan(&left)
	if left != 0 {
		t.Errorf("%d trial embeddings left", left)
	}
	if err := l.s.SwitchModel(ctx); err != ErrNoTrial {
		t.Errorf("switch without a trial: %v", err)
	}
}

func TestScoreModels(t *testing.T) {
	// Model a groups tracks by genre; model b scatters them.
	cur, next, album := map[int64][]float32{}, map[int64][]float32{}, map[int64]int64{}
	genres := map[int64]map[string]bool{}
	for i := int64(1); i <= 40; i++ {
		g := i % 2
		cur[i] = normalize([]float32{float32(g), float32(1 - g), float32(i) / 1000})
		next[i] = normalize([]float32{float32(i % 5), float32(i % 3), 1})
		album[i] = i
		genres[i] = map[string]bool{fmt.Sprint("g", g): true}
	}
	a, b := scoreModels(cur, next, album, genres, nil)
	if a.GenreMatch < 0.95 || b.GenreMatch >= a.GenreMatch {
		t.Errorf("genre match %v vs %v", a.GenreMatch, b.GenreMatch)
	}
}

// The time-of-day mix plays what someone plays at this time of day, and nothing the day's
// other mixes have.
func TestTimeMix(t *testing.T) {
	l := newLearnLibrary(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 30, 8, 30, 0, 0, time.Local) // morning
	for i := int64(1); i <= 60; i++ {
		v := []float32{1, 0}
		if i > 30 {
			v = []float32{0, 1} // the night music
		}
		l.track(i, i, append(v, float32(i)/1000), nil)
	}
	for d := 1; d <= 12; d++ {
		day := now.AddDate(0, 0, -d)
		l.play(1, int64(d), day.Add(-time.Hour), 190000)                       // 7:30
		l.play(1, int64(30+d), day.Add(15*time.Hour), 190000)                  // 23:30
		l.play(1, int64(d+12), day.Add(-30*time.Minute), 190000)               // 8:00
		l.play(1, int64(42+d%18), day.Add(15*time.Hour+10*time.Minute), 10000) // a skip at night
	}
	m := l.s.timeMix(ctx, 1, "", nil, now, []Mix{{Station: Station{IDs: []int64{1, 2}}}})
	if m == nil {
		t.Fatal("no morning mix")
	}
	if m.Title != "Your Morning Mix" || m.ID != "time" {
		t.Errorf("%q %q", m.Title, m.ID)
	}
	for _, id := range m.IDs {
		if id > 30 {
			t.Errorf("night music %d in the morning mix", id)
		}
		if id == 1 || id == 2 {
			t.Errorf("%d is in another mix", id)
		}
	}
	if again := l.s.timeMix(ctx, 1, "", nil, now, nil); again != m {
		t.Error("not kept for the part of the day")
	}
}

// A model whose words point at the right music scores high on TextMatch; one whose words
// point nowhere (as the music model's did in testing) scores about chance.
func TestTextMatch(t *testing.T) {
	vecs, genres := map[int64][]float32{}, map[int64]map[string]bool{}
	for i := int64(1); i <= 240; i++ {
		g, v := "rock", []float32{1, 0.01 * float32(i%5)}
		if i%2 == 0 {
			g, v = "jazz", []float32{0.01 * float32(i%5), 1}
		}
		vecs[i], genres[i] = normalize(v), map[string]bool{g: true}
	}
	names := topGenres(genres, textGenres)
	if len(names) != 2 {
		t.Fatalf("genres %v", names)
	}
	srv := fakeSidecar(t, map[string][]float32{
		"general|rock music": {1, 0}, "general|jazz music": {0, 1},
		"music|rock music": {0.7, 0.71}, "music|jazz music": {0.71, 0.7},
	}, nil)
	s := &Service{Client: &Client{BaseURL: srv.URL}}
	good := s.textMatch(context.Background(), ModelGeneral, vecs, genres, names)
	poor := s.textMatch(context.Background(), ModelMusic, vecs, genres, names)
	if good < 0.95 || poor > 0.6 {
		t.Errorf("text match: general %.2f, music %.2f", good, poor)
	}
}
