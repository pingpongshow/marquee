package soundprint

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"time"
)

func parseTraits(s string) *Traits {
	if s == "" {
		return nil
	}
	var t Traits
	if json.Unmarshal([]byte(s), &t) != nil {
		return nil
	}
	return &t
}

// trait reads one trait by name (0 when the track has no traits).
func (t *Traits) trait(name string) float64 {
	if t == nil {
		return 0
	}
	switch name {
	case "happy":
		return t.Happy
	case "sad":
		return t.Sad
	case "tender":
		return t.Tender
	case "exciting":
		return t.Exciting
	case "angry":
		return t.Angry
	case "scary":
		return t.Scary
	case "dance":
		return t.Dance
	case "vocal":
		return t.Vocal
	case "electronic":
		return t.Electronic
	case "acoustic":
		return t.Acoustic
	case "ambient":
		return t.Ambient
	}
	return 0
}

// TraitRanks turns the tracks' raw trait scores into ranks (0 the least of the tracks, 1 the
// most): the classifier's scores aren't on a common scale (a "sad" 0.2 can be the saddest
// track in the library), so a trait counts by how it compares. Tracks without traits are
// left out.
func TraitRanks(tracks []*Track, name string) map[int64]float64 {
	var with []*Track
	for _, t := range tracks {
		if t.Traits != nil {
			with = append(with, t)
		}
	}
	out := make(map[int64]float64, len(with))
	if len(with) < 2 {
		for _, t := range with {
			out[t.ItemID] = 0.5
		}
		return out
	}
	sort.Slice(with, func(i, j int) bool { return with[i].Traits.trait(name) < with[j].Traits.trait(name) })
	for i, t := range with {
		out[t.ItemID] = float64(i) / float64(len(with)-1)
	}
	return out
}

// traitsPass measures the traits of analysed tracks that don't have them (or have an older
// analysis's), when the sidecar offers traits.
func (s *Service) traitsPass(ctx context.Context, h Health) (string, error) {
	if h.Traits == "" {
		return "", nil
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT sp.item_id, f.path FROM soundprint sp JOIN media_files f ON f.id = sp.file_id AND f.available = 1
		WHERE sp.embedding IS NOT NULL AND (sp.traits_model IS NULL OR sp.traits_model != ?) ORDER BY sp.item_id`, h.Traits)
	if err != nil {
		return "", err
	}
	var todo []pending
	for rows.Next() {
		var p pending
		if rows.Scan(&p.itemID, &p.path) == nil {
			todo = append(todo, p)
		}
	}
	rows.Close()
	if len(todo) == 0 {
		return "", nil
	}
	s.setPhase("Measuring what tracks are like", len(todo))
	start := time.Now()
	done, failed := 0, 0
	const batch = 32
	for i := 0; i < len(todo); i += batch {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if !s.Enabled() {
			break
		}
		chunk := todo[i:min(i+batch, len(todo))]
		paths := make([]string, len(chunk))
		for j, p := range chunk {
			paths[j] = p.path
		}
		model, results, err := s.Client.Traits(ctx, paths)
		if err != nil {
			return "", fmt.Errorf("traits after %d tracks: %w", done, err)
		}
		for j, r := range results {
			if j >= len(chunk) {
				break
			}
			id := chunk[j].itemID
			if r.Error != "" {
				failed++
				// Marked so the file isn't retried every run; it keeps no traits.
				s.DB.ExecContext(ctx, `UPDATE soundprint SET traits = NULL, traits_model = ? WHERE item_id = ?`, model, id)
				continue
			}
			b, _ := json.Marshal(r.Traits)
			if _, err := s.DB.ExecContext(ctx, `UPDATE soundprint SET traits = ?, traits_model = ? WHERE item_id = ?`, string(b), model, id); err != nil {
				return "", err
			}
			tr := r.Traits
			s.Index.update(id, func(t *Track) { t.Traits = &tr })
			done++
		}
		s.setDone(min(i+batch, len(todo)))
	}
	slog.Info("soundprint traits measured", "tracks", done, "failed", failed, "took", time.Since(start).Round(time.Second))
	return fmt.Sprintf("measured what %d tracks are like", done), nil
}
