package vodsave

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"marquee/internal/playback"
	"marquee/internal/vod"
)

// The quality of films and episodes on demand: the source's details when they describe the
// video, else a probe of the file. Kept in vod_quality; checked only for titles an admin
// opens that have a copy in the library, one probe at a time, never over a whole catalogue.

const (
	// QualityMaxAge is how long a film's or episode's quality is kept before it's checked again.
	QualityMaxAge = 30 * 24 * time.Hour
	// qualityRetry is how long after a failed probe it's tried again.
	qualityRetry = 24 * time.Hour
	// maxQualityQueue bounds the titles waiting for a background check.
	maxQualityQueue = 200
)

type qualityKey struct {
	episode bool
	id      int64
}

func toQuality(q vod.Quality) Quality {
	return Quality{Width: q.Width, Height: q.Height, Codec: q.VideoCodec, HDR: q.HDR, BitrateKbps: q.BitrateKbps}
}

// fresh reports whether a stored quality needs no new check.
func fresh(q vod.Quality) bool {
	age := time.Since(q.CheckedAt)
	if q.Error != "" {
		return age < qualityRetry
	}
	return age < QualityMaxAge
}

func (s *Service) probeGap() time.Duration { return or(s.ProbeGap, 2*time.Second) }

// probeFile reads a remote file's video.
func (s *Service) probeFile(ctx context.Context, link, ua, sourceID string) (vod.Quality, error) {
	if s.ProbeRemote != nil {
		return s.ProbeRemote(ctx, link, ua)
	}
	res, err := playback.ProbeRemote(ctx, s.FFprobe, link, ua, func(m string) string { return s.VOD.Redact(sourceID, m) })
	if err != nil {
		return vod.Quality{}, err
	}
	q := vod.Quality{BitrateKbps: res.BitrateKbps, Origin: "probe"}
	if v := res.Video(); v != nil {
		q.Width, q.Height, q.VideoCodec = v.Width, v.Height, v.Codec
		q.HDR = v.HDRFormat
	}
	if !q.Known() {
		return q, errors.New("the file has no video")
	}
	return q, nil
}

// checkQuality finds out what a film's or episode's file is and keeps it: the source's
// details first (films), else a probe. Probes run one at a time, ProbeGap apart.
func (s *Service) checkQuality(ctx context.Context, k qualityKey) (vod.Quality, error) {
	s.init()
	var srcID, file string
	var err error
	if k.episode {
		err = s.DB.QueryRowContext(ctx, `SELECT m.source_id, e.stream_url FROM vod_episodes e JOIN vod_series m ON m.id=e.series_id
			WHERE e.id=?`, k.id).Scan(&srcID, &file)
	} else {
		if q, ok, err := s.VOD.MovieQualityFromSource(ctx, k.id); err == nil && ok {
			return q, nil
		} else if errors.Is(err, vod.ErrNotFound) || errors.Is(err, vod.ErrNotSavable) {
			return vod.Quality{}, err
		}
		err = s.DB.QueryRowContext(ctx, "SELECT source_id, stream_url FROM vod_movies WHERE id=?", k.id).Scan(&srcID, &file)
	}
	if err != nil {
		return vod.Quality{}, err
	}
	link, ua, err := s.VOD.FileLink(srcID, k.episode, file)
	if err != nil {
		return vod.Quality{}, err
	}
	s.probeMu.Lock()
	defer s.probeMu.Unlock()
	if wait := s.probeGap() - time.Since(s.lastProbe); wait > 0 {
		select {
		case <-ctx.Done():
			return vod.Quality{}, ctx.Err()
		case <-time.After(wait):
		}
	}
	q, perr := s.probeFile(ctx, link, ua, srcID)
	s.lastProbe = time.Now()
	if ctx.Err() != nil {
		return vod.Quality{}, ctx.Err()
	}
	if perr != nil {
		msg := s.VOD.Redact(srcID, perr.Error())
		q = vod.Quality{Origin: "probe", Error: msg}
	}
	q.Origin, q.CheckedAt = "probe", time.Now()
	if k.episode {
		err = s.VOD.StoreEpisodeQuality(ctx, k.id, q)
	} else {
		err = s.VOD.StoreMovieQuality(ctx, k.id, q)
	}
	if perr != nil {
		return q, errors.New(q.Error)
	}
	return q, err
}

// requestQuality asks the background checker to look at a title (ignored when the queue is
// full or it's already waiting).
func (s *Service) requestQuality(k qualityKey) {
	s.init()
	s.mu.Lock()
	if !s.qualityWaiting[k] && len(s.qualityQueue) < maxQualityQueue {
		s.qualityWaiting[k] = true
		s.qualityQueue = append(s.qualityQueue, k)
	}
	s.mu.Unlock()
	select {
	case s.qualityWake <- struct{}{}:
	default:
	}
}

// qualityPending reports whether a title waits for (or is in) a background check.
func (s *Service) qualityPending(k qualityKey) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.qualityWaiting[k]
}

// runQuality checks the titles asked for, one at a time, until ctx ends.
func (s *Service) runQuality(ctx context.Context) {
	for {
		s.mu.Lock()
		var k qualityKey
		ok := len(s.qualityQueue) > 0
		if ok {
			k = s.qualityQueue[0]
			s.qualityQueue = s.qualityQueue[1:]
		}
		s.mu.Unlock()
		if !ok {
			select {
			case <-ctx.Done():
				return
			case <-s.qualityWake:
			}
			continue
		}
		if _, err := s.checkQuality(ctx, k); err != nil && ctx.Err() == nil {
			slog.Info("vod quality check failed", "episode", k.episode, "id", k.id, "err", err)
		}
		s.mu.Lock()
		delete(s.qualityWaiting, k)
		s.mu.Unlock()
		if ctx.Err() != nil {
			return
		}
	}
}

// vodQuality is what's known of a title's file: known false while nothing is; pending
// while a check is on its way. With sync it's checked now when needed (a save request).
func (s *Service) vodQuality(ctx context.Context, k qualityKey, stored *vod.Quality, sync bool) (q Quality, known, pending bool, note string) {
	if stored != nil && fresh(*stored) {
		if stored.Error != "" {
			return Quality{}, false, false, "Couldn't check the quality of the VOD copy: " + stored.Error
		}
		return toQuality(*stored), stored.Known(), false, ""
	}
	if sync {
		vq, err := s.checkQuality(ctx, k)
		if err != nil {
			return Quality{}, false, false, "Couldn't check the quality of the VOD copy: " + strings.TrimSpace(err.Error())
		}
		return toQuality(vq), vq.Known(), false, ""
	}
	s.requestQuality(k)
	if stored != nil && stored.Error == "" && stored.Known() {
		return toQuality(*stored), true, true, "" // the old answer while a new one comes
	}
	return Quality{}, false, true, "Checking the quality of the VOD copy…"
}
