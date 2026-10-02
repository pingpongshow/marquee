package sonic

import (
	"context"
	"database/sql"
	"encoding/binary"
	"math"
	"sort"
	"sync"
)

// Track is one analysed track in memory.
type Track struct {
	ItemID, AlbumID, ArtistID, LibraryID int64
	Year                                 int
	Vec                                  []float32
	BPM, Energy                          float64
	Key, Mode                            string
}

// Index holds every analysed track's embedding for fast similarity search (16k tracks of
// 512 floats is ~32 MB; a full scan takes a few milliseconds).
type Index struct {
	mu     sync.RWMutex
	tracks map[int64]*Track
}

func NewIndex() *Index { return &Index{tracks: map[int64]*Track{}} }

func encode(v []float32) []byte {
	b := make([]byte, 4*len(v))
	for i, x := range v {
		binary.LittleEndian.PutUint32(b[4*i:], math.Float32bits(x))
	}
	return b
}

func decode(b []byte) []float32 {
	v := make([]float32, len(b)/4)
	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[4*i:]))
	}
	return v
}

// Load reads every embedding from the database.
func (x *Index) Load(ctx context.Context, db *sql.DB) error {
	rows, err := db.QueryContext(ctx, `SELECT s.item_id, COALESCE(i.parent_id, 0), COALESCE(i.grandparent_id, 0), i.library_id,
		COALESCE(i.year, a.year, 0), s.embedding, COALESCE(s.bpm, 0), COALESCE(s.energy, 0), COALESCE(s.musical_key, ''), COALESCE(s.mode, '')
		FROM sonic s JOIN items i ON i.id = s.item_id LEFT JOIN items a ON a.id = i.parent_id WHERE s.embedding IS NOT NULL`)
	if err != nil {
		return err
	}
	defer rows.Close()
	m := map[int64]*Track{}
	for rows.Next() {
		t := &Track{}
		var emb []byte
		if err := rows.Scan(&t.ItemID, &t.AlbumID, &t.ArtistID, &t.LibraryID, &t.Year, &emb, &t.BPM, &t.Energy, &t.Key, &t.Mode); err != nil {
			return err
		}
		t.Vec = decode(emb)
		m[t.ItemID] = t
	}
	x.mu.Lock()
	x.tracks = m
	x.mu.Unlock()
	return rows.Err()
}

func (x *Index) Put(t *Track) {
	x.mu.Lock()
	x.tracks[t.ItemID] = t
	x.mu.Unlock()
}

func (x *Index) Get(id int64) *Track {
	x.mu.RLock()
	defer x.mu.RUnlock()
	return x.tracks[id]
}

func (x *Index) Len() int {
	x.mu.RLock()
	defer x.mu.RUnlock()
	return len(x.tracks)
}

func dot(a, b []float32) float64 {
	var s float32
	n := min(len(a), len(b))
	for i := 0; i < n; i++ {
		s += a[i] * b[i]
	}
	return float64(s)
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

// Mean is the normalized average of tracks' vectors (an album's or artist's sound).
func Mean(ts []*Track) []float32 {
	if len(ts) == 0 {
		return nil
	}
	sum := make([]float32, len(ts[0].Vec))
	for _, t := range ts {
		for i, x := range t.Vec {
			if i < len(sum) {
				sum[i] += x
			}
		}
	}
	return normalize(sum)
}

// Scored is a track and its similarity to a query.
type Scored struct {
	*Track
	Score float64
}

// Filter limits which tracks a query may return.
type Filter func(*Track) bool

// Nearest returns the k most similar tracks to v that pass keep, best first.
func (x *Index) Nearest(v []float32, k int, keep Filter) []Scored {
	x.mu.RLock()
	all := make([]Scored, 0, len(x.tracks))
	for _, t := range x.tracks {
		if keep == nil || keep(t) {
			all = append(all, Scored{t, dot(v, t.Vec)})
		}
	}
	x.mu.RUnlock()
	sort.Slice(all, func(i, j int) bool { return all[i].Score > all[j].Score })
	if len(all) > k {
		all = all[:k]
	}
	return all
}

// Where returns every track passing keep.
func (x *Index) Where(keep Filter) []*Track {
	x.mu.RLock()
	defer x.mu.RUnlock()
	var out []*Track
	for _, t := range x.tracks {
		if keep(t) {
			out = append(out, t)
		}
	}
	return out
}
