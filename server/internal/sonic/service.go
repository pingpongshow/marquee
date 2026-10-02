package sonic

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"strings"
	"sync"
	"time"
)

var (
	ErrUnavailable = errors.New("sonic analysis isn't running")
	ErrNotAnalyzed = errors.New("this hasn't been sonically analysed yet")
)

// Service owns the index and the analysis task.
type Service struct {
	DB     *sql.DB
	Client *Client
	Index  *Index
	// Enabled reports the Music → Sonic analysis setting.
	Enabled func() bool

	mu      sync.Mutex
	running bool
	done    int
	total   int
	model   string
}

// Status is what Settings and the activity indicator show.
type Status struct {
	Enabled, Available, Running bool
	Model, Device               string
	Analyzed, Total, Failed     int
	Progress                    int // tracks done in the current run
	RunTotal                    int
}

func (s *Service) Status(ctx context.Context) Status {
	st := Status{Enabled: s.Enabled()}
	if h, err := s.Client.Health(ctx); err == nil {
		st.Available, st.Model, st.Device = true, h.Model, h.Device
	}
	s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM items WHERE type = 'track'`).Scan(&st.Total)
	s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FILTER (WHERE embedding IS NOT NULL), COUNT(*) FILTER (WHERE error IS NOT NULL) FROM sonic`).Scan(&st.Analyzed, &st.Failed)
	s.mu.Lock()
	st.Running, st.Progress, st.RunTotal = s.running, s.done, s.total
	s.mu.Unlock()
	return st
}

type pending struct {
	itemID, fileID int64
	path           string
}

// Analyze embeds every track that isn't analysed yet (or whose file changed). It runs as
// a scheduled task; report receives progress.
func (s *Service) Analyze(ctx context.Context) (string, error) {
	if !s.Enabled() {
		return "Sonic analysis is turned off", nil
	}
	// The sidecar loads its model at startup; give it a few minutes.
	h, err := s.Client.Health(ctx)
	for wait := 0; err != nil && wait < 36; wait++ {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(5 * time.Second):
		}
		h, err = s.Client.Health(ctx)
	}
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return "Already running", nil
	}
	s.running, s.done, s.model = true, 0, h.Model
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
	}()

	var summary []string
	for pass := 0; pass < 5; pass++ {
		msg, n, err := s.analyzePass(ctx, h)
		if err != nil {
			return "", err
		}
		if n == 0 && pass > 0 {
			break
		}
		summary = append(summary, msg)
		if n == 0 {
			break
		}
	}
	return strings.Join(summary, "; "), nil
}

