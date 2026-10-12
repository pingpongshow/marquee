package items

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"marquee/internal/origins"
)

// ErrEmbedderUnavailable means the sidecar that embeds text isn't reachable.
var ErrEmbedderUnavailable = errors.New("the analysis service isn't running")

// TextEmbedder embeds text with the Soundprint sidecar's text model (soundprint.Client.EmbedDocs).
// kind is "doc" for descriptions and "query" for prompts. An empty texts list only
// reports the model id.
type TextEmbedder interface {
	EmbedDocs(ctx context.Context, texts []string, kind string) (string, [][]float32, error)
}

// VideoVec is one embedded movie or show in memory.
type VideoVec struct {
	ItemID, LibraryID int64
	Type              string // movie or show
	Vec               []float32
}

// VideoIndex holds the text embeddings of every movie and show (USER-15, USER-16) for fast
// cosine search: ~3,400 items of 768 floats is ~10 MB and a full scan takes about a
// millisecond.
type VideoIndex struct {
	mu    sync.RWMutex
	items map[int64]*VideoVec
	model string
}

func NewVideoIndex() *VideoIndex { return &VideoIndex{items: map[int64]*VideoVec{}} }

func encodeVec(v []float32) []byte {
	b := make([]byte, 4*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint32(b[4*i:], math.Float32bits(x))
	}
	return b
}

func decodeVec(b []byte) []float32 {
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:]))
	}
	return v
}

func normalizeVec(v []float32) []float32 {
	var n float64
	for _, x := range v {
		n += float64(x) * float64(x)
	}
	n = math.Sqrt(n)
	if n == 0 {
		return v
	}
	out := make([]float32, len(v))
	for i, x := range v {
		out[i] = float32(float64(x) / n)
	}
	return out
}

func dotVec(a, b []float32) float64 {
	var s float32
	n := min(len(a), len(b))
	for i := 0; i < n; i++ {
		s += a[i] * b[i]
	}
	return float64(s)
}

// Load reads the embeddings of the newest model from the database (rows of an older model
// are re-embedded by the task and can't be compared with new queries meanwhile).
func (x *VideoIndex) Load(ctx context.Context, db *sql.DB) error {
	var model string
	err := db.QueryRowContext(ctx, `SELECT model FROM item_embeddings ORDER BY embedded_at DESC LIMIT 1`).Scan(&model)
	if errors.Is(err, sql.ErrNoRows) {
		x.Replace("", nil)
		return nil
	}
	if err != nil {
		return err
	}
	rows, err := db.QueryContext(ctx, `SELECT e.item_id, i.library_id, i.type, e.vector FROM item_embeddings e
		JOIN items i ON i.id = e.item_id WHERE e.model = ? AND i.extra_type IS NULL`, model)
	if err != nil {
		return err
	}
	defer rows.Close()
	var list []*VideoVec
	for rows.Next() {
		v := &VideoVec{}
		var b []byte
		if err := rows.Scan(&v.ItemID, &v.LibraryID, &v.Type, &b); err != nil {
			return err
		}
		v.Vec = decodeVec(b)
		list = append(list, v)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	x.Replace(model, list)
	return nil
}

// Replace swaps the whole index (tests inject vectors this way).
func (x *VideoIndex) Replace(model string, list []*VideoVec) {
	m := make(map[int64]*VideoVec, len(list))
	for _, v := range list {
		m[v.ItemID] = v
	}
	x.mu.Lock()
	x.items, x.model = m, model
	x.mu.Unlock()
}

// Put adds or replaces one item; a different model starts the index over.
func (x *VideoIndex) Put(model string, v *VideoVec) {
	x.mu.Lock()
	if model != x.model {
		x.items, x.model = map[int64]*VideoVec{}, model
	}
	x.items[v.ItemID] = v
	x.mu.Unlock()
}

func (x *VideoIndex) Get(id int64) *VideoVec {
	if x == nil {
		return nil
	}
	x.mu.RLock()
	defer x.mu.RUnlock()
	return x.items[id]
}

func (x *VideoIndex) Len() int {
	if x == nil {
		return 0
	}
	x.mu.RLock()
	defer x.mu.RUnlock()
	return len(x.items)
}

func (x *VideoIndex) Model() string {
	x.mu.RLock()
	defer x.mu.RUnlock()
	return x.model
}

// Scored is an item with its cosine similarity to a query.
type Scored struct {
	ItemID int64
	Score  float64
}

// Nearest ranks the items keep accepts (nil = all) by cosine to q, best first, n at most
// (0 = all).
func (x *VideoIndex) Nearest(q []float32, keep func(*VideoVec) bool, n int) []Scored {
	x.mu.RLock()
	out := make([]Scored, 0, len(x.items))
	for _, v := range x.items {
		if keep == nil || keep(v) {
			out = append(out, Scored{v.ItemID, dotVec(q, v.Vec)})
		}
	}
	x.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].ItemID < out[j].ItemID
	})
	if n > 0 && len(out) > n {
		out = out[:n]
	}
	return out
}

