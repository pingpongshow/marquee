package items

import (
	"context"
	"database/sql"
	"slices"
	"strings"
)

// topRating is the least rating that makes a song one of someone's top rated: 3.5 stars.
const topRating = 7.0

// songSignals is what puts a song among an artist's top songs.
type songSignals struct {
	id       int64
	title    string
	favorite bool
	rating   float64 // 0 = not rated
	rank     int     // on ListenBrainz's list for the artist; 0 = not on it
	plays    int     // by everyone on this server
}

// rankSongs keeps the songs that are someone's favourite, rated 3.5 stars or more, or popular,
// best first. Being a favourite counts most, then a high rating; popularity orders the rest
// and comes from whichever of ListenBrainz's list and this server's plays ranks a song higher.
func rankSongs(songs []songSignals) []int64 {
	maxRank, maxPlays := 0, 0
	for _, t := range songs {
		maxRank, maxPlays = max(maxRank, t.rank), max(maxPlays, t.plays)
	}
	score := func(t songSignals) float64 {
		v := 0.0
		if t.favorite {
			v += 1
		}
		if t.rating >= topRating {
			v += (t.rating - 5) / 5
		}
		pop := 0.0
		if t.rank > 0 {
			pop = 1 - float64(t.rank-1)/float64(maxRank)
		}
		if t.plays > 0 {
			pop = max(pop, float64(t.plays)/float64(maxPlays))
		}
		return v + pop
	}
	kept := []songSignals{}
	for _, t := range songs {
		if t.favorite || t.rating >= topRating || t.rank > 0 || t.plays > 0 {
			kept = append(kept, t)
		}
	}
	scores := make(map[int64]float64, len(kept))
	for _, t := range kept {
		scores[t.id] = score(t)
	}
	slices.SortStableFunc(kept, func(a, b songSignals) int {
		if d := scores[b.id] - scores[a.id]; d != 0 {
			if d > 0 {
				return 1
			}
			return -1
		}
		return strings.Compare(strings.ToLower(a.title), strings.ToLower(b.title))
	})
	out := make([]int64, len(kept))
	for i, t := range kept {
		out[i] = t.id
	}
	return out
}

// TopSongs returns a page of an artist's top songs for acc (see rankSongs), and how many there
// are in all.
func (s *Store) TopSongs(ctx context.Context, acc Access, artistID int64, offset, limit int) ([]Summary, int, error) {
	ac, aargs := acc.clause()
	var songs []songSignals
	args := append([]any{acc.UserID, artistID, artistID}, aargs...)
	err := eachRow(ctx, s.db, `SELECT i.id, i.title, COALESCE(u.favorite_at IS NOT NULL, 0), COALESCE(u.rating, 0), COALESCE(mp.rank, 0),
		(SELECT COALESCE(SUM(a.play_count), 0) FROM user_item_state a WHERE a.item_id = i.id)`+summaryFrom+`
		LEFT JOIN user_item_state u ON u.item_id = i.id AND u.user_id = ?
		LEFT JOIN music_popular mp ON mp.track_id = i.id AND mp.artist_id = ?
		WHERE i.type = 'track' AND i.grandparent_id = ? AND `+ac, args, func(r *sql.Rows) error {
		var t songSignals
		if err := r.Scan(&t.id, &t.title, &t.favorite, &t.rating, &t.rank, &t.plays); err != nil {
			return err
		}
		songs = append(songs, t)
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	ids := rankSongs(songs)
	total := len(ids)
	list, err := s.ByIDs(ctx, acc, ids[min(offset, total):min(offset+limit, total)])
	return list, total, err
}

// RadioWithoutAnalysis picks songs for the radio of a track, album or artist that Soundprint
// hasn't analysed: one of the artist's for every two by similar artists or in the same genres
// (just the artist's when there are no others). A track's own radio starts with that track.
func (s *Store) RadioWithoutAnalysis(ctx context.Context, acc Access, itemID int64, exclude map[int64]bool, limit int) ([]int64, error) {
	var typ string
	var artist int64
	err := s.db.QueryRowContext(ctx, `SELECT type, CASE type WHEN 'track' THEN COALESCE(grandparent_id, 0) WHEN 'album' THEN COALESCE(parent_id, 0) ELSE id END
		FROM items WHERE id = ?`, itemID).Scan(&typ, &artist)
	if err != nil {
		return nil, err
	}
	if typ != "track" && typ != "album" && typ != "artist" {
		return nil, nil
	}
	ac, aargs := acc.clause()
	// Random and capped, since an enormous genre needn't all be looked at.
	var own, others []int64
	rows, err := s.db.QueryContext(ctx, `SELECT i.id, COALESCE(i.grandparent_id = ?, 0)`+summaryFrom+`
		WHERE i.type = 'track' AND i.id != ? AND `+ac+` AND (i.grandparent_id = ?
			OR i.grandparent_id IN (SELECT similar_id FROM music_similar_artists WHERE artist_id = ?)
			OR i.id IN (SELECT e.id FROM items e JOIN item_tags et ON et.item_id IN (e.id, e.parent_id, e.grandparent_id) JOIN tags t ON t.id = et.tag_id AND t.kind = 'genre'
				WHERE e.type = 'track' AND et.tag_id IN (
					SELECT st.tag_id FROM item_tags st JOIN tags sg ON sg.id = st.tag_id AND sg.kind = 'genre'
					WHERE st.item_id = ? OR st.item_id IN (SELECT id FROM items WHERE parent_id = ? OR grandparent_id = ?))))
		ORDER BY RANDOM() LIMIT 5000`, append(append([]any{artist, itemID}, aargs...), artist, artist, artist, artist, artist)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var mine bool
		if err := rows.Scan(&id, &mine); err != nil {
			return nil, err
		}
		if exclude[id] {
			continue
		}
		if mine {
			own = append(own, id)
		} else {
			others = append(others, id)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]int64, 0, limit)
	if typ == "track" && !exclude[itemID] && s.Visible(ctx, acc, itemID) == nil {
		out = append(out, itemID)
	}
	for i := 0; len(out) < limit && (len(own) > 0 || len(others) > 0); i++ {
		if (i%3 == 0 && len(own) > 0) || len(others) == 0 {
			out, own = append(out, own[0]), own[1:]
		} else {
			out, others = append(out, others[0]), others[1:]
		}
	}
	return out, nil
}
