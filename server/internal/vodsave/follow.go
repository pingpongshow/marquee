package vodsave

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"
)

// Following series on demand: a follower's series has its new episodes saved to the library
// as they appear (or all of them, with includeExisting), through the save queue and its
// rules, as the follower (requested_by) into the follow's save folder. A follow stops
// queueing while the episodes it saved that are still in the library reach its own size
// limit (full), or while the follower's DVR usage (recordings plus followed saves) reaches
// their DVR quota (quota), and while following is off or the follower may no longer record
// (paused). It resumes by itself once there's room again.

// Follow statuses.
const (
	FollowFollowing = "following"
	FollowFull      = "full"
	FollowQuota     = "quota"
	FollowPaused    = "paused"
)

// followBatch is how many of a follow's episodes wait in the queue at once: the next ones
// are queued as these finish, so the size limit and the quota are looked at between them.
const followBatch = 3

const bytesPerGB = 1_000_000_000

var (
	ErrFollowOff   = errors.New("following series isn't turned on")
	ErrNotFollowed = errors.New("not followed")
)

// Follow is one follower's followed series.
type Follow struct {
	ID              int64
	UserID          int64
	UserName        string
	SourceID        string
	SeriesKey       string
	SeriesID        int64 // the series now (or when last seen)
	Title           string
	HasPoster       bool
	MaxBytes        int64 // 0 = no own limit
	IncludeExisting bool
	Destination     string // a save folder's id ("" = the shows folder)
	Status          string
	StatusText      string
	CreatedAt       time.Time
	LastNewAt       *time.Time
	// UsedBytes and EpisodesSaved: the episodes it saved that are still in the library;
	// EpisodesQueued: those waiting or saving now.
	UsedBytes      int64
	EpisodesSaved  int
	EpisodesQueued int
}

// FollowOptions are a follow's settings: nil keeps them (the defaults on a new follow).
type FollowOptions struct {
	MaxBytes        *int64
	IncludeExisting bool // only on a new follow
	// Destination is a save folder's id or name ("" = the shows folder).
	Destination *string
}

func (s *Service) followEvery() time.Duration { return or(s.FollowEvery, 30*time.Minute) }

// FollowAvailable reports why series can't be followed now (nil: they can): following is
// off (ErrFollowOff), or there's nowhere to save episodes (ErrNoFolder).
func (s *Service) FollowAvailable() error {
	c := s.cfg()
	if !c.FollowEnabled {
		return ErrFollowOff
	}
	if c.ShowsFolder == "" && len(s.Destinations("episode")) == 0 {
		return ErrNoFolder
	}
	return nil
}

// resolveDestination is the id of the save folder named by id or name ("" = the shows
// folder), which must take episodes.
func (s *Service) resolveDestination(v string) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		_, _, err := s.destination("", "episode")
		return "", err
	}
	for _, d := range s.cfg().Destinations {
		if d.ID == v || strings.EqualFold(d.Name, v) {
			_, _, err := s.destination(d.ID, "episode")
			return d.ID, err
		}
	}
	return "", destError("that save folder isn't in Settings any more")
}

const followCols = `f.id, f.user_id, COALESCE(NULLIF(u.display_name,''), u.username, ''), f.source_id, f.series_key,
	COALESCE(m.id, f.series_id), COALESCE(m.title, f.title), COALESCE(m.poster_url<>'' OR m.logo_url<>'', 0), f.max_bytes,
	f.include_existing, f.destination, f.status, f.status_text, f.created_at, f.last_new_at`

const followFrom = ` FROM vod_follows f LEFT JOIN users u ON u.id=f.user_id
	LEFT JOIN vod_series m ON m.source_id=f.source_id AND m.source_key=f.series_key`

func scanFollow(sc interface{ Scan(...any) error }) (Follow, error) {
	var f Follow
	var created string
	var last sql.NullString
	err := sc.Scan(&f.ID, &f.UserID, &f.UserName, &f.SourceID, &f.SeriesKey, &f.SeriesID, &f.Title, &f.HasPoster, &f.MaxBytes,
		&f.IncludeExisting, &f.Destination, &f.Status, &f.StatusText, &created, &last)
	if err != nil {
		return f, err
	}
	f.CreatedAt, _ = time.Parse(time.RFC3339, created)
	if t, err := time.Parse(time.RFC3339, last.String); last.Valid && err == nil {
		f.LastNewAt = &t
	}
	return f, nil
}

