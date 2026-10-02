// Package scanner discovers media files in library folders, probes them and builds the
// item tree (movies; shows → seasons → episodes; artists → albums → tracks; videos).
// Metadata agents enrich the items afterwards.
package scanner

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"marquee/internal/library"
	"marquee/internal/probe"
)

var errSkip = errors.New("skip file")

// Progress is reported while a scan runs.
type Progress struct {
	Phase   string // walking, probing, cleanup
	Done    int
	Total   int
	Current string
}

// Stats summarises a scan.
type Stats struct {
	Found, Added, Updated, Unchanged, Moved, Missing, Restored, Skipped, Failed int
	Duration                                                                    time.Duration
}

func (s Stats) String() string {
	return fmt.Sprintf("%d files: %d added, %d updated, %d unchanged, %d moved, %d missing, %d restored, %d skipped, %d failed in %s",
		s.Found, s.Added, s.Updated, s.Unchanged, s.Moved, s.Missing, s.Restored, s.Skipped, s.Failed, s.Duration.Round(time.Second))
}

// Prober is satisfied by *probe.Prober; tests substitute a fake.
type Prober interface {
	Probe(ctx context.Context, path string) (*probe.Result, error)
}

type Scanner struct {
	DB     *sql.DB
	Prober Prober
	// Workers is the number of files probed concurrently.
	Workers int
}

type existingFile struct {
	ID, VersionID int64
	Size, MTime   int64
	Available     bool
}

type probed struct {
	c   candidate
	res *probe.Result
	err error
}