// analyzePass analyses everything pending now; tracks added meanwhile are picked up by the
// next pass, so a scan that finishes during a run isn't missed.
func (s *Service) analyzePass(ctx context.Context, h Health) (string, int, error) {
	var todo []pending
	rows, err := s.DB.QueryContext(ctx, `SELECT i.id, f.id, f.path FROM items i
		JOIN media_versions v ON v.item_id = i.id JOIN media_files f ON f.version_id = v.id AND f.part_index = 0
		LEFT JOIN sonic s ON s.item_id = i.id
		WHERE i.type = 'track' AND f.available = 1
		  AND (s.item_id IS NULL OR s.file_id != f.id OR (s.model != ? AND s.error IS NULL))
		GROUP BY i.id ORDER BY i.id`, h.Model)
	if err != nil {
		return "", 0, err
	}
	for rows.Next() {
		var p pending
		if err := rows.Scan(&p.itemID, &p.fileID, &p.path); err != nil {
			rows.Close()
			return "", 0, err
		}
		todo = append(todo, p)
	}
	rows.Close()
	s.mu.Lock()
	s.total = len(todo)
	s.mu.Unlock()
	if len(todo) == 0 {
		return fmt.Sprintf("All %d tracks analysed", s.Index.Len()), 0, nil
	}
	slog.Info("sonic analysis starting", "tracks", len(todo), "model", h.Model, "device", h.Device)
	start := time.Now()
	analysed, failed := 0, 0
	const batch = 32
	for i := 0; i < len(todo); i += batch {
		if ctx.Err() != nil {
			return "", 0, ctx.Err()
		}
		if !s.Enabled() {
			return fmt.Sprintf("Stopped (turned off) after %d tracks", analysed), 0, nil
		}
		chunk := todo[i:min(i+batch, len(todo))]
		paths := make([]string, len(chunk))
		for j, p := range chunk {
			paths[j] = p.path
		}
		model, results, err := s.analyzeBatch(ctx, paths)
		if err != nil {
			return "", 0, fmt.Errorf("after %d tracks: %w", analysed, err)
		}
		for j, r := range results {
			if j >= len(chunk) {
				break
			}
			p := chunk[j]
			if r.Error != "" || len(r.Embedding) == 0 {
				failed++
				s.DB.ExecContext(ctx, `INSERT INTO sonic(item_id, file_id, model, error) VALUES (?, ?, ?, ?)
					ON CONFLICT(item_id) DO UPDATE SET file_id = excluded.file_id, model = excluded.model, embedding = NULL,
					error = excluded.error, analyzed_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')`, p.itemID, p.fileID, model, nonEmpty(r.Error, "no embedding"))
				continue
			}
			_, err := s.DB.ExecContext(ctx, `INSERT INTO sonic(item_id, file_id, model, embedding, bpm, musical_key, mode, energy) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
				ON CONFLICT(item_id) DO UPDATE SET file_id = excluded.file_id, model = excluded.model, embedding = excluded.embedding,
				bpm = excluded.bpm, musical_key = excluded.musical_key, mode = excluded.mode, energy = excluded.energy, error = NULL,
				analyzed_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')`,
				p.itemID, p.fileID, model, encode(r.Embedding), r.BPM, r.Key, r.Mode, r.Energy)
			if err != nil {
				return "", 0, err
			}
			analysed++
			s.indexOne(ctx, p.itemID, r)
		}
		s.mu.Lock()
		s.done = min(i+batch, len(todo))
		s.mu.Unlock()
	}
	msg := fmt.Sprintf("Analysed %d tracks in %s", analysed, time.Since(start).Round(time.Second))
	if failed > 0 {
		msg += fmt.Sprintf(" (%d unreadable)", failed)
	}
	slog.Info("sonic analysis finished", "analysed", analysed, "failed", failed, "took", time.Since(start).Round(time.Second))
	return msg, len(todo), nil
}

// analyzeBatch analyses paths together, and if the batch fails (one bad file can break
// it), one at a time so a single file can't stall the whole library. It only fails when
// the sidecar itself is unreachable.
func (s *Service) analyzeBatch(ctx context.Context, paths []string) (string, []Analysis, error) {
	model, results, err := s.Client.Analyze(ctx, paths)
	if err == nil || len(paths) == 1 || ctx.Err() != nil {
		return model, results, err
	}
	slog.Warn("sonic batch failed; retrying files one by one", "err", err)
	results = make([]Analysis, len(paths))
	ok := false
	for i, p := range paths {
		m, r, err := s.Client.Analyze(ctx, []string{p})
		if err != nil || len(r) == 0 {
			if _, herr := s.Client.Health(ctx); herr != nil {
				return "", nil, herr // the sidecar went away: don't mark files as unreadable
			}
			results[i] = Analysis{Path: p, Error: fmt.Sprint("analysis failed: ", err)}
			continue
		}
		model, results[i], ok = m, r[0], true
	}
	if !ok {
		return "", nil, err
	}
	return model, results, nil
}

