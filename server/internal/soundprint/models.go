package soundprint

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"sort"
	"time"
)

// Sound models (MUSIC-1). The general model knows all kinds of sound; the music model was
// trained on music alone. Switching re-analyses the whole library, so a model asked for in
// Settings is tried first: the library is analysed with it alongside the one in use (the
// stations keep working), the two are compared on the library, and the admin then switches
// or keeps the current one.
const (
	ModelGeneral = "general"
	ModelMusic   = "music"
)

// activeKey is where the model in use is kept (the settings table): the embeddings in the
// index are all this model's.
const activeKey = "soundprint_model"

// trialKey keeps the finished comparison while it waits for the admin.
const trialKey = "soundprint_trial"

// ErrNoTrial is a switch asked for before a model has been tried on the whole library.
var ErrNoTrial = errors.New("no other sound model has finished its trial")

// ActiveModel is the sound model the index's embeddings come from.
func (s *Service) ActiveModel(ctx context.Context) string {
	var v string
	if s.DB == nil || s.DB.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, activeKey).Scan(&v) != nil || v == "" {
		return ModelGeneral
	}
	return v
}

// wanted is the model chosen in Settings (the active one when unset).
func (s *Service) wanted(ctx context.Context) string {
	if s.Model != nil {
		if m := s.Model(); m == ModelGeneral || m == ModelMusic {
			return m
		}
	}
	return s.ActiveModel(ctx)
}

// ModelScore is how well a model's sense of "sounds like" matches the library: the share of
// each sampled track's nearest tracks (other albums) that share one of its genres, and that
// someone has played in the same sitting (when there's enough history). TextMatch is how
// well it finds music from words, which Muse and the moods depend on: of the tracks it
// ranks first for "<genre> music", the share that have that genre. (In testing, the music
// model's text side barely matched its audio side at all.)
type ModelScore struct {
	GenreMatch  float64 `json:"genreMatch"`
	PlayedMatch float64 `json:"playedMatch"` // 0 when there isn't enough listening history
	TextMatch   float64 `json:"textMatch"`   // 0 when it couldn't be measured
}

// Trial is a sound model being tried: its analysis progress and, once it covers the library,
// how it compares with the model in use.
type Trial struct {
	Model    string      `json:"model"`
	Analyzed int         `json:"analyzed"`
	Total    int         `json:"total"`
	Current  *ModelScore `json:"current,omitempty"`
	Tried    *ModelScore `json:"tried,omitempty"`
	At       string      `json:"at,omitempty"`
}

// Trial reports the model being tried (nil when the one chosen is the one in use).
func (s *Service) Trial(ctx context.Context) *Trial {
	active, want := s.ActiveModel(ctx), s.wanted(ctx)
	if want == active {
		return nil
	}
	t := &Trial{Model: want}
	var raw string
	if s.DB.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, trialKey).Scan(&raw) == nil {
		var done Trial
		if json.Unmarshal([]byte(raw), &done) == nil && done.Model == want {
			t = &done
		}
	}
	s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FILTER (WHERE next_model IS NOT NULL AND next_model LIKE ?), COUNT(*) FROM soundprint
		WHERE embedding IS NOT NULL`, trialPattern(want)).Scan(&t.Analyzed, &t.Total)
	return t
}

// trialPattern matches the analysis ids of a model by name in next_model (ids are the model's
// Hugging Face name and a version: the music model's contains "music").
func trialPattern(name string) string {
	if name == ModelMusic {
		return "%music%"
	}
	return "%general%"
}

// trialPass analyses the library with the model chosen in Settings when it isn't the one in
// use, and compares the two once it's done.
func (s *Service) trialPass(ctx context.Context, h Health) (string, error) {
	active, want := s.ActiveModel(ctx), s.wanted(ctx)
	id := h.ModelID(want)
	if want == active || id == "" {
		return "", nil
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT sp.item_id, f.path FROM soundprint sp JOIN media_files f ON f.id = sp.file_id AND f.available = 1
		WHERE sp.embedding IS NOT NULL AND (sp.next_model IS NULL OR sp.next_model != ?) ORDER BY sp.item_id`, id)
	if err != nil {
		return "", err
	}
	var todo []pending
	for rows.Next() {
		var p pending
		if rows.Scan(&p.itemID, &p.path) == nil {
			todo = append(todo, p)
		}
	}
	rows.Close()
	tried := 0
	if len(todo) > 0 {
		s.setPhase("Trying the "+want+" sound model", len(todo))
		const batch = 32
		for i := 0; i < len(todo); i += batch {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			if !s.Enabled() || s.wanted(ctx) != want {
				return "", nil
			}
			chunk := todo[i:min(i+batch, len(todo))]
			paths := make([]string, len(chunk))
			for j, p := range chunk {
				paths[j] = p.path
			}
			model, results, err := s.Client.Analyze(ctx, paths, want, false)
			if err != nil {
				return "", fmt.Errorf("trying the %s model after %d tracks: %w", want, tried, err)
			}
			for j, r := range results {
				if j >= len(chunk) {
					break
				}
				var emb any
				if r.Error == "" && len(r.Embedding) > 0 {
					emb = encode(r.Embedding)
				}
				if _, err := s.DB.ExecContext(ctx, `UPDATE soundprint SET next_model = ?, next_embedding = ? WHERE item_id = ?`, model, emb, chunk[j].itemID); err != nil {
					return "", err
				}
				tried++
			}
			s.setDone(min(i+batch, len(todo)))
		}
	}
	// Compared once, when the trial first covers the library (and again if tracks were added).
	if t := s.Trial(ctx); t != nil && (t.Tried == nil || tried > 0) && t.Analyzed >= t.Total {
		if err := s.compare(ctx, want); err != nil {
			return "", err
		}
		return fmt.Sprintf("tried the %s sound model on the library: it's ready to compare", want), nil
	}
	if tried > 0 {
		return fmt.Sprintf("tried the %s sound model on %d tracks", want, tried), nil
	}
	return "", nil
}

