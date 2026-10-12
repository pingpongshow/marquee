package muse

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"log/slog"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"marquee/internal/ai"
	"marquee/internal/lyrics"
)

// Embedder is the embedding part of ai.Client.
type Embedder interface {
	Ready() bool
	EmbedModel() string
	Embed(ctx context.Context, texts []string) ([][]float32, error)
}

// LyricThemes indexes the lyrics of tracks with the AI's embedding model, so music Muse can
// favour songs whose lyrics match a theme ("songs about the ocean"). It runs as a paced
// background task while AI and lyric themes are on.
type LyricThemes struct {
	DB *sql.DB
	AI Embedder
	// Enabled reports AI on and the lyric themes switch on.
	Enabled func() bool
	// Local caches lyrics from tags and .lrc files for tracks never looked up (lyrics.Service.CacheLocal).
	Local func(ctx context.Context, itemID int64) error
	// Pace waits between batches so indexing doesn't hog the GPU.
	Pace time.Duration

	mu          sync.RWMutex
	model       string
	vecs        map[int64][]float32
	running     bool
	done, total int
}

// lyricChars is roughly the first 400 tokens of lyrics.
const (
	lyricChars = 1600
	lyricBatch = 16
	// localPerRun bounds how many never-looked-up tracks one run checks for local lyrics.
	localPerRun = 20000
)

func (l *LyricThemes) modelID() string { return ai.ModelID(l.AI.EmbedModel()) }

// Load reads the vectors of the current model.
func (l *LyricThemes) Load(ctx context.Context) error {
	model := l.modelID()
	vecs := map[int64][]float32{}
	rows, err := l.DB.QueryContext(ctx, `SELECT item_id, vector FROM lyric_embeddings WHERE model = ?`, model)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var b []byte
		if err := rows.Scan(&id, &b); err != nil {
			return err
		}
		vecs[id] = decodeVec(b)
	}
	l.mu.Lock()
	l.model, l.vecs = model, vecs
	l.mu.Unlock()
	return rows.Err()
}

// LyricStatus is the indexing progress shown in Settings → AI.
type LyricStatus struct {
	Indexed, WithLyrics, Tracks int
	Running                     bool
}

func (l *LyricThemes) Status(ctx context.Context) LyricStatus {
	var st LyricStatus
	l.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM items WHERE type = 'track'`).Scan(&st.Tracks)
	l.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM lyrics l JOIN items i ON i.id = l.item_id AND i.type = 'track' WHERE l.text != ''`).Scan(&st.WithLyrics)
	l.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM lyric_embeddings WHERE model = ?`, l.modelID()).Scan(&st.Indexed)
	l.mu.RLock()
	st.Running = l.running
	l.mu.RUnlock()
	return st
}

// Progress is for the task list.
func (l *LyricThemes) Progress() (int, int) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.done, l.total
}

// Rebuild forgets the index; the next run embeds everything again.
func (l *LyricThemes) Rebuild(ctx context.Context) error {
	if _, err := l.DB.ExecContext(ctx, `DELETE FROM lyric_embeddings`); err != nil {
		return err
	}
	l.mu.Lock()
	l.vecs = map[int64][]float32{}
	l.mu.Unlock()
	return nil
}

type lyricDoc struct {
	id   int64
	text string
	hash string
}

// Run indexes new and changed lyrics (and everything after a model change).
func (l *LyricThemes) Run(ctx context.Context) (string, error) {
	if l.Enabled == nil || !l.Enabled() {
		return "Lyric themes are off", nil
	}
	if !l.AI.Ready() {
		return "The AI service isn't available; lyric themes wait for it", nil
	}
	l.mu.Lock()
	if l.running {
		l.mu.Unlock()
		return "Already running", nil
	}
	l.running = true
	l.mu.Unlock()
	defer func() {
		l.mu.Lock()
		l.running, l.done, l.total = false, 0, 0
		l.mu.Unlock()
	}()
	model := l.modelID()
	l.mu.RLock()
	stale := l.model != model
	l.mu.RUnlock()
	if stale {
		if err := l.Load(ctx); err != nil {
			return "", err
		}
	}
	found := 0
	if l.Local != nil {
		var ids []int64
		rows, err := l.DB.QueryContext(ctx, `SELECT i.id FROM items i WHERE i.type = 'track'
			AND NOT EXISTS (SELECT 1 FROM lyrics x WHERE x.item_id = i.id) ORDER BY i.id LIMIT ?`, localPerRun)
		if err != nil {
			return "", err
		}
		for rows.Next() {
			var id int64
			if rows.Scan(&id) == nil {
				ids = append(ids, id)
			}
		}
		rows.Close()
		for _, id := range ids {
			if ctx.Err() != nil {
				return "", ctx.Err()
			}
			if l.Local(ctx, id) == nil {
				found++
			}
		}
	}
	docs, err := l.pending(ctx, model)
	if err != nil {
		return "", err
	}
	if len(docs) == 0 {
		return fmt.Sprintf("All %d tracks with lyrics are indexed", l.count()), nil
	}
	start := time.Now()
	slog.Info("indexing lyrics", "tracks", len(docs), "model", model)
	for i := 0; i < len(docs); i += lyricBatch {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if !l.Enabled() {
			return fmt.Sprintf("Stopped (turned off) after %d tracks", i), nil
		}
		if i > 0 && l.Pace > 0 {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(l.Pace):
			}
		}
		l.mu.Lock()
		l.done, l.total = i, len(docs)
		l.mu.Unlock()
		chunk := docs[i:min(i+lyricBatch, len(docs))]
		texts := make([]string, len(chunk))
		prefix := ai.DocPrefix(l.AI.EmbedModel())
		for j, d := range chunk {
			texts[j] = prefix + d.text
		}
		vecs, err := l.AI.Embed(ctx, texts)
		if err != nil {
			return "", fmt.Errorf("after %d tracks: %w", i, err)
		}
		if err := l.save(ctx, model, chunk, vecs); err != nil {
			return "", err
		}
	}
	return fmt.Sprintf("Indexed the lyrics of %d tracks in %s", len(docs), time.Since(start).Round(time.Second)), nil
}

func (l *LyricThemes) count() int {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return len(l.vecs)
}

// pending lists lyrics whose text or model changed since they were embedded.
func (l *LyricThemes) pending(ctx context.Context, model string) ([]lyricDoc, error) {
	have := map[int64]string{}
	rows, err := l.DB.QueryContext(ctx, `SELECT item_id, model || ' ' || text_hash FROM lyric_embeddings`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int64
		var k string
		if rows.Scan(&id, &k) == nil {
			have[id] = k
		}
	}
	rows.Close()
	rows, err = l.DB.QueryContext(ctx, `SELECT l.item_id, l.text FROM lyrics l JOIN items i ON i.id = l.item_id AND i.type = 'track'
		WHERE l.text != '' ORDER BY l.item_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []lyricDoc
	for rows.Next() {
		var d lyricDoc
		if err := rows.Scan(&d.id, &d.text); err != nil {
			return nil, err
		}
		d.text = LyricText(d.text)
		if d.text == "" {
			continue
		}
		sum := sha256.Sum256([]byte(d.text))
		d.hash = hex.EncodeToString(sum[:16])
		if have[d.id] != model+" "+d.hash {
			out = append(out, d)
		}
	}
	return out, rows.Err()
}

