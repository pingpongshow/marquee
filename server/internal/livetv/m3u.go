// Package livetv is Live TV (LIVE-1..4): channels and a guide from M3U playlists with XMLTV
// guides (Dispatcharr is one such source), favourites, and live streams copied or transcoded
// to HLS for each device.
package livetv

import (
	"bufio"
	"io"
	"regexp"
	"strings"
)

// Entry is one channel in an M3U playlist.
type Entry struct {
	TvgID, Name, Logo, Number, Group, URL string
}

// Playlist is a parsed M3U: its channels and the guide URL from its header, if any.
type Playlist struct {
	EPG     string
	Entries []Entry
}

var attrRe = regexp.MustCompile(`([a-zA-Z0-9_-]+)="([^"]*)"`)

func attrs(s string) map[string]string {
	m := map[string]string{}
	for _, a := range attrRe.FindAllStringSubmatch(s, -1) {
		m[strings.ToLower(a[1])] = a[2]
	}
	return m
}

// ParseM3U reads an extended M3U playlist (#EXTINF lines with tvg-* attributes).
func ParseM3U(r io.Reader) (Playlist, error) {
	var p Playlist
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	var cur *Entry
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case line == "":
		case strings.HasPrefix(line, "#EXTM3U"):
			a := attrs(line)
			p.EPG = a["url-tvg"]
			if p.EPG == "" {
				p.EPG = a["x-tvg-url"]
			}
			if i := strings.IndexByte(p.EPG, ','); i > 0 {
				p.EPG = p.EPG[:i] // several guides: use the first
			}
		case strings.HasPrefix(line, "#EXTINF"):
			// #EXTINF:-1 tvg-id=".." tvg-name=".." ...,Display Name
			head, name := line, ""
			if i := firstUnquotedComma(line); i >= 0 {
				head, name = line[:i], strings.TrimSpace(line[i+1:])
			}
			a := attrs(head)
			e := Entry{TvgID: a["tvg-id"], Name: name, Logo: a["tvg-logo"], Number: a["tvg-chno"], Group: a["group-title"]}
			if e.Name == "" {
				e.Name = a["tvg-name"]
			}
			if e.Number == "" {
				e.Number = a["channel-number"]
			}
			cur = &e
		case strings.HasPrefix(line, "#EXTGRP:"):
			if cur != nil && cur.Group == "" {
				cur.Group = strings.TrimSpace(strings.TrimPrefix(line, "#EXTGRP:"))
			}
		case strings.HasPrefix(line, "#"):
		default:
			if cur != nil {
				cur.URL = line
				p.Entries = append(p.Entries, *cur)
				cur = nil
			}
		}
	}
	return p, sc.Err()
}

// firstUnquotedComma finds the comma that separates attributes from the display name.
func firstUnquotedComma(s string) int {
	in := false
	for i, c := range s {
		switch c {
		case '"':
			in = !in
		case ',':
			if !in {
				return i // the first unquoted comma ends the attributes
			}
		}
	}
	return -1
}
