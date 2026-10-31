package soundprint

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrUnavailable = errors.New("Soundprint analysis isn't running")
	ErrNotAnalyzed = errors.New("this hasn't had its Soundprint analysis yet")
)

// Service owns the index and the analysis task.
type Service struct {
	DB     *sql.DB
	Client *Client
	Index  *Index
	// Enabled reports the Music → Soundprint analysis setting.
	Enabled func() bool
	// Learn reports the "learn from skips and ratings" setting (nil = off): Avoid then uses
	// Negatives instead of the older fixed rules.
	Learn func() bool
	// Model is the sound model chosen in Settings (ModelGeneral or ModelMusic; nil = the one
	// in use): another than the one in use is tried on the library first (see models.go).
	Model func() string

	mu      sync.Mutex
	running bool
	phase   string // what the run is doing ("" = analysing new tracks)
	done    int
	total   int
	model   string

	// What listeners do (coplays.go) and say elsewhere (community.go), loaded from the
	// database and replaced whole when rebuilt.
	learnMu   sync.RWMutex
	coplays   map[int64]map[int64]float64
	similar   map[int64]map[int64]float64 // artist → similar artist → 0–1
	tags      map[int64]map[string]float64
	tagsByArt map[int64]map[string]float64
}

// Status is what Settings and the activity indicator show.
type Status struct {
	Enabled, Available, Running bool
	Model, Device               string
	Analyzed, Total, Failed     int
	Progress                    int    // tracks done in the current stage of the run
	RunTotal                    int    // tracks in that stage
	Phase                       string // the stage ("" = analysing new tracks)
	// SoundModel is the model in use (ModelGeneral or ModelMusic); Trial the one being
	// tried, when another is chosen in Settings.
	SoundModel string
	Trial      *Trial
	// Traits and Languages count the analysed tracks with traits and with a language.
	Traits, Languages int
}

func (s *Service) Status(ctx context.Context) Status {
	st := Status{Enabled: s.Enabled()}
	if h, err := s.Client.Health(ctx); err == nil {
		st.Available, st.Model, st.Device = true, h.Model, h.Device
	}
	s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM items WHERE type = 'track'`).Scan(&st.Total)
	s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FILTER (WHERE embedding IS NOT NULL), COUNT(*) FILTER (WHERE error IS NOT NULL),
		COUNT(*) FILTER (WHERE embedding IS NOT NULL AND traits IS NOT NULL), COUNT(*) FILTER (WHERE embedding IS NOT NULL AND language != '')
		FROM soundprint`).Scan(&st.Analyzed, &st.Failed, &st.Traits, &st.Languages)
	st.SoundModel, st.Trial = s.ActiveModel(ctx), s.Trial(ctx)
	s.mu.Lock()
	st.Running, st.Progress, st.RunTotal, st.Phase = s.running, s.done, s.total, s.phase
	s.mu.Unlock()
	return st
}

// Progress reports the running stage's progress (the task's activity indicator).
func (s *Service) Progress() (int, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return 0, 0
	}
	return s.done, s.total
}

// setPhase starts a stage of the run over total tracks.
func (s *Service) setPhase(phase string, total int) {
	s.mu.Lock()
	s.phase, s.done, s.total = phase, 0, total
	s.mu.Unlock()
}

func (s *Service) setDone(n int) {
	s.mu.Lock()
	s.done = n
	s.mu.Unlock()
}

type pending struct {
	itemID, fileID int64
	path           string
}

// Analyze embeds every track that isn't analysed yet (or whose file changed) with the sound
// model in use, then measures what tracks are like (traits), reads their languages from
// their lyrics, and tries the model chosen in Settings when it isn't the one in use. It runs
// as a scheduled task.
func (s *Service) Analyze(ctx context.Context) (string, error) {
	if !s.Enabled() {
		return "Soundprint analysis is turned off", nil
	}
	// The sidecar may still be starting; give it a few minutes.
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
	active := s.ActiveModel(ctx)
	id := h.ModelID(active)
	if id == "" {
		s.mu.Unlock()
		return "", fmt.Errorf("%w: it doesn't offer the %s sound model in use (update the Soundprint container)", ErrUnavailable, active)
	}
	s.running, s.done, s.model, s.phase = true, 0, id, ""
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.running, s.phase = false, ""
		s.mu.Unlock()
	}()

	var summary []string
	for pass := 0; pass < 5; pass++ {
		s.setPhase("", 0)
		msg, n, err := s.analyzePass(ctx, active, id)
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
	if msg, err := s.traitsPass(ctx, h); err != nil {
		return "", err
	} else if msg != "" {
		summary = append(summary, msg)
	}
	if n, err := s.languagePass(ctx); err != nil {
		return "", err
	} else if n > 0 {
		summary = append(summary, fmt.Sprintf("read the language of %d tracks' lyrics", n))
	}
	if msg, err := s.trialPass(ctx, h); err != nil {
		return "", err
	} else if msg != "" {
		summary = append(summary, msg)
	}
	return strings.Join(summary, "; "), nil
}

