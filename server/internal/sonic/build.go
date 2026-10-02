package sonic

import (
	"math"
	"math/rand/v2"
	"sort"
)

// Options shape a generated playlist.
type Options struct {
	Length int
	// Avoid lowers the chance of these tracks (recently played or skipped); 0–1 penalty each.
	Avoid map[int64]float64
	// Exclude never returns these tracks (already queued).
	Exclude map[int64]bool
	Keep    Filter
	Rand    *rand.Rand
}

func (o *Options) rng() *rand.Rand {
	if o.Rand == nil {
		o.Rand = rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64()))
	}
	return o.Rand
}

func (o *Options) allowed(t *Track) bool {
	if o.Exclude[t.ItemID] {
		return false
	}
	return o.Keep == nil || o.Keep(t)
}

// Flow builds a radio-style sequence: each next track is close to both the current one and
// the station's sound (anchor), with variety rules (no artist twice within three tracks, no
// album twice within six) and a little randomness so stations don't repeat.
func (x *Index) Flow(anchor []float32, first *Track, o Options) []*Track {
	if o.Length <= 0 {
		o.Length = 50
	}
	pool := x.Nearest(anchor, max(400, o.Length*8), o.allowed)
	used := map[int64]bool{}
	var out []*Track
	if first != nil {
		out = append(out, first)
		used[first.ItemID] = true
	}
	r := o.rng()
	for len(out) < o.Length {
		var cur []float32
		if len(out) > 0 {
			cur = out[len(out)-1].Vec
		}
		best, bestScore := -1, math.Inf(-1)
		for i, c := range pool {
			if used[c.ItemID] {
				continue
			}
			s := c.Score
			if cur != nil {
				s = 0.55*dot(cur, c.Vec) + 0.45*c.Score
			}
			for j := len(out) - 1; j >= 0 && j >= len(out)-6; j-- {
				if out[j].AlbumID == c.AlbumID && c.AlbumID != 0 {
					s -= 0.25
				}
				if j >= len(out)-3 && out[j].ArtistID == c.ArtistID && c.ArtistID != 0 {
					s -= 0.2
				}
			}
			s -= 0.3 * o.Avoid[c.ItemID]
			s += r.Float64() * 0.04
			if s > bestScore {
				best, bestScore = i, s
			}
		}
		if best < 0 {
			break
		}
		used[pool[best].ItemID] = true
		out = append(out, pool[best].Track)
	}
	return out
}

// Pick chooses n diverse tracks close to v (maximal marginal relevance), then orders them
// so each flows into the next. Used for Sonic Sage prompts and mixes.
func (x *Index) Pick(v []float32, n int, o Options) []*Track {
	pool := x.Nearest(v, max(300, n*10), o.allowed)
	// Drop the weakest part of the pool: in a small library (or for a prompt few tracks
	// match) the pool reaches tracks that have nothing to do with the request. In a large
	// library the pool is tight and this cuts almost nothing.
	if len(pool) > 5 {
		floor := pool[0].Score - 0.6*(pool[0].Score-pool[len(pool)-1].Score)
		cut := len(pool)
		for cut > 5 && pool[cut-1].Score < floor {
			cut--
		}
		pool = pool[:cut]
	}
	var chosen []*Track
	used := map[int]bool{}
	for len(chosen) < n {
		best, bestScore := -1, math.Inf(-1)
		for i, c := range pool {
			if used[i] {
				continue
			}
			sim := 0.0
			for _, ch := range chosen {
				sim = math.Max(sim, dot(ch.Vec, c.Vec))
				if ch.ArtistID == c.ArtistID && c.ArtistID != 0 {
					sim = math.Max(sim, 0.97)
				}
			}
			s := 0.75*c.Score - 0.25*sim - 0.3*o.Avoid[c.ItemID]
			if s > bestScore {
				best, bestScore = i, s
			}
		}
		if best < 0 {
			break
		}
		used[best] = true
		chosen = append(chosen, pool[best].Track)
	}
	return order(chosen, v)
}

