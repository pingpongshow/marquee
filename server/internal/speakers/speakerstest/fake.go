// Package speakerstest is a fake Sonos player for tests: an HTTP server that answers the UPnP
// calls the speakers package makes, keeps the state a player would and records every call.
package speakerstest

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// Call is one SOAP call received.
type Call struct {
	Action string
	Args   map[string]string
}

// Player is the fake. Its fields are what it reports; change them through Set.
type Player struct {
	Srv  *httptest.Server
	Addr string // host:port
	UUID string

	mu sync.Mutex
	// What it's doing.
	State             string
	URI, Meta         string
	NextURI, NextMeta string
	Rel, Duration     string
	Volume            int
	// Topology is what GetZoneGroupState answers; defaults to one room, this player.
	Topology string
	// Faults answers an action with a UPnP error instead.
	Faults map[string]int
	Calls  []Call
	Model  string
	// OnCall runs before a call is answered, with the player unlocked, as a real one might fetch
	// a URL before answering the call that gave it.
	OnCall func(action string, args map[string]string)
}

// New starts a player, stopped at volume 30.
func New(t *testing.T, uuid string) *Player {
	t.Helper()
	p := &Player{UUID: uuid, State: "STOPPED", Rel: "0:00:00", Duration: "0:00:00", Volume: 30, Model: "Sonos Fake", Faults: map[string]int{}}
	p.Srv = httptest.NewServer(http.HandlerFunc(p.serve))
	p.Addr = p.Srv.Listener.Addr().String()
	t.Cleanup(p.Srv.Close)
	return p
}

// Set changes the player's state under its lock.
func (p *Player) Set(f func(p *Player)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	f(p)
}

// Snapshot is what a player is doing at a moment.
type Snapshot struct {
	State             string
	URI, Meta         string
	NextURI, NextMeta string
	Rel, Duration     string
	Volume            int
}

// Get reads the player's state under its lock.
func (p *Player) Get() Snapshot {
	p.mu.Lock()
	defer p.mu.Unlock()
	return Snapshot{State: p.State, URI: p.URI, Meta: p.Meta, NextURI: p.NextURI, NextMeta: p.NextMeta, Rel: p.Rel, Duration: p.Duration, Volume: p.Volume}
}

// Finish plays the song out: on to the next one it was told about, or stopped.
func (p *Player) Finish() {
	p.Set(func(p *Player) {
		if p.NextURI != "" {
			p.URI, p.Meta, p.NextURI, p.NextMeta = p.NextURI, p.NextMeta, "", ""
			p.Rel = "0:00:01"
			return
		}
		p.State, p.Rel = "STOPPED", "0:00:00"
	})
}

// Actions lists the actions received, for assertions; those named by skip are left out.
func (p *Player) Actions(skip ...string) []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []string
next:
	for _, c := range p.Calls {
		for _, s := range skip {
			if c.Action == s {
				continue next
			}
		}
		out = append(out, c.Action)
	}
	return out
}

// Last returns the last call of an action.
func (p *Player) Last(action string) (Call, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i := len(p.Calls) - 1; i >= 0; i-- {
		if p.Calls[i].Action == action {
			return p.Calls[i], true
		}
	}
	return Call{}, false
}

func esc(v string) string {
	var b strings.Builder
	xml.EscapeText(&b, []byte(v))
	return b.String()
}

// RoomTopology is one room with this player alone in its group.
func (p *Player) RoomTopology(name string) string {
	return `<ZoneGroupState><ZoneGroups><ZoneGroup Coordinator="` + p.UUID + `" ID="` + p.UUID + `:1"><ZoneGroupMember UUID="` + p.UUID +
		`" Location="http://` + p.Addr + `/xml/device_description.xml" ZoneName="` + esc(name) + `"/></ZoneGroup></ZoneGroups></ZoneGroupState>`
}

func (p *Player) serve(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet && r.URL.Path == "/xml/device_description.xml" {
		fmt.Fprintf(w, `<root xmlns="urn:schemas-upnp-org:device-1-0"><device><modelName>%s</modelName></device></root>`, esc(p.Model))
		return
	}
	_, action, _ := strings.Cut(strings.Trim(r.Header.Get("SOAPACTION"), `"`), "#")
	raw, _ := io.ReadAll(r.Body)
	args := soapArgs(raw)
	p.mu.Lock()
	onCall := p.OnCall
	p.mu.Unlock()
	if onCall != nil {
		onCall(action, args)
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Calls = append(p.Calls, Call{Action: action, Args: args})
	if code, ok := p.Faults[action]; ok {
		w.WriteHeader(http.StatusInternalServerError)
		fmt.Fprintf(w, `<s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><s:Fault><detail><UPnPError xmlns="urn:schemas-upnp-org:control-1-0"><errorCode>%d</errorCode></UPnPError></detail></s:Fault></s:Body></s:Envelope>`, code)
		return
	}
	out := ""
	switch action {
	case "GetTransportInfo":
		out = "<CurrentTransportState>" + p.State + "</CurrentTransportState>"
	case "GetPositionInfo":
		out = "<TrackDuration>" + p.Duration + "</TrackDuration><TrackMetaData>" + esc(p.Meta) + "</TrackMetaData><TrackURI>" + esc(p.URI) + "</TrackURI><RelTime>" + p.Rel + "</RelTime>"
	case "GetVolume":
		out = fmt.Sprintf("<CurrentVolume>%d</CurrentVolume>", p.Volume)
	case "SetVolume":
		fmt.Sscan(args["DesiredVolume"], &p.Volume)
	case "SetAVTransportURI":
		p.URI, p.Meta, p.NextURI, p.NextMeta, p.State, p.Rel = args["CurrentURI"], args["CurrentURIMetaData"], "", "", "STOPPED", "0:00:00"
	case "SetNextAVTransportURI":
		p.NextURI, p.NextMeta = args["NextURI"], args["NextURIMetaData"]
	case "Play":
		p.State = "PLAYING"
	case "Pause":
		p.State = "PAUSED_PLAYBACK"
	case "Stop":
		p.State, p.Rel = "STOPPED", "0:00:00"
	case "Seek":
		p.Rel = args["Target"]
	case "Next":
		p.URI, p.Meta, p.NextURI, p.NextMeta, p.Rel = p.NextURI, p.NextMeta, "", "", "0:00:00"
	case "GetZoneGroupState":
		t := p.Topology
		if t == "" {
			t = p.RoomTopology("Room")
		}
		out = "<ZoneGroupState>" + esc(t) + "</ZoneGroupState>"
	}
	fmt.Fprintf(w, `<?xml version="1.0"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/"><s:Body><u:%sResponse xmlns:u="x">%s</u:%sResponse></s:Body></s:Envelope>`, action, out, action)
}

// soapArgs reads the arguments of a SOAP request, unescaped, as a player would.
func soapArgs(raw []byte) map[string]string {
	out := map[string]string{}
	d := xml.NewDecoder(strings.NewReader(string(raw)))
	name, text := "", ""
	for {
		tok, err := d.Token()
		if err != nil {
			return out
		}
		switch t := tok.(type) {
		case xml.StartElement:
			name, text = t.Name.Local, ""
		case xml.CharData:
			text += string(t)
		case xml.EndElement:
			if name == t.Name.Local {
				out[name] = text
			}
			name = ""
		}
	}
}
