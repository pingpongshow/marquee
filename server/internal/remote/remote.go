// Package remote is remote control (USER-14): an app that can be controlled long-polls its
// inbox for commands (which is also how it shows up as a player), reports what it's playing,
// and other apps of the same person list those players and send them commands.
//
// Everything is in memory, like watch together: a restart drops the players, which come back
// on their next poll.
package remote

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"
)

var (
	ErrNotFound = errors.New("player not found")
	ErrFull     = errors.New("too many players")
)

const (
	// Online is how long a device stays listed after its last inbox poll ended.
	Online = 40 * time.Second
	// MaxQueue is how many unacknowledged commands a device keeps (the oldest go first).
	MaxQueue = 20
	// MaxDevices bounds memory; offline devices are dropped first.
	MaxDevices = 2000
	// commandTTL drops commands a device never picked up, so a play sent to an app that was
	// closed doesn't start when it's opened later.
	commandTTL = 60 * time.Second
)

// Info identifies a device and its owner.
type Info struct {
	DeviceID, UserID         int64
	Name, Platform, UserName string
}

// Player is what controllers see.
type Player struct {
	Info
	Capabilities []string
	State        any // the app's last reported state; nil until it reports one
	Version      int64
}

// Command is a queued command; Body is opaque to the hub.
type Command struct {
	Seq  int64
	Body any
	at   time.Time
}

type device struct {
	Player
	seen    time.Time // when the last poll ended (or began, while it's running)
	polling int       // inbox polls in flight
	seq     int64     // the last command number handed out
	queue   []Command
	wake    chan struct{} // closed when a command arrives
	changed chan struct{} // closed when the state changes or the device goes away
}

// Hub holds the players in memory.
type Hub struct {
	mu      sync.Mutex
	devices map[int64]*device
	once    sync.Once
	now     func() time.Time
}

func (h *Hub) init() {
	h.once.Do(func() {
		h.devices = map[int64]*device{}
		if h.now == nil {
			h.now = time.Now
		}
	})
}

// Run drops devices that went offline, until ctx ends.
func (h *Hub) Run(ctx context.Context) {
	h.init()
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			h.reap()
		}
	}
}

func (h *Hub) online(d *device) bool { return d.polling > 0 || h.now().Sub(d.seen) <= Online }

func (h *Hub) reap() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, d := range h.devices {
		if !h.online(d) {
			h.drop(id, d)
		}
	}
}

func (h *Hub) drop(id int64, d *device) {
	delete(h.devices, id)
	close(d.changed)
	close(d.wake)
}

func (d *device) bump() {
	d.Version++
	close(d.changed)
	d.changed = make(chan struct{})
}

// pending returns the commands past cursor that haven't expired.
func (h *Hub) pending(d *device, cursor int64) []Command {
	cutoff := h.now().Add(-commandTTL)
	keep := d.queue[:0]
	var out []Command
	for _, c := range d.queue {
		if c.Seq <= cursor || c.at.Before(cutoff) {
			continue // acknowledged or stale
		}
		keep = append(keep, c)
		out = append(out, c)
	}
	d.queue = keep
	return out
}