// analyzePass analyses everything pending now; tracks added meanwhile are picked up by the
// next pass, so a scan that finishes during a run isn't missed.
func (s *Service) analyzePass(ctx context.Context, active, modelID string) (string, int, error) {
	var todo []pending
	rows, err := s.DB.QueryContext(ctx, `SELECT i.id, f.id, f.path FROM items i
		JOIN media_versions v ON v.item_id = i.id JOIN media_files f ON f.version_id = v.id AND f.part_index = 0
		LEFT JOIN soundprint s ON s.item_id = i.id
		WHERE i.type = 'track' AND f.available = 1
		  AND (s.item_id IS NULL OR s.file_id != f.id OR (s.model != ? AND s.error IS NULL))
		GROUP BY i.id ORDER BY i.id`, modelID)
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
	slog.Info("soundprint analysis starting", "tracks", len(todo), "model", modelID)
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
		model, results, err := s.analyzeBatch(ctx, paths, active)
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
				s.DB.ExecContext(ctx, `INSERT INTO soundprint(item_id, file_id, model, error) VALUES (?, ?, ?, ?)
					ON CONFLICT(item_id) DO UPDATE SET file_id = excluded.file_id, model = excluded.model, embedding = NULL,
					error = excluded.error, analyzed_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')`, p.itemID, p.fileID, model, nonEmpty(r.Error, "no embedding"))
				continue
			}
			_, err := s.DB.ExecContext(ctx, `INSERT INTO soundprint(item_id, file_id, model, embedding, bpm, musical_key, mode, energy) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
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
	slog.Info("soundprint analysis finished", "analysed", analysed, "failed", failed, "took", time.Since(start).Round(time.Second))
	return msg, len(todo), nil
}

// analyzeBatch analyses paths together, and if the batch fails (one bad file can break
// it), one at a time so a single file can't stall the whole library. It only fails when
// the sidecar itself is unreachable.
func (s *Service) analyzeBatch(ctx context.Context, paths []string, active string) (string, []Analysis, error) {
	model, results, err := s.Client.Analyze(ctx, paths, active, true)
	if err == nil || len(paths) == 1 || ctx.Err() != nil {
		return model, results, err
	}
	slog.Warn("soundprint batch failed; retrying files one by one", "err", err)
	results = make([]Analysis, len(paths))
	ok := false
	for i, p := range paths {
		m, r, err := s.Client.Analyze(ctx, []string{p}, active, true)
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
	var d trackDetails
	var traits string
	s.DB.QueryRowContext(ctx, `SELECT `+trackColumns+`, COALESCE(s.traits, ''), COALESCE(s.language, '')
		FROM items i LEFT JOIN items a ON a.id = i.parent_id LEFT JOIN soundprint s ON s.item_id = i.id WHERE i.id = ?`, itemID).
		Scan(append(d.dest(t), &traits, &t.Language)...)
	d.apply(t)
	t.Traits = parseTraits(traits)
	s.Index.Put(t)
}

// ---------- listening history ----------

// Avoid weights tracks the user played recently (so radios feel fresh) or skipped.
func (s *Service) Avoid(ctx context.Context, userID int64) map[int64]float64 {
	out := map[int64]float64{}
	recent := map[int64]float64{}
	rows, err := s.DB.QueryContext(ctx, `SELECT item_id, last_viewed_at FROM user_item_state
		WHERE user_id = ? AND last_viewed_at > strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '-3 days')`, userID)
	if err == nil {
		for rows.Next() {
			var id int64
			var at string
			rows.Scan(&id, &at)
			out[id] = 0.6
			recent[id] = 0.6
		}
		rows.Close()
	}
	// Favourite songs are never avoided for skips or low ratings.
	defer s.spareFavourites(ctx, userID, out, recent)
	if s.Learn != nil && s.Learn() {
		for id, w := range s.learned(ctx, userID) {
			out[id] = max(out[id], w)
		}
		return out
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

// Liked returns the user's taste: their favourite songs first (newest favourite first),
// then tracks played more than once or rated 4+ stars, most recent first (at most 2,000).
func (s *Service) Liked(ctx context.Context, userID int64) []*Track {
	rows, err := s.DB.QueryContext(ctx, `SELECT item_id FROM user_item_state
		WHERE user_id = ? AND (favorite_at IS NOT NULL OR play_count > 1 OR rating >= 8 OR (play_count > 0 AND last_viewed_at > strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '-90 days')))
		ORDER BY favorite_at IS NULL, favorite_at DESC, last_viewed_at DESC LIMIT 2000`, userID)
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
	// Context says what kind of station or mix it is (mix:daily, radio:mood…): the apps send
	// it back with each play, for the playlist statistics (MUSIC-17).
	Context string
	// Query is a Muse playlist's query as it ran: without the filters that had to be relaxed
	// for it to find enough tracks.
	Query MusicQuery
}

// Lifts in an item's radio: tracks played together with the seeds, and tracks by artists
// listeners call similar to the seeds' (each up to this much).
const (
	seedCoplayAffinity  = 0.15
	seedSimilarAffinity = 0.12
)

// RadioFromItem starts an endless-feeling station from a track, album or artist: it plays
// what sounds like the seeds, lifting what's played together with them and artists listeners
// call similar, in the seeds' languages (and the listener's) and with the seeds' kinds of
// asides (a live album's radio plays live tracks).
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
	o.AllowAside |= allowAside(ts)
	if seedLangs := LanguagesOf(ts); seedLangs != nil {
		o.Languages = mergeLanguages(o.Languages, seedLangs)
	} else if typ == "track" && ts[0].Language != "" {
		o.Languages = mergeLanguages(o.Languages, Languages{ts[0].Language: 1})
	}
	artists := map[int64]bool{}
	for _, t := range ts {
		if t.ArtistID != 0 {
			artists[t.ArtistID] = true
		}
	}
	o.Affinity = addAffinity(o.Affinity, s.coplayWith(ts), seedCoplayAffinity)
	o.Affinity = addAffinity(o.Affinity, s.similarArtistBoost(artists, 1, o.Keep), seedSimilarAffinity)
	if o.CoPlay == nil {
		o.CoPlay = s.CoPlay
	}
	return Station{Title: title + " Radio", IDs: TrackIDs(s.Index.Flow(anchor, first, o))}, nil
}

// CoPlay is how strongly two tracks are played together (0–1).
func (s *Service) CoPlay(a, b int64) float64 {
	return s.coplayed()[a][b]
}

// addAffinity adds lifts (0–1 each) scaled by weight to an affinity map (a new one when nil).
func addAffinity(into, add map[int64]float64, weight float64) map[int64]float64 {
	if len(add) == 0 {
		return into
	}
	out := make(map[int64]float64, len(into)+len(add))
	for id, w := range into {
		out[id] = w
	}
	for id, w := range add {
		out[id] += weight * w
	}
	return out
}

// mergeLanguages allows the languages of either (the stronger weight of each); with no
// listener languages (a), the seeds' (b) lead.
func mergeLanguages(a, b Languages) Languages {
	if a == nil {
		return b
	}
	out := Languages{}
	for l, w := range a {
		out[l] = w
	}
	for l, w := range b {
		out[l] = max(out[l], w)
	}
	return out
}

// ListenerLanguages are the languages someone's liked music is sung in (nil when it's
// mixed or too little says), for the stations made for them.
func (s *Service) ListenerLanguages(ctx context.Context, userID int64) Languages {
	return LanguagesOf(s.Liked(ctx, userID))
}

// RadioFromVector starts a station from any sound (a genre's average, a mood prompt…).
func (s *Service) RadioFromVector(v []float32, title string, o Options) Station {
	return Station{Title: title, IDs: TrackIDs(s.Index.Flow(v, nil, o))}
}

// TextVector embeds a prompt (Muse, moods) with the sound model in use.
func (s *Service) TextVector(ctx context.Context, prompt string) ([]float32, error) {
	vs, err := s.Client.EmbedText(ctx, []string{prompt}, s.ActiveModel(ctx))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	if len(vs) == 0 {
		return nil, ErrUnavailable
	}
	return normalize(vs[0]), nil
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
	// timeCache holds the time-of-day mix, made again when the part of the day changes.
	timeCache = map[timeCacheKey]*Mix{}
)

type timeCacheKey struct {
	mixCacheKey
	slot string
}

// forgetMixes drops the mixes made so far (the sound model changed), so they're made again.
func forgetMixes() {
	mixMu.Lock()
	mixCache, timeCache = map[mixCacheKey][]Mix{}, map[timeCacheKey]*Mix{}
	mixMu.Unlock()
	recentMu.Lock()
	recentMixes = map[mixHistoryKey]map[string][]int64{}
	recentMu.Unlock()
}

// DailyMixes clusters the listener's taste into up to four groups and builds a 25-track
// mix around each, mixing favourites with similar tracks they haven't played lately, then
// adds the history-based mixes (MUSIC-17). Mixes stay the same for the day.
//
// scope names what keep allows (e.g. the library id), so different filters get their own mixes.
func (s *Service) DailyMixes(ctx context.Context, userID int64, scope string, keep Filter) []Mix {
	now := time.Now()
	mixes := s.dailyMixes(ctx, userID, scope, keep, now)
	if m := s.timeMix(ctx, userID, scope, keep, now, mixes); m != nil {
		mixes = append(append([]Mix{}, mixes...), *m)
	}
	return mixes
}

// mixJitter varies daily picks: enough to change most of a mix from day to day while the
// closest matches stay likely.
const mixJitter = 0.08

// recentMixes remembers each listener's mix tracks for the last few days, so tomorrow's
// mixes favour other tracks (kept in memory; after a restart the day's seed alone varies).
var (
	recentMu    sync.Mutex
	recentMixes = map[mixHistoryKey]map[string][]int64{} // → day → track ids
)

type mixHistoryKey struct {
	user  int64
	scope string
}

// taken remembers what the day's mixes have already used, so no two of them offer the same
// track, and no mix offers a second copy of a recording another one has (MUSIC-17).
type taken struct {
	ids    map[int64]bool
	copies map[string]bool
}

func newTaken() *taken { return &taken{ids: map[int64]bool{}, copies: map[string]bool{}} }

func (t *taken) add(ts []*Track) {
	for _, tr := range ts {
		t.ids[tr.ItemID] = true
		if tr.Copy != "" {
			t.copies[tr.Copy] = true
		}
	}
}

func (t *taken) has(tr *Track) bool {
	return t.ids[tr.ItemID] || (tr.Copy != "" && t.copies[tr.Copy])
}

// oneEach drops the second copy of a recording (and any track listed twice), keeping the
// first of each in order. Picked lists come this way already; lists read from the database
// don't (MUSIC-17).
func oneEach(ts []*Track) []*Track {
	seen, copies := map[int64]bool{}, map[string]bool{}
	out := make([]*Track, 0, len(ts))
	for _, t := range ts {
		if seen[t.ItemID] || (t.Copy != "" && copies[t.Copy]) {
			continue
		}
		seen[t.ItemID] = true
		if t.Copy != "" {
			copies[t.Copy] = true
		}
		out = append(out, t)
	}
	return out
}

// spreadOut takes up to n of ts in order, at most per tracks of any one artist, letting one
// more of each in at a time only when that leaves the list short (Pick's caps, for a list
// that isn't picked).
func spreadOut(ts []*Track, n, per int) []*Track {
	n = min(n, len(ts))
	out := make([]*Track, 0, n)
	in := make([]bool, len(ts))
	byArtist := map[int64]int{}
	for ; len(out) < n; per++ {
		for i, t := range ts {
			if len(out) == n {
				break
			}
			if in[i] || (t.ArtistID != 0 && byArtist[t.ArtistID] >= per) {
				continue
			}
			in[i] = true
			byArtist[t.ArtistID]++
			out = append(out, t)
		}
	}
	return out
}

// mixPerArtist and mixPerAlbum cap one artist's and one album's share of a mix: the tracks
// nearest a taste are mostly a few artists' albums (and Favour lifts a favourite artist's
// whole catalogue), so without caps a mix read like those albums back to back instead of a
// dozen or more artists. Pick raises them a step at a time where the artists run out.
const mixPerArtist, mixPerAlbum = 2, 1

// pick chooses n tracks none of the day's other mixes has taken, and remembers them. A mix
// that leaves fewer than least tracks that way is picked again without the restriction: a
// mix too short to offer is worse than one sharing a few tracks (least 0 never gives way).
func (s *Service) pick(v []float32, n, least int, o Options, used *taken) []*Track {
	o.PerArtist, o.PerAlbum = mixPerArtist, mixPerAlbum
	strict := o
	strict.Exclude, strict.ExcludeCopy = used.ids, used.copies
	ts := s.Index.Pick(v, n, strict)
	if len(ts) < least {
		ts = s.Index.Pick(v, n, o)
	}
	used.add(ts)
	return ts
}

func (s *Service) dailyMixes(ctx context.Context, userID int64, scope string, keep Filter, now time.Time) []Mix {
	day := now.Format("2006-01-02")
	key := mixCacheKey{userID, day, scope}
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
	seed := uint64(userID)*1_000_003 + uint64(now.Year())*1000 + uint64(now.YearDay())
	r := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
	k := min(4, max(1, len(keepLiked)/15))
	avoid := s.Avoid(ctx, userID)
	if avoid == nil {
		avoid = map[int64]float64{}
	}
	// Tracks from the last three days' mixes count against today's.
	hk := mixHistoryKey{userID, scope}
	recentMu.Lock()
	for d, ids := range recentMixes[hk] {
		if then, err := time.Parse("2006-01-02", d); err == nil && now.Sub(then) < 4*24*time.Hour && d != day {
			for _, id := range ids {
				avoid[id] = math.Max(avoid[id], 0.6)
			}
		}
	}
	recentMu.Unlock()
	var mixes []Mix
	used := newTaken()
	favour := s.Favour(ctx, userID)
	centres := Clusters(keepLiked, k, r)
	members := clusterMembers(keepLiked, centres)
	for i, c := range centres {
		// Each mix keeps to its cluster's languages and lifts what's played with its tracks.
		o := Options{Keep: keep, Avoid: avoid, Rand: r, Jitter: mixJitter, Favour: favour, Arc: true,
			Languages: LanguagesOf(members[i]), Affinity: addAffinity(nil, s.coplayWith(members[i]), mixCoplayAffinity)}
		ts := s.pick(dayCentre(c, keepLiked, r), 25, 0, o, used)
		mixes = append(mixes, Mix{Station: Station{Title: fmt.Sprintf("Daily Mix %d", i+1), IDs: TrackIDs(ts)}, ID: fmt.Sprintf("daily-%d", i+1)})
	}
	mixes = append(mixes, s.historyMixes(ctx, userID, keep, keepLiked, avoid, favour, r, used)...)
	var today []int64
	for _, m := range mixes {
		today = append(today, m.IDs...)
	}
	recentMu.Lock()
	hist := recentMixes[hk]
	if hist == nil {
		hist = map[string][]int64{}
		recentMixes[hk] = hist
	}
	hist[day] = today
	for d := range hist {
		if then, err := time.Parse("2006-01-02", d); err != nil || now.Sub(then) > 4*24*time.Hour {
			delete(hist, d)
		}
	}
	recentMu.Unlock()
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

// mixCoplayAffinity is how much a mix lifts what's played together with its seeds.
const mixCoplayAffinity = 0.12

// clusterMembers groups tracks by their nearest centre.
func clusterMembers(ts []*Track, centres [][]float32) [][]*Track {
	out := make([][]*Track, len(centres))
	for _, t := range ts {
		best := -1
		for c := range centres {
			if best < 0 || dot(t.Vec, centres[c]) > dot(t.Vec, centres[best]) {
				best = c
			}
		}
		if best >= 0 {
			out[best] = append(out[best], t)
		}
	}
	return out
}

// dayCentre moves a taste cluster's centre towards a few of its favourites drawn at random
// for the day, so each Daily Mix keeps its character but starts somewhere new.
func dayCentre(centre []float32, liked []*Track, r *rand.Rand) []float32 {
	near := append([]*Track{}, liked...)
	sort.Slice(near, func(i, j int) bool { return dot(near[i].Vec, centre) > dot(near[j].Vec, centre) })
	near = near[:min(len(near), max(6, len(near)/4))]
	r.Shuffle(len(near), func(i, j int) { near[i], near[j] = near[j], near[i] })
	picked := Mean(near[:min(3, len(near))])
	if picked == nil {
		return centre
	}
	v := make([]float32, len(centre))
	for i := range v {
		v[i] = 0.4*centre[i] + 0.6*picked[i]
	}
	return normalize(v)
}

// historyMixes are mixes from what someone has (and hasn't) played:
// Discovery (never-played tracks close to their taste, favouring artists they rarely play),
// Rediscover (favourites not played for six months), New Music (recent additions that fit
// their taste) and their favourite decade.
func (s *Service) historyMixes(ctx context.Context, userID int64, keep Filter, liked []*Track, avoid, favour map[int64]float64, r *rand.Rand, used *taken) []Mix {
	taste := s.Taste(ctx, userID, liked)
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
	langs := LanguagesOf(liked)
	var out []Mix

	// Discovery: unplayed, near their taste; artists they've played a lot count against, and
	// artists listeners call similar to the ones they love are lifted.
	disc := Options{Keep: func(t *Track) bool { return allowed(t) && !played[t.ItemID] }, Avoid: map[int64]float64{}, Rand: r, Jitter: mixJitter,
		Arc: true, Languages: langs}
	for _, t := range s.Index.Where(disc.Keep) {
		if n := artistPlays[t.ArtistID]; n > 0 {
			disc.Avoid[t.ItemID] = min(0.8, float64(n)/40)
		}
	}
	disc.Affinity = addAffinity(nil, s.similarArtistBoost(lovedArtists(liked, artistPlays), 1, disc.Keep), discoverySimilarAffinity)
	if ts := s.pick(taste, 25, 10, disc, used); len(ts) >= 10 {
		out = append(out, Mix{Station: Station{Title: "Discovery Mix", IDs: TrackIDs(ts)}, ID: "discovery",
			Description: "Tracks in your library you haven't played yet, close to what you love"})
	}

	// Rediscover: favourites (the heart, 4+ stars, or played 3+ times) not played for six months.
	var old []*Track
	rows, err = s.DB.QueryContext(ctx, `SELECT item_id FROM user_item_state WHERE user_id = ? AND (favorite_at IS NOT NULL OR rating >= 8 OR play_count >= 3)
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
		// One copy of each recording, and nothing another of the day's mixes has already
		// taken — unless that leaves too few favourites to offer (as Service.pick does).
		var free []*Track
		for _, t := range old {
			if !used.has(t) {
				free = append(free, t)
			}
		}
		if free = oneEach(free); len(free) < 10 {
			free = oneEach(old)
		}
		old = spreadOut(free, 25, mixPerArtist)
		used.add(old)
		out = append(out, Mix{Station: Station{Title: "Rediscover", IDs: TrackIDs(arcOrder(old, Mean(old), true))}, ID: "rediscover",
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
		o := Options{Keep: func(t *Track) bool { return allowed(t) && fresh[t.ItemID] }, Avoid: avoid, Rand: r, Jitter: mixJitter, Favour: favour,
			Arc: true, Languages: langs}
		if ts := s.pick(taste, 25, 10, o, used); len(ts) >= 10 {
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
		o := Options{Keep: func(t *Track) bool { return allowed(t) && t.Year/10*10 == best }, Avoid: avoid, Rand: r, Jitter: mixJitter, Favour: favour,
			Arc: true, Languages: langs}
		var inDecade []*Track
		for _, t := range liked {
			if t.Year/10*10 == best {
				inDecade = append(inDecade, t)
			}
		}
		if ts := s.pick(Mean(inDecade), 25, 10, o, used); len(ts) >= 10 {
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

// discoverySimilarAffinity is how much Discovery lifts artists similar to the ones loved.
const discoverySimilarAffinity = 0.2

// lovedArtists are the artists of someone's favourite and most-played tracks (the 20 they
// play most, and every artist of a favourite).
func lovedArtists(liked []*Track, artistPlays map[int64]int) map[int64]bool {
	out := map[int64]bool{}
	for _, t := range liked {
		if t.ArtistID != 0 && len(out) < 40 {
			out[t.ArtistID] = true
		}
	}
	type ap struct {
		id int64
		n  int
	}
	var list []ap
	for id, n := range artistPlays {
		if id != 0 {
			list = append(list, ap{id, n})
		}
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].n > list[j].n || (list[i].n == list[j].n && list[i].id < list[j].id)
	})
	for _, a := range list[:min(20, len(list))] {
		out[a.id] = true
	}
	return out
}

// Parts of the day for the time-of-day mix, by the server's local hour.
var daySlots = []struct {
	id, title, desc string
	from, to        int // hours, [from, to)
}{
	{"morning", "Your Morning Mix", "What you play in the mornings, with more like it", 5, 11},
	{"afternoon", "Your Afternoon Mix", "What you play in the afternoons, with more like it", 11, 17},
	{"evening", "Your Evening Mix", "What you play in the evenings, with more like it", 17, 22},
	{"night", "Your Late Night Mix", "What you play late at night, with more like it", 22, 29},
}

func slotOf(hour int) int {
	for i, sl := range daySlots {
		if (hour >= sl.from && hour < sl.to) || (hour+24 >= sl.from && hour+24 < sl.to) {
			return i
		}
	}
	return 0
}

// timeMix is a mix of what someone plays at this time of day (played through in this part of
// the day over the last four months), with more like it: made again when the part of the day
// changes, and taking nothing the day's other mixes have.
func (s *Service) timeMix(ctx context.Context, userID int64, scope string, keep Filter, now time.Time, others []Mix) *Mix {
	slot := daySlots[slotOf(now.Hour())]
	key := timeCacheKey{mixCacheKey{userID, now.Format("2006-01-02"), scope}, slot.id}
	mixMu.Lock()
	if m, ok := timeCache[key]; ok {
		mixMu.Unlock()
		return m
	}
	mixMu.Unlock()
	// The hour each play started, in the server's time zone.
	rows, err := s.DB.QueryContext(ctx, `SELECT h.item_id, h.started_at FROM play_history h JOIN items i ON i.id = h.item_id AND i.type = 'track'
		WHERE h.user_id = ? AND h.started_at > strftime('%Y-%m-%dT%H:%M:%fZ', 'now', '-120 days')
		  AND COALESCE(h.position_ms, 0) >= MIN(30000, COALESCE(i.duration_ms, 60000) / 2)`, userID)
	var inSlot []*Track
	if err == nil {
		seen := map[int64]bool{}
		for rows.Next() {
			var id int64
			var at string
			if rows.Scan(&id, &at) != nil {
				continue
			}
			t, err := time.Parse(time.RFC3339Nano, at)
			if err != nil || seen[id] {
				continue
			}
			if h := t.In(now.Location()).Hour(); slotOf(h) == slotOf(now.Hour()) {
				if tr := s.Index.Get(id); tr != nil && (keep == nil || keep(tr)) {
					seen[id] = true
					inSlot = append(inSlot, tr)
				}
			}
		}
		rows.Close()
	}
	var m *Mix
	if len(inSlot) >= 10 {
		used := newTaken()
		for _, o := range others {
			for _, id := range o.IDs {
				used.ids[id] = true
			}
		}
		seed := uint64(userID)*7919 + uint64(now.YearDay())*31 + uint64(slotOf(now.Hour()))
		r := rand.New(rand.NewPCG(seed, seed^0x51ed270b))
		o := Options{Keep: keep, Avoid: s.Avoid(ctx, userID), Rand: r, Jitter: mixJitter, Favour: s.Favour(ctx, userID), Arc: true,
			Languages: LanguagesOf(inSlot), Affinity: addAffinity(nil, s.coplayWith(inSlot), mixCoplayAffinity)}
		if ts := s.pick(Mean(inSlot), 25, 10, o, used); len(ts) >= 10 {
			m = &Mix{Station: Station{Title: slot.title, IDs: TrackIDs(ts)}, ID: "time", Description: slot.desc}
		}
	}
	mixMu.Lock()
	for k := range timeCache {
		if k.day != key.day || (k.user == key.user && k.scope == key.scope && k.slot != key.slot) {
			delete(timeCache, k)
		}
	}
	timeCache[key] = m
	mixMu.Unlock()
	return m
}