// Centroid is the normalized weighted mean of the given items' vectors (nil when none
// are embedded).
func (x *VideoIndex) Centroid(ids []int64, weights []float64) []float32 {
	x.mu.RLock()
	defer x.mu.RUnlock()
	var sum []float64
	for i, id := range ids {
		v := x.items[id]
		if v == nil {
			continue
		}
		if sum == nil {
			sum = make([]float64, len(v.Vec))
		}
		w := 1.0
		if i < len(weights) {
			w = weights[i]
		}
		for j := 0; j < len(sum) && j < len(v.Vec); j++ {
			sum[j] += w * float64(v.Vec[j])
		}
	}
	if sum == nil {
		return nil
	}
	out := make([]float32, len(sum))
	for i, s := range sum {
		out[i] = float32(s)
	}
	return normalizeVec(out)
}

// ---------- the embedding task ----------

// Embedder keeps item_embeddings and the index up to date. It runs as a maintenance task
// after library scans and nightly; it's resumable (finished batches are saved) and only
// re-embeds items whose text or model changed.
type Embedder struct {
	DB     *sql.DB
	Index  *VideoIndex
	Client TextEmbedder
	// Pace waits this long between batches, so a re-index doesn't hog the GPU.
	Pace time.Duration

	mu          sync.Mutex
	done, total int
}

// embedBatch is how many descriptions go to the sidecar at once.
const embedBatch = 32

func (e *Embedder) Progress() (done, total int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.done, e.total
}

func (e *Embedder) setProgress(done, total int) {
	e.mu.Lock()
	e.done, e.total = done, total
	e.mu.Unlock()
}

