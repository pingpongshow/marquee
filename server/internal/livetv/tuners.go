package livetv

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Connection limits (LIVE-3, LIVE-5): a source may allow only so many streams at once
// (settings.LiveTVSource.MaxStreams; a provider with one connection, say). Recordings in
// progress and live streams each hold one, except that a channel being recorded is
// watched from the recording's own input (StartShared) and, with no connection free, a
// channel already being watched is shared with the stream that's running. At the limit
// the source's other channels are unavailable, tuning one answers ErrNoTuner, and a
// recording that needs a connection takes it from live streams on other channels.

// NoTunerError says every connection a source allows is in use, and by what.
type NoTunerError struct{ Msg string }

func (e *NoTunerError) Error() string { return e.Msg }

// IsNoTuner reports whether err is a NoTunerError.
func IsNoTuner(err error) bool {
	var e *NoTunerError
	return errors.As(err, &e)
}

// tailBack is how far before the end of a recording's newest part a shared stream starts:
// a few seconds of most channels, so it starts near live with a keyframe or two to go on.
const tailBack = 4 << 20

// MaxStreams is how many connections a source allows at once (0 = unlimited).
func (s *Service) MaxStreams(sourceID string) int {
	if s.Settings == nil {
		return 0
	}
	for _, src := range s.Settings.Get().Integrations.LiveTVSources {
		if src.ID == sourceID {
			return max(src.MaxStreams, 0)
		}
	}
	return 0
}

// sourceUse is what holds a source's connections now.
type sourceUse struct {
	recordings []ActiveRecording
	live       []*Session // streams with a connection of their own (not shared, not ended)
	all        []*Session // every running stream of the source
	pending    int
}

func (u sourceUse) n() int { return len(u.recordings) + len(u.live) + u.pending }

// receiving reports whether a channel is coming in already (recorded or watched), so it
// can be watched without another connection.
func (u sourceUse) receiving(channelID int64) bool {
	for _, r := range u.recordings {
		if r.ChannelID == channelID {
			return true
		}
	}
	for _, l := range u.all {
		if l.ChannelID == channelID {
			return true
		}
	}
	return false
}

// reason says what holds the connections: "All connections are in use: recording Gold
// Rush, watching News One".
func (u sourceUse) reason() string {
	var parts []string
	add := func(p string) {
		if !slices.Contains(parts, p) {
			parts = append(parts, p)
		}
	}
	for _, r := range u.recordings {
		add("recording " + r.Title)
	}
	for _, l := range u.live {
		name := l.ChannelName
		if name == "" {
			name = "another channel"
		}
		add("watching " + name)
	}
	if len(parts) == 0 {
		return "All connections are in use"
	}
	return "All connections are in use: " + strings.Join(parts, ", ")
}

func (s *Service) use(sourceID string) sourceUse {
	var u sourceUse
	if s.DVR != nil {
		for _, r := range s.DVR.ActiveRecordings() {
			if r.SourceID == sourceID {
				u.recordings = append(u.recordings, r)
			}
		}
	}
	if s.Sessions != nil {
		for _, l := range s.Sessions.List() {
			if l.SourceID != sourceID {
				continue
			}
			u.all = append(u.all, l)
			if !l.Shared && !l.Ended() {
				u.live = append(u.live, l)
			}
		}
		u.pending = s.Sessions.pendingFor(sourceID)
	}
	return u
}

// InUse is how many connections a source has in use now.
func (s *Service) InUse(sourceID string) int { return s.use(sourceID).n() }

// Availability returns a function telling whether a channel can be tuned now, and why
// not. Usage is read once, so call it once per listing.
func (s *Service) Availability() func(c Channel) (bool, string) {
	uses := map[string]*sourceUse{}
	return func(c Channel) (bool, string) {
		limit := s.MaxStreams(c.SourceID)
		if limit <= 0 {
			return true, ""
		}
		u, ok := uses[c.SourceID]
		if !ok {
			x := s.use(c.SourceID)
			u = &x
			uses[c.SourceID] = u
		}
		if u.n() < limit || u.receiving(c.ID) {
			return true, ""
		}
		return false, u.reason()
	}
}

