package muse

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"marquee/internal/ai"
	"marquee/internal/origins"
)

// MusicDocs indexes descriptions of artists and albums (name, type, origin, years, genres,
// styles, bio; title, artist, year, type, styles, label) with the AI's embedding model, so
// music Muse can match descriptive requests ("Icelandic female singer-songwriters", "90s
// Manchester bands") to artists and albums. It runs as a paced background task while AI is
// on, and re-embeds a description when it changes.
type MusicDocs struct {
	DB *sql.DB
	AI Embedder
	// Enabled reports AI on.
	Enabled func() bool
	// Pace waits between batches so indexing doesn't hog the GPU.
	Pace time.Duration

	mu          sync.RWMutex
	model       string
	artists     map[int64][]float32
	albums      map[int64][]float32
	running     bool
	done, total int
}

// musicDocVersion is part of every description's hash: raising it re-embeds them all.
const (
	musicDocVersion = 1
	musicDocBatch   = 32
	bioChars        = 700
)

func (m *MusicDocs) modelID() string { return ai.ModelID(m.AI.EmbedModel()) }

// Load reads the vectors of the current model.
func (m *MusicDocs) Load(ctx context.Context) error {
	model := m.modelID()
	artists, albums := map[int64][]float32{}, map[int64][]float32{}
	rows, err := m.DB.QueryContext(ctx, `SELECT e.item_id, i.type, e.vector FROM music_embeddings e JOIN items i ON i.id = e.item_id WHERE e.model = ?`, model)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var typ string
		var b []byte
		if err := rows.Scan(&id, &typ, &b); err != nil {
			return err
		}
		if typ == "artist" {
			artists[id] = decodeVec(b)
		} else {
			albums[id] = decodeVec(b)
		}
	}
	m.mu.Lock()
	m.model, m.artists, m.albums = model, artists, albums
	m.mu.Unlock()
	return rows.Err()
}

// Progress is for the task list.
func (m *MusicDocs) Progress() (int, int) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.done, m.total
}

// ArtistFacts is what an artist's description is built from.
type ArtistFacts struct {
	Name, Type, Gender, Country, Area, BeginArea string
	BeginYear, EndYear                           int
	Genres, Styles                               []string
	Summary                                      string
}

// AlbumFacts is what an album's description is built from.
type AlbumFacts struct {
	Title, Artist, Type, Label string
	Year                       int
	Genres, Styles             []string
}

// ArtistDoc describes an artist in plain sentences.
func ArtistDoc(f ArtistFacts) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(f.Name) + ".")
	kind := map[string]string{"group": "A band", "orchestra": "An orchestra", "choir": "A choir", "person": "A solo artist"}[f.Type]
	if f.Type == "person" && (f.Gender == "female" || f.Gender == "male") {
		kind = "A " + f.Gender + " solo artist"
	}
	var place []string
	if f.BeginArea != "" {
		place = append(place, f.BeginArea)
	}
	if n := origins.CountryName(f.Country); n != "" {
		if !strings.EqualFold(n, f.BeginArea) {
			place = append(place, n)
		}
	} else if f.Area != "" && !strings.EqualFold(f.Area, f.BeginArea) {
		place = append(place, f.Area)
	}
	var s []string
	if kind != "" {
		s = append(s, kind)
	}
	if len(place) > 0 {
		if len(s) == 0 {
			s = append(s, "An artist")
		}
		s = append(s, "from "+strings.Join(place, ", "))
	}
	switch {
	case f.BeginYear > 0 && f.EndYear > 0:
		s = append(s, fmt.Sprintf("active %d–%d", f.BeginYear, f.EndYear))
	case f.BeginYear > 0:
		s = append(s, fmt.Sprintf("active since %d", f.BeginYear))
	}
	if len(s) > 0 {
		b.WriteString(" " + strings.Join(s, ", ") + ".")
	}
	writeTags(&b, f.Genres, f.Styles)
	if sum := strings.TrimSpace(f.Summary); sum != "" {
		if r := []rune(sum); len(r) > bioChars {
			sum = string(r[:bioChars])
			if i := strings.LastIndex(sum, ". "); i > bioChars/2 {
				sum = sum[:i+1]
			}
		}
		b.WriteString(" " + sum)
	}
	return b.String()
}

