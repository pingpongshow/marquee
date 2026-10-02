package livetv

import (
	"encoding/xml"
	"io"
	"strings"
	"time"
)

// Programme is one guide entry.
type Programme struct {
	Channel     string
	Start, Stop time.Time
	Title       string
	Subtitle    string
	Description string
	Category    string
	Episode     string
	Image       string
}

// GuideChannel is a channel as the guide names it.
type GuideChannel struct {
	ID, Name, Icon string
}

// xmltvTime parses "20261002124500 +0000" (the offset is optional).
func xmltvTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	for _, layout := range []string{"20060102150405 -0700", "20060102150405"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// ParseXMLTV streams an XMLTV document, calling fn for each programme ending after from and
// starting before until. It returns the guide's channels.
func ParseXMLTV(r io.Reader, from, until time.Time, fn func(Programme)) ([]GuideChannel, error) {
	dec := xml.NewDecoder(r)
	dec.Strict = false
	dec.CharsetReader = func(_ string, in io.Reader) (io.Reader, error) { return in, nil }
	var chans []GuideChannel
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return chans, nil
		}
		if err != nil {
			return chans, err
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		switch se.Name.Local {
		case "channel":
			var c struct {
				ID    string   `xml:"id,attr"`
				Names []string `xml:"display-name"`
				Icon  struct {
					Src string `xml:"src,attr"`
				} `xml:"icon"`
			}
			if dec.DecodeElement(&c, &se) == nil {
				g := GuideChannel{ID: c.ID, Icon: c.Icon.Src}
				if len(c.Names) > 0 {
					g.Name = strings.TrimSpace(c.Names[0])
				}
				chans = append(chans, g)
			}
		case "programme":
			var p struct {
				Start    string   `xml:"start,attr"`
				Stop     string   `xml:"stop,attr"`
				Channel  string   `xml:"channel,attr"`
				Title    []string `xml:"title"`
				Sub      string   `xml:"sub-title"`
				Desc     string   `xml:"desc"`
				Category []string `xml:"category"`
				Episodes []struct {
					System string `xml:"system,attr"`
					Value  string `xml:",chardata"`
				} `xml:"episode-num"`
				Icon struct {
					Src string `xml:"src,attr"`
				} `xml:"icon"`
			}
			if dec.DecodeElement(&p, &se) != nil {
				continue
			}
			start, ok1 := xmltvTime(p.Start)
			stop, ok2 := xmltvTime(p.Stop)
			if !ok1 {
				continue
			}
			if !ok2 || !stop.After(start) {
				stop = start.Add(30 * time.Minute)
			}
			if !stop.After(from) || !start.Before(until) {
				continue
			}
			prog := Programme{Channel: p.Channel, Start: start.UTC(), Stop: stop.UTC(), Subtitle: strings.TrimSpace(p.Sub),
				Description: strings.TrimSpace(p.Desc), Image: p.Icon.Src}
			if len(p.Title) > 0 {
				prog.Title = strings.TrimSpace(p.Title[0])
			}
			if len(p.Category) > 0 {
				prog.Category = strings.TrimSpace(p.Category[0])
			}
			prog.Episode = episode(p.Episodes)
			fn(prog)
		}
	}
}

// episode turns XMLTV episode numbers into "S2 E5", preferring the xmltv_ns system
// (zero-based "1.4.0/1") and falling back to the on-screen form.
func episode(eps []struct {
	System string `xml:"system,attr"`
	Value  string `xml:",chardata"`
}) string {
	for _, e := range eps {
		if e.System != "xmltv_ns" {
			continue
		}
		parts := strings.Split(strings.TrimSpace(e.Value), ".")
		if len(parts) < 2 {
			continue
		}
		num := func(s string) (int, bool) {
			s = strings.TrimSpace(strings.SplitN(s, "/", 2)[0])
			if s == "" {
				return 0, false
			}
			n := 0
			for _, c := range s {
				if c < '0' || c > '9' {
					return 0, false
				}
				n = n*10 + int(c-'0')
			}
			return n + 1, true
		}
		season, okS := num(parts[0])
		ep, okE := num(parts[1])
		switch {
		case okS && okE:
			return "S" + itoa(season) + " E" + itoa(ep)
		case okE:
			return "E" + itoa(ep)
		}
	}
	for _, e := range eps {
		if v := strings.TrimSpace(e.Value); v != "" && e.System != "dd_progid" {
			return v
		}
	}
	return ""
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
