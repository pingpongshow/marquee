package soundprint

import (
	"context"
	"time"
)

// Learning from behaviour: what someone skips and how they rate shapes what radios, mixes
// and Muse pick for them. The rules are deliberately simple so they can be explained:
//
//   - A quick skip (stopped in the first 30 s, and before half way) counts against the
//     track. It counts fully today and fades to nothing over 30 days.
//   - A full play (80% or more) counts for it, fading the same way, and cancels skips.
//   - 2 stars or lower: always avoided. 4 stars or more: never avoided for skips.
//   - Skipping several tracks by one artist (more skips than full plays, at least three)
//     makes that artist's other tracks a little less likely.

// PlayEvent is one play of a track from the listening history.
type PlayEvent struct {
	ItemID, ArtistID       int64
	Age                    time.Duration // how long ago it was played
	PositionMS, DurationMS int64
}

const (
	learnWindow   = 30 * 24 * time.Hour
	skipWeight    = 0.4
	fullWeight    = 0.5
	artistPenalty = 0.1 // per skip beyond the allowance, up to artistMax
	artistMax     = 0.3
)

// decay is 1 for now, falling linearly to 0 at learnWindow.
func decay(age time.Duration) float64 {
	if age < 0 {
		age = 0
	}
	return max(0, 1-float64(age)/float64(learnWindow))
}

// quickSkip reports a play stopped in the first 30 s and before half way.
func quickSkip(e PlayEvent) bool {
	half := e.DurationMS / 2
	if e.DurationMS <= 0 {
		half = 30000
	}
	return e.PositionMS < min(30000, half)
}

func fullPlay(e PlayEvent) bool {
	return e.DurationMS > 0 && e.PositionMS*10 >= e.DurationMS*8
}

// Negatives turns listening history and ratings (0–10) into avoid weights (0–1) per track.
// artistTracks lists each artist's tracks, for the artist rule (nil skips it).
func Negatives(events []PlayEvent, ratings map[int64]float64, artistTracks map[int64][]int64) map[int64]float64 {
	score := map[int64]float64{}
	artistSkips := map[int64]float64{}
	artistFull := map[int64]float64{}
	for _, e := range events {
		d := decay(e.Age)
		if d == 0 {
			continue
		}
		switch {
		case quickSkip(e):
			score[e.ItemID] += skipWeight * d
			if e.ArtistID != 0 {
				artistSkips[e.ArtistID] += d
			}
		case fullPlay(e):
			score[e.ItemID] -= fullWeight * d
			if e.ArtistID != 0 {
				artistFull[e.ArtistID] += d
			}
		}
	}
	out := map[int64]float64{}
	for id, s := range score {
		if s > 0 {
			out[id] = min(1, s)
		}
	}
	for artist, skips := range artistSkips {
		extra := skips - artistFull[artist] - 2 // two skips are allowed: moods change
		if extra <= 0 {
			continue
		}
		p := min(artistMax, artistPenalty*extra)
		for _, id := range artistTracks[artist] {
			out[id] = max(out[id], p)
		}
	}
	for id, r := range ratings {
		switch {
		case r <= 4:
			out[id] = 1
		case r >= 8:
			delete(out, id)
		}
	}
	return out
}

// learned reads the last 30 days of track plays and every track rating and applies
// Negatives.
func (s *Service) learned(ctx context.Context, userID int64) map[int64]float64 {
	var events []PlayEvent
	rows, err := s.DB.QueryContext(ctx, `SELECT h.item_id, COALESCE(i.grandparent_id, 0), h.started_at, COALESCE(h.position_ms, 0), COALESCE(i.duration_ms, 0)
		FROM play_history h JOIN items i ON i.id = h.item_id AND i.type = 'track'
		WHERE h.user_id = ? AND h.stopped_at IS NOT NULL AND h.started_at > strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '-30 days')`, userID)
	if err == nil {
		now := time.Now()
		for rows.Next() {
			var e PlayEvent
			var at string
			if rows.Scan(&e.ItemID, &e.ArtistID, &at, &e.PositionMS, &e.DurationMS) != nil {
				continue
			}
			if t, err := time.Parse(time.RFC3339Nano, at); err == nil {
				e.Age = now.Sub(t)
			}
			events = append(events, e)
		}
		rows.Close()
	}
	ratings := map[int64]float64{}
	rows, err = s.DB.QueryContext(ctx, `SELECT s.item_id, s.rating FROM user_item_state s JOIN items i ON i.id = s.item_id AND i.type = 'track'
		WHERE s.user_id = ? AND s.rating IS NOT NULL`, userID)
	if err == nil {
		for rows.Next() {
			var id int64
			var r float64
			if rows.Scan(&id, &r) == nil {
				ratings[id] = r
			}
		}
		rows.Close()
	}
	artists := map[int64]bool{}
	for _, e := range events {
		if quickSkip(e) && e.ArtistID != 0 {
			artists[e.ArtistID] = true
		}
	}
	var byArtist map[int64][]int64
	if len(artists) > 0 && s.Index != nil {
		byArtist = map[int64][]int64{}
		for _, t := range s.Index.Where(func(t *Track) bool { return artists[t.ArtistID] }) {
			byArtist[t.ArtistID] = append(byArtist[t.ArtistID], t.ItemID)
		}
	}
	return Negatives(events, ratings, byArtist)
}