// Query embeds a search prompt. It returns ErrEmbedderUnavailable when the sidecar is
// down.
func (e *Embedder) Query(ctx context.Context, text string) ([]float32, string, error) {
	if e == nil || e.Client == nil {
		return nil, "", ErrEmbedderUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	model, vs, err := e.Client.EmbedDocs(ctx, []string{text}, "query")
	if err != nil || len(vs) == 0 {
		return nil, "", fmt.Errorf("%w: %v", ErrEmbedderUnavailable, err)
	}
	return normalizeVec(vs[0]), model, nil
}

type embedDoc struct {
	id, lib int64
	typ     string
	text    string
	hash    string
}

// Run embeds every movie and show whose description changed since it was last embedded.
// Items added while it runs (another library's scan finishing) are picked up by a further
// pass.
func (e *Embedder) Run(ctx context.Context) (string, error) {
	model, _, err := e.Client.EmbedDocs(ctx, nil, "doc")
	if err != nil {
		// Not a failure: servers without the sidecar simply don't get Muse for movies.
		return "The analysis service isn't running; Muse for movies and recommendations wait for it", nil
	}
	defer e.setProgress(0, 0)
	start := time.Now()
	done, total := 0, 0
	for pass := 0; pass < 5; pass++ {
		n, all, err := e.pass(ctx, model)
		done, total = done+n, all
		if err != nil {
			return "", fmt.Errorf("after %d: %w", done, err)
		}
		if n == 0 {
			break
		}
	}
	if done == 0 {
		if e.Index.Len() != total || e.Index.Model() != model {
			if err := e.Index.Load(ctx, e.DB); err != nil {
				return "", err
			}
		}
		return fmt.Sprintf("All %d movies and shows are indexed", total), nil
	}
	if err := e.Index.Load(ctx, e.DB); err != nil {
		return "", err
	}
	took := time.Since(start).Round(time.Second)
	slog.Info("embedding finished", "items", done, "took", took)
	return fmt.Sprintf("Indexed %d movies and shows in %s", done, took), nil
}

// pass embeds what's pending now; it returns how many it embedded and how many movies and
// shows there are.
func (e *Embedder) pass(ctx context.Context, model string) (int, int, error) {
	docs, err := videoDocs(ctx, e.DB)
	if err != nil {
		return 0, 0, err
	}
	have := map[int64]string{}
	err = eachRow(ctx, e.DB, `SELECT item_id, model || ' ' || text_hash FROM item_embeddings`, nil, func(r *sql.Rows) error {
		var id int64
		var key string
		if err := r.Scan(&id, &key); err != nil {
			return err
		}
		have[id] = key
		return nil
	})
	if err != nil {
		return 0, 0, err
	}
	var todo []embedDoc
	for _, d := range docs {
		if have[d.id] != model+" "+d.hash {
			todo = append(todo, d)
		}
	}
	if len(todo) == 0 {
		return 0, len(docs), nil
	}
	slog.Info("embedding movies and shows", "items", len(todo), "model", model)
	for i := 0; i < len(todo); i += embedBatch {
		if ctx.Err() != nil {
			return i, len(docs), ctx.Err()
		}
		e.setProgress(i, len(todo))
		if i > 0 && e.Pace > 0 {
			select {
			case <-ctx.Done():
				return i, len(docs), ctx.Err()
			case <-time.After(e.Pace):
			}
		}
		chunk := todo[i:min(i+embedBatch, len(todo))]
		texts := make([]string, len(chunk))
		for j, d := range chunk {
			texts[j] = d.text
		}
		m, vecs, err := e.Client.EmbedDocs(ctx, texts, "doc")
		if err != nil {
			return i, len(docs), err
		}
		if err := e.save(ctx, m, chunk, vecs); err != nil {
			return i, len(docs), err
		}
	}
	return len(todo), len(docs), nil
}

func (e *Embedder) save(ctx context.Context, model string, chunk []embedDoc, vecs [][]float32) error {
	tx, err := e.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for j, d := range chunk {
		v := normalizeVec(vecs[j])
		if _, err := tx.ExecContext(ctx, `INSERT INTO item_embeddings (item_id, model, vector, text_hash) VALUES (?, ?, ?, ?)
			ON CONFLICT (item_id) DO UPDATE SET model = excluded.model, vector = excluded.vector, text_hash = excluded.text_hash,
			embedded_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')`, d.id, model, encodeVec(v), d.hash); err != nil {
			if strings.Contains(err.Error(), "FOREIGN KEY") {
				continue // deleted meanwhile
			}
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	for j, d := range chunk {
		e.Index.Put(model, &VideoVec{ItemID: d.id, LibraryID: d.lib, Type: d.typ, Vec: normalizeVec(vecs[j])})
	}
	return nil
}

// docVersion is part of every description's hash: raising it re-embeds everything in the
// background (the text changing does the same for one item).
const docVersion = 2

// docMeta is what one movie's or show's description is built from.
type docMeta struct {
	typ, title, tagline, summary, studio string
	year                                 int
	genres, keywords, directors, cast    []string
	countries                            []string // ISO codes
	language, contentRating              string
	rtCritic, metacritic                 int     // 0–100, -1 unknown
	audience                             float64 // 0–10
	collection                           string
}

// videoDocs builds the description embedded for every movie and show (not extras).
func videoDocs(ctx context.Context, db *sql.DB) ([]embedDoc, error) {
	type meta struct {
		embedDoc
		docMeta
	}
	byID := map[int64]*meta{}
	var order []int64
	err := eachRow(ctx, db, `SELECT id, library_id, type, title, COALESCE(year, 0), COALESCE(tagline, ''), COALESCE(summary, ''), COALESCE(studio, ''),
			COALESCE(original_language, ''), COALESCE(content_rating, ''), COALESCE(rt_critic, -1), COALESCE(metacritic, -1), COALESCE(audience_rating, imdb_rating, 0)
		FROM items WHERE type IN ('movie', 'show') AND extra_type IS NULL ORDER BY id`, nil, func(r *sql.Rows) error {
		m := &meta{}
		if err := r.Scan(&m.id, &m.lib, &m.embedDoc.typ, &m.title, &m.year, &m.tagline, &m.summary, &m.studio,
			&m.language, &m.contentRating, &m.rtCritic, &m.metacritic, &m.audience); err != nil {
			return err
		}
		m.docMeta.typ = m.embedDoc.typ
		byID[m.id] = m
		order = append(order, m.id)
		return nil
	})
	if err != nil {
		return nil, err
	}
	err = eachRow(ctx, db, `SELECT it.item_id, t.kind, t.name FROM item_tags it JOIN tags t ON t.id = it.tag_id AND t.kind IN ('genre', 'keyword', 'country')
		JOIN items i ON i.id = it.item_id AND i.type IN ('movie', 'show') ORDER BY it.item_id, t.kind, t.id`, nil, func(r *sql.Rows) error {
		var id int64
		var kind, name string
		if err := r.Scan(&id, &kind, &name); err != nil {
			return err
		}
		m := byID[id]
		switch {
		case m == nil:
		case kind == "genre":
			m.genres = append(m.genres, name)
		case kind == "keyword":
			m.keywords = append(m.keywords, name)
		case kind == "country":
			m.countries = append(m.countries, name)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	err = eachRow(ctx, db, `SELECT c.item_id, c.role, p.name FROM credits c JOIN people p ON p.id = c.person_id
		JOIN items i ON i.id = c.item_id AND i.type IN ('movie', 'show')
		WHERE c.role IN ('director', 'creator') OR (c.role = 'actor' AND c.ord < 8) ORDER BY c.item_id, c.ord`, nil, func(r *sql.Rows) error {
		var id int64
		var role, name string
		if err := r.Scan(&id, &role, &name); err != nil {
			return err
		}
		m := byID[id]
		switch {
		case m == nil:
		case role == "actor" && len(m.cast) < 3:
			m.cast = append(m.cast, name)
		case role != "actor" && len(m.directors) < 2:
			m.directors = append(m.directors, name)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	err = eachRow(ctx, db, `SELECT ci.item_id, c.title FROM collection_items ci JOIN items c ON c.id = ci.collection_id
		ORDER BY ci.item_id, c.id`, nil, func(r *sql.Rows) error {
		var id int64
		var title string
		if err := r.Scan(&id, &title); err != nil {
			return err
		}
		if m := byID[id]; m != nil && m.collection == "" {
			m.collection = title
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	out := make([]embedDoc, 0, len(order))
	for _, id := range order {
		m := byID[id]
		m.text = DocText(m.docMeta)
		sum := sha256.Sum256([]byte(fmt.Sprintf("v%d\n%s", docVersion, m.text)))
		m.hash = hex.EncodeToString(sum[:16])
		out = append(out, m.embedDoc)
	}
	return out, nil
}

// docKeywords bounds the keywords in a description (TMDB lists the most relevant first).
const docKeywords = 12

// DocText is the description of one movie or show. It leads with what the title is about
// (genres, keywords, tagline, plot), then who made it, a few lead actors, where it's from
// and how it's rated: similarity should follow the story, not shared actors or studios
// (people are searched through the credits filter instead).
func DocText(m docMeta) string {
	var b strings.Builder
	b.WriteString(m.title)
	if m.year > 0 {
		fmt.Fprintf(&b, " (%d)", m.year)
	}
	b.WriteString(map[string]string{"movie": ". A film.", "show": ". A TV series."}[m.typ])
	sentence := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		b.WriteString(" " + s)
		if !strings.HasSuffix(s, ".") && !strings.HasSuffix(s, "!") && !strings.HasSuffix(s, "?") {
			b.WriteString(".")
		}
	}
	if len(m.genres) > 0 {
		sentence(strings.Join(m.genres, ", "))
	}
	if kw := m.keywords; len(kw) > 0 {
		if len(kw) > docKeywords {
			kw = kw[:docKeywords]
		}
		sentence("Themes: " + strings.Join(kw, ", "))
	}
	sentence(m.tagline)
	sentence(m.summary)
	if len(m.directors) > 0 {
		verb := "Directed by "
		if m.typ == "show" {
			verb = "Created by "
		}
		sentence(verb + strings.Join(m.directors, " and "))
	}
	if len(m.cast) > 0 {
		sentence("Starring " + strings.Join(m.cast, ", "))
	}
	var from []string
	for _, c := range m.countries {
		if n := origins.CountryName(c); n != "" {
			from = append(from, n)
		}
	}
	origin := ""
	if len(from) > 0 {
		origin = "From " + strings.Join(from, " and ")
	}
	if l := origins.LanguageName(m.language); l != "" && m.language != "en" {
		if origin == "" {
			origin = "In " + l
		} else {
			origin += ", in " + l
		}
	}
	sentence(origin)
	if m.typ == "show" && m.studio != "" {
		sentence("On " + m.studio) // the network says something about a show; a film's studio rarely does
	}
	if m.collection != "" {
		sentence("Part of " + m.collection)
	}
	if m.contentRating != "" {
		sentence("Rated " + m.contentRating)
	}
	var ratings []string
	switch {
	case m.rtCritic >= 0:
		ratings = append(ratings, fmt.Sprintf("critics %d%%", m.rtCritic))
	case m.metacritic >= 0:
		ratings = append(ratings, fmt.Sprintf("Metacritic %d", m.metacritic))
	}
	if m.audience > 0 {
		ratings = append(ratings, fmt.Sprintf("audiences %.1f/10", m.audience))
	}
	if len(ratings) > 0 {
		r := strings.Join(ratings, ", ")
		sentence(strings.ToUpper(r[:1]) + r[1:])
	}
	return b.String()
}
