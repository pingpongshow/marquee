package remote

import (
	"context"
	"sync"
	"testing"
	"time"
)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *clock) now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func newHub() (*Hub, *clock) {
	c := &clock{t: time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)}
	return &Hub{now: c.now}, c
}

var tv = Info{DeviceID: 7, UserID: 1, Name: "Living room TV", Platform: "tvos", UserName: "Ann"}

func TestPresenceAndExpiry(t *testing.T) {
	h, c := newHub()
	if _, err := h.Player(7); err != ErrNotFound {
		t.Fatalf("unknown device: %v", err)
	}
	if h.SetState(7, "x") {
		t.Error("state from a device that never polled was kept")
	}
	h.Inbox(context.Background(), tv, []string{"video"}, nil, 0, 0)
	p, err := h.Player(7)
	if err != nil || p.Name != "Living room TV" || p.Version != 1 || len(p.Capabilities) != 1 {
		t.Fatalf("after first poll: %+v %v", p, err)
	}
	// Still listed 39 s after the poll, gone at 41 s, and dropped by the reaper.
	c.add(Online - time.Second)
	if len(h.Players()) != 1 {
		t.Error("dropped too early")
	}
	c.add(2 * time.Second)
	if len(h.Players()) != 0 {
		t.Error("still online after 41 s")
	}
	if err := h.Send(7, "pause"); err != ErrNotFound {
		t.Errorf("send to offline device: %v", err)
	}
	h.reap()
	if _, ok := h.Known(7); ok {
		t.Error("offline device not reaped")
	}
}

func TestOnlineWhilePolling(t *testing.T) {
	h, c := newHub()
	done := make(chan struct{})
	go func() {
		h.Inbox(context.Background(), tv, nil, nil, 0, 2*time.Second)
		close(done)
	}()
	time.Sleep(50 * time.Millisecond)
	c.add(time.Hour) // a poll in flight counts as present however old its start is
	h.reap()
	if _, err := h.Player(7); err != nil {
		t.Errorf("mid-poll device not online: %v", err)
	}
	<-done
}

func TestCommandWakesInboxAndCursor(t *testing.T) {
	h, _ := newHub()
	ctx := context.Background()
	h.Inbox(ctx, tv, nil, nil, 0, 0)

	type res struct {
		cmds   []Command
		cursor int64
	}
	got := make(chan res)
	go func() {
		cmds, cur, _ := h.Inbox(ctx, tv, nil, nil, 0, 5*time.Second)
		got <- res{cmds, cur}
	}()
	time.Sleep(50 * time.Millisecond)
	if err := h.Send(7, "pause"); err != nil {
		t.Fatal(err)
	}
	var r res
	select {
	case r = <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("inbox not woken by a command")
	}
	if len(r.cmds) != 1 || r.cmds[0].Body != "pause" || r.cursor != 1 {
		t.Fatalf("delivered %+v cursor %d", r.cmds, r.cursor)
	}
	// Not acknowledged yet: polling with the old cursor repeats it.
	if cmds, _, _ := h.Inbox(ctx, tv, nil, nil, 0, 0); len(cmds) != 1 {
		t.Errorf("unacknowledged command lost: %+v", cmds)
	}
	// The cursor acknowledges it.
	h.Send(7, "resume")
	cmds, cur, _ := h.Inbox(ctx, tv, nil, nil, r.cursor, 0)
	if len(cmds) != 1 || cmds[0].Body != "resume" || cur != 2 {
		t.Errorf("after ack: %+v %d", cmds, cur)
	}
	// An empty poll keeps the cursor.
	if cmds, cur2, _ := h.Inbox(ctx, tv, nil, nil, cur, 0); len(cmds) != 0 || cur2 != cur {
		t.Errorf("empty poll: %+v %d", cmds, cur2)
	}
}

