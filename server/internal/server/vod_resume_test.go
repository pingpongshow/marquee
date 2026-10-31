package server

import (
	"context"
	"fmt"
	"testing"

	"marquee/internal/api"
	"marquee/internal/settings"
	"marquee/internal/vod"
)

// A film resumes where it was left: the session starts there (or where the viewer chose),
// and until the player gets there a position from near the beginning (a player that hasn't
// sought yet) doesn't overwrite the resume point. Once there, rewinding saves as usual.
func TestVodResumePointSurvivesASlowStart(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	var res api.AuthResult
	h.do("POST", "/setup", map[string]any{"serverName": "T", "username": "admin", "password": "correct horse", "device": device}, &res)
	h.token = res.Token
	h.store.Update(ctx, func(s *settings.Settings) error {
		s.Integrations.VODEnabled = true
		s.Integrations.VODSources = []settings.VODSource{{ID: "p", Name: "Films", URL: "http://127.0.0.1:1/films.m3u", Enabled: true}}
		return nil
	})
	h.handlers.VOD = &vod.Service{DB: h.db, Settings: h.store, Streams: &vod.Streams{}}
	r, err := h.db.Exec(`INSERT INTO vod_movies(source_id, source_key, title, stream_url, added_at, runtime_ms)
		VALUES ('p', 'f1', 'Ronin', 'http://127.0.0.1:1/ronin.m3u8', '2026-01-01T00:00:00Z', 7200000)`)
	if err != nil {
		t.Fatal(err)
	}
	film, _ := r.LastInsertId()
	var uid int64
	h.db.QueryRow("SELECT id FROM users WHERE username='admin'").Scan(&uid)
	h.db.Exec(`INSERT INTO vod_progress(user_id, movie_id, position_ms, updated_at) VALUES (?, ?, 600000, '2026-01-01T00:00:00Z')`, uid, film)

	resume := func() int64 {
		var pos int64
		h.db.QueryRow(`SELECT position_ms FROM vod_progress WHERE user_id=? AND movie_id=?`, uid, film).Scan(&pos)
		return pos
	}
	play := func(body map[string]any) api.VodSession {
		t.Helper()
		var s api.VodSession
		if code := h.do("POST", fmt.Sprintf("/livetv/vod/movies/%d/play", film), body, &s); code != 200 {
			t.Fatalf("play: %d", code)
		}
		return s
	}
	report := func(s api.VodSession, ms int64) {
		t.Helper()
		if code := h.do("PATCH", "/livetv/vod/sessions/"+s.Id, map[string]any{"positionMs": ms, "durationMs": 7200000}, nil); code != 204 {
			t.Fatalf("report %d: %d", ms, code)
		}
	}

	s := play(nil)
	if s.StartMs == nil || *s.StartMs != 600000 {
		t.Fatalf("the session starts at %v, want the resume point", s.StartMs)
	}
	report(s, 15000) // still at the beginning: not where they are
	if got := resume(); got != 600000 {
		t.Errorf("an early report from the beginning overwrote the resume point: %d", got)
	}
	report(s, 590000) // the seek landed on a keyframe a little before
	report(s, 620000)
	if got := resume(); got != 620000 {
		t.Errorf("after reaching it: %d", got)
	}
	report(s, 20000) // rewound on purpose
	if got := resume(); got != 20000 {
		t.Errorf("a rewind wasn't saved: %d", got)
	}

	// From the start, asked for: the session starts at 0 and saves from there.
	h.db.Exec(`UPDATE vod_progress SET position_ms=600000 WHERE user_id=? AND movie_id=?`, uid, film)
	s = play(map[string]any{"startMs": 0})
	if s.StartMs != nil && *s.StartMs != 0 { // 0 is left out
		t.Fatalf("from the start: the session starts at %d", *s.StartMs)
	}
	report(s, 15000)
	if got := resume(); got != 15000 {
		t.Errorf("from the start: %d", got)
	}
}
