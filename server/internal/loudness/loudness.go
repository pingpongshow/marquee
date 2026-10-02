// Package loudness provides volume levelling for music (MUSIC-9): ReplayGain-style track
// and album gains (dB relative to -18 LUFS), from tags when files have them and from an
// EBU R128 measurement otherwise.
package loudness

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Reference is ReplayGain 2.0's target loudness.
const Reference = -18.0

type Gains struct {
	Track, Album, Peak *float64
}

var dbValue = regexp.MustCompile(`[-+]?\d+(\.\d+)?`)

func parseDB(s string) *float64 {
	m := dbValue.FindString(s)
	if m == "" {
		return nil
	}
	v, err := strconv.ParseFloat(m, 64)
	if err != nil {
		return nil
	}
	return &v
}

// FromTags reads REPLAYGAIN_* or R128_* tags from ffprobe output.
func FromTags(probeJSON string) Gains {
	var g Gains
	if probeJSON == "" {
		return g
	}
	var p struct {
		Format struct {
			Tags map[string]string `json:"tags"`
		} `json:"format"`
		Streams []struct {
			Tags map[string]string `json:"tags"`
		} `json:"streams"`
	}
	if json.Unmarshal([]byte(probeJSON), &p) != nil {
		return g
	}
	all := []map[string]string{p.Format.Tags}
	for _, s := range p.Streams {
		all = append(all, s.Tags)
	}
	for _, tags := range all {
		for k, v := range tags {
			switch strings.ToUpper(k) {
			case "REPLAYGAIN_TRACK_GAIN":
				g.Track = parseDB(v)
			case "REPLAYGAIN_ALBUM_GAIN":
				g.Album = parseDB(v)
			case "REPLAYGAIN_TRACK_PEAK":
				g.Peak = parseDB(v)
			case "R128_TRACK_GAIN":
				// Q7.8 fixed point relative to -23 LUFS; ReplayGain's reference is 5 dB louder.
				if q := parseDB(v); q != nil && g.Track == nil {
					t := *q/256 + 5
					g.Track = &t
				}
			case "R128_ALBUM_GAIN":
				if q := parseDB(v); q != nil && g.Album == nil {
					a := *q/256 + 5
					g.Album = &a
				}
			}
		}
	}
	return g
}

var (
	integrated = regexp.MustCompile(`I:\s+(-?\d+(\.\d+)?) LUFS`)
	truePeak   = regexp.MustCompile(`Peak:\s+(-?\d+(\.\d+)?|-inf) dBFS`)
)

// Measure runs FFmpeg's EBU R128 meter over a file and returns integrated loudness (LUFS)
// and the true peak as a linear sample value.
func Measure(ctx context.Context, ffmpeg, path string) (lufs, peak float64, err error) {
	args := []string{"-hide_banner", "-nostats", "-i", path, "-vn", "-af", "ebur128=peak=true", "-f", "null", "-"}
	cmd := exec.CommandContext(ctx, ffmpeg, args...)
	if nice, err := exec.LookPath("nice"); err == nil {
		// Background work: stay out of the way of streaming and other services.
		cmd = exec.CommandContext(ctx, nice, append([]string{"-n", "15", ffmpeg}, args...)...)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return 0, 0, fmt.Errorf("ebur128: %w", err)
	}
	// The summary is at the end of the output.
	out := stderr.String()
	if i := strings.LastIndex(out, "Summary:"); i >= 0 {
		out = out[i:]
	}
	m := integrated.FindStringSubmatch(out)
	if m == nil {
		return 0, 0, fmt.Errorf("no loudness in FFmpeg output")
	}
	lufs, _ = strconv.ParseFloat(m[1], 64)
	peak = 1
	if p := truePeak.FindStringSubmatch(out); p != nil && p[1] != "-inf" {
		db, _ := strconv.ParseFloat(p[1], 64)
		peak = math.Pow(10, db/20)
	}
	return lufs, peak, nil
}

// Service fills in gains for every track.
type Service struct {
	DB      *sql.DB
	FFmpeg  string
	Analyse func() bool // measure files without tags
	Workers int
	mu      sync.Mutex
}

