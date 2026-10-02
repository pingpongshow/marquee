package syncplay

import (
	"context"
	"testing"
	"time"
)

func TestGroupFlow(t *testing.T) {
	clock := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	s := &Service{now: func() time.Time { return clock }}
	g := s.Create(1, "Ann", nil, 42, "Movie", 10_000)
	if !g.Playing || g.Version != 1 || len(g.Members) != 1 {
		t.Fatalf("created: %+v", g)
	}
	// Bob joins and is loading: everyone waits for him.
	if _, err := s.Command(g.ID, 2, "pause", 0); err != ErrNotMember {
		t.Errorf("non-member: %v", err)
	}
	g, _ = s.Join(g.ID, 2, "Bob", nil)
	clock = clock.Add(5 * time.Second)
	g, _ = s.Command(g.ID, 2, "buffering", 0)
	if g.Playing || g.PositionMS != 15_000 {
		t.Errorf("buffering pauses at the group's position: %+v", g)
	}
	clock = clock.Add(2 * time.Second)
	g, _ = s.Command(g.ID, 2, "ready", 0)
	if !g.Playing || g.PositionMS != 15_000 || !g.At.Equal(clock) {
		t.Errorf("ready resumes where it stopped: %+v", g)
	}
	// Ann pauses; a seek keeps it paused; play resumes.
	g, _ = s.Command(g.ID, 1, "pause", 20_000)
	g, _ = s.Command(g.ID, 1, "seek", 60_000)
	if g.Playing || g.PositionMS != 60_000 || g.LastBy != "Ann" {
		t.Errorf("seek while paused: %+v", g)
	}
	g, _ = s.Command(g.ID, 2, "play", 60_000)
	if !g.Playing {
		t.Errorf("play: %+v", g)
	}
	// A long poll returns as soon as something changes.
	done := make(chan State)
	go func() {
		st, _ := s.Wait(context.Background(), g.ID, 2, g.Version, 5*time.Second)
		done <- st
	}()
	time.Sleep(50 * time.Millisecond)
	s.Command(g.ID, 1, "pause", 61_000)
	select {
	case st := <-done:
		if st.Playing || st.LastAction != "pause" {
			t.Errorf("woken with %+v", st)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("long poll not woken")
	}
	// Members who stop polling drop out; the last one out ends the group.
	clock = clock.Add(Idle + time.Second)
	s.Wait(context.Background(), g.ID, 1, 0, 0)
	s.reap()
	if st := s.List(); len(st) != 1 || len(st[0].Members) != 1 {
		t.Errorf("after Bob went quiet: %+v", st)
	}
	s.Leave(g.ID, 1)
	if len(s.List()) != 0 {
		t.Error("empty group remains")
	}
}
