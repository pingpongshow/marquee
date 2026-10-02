package sonic

import "sort"

// Guest DJ modes (MUSIC-6), after Plexamp's DJs: each weaves one track into the queue now
// and then, chosen from what's playing.
const (
	DJStretch  = "stretch"   // sounds like this, by someone else
	DJGroupie  = "groupie"   // the same artist's other albums
	DJDeepCuts = "deep_cuts" // the artist's least-played tracks
	DJContempo = "contempo"  // similar sound from the same era
)

var DJModes = []string{DJStretch, DJGroupie, DJDeepCuts, DJContempo}

// DJPick chooses a track to play after t. plays gives the listener's play counts (for
// deep cuts). It returns nil when nothing fits.
func (x *Index) DJPick(t *Track, mode string, o Options, plays func(ids []int64) map[int64]int) *Track {
	r := o.rng()
	keep := func(extra func(c *Track) bool) Filter {
		return func(c *Track) bool { return c.ItemID != t.ItemID && o.allowed(c) && extra(c) }
	}
	var pool []Scored
	switch mode {
	case DJStretch:
		pool = x.Nearest(t.Vec, 12, keep(func(c *Track) bool { return c.ArtistID != t.ArtistID }))
	case DJGroupie:
		pool = x.Nearest(t.Vec, 6, keep(func(c *Track) bool { return c.ArtistID == t.ArtistID && c.AlbumID != t.AlbumID }))
	case DJContempo:
		if t.Year == 0 {
			return nil
		}
		pool = x.Nearest(t.Vec, 10, keep(func(c *Track) bool { return c.ArtistID != t.ArtistID && c.Year != 0 && abs(c.Year-t.Year) <= 3 }))
	case DJDeepCuts:
		mine := x.Nearest(t.Vec, 200, keep(func(c *Track) bool { return c.ArtistID == t.ArtistID }))
		if len(mine) == 0 {
			return nil
		}
		ids := make([]int64, len(mine))
		for i, c := range mine {
			ids[i] = c.ItemID
		}
		counts := plays(ids)
		// Least played first; among equals, what sounds closest to now.
		sort.SliceStable(mine, func(i, j int) bool { return counts[mine[i].ItemID] < counts[mine[j].ItemID] })
		least := counts[mine[0].ItemID]
		for _, c := range mine {
			if counts[c.ItemID] > least {
				break
			}
			pool = append(pool, c)
		}
		if len(pool) > 6 {
			pool = pool[:6]
		}
	default:
		return nil
	}
	if len(pool) == 0 {
		return nil
	}
	// A little randomness among the best few, avoiding recent skips and plays.
	best, bestScore := -1, -1e9
	for i, c := range pool {
		s := c.Score - 0.3*o.Avoid[c.ItemID] + r.Float64()*0.05
		if s > bestScore {
			best, bestScore = i, s
		}
	}
	return pool[best].Track
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

