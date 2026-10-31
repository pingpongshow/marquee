package soundprint

import (
	"regexp"
	"strings"
)

// Aside marks tracks that interrupt a station or mix: a skit, an intro or interlude, a live
// recording, a remix. They're left out unless the seed is one, or a prompt asks for them
// (MUSIC-17).
type Aside uint8

const (
	AsideInterlude Aside = 1 << iota // short, or titled as an intro, interlude, skit…
	AsideLive                        // a live recording (the title or the album says so)
	AsideRemix                       // a remix or club version
)

// interludeMS is how short a track has to be to count as an interlude by length alone.
const interludeMS = 75_000

var (
	interludeTitle = regexp.MustCompile(`(?i)\b(interlude|intro|outro|skit|prelude|segue|interval|reprise)\b`)
	liveTitle      = regexp.MustCompile(`(?i)[(\[]\s*live\b|\s[-–]\s*live\b|\blive (at|in|from|on)\b|\bunplugged\b`)
	liveAlbum      = regexp.MustCompile(`(?i)^live\b|\blive\s*$|[(\[]\s*live\b|\blive (at|in|from|on)\b|\bunplugged\b|\bin concert\b`)
	remixTitle     = regexp.MustCompile(`(?i)\b(remix|rmx|club mix|extended mix|dub mix|extended version|vip mix|bootleg)\b`)
)

// AsideOf classifies a track by its title, its album's title and its length.
func AsideOf(title, album string, durationMS int64) Aside {
	var a Aside
	if (durationMS > 0 && durationMS < interludeMS) || interludeTitle.MatchString(title) {
		a |= AsideInterlude
	}
	if liveTitle.MatchString(title) || liveAlbum.MatchString(strings.TrimSpace(album)) {
		a |= AsideLive
	}
	if remixTitle.MatchString(title) {
		a |= AsideRemix
	}
	return a
}

// allowAside is the asides a station or mix from these seeds may play: the kinds the seeds
// are themselves (a live album's radio plays live tracks).
func allowAside(seeds []*Track) Aside {
	var a Aside
	for _, t := range seeds {
		a |= t.Aside
	}
	return a
}
