// Package appearance holds what the web app looks like for everyone (USER-20): the admin's
// theme, the holiday themes that can replace it around their dates, and the image behind
// the page. The themes themselves live in the web app; this package only knows their ids.
package appearance

import "time"

// DefaultTheme is the look when no other theme has been chosen.
const DefaultTheme = "marquee"

// Themes lists every theme id the web app has (web/src/features/appearance/themes.ts; a web
// test checks the two lists match).
var Themes = []string{
	"marquee", "midnight-blue", "true-black", "daylight", "vintage-cinema", "neon", "forest",
	"ocean", "rose", "nord", "high-contrast",
	"spring", "summer", "fall", "winter",
	"new-year", "chinese-new-year", "valentines", "mardi-gras", "st-patricks", "easter",
	"independence-day", "halloween", "dia-de-los-muertos", "diwali", "thanksgiving", "hanukkah",
	"christmas",
}

// KnownTheme reports whether id is a theme.
func KnownTheme(id string) bool {
	for _, t := range Themes {
		if t == id {
			return true
		}
	}
	return false
}

// Active is the theme clients should show at now, in now's time zone: the holiday's while
// automatic holidays are on and one is under way, otherwise the chosen one. holiday is empty
// when none applies.
func Active(chosen string, autoHolidays bool, now time.Time) (theme, holiday string) {
	if !KnownTheme(chosen) {
		chosen = DefaultTheme
	}
	if autoHolidays {
		if h := HolidayAt(now); h != "" {
			return h, h
		}
	}
	return chosen, ""
}
