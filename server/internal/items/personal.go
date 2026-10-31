package items

import (
	"context"
	"database/sql"
	"hash/fnv"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Personal Home suggestions (USER-16). A person's taste is a few clusters of what they liked:
// one average blurs someone who loves both horror and romantic comedies into something between
// that fits neither. Every suggestion row is ordered by how well a title fits one of their
// clusters as well as by its rating, with variety (no near-duplicates, one title of a film
// series a row), a daily turn among the strong candidates so Home isn't the same every day,
// and titles passed over on Home day after day slowly giving way to others.

// TasteModel is one person's taste for ordering Home rows; build it once per Home request.
type TasteModel struct {
	uid        int64
	idx        *VideoIndex
	day        string
	clusters   []tasteCluster
	seeds      map[int64]modelSeed
	negs       [][]float32
	passed     map[int64]int   // days on Home without being opened
	collection map[int64]int64 // movie → its film series (collection)
	// From what they watched and liked on demand (LIVE-6): library copies of what they've
	// seen there, and TMDB's recommendations for what they liked (with the title that brought each).
	vodSeen   map[int64]bool
	vodBoost  map[int64]float64
	vodReason map[int64]string
	favs      []int64
}

type tasteCluster struct {
	centroid []float32
	weight   float64
	seeds    []int64 // strongest first
}

type modelSeed struct {
	title  string
	weight float64
	loved  bool // a favourite or rated 8/10 or more
}

// Pick is a title chosen for a row, with why when there's a personal reason.
type Pick struct {
	ID     int64
	Reason string
}

// Cand is a title a row could show: its rating (0–10) and how well it fits the row itself
// (0–1, e.g. a holiday's keywords; 0 when every candidate fits equally).
type Cand struct {
	ID     int64
	Rating float64
	Prior  float64
}

// maxClusters is the most taste clusters a person has; fewer liked titles make fewer.
const maxClusters = 4

// TasteModel builds uid's taste: their favourites and the last 40 titles they finished
// (weighted by rating where they gave one), and what they liked on demand that the library
// has, in up to four clusters; what they turned away from; and how often Home has shown each
// title without them opening it. idx may be empty (no clusters then: rows go by rating).
func (s *Store) TasteModel(ctx context.Context, uid int64, idx *VideoIndex, vod VODTaste) (*TasteModel, error) {
	m := &TasteModel{uid: uid, idx: idx, day: time.Now().Format("2006-01-02"), seeds: map[int64]modelSeed{},
		passed: map[int64]int{}, collection: map[int64]int64{}, vodSeen: map[int64]bool{}, vodBoost: map[int64]float64{},
		vodReason: map[int64]string{}}
	if uid == 0 {
		return m, nil
	}
	seeds, err := s.recentSeeds(ctx, uid, 3650, 40)
	if err != nil {
		return nil, err
	}
	if seeds, err = s.withFavorites(ctx, uid, seeds, 30); err != nil {
		return nil, err
	}
	var ids []int64
	var weights []float64
	for _, sd := range seeds {
		w := 1.0
		switch {
		case sd.Favorite:
			w = favoriteWeight
			m.favs = append(m.favs, sd.ID)
		case sd.Rating > 0 && sd.Rating < 5:
			continue // not liked
		case sd.Rating > 0:
			w = sd.Rating / 7
		}
		ids, weights = append(ids, sd.ID), append(weights, w)
		m.seeds[sd.ID] = modelSeed{weight: w, loved: sd.Favorite || sd.Rating >= 8}
	}
	// On demand: liked titles the library has are seeds too.
	lib, err := s.itemsByTMDB(ctx, vod.refs())
	if err != nil {
		return nil, err
	}
	for _, l := range vod.Liked {
		for _, id := range lib[l.TMDBRef] {
			if _, ok := m.seeds[id]; !ok {
				ids, weights = append(ids, id), append(weights, l.Weight)
				m.seeds[id] = modelSeed{weight: l.Weight, loved: l.Weight >= 8.0/7}
			}
		}
		for rank, tm := range l.Recs {
			if rank == 40 {
				break
			}
			b := vodRecWeight * min(l.Weight, favoriteWeight) * max(0.2, 1-0.02*float64(rank))
			for _, id := range lib[TMDBRef{l.Type, tm}] {
				if b > m.vodBoost[id] {
					m.vodBoost[id] = b
					if l.Title != "" {
						m.vodReason[id] = l.Title
					}
				}
			}
		}
	}
	for _, r := range vod.Seen {
		for _, id := range lib[r] {
			m.vodSeen[id] = true
		}
	}
	if err := s.seedTitles(ctx, m.seeds); err != nil {
		return nil, err
	}
	m.clusters = clusterSeeds(idx, ids, weights)
	neg, err := s.disliked(ctx, uid)
	if err != nil {
		return nil, err
	}
	for _, id := range neg {
		if v := idx.Get(id); v != nil {
			m.negs = append(m.negs, v.Vec)
		}
	}
	if err := eachRow(ctx, s.db, `SELECT item_id, days FROM home_seen WHERE user_id = ?`, []any{uid}, func(r *sql.Rows) error {
		var id int64
		var days int
		if err := r.Scan(&id, &days); err != nil {
			return err
		}
		m.passed[id] = days
		return nil
	}); err != nil {
		return nil, err
	}
	err = eachRow(ctx, s.db, `SELECT ci.item_id, ci.collection_id FROM collection_items ci JOIN items i ON i.id = ci.item_id AND i.type = 'movie'`,
		nil, func(r *sql.Rows) error {
			var id, c int64
			if err := r.Scan(&id, &c); err != nil {
				return err
			}
			m.collection[id] = c
			return nil
		})
	return m, err
}

func (s *Store) seedTitles(ctx context.Context, seeds map[int64]modelSeed) error {
	if len(seeds) == 0 {
		return nil
	}
	args := make([]any, 0, len(seeds))
	for id := range seeds {
		args = append(args, id)
	}
	return eachRow(ctx, s.db, `SELECT id, title FROM items WHERE id IN (?`+strings.Repeat(",?", len(args)-1)+`)`, args, func(r *sql.Rows) error {
		var id int64
		var t string
		if err := r.Scan(&id, &t); err != nil {
			return err
		}
		sd := seeds[id]
		sd.title = t
		seeds[id] = sd
		return nil
	})
}

// clusterSeeds groups the embedded seeds into up to maxClusters clusters (one per five or so
// liked titles) by weighted spherical k-means, started from the strongest seed and then the
// seeds least like the clusters so far, so the result is the same every time.
func clusterSeeds(idx *VideoIndex, ids []int64, weights []float64) []tasteCluster {
	type pt struct {
		id  int64
		w   float64
		vec []float32
	}
	var pts []pt
	for i, id := range ids {
		if v := idx.Get(id); v != nil {
			pts = append(pts, pt{id, weights[i], v.Vec})
		}
	}
	if len(pts) == 0 {
		return nil
	}
	k := min(maxClusters, max(1, (len(pts)+4)/6))
	// Strongest first (stable: most recent first among equals).
	order := make([]int, len(pts))
	for i := range order {
		order[i] = i
	}
	sort.SliceStable(order, func(a, b int) bool { return pts[order[a]].w > pts[order[b]].w })
	centers := [][]float32{pts[order[0]].vec}
	for len(centers) < k {
		best, bestSim := -1, math.Inf(1)
		for _, i := range order {
			sim := math.Inf(-1)
			for _, c := range centers {
				sim = math.Max(sim, dotVec(c, pts[i].vec))
			}
			if sim < bestSim {
				best, bestSim = i, sim
			}
		}
		if best < 0 || bestSim > 0.97 { // the rest are all alike: fewer clusters
			break
		}
		centers = append(centers, pts[best].vec)
	}
	assign := make([]int, len(pts))
	for iter := 0; iter < 8; iter++ {
		for i, p := range pts {
			best, bestSim := 0, math.Inf(-1)
			for c, ctr := range centers {
				if s := dotVec(ctr, p.vec); s > bestSim {
					best, bestSim = c, s
				}
			}
			assign[i] = best
		}
		for c := range centers {
			var sum []float64
			for i, p := range pts {
				if assign[i] != c {
					continue
				}
				if sum == nil {
					sum = make([]float64, len(p.vec))
				}
				for j := range sum {
					sum[j] += p.w * float64(p.vec[j])
				}
			}
			if sum == nil {
				continue
			}
			v := make([]float32, len(sum))
			for j, x := range sum {
				v[j] = float32(x)
			}
			centers[c] = normalizeVec(v)
		}
	}
	out := make([]tasteCluster, len(centers))
	for c := range centers {
		out[c].centroid = centers[c]
	}
	for _, i := range order {
		c := assign[i]
		out[c].weight += pts[i].w
		out[c].seeds = append(out[c].seeds, pts[i].id)
	}
	kept := out[:0]
	for _, c := range out {
		if c.weight > 0 {
			kept = append(kept, c)
		}
	}
	sort.SliceStable(kept, func(a, b int) bool { return kept[a].weight > kept[b].weight })
	return kept
}

// Personal reports whether there's any taste to go on.
func (m *TasteModel) Personal() bool { return m != nil && len(m.clusters) > 0 }

// fit is how well id fits the cluster it's closest to, and that cluster (-1 when either it
// isn't embedded or there's no taste).
func (m *TasteModel) fit(id int64) (float64, int) {
	if !m.Personal() {
		return 0, -1
	}
	v := m.idx.Get(id)
	if v == nil {
		return 0, -1
	}
	best, bestSim := -1, math.Inf(-1)
	for c, cl := range m.clusters {
		if s := dotVec(cl.centroid, v.Vec); s > bestSim {
			best, bestSim = c, s
		}
	}
	return bestSim, best
}

// nearestSeed is the liked title in cluster c most like id.
func (m *TasteModel) nearestSeed(id int64, c int) (int64, bool) {
	if c < 0 || c >= len(m.clusters) {
		return 0, false
	}
	v := m.idx.Get(id)
	if v == nil {
		return 0, false
	}
	var best int64
	bestSim := math.Inf(-1)
	for _, sd := range m.clusters[c].seeds {
		if w := m.idx.Get(sd); w != nil {
			if s := dotVec(v.Vec, w.Vec); s > bestSim {
				best, bestSim = sd, s
			}
		}
	}
	return best, best != 0
}

// becauseReason is "Because you loved/watched X" for a title chosen near a liked one.
func (m *TasteModel) becauseReason(id int64, c int) string {
	if name := m.vodReason[id]; name != "" && m.vodBoost[id] > 0 && c < 0 {
		return "Because you watched " + name
	}
	sd, ok := m.nearestSeed(id, c)
	if !ok {
		if name := m.vodReason[id]; name != "" {
			return "Because you watched " + name
		}
		return ""
	}
	s := m.seeds[sd]
	if s.title == "" {
		return ""
	}
	if s.loved {
		return "Because you loved " + s.title
	}
	return "Because you watched " + s.title
}

// likeReason is "Like X" for a category row's title that fits a liked one closely.
func (m *TasteModel) likeReason(id int64, c int) string {
	if sd, ok := m.nearestSeed(id, c); ok && m.seeds[sd].title != "" {
		return "Like " + m.seeds[sd].title
	}
	return ""
}

// jitterScale scales the daily turn (tests set it to 0 for a fixed order).
var jitterScale = 1.0

// jitter is a number in [0, 1) that's the same for a person, a title and a day, and changes
// the next day: the daily turn among titles that score about the same.
func (m *TasteModel) jitter(id int64) float64 {
	h := fnv.New64a()
	h.Write([]byte(strconv.FormatInt(m.uid, 10) + "|" + m.day + "|" + strconv.FormatInt(id, 10)))
	return jitterScale * float64(h.Sum64()%10000) / 10000
}

// passedPenalty lowers a title shown on Home for more than three days without being opened.
func (m *TasteModel) passedPenalty(id int64) float64 {
	if m == nil {
		return 0
	}
	if d := m.passed[id]; d > 3 {
		return min(0.3, 0.05*float64(d-3))
	}
	return 0
}

// negPenalty lowers a title like one they turned away from, as PenaliseSimilar does.
func (m *TasteModel) negPenalty(id int64) float64 {
	if m == nil || len(m.negs) == 0 {
		return 0
	}
	v := m.idx.Get(id)
	if v == nil {
		return 0
	}
	worst := 0.0
	for _, n := range m.negs {
		worst = math.Max(worst, dotVec(v.Vec, n))
	}
	return math.Min(0.2, dislikeWeight*max(0, worst-dislikeFloor))
}

// Order picks up to n of cands for a row: by fit (when there's a taste) and rating, with the
// row's own relevance, a daily turn, titles passed over and ones like dislikes lowered, then
// for variety no near-duplicates and one title per film series (filled in from those only if
// the row would otherwise be short).
func (m *TasteModel) Order(cands []Cand, n int) []Pick {
	if len(cands) == 0 {
		return nil
	}
	type scored struct {
		Cand
		score, fit float64
		cluster    int
	}
	list := make([]scored, len(cands))
	lo, hi := math.Inf(1), math.Inf(-1)
	for i, c := range cands {
		f, cl := m.fit(c.ID)
		list[i] = scored{Cand: c, fit: f, cluster: cl}
		if cl >= 0 {
			lo, hi = math.Min(lo, f), math.Max(hi, f)
		}
	}
	personal := m.Personal() && hi > lo
	for i := range list {
		c := &list[i]
		r := math.Min(c.Rating, 10) / 10
		if personal && c.cluster >= 0 {
			fn := (c.fit - lo) / (hi - lo)
			c.score = 0.5*fn + 0.35*r + 0.15*c.Prior
		} else {
			c.score = 0.85*r + 0.15*c.Prior
			if personal {
				c.score -= 0.1 // nothing to say it fits them
			}
		}
		if m != nil {
			c.score += 0.06*m.jitter(c.ID) - m.passedPenalty(c.ID) - m.negPenalty(c.ID)
		}
	}
	sort.SliceStable(list, func(a, b int) bool { return list[a].score > list[b].score })
	picks := m.diverse(len(list), n, func(i int) int64 { return list[i].ID })
	out := make([]Pick, len(picks))
	for i, p := range picks {
		c := list[p]
		out[i].ID = c.ID
		if personal && c.cluster >= 0 && (c.fit-lo)/(hi-lo) >= 0.7 {
			out[i].Reason = m.likeReason(c.ID, c.cluster)
		}
	}
	return out
}

// diverse picks up to n of a ranked list (len items, id(i) the i-th): skipping titles almost
// the same as one already picked and a second title of a film series, unless the row would be
// short without them. It returns the positions picked, in order.
func (m *TasteModel) diverse(count, n int, id func(int) int64) []int {
	var picked []int
	var vecs [][]float32
	series := map[int64]bool{}
	var skipped []int
	for i := 0; i < count && len(picked) < n; i++ {
		item := id(i)
		var v *VideoVec
		if m != nil {
			v = m.idx.Get(item)
		}
		dup := false
		if v != nil {
			for _, p := range vecs {
				if dotVec(v.Vec, p) >= 0.95 {
					dup = true
					break
				}
			}
		}
		var col int64
		if m != nil {
			col = m.collection[item]
		}
		if dup || (col != 0 && series[col]) {
			skipped = append(skipped, i)
			continue
		}
		picked = append(picked, i)
		if v != nil {
			vecs = append(vecs, v.Vec)
		}
		if col != 0 {
			series[col] = true
		}
	}
	for _, i := range skipped {
		if len(picked) >= n {
			break
		}
		picked = append(picked, i)
	}
	sort.SliceStable(picked, func(a, b int) bool { return picked[a] < picked[b] })
	return picked
}
