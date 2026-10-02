package introdetect

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

const (
	// introSearch is how much of an episode's start is searched: a quarter, at most 10 min.
	introSearch = 10 * 60.0
	// creditsSearch is how much of the end is searched.
	creditsSearch = 5 * 60.0
	minSegment    = 15.0
	maxIntro      = 150.0
	maxGap        = 3.5
	// partners is how many other episodes each one is compared with before giving up.
	partners     = 3
	minEpisode   = 5 * time.Minute
	endTolerance = 10.0
)

// Service detects markers season by season.
type Service struct {
	DB      *sql.DB
	FFmpeg  string
	Dir     string // fingerprint cache: <cache>/fingerprints
	Enabled func() bool
	CUDA    bool // decode video on the NVIDIA GPU for credits detection

	done, total atomic.Int64
}

func (s *Service) Progress() (done, total int) { return int(s.done.Load()), int(s.total.Load()) }

type episode struct {
	itemID, fileID int64
	idx            int
	path           string
	mtime          int64
	duration       float64 // seconds
	pending        bool
	found          bool // an earlier check found both an intro and credits
}

// Run looks at seasons with episodes that haven't been checked, newest first. It stops
// when ctx ends and continues next time.
func (s *Service) Run(ctx context.Context) (string, error) {
	if s.Enabled != nil && !s.Enabled() {
		return "Turned off", nil
	}
	seasons, err := s.pendingSeasons(ctx)
	if err != nil {
		return "", err
	}
	movies, err := s.pendingMovies(ctx)
	if err != nil {
		return "", err
	}
	s.done.Store(0)
	s.total.Store(int64(len(seasons) + len(movies)))
	start := time.Now()
	var intros, credits int
	for _, season := range seasons {
		if ctx.Err() != nil {
			break
		}
		i, c, err := s.season(ctx, season)
		if err != nil && ctx.Err() == nil {
			slog.Warn("intro detection", "season", season, "err", err)
		}
		intros += i
		credits += c
		s.done.Add(1)
	}
	for _, m := range movies {
		if ctx.Err() != nil {
			break
		}
		_, length := movieWindow(m)
		r, err := s.visualCredits(ctx, m.path, m.duration-length, length, s.CUDA)
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			slog.Debug("movie credits", "path", m.path, "err", err)
		}
		if r != nil {
			if m.duration-r.End < endTolerance {
				r.End = m.duration
			}
			credits++
		}
		if err := s.save(ctx, m, nil, r); err != nil && ctx.Err() == nil {
			return "", err
		}
		s.done.Add(1)
	}
	msg := fmt.Sprintf("Checked %d seasons and movies in %s: found %d intros and %d credits", s.done.Load(), time.Since(start).Round(time.Second), intros, credits)
	if err := ctx.Err(); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return msg + fmt.Sprintf("; %d left for the next maintenance window", int64(len(seasons)+len(movies))-s.done.Load()), nil
		}
		return "", err
	}
	return msg, nil
}

func (s *Service) pendingSeasons(ctx context.Context) ([]int64, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT e.parent_id, MAX(e.added_at) AS added FROM items e
		JOIN media_versions v ON v.item_id = e.id
		JOIN media_files f ON f.version_id = v.id AND f.part_index = 0
		LEFT JOIN marker_scans ms ON ms.file_id = f.id AND ms.mtime = f.mtime
		WHERE e.type = 'episode' AND e.parent_id IS NOT NULL AND f.available = 1 AND f.duration_ms >= ? AND ms.file_id IS NULL
		GROUP BY e.parent_id ORDER BY added DESC`, minEpisode.Milliseconds())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		var added string
		if err := rows.Scan(&id, &added); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// pendingMovies lists movies not checked yet (credits only), newest first.
func (s *Service) pendingMovies(ctx context.Context) ([]episode, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT i.id, f.id, f.path, f.mtime, f.duration_ms FROM items i
		JOIN media_versions v ON v.item_id = i.id
		JOIN media_files f ON f.version_id = v.id AND f.part_index = 0
		LEFT JOIN marker_scans ms ON ms.file_id = f.id AND ms.mtime = f.mtime
		WHERE i.type = 'movie' AND f.available = 1 AND f.duration_ms >= ? AND ms.file_id IS NULL
		GROUP BY i.id ORDER BY i.added_at DESC`, (40 * time.Minute).Milliseconds())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []episode
	for rows.Next() {
		var e episode
		var ms int64
		if err := rows.Scan(&e.itemID, &e.fileID, &e.path, &e.mtime, &ms); err != nil {
			return nil, err
		}
		e.duration = float64(ms) / 1000
		out = append(out, e)
	}
	return out, rows.Err()
}

