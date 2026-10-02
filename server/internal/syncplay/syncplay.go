// Package syncplay is watch-together: a group shares one play/pause/seek state for a
// video, and each member's player follows it (correcting its own drift).
package syncplay

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sort"
	"sync"
	"time"
)

var (
	ErrNotFound  = errors.New("group not found")
	ErrNotMember = errors.New("join the group first")
	ErrAction    = errors.New("unknown action")
)

// Idle is how long a member stays without polling; empty groups end.
const Idle = 45 * time.Second

// Member is someone in a group.
type Member struct {
	UserID    int64
	Name      string
	Avatar    *string
	Buffering bool
	seen      time.Time
	joined    time.Time
}

// State is what clients see.
type State struct {
	ID, Title          string
	ItemID, HostID     int64
	Members            []Member
	Version            int64
	Playing            bool
	PositionMS         int64
	At                 time.Time
	LastAction, LastBy string
}

type group struct {
	State
	members  map[int64]*Member
	wantPlay bool // resume once nobody is buffering
	changed  chan struct{}
}

// Service holds the groups in memory.
type Service struct {
	mu     sync.Mutex
	groups map[string]*group
	once   sync.Once
	now    func() time.Time
}

func (s *Service) init() {
	s.once.Do(func() {
		s.groups = map[string]*group{}
		if s.now == nil {
			s.now = time.Now
		}
	})
}

// Run removes members who stopped polling, and empty groups, until ctx ends.
func (s *Service) Run(ctx context.Context) {
	s.init()
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.reap()
		}
	}
}

func (s *Service) reap() {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for id, g := range s.groups {
		removed := false
		for uid, m := range g.members {
			if now.Sub(m.seen) > Idle {
				delete(g.members, uid)
				removed = true
			}
		}
		if len(g.members) == 0 {
			delete(s.groups, id)
			close(g.changed)
			continue
		}
		if removed {
			s.settle(g)
			s.bump(g, "left", "")
		}
	}
}

func newID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// position is where the group is now.
func (s *Service) position(g *group) int64 {
	if !g.Playing {
		return g.PositionMS
	}
	return g.PositionMS + s.now().Sub(g.At).Milliseconds()
}

func (s *Service) bump(g *group, action, by string) {
	g.Version++
	g.LastAction, g.LastBy = action, by
	close(g.changed)
	g.changed = make(chan struct{})
}

// settle starts playback again once nobody is loading.
func (s *Service) settle(g *group) {
	if !g.wantPlay || g.Playing {
		return
	}
	for _, m := range g.members {
		if m.Buffering {
			return
		}
	}
	g.Playing, g.At = true, s.now()
}

func (s *Service) snapshot(g *group) State {
	st := g.State
	st.Members = make([]Member, 0, len(g.members))
	for _, m := range g.members {
		st.Members = append(st.Members, *m)
	}
	sort.Slice(st.Members, func(i, j int) bool { return st.Members[i].joined.Before(st.Members[j].joined) })
	return st
}

// Create starts a group playing an item; the creator is its first member.
func (s *Service) Create(userID int64, name string, avatar *string, itemID int64, title string, positionMS int64) State {
	s.init()
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	g := &group{State: State{ID: newID(), Title: title, ItemID: itemID, HostID: userID, Version: 1, Playing: true, PositionMS: positionMS, At: now,
		LastAction: "start", LastBy: name}, members: map[int64]*Member{}, wantPlay: true, changed: make(chan struct{})}
	g.members[userID] = &Member{UserID: userID, Name: name, Avatar: avatar, seen: now, joined: now}
	s.groups[g.ID] = g
	return s.snapshot(g)
}

// List returns every group.
func (s *Service) List() []State {
	s.init()
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]State, 0, len(s.groups))
	for _, g := range s.groups {
		out = append(out, s.snapshot(g))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// ItemOf returns a group's item (for access checks before joining).
func (s *Service) ItemOf(id string) (int64, error) {
	s.init()
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.groups[id]
	if g == nil {
		return 0, ErrNotFound
	}
	return g.ItemID, nil
}

// Join adds a member; they start out loading, so the group waits for them.
func (s *Service) Join(id string, userID int64, name string, avatar *string) (State, error) {
	s.init()
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.groups[id]
	if g == nil {
		return State{}, ErrNotFound
	}
	now := s.now()
	if m := g.members[userID]; m != nil {
		m.seen = now
		return s.snapshot(g), nil
	}
	g.members[userID] = &Member{UserID: userID, Name: name, Avatar: avatar, seen: now, joined: now}
	s.bump(g, "joined", name)
	return s.snapshot(g), nil
}

func (s *Service) Leave(id string, userID int64) {
	s.init()
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.groups[id]
	if g == nil || g.members[userID] == nil {
		return
	}
	name := g.members[userID].Name
	delete(g.members, userID)
	if len(g.members) == 0 {
		delete(s.groups, id)
		close(g.changed)
		return
	}
	s.settle(g)
	s.bump(g, "left", name)
}

// Command applies a member's play, pause, seek, buffering or ready.
func (s *Service) Command(id string, userID int64, action string, positionMS int64) (State, error) {
	s.init()
	s.mu.Lock()
	defer s.mu.Unlock()
	g := s.groups[id]
	if g == nil {
		return State{}, ErrNotFound
	}
	m := g.members[userID]
	if m == nil {
		return State{}, ErrNotMember
	}
	now := s.now()
	m.seen = now
	switch action {
	case "play":
		g.wantPlay = true
		g.PositionMS, g.At, g.Playing = positionMS, now, false
		s.settle(g)
	case "pause":
		g.wantPlay, g.Playing, g.PositionMS, g.At = false, false, positionMS, now
	case "seek":
		g.PositionMS, g.At = positionMS, now
	case "buffering":
		if m.Buffering {
			return s.snapshot(g), nil
		}
		m.Buffering = true
		if g.Playing {
			g.PositionMS, g.At, g.Playing = s.position(g), now, false
		}
	case "ready":
		if !m.Buffering {
			return s.snapshot(g), nil
		}
		m.Buffering = false
		s.settle(g)
	default:
		return State{}, ErrAction
	}
	s.bump(g, action, m.Name)
	return s.snapshot(g), nil
}

// Wait returns the group's state once its version is past since, or after timeout. It also
// marks the caller as present.
func (s *Service) Wait(ctx context.Context, id string, userID int64, since int64, timeout time.Duration) (State, error) {
	s.init()
	s.mu.Lock()
	g := s.groups[id]
	if g == nil {
		s.mu.Unlock()
		return State{}, ErrNotFound
	}
	if m := g.members[userID]; m != nil {
		m.seen = s.now()
	}
	if g.Version > since {
		st := s.snapshot(g)
		s.mu.Unlock()
		return st, nil
	}
	ch := g.changed
	s.mu.Unlock()
	select {
	case <-ch:
	case <-time.After(timeout):
	case <-ctx.Done():
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	g = s.groups[id]
	if g == nil {
		return State{}, ErrNotFound
	}
	return s.snapshot(g), nil
}