// Tune starts watching a channel within its source's connection limit: from a recording
// of it in progress (no connection of its own), else on a connection of its own while one
// is free, else by joining a stream of it that's running; otherwise NoTunerError.
func (s *Service) Tune(ctx context.Context, c Channel, userID int64, o Options) (*Session, error) {
	o.SourceID, o.ChannelName = c.SourceID, c.Name
	limit := s.MaxStreams(c.SourceID)
	s.tuneMu.Lock()
	if limit > 0 {
		u := s.use(c.SourceID)
		for _, r := range u.recordings {
			if r.ChannelID == c.ID && r.Work != "" {
				s.tuneMu.Unlock()
				id := r.ID
				return s.Sessions.StartShared(ctx, c.ID, userID, r.Work, func() bool { return s.DVR.Capturing(id) }, o)
			}
		}
		if u.n() >= limit {
			for _, l := range u.all {
				if l.ChannelID == c.ID && !l.Ended() {
					s.Sessions.join(l, userID)
					s.tuneMu.Unlock()
					return l, nil
				}
			}
			s.tuneMu.Unlock()
			return nil, &NoTunerError{u.reason()}
		}
	}
	s.Sessions.addPending(c.SourceID, 1) // holds the connection while it tunes
	s.tuneMu.Unlock()
	defer s.Sessions.addPending(c.SourceID, -1)
	return s.Sessions.Start(ctx, c.ID, userID, c.StreamURL, o)
}

// makeRoom frees connections for a recording that has started on channelID: while the
// source is over its limit, live streams with their own connection stop, those of the
// recorded channel first (their viewers tune again and share the recording), then others
// (their viewers are told a recording needed the connection).
func (s *Service) makeRoom(sourceID string, channelID int64) {
	limit := s.MaxStreams(sourceID)
	if limit <= 0 || s.Sessions == nil {
		return
	}
	s.tuneMu.Lock()
	defer s.tuneMu.Unlock()
	u := s.use(sourceID)
	n := u.n()
	if n <= limit {
		return
	}
	live := slices.Clone(u.live)
	slices.SortStableFunc(live, func(a, b *Session) int {
		switch {
		case a.ChannelID == channelID && b.ChannelID != channelID:
			return -1
		case b.ChannelID == channelID && a.ChannelID != channelID:
			return 1
		}
		return 0
	})
	stopped := 0
	for _, l := range live {
		if n <= limit {
			break
		}
		reason := ReasonPreempted
		if l.ChannelID == channelID {
			reason = ReasonMoved
		}
		slog.Info("live TV: stopping a live stream for a recording", "channel", l.ChannelID, "source", sourceID, "reason", reason)
		if s.Sessions.StopWithReason(l.ID, reason) {
			stopped++
		}
		n--
	}
	if stopped > 0 {
		time.Sleep(time.Second) // let the provider see the connection close
	}
}

func partPath(dir string, i int) string { return filepath.Join(dir, fmt.Sprintf("part-%03d.ts", i)) }

// lastPart is the index of the newest part in dir (-1 when there's none).
func lastPart(dir string) int {
	i := -1
	for {
		if _, err := os.Stat(partPath(dir, i+1)); err != nil {
			return i
		}
		i++
	}
}

// waitPart waits for the recording in dir to have written enough to probe, returning its
// newest part.
func waitPart(ctx context.Context, dir string, capturing func() bool) (string, error) {
	deadline := time.Now().Add(20 * time.Second)
	for {
		if i := lastPart(dir); i >= 0 {
			p := partPath(dir, i)
			if fi, err := os.Stat(p); err == nil && fi.Size() >= 128<<10 {
				return p, nil
			}
		}
		if !capturing() || time.Now().After(deadline) {
			return "", ErrUnavailable
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// tailReader reads a recording's parts as the recorder writes them: from back bytes
// before the end of the newest part, on into later parts, waiting at the end for more
// while the recorder is capturing.
type tailReader struct {
	ctx       context.Context
	dir       string
	capturing func() bool
	f         *os.File
	idx       int
}

func openTail(ctx context.Context, dir string, capturing func() bool, back int64) (*tailReader, error) {
	i := lastPart(dir)
	if i < 0 {
		return nil, errors.New("the recording hasn't written anything yet")
	}
	f, err := os.Open(partPath(dir, i))
	if err != nil {
		return nil, err
	}
	if fi, err := f.Stat(); err == nil && fi.Size() > back {
		off := (fi.Size() - back) / 188 * 188 // on an MPEG-TS packet boundary
		f.Seek(off, io.SeekStart)
	}
	return &tailReader{ctx: ctx, dir: dir, capturing: capturing, f: f, idx: i}, nil
}

func (t *tailReader) Read(p []byte) (int, error) {
	for {
		if err := t.ctx.Err(); err != nil {
			return 0, err
		}
		n, err := t.f.Read(p)
		if n > 0 {
			return n, nil
		}
		if err != nil && !errors.Is(err, io.EOF) {
			return 0, err
		}
		if f, err := os.Open(partPath(t.dir, t.idx+1)); err == nil {
			t.f.Close()
			t.f, t.idx = f, t.idx+1 // the recorder reconnected and began a new part
			continue
		}
		if !t.capturing() {
			return 0, io.EOF
		}
		select {
		case <-t.ctx.Done():
			return 0, t.ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func (t *tailReader) Close() error { return t.f.Close() }