// Scan scans one library. ignore holds the server-wide ignore patterns; the library's own
// patterns are appended.
func (s *Scanner) Scan(ctx context.Context, lib library.Library, ignore []string, report func(Progress)) (Stats, error) {
	start := time.Now()
	var st Stats
	if report == nil {
		report = func(Progress) {}
	}
	if lib.Options.IgnorePatterns != nil {
		ignore = append(append([]string{}, ignore...), *lib.Options.IgnorePatterns...)
	}
	wantAudio := lib.Type == library.Music

	// 1. Walk. Roots that can't be read (offline drive) are excluded from missing-file
	// detection so a disconnected disk never marks its items as gone.
	report(Progress{Phase: "walking"})
	wr := walkResult{Dirs: map[string][]string{}}
	var liveRoots []string
	for _, root := range lib.Paths {
		if fi, err := os.Stat(root); err != nil || !fi.IsDir() {
			slog.Warn("library folder unavailable; skipping", "library", lib.Name, "path", root, "err", err)
			continue
		}
		if err := walk(root, wantAudio, ignore, &wr); err != nil {
			slog.Warn("library folder unreadable; skipping", "library", lib.Name, "path", root, "err", err)
			continue
		}
		liveRoots = append(liveRoots, root)
	}
	if len(liveRoots) == 0 {
		return st, fmt.Errorf("no library folders are available")
	}
	st.Found = len(wr.Media)

	existing, err := s.loadExisting(ctx, lib.ID)
	if err != nil {
		return st, err
	}
	knownBad, err := s.loadFailures(ctx, lib.ID)
	if err != nil {
		return st, err
	}

	// 2. Diff against the database.
	seen := make(map[string]bool, len(wr.Media))
	var work []candidate
	var restore []int64
	for _, c := range wr.Media {
		seen[c.Path] = true
		if e, ok := existing[c.Path]; ok && e.Size == c.Size && e.MTime == c.MTime {
			st.Unchanged++
			if !e.Available {
				restore = append(restore, e.ID)
			}
			continue
		}
		if f, ok := knownBad[c.Path]; ok && f.Size == c.Size && f.MTime == c.MTime {
			st.Failed++ // still unreadable; already logged when it first failed
			continue
		}
		work = append(work, c)
	}
	missing := map[string]existingFile{}
	for p, e := range existing {
		if !seen[p] && underAny(p, liveRoots) {
			missing[p] = e
		}
	}

	// 3. Probe changed/new files in parallel; write results from this goroutine.
	w := &writer{db: s.DB, lib: lib, dirs: wr.Dirs, existing: existing, missing: missing}
	if err := w.begin(ctx); err != nil {
		return st, err
	}
	defer w.rollback()
	if len(restore) > 0 {
		if err := w.restore(ctx, restore); err != nil {
			return st, err
		}
		st.Restored = len(restore)
	}

	jobs := make(chan candidate)
	results := make(chan probed)
	workers := s.Workers
	if workers < 1 {
		workers = 4
	}
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for c := range jobs {
				res, err := s.Prober.Probe(ctx, c.Path)
				select {
				case results <- probed{c, res, err}:
				case <-ctx.Done():
					return
				}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, c := range work {
			select {
			case jobs <- c:
			case <-ctx.Done():
				return
			}
		}
	}()
	go func() { wg.Wait(); close(results) }()

	done := 0
	lastReport := time.Time{}
	for r := range results {
		done++
		if time.Since(lastReport) > 500*time.Millisecond {
			report(Progress{Phase: "probing", Done: done, Total: len(work), Current: r.c.Rel})
			lastReport = time.Now()
		}
		if r.err != nil {
			if ctx.Err() != nil {
				break
			}
			slog.Warn("probe failed", "path", r.c.Path, "err", r.err)
			st.Failed++
			w.tx.ExecContext(ctx, `INSERT INTO probe_failures(path, library_id, size, mtime, error) VALUES (?, ?, ?, ?, ?)
				ON CONFLICT(path) DO UPDATE SET size = excluded.size, mtime = excluded.mtime, error = excluded.error,
				failed_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')`, r.c.Path, lib.ID, r.c.Size, r.c.MTime, r.err.Error())
			continue
		}
		if _, ok := knownBad[r.c.Path]; ok {
			w.tx.ExecContext(ctx, `DELETE FROM probe_failures WHERE path = ?`, r.c.Path) // repaired
		}
		outcome, err := w.upsert(ctx, r.c, r.res)
		switch {
		case errors.Is(err, errSkip):
			st.Skipped++
		case err != nil:
			slog.Error("scan: store file", "path", r.c.Path, "err", err)
			st.Failed++
		default:
			switch outcome {
			case added:
				st.Added++
			case moved:
				st.Moved++
			default:
				st.Updated++
			}
		}
		if err := w.maybeCommit(ctx); err != nil {
			return st, err
		}
	}
	if err := ctx.Err(); err != nil {
		return st, err
	}

	// 4. Whatever is still missing goes unavailable (not deleted: see LIB-9).
	report(Progress{Phase: "cleanup", Done: len(work), Total: len(work)})
	for _, e := range w.missing {
		if e.Available {
			st.Missing++
		}
	}
	if err := w.finish(ctx); err != nil {
		return st, err
	}
	if lib.Type == library.Movies || lib.Type == library.Shows || lib.Type == library.Anime {
		if err := s.linkExtras(ctx, lib.ID); err != nil {
			slog.Warn("scan: link extras", "err", err)
		}
	}
	st.Duration = time.Since(start)
	return st, nil
}

func (s *Scanner) loadExisting(ctx context.Context, libID int64) (map[string]existingFile, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, version_id, path, size, mtime, available FROM media_files WHERE library_id = ?`, libID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]existingFile{}
	for rows.Next() {
		var e existingFile
		var p string
		if err := rows.Scan(&e.ID, &e.VersionID, &p, &e.Size, &e.MTime, &e.Available); err != nil {
			return nil, err
		}
		out[p] = e
	}
	return out, rows.Err()
}

func (s *Scanner) loadFailures(ctx context.Context, libID int64) (map[string]existingFile, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT path, size, mtime FROM probe_failures WHERE library_id = ?`, libID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]existingFile{}
	for rows.Next() {
		var p string
		var e existingFile
		if err := rows.Scan(&p, &e.Size, &e.MTime); err != nil {
			return nil, err
		}
		out[p] = e
	}
	return out, rows.Err()
}

func underAny(p string, roots []string) bool {
	for _, r := range roots {
		if p == r || strings.HasPrefix(p, strings.TrimSuffix(r, string(filepath.Separator))+string(filepath.Separator)) {
			return true
		}
	}
	return false
}