// AlbumDoc describes an album in plain sentences.
func AlbumDoc(f AlbumFacts) string {
	var b strings.Builder
	b.WriteString(strings.TrimSpace(f.Title))
	if f.Artist != "" {
		b.WriteString(" by " + f.Artist)
	}
	if f.Year > 0 {
		fmt.Fprintf(&b, " (%d)", f.Year)
	}
	b.WriteString(".")
	if t := map[string]string{"album": "A studio album", "ep": "An EP", "single": "A single", "live": "A live album",
		"compilation": "A compilation", "soundtrack": "A soundtrack", "remix": "A remix album", "demo": "A demo"}[f.Type]; t != "" {
		b.WriteString(" " + t + ".")
	}
	writeTags(&b, f.Genres, f.Styles)
	if f.Label != "" {
		b.WriteString(" Released on " + f.Label + ".")
	}
	return b.String()
}

func writeTags(b *strings.Builder, genres, styles []string) {
	if len(genres) > 0 {
		b.WriteString(" Genres: " + strings.Join(genres, ", ") + ".")
	}
	seen := map[string]bool{}
	for _, g := range genres {
		seen[strings.ToLower(g)] = true
	}
	var st []string
	for _, s := range styles {
		if !seen[strings.ToLower(s)] {
			st = append(st, s)
		}
	}
	if len(st) > 0 {
		b.WriteString(" Styles: " + strings.Join(st, ", ") + ".")
	}
}

type musicDoc struct {
	id         int64
	text, hash string
}

