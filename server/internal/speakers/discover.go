package speakers

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/ipv4"
)

const searchTarget = "urn:schemas-upnp-org:device:ZonePlayer:1"

var (
	ssdpGroup   = &net.UDPAddr{IP: net.IPv4(239, 255, 255, 250), Port: 1900}
	ssdpMessage = []byte("M-SEARCH * HTTP/1.1\r\nHOST: 239.255.255.250:1900\r\nMAN: \"ssdp:discover\"\r\nMX: 1\r\nST: " + searchTarget + "\r\n\r\n")
)

// searchNetwork asks every network the server is on for Sonos players, and returns the
// addresses that answered. No multicast (a container off the host network, say) just finds none.
func searchNetwork(ctx context.Context) []string {
	ifaces, err := net.Interfaces()
	if err != nil {
		slog.Debug("speakers: interfaces", "err", err)
		return nil
	}
	var mu sync.Mutex
	var found []string
	var wg sync.WaitGroup
	for _, ifi := range ifaces {
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagMulticast == 0 || ifi.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := ifi.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			n, ok := a.(*net.IPNet)
			if !ok || n.IP.To4() == nil {
				continue
			}
			pc, err := net.ListenPacket("udp4", net.JoinHostPort(n.IP.String(), "0"))
			if err != nil {
				continue
			}
			// Without this the search leaves only by the default route.
			if err := ipv4.NewPacketConn(pc).SetMulticastInterface(&ifi); err != nil {
				pc.Close()
				continue
			}
			wg.Go(func() {
				defer pc.Close()
				hosts := searchOn(ctx, pc, ssdpGroup, searchWait)
				mu.Lock()
				found = append(found, hosts...)
				mu.Unlock()
			})
		}
	}
	wg.Wait()
	slices.Sort(found)
	return slices.Compact(found)
}

// searchOn sends a search to dst and collects who answers as a player until wait is over.
// The address is the sender's, never a URL from the answer. That's only who to ask next: any
// device can answer, and what it then reports is checked in parseTopology and refresh.
func searchOn(ctx context.Context, pc net.PacketConn, dst net.Addr, wait time.Duration) []string {
	deadline := time.Now().Add(wait)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	pc.SetDeadline(deadline)
	for range 2 { // it's UDP
		if _, err := pc.WriteTo(ssdpMessage, dst); err != nil {
			slog.Debug("speakers: search", "err", err)
			return nil
		}
	}
	var out []string
	buf := make([]byte, 4096)
	for {
		n, from, err := pc.ReadFrom(buf)
		if err != nil {
			return out
		}
		udp, ok := from.(*net.UDPAddr)
		if ok && isPlayerAnswer(string(buf[:n])) {
			out = append(out, withPort(udp.IP.String()))
		}
	}
}

// isPlayerAnswer reports whether a search answer is from a player.
func isPlayerAnswer(msg string) bool {
	for line := range strings.Lines(msg) {
		if k, v, ok := strings.Cut(line, ":"); ok && strings.EqualFold(k, "ST") && strings.TrimSpace(v) == searchTarget {
			return true
		}
	}
	return false
}

// The topology a player reports: groups of rooms, with bonded speakers inside them.
type topologyXML struct {
	// S2 wraps the groups in ZoneGroupState; older software starts at ZoneGroups.
	XMLName xml.Name
	Nested  []groupXML `xml:"ZoneGroups>ZoneGroup"`
	Direct  []groupXML `xml:"ZoneGroup"`
}

type groupXML struct {
	Coordinator string      `xml:"Coordinator,attr"`
	Members     []memberXML `xml:"ZoneGroupMember"`
}

type memberXML struct {
	UUID         string      `xml:"UUID,attr"`
	Location     string      `xml:"Location,attr"`
	ZoneName     string      `xml:"ZoneName,attr"`
	Invisible    string      `xml:"Invisible,attr"`
	IsZoneBridge string      `xml:"IsZoneBridge,attr"`
	Satellites   []memberXML `xml:"Satellite"`
}

