// Package speakers plays music on Sonos rooms (MUSIC-26). The server finds the rooms on its own
// network, hands each one a URL to fetch from the server and follows along, so whatever asked
// for the music can close. A room's session is the queue, its position in it and who it's for;
// sessions live in memory, like remote control, and don't survive a restart.
package speakers

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"
)

var (
	// ErrNotFound means there's no such room.
	ErrNotFound = errors.New("speaker not found")
	// ErrUnreachable means the room didn't answer, or answered with something unusable.
	ErrUnreachable = errors.New("speaker unreachable")
	// ErrNotOurs means Marquee isn't what's playing in the room.
	ErrNotOurs = errors.New("marquee isn't playing there")
	// ErrNothingToPlay means none of the songs could be played.
	ErrNothingToPlay = errors.New("nothing to play")
	// ErrRefused means the room can't do that right now (seek while stopped, say).
	ErrRefused = errors.New("the speaker can't do that now")
)

const (
	// MaxQueue is how many songs a session holds.
	MaxQueue = 2000
	// Rooms are read again this often while they're in use, and when a listing finds them older
	// than listFresh.
	refreshEvery = 5 * time.Minute
	listFresh    = time.Minute
	// usedRecently is how long after a listing they count as in use: a server nobody plays to
	// speakers from has no reason to search the network.
	usedRecently = time.Hour
	// searchWait is how long a search listens for answers.
	searchWait = 1500 * time.Millisecond
	// refreshTimeout bounds finding the rooms, whoever is waiting.
	refreshTimeout = 10 * time.Second
	// giveUpAfter ends a session whose speaker has stopped answering.
	giveUpAfter = time.Minute
)

// Room is what the apps call a speaker: a home theatre or a stereo pair is one room. Commands
// about what's playing go to its group's coordinator; volume is the room's own.
type Room struct {
	ID, Name, Model string
	Addr            string // the room's main player, host:port
	Coordinator     string // the id of the player leading its group
	CoordinatorAddr string
}

// Track is a queue entry, with what the speaker shows for it.
type Track struct {
	ItemID int64
	// AddedBy is whose play it is: the person who asked for it, who may not be who started the
	// session (0 = the session's owner).
	AddedBy              int64
	Title, Artist, Album string
	DurationMS           int64
	ArtworkID            int64 // 0 = none
}

// Status is what a room is doing.
type Status struct {
	Room
	State                  string // playing, paused, stopped, loading
	Volume                 int
	Marquee                bool
	ItemID                 int64 // the song playing, when Marquee
	PositionMS, DurationMS int64
	QueueIndex             int // when Marquee
	QueueLength            int
}

// Service finds rooms and runs their sessions.
type Service struct {
	DB *sql.DB
	// Convert makes an M4A at out for what a speaker can't play, resampled to sampleRate
	// unless that's 0.
	Convert func(ctx context.Context, userID, itemID, fileID int64, sampleRate int, out string) error
	// Art finds an artwork's file at a width (the server's image code).
	Art func(ctx context.Context, artworkID int64, width int) (path, contentType string, err error)
	// Dir holds conversions, per session: <transcode dir>/speakers.
	Dir string
	// Port is the port the server listens on, for the URLs given to speakers.
	Port int
	// Hosts, when set, are the only players ever contacted (host or host:port): there's no
	// network search, and a room is listed only if it and its coordinator are among them.
	Hosts []string

	// For tests: how often sessions poll, how long after starting a track a speaker may still
	// look stopped, and the network search.
	PollEvery time.Duration
	Grace     *time.Duration
	search    func(ctx context.Context) []string

	once   sync.Once
	client *http.Client
	allow  map[string]bool

	refreshMu sync.Mutex // one refresh at a time

	mu        sync.Mutex // guards what follows; a session's lock is never taken while holding it
	base      context.Context
	rooms     []Room
	addrs     map[string]string // every player's address by id, satellites too
	models    map[string]string
	refreshed time.Time
	asked     time.Time
	sessions  map[string]*session // by coordinator
	tokens    map[string]*session
	playLocks map[string]*sync.Mutex // by coordinator: one play at a time, so two can't race for a room
}

