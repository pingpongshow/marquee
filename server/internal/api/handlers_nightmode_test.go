package api

import (
	"testing"

	"marquee/internal/auth"
)

func TestNightModePreference(t *testing.T) {
	// Default off, and stored only when on.
	if got := toAPIPrefs(auth.Preferences{}); got.NightMode == nil || *got.NightMode {
		t.Fatalf("default should be false: %+v", got.NightMode)
	}
	p, err := fromAPIPrefs(UserPreferences{NightMode: ptr(true)})
	if err != nil || !p.NightMode || !*toAPIPrefs(p).NightMode {
		t.Fatalf("on: %+v %v", p, err)
	}
	if p, _ := fromAPIPrefs(UserPreferences{}); p.NightMode {
		t.Fatal("omitted should be off")
	}
	// A session uses the player's choice, else the preference.
	on, off := auth.Preferences{NightMode: true}, auth.Preferences{}
	for _, c := range []struct {
		req  *bool
		pref auth.Preferences
		want bool
	}{{nil, off, false}, {nil, on, true}, {ptr(false), on, false}, {ptr(true), off, true}} {
		if got := nightMode(c.req, c.pref); got != c.want {
			t.Errorf("nightMode(%v, %v) = %v", c.req, c.pref.NightMode, got)
		}
	}
}