// parseTopology returns the rooms in a player's topology and every player's address. Only the
// visible members of a group are rooms: home theatre satellites, the second half of a stereo
// pair and Boosts aren't, and don't answer AVTransport usefully. A topology comes from whoever
// answered a search, so a player at an address accept refuses doesn't exist, and neither does a
// room led by one.
func parseTopology(raw string, accept func(addr string) bool) ([]Room, map[string]string, error) {
	var t topologyXML
	if err := xml.Unmarshal([]byte(raw), &t); err != nil {
		return nil, nil, fmt.Errorf("zone group state: %w", err)
	}
	addrs := map[string]string{}
	note := func(m memberXML) {
		if a := locationAddr(m.Location); a != "" && accept(a) {
			addrs[m.UUID] = a
		}
	}
	var rooms []Room
	for _, g := range append(t.Nested, t.Direct...) {
		for _, m := range g.Members {
			note(m)
			for _, sat := range m.Satellites {
				note(sat)
			}
		}
		for _, m := range g.Members {
			if m.Invisible == "1" || m.IsZoneBridge == "1" || addrs[m.UUID] == "" || addrs[g.Coordinator] == "" {
				continue
			}
			rooms = append(rooms, Room{ID: m.UUID, Name: m.ZoneName, Addr: addrs[m.UUID], Coordinator: g.Coordinator, CoordinatorAddr: addrs[g.Coordinator]})
		}
	}
	return rooms, addrs, nil
}

func locationAddr(location string) string {
	u, err := url.Parse(location)
	if err != nil || u.Scheme != "http" {
		return ""
	}
	return u.Host
}

// localPlayerAddr reports an address a player could really have: a private or link-local IP,
// on the players' port. Nothing else (the server's own loopback, an outside host, a name) is
// ever sent a request because a device on the network said so.
func localPlayerAddr(addr string) bool {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || port != playerPort {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && (ip.IsPrivate() || ip.IsLinkLocalUnicast())
}

// acceptable is who may be taken for a player: the configured hosts when there are any, which
// are the admin's word, else an address a player could have.
func (s *Service) acceptable(addr string) bool {
	if s.allow != nil {
		return s.allow[addr]
	}
	return localPlayerAddr(addr)
}

func (s *Service) readTopology(ctx context.Context, addr string) ([]Room, map[string]string, error) {
	v, err := s.soap(ctx, addr, zoneGroupTopology, "GetZoneGroupState")
	if err != nil {
		return nil, nil, err
	}
	return parseTopology(v["ZoneGroupState"], s.acceptable)
}

// modelOf reads a player's description for its model name.
func (s *Service) modelOf(ctx context.Context, addr string) (string, error) {
	if !s.allowed(addr) {
		return "", fmt.Errorf("%w: %s isn't in the configured hosts", ErrUnreachable, addr)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+addr+"/xml/device_description.xml", nil)
	if err != nil {
		return "", err
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("device description: %d", resp.StatusCode)
	}
	var d struct {
		Device struct {
			ModelName string `xml:"modelName"`
		} `xml:"device"`
	}
	if err := xml.NewDecoder(io.LimitReader(resp.Body, maxResponse)).Decode(&d); err != nil {
		return "", err
	}
	return d.Device.ModelName, nil
}

// seeds is who to ask for the topology: those who answered a search, then the players already
// known, in case the search got lost; only the configured hosts when there are any.
func (s *Service) seeds(ctx context.Context) []string {
	if s.allow != nil {
		out := make([]string, 0, len(s.Hosts))
		for _, h := range s.Hosts {
			out = append(out, withPort(h))
		}
		return out
	}
	out := slices.DeleteFunc(s.search(ctx), func(a string) bool { return !s.acceptable(a) })
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.rooms {
		if !slices.Contains(out, r.Addr) {
			out = append(out, r.Addr)
		}
	}
	return out
}

// refresh finds the rooms again. One player reports the whole system, so only a player it
// doesn't mention (another system on the same network) is asked as well.
//
// It's for everyone's benefit, so it doesn't end with the request that happened to start it. When
// nobody answers, the rooms stay as they were: a lost search or a blip mustn't make a room
// that's playing vanish. Rooms that are really gone drop out of List, which asks each of them.
func (s *Service) refresh(ctx context.Context) {
	s.init()
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), refreshTimeout)
	defer cancel()
	s.refreshMu.Lock()
	defer s.refreshMu.Unlock()
	s.mu.Lock()
	last := s.refreshed
	s.mu.Unlock()
	if time.Since(last) < time.Second {
		return // whoever had this lock just did it
	}
	var rooms []Room
	addrs := map[string]string{}
	known := map[string]bool{}
	answered := false
	for _, seed := range s.seeds(ctx) {
		if known[seed] {
			continue
		}
		rs, as, err := s.readTopology(ctx, seed)
		if err != nil {
			slog.Debug("speakers: topology", "player", seed, "err", err)
			continue
		}
		answered = true
		rooms = append(rooms, rs...)
		for id, a := range as {
			known[a] = true
			if _, taken := addrs[id]; !taken {
				addrs[id] = a
			}
		}
	}
	if !answered {
		s.mu.Lock()
		s.refreshed = time.Now()
		s.mu.Unlock()
		return
	}
	rooms = s.settleAddrs(ctx, rooms, addrs)
	for i := range rooms {
		rooms[i].Model = s.model(ctx, rooms[i])
	}
	slices.SortFunc(rooms, func(a, b Room) int { return strings.Compare(a.Name, b.Name) })
	s.mu.Lock()
	s.rooms, s.refreshed = rooms, time.Now()
	// Addresses are only ever updated: a session keeps polling through a search that found nobody.
	for id, a := range addrs {
		s.addrs[id] = a
	}
	s.mu.Unlock()
}