// fill adds what the follow saved and has queued.
func (s *Service) fill(ctx context.Context, f *Follow) {
	f.UsedBytes, f.EpisodesSaved = s.followUsage(ctx, f.UserID, f.SourceID, f.SeriesKey)
	f.EpisodesQueued = s.followQueued(ctx, *f)
}

// followUsage is the size and number of the episodes a follower's follow of a series saved
// that are still in the library (their files are where they were saved).
func (s *Service) followUsage(ctx context.Context, userID int64, sourceID, seriesKey string) (bytes int64, n int) {
	rows, err := s.DB.QueryContext(ctx, `SELECT path FROM vod_follow_episodes WHERE user_id=? AND source_id=? AND series_key=? AND path<>''`,
		userID, sourceID, seriesKey)
	if err != nil {
		return 0, 0
	}
	var paths []string
	for rows.Next() {
		var p string
		if rows.Scan(&p) == nil {
			paths = append(paths, p)
		}
	}
	rows.Close()
	for _, p := range paths {
		if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() {
			bytes += st.Size()
			n++
		}
	}
	return bytes, n
}

// followQueued is how many of a follow's episodes wait in the queue or are being saved.
func (s *Service) followQueued(ctx context.Context, f Follow) int {
	var n int
	s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM vod_follow_episodes e JOIN vod_saves v ON v.id=e.save_id
		WHERE e.user_id=? AND e.source_id=? AND e.series_key=? AND e.path='' AND v.state IN (`+in(len(activeStates))+`)`,
		append([]any{f.UserID, f.SourceID, f.SeriesKey}, activeStates...)...).Scan(&n)
	return n
}

func (s *Service) followByID(ctx context.Context, id int64) (Follow, error) {
	f, err := scanFollow(s.DB.QueryRowContext(ctx, "SELECT "+followCols+followFrom+" WHERE f.id=?", id))
	if errors.Is(err, sql.ErrNoRows) {
		return f, ErrNotFollowed
	}
	if err != nil {
		return f, err
	}
	s.fill(ctx, &f)
	return f, nil
}

// Follows lists a user's follows (userID 0: everyone's), by title.
func (s *Service) Follows(ctx context.Context, userID int64) ([]Follow, error) {
	q := "SELECT " + followCols + followFrom
	var args []any
	if userID > 0 {
		q += " WHERE f.user_id=?"
		args = append(args, userID)
	}
	rows, err := s.DB.QueryContext(ctx, q+" ORDER BY COALESCE(m.title, f.title) COLLATE NOCASE, f.user_id", args...)
	if err != nil {
		return nil, err
	}
	var out []Follow
	for rows.Next() {
		f, err := scanFollow(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, f)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		s.fill(ctx, &out[i])
	}
	return out, nil
}

// Followed is which of these series (ids) the user follows.
func (s *Service) Followed(ctx context.Context, userID int64, seriesIDs []int64) map[int64]bool {
	out := map[int64]bool{}
	if len(seriesIDs) == 0 {
		return out
	}
	args := []any{userID}
	for _, id := range seriesIDs {
		args = append(args, id)
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT m.id FROM vod_follows f JOIN vod_series m ON m.source_id=f.source_id AND m.source_key=f.series_key
		WHERE f.user_id=? AND m.id IN (`+in(len(seriesIDs))+`)`, args...)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			out[id] = true
		}
	}
	return out
}

// FollowOf is the user's follow of a series (ErrNotFollowed).
func (s *Service) FollowOf(ctx context.Context, userID, seriesID int64) (Follow, error) {
	var id int64
	err := s.DB.QueryRowContext(ctx, `SELECT f.id FROM vod_follows f LEFT JOIN vod_series m ON m.source_id=f.source_id AND m.source_key=f.series_key
		WHERE f.user_id=? AND (m.id=? OR (m.id IS NULL AND f.series_id=?)) ORDER BY f.id LIMIT 1`, userID, seriesID, seriesID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return Follow{}, ErrNotFollowed
	}
	if err != nil {
		return Follow{}, err
	}
	return s.followByID(ctx, id)
}

// FollowedBytes is, by user, the size of the episodes their follows saved that are still in
// the library (what counts towards their DVR quota besides recordings).
func (s *Service) FollowedBytes(ctx context.Context) map[int64]int64 {
	out := map[int64]int64{}
	rows, err := s.DB.QueryContext(ctx, `SELECT e.user_id, e.path FROM vod_follow_episodes e
		JOIN vod_follows f ON f.user_id=e.user_id AND f.source_id=e.source_id AND f.series_key=e.series_key WHERE e.path<>''`)
	if err != nil {
		return out
	}
	type row struct {
		user int64
		path string
	}
	var list []row
	for rows.Next() {
		var r row
		if rows.Scan(&r.user, &r.path) == nil {
			list = append(list, r)
		}
	}
	rows.Close()
	for _, r := range list {
		if st, err := os.Stat(r.path); err == nil && st.Mode().IsRegular() {
			out[r.user] += st.Size()
		}
	}
	return out
}