type file struct {
	id    int64
	path  string
	probe string
}

// Run is the scheduled task: tags first (instant), then measurement for the rest.
func (s *Service) Run(ctx context.Context) (string, error) {
	if !s.mu.TryLock() {
		return "Already running", nil
	}
	defer s.mu.Unlock()
	rows, err := s.DB.QueryContext(ctx, `SELECT f.id, f.path, COALESCE(f.probe_json, '') FROM media_files f
		JOIN media_versions v ON v.id = f.version_id JOIN items i ON i.id = v.item_id
		WHERE i.type = 'track' AND f.loudness_source IS NULL AND f.available = 1`)
	if err != nil {
		return "", err
	}
	var todo []file
	for rows.Next() {
		var f file
		rows.Scan(&f.id, &f.path, &f.probe)
		todo = append(todo, f)
	}
	rows.Close()
	tagged, measured, failed := 0, 0, 0
	var toMeasure []file
	for _, f := range todo {
		g := FromTags(f.probe)
		if g.Track == nil {
			toMeasure = append(toMeasure, f)
			continue
		}
		s.DB.ExecContext(ctx, `UPDATE media_files SET track_gain_db = ?, album_gain_db = ?, track_peak = ?, loudness_source = 'tags' WHERE id = ?`,
			g.Track, g.Album, g.Peak, f.id)
		tagged++
	}
	if s.Analyse() && len(toMeasure) > 0 {
		slog.Info("loudness analysis starting", "files", len(toMeasure))
		start := time.Now()
		work := make(chan file)
		var wg sync.WaitGroup
		var mu sync.Mutex
		for w := 0; w < max(1, s.Workers); w++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for f := range work {
					fctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
					lufs, peak, err := Measure(fctx, s.FFmpeg, f.path)
					cancel()
					mu.Lock()
					if err != nil || math.IsInf(lufs, 0) || lufs < -70 {
						failed++
						s.DB.ExecContext(ctx, `UPDATE media_files SET loudness_source = 'failed' WHERE id = ?`, f.id)
					} else {
						measured++
						s.DB.ExecContext(ctx, `UPDATE media_files SET track_gain_db = ?, track_peak = ?, loudness_source = 'analysis' WHERE id = ?`,
							math.Round((Reference-lufs)*100)/100, peak, f.id)
					}
					mu.Unlock()
				}
			}()
		}
	feed:
		for _, f := range toMeasure {
			select {
			case work <- f:
			case <-ctx.Done():
				break feed
			}
		}
		close(work)
		wg.Wait()
		slog.Info("loudness analysis finished", "measured", measured, "failed", failed, "took", time.Since(start).Round(time.Second))
		if err := s.albumGains(ctx); err != nil {
			return "", err
		}
	}
	return fmt.Sprintf("%d from tags, %d measured, %d unreadable", tagged, measured, failed), nil
}

// albumGains derives album gain for measured albums from the energy average of their tracks.
func (s *Service) albumGains(ctx context.Context) error {
	rows, err := s.DB.QueryContext(ctx, `SELECT i.parent_id, f.id, f.track_gain_db FROM media_files f
		JOIN media_versions v ON v.id = f.version_id JOIN items i ON i.id = v.item_id
		WHERE i.type = 'track' AND f.loudness_source = 'analysis' AND i.parent_id IS NOT NULL ORDER BY i.parent_id`)
	if err != nil {
		return err
	}
	type tr struct {
		file int64
		gain float64
	}
	albums := map[int64][]tr{}
	for rows.Next() {
		var album int64
		var t tr
		rows.Scan(&album, &t.file, &t.gain)
		albums[album] = append(albums[album], t)
	}
	rows.Close()
	for _, ts := range albums {
		var energy float64
		for _, t := range ts {
			energy += math.Pow(10, (Reference-t.gain)/10)
		}
		albumLUFS := 10 * math.Log10(energy/float64(len(ts)))
		gain := math.Round((Reference-albumLUFS)*100) / 100
		for _, t := range ts {
			s.DB.ExecContext(ctx, `UPDATE media_files SET album_gain_db = ? WHERE id = ?`, gain, t.file)
		}
	}
	return nil
}