func (s *Service) init() {
	s.once.Do(func() {
		s.client = &http.Client{
			Timeout: 4 * time.Second,
			// Speakers are on the local network, whatever proxy the server has for the internet.
			Transport:     &http.Transport{DialContext: dialer.DialContext, MaxIdleConnsPerHost: 2, IdleConnTimeout: 30 * time.Second},
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		}
		s.addrs, s.models = map[string]string{}, map[string]string{}
		s.sessions, s.tokens, s.playLocks = map[string]*session{}, map[string]*session{}, map[string]*sync.Mutex{}
		s.base = context.Background()
		if s.PollEvery <= 0 {
			s.PollEvery = 2 * time.Second
		}
		if s.Grace == nil {
			g := 10 * time.Second
			s.Grace = &g
		}
		if len(s.Hosts) > 0 {
			s.allow = map[string]bool{}
			for _, h := range s.Hosts {
				s.allow[withPort(h)] = true
			}
		}
		if s.search == nil {
			s.search = searchNetwork
		}
	})
}

// Run clears conversions left by a previous run and refreshes the rooms while they're in use,
// until ctx ends.
func (s *Service) Run(ctx context.Context) {
	s.init()
	s.mu.Lock()
	s.base = ctx
	s.mu.Unlock()
	if s.Dir != "" {
		if err := os.RemoveAll(s.Dir); err != nil {
			slog.Warn("speakers: clear conversions", "err", err)
		}
	}
	t := time.NewTicker(refreshEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			for _, ss := range s.liveSessions() {
				ss.mu.Lock()
				ss.finish("the server is stopping")
				ss.mu.Unlock()
			}
			return
		case <-t.C:
			s.mu.Lock()
			used := time.Since(s.asked) < usedRecently || len(s.sessions) > 0
			s.mu.Unlock()
			if used {
				s.refresh(ctx)
			}
		}
	}
}

func (s *Service) liveSessions() []*session {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*session, 0, len(s.sessions))
	for _, ss := range s.sessions {
		out = append(out, ss)
	}
	return out
}

// session returns the session driving a coordinator, if any.
func (s *Service) session(coordinator string) *session {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessions[coordinator]
}

// List returns the rooms that answer, by name; none is an empty list, never an error.
func (s *Service) List(ctx context.Context) []Status {
	rooms := s.Rooms(ctx)
	got := make([]*Status, len(rooms))
	var wg sync.WaitGroup
	for i, r := range rooms {
		wg.Go(func() {
			if st, err := s.status(ctx, r); err == nil {
				got[i] = &st
			}
		})
	}
	wg.Wait()
	out := make([]Status, 0, len(got))
	for _, st := range got {
		if st != nil {
			out = append(out, *st)
		}
	}
	return out
}

// Get returns what one room is doing.
func (s *Service) Get(ctx context.Context, id string) (Status, error) {
	r, err := s.room(ctx, id)
	if err != nil {
		return Status{}, err
	}
	return s.status(ctx, r)
}

// status reads a room. With a session there, the read is also one of its polls, so what's
// reported is never behind the speaker.
func (s *Service) status(ctx context.Context, r Room) (Status, error) {
	vol, err := s.volume(ctx, r.Addr)
	if err != nil {
		return Status{}, unreachable(err)
	}
	st := Status{Room: r, Volume: vol}
	if ss := s.session(r.Coordinator); ss != nil {
		ss.mu.Lock()
		ss.tick(ctx)
		if !ss.ended {
			ss.fill(&st)
			ss.mu.Unlock()
			return st, nil
		}
		ss.mu.Unlock()
	}
	state, err := s.transportInfo(ctx, r.CoordinatorAddr)
	if err != nil {
		return Status{}, unreachable(err)
	}
	st.State = apiState(state)
	if pos, err := s.positionInfo(ctx, r.CoordinatorAddr); err == nil {
		st.PositionMS, st.DurationMS = pos.RelMS, pos.DurationMS
	}
	return st, nil
}