func TestQueueBoundAndTTL(t *testing.T) {
	h, c := newHub()
	ctx := context.Background()
	h.Inbox(ctx, tv, nil, nil, 0, 0)
	for i := 0; i < MaxQueue+5; i++ {
		h.Send(7, i)
	}
	cmds, cur, _ := h.Inbox(ctx, tv, nil, nil, 0, 0)
	if len(cmds) != MaxQueue || cmds[0].Body != 5 || cur != int64(MaxQueue+5) {
		t.Errorf("queue: %d commands, first %v, cursor %d", len(cmds), cmds[0].Body, cur)
	}
	// Commands nobody picked up within a minute are dropped.
	h.Send(7, "late")
	c.add(commandTTL + time.Second)
	h.Inbox(ctx, tv, nil, nil, cur, 0) // keeps it online
	if cmds, _, _ := h.Inbox(ctx, tv, nil, nil, cur, 0); len(cmds) != 0 {
		t.Errorf("stale command delivered: %+v", cmds)
	}
}

func TestCursorAfterRestart(t *testing.T) {
	// The app kept cursor 57 from before a server restart; new commands must still arrive.
	h, _ := newHub()
	ctx := context.Background()
	h.Inbox(ctx, tv, nil, nil, 57, 0)
	h.Send(7, "stop")
	cmds, cur, _ := h.Inbox(ctx, tv, nil, nil, 57, 0)
	if len(cmds) != 1 || cur != 58 {
		t.Errorf("after restart: %+v %d", cmds, cur)
	}
}

func TestStateLongPoll(t *testing.T) {
	h, _ := newHub()
	ctx := context.Background()
	h.Inbox(ctx, tv, nil, "idle", 0, 0)
	p, _ := h.Player(7)
	if p.State != "idle" || p.Version != 2 {
		t.Fatalf("state with the poll: %+v", p)
	}
	// Already past since: returns at once.
	if p2, _ := h.Wait(ctx, 7, 0, time.Minute); p2.Version != 2 {
		t.Errorf("wait past version: %+v", p2)
	}
	done := make(chan Player)
	go func() {
		p, _ := h.Wait(ctx, 7, 2, 5*time.Second)
		done <- p
	}()
	time.Sleep(50 * time.Millisecond)
	h.SetState(7, "playing")
	select {
	case p := <-done:
		if p.State != "playing" || p.Version != 3 {
			t.Errorf("woken with %+v", p)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("state wait not woken")
	}
	// A timeout returns the unchanged state.
	if p, err := h.Wait(ctx, 7, 3, 20*time.Millisecond); err != nil || p.Version != 3 {
		t.Errorf("timeout: %+v %v", p, err)
	}
}

func TestMaxDevices(t *testing.T) {
	h, c := newHub()
	ctx := context.Background()
	for i := int64(1); i <= MaxDevices; i++ {
		h.Inbox(ctx, Info{DeviceID: i}, nil, nil, 0, 0)
	}
	if _, _, err := h.Inbox(ctx, Info{DeviceID: MaxDevices + 1}, nil, nil, 0, 0); err != ErrFull {
		t.Errorf("over the limit: %v", err)
	}
	// Offline devices make room.
	c.add(Online + time.Second)
	if _, _, err := h.Inbox(ctx, Info{DeviceID: MaxDevices + 1}, nil, nil, 0, 0); err != nil {
		t.Errorf("after expiry: %v", err)
	}
	if n := len(h.Players()); n != 1 {
		t.Errorf("players: %d", n)
	}
}

func TestReusedDeviceID(t *testing.T) {
	h, _ := newHub()
	ctx := context.Background()
	h.Inbox(ctx, tv, []string{"video"}, "old state", 0, 0)
	h.Send(7, "pause")
	// The same device id, now another user's (ids are reused after a user is deleted).
	other := Info{DeviceID: 7, UserID: 2, Name: "Phone", Platform: "android", UserName: "Bo"}
	cmds, _, _ := h.Inbox(ctx, other, []string{"music"}, nil, 0, 0)
	p, err := h.Player(7)
	if err != nil || p.UserID != 2 || p.UserName != "Bo" || p.State != nil || len(cmds) != 0 {
		t.Fatalf("old player carried over: %+v %v %v", p, err, cmds)
	}
}

func TestDropUser(t *testing.T) {
	h, _ := newHub()
	h.Inbox(context.Background(), tv, nil, nil, 0, 0)
	h.DropUser(1)
	if len(h.Players()) != 0 {
		t.Error("deleted user's player still listed")
	}
}