// order chains tracks greedily by similarity, starting from the one closest to v (the
// prompt or mix centre) so the strongest match plays first.
func order(ts []*Track, v []float32) []*Track {
	if len(ts) < 3 {
		return ts
	}
	start := 0
	for i, t := range ts {
		if dot(v, t.Vec) > dot(v, ts[start].Vec) {
			start = i
		}
	}
	out := []*Track{ts[start]}
	left := append(append([]*Track{}, ts[:start]...), ts[start+1:]...)
	for len(left) > 0 {
		cur := out[len(out)-1]
		bi := 0
		for i, t := range left {
			if dot(cur.Vec, t.Vec) > dot(cur.Vec, left[bi].Vec) {
				bi = i
			}
		}
		out = append(out, left[bi])
		left = append(left[:bi], left[bi+1:]...)
	}
	return out
}

// Adventure travels from one track to another through tracks that sit between them in
// sound (Plex's "Sonic Adventure").
func (x *Index) Adventure(from, to *Track, n int, o Options) []*Track {
	if n < 3 {
		n = 3
	}
	out := []*Track{from}
	used := map[int64]bool{from.ItemID: true, to.ItemID: true}
	for i := 1; i < n-1; i++ {
		t := float32(i) / float32(n-1)
		p := make([]float32, len(from.Vec))
		for j := range p {
			p[j] = (1-t)*from.Vec[j] + t*to.Vec[j]
		}
		p = normalize(p)
		prev := out[len(out)-1]
		for _, c := range x.Nearest(p, 60, o.allowed) {
			if used[c.ItemID] || (c.ArtistID == prev.ArtistID && c.ArtistID != 0) {
				continue
			}
			used[c.ItemID] = true
			out = append(out, c.Track)
			break
		}
	}
	return append(out, to)
}

// Similar returns tracks that sound like t, skipping its own album.
func (x *Index) Similar(t *Track, n int, keep Filter) []*Track {
	var out []*Track
	perArtist := map[int64]int{}
	for _, c := range x.Nearest(t.Vec, n*6, keep) {
		if c.ItemID == t.ItemID || (c.AlbumID == t.AlbumID && t.AlbumID != 0) || perArtist[c.ArtistID] >= 2 {
			continue
		}
		perArtist[c.ArtistID]++
		out = append(out, c.Track)
		if len(out) == n {
			break
		}
	}
	return out
}

// Group is an album's or artist's averaged sound.
type Group struct {
	ID    int64
	Vec   []float32
	Score float64
}

// SimilarGroups ranks albums (byAlbum) or artists by how close their average sound is to v.
func (x *Index) SimilarGroups(v []float32, byAlbum bool, exclude int64, n int, keep Filter) []Group {
	members := map[int64][]*Track{}
	for _, t := range x.Where(func(t *Track) bool { return keep == nil || keep(t) }) {
		id := t.ArtistID
		if byAlbum {
			id = t.AlbumID
		}
		if id != 0 && id != exclude {
			members[id] = append(members[id], t)
		}
	}
	out := make([]Group, 0, len(members))
	for id, ts := range members {
		m := Mean(ts)
		out = append(out, Group{ID: id, Vec: m, Score: dot(v, m)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	if len(out) > n {
		out = out[:n]
	}
	return out
}

// Clusters splits a listener's tracks into k taste groups (k-means on embeddings) and
// returns each group's centre, biggest group first. Used for daily mixes.
func Clusters(ts []*Track, k int, r *rand.Rand) [][]float32 {
	if len(ts) == 0 || k <= 0 {
		return nil
	}
	k = min(k, len(ts))
	centres := make([][]float32, k)
	perm := r.Perm(len(ts))
	for i := range centres {
		centres[i] = ts[perm[i]].Vec
	}
	assign := make([]int, len(ts))
	for iter := 0; iter < 12; iter++ {
		for i, t := range ts {
			best := 0
			for c := range centres {
				if dot(t.Vec, centres[c]) > dot(t.Vec, centres[best]) {
					best = c
				}
			}
			assign[i] = best
		}
		for c := range centres {
			var members []*Track
			for i, t := range ts {
				if assign[i] == c {
					members = append(members, t)
				}
			}
			if len(members) > 0 {
				centres[c] = Mean(members)
			}
		}
	}
	size := make([]int, k)
	for _, a := range assign {
		size[a]++
	}
	idx := make([]int, k)
	for i := range idx {
		idx[i] = i
	}
	sort.Slice(idx, func(a, b int) bool { return size[idx[a]] > size[idx[b]] })
	out := make([][]float32, 0, k)
	for _, i := range idx {
		if size[i] > 0 {
			out = append(out, centres[i])
		}
	}
	return out
}