// Inbox registers a device as a player (or refreshes it) and returns its commands after
// cursor, waiting up to timeout for one to arrive. The returned cursor acknowledges them on
// the next call. state, when not nil, is stored as the device's state.
func (h *Hub) Inbox(ctx context.Context, info Info, caps []string, state any, cursor int64, timeout time.Duration) ([]Command, int64, error) {
	h.init()
	h.mu.Lock()
	d := h.devices[info.DeviceID]
	if d != nil && d.Info.UserID != info.UserID {
		// The device id now belongs to someone else (a signed-out or deleted user's id
		// reused): nothing of the old player carries over.
		h.drop(info.DeviceID, d)
		d = nil
	}
	if d == nil {
		if len(h.devices) >= MaxDevices {
			for id, o := range h.devices {
				if !h.online(o) {
					h.drop(id, o)
				}
			}
			if len(h.devices) >= MaxDevices {
				h.mu.Unlock()
				return nil, cursor, ErrFull
			}
		}
		d = &device{Player: Player{Version: 1}, wake: make(chan struct{}), changed: make(chan struct{})}
		h.devices[info.DeviceID] = d
	}
	d.Info, d.Capabilities = info, caps
	if state != nil {
		d.State = state
		d.bump()
	}
	// A cursor ahead of ours means the server restarted since the app last polled; carry on
	// numbering from it so new commands aren't mistaken for acknowledged ones.
	if cursor > d.seq {
		d.seq = cursor
	}
	d.seen = h.now()
	if cmds := h.pending(d, cursor); len(cmds) > 0 || timeout <= 0 {
		h.mu.Unlock()
		return cmds, last(cmds, cursor), nil
	}
	d.polling++
	wake := d.wake
	h.mu.Unlock()

	t := time.NewTimer(timeout)
	defer t.Stop()
	select {
	case <-wake:
	case <-t.C:
	case <-ctx.Done():
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	d.polling--
	d.seen = h.now()
	if h.devices[info.DeviceID] != d {
		return nil, cursor, nil // dropped meanwhile
	}
	cmds := h.pending(d, cursor)
	return cmds, last(cmds, cursor), nil
}

func last(cmds []Command, cursor int64) int64 {
	if len(cmds) == 0 {
		return cursor
	}
	return cmds[len(cmds)-1].Seq
}

// SetState stores what a device is playing and wakes anyone waiting for a change. Devices
// that haven't polled their inbox yet aren't players, so their state is ignored.
func (h *Hub) SetState(deviceID int64, state any) bool {
	h.init()
	h.mu.Lock()
	defer h.mu.Unlock()
	d := h.devices[deviceID]
	if d == nil {
		return false
	}
	d.State = state
	d.bump()
	return true
}

// Players returns the online players, oldest device first.
func (h *Hub) Players() []Player {
	h.init()
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]Player, 0, len(h.devices))
	for _, d := range h.devices {
		if h.online(d) {
			out = append(out, d.Player)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].DeviceID < out[j].DeviceID })
	return out
}

// Player returns one online player.
func (h *Hub) Player(deviceID int64) (Player, error) {
	h.init()
	h.mu.Lock()
	defer h.mu.Unlock()
	d := h.devices[deviceID]
	if d == nil || !h.online(d) {
		return Player{}, ErrNotFound
	}
	return d.Player, nil
}

// Wait returns the player once its version is past since, or after timeout.
func (h *Hub) Wait(ctx context.Context, deviceID, since int64, timeout time.Duration) (Player, error) {
	h.init()
	h.mu.Lock()
	d := h.devices[deviceID]
	if d == nil || !h.online(d) {
		h.mu.Unlock()
		return Player{}, ErrNotFound
	}
	if d.Version > since || timeout <= 0 {
		p := d.Player
		h.mu.Unlock()
		return p, nil
	}
	ch := d.changed
	h.mu.Unlock()
	t := time.NewTimer(timeout)
	defer t.Stop()
	select {
	case <-ch:
	case <-t.C:
	case <-ctx.Done():
	}
	return h.Player(deviceID)
}

// Send queues a command for an online player and wakes its inbox.
func (h *Hub) Send(deviceID int64, body any) error {
	h.init()
	h.mu.Lock()
	defer h.mu.Unlock()
	d := h.devices[deviceID]
	if d == nil || !h.online(d) {
		return ErrNotFound
	}
	d.seq++
	d.queue = append(d.queue, Command{Seq: d.seq, Body: body, at: h.now()})
	if n := len(d.queue); n > MaxQueue {
		d.queue = append([]Command(nil), d.queue[n-MaxQueue:]...)
	}
	close(d.wake)
	d.wake = make(chan struct{})
	return nil
}

// Known returns what the hub knows about a device (ok=false if it isn't a player).
// DropUser forgets a deleted user's players at once.
func (h *Hub) DropUser(userID int64) {
	h.init()
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, d := range h.devices {
		if d.Info.UserID == userID {
			h.drop(id, d)
		}
	}
}

func (h *Hub) Known(deviceID int64) (Info, bool) {
	h.init()
	h.mu.Lock()
	defer h.mu.Unlock()
	if d := h.devices[deviceID]; d != nil {
		return d.Info, true
	}
	return Info{}, false
}