// compareSample is how many tracks the comparison looks at (their nearest tracks are found
// among the whole library).
const compareSample = 1500

// compare scores both models on the library and keeps the result for Settings.
func (s *Service) compare(ctx context.Context, want string) error {
	cur, next := map[int64][]float32{}, map[int64][]float32{}
	album := map[int64]int64{}
	rows, err := s.DB.QueryContext(ctx, `SELECT sp.item_id, sp.embedding, sp.next_embedding, COALESCE(i.parent_id, 0) FROM soundprint sp
		JOIN items i ON i.id = sp.item_id WHERE sp.embedding IS NOT NULL AND sp.next_embedding IS NOT NULL`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var id, alb int64
		var a, b []byte
		if rows.Scan(&id, &a, &b, &alb) == nil {
			cur[id], next[id], album[id] = decode(a), decode(b), alb
		}
	}
	rows.Close()
	genres := s.trackGenres(ctx)
	played := s.coplayed()
	t := Trial{Model: want, At: time.Now().UTC().Format(time.RFC3339)}
	cs, ts := scoreModels(cur, next, album, genres, played)
	names := topGenres(genres, textGenres)
	cs.TextMatch = s.textMatch(ctx, s.ActiveModel(ctx), cur, genres, names)
	ts.TextMatch = s.textMatch(ctx, want, next, genres, names)
	t.Current, t.Tried = &cs, &ts
	b, _ := json.Marshal(t)
	_, err = s.DB.ExecContext(ctx, `INSERT INTO settings(key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, trialKey, string(b))
	slog.Info("soundprint models compared", "tried", want, "current", cs, "trial", ts)
	return err
}

// scoreModels scores two sets of embeddings of the same tracks on the same sample.
func scoreModels(cur, next map[int64][]float32, album map[int64]int64, genres map[int64]map[string]bool, played map[int64]map[int64]float64) (ModelScore, ModelScore) {
	ids := make([]int64, 0, len(cur))
	for id := range cur {
		if next[id] != nil {
			ids = append(ids, id)
		}
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	r := rand.New(rand.NewPCG(1, 2)) // the same sample each time
	sample := append([]int64{}, ids...)
	r.Shuffle(len(sample), func(i, j int) { sample[i], sample[j] = sample[j], sample[i] })
	sample = sample[:min(len(sample), compareSample)]
	return scoreModel(cur, ids, sample, album, genres, played), scoreModel(next, ids, sample, album, genres, played)
}

// neighbours is how many nearest tracks a sampled track is judged by.
const neighbours = 10

func scoreModel(vecs map[int64][]float32, ids, sample []int64, album map[int64]int64, genres map[int64]map[string]bool, played map[int64]map[int64]float64) ModelScore {
	var genreHits, genreAll, playHits, playAll int
	type near struct {
		id  int64
		sim float64
	}
	for _, q := range sample {
		qv := vecs[q]
		best := make([]near, 0, neighbours+1)
		for _, id := range ids {
			if id == q || (album[id] == album[q] && album[q] != 0) {
				continue
			}
			sim := dot(qv, vecs[id])
			if len(best) == neighbours && sim <= best[len(best)-1].sim {
				continue
			}
			at := sort.Search(len(best), func(i int) bool { return best[i].sim < sim })
			best = append(best, near{})
			copy(best[at+1:], best[at:])
			best[at] = near{id, sim}
			if len(best) > neighbours {
				best = best[:neighbours]
			}
		}
		if g := genres[q]; len(g) > 0 {
			for _, n := range best {
				ng := genres[n.id]
				if len(ng) == 0 {
					continue
				}
				genreAll++
				for x := range g {
					if ng[x] {
						genreHits++
						break
					}
				}
			}
		}
		if p := played[q]; len(p) >= 3 {
			for _, n := range best {
				playAll++
				if p[n.id] > 0 {
					playHits++
				}
			}
		}
	}
	var sc ModelScore
	if genreAll > 0 {
		sc.GenreMatch = float64(genreHits) / float64(genreAll)
	}
	if playAll >= 200 { // enough history to say something
		sc.PlayedMatch = float64(playHits) / float64(playAll)
	}
	return sc
}

// textGenres is how many of the library's commonest genres TextMatch asks for; textTop how
// many of the tracks ranked first for each it judges.
const (
	textGenres = 15
	textTop    = 100
)

// topGenres are the commonest genres among the tracks (each on at least textTop of them).
func topGenres(genres map[int64]map[string]bool, n int) []string {
	count := map[string]int{}
	for _, gs := range genres {
		for g := range gs {
			count[g]++
		}
	}
	var out []string
	for g, c := range count {
		if c >= textTop {
			out = append(out, g)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		return count[out[i]] > count[out[j]] || (count[out[i]] == count[out[j]] && out[i] < out[j])
	})
	return out[:min(n, len(out))]
}

// textMatch asks a model for "<genre> music" for each genre and returns the share of the
// tracks it ranks first that have the genre (0 when the sidecar can't embed the words).
func (s *Service) textMatch(ctx context.Context, model string, vecs map[int64][]float32, genres map[int64]map[string]bool, names []string) float64 {
	if len(names) == 0 || s.Client == nil {
		return 0
	}
	texts := make([]string, len(names))
	for i, g := range names {
		texts[i] = g + " music"
	}
	embs, err := s.Client.EmbedText(ctx, texts, model)
	if err != nil || len(embs) != len(names) {
		slog.Warn("soundprint model comparison: words", "model", model, "err", err)
		return 0
	}
	hits, all := 0, 0
	for i, g := range names {
		q := normalize(embs[i])
		ranked := make([]Scored, 0, len(vecs))
		for id, v := range vecs {
			if len(genres[id]) > 0 { // tracks without genres can't be judged
				ranked = append(ranked, Scored{&Track{ItemID: id}, dot(q, v)})
			}
		}
		sort.Slice(ranked, func(a, b int) bool {
			return ranked[a].Score > ranked[b].Score || (ranked[a].Score == ranked[b].Score && ranked[a].ItemID < ranked[b].ItemID)
		})
		for _, sc := range ranked[:min(textTop, len(ranked))] {
			all++
			if genres[sc.ItemID][g] {
				hits++
			}
		}
	}
	if all == 0 {
		return 0
	}
	return float64(hits) / float64(all)
}

// trackGenres reads each track's genres (its own, its album's and its artist's).
func (s *Service) trackGenres(ctx context.Context) map[int64]map[string]bool {
	out := map[int64]map[string]bool{}
	rows, err := s.DB.QueryContext(ctx, `SELECT i.id, LOWER(t.name) FROM items i
		JOIN item_tags it ON it.item_id IN (i.id, i.parent_id, i.grandparent_id) JOIN tags t ON t.id = it.tag_id AND t.kind = 'genre'
		WHERE i.type = 'track'`)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var g string
		if rows.Scan(&id, &g) == nil {
			if out[id] == nil {
				out[id] = map[string]bool{}
			}
			out[id][g] = true
		}
	}
	return out
}

// SwitchModel makes the tried model the one in use: its embeddings replace the current ones
// (tracks it couldn't analyse are analysed again), the index reloads and the day's mixes are
// made again.
func (s *Service) SwitchModel(ctx context.Context) error {
	t := s.Trial(ctx)
	if t == nil || t.Tried == nil {
		return ErrNoTrial
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, q := range []string{
		`UPDATE soundprint SET model = next_model, embedding = next_embedding WHERE next_embedding IS NOT NULL`,
		// Not analysed by the new model (failed, or added since): analysed again by the next run.
		`UPDATE soundprint SET embedding = NULL, error = NULL, model = '' WHERE next_embedding IS NULL AND embedding IS NOT NULL`,
		`UPDATE soundprint SET next_model = NULL, next_embedding = NULL`,
	} {
		if _, err := tx.ExecContext(ctx, q); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO settings(key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`, activeKey, t.Model); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM settings WHERE key = ?`, trialKey); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	slog.Info("soundprint switched sound model", "model", t.Model)
	forgetMixes()
	return s.Index.Load(ctx, s.DB)
}

// EndTrial drops a trial (the admin kept the current model): Settings is set back by the caller.
func (s *Service) EndTrial(ctx context.Context) error {
	if _, err := s.DB.ExecContext(ctx, `UPDATE soundprint SET next_model = NULL, next_embedding = NULL WHERE next_model IS NOT NULL`); err != nil {
		return err
	}
	_, err := s.DB.ExecContext(ctx, `DELETE FROM settings WHERE key = ?`, trialKey)
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return err
}
