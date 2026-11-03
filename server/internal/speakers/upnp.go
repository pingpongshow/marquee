package speakers

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// The UPnP services a Sonos player offers on port 1400.
type service struct{ name, path string }

var (
	avTransport       = service{"AVTransport", "/MediaRenderer/AVTransport/Control"}
	renderingControl  = service{"RenderingControl", "/MediaRenderer/RenderingControl/Control"}
	zoneGroupTopology = service{"ZoneGroupTopology", "/ZoneGroupTopology/Control"}
)

const (
	playerPort = "1400"
	// faultNotPossible is UPnP's "transition not available": already paused, nothing to stop.
	faultNotPossible = 701
	// maxResponse bounds what's read from a player; a large system's topology is some 100 KB.
	maxResponse = 4 << 20
)

var dialer = &net.Dialer{Timeout: 2 * time.Second}

// withPort adds the players' port to an address that has none.
func withPort(host string) string {
	if _, _, err := net.SplitHostPort(host); err == nil {
		return host
	}
	return net.JoinHostPort(host, playerPort)
}

// faultError is a player refusing a command with a UPnP error.
type faultError struct {
	Action string
	Code   int
}

func (e *faultError) Error() string { return fmt.Sprintf("%s: UPnP error %d", e.Action, e.Code) }

// ignoreFault treats "transition not available" as done.
func ignoreFault(err error) error {
	var f *faultError
	if errors.As(err, &f) && f.Code == faultNotPossible {
		return nil
	}
	return err
}

// esc escapes a value for XML text or an attribute, quotes included.
func esc(v string) string {
	var b strings.Builder
	xml.EscapeText(&b, []byte(v))
	return b.String()
}

// allowed reports whether a player may be contacted at all (see Service.Hosts).
func (s *Service) allowed(addr string) bool { return s.allow == nil || s.allow[addr] }

// soap calls an action; args are name and value pairs, in the order the action defines them.
// It returns the response's values by name. A player that isn't there gives ErrUnreachable and
// one that refuses gives a *faultError.
func (s *Service) soap(ctx context.Context, addr string, svc service, action string, args ...string) (map[string]string, error) {
	s.init()
	if !s.allowed(addr) {
		return nil, fmt.Errorf("%w: %s isn't in the configured hosts", ErrUnreachable, addr)
	}
	urn := "urn:schemas-upnp-org:service:" + svc.name + ":1"
	var body strings.Builder
	body.WriteString(`<?xml version="1.0" encoding="utf-8"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body><u:` +
		action + ` xmlns:u="` + urn + `">`)
	for i := 0; i+1 < len(args); i += 2 {
		body.WriteString("<" + args[i] + ">" + esc(args[i+1]) + "</" + args[i] + ">")
	}
	body.WriteString(`</u:` + action + `></s:Body></s:Envelope>`)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://"+addr+svc.path, strings.NewReader(body.String()))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnreachable, err)
	}
	req.Header.Set("Content-Type", `text/xml; charset="utf-8"`)
	req.Header.Set("SOAPACTION", `"`+urn+`#`+action+`"`)
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%w: %s %v", ErrUnreachable, action, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse))
	if err != nil {
		return nil, fmt.Errorf("%w: %s %v", ErrUnreachable, action, err)
	}
	values := leaves(raw)
	if resp.StatusCode != http.StatusOK {
		if code, err := strconv.Atoi(values["errorCode"]); err == nil {
			return nil, &faultError{Action: action, Code: code}
		}
		return nil, fmt.Errorf("%w: %s answered %d", ErrUnreachable, action, resp.StatusCode)
	}
	return values, nil
}

