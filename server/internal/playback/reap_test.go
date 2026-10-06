package playback

import (
	"context"
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// A gapless next track waits silently while a long track plays on the same device: it must
// outlive the idle timeout, or the player is left holding a dead stream address.
func TestReapKeepsPreloadWhileDevicePlays(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	old := time.Now().Add(-10 * time.Minute)
	m := &Manager{DB: db}
	m.sessions = map[string]*Session{
		"playing": {ID: "playing", DeviceID: 1, lastActive: time.Now()},
		"next":    {ID: "next", DeviceID: 1, preload: true, lastActive: old},
		"gone":    {ID: "gone", DeviceID: 2, preload: true, lastActive: old}, // its device stopped
		"stale":   {ID: "stale", DeviceID: 3, lastActive: old},
	}
	m.Reap(context.Background(), 3*time.Minute)
	for id, want := range map[string]bool{"playing": true, "next": true, "gone": false, "stale": false} {
		if _, ok := m.sessions[id]; ok != want {
			t.Errorf("%s kept=%v, want %v", id, ok, want)
		}
	}
}
