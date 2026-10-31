package soundprint

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// Played together (MUSIC-17): songs someone plays in one sitting, or keeps in one playlist,
// belong together in a way their sound can't tell. Every night the pairs are counted from the
// listening history (everyone's on the server) and the playlists; radios and mixes then
// lift the tracks played with the one playing or with their seeds.

const (
	// sittingGap is the pause that ends a listening session.
	sittingGap = 45 * time.Minute
	// pairWindow is how far apart two tracks of a sitting (or a playlist) may be to pair.
	pairWindow = 4
	// pairsKept is how many pairings each track keeps (its strongest).
	pairsKept = 30
	// minPairWeight leaves out pairs seen once in passing.
	minPairWeight = 1.5
	// playlistWeight is how much a playlist's neighbours count against a sitting's.
	playlistWeight = 0.5
	// coplayHistory is how far back the history is read.
	coplayHistory = "-730 days"
)

type play struct {
	user, item, album int64
	at                time.Time
	full              bool
}

// RebuildCoplays counts the pairs again and replaces the table and the loaded pairs.
func (s *Service) RebuildCoplays(ctx context.Context) (string, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT h.user_id, h.item_id, COALESCE(i.parent_id, 0), h.started_at,
		COALESCE(h.position_ms, 0) >= MIN(30000, COALESCE(i.duration_ms, 60000) / 2)
		FROM play_history h JOIN items i ON i.id = h.item_id AND i.type = 'track'
		WHERE h.user_id IS NOT NULL AND h.started_at > strftime('%Y-%m-%dT%H:%M:%fZ', 'now', ?)
		ORDER BY h.user_id, h.started_at`, coplayHistory)
	if err != nil {
		return "", err
	}
	var plays []play
	for rows.Next() {
		var p play
		var at string
		if rows.Scan(&p.user, &p.item, &p.album, &at, &p.full) == nil {
			if t, err := time.Parse(time.RFC3339Nano, at); err == nil {
				p.at = t
				plays = append(plays, p)
			}
		}
	}
	rows.Close()
	weights := map[[2]int64]float64{}
	add := func(a, b int64, w float64) {
		if a == b {
			return
		}
		if a > b {
			a, b = b, a
		}
		weights[[2]int64{a, b}] += w
	}
	sittings := 0
	for i := 0; i < len(plays); {
		j := i + 1
		for j < len(plays) && plays[j].user == plays[i].user && plays[j].at.Sub(plays[j-1].at) < sittingGap {
			j++
		}
		pairSitting(plays[i:j], add)
		sittings++
		i = j
	}

	// Playlists: neighbours in each manual audio playlist.
	rows, err = s.DB.QueryContext(ctx, `SELECT pi.playlist_id, pi.item_id, COALESCE(i.parent_id, 0) FROM playlist_items pi
		JOIN playlists p ON p.id = pi.playlist_id AND p.kind = 'audio' AND p.smart_rule IS NULL
		JOIN items i ON i.id = pi.item_id AND i.type = 'track' ORDER BY pi.playlist_id, pi.ord`)
	if err != nil {
		return "", err
	}
	var list []play
	var cur int64 = -1
	flush := func() {
		pairList(list, playlistWeight, add)
		list = list[:0]
	}
	for rows.Next() {
		var pl int64
		var p play
		if rows.Scan(&pl, &p.item, &p.album) == nil {
			if pl != cur {
				flush()
				cur = pl
			}
			p.full = true
			list = append(list, p)
		}
	}
	rows.Close()
	flush()

	pairs := strongest(weights)
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM music_coplays`); err != nil {
		return "", err
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO music_coplays(a, b, weight) VALUES (?, ?, ?)`)
	if err != nil {
		return "", err
	}
	n := 0
	for a, bs := range pairs {
		for b, w := range bs {
			if _, err := stmt.ExecContext(ctx, a, b, w); err != nil {
				stmt.Close()
				return "", err
			}
			n++
		}
	}
	stmt.Close()
	if err := tx.Commit(); err != nil {
		return "", err
	}
	s.learnMu.Lock()
	s.coplays = pairs
	s.learnMu.Unlock()
	return fmt.Sprintf("%d tracks paired with the tracks played or kept with them (%d listening sessions)", len(pairs), sittings), nil
}

// pairSitting pairs the tracks played through in one sitting that are near each other,
// nearer pairs counting more. Tracks of one album don't pair: an album played through says
// nothing its sound doesn't.
func pairSitting(ps []play, add func(a, b int64, w float64)) {
	var full []play
	for _, p := range ps {
		if p.full {
			full = append(full, p)
		}
	}
	pairList(full, 1, add)
}

func pairList(ps []play, scale float64, add func(a, b int64, w float64)) {
	for i := range ps {
		for d := 1; d <= pairWindow && i+d < len(ps); d++ {
			a, b := ps[i], ps[i+d]
			if a.album == b.album && a.album != 0 {
				continue
			}
			add(a.item, b.item, scale/float64(d))
		}
	}
}

// strongest keeps each track's strongest pairs, scaled so its strongest is 1.
func strongest(weights map[[2]int64]float64) map[int64]map[int64]float64 {
	by := map[int64][]Scored{}
	for k, w := range weights {
		if w < minPairWeight {
			continue
		}
		by[k[0]] = append(by[k[0]], Scored{&Track{ItemID: k[1]}, w})
		by[k[1]] = append(by[k[1]], Scored{&Track{ItemID: k[0]}, w})
	}
	out := make(map[int64]map[int64]float64, len(by))
	for a, list := range by {
		sort.Slice(list, func(i, j int) bool {
			if list[i].Score != list[j].Score {
				return list[i].Score > list[j].Score
			}
			return list[i].ItemID < list[j].ItemID
		})
		list = list[:min(len(list), pairsKept)]
		top := list[0].Score
		m := make(map[int64]float64, len(list))
		for _, sc := range list {
			m[sc.ItemID] = sc.Score / top
		}
		out[a] = m
	}
	return out
}

// LoadLearned reads the pairs, similar artists and tags into memory (at start).
func (s *Service) LoadLearned(ctx context.Context) error {
	pairs := map[int64]map[int64]float64{}
	rows, err := s.DB.QueryContext(ctx, `SELECT a, b, weight FROM music_coplays`)
	if err != nil {
		return err
	}
	for rows.Next() {
		var a, b int64
		var w float64
		if rows.Scan(&a, &b, &w) == nil {
			if pairs[a] == nil {
				pairs[a] = map[int64]float64{}
			}
			pairs[a][b] = w
		}
	}
	rows.Close()
	s.learnMu.Lock()
	s.coplays = pairs
	s.learnMu.Unlock()
	return s.LoadCommunity(ctx)
}

// coplayed is the loaded pairs (read only).
func (s *Service) coplayed() map[int64]map[int64]float64 {
	s.learnMu.RLock()
	defer s.learnMu.RUnlock()
	return s.coplays
}

// coplayWith returns how strongly each track pairs with any of the seeds (the strongest
// pairing, 0–1), for Options.Affinity.
func (s *Service) coplayWith(seeds []*Track) map[int64]float64 {
	pairs := s.coplayed()
	out := map[int64]float64{}
	for _, t := range seeds {
		for b, w := range pairs[t.ItemID] {
			out[b] = max(out[b], w)
		}
	}
	return out
}