// LyricText is the part of a song's lyrics that's embedded: the plain lines, up to about
// 400 tokens, cut at a line break.
func LyricText(raw string) string {
	t := lyrics.PlainText(raw)
	if len(t) <= lyricChars {
		return t
	}
	cut := strings.LastIndexByte(t[:lyricChars], '\n')
	if cut < lyricChars/2 {
		cut = lyricChars
		for cut > 0 && t[cut]&0xC0 == 0x80 { // don't split a UTF-8 character
			cut--
		}
	}
	return t[:cut]
}

func (l *LyricThemes) save(ctx context.Context, model string, chunk []lyricDoc, vecs [][]float32) error {
	tx, err := l.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	norm := make([][]float32, len(chunk))
	for j, d := range chunk {
		norm[j] = normalize(vecs[j])
		if _, err := tx.ExecContext(ctx, `INSERT INTO lyric_embeddings (item_id, model, vector, text_hash) VALUES (?, ?, ?, ?)
			ON CONFLICT (item_id) DO UPDATE SET model = excluded.model, vector = excluded.vector, text_hash = excluded.text_hash,
			embedded_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')`, d.id, model, encodeVec(norm[j]), d.hash); err != nil {
			if strings.Contains(err.Error(), "FOREIGN KEY") {
				continue
			}
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	l.mu.Lock()
	if l.vecs == nil || l.model != model {
		l.vecs, l.model = map[int64][]float32{}, model
	}
	for j, d := range chunk {
		l.vecs[d.id] = norm[j]
	}
	l.mu.Unlock()
	return nil
}

// Boost scores tracks by how well their lyrics match theme, for soundprint.Options.Boost.
// nil when lyric themes are off, the AI is unavailable or nothing is indexed.
func (l *LyricThemes) Boost(ctx context.Context, theme string) map[int64]float64 {
	theme = strings.TrimSpace(theme)
	if l == nil || theme == "" || l.Enabled == nil || !l.Enabled() || !l.AI.Ready() {
		return nil
	}
	l.mu.RLock()
	ready := len(l.vecs) > 0 && l.model == l.modelID()
	l.mu.RUnlock()
	if !ready {
		return nil
	}
	vs, err := l.AI.Embed(ctx, []string{ai.QueryPrefix(l.AI.EmbedModel()) + "song lyrics about " + theme})
	if err != nil || len(vs) == 0 {
		return nil
	}
	q := normalize(vs[0])
	l.mu.RLock()
	sims := make(map[int64]float64, len(l.vecs))
	for id, v := range l.vecs {
		sims[id] = dot(q, v)
	}
	l.mu.RUnlock()
	return ThemeBoost(sims)
}

// Theme scoring: only the best-matching share of the indexed lyrics gets a bonus, rising
// from 0 at that share's edge to themeWeight for the closest match. Tracks without lyrics
// simply get none.
const (
	themeWeight = 0.35
	themeShare  = 0.15
)

// ThemeBoost turns lyric similarities into bonuses.
func ThemeBoost(sims map[int64]float64) map[int64]float64 {
	if len(sims) == 0 {
		return nil
	}
	all := make([]float64, 0, len(sims))
	for _, s := range sims {
		all = append(all, s)
	}
	sort.Float64s(all)
	top := all[len(all)-1]
	floor := all[int(float64(len(all)-1)*(1-themeShare))]
	if len(all) < 8 {
		floor = all[0]
	}
	if top <= floor {
		return nil
	}
	out := map[int64]float64{}
	for id, s := range sims {
		if s > floor {
			out[id] = themeWeight * (s - floor) / (top - floor)
		}
	}
	return out
}

// ---------- vectors ----------

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

func normalize(v []float32) []float32 {
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

func dot(a, b []float32) float64 {
	var s float32
	for i := 0; i < min(len(a), len(b)); i++ {
		s += a[i] * b[i]
	}
	return float64(s)
}