// leaves reads every element's text by its local name, which is all a SOAP response here has.
func leaves(raw []byte) map[string]string {
	out := map[string]string{}
	d := xml.NewDecoder(bytes.NewReader(raw))
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

const instance = "0"

func (s *Service) transportInfo(ctx context.Context, addr string) (string, error) {
	v, err := s.soap(ctx, addr, avTransport, "GetTransportInfo", "InstanceID", instance)
	if err != nil {
		return "", err
	}
	return v["CurrentTransportState"], nil
}

// position is GetPositionInfo: the track the speaker is on and how far in.
type position struct {
	URI               string
	Metadata          string
	RelMS, DurationMS int64
}

func (s *Service) positionInfo(ctx context.Context, addr string) (position, error) {
	v, err := s.soap(ctx, addr, avTransport, "GetPositionInfo", "InstanceID", instance)
	if err != nil {
		return position{}, err
	}
	return position{URI: v["TrackURI"], Metadata: v["TrackMetaData"], RelMS: parseClock(v["RelTime"]), DurationMS: parseClock(v["TrackDuration"])}, nil
}

func (s *Service) setURI(ctx context.Context, addr, uri, meta string) error {
	_, err := s.soap(ctx, addr, avTransport, "SetAVTransportURI", "InstanceID", instance, "CurrentURI", uri, "CurrentURIMetaData", meta)
	return err
}

func (s *Service) setNextURI(ctx context.Context, addr, uri, meta string) error {
	_, err := s.soap(ctx, addr, avTransport, "SetNextAVTransportURI", "InstanceID", instance, "NextURI", uri, "NextURIMetaData", meta)
	return err
}

func (s *Service) play(ctx context.Context, addr string) error {
	_, err := s.soap(ctx, addr, avTransport, "Play", "InstanceID", instance, "Speed", "1")
	return err
}

func (s *Service) pause(ctx context.Context, addr string) error {
	_, err := s.soap(ctx, addr, avTransport, "Pause", "InstanceID", instance)
	return err
}

func (s *Service) stop(ctx context.Context, addr string) error {
	_, err := s.soap(ctx, addr, avTransport, "Stop", "InstanceID", instance)
	return err
}

func (s *Service) next(ctx context.Context, addr string) error {
	_, err := s.soap(ctx, addr, avTransport, "Next", "InstanceID", instance)
	return err
}

func (s *Service) seekTo(ctx context.Context, addr string, ms int64) error {
	_, err := s.soap(ctx, addr, avTransport, "Seek", "InstanceID", instance, "Unit", "REL_TIME", "Target", clock(ms))
	return err
}

func (s *Service) volume(ctx context.Context, addr string) (int, error) {
	v, err := s.soap(ctx, addr, renderingControl, "GetVolume", "InstanceID", instance, "Channel", "Master")
	if err != nil {
		return 0, err
	}
	n, err := strconv.Atoi(v["CurrentVolume"])
	if err != nil {
		return 0, fmt.Errorf("%w: volume %q", ErrUnreachable, v["CurrentVolume"])
	}
	return n, nil
}

func (s *Service) setVolume(ctx context.Context, addr string, volume int) error {
	_, err := s.soap(ctx, addr, renderingControl, "SetVolume", "InstanceID", instance, "Channel", "Master", "DesiredVolume", strconv.Itoa(volume))
	return err
}

// clock formats a position as UPnP wants it, H:MM:SS.
func clock(ms int64) string {
	sec := max(ms, 0) / 1000
	return fmt.Sprintf("%d:%02d:%02d", sec/3600, sec/60%60, sec%60)
}

// parseClock reads H:MM:SS (with any fraction); anything else, like NOT_IMPLEMENTED, is 0.
func parseClock(v string) int64 {
	p := strings.Split(v, ":")
	if len(p) != 3 {
		return 0
	}
	h, err1 := strconv.ParseInt(p[0], 10, 64)
	m, err2 := strconv.ParseInt(p[1], 10, 64)
	sec, err3 := strconv.ParseFloat(p[2], 64)
	if err1 != nil || err2 != nil || err3 != nil {
		return 0
	}
	return (h*3600+m*60)*1000 + int64(sec*1000)
}

// apiState is a transport state as the API names it.
func apiState(state string) string {
	switch state {
	case "PLAYING":
		return "playing"
	case "PAUSED_PLAYBACK":
		return "paused"
	case "TRANSITIONING":
		return "loading"
	}
	return "stopped"
}

// didl describes a track, so the speaker and the Sonos app show what's playing. Every value is
// escaped here, and the whole document again as a SOAP value by soap.
func didl(t Track, uri, mime, art string) string {
	var b strings.Builder
	b.WriteString(`<DIDL-Lite xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:upnp="urn:schemas-upnp-org:metadata-1-0/upnp/" xmlns:r="urn:schemas-rinconnetworks-com:metadata-1-0/" xmlns="urn:schemas-upnp-org:metadata-1-0/DIDL-Lite/">`)
	b.WriteString(`<item id="-1" parentID="-1" restricted="true"><res protocolInfo="http-get:*:` + esc(mime) + `:*"`)
	if t.DurationMS > 0 {
		b.WriteString(` duration="` + clock(t.DurationMS) + `"`)
	}
	b.WriteString(`>` + esc(uri) + `</res>`)
	if art != "" {
		b.WriteString(`<upnp:albumArtURI>` + esc(art) + `</upnp:albumArtURI>`)
	}
	b.WriteString(`<dc:title>` + esc(t.Title) + `</dc:title><upnp:class>object.item.audioItem.musicTrack</upnp:class>`)
	if t.Artist != "" {
		b.WriteString(`<dc:creator>` + esc(t.Artist) + `</dc:creator>`)
	}
	if t.Album != "" {
		b.WriteString(`<upnp:album>` + esc(t.Album) + `</upnp:album>`)
	}
	b.WriteString(`</item></DIDL-Lite>`)
	return b.String()
}