func nonEmpty(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

func (s *Service) indexOne(ctx context.Context, itemID int64, r Analysis) {
	t := &Track{ItemID: itemID, Vec: r.Embedding}
	if r.BPM != nil {
		t.BPM = *r.BPM
	}
	if r.Energy != nil {
		t.Energy = *r.Energy
	}
	if r.Key != nil {
		t.Key = *r.Key
	}
	if r.Mode != nil {
		t.Mode = *r.Mode
	}
	s.DB.QueryRowContext(ctx, `SELECT COALESCE(i.parent_id, 0), COALESCE(i.grandparent_id, 0), i.library_id, COALESCE(i.year, a.year, 0)
		FROM items i LEFT JOIN items a ON a.id = i.parent_id WHERE i.id = ?`, itemID).Scan(&t.AlbumID, &t.ArtistID, &t.LibraryID, &t.Year)
	s.Index.Put(t)
}

// ---------- listening history ----------

// Avoid weights tracks the user played recently (so radios feel fresh) or skipped.
func (s *Service) Avoid(ctx context.Context, userID int64) map[int64]float64 {
	out := map[int64]float64{}
	rows, err := s.DB.QueryContext(ctx, `SELECT item_id, last_viewed_at FROM user_item_state
		WHERE user_id = ? AND last_viewed_at > strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '-3 days')`, userID)
	if err == nil {
		for rows.Next() {
			var id int64
			var at string
			rows.Scan(&id, &at)
			out[id] = 0.6
		}
		rows.Close()
	}
	// Skipped: playback stopped within 30 s (and before half way) in the last 60 days.
	rows, err = s.DB.QueryContext(ctx, `SELECT h.item_id, COUNT(*) FROM play_history h JOIN items i ON i.id = h.item_id AND i.type = 'track'
		WHERE h.user_id = ? AND h.stopped_at IS NOT NULL AND h.started_at > strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '-60 days')
		  AND COALESCE(h.position_ms, 0) < MIN(30000, COALESCE(i.duration_ms, 60000) / 2) GROUP BY h.item_id`, userID)
	if err == nil {
		for rows.Next() {
			var id int64
			var n int
			rows.Scan(&id, &n)
			out[id] = min(1, out[id]+0.35*float64(n))
		}
		rows.Close()
	}
	// Low ratings are avoided too (ratings are 0–10; 2 stars and below).
	rows, err = s.DB.QueryContext(ctx, `SELECT item_id FROM user_item_state WHERE user_id = ? AND rating IS NOT NULL AND rating <= 4`, userID)
	if err == nil {
		for rows.Next() {
			var id int64
			rows.Scan(&id)
			out[id] = 1
		}
		rows.Close()
	}
	return out
}

// Liked returns the user's taste: tracks played more than once or rated 4+ stars, most
// recent first (at most 2,000).
func (s *Service) Liked(ctx context.Context, userID int64) []*Track {
	rows, err := s.DB.QueryContext(ctx, `SELECT item_id FROM user_item_state
		WHERE user_id = ? AND (play_count > 1 OR rating >= 8 OR (play_count > 0 AND last_viewed_at > strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '-90 days')))
		ORDER BY last_viewed_at DESC LIMIT 2000`, userID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []*Track
	for rows.Next() {
		var id int64
		rows.Scan(&id)
		if t := s.Index.Get(id); t != nil {
			out = append(out, t)
		}
	}
	return out
}

// ---------- seeds ----------

// tracksOf returns the analysed tracks of an item (itself, an album's or an artist's).
func (s *Service) tracksOf(ctx context.Context, itemID int64) ([]*Track, string, error) {
	var typ string
	if err := s.DB.QueryRowContext(ctx, `SELECT type FROM items WHERE id = ?`, itemID).Scan(&typ); err != nil {
		return nil, "", err
	}
	if typ == "track" {
		if t := s.Index.Get(itemID); t != nil {
			return []*Track{t}, typ, nil
		}
		return nil, typ, ErrNotAnalyzed
	}
	col := map[string]string{"album": "parent_id", "artist": "grandparent_id"}[typ]
	if col == "" {
		return nil, typ, fmt.Errorf("can't build music from a %s", typ)
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT id FROM items WHERE type = 'track' AND `+col+` = ?`, itemID)
	if err != nil {
		return nil, typ, err
	}
	defer rows.Close()
	var out []*Track
	for rows.Next() {
		var id int64
		rows.Scan(&id)
		if t := s.Index.Get(id); t != nil {
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		return nil, typ, ErrNotAnalyzed
	}
	return out, typ, nil
}

// TrackIDs flattens tracks to item ids.
func TrackIDs(ts []*Track) []int64 {
	out := make([]int64, len(ts))
	for i, t := range ts {
		out[i] = t.ItemID
	}
	return out
}

// Station is a generated list with a name.
type Station struct {
	Title string
	IDs   []int64
}

// RadioFromItem starts an endless-feeling station from a track, album or artist.
func (s *Service) RadioFromItem(ctx context.Context, itemID int64, title string, o Options) (Station, error) {
	ts, typ, err := s.tracksOf(ctx, itemID)
	if err != nil {
		return Station{}, err
	}
	anchor := Mean(ts)
	first := ts[o.rng().IntN(len(ts))]
	if typ == "track" {
		first = ts[0]
	}
	return Station{Title: title + " Radio", IDs: TrackIDs(s.Index.Flow(anchor, first, o))}, nil
}

// RadioFromVector starts a station from any sound (a genre's average, a mood prompt…).
func (s *Service) RadioFromVector(v []float32, title string, o Options) Station {
	return Station{Title: title, IDs: TrackIDs(s.Index.Flow(v, nil, o))}
}

// TextVector embeds a prompt (Muse, moods).
func (s *Service) TextVector(ctx context.Context, prompt string) ([]float32, error) {
	vs, err := s.Client.EmbedText(ctx, []string{prompt})
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	if len(vs) == 0 {
		return nil, ErrUnavailable
	}
	return normalize(vs[0]), nil
}

// Muse builds a playlist from a description ("rainy Sunday jazz").
func (s *Service) Muse(ctx context.Context, prompt string, n int, o Options) (Station, error) {
	v, err := s.TextVector(ctx, prompt)
	if err != nil {
		return Station{}, err
	}
	return Station{Title: prompt, IDs: TrackIDs(s.Index.Pick(v, n, o))}, nil
}

// MeanOf averages the analysed tracks among ids (a genre's or decade's sound).
func (s *Service) MeanOf(ids []int64) []float32 {
	var ts []*Track
	for _, id := range ids {
		if t := s.Index.Get(id); t != nil {
			ts = append(ts, t)
		}
	}
	return Mean(ts)
}

// Mix is one of a listener's mixes.
type Mix struct {
	Station
	SeedIDs     []int64 // tracks from the listener's taste that shaped it
	ID          string  // stable for the day: daily-1, discovery, rediscover, new, decade
	Description string  // what it is, when not just its artists
}

type mixCacheKey struct {
	user  int64
	day   string
	scope string // which libraries the mixes were made from
}

var (
	mixMu    sync.Mutex
	mixCache = map[mixCacheKey][]Mix{}
)

// DailyMixes clusters the listener's taste into up to four groups and builds a 25-track
// mix around each, mixing favourites with similar tracks they haven't played lately, then
// adds the history-based mixes (MUSIC-17). Mixes stay the same for the day.
//
// scope names what keep allows (e.g. the library id), so different filters get their own mixes.
func (s *Service) DailyMixes(ctx context.Context, userID int64, scope string, keep Filter) []Mix {
	key := mixCacheKey{userID, time.Now().Format("2006-01-02"), scope}
	mixMu.Lock()
	if m, ok := mixCache[key]; ok {
		mixMu.Unlock()
		return m
	}
	mixMu.Unlock()
	liked := s.Liked(ctx, userID)
	var keepLiked []*Track
	for _, t := range liked {
		if keep == nil || keep(t) {
			keepLiked = append(keepLiked, t)
		}
	}
	if len(keepLiked) < 8 {
		return nil
	}
	seed := uint64(userID)*1_000_003 + uint64(time.Now().YearDay())
	r := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	k := min(4, max(1, len(keepLiked)/15))
	avoid := s.Avoid(ctx, userID)
	var mixes []Mix
	used := map[int64]bool{}
	for i, c := range Clusters(keepLiked, k, r) {
		o := Options{Keep: keep, Avoid: avoid, Exclude: used, Rand: r}
		ts := s.Index.Pick(c, 25, o)
		for _, t := range ts {
			used[t.ItemID] = true
		}
		mixes = append(mixes, Mix{Station: Station{Title: fmt.Sprintf("Daily Mix %d", i+1), IDs: TrackIDs(ts)}, ID: fmt.Sprintf("daily-%d", i+1)})
	}
	mixes = append(mixes, s.historyMixes(ctx, userID, keep, keepLiked, avoid, r)...)
	mixMu.Lock()
	for k := range mixCache {
		if k.day != key.day {
			delete(mixCache, k) // yesterday's mixes
		}
	}
	mixCache[key] = mixes
	mixMu.Unlock()
	return mixes
}

// historyMixes are Plexamp-style mixes from what someone has (and hasn't) played:
// Discovery (never-played tracks close to their taste, favouring artists they rarely play),
// Rediscover (favourites not played for six months), New Music (recent additions that fit
// their taste) and their favourite decade.
func (s *Service) historyMixes(ctx context.Context, userID int64, keep Filter, liked []*Track, avoid map[int64]float64, r *rand.Rand) []Mix {
	taste := Mean(liked)
	if taste == nil {
		return nil
	}
	played := map[int64]bool{}
	artistPlays := map[int64]int{}
	rows, err := s.DB.QueryContext(ctx, `SELECT s.item_id, s.play_count, COALESCE(i.grandparent_id, 0) FROM user_item_state s JOIN items i ON i.id = s.item_id
		WHERE s.user_id = ? AND i.type = 'track' AND s.play_count > 0`, userID)
	if err == nil {
		for rows.Next() {
			var id, artist int64
			var n int
			if rows.Scan(&id, &n, &artist) == nil {
				played[id] = true
				artistPlays[artist] += n
			}
		}
		rows.Close()
	}
	allowed := func(t *Track) bool { return keep == nil || keep(t) }
	var out []Mix

	// Discovery: unplayed, near their taste; artists they've played a lot count against.
	disc := Options{Keep: func(t *Track) bool { return allowed(t) && !played[t.ItemID] }, Avoid: map[int64]float64{}, Rand: r}
	for _, t := range s.Index.Where(disc.Keep) {
		if n := artistPlays[t.ArtistID]; n > 0 {
			disc.Avoid[t.ItemID] = min(0.8, float64(n)/40)
		}
	}
	if ts := s.Index.Pick(taste, 25, disc); len(ts) >= 10 {
		out = append(out, Mix{Station: Station{Title: "Discovery Mix", IDs: TrackIDs(ts)}, ID: "discovery",
			Description: "Tracks in your library you haven't played yet, close to what you love"})
	}

	// Rediscover: favourites (4+ stars, or played 3+ times) not played for six months.
	var old []*Track
	rows, err = s.DB.QueryContext(ctx, `SELECT item_id FROM user_item_state WHERE user_id = ? AND (rating >= 8 OR play_count >= 3)
		AND (last_viewed_at IS NULL OR last_viewed_at < strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '-180 days'))`, userID)
	if err == nil {
		for rows.Next() {
			var id int64
			if rows.Scan(&id) == nil {
				if t := s.Index.Get(id); t != nil && allowed(t) {
					old = append(old, t)
				}
			}
		}
		rows.Close()
	}
	if len(old) >= 10 {
		r.Shuffle(len(old), func(i, j int) { old[i], old[j] = old[j], old[i] })
		old = old[:min(25, len(old))]
		out = append(out, Mix{Station: Station{Title: "Rediscover", IDs: TrackIDs(order(old, Mean(old)))}, ID: "rediscover",
			Description: "Favourites you haven't played in a while"})
	}

	// New Music: added in the last 60 days, near their taste.
	fresh := map[int64]bool{}
	rows, err = s.DB.QueryContext(ctx, `SELECT id FROM items WHERE type = 'track' AND added_at > strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '-60 days')`)
	if err == nil {
		for rows.Next() {
			var id int64
			if rows.Scan(&id) == nil {
				fresh[id] = true
			}
		}
		rows.Close()
	}
	if len(fresh) >= 10 {
		o := Options{Keep: func(t *Track) bool { return allowed(t) && fresh[t.ItemID] }, Avoid: avoid, Rand: r}
		if ts := s.Index.Pick(taste, 25, o); len(ts) >= 10 {
			out = append(out, Mix{Station: Station{Title: "New Music Mix", IDs: TrackIDs(ts)}, ID: "new",
				Description: "Recently added music that fits your taste"})
		}
	}

	// Their favourite decade.
	decades := map[int]int{}
	for _, t := range liked {
		if t.Year > 1900 {
			decades[t.Year/10*10]++
		}
	}
	best, n := 0, 0
	for d, c := range decades {
		if c > n || (c == n && d > best) {
			best, n = d, c
		}
	}
	if n >= 10 {
		o := Options{Keep: func(t *Track) bool { return allowed(t) && t.Year/10*10 == best }, Avoid: avoid, Rand: r}
		var inDecade []*Track
		for _, t := range liked {
			if t.Year/10*10 == best {
				inDecade = append(inDecade, t)
			}
		}
		if ts := s.Index.Pick(Mean(inDecade), 25, o); len(ts) >= 10 {
			label := fmt.Sprintf("%ds", best%100)
			if best < 1950 || best >= 2000 {
				label = fmt.Sprintf("%ds", best)
			}
			out = append(out, Mix{Station: Station{Title: label + " Mix", IDs: TrackIDs(ts)}, ID: "decade",
				Description: "Your favourite decade, with more like it"})
		}
	}
	return out
}