// Play replaces what a room is playing with tracks, starting at start, startMS into it.
// owner is who the session is for and label is what they're played from (for the statistics).
func (s *Service) Play(ctx context.Context, id string, owner int64, tracks []Track, start int, startMS int64, label string) (Status, error) {
	if len(tracks) == 0 {
		return Status{}, ErrNothingToPlay
	}
	r, err := s.room(ctx, id)
	if err != nil {
		return Status{}, err
	}
	if err := s.begin(ctx, r, owner, tracks, start, startMS, label); err != nil {
		return Status{}, err
	}
	return s.status(ctx, r)
}

// begin replaces the room's session. Only this room's plays wait for it: a speaker that hangs
// holds up nobody else's.
func (s *Service) begin(ctx context.Context, r Room, owner int64, tracks []Track, start int, startMS int64, label string) error {
	s.mu.Lock()
	lock := s.playLocks[r.Coordinator]
	if lock == nil {
		lock = &sync.Mutex{}
		s.playLocks[r.Coordinator] = lock
	}
	s.mu.Unlock()
	lock.Lock()
	defer lock.Unlock()
	// Before anything changes: a room that isn't there keeps what it had.
	if _, err := s.transportInfo(ctx, r.CoordinatorAddr); err != nil {
		return unreachable(err)
	}
	if old := s.session(r.Coordinator); old != nil {
		old.mu.Lock()
		old.finish("replaced")
		old.mu.Unlock()
	}
	ss := s.newSession(r, owner, tracks, start, startMS, label)
	ss.mu.Lock()
	err := ss.startIfReady(ctx)
	if err != nil {
		ss.finish("couldn't start")
	}
	ended := ss.ended
	ss.mu.Unlock()
	if err != nil {
		return unreachable(err)
	}
	if ended {
		return ErrNothingToPlay
	}
	go ss.run()
	return nil
}

// Add puts songs at the end of the room's session. A queue about to pass MaxQueue loses what's
// been played already, as a radio that keeps topping itself up would, then the newest songs.
func (s *Service) Add(ctx context.Context, id string, tracks []Track) (Status, error) {
	r, err := s.room(ctx, id)
	if err != nil {
		return Status{}, err
	}
	ss, err := s.ours(ctx, r)
	if err != nil {
		return Status{}, err
	}
	ss.add(tracks)
	if ss.nextSet < 0 {
		ss.prepareAhead()
	}
	ss.mu.Unlock()
	return s.status(ctx, r)
}

// ours returns the room's session after a poll, locked; ErrNotOurs if there is none or the
// poll finds another app has taken over.
func (s *Service) ours(ctx context.Context, r Room) (*session, error) {
	ss := s.session(r.Coordinator)
	if ss == nil {
		return nil, ErrNotOurs
	}
	ss.mu.Lock()
	ss.tick(ctx)
	if ss.ended {
		ss.mu.Unlock()
		return nil, ErrNotOurs
	}
	return ss, nil
}

// Pause, Resume and Stop work on whatever is playing, Marquee or not. A speaker already in
// that state answers with a fault, which isn't a problem worth reporting.

// Pause pauses a room. With a song still being prepared there's nothing on the speaker to
// pause, so the song waits for a resume when it's ready.
func (s *Service) Pause(ctx context.Context, id string) (Status, error) {
	return s.transport(ctx, id, func(r Room) error {
		if ss := s.session(r.Coordinator); ss != nil {
			ss.mu.Lock()
			pending := ss.pending
			if pending {
				ss.paused = true
			}
			ss.mu.Unlock()
			if pending {
				return nil
			}
		}
		return ignoreFault(s.pause(ctx, r.CoordinatorAddr))
	})
}