// FollowSeries follows a series for a user, or changes their follow. A new follow without
// includeExisting first notes the episodes there now (fetched from the source), so only
// later ones are saved. The engine queues episodes right after.
func (s *Service) FollowSeries(ctx context.Context, userID, seriesID int64, o FollowOptions) (Follow, error) {
	s.init()
	if err := s.FollowAvailable(); err != nil {
		return Follow{}, err
	}
	var srcID, key, title string
	err := s.DB.QueryRowContext(ctx, "SELECT source_id, source_key, title FROM vod_series WHERE id=?", seriesID).Scan(&srcID, &key, &title)
	if errors.Is(err, sql.ErrNoRows) {
		return Follow{}, ErrNotFound
	}
	if err != nil {
		return Follow{}, err
	}
	if s.VOD == nil || !s.VOD.Savable(srcID) {
		return Follow{}, ErrNotSavable
	}
	var dest *string
	if o.Destination != nil {
		id, err := s.resolveDestination(*o.Destination)
		if err != nil {
			return Follow{}, err
		}
		dest = &id
	}
	var maxBytes *int64
	if o.MaxBytes != nil {
		m := max(*o.MaxBytes, 0)
		maxBytes = &m
	}
	existing, err := s.FollowOf(ctx, userID, seriesID)
	switch {
	case err == nil:
		q, args := "UPDATE vod_follows SET series_id=?, title=?", []any{seriesID, title}
		if maxBytes != nil {
			q, args = q+", max_bytes=?", append(args, *maxBytes)
		}
		if dest != nil {
			q, args = q+", destination=?", append(args, *dest)
		}
		if _, err := s.DB.ExecContext(ctx, q+" WHERE id=?", append(args, existing.ID)...); err != nil {
			return Follow{}, err
		}
		s.refreshStatus(ctx, existing.ID)
		s.wakeFollows()
		return s.followByID(ctx, existing.ID)
	case !errors.Is(err, ErrNotFollowed):
		return Follow{}, err
	}
	if dest == nil {
		if _, err := s.resolveDestination(""); err != nil {
			return Follow{}, err
		}
		dest = new(string)
	}
	if maxBytes == nil {
		m := int64(s.cfg().FollowDefaultMaxGB * bytesPerGB)
		maxBytes = &m
	}
	baselined := false
	if !o.IncludeExisting {
		// The episodes there now aren't saved: fetch the list fresh to know them.
		if err := s.VOD.LoadEpisodes(ctx, seriesID, true); err != nil {
			slog.Warn("vod follow: episodes", "series", seriesID, "err", err)
		}
		n, err := s.baseline(ctx, userID, srcID, key, seriesID)
		if err != nil {
			return Follow{}, err
		}
		baselined = n > 0
	}
	r, err := s.DB.ExecContext(ctx, `INSERT INTO vod_follows(user_id, source_id, series_key, series_id, title, max_bytes,
		include_existing, baselined, destination, created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		userID, srcID, key, seriesID, title, *maxBytes, o.IncludeExisting, baselined, *dest, now())
	if err != nil {
		return Follow{}, err
	}
	id, _ := r.LastInsertId()
	slog.Info("vod follow: following", "series", title, "user", userID, "includeExisting", o.IncludeExisting)
	s.refreshStatus(ctx, id)
	s.wakeFollows()
	return s.followByID(ctx, id)
}

// baseline notes a series' episodes known now as dealt with (not to be saved), returning
// how many it has.
func (s *Service) baseline(ctx context.Context, userID int64, srcID, key string, seriesID int64) (int, error) {
	_, err := s.DB.ExecContext(ctx, `INSERT OR IGNORE INTO vod_follow_episodes(user_id, source_id, series_key, episode_key, created_at)
		SELECT ?, ?, ?, source_key, ? FROM vod_episodes WHERE series_id=?`, userID, srcID, key, now(), seriesID)
	if err != nil {
		return 0, err
	}
	var n int
	err = s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM vod_episodes WHERE series_id=?", seriesID).Scan(&n)
	return n, err
}

// Unfollow stops following a series. What it saved stays in the library (and stops
// counting towards the follower's quota); what it queued stays in the queue.
func (s *Service) Unfollow(ctx context.Context, userID, seriesID int64) error {
	f, err := s.FollowOf(ctx, userID, seriesID)
	if err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, "DELETE FROM vod_follows WHERE id=?", f.ID); err != nil {
		return err
	}
	// The episodes passed over are forgotten (following again starts afresh); those queued
	// and saved are kept, to count again if the series is followed again.
	if _, err := tx.ExecContext(ctx, `DELETE FROM vod_follow_episodes WHERE user_id=? AND source_id=? AND series_key=?
		AND save_id IS NULL AND path=''`, f.UserID, f.SourceID, f.SeriesKey); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	slog.Info("vod follow: unfollowed", "series", f.Title, "user", userID)
	return nil
}

// followSaved notes the file a followed episode was saved as (worker.go).
func (s *Service) followSaved(ctx context.Context, saveID int64, path string, bytes int64) {
	s.DB.ExecContext(ctx, "UPDATE vod_follow_episodes SET path=?, bytes=?, saved_at=? WHERE save_id=?", path, bytes, now(), saveID)
}

// WakeFollows asks for the follows to be looked at now (after a catalogue refresh, or new
// episodes fetched).
func (s *Service) WakeFollows() { s.wakeFollows() }

func (s *Service) wakeFollows() {
	s.init()
	select {
	case s.followWake <- struct{}{}:
	default:
	}
}

func (s *Service) runFollows(ctx context.Context) {
	t := time.NewTicker(s.followEvery())
	defer t.Stop()
	for {
		s.CheckFollows(ctx)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-s.followWake:
		}
	}
}

// CheckFollows looks at every follow: its status, and the episodes to queue.
func (s *Service) CheckFollows(ctx context.Context) {
	rows, err := s.DB.QueryContext(ctx, "SELECT id FROM vod_follows ORDER BY id")
	if err != nil {
		return
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()
	for _, id := range ids {
		if ctx.Err() != nil {
			return
		}
		if err := s.checkFollow(ctx, id); err != nil && ctx.Err() == nil {
			slog.Warn("vod follow", "follow", id, "err", err)
		}
	}
}

func gb(b int64) string {
	v := float64(b) / bytesPerGB
	if v >= 100 {
		return fmt.Sprintf("%.0f GB", v)
	}
	return fmt.Sprintf("%.1f GB", v)
}

// evaluate is a follow's status now and why.
func (s *Service) evaluate(ctx context.Context, f Follow, seriesThere bool) (status, text string) {
	c := s.cfg()
	switch {
	case !c.FollowEnabled:
		return FollowPaused, "Paused: following series is turned off"
	case s.MayFollow != nil && !s.MayFollow(ctx, f.UserID):
		return FollowPaused, "Paused: this profile may not record"
	case !seriesThere:
		return FollowPaused, "Paused: the series isn't on the source now"
	case s.VOD == nil || !s.VOD.Savable(f.SourceID):
		return FollowPaused, "Paused: episodes from this source can't be saved"
	}
	if _, _, err := s.destination(f.Destination, "episode"); err != nil {
		return FollowPaused, "Paused: " + err.Error()
	}
	if f.MaxBytes > 0 && f.UsedBytes >= f.MaxBytes {
		return FollowFull, "Full: its " + gb(f.MaxBytes) + " size limit is reached"
	}
	if s.DVRUsage != nil {
		if used, quota := s.DVRUsage(ctx, f.UserID); quota > 0 && used >= quota {
			return FollowQuota, fmt.Sprintf("Paused: your recording space is full (%s of %s)", gb(used), gb(quota))
		}
	}
	return FollowFollowing, ""
}

// refreshStatus works out and stores a follow's status, without queueing anything.
func (s *Service) refreshStatus(ctx context.Context, id int64) {
	f, err := s.followByID(ctx, id)
	if err != nil {
		return
	}
	var there int
	s.DB.QueryRowContext(ctx, "SELECT COUNT(*) FROM vod_series WHERE source_id=? AND source_key=?", f.SourceID, f.SeriesKey).Scan(&there)
	st, text := s.evaluate(ctx, f, there > 0)
	s.setStatus(ctx, f, st, text)
}

func (s *Service) setStatus(ctx context.Context, f Follow, status, text string) {
	if status == f.Status && text == f.StatusText {
		return
	}
	s.DB.ExecContext(ctx, "UPDATE vod_follows SET status=?, status_text=? WHERE id=?", status, text, f.ID)
	if status != f.Status {
		slog.Info("vod follow: status", "series", f.Title, "user", f.UserID, "status", status, "why", text)
	}
	if status == FollowQuota && f.Status != FollowQuota && s.OnFollowQuota != nil {
		s.OnFollowQuota(f.UserID, f.UserName, f.Title, fmt.Sprintf("New episodes of %q aren't being saved for %s: %s.",
			f.Title, f.UserName, strings.Replace(text, "Paused: your", "their", 1)))
	}
}

// checkFollow updates a follow's status and, while it's following, queues its next
// episodes: those not dealt with yet, oldest first, keeping at most followBatch waiting.
func (s *Service) checkFollow(ctx context.Context, id int64) error {
	f, err := s.followByID(ctx, id)
	if err != nil {
		return err
	}
	var seriesID int64
	var title string
	err = s.DB.QueryRowContext(ctx, "SELECT id, title FROM vod_series WHERE source_id=? AND source_key=?", f.SourceID, f.SeriesKey).Scan(&seriesID, &title)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if seriesID > 0 && (seriesID != f.SeriesID || title != f.Title) {
		s.DB.ExecContext(ctx, "UPDATE vod_follows SET series_id=?, title=? WHERE id=?", seriesID, title, f.ID)
		f.SeriesID, f.Title = seriesID, title
	}
	status, text := s.evaluate(ctx, f, seriesID > 0)
	s.setStatus(ctx, f, status, text)
	if status != FollowFollowing {
		return nil
	}
	if err := s.VOD.LoadEpisodes(ctx, seriesID, false); err != nil {
		return nil // the source is down: next time
	}
	if !f.IncludeExisting {
		var baselined bool
		s.DB.QueryRowContext(ctx, "SELECT baselined FROM vod_follows WHERE id=?", f.ID).Scan(&baselined)
		if !baselined {
			// The episodes weren't known when it was followed: those there now are the old ones.
			n, err := s.baseline(ctx, f.UserID, f.SourceID, f.SeriesKey, seriesID)
			if err != nil {
				return err
			}
			if n > 0 {
				s.DB.ExecContext(ctx, "UPDATE vod_follows SET baselined=1 WHERE id=?", f.ID)
			}
			return nil
		}
	}
	slots := followBatch - f.EpisodesQueued
	queued := 0
	for slots > 0 {
		next, err := s.followCandidates(ctx, f, seriesID, slots)
		if err != nil {
			return err
		}
		if len(next) == 0 {
			break
		}
		// Whatever happens to them, they're dealt with: saved now, or passed over because the
		// library has them or they were saved already.
		for _, e := range next {
			if _, err := s.DB.ExecContext(ctx, `INSERT OR IGNORE INTO vod_follow_episodes(user_id, source_id, series_key, episode_key, created_at)
				VALUES(?,?,?,?,?)`, f.UserID, f.SourceID, f.SeriesKey, e.key, now()); err != nil {
				return err
			}
		}
		ids := make([]int64, len(next))
		for i, e := range next {
			ids[i] = e.id
		}
		res, err := s.saveEpisodes(ctx, seriesID, nil, 0, ids, f.UserID, false, f.Destination)
		if err != nil && !errors.Is(err, errNoEpisodes) {
			return err
		}
		mine := map[int64]bool{}
		for _, id := range res.QueuedIDs {
			mine[id] = true
		}
		for _, v := range res.Saves {
			if mine[v.ID] {
				s.DB.ExecContext(ctx, `UPDATE vod_follow_episodes SET save_id=? WHERE user_id=? AND source_id=? AND episode_key=?`,
					v.ID, f.UserID, f.SourceID, v.SourceKey)
			}
		}
		slots -= len(res.QueuedIDs)
		queued += len(res.QueuedIDs)
	}
	if queued > 0 {
		s.DB.ExecContext(ctx, "UPDATE vod_follows SET last_new_at=? WHERE id=?", now(), f.ID)
		slog.Info("vod follow: queued episodes", "series", f.Title, "user", f.UserID, "count", queued)
	}
	return nil
}

type followEpisode struct {
	id  int64
	key string
}

// followCandidates are a follow's next episodes not dealt with, oldest first.
func (s *Service) followCandidates(ctx context.Context, f Follow, seriesID int64, n int) ([]followEpisode, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT e.id, e.source_key FROM vod_episodes e WHERE e.series_id=?
		AND NOT EXISTS (SELECT 1 FROM vod_follow_episodes x WHERE x.user_id=? AND x.source_id=? AND x.episode_key=e.source_key)
		ORDER BY e.season=0, e.season, e.episode, e.id LIMIT ?`, seriesID, f.UserID, f.SourceID, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []followEpisode
	for rows.Next() {
		var e followEpisode
		if err := rows.Scan(&e.id, &e.key); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
