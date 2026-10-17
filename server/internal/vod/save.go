package vod

import (
	"context"
	"errors"
	"net/url"
	"time"
)

// Helpers for saving films and episodes into the library (package vodsave).

// ErrNotSavable is a film or episode whose source isn't an Xtream Codes server (its films
// are streams, not files).
var ErrNotSavable = errors.New("only films and episodes from Xtream Codes sources can be saved")

// FileLink is the link a source's file ("{id}.{ext}") downloads from, and the user agent the
// source asks for. The link holds the password: never store or log it (see Redact).
func (s *Service) FileLink(sourceID string, episode bool, file string) (link, userAgent string, err error) {
	src, ok := s.source(sourceID)
	if !ok {
		return "", "", errors.New("the source no longer exists")
	}
	if !src.Xtream() {
		return "", "", ErrNotSavable
	}
	x := s.xtream(src)
	if episode {
		return x.seriesStreamURL(file), src.UserAgent, nil
	}
	return x.streamURL(file), src.UserAgent, nil
}

// Savable reports whether a source's films and episodes can be saved (an Xtream Codes one).
func (s *Service) Savable(sourceID string) bool {
	src, ok := s.source(sourceID)
	return ok && src.Xtream()
}

// CheckSource signs in to an Xtream Codes source to see whether it is up. The error never
// holds the password.
func (s *Service) CheckSource(ctx context.Context, sourceID string) error {
	src, ok := s.source(sourceID)
	if !ok || !src.Xtream() {
		return errors.New("the source no longer exists")
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	_, _, err := s.xtream(src).account(ctx)
	return err
}

// SourceHost is the host name a source is on ("" when unknown).
func (s *Service) SourceHost(sourceID string) string {
	src, ok := s.source(sourceID)
	if !ok {
		return ""
	}
	u, err := url.Parse(src.URL)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

// SourceName is a source's name ("" when it's gone).
func (s *Service) SourceName(sourceID string) string {
	src, _ := s.source(sourceID)
	return src.Name
}

// WatchingSources are the sources films or episodes are being watched from now: sessions
// that fetched something or reported their position within the last `within`.
func (s *Service) WatchingSources(ctx context.Context, within time.Duration) []string {
	return s.watching(ctx, within, true)
}

// WatchingStreams are the sources of the films and episodes being watched now, one per
// stream (each holds a connection to the provider).
func (s *Service) WatchingStreams(ctx context.Context, within time.Duration) []string {
	return s.watching(ctx, within, false)
}

func (s *Service) watching(ctx context.Context, within time.Duration, unique bool) []string {
	if s.Streams == nil {
		return nil
	}
	seen := map[string]bool{}
	var out []string
	for _, st := range s.Streams.List() {
		if !st.ActiveWithin(within) {
			continue
		}
		var src string
		var err error
		if st.EpisodeID > 0 {
			err = s.DB.QueryRowContext(ctx, "SELECT m.source_id FROM vod_episodes e JOIN vod_series m ON m.id=e.series_id WHERE e.id=?",
				st.EpisodeID).Scan(&src)
		} else {
			err = s.DB.QueryRowContext(ctx, "SELECT source_id FROM vod_movies WHERE id=?", st.MovieID).Scan(&src)
		}
		if err == nil && (!unique || !seen[src]) {
			seen[src] = true
			out = append(out, src)
		}
	}
	return out
}

// List returns the open sessions.
func (st *Streams) List() []*Stream {
	st.mu.Lock()
	defer st.mu.Unlock()
	out := make([]*Stream, 0, len(st.sessions))
	for _, s := range st.sessions {
		out = append(out, s)
	}
	return out
}

// ActiveWithin reports whether the session was used (fetched from or touched) recently.
func (s *Stream) ActiveWithin(d time.Duration) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return time.Since(s.last) <= d
}