// movieWindow is where movie credits are looked for: the last tenth, at most 12 minutes.
func movieWindow(e episode) (start, length float64) {
	length = min(12*60, e.duration/10)
	return e.duration - length, length
}

func (s *Service) episodes(ctx context.Context, season int64) ([]episode, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT e.id, f.id, COALESCE(e.idx, 0), f.path, f.mtime, f.duration_ms, ms.file_id IS NULL,
		       COALESCE(ms.intro AND ms.credits, 0)
		FROM items e
		JOIN media_versions v ON v.item_id = e.id
		JOIN media_files f ON f.version_id = v.id AND f.part_index = 0
		LEFT JOIN marker_scans ms ON ms.file_id = f.id AND ms.mtime = f.mtime
		WHERE e.parent_id = ? AND e.type = 'episode' AND f.available = 1 AND f.duration_ms >= ?
		GROUP BY e.id ORDER BY e.idx`, season, minEpisode.Milliseconds())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []episode
	for rows.Next() {
		var e episode
		var ms int64
		if err := rows.Scan(&e.itemID, &e.fileID, &e.idx, &e.path, &e.mtime, &ms, &e.pending, &e.found); err != nil {
			return nil, err
		}
		e.duration = float64(ms) / 1000
		out = append(out, e)
	}
	return out, rows.Err()
}

// season compares each unchecked episode with its neighbours and records what it finds.
func (s *Service) season(ctx context.Context, season int64) (intros, credits int, err error) {
	eps, err := s.episodes(ctx, season)
	if err != nil {
		return 0, 0, err
	}
	// New episodes give earlier ones something to compare with: re-check those that found
	// nothing last time (their fingerprints are cached, so it's cheap).
	for i := range eps {
		if !eps[i].pending && !eps[i].found {
			eps[i].pending = true
		}
	}
	for _, e := range eps {
		if !e.pending || ctx.Err() != nil {
			continue
		}
		others := make([]episode, 0, len(eps)-1)
		for _, o := range eps {
			if o.fileID != e.fileID {
				others = append(others, o)
			}
		}
		// Nearest episodes first: intros change between seasons and sometimes within one.
		sort.SliceStable(others, func(i, j int) bool { return abs(others[i].idx-e.idx) < abs(others[j].idx-e.idx) })
		if len(others) > partners {
			others = others[:partners]
		}
		intro, credit := s.detect(ctx, e, others)
		if ctx.Err() != nil {
			return intros, credits, ctx.Err()
		}
		if err := s.save(ctx, e, intro, credit); err != nil {
			return intros, credits, err
		}
		if intro != nil {
			intros++
		}
		if credit != nil {
			credits++
		}
	}
	return intros, credits, nil
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func introWindow(e episode) (start, length float64) { return 0, min(introSearch, e.duration/4) }

func creditsWindow(e episode) (start, length float64) {
	length = min(creditsSearch, e.duration/4)
	return e.duration - length, length
}

// detect returns e's intro and credits (seconds from the start of the file), if found.
func (s *Service) detect(ctx context.Context, e episode, others []episode) (intro, credits *Range) {
	if fa, err := s.fingerprint(ctx, e, "intro"); err == nil {
		for _, o := range others {
			fb, err := s.fingerprint(ctx, o, "intro")
			if err != nil {
				continue
			}
			ra, _, ok := Match(fa, fb, minSegment, maxGap)
			if ok && ra.Len() <= maxIntro {
				intro = &ra
				break
			}
		}
	} else if ctx.Err() == nil {
		slog.Debug("intro fingerprint", "path", e.path, "err", err)
	}
	if fa, err := s.fingerprint(ctx, e, "credits"); err == nil {
		offset, length := creditsWindow(e)
		for _, o := range others {
			fb, err := s.fingerprint(ctx, o, "credits")
			if err != nil {
				continue
			}
			if ra, _, ok := Match(fa, fb, minSegment, maxGap); ok {
				r := Range{offset + ra.Start, offset + ra.End}
				if offset+length-r.End < endTolerance {
					r.End = e.duration // credits run to the end
				}
				credits = &r
				break
			}
		}
	}
	if credits == nil && ctx.Err() == nil {
		// End-credit songs often change every episode; fall back to the look of a credits roll.
		start, length := creditsWindow(e)
		if r, err := s.visualCredits(ctx, e.path, start, length, s.CUDA); err == nil && r != nil {
			if e.duration-r.End < endTolerance {
				r.End = e.duration
			}
			credits = r
		}
	}
	return intro, credits
}

// fingerprint returns a region's Chromaprint points, cached on disk by file and mtime.
func (s *Service) fingerprint(ctx context.Context, e episode, region string) ([]uint32, error) {
	cache := filepath.Join(s.Dir, fmt.Sprintf("%d-%s-%d.bin", e.fileID, region, e.mtime))
	if b, err := os.ReadFile(cache); err == nil {
		return decode(b), nil
	}
	start, length := introWindow(e)
	if region == "credits" {
		start, length = creditsWindow(e)
	}
	args := []string{"-hide_banner", "-v", "error", "-ss", fmt.Sprintf("%.3f", start), "-t", fmt.Sprintf("%.3f", length),
		"-i", e.path, "-map", "0:a:0", "-ac", "2", "-f", "chromaprint", "-fp_format", "raw", "-"}
	cmd := exec.CommandContext(ctx, s.FFmpeg, args...)
	if nice, err := exec.LookPath("nice"); err == nil {
		cmd = exec.CommandContext(ctx, nice, append([]string{"-n", "15", s.FFmpeg}, args...)...)
	}
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("chromaprint: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	if float64(out.Len()/4)*PointSeconds < minSegment {
		return nil, errors.New("too little audio")
	}
	if err := os.MkdirAll(s.Dir, 0o755); err == nil {
		os.WriteFile(cache, out.Bytes(), 0o644)
	}
	return decode(out.Bytes()), nil
}

func decode(b []byte) []uint32 {
	out := make([]uint32, len(b)/4)
	for i := range out {
		out[i] = binary.LittleEndian.Uint32(b[i*4:])
	}
	return out
}

// save replaces e's detected markers. Markers from the file, Plex or an admin win: a kind
// that already has one of those is left alone.
func (s *Service) save(ctx context.Context, e episode, intro, credits *Range) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM markers WHERE file_id = ? AND source = 'detected' AND kind IN ('intro', 'credits')`, e.fileID); err != nil {
		return err
	}
	put := func(kind string, r *Range) error {
		if r == nil {
			return nil
		}
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM markers WHERE file_id = ? AND kind = ?`, e.fileID, kind).Scan(&n); err != nil || n > 0 {
			return err
		}
		_, err := tx.ExecContext(ctx, `INSERT INTO markers (file_id, kind, start_ms, end_ms, source) VALUES (?, ?, ?, ?, 'detected')`,
			e.fileID, kind, int64(r.Start*1000), int64(r.End*1000))
		return err
	}
	if err := put("intro", intro); err != nil {
		return err
	}
	if err := put("credits", credits); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO marker_scans (file_id, mtime, intro, credits) VALUES (?, ?, ?, ?)
		ON CONFLICT(file_id) DO UPDATE SET mtime = excluded.mtime, intro = excluded.intro, credits = excluded.credits,
		scanned_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')`, e.fileID, e.mtime, intro != nil, credits != nil); err != nil {
		return err
	}
	return tx.Commit()
}