// Resume resumes a room.
func (s *Service) Resume(ctx context.Context, id string) (Status, error) {
	return s.transport(ctx, id, func(r Room) error {
		if ss := s.session(r.Coordinator); ss != nil {
			ss.mu.Lock()
			defer ss.mu.Unlock()
			if ss.pending {
				ss.paused = false
				return ss.startIfReady(ctx)
			}
		}
		return s.play(ctx, r.CoordinatorAddr)
	})
}

// Stop stops a room, and ends the session there.
func (s *Service) Stop(ctx context.Context, id string) (Status, error) {
	return s.transport(ctx, id, func(r Room) error {
		if err := ignoreFault(s.stop(ctx, r.CoordinatorAddr)); err != nil {
			return err
		}
		if ss := s.session(r.Coordinator); ss != nil {
			ss.mu.Lock()
			ss.finish("stopped")
			ss.mu.Unlock()
		}
		return nil
	})
}

// SetVolume sets a room's volume, 0–100.
func (s *Service) SetVolume(ctx context.Context, id string, volume int) (Status, error) {
	return s.transport(ctx, id, func(r Room) error { return s.setVolume(ctx, r.Addr, volume) })
}

// Next moves on to the next song of the session (past the last one, it ends).
func (s *Service) Next(ctx context.Context, id string) (Status, error) {
	return s.control(ctx, id, func(ss *session) error { return ss.next(ctx) })
}

// Previous restarts the song, or goes back one when it has only just started.
func (s *Service) Previous(ctx context.Context, id string) (Status, error) {
	return s.control(ctx, id, func(ss *session) error { return ss.previous(ctx) })
}

// Seek moves within the song playing in the session.
func (s *Service) Seek(ctx context.Context, id string, positionMS int64) (Status, error) {
	return s.control(ctx, id, func(ss *session) error { return ss.seek(ctx, positionMS) })
}

func (s *Service) transport(ctx context.Context, id string, do func(Room) error) (Status, error) {
	r, err := s.room(ctx, id)
	if err != nil {
		return Status{}, err
	}
	if err := do(r); err != nil {
		return Status{}, commandError(err)
	}
	return s.status(ctx, r)
}

func (s *Service) control(ctx context.Context, id string, do func(*session) error) (Status, error) {
	r, err := s.room(ctx, id)
	if err != nil {
		return Status{}, err
	}
	ss, err := s.ours(ctx, r)
	if err != nil {
		return Status{}, err
	}
	err = do(ss)
	ss.mu.Unlock()
	if err != nil {
		return Status{}, commandError(err)
	}
	return s.status(ctx, r)
}

// DropUser ends a deleted user's sessions at once: their songs stop being served.
func (s *Service) DropUser(ctx context.Context, userID int64) {
	s.init()
	for _, ss := range s.liveSessions() {
		ss.mu.Lock()
		if ss.owner == userID && !ss.ended {
			if addr, ok := s.addrOf(ss.coordinator); ok {
				s.stop(ctx, addr)
			}
			ss.finish("its owner was deleted")
		}
		ss.mu.Unlock()
	}
}

// unreachable keeps the reason for the log and tells the caller the one thing that matters.
func unreachable(err error) error {
	if errors.Is(err, ErrUnreachable) {
		return err
	}
	slog.Warn("speakers: refused", "err", err)
	return ErrUnreachable
}

// commandError maps a failed command: a speaker saying no is a conflict, silence is a 503.
func commandError(err error) error {
	var f *faultError
	switch {
	case errors.As(err, &f):
		return ErrRefused
	case errors.Is(err, ErrRefused), errors.Is(err, ErrNotOurs), errors.Is(err, ErrNothingToPlay):
		return err
	}
	return unreachable(err)
}