// docs builds every artist's and album's description.
func (m *MusicDocs) docs(ctx context.Context) ([]musicDoc, error) {
	tags := map[int64][2][]string{} // genres, styles
	rows, err := m.DB.QueryContext(ctx, `SELECT it.item_id, t.kind, t.name FROM item_tags it JOIN tags t ON t.id = it.tag_id AND t.kind IN ('genre', 'style')
		JOIN items i ON i.id = it.item_id AND i.type IN ('artist', 'album') ORDER BY it.item_id, t.kind, t.id`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int64
		var kind, name string
		if rows.Scan(&id, &kind, &name) != nil {
			continue
		}
		t := tags[id]
		if kind == "genre" {
			t[0] = append(t[0], name)
		} else {
			t[1] = append(t[1], name)
		}
		tags[id] = t
	}
	rows.Close()
	var out []musicDoc
	add := func(id int64, text string) {
		sum := sha256.Sum256([]byte(fmt.Sprintf("v%d\n%s", musicDocVersion, text)))
		out = append(out, musicDoc{id, text, hex.EncodeToString(sum[:16])})
	}
	rows, err = m.DB.QueryContext(ctx, `SELECT i.id, i.title, COALESCE(i.summary, ''), COALESCE(mi.artist_type, ''), COALESCE(mi.gender, ''),
			COALESCE(mi.country, ''), COALESCE(mi.area, ''), COALESCE(mi.begin_area, ''), COALESCE(mi.begin_year, 0), COALESCE(mi.end_year, 0)
		FROM items i JOIN libraries l ON l.id = i.library_id AND l.type = 'music' LEFT JOIN music_info mi ON mi.item_id = i.id AND mi.found = 1
		WHERE i.type = 'artist' AND i.title NOT IN ('', 'Unknown Artist', 'Various Artists') ORDER BY i.id`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int64
		var f ArtistFacts
		if err := rows.Scan(&id, &f.Name, &f.Summary, &f.Type, &f.Gender, &f.Country, &f.Area, &f.BeginArea, &f.BeginYear, &f.EndYear); err != nil {
			rows.Close()
			return nil, err
		}
		f.Genres, f.Styles = tags[id][0], tags[id][1]
		add(id, ArtistDoc(f))
	}
	rows.Close()
	rows, err = m.DB.QueryContext(ctx, `SELECT i.id, i.title, COALESCE(a.title, ''), COALESCE(i.year, 0), COALESCE(i.release_type, mi.primary_type, ''), COALESCE(mi.label, '')
		FROM items i JOIN libraries l ON l.id = i.library_id AND l.type = 'music' LEFT JOIN items a ON a.id = i.parent_id
		LEFT JOIN music_info mi ON mi.item_id = i.id AND mi.found = 1
		WHERE i.type = 'album' AND i.title NOT IN ('', 'Unknown Album') ORDER BY i.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var f AlbumFacts
		if err := rows.Scan(&id, &f.Title, &f.Artist, &f.Year, &f.Type, &f.Label); err != nil {
			return nil, err
		}
		f.Genres, f.Styles = tags[id][0], tags[id][1]
		add(id, AlbumDoc(f))
	}
	return out, rows.Err()
}

// Run embeds new and changed descriptions (and everything after a model change).
func (m *MusicDocs) Run(ctx context.Context) (string, error) {
	if m.Enabled == nil || !m.Enabled() {
		return "AI is off", nil
	}
	if !m.AI.Ready() {
		return "The AI service isn't available; the artist and album index waits for it", nil
	}
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return "Already running", nil
	}
	m.running = true
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		m.running, m.done, m.total = false, 0, 0
		m.mu.Unlock()
	}()
	model := m.modelID()
	m.mu.RLock()
	stale := m.model != model
	m.mu.RUnlock()
	if stale {
		if err := m.Load(ctx); err != nil {
			return "", err
		}
	}
	docs, err := m.docs(ctx)
	if err != nil {
		return "", err
	}
	have := map[int64]string{}
	rows, err := m.DB.QueryContext(ctx, `SELECT item_id, model || ' ' || text_hash FROM music_embeddings`)
	if err != nil {
		return "", err
	}
	for rows.Next() {
		var id int64
		var k string
		if rows.Scan(&id, &k) == nil {
			have[id] = k
		}
	}
	rows.Close()
	var todo []musicDoc
	for _, d := range docs {
		if have[d.id] != model+" "+d.hash {
			todo = append(todo, d)
		}
	}
	if len(todo) == 0 {
		return fmt.Sprintf("All %d artists and albums are indexed", len(docs)), nil
	}
	start := time.Now()
	slog.Info("indexing artists and albums", "items", len(todo), "model", model)
	prefix := ai.DocPrefix(m.AI.EmbedModel())
	for i := 0; i < len(todo); i += musicDocBatch {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if !m.Enabled() {
			return fmt.Sprintf("Stopped (AI turned off) after %d", i), nil
		}
		if i > 0 && m.Pace > 0 {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(m.Pace):
			}
		}
		m.mu.Lock()
		m.done, m.total = i, len(todo)
		m.mu.Unlock()
		chunk := todo[i:min(i+musicDocBatch, len(todo))]
		texts := make([]string, len(chunk))
		for j, d := range chunk {
			texts[j] = prefix + d.text
		}
		vecs, err := m.AI.Embed(ctx, texts)
		if err != nil {
			return "", fmt.Errorf("after %d: %w", i, err)
		}
		if err := m.save(ctx, model, chunk, vecs); err != nil {
			return "", err
		}
	}
	if err := m.Load(ctx); err != nil {
		return "", err
	}
	return fmt.Sprintf("Indexed %d artists and albums in %s", len(todo), time.Since(start).Round(time.Second)), nil
}

func (m *MusicDocs) save(ctx context.Context, model string, chunk []musicDoc, vecs [][]float32) error {
	tx, err := m.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for j, d := range chunk {
		if j >= len(vecs) {
			break
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO music_embeddings (item_id, model, vector, text_hash) VALUES (?, ?, ?, ?)
			ON CONFLICT (item_id) DO UPDATE SET model = excluded.model, vector = excluded.vector, text_hash = excluded.text_hash,
			embedded_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')`, d.id, model, encodeVec(normalize(vecs[j])), d.hash); err != nil {
			if strings.Contains(err.Error(), "FOREIGN KEY") {
				continue
			}
			return err
		}
	}
	return tx.Commit()
}

// Rank scores every indexed artist and album by how well its description matches text
// (cosine similarity). Both are nil when AI is off or nothing is indexed.
func (m *MusicDocs) Rank(ctx context.Context, text string) (artists, albums map[int64]float64) {
	text = strings.TrimSpace(text)
	if m == nil || text == "" || m.Enabled == nil || !m.Enabled() || !m.AI.Ready() {
		return nil, nil
	}
	m.mu.RLock()
	ready := len(m.artists)+len(m.albums) > 0 && m.model == m.modelID()
	m.mu.RUnlock()
	if !ready {
		return nil, nil
	}
	vs, err := m.AI.Embed(ctx, []string{ai.QueryPrefix(m.AI.EmbedModel()) + text})
	if err != nil || len(vs) == 0 {
		return nil, nil
	}
	q := normalize(vs[0])
	m.mu.RLock()
	defer m.mu.RUnlock()
	artists = make(map[int64]float64, len(m.artists))
	for id, v := range m.artists {
		artists[id] = dot(q, v)
	}
	albums = make(map[int64]float64, len(m.albums))
	for id, v := range m.albums {
		albums[id] = dot(q, v)
	}
	return artists, albums
}