// settleAddrs stops a device taking over a known player's id, which would have it sent that
// room's session tokens: an entry moving a player to another address is ignored while the
// player still answers at the one it had. One that's gone has really moved (a new DHCP lease).
// A player reported twice keeps its first address, and is one room.
func (s *Service) settleAddrs(ctx context.Context, rooms []Room, addrs map[string]string) []Room {
	for id, a := range addrs {
		old, known := s.addrOf(id)
		if !known || old == a {
			continue
		}
		if _, err := s.modelOf(ctx, old); err == nil {
			slog.Warn("speakers: ignoring a player claiming another's id", "id", id, "at", a, "known at", old)
			addrs[id] = old
		}
	}
	seen := map[string]bool{}
	out := make([]Room, 0, len(rooms))
	for _, r := range rooms {
		if seen[r.ID] {
			continue
		}
		seen[r.ID] = true
		r.Addr, r.CoordinatorAddr = addrs[r.ID], addrs[r.Coordinator]
		out = append(out, r)
	}
	return out
}

// model finds a room's model, once: it doesn't change. Without it the room is still a room.
func (s *Service) model(ctx context.Context, r Room) string {
	s.mu.Lock()
	m, ok := s.models[r.ID]
	s.mu.Unlock()
	if ok {
		return m
	}
	m, err := s.modelOf(ctx, r.Addr)
	if err != nil || m == "" {
		slog.Debug("speakers: model", "room", r.Name, "err", err)
		return "Sonos"
	}
	s.mu.Lock()
	s.models[r.ID] = m
	s.mu.Unlock()
	return m
}

// Rooms returns the rooms, refreshing them first when they're stale.
func (s *Service) Rooms(ctx context.Context) []Room {
	s.init()
	s.mu.Lock()
	s.asked = time.Now()
	stale := time.Since(s.refreshed) > listFresh
	s.mu.Unlock()
	if stale {
		s.refresh(ctx)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.rooms)
}

// room finds one room, looking again if it isn't known (it may just have been plugged in).
func (s *Service) room(ctx context.Context, id string) (Room, error) {
	find := func(rooms []Room) (Room, bool) {
		i := slices.IndexFunc(rooms, func(r Room) bool { return r.ID == id })
		if i < 0 {
			return Room{}, false
		}
		return rooms[i], true
	}
	if r, ok := find(s.Rooms(ctx)); ok {
		return r, nil
	}
	s.mu.Lock()
	fresh := time.Since(s.refreshed) < 10*time.Second
	s.mu.Unlock()
	if !fresh {
		s.refresh(ctx)
		if r, ok := find(s.Rooms(ctx)); ok {
			return r, nil
		}
	}
	return Room{}, ErrNotFound
}

// addrOf is a player's address by its id.
func (s *Service) addrOf(id string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.addrs[id]
	return a, ok && a != ""
}

// localBase is the server's own base URL as seen from a player: a UDP "connection" sends
// nothing, but makes the system choose the local address on the route to it.
func (s *Service) localBase(addr string) (string, error) {
	c, err := net.Dial("udp4", addr)
	if err != nil {
		return "", errors.Join(ErrUnreachable, err)
	}
	defer c.Close()
	ip := c.LocalAddr().(*net.UDPAddr).IP.String()
	return "http://" + net.JoinHostPort(ip, fmt.Sprint(s.Port)), nil
}
