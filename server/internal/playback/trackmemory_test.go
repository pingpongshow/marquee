package playback

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	"marquee/internal/db"
)

// Tracks chosen on one episode play on the next (PLAY-8): chosen at the start or in the
// player's own menu, matched by language, title and forced flag across the show's files, with
// a track id carried over from the last file ignored.
func TestTrackChoicesCarryAcrossEpisodes(t *testing.T) {
	dir := t.TempDir()
	database, err := db.Open(context.Background(), filepath.Join(dir, "t.db"), filepath.Join(dir, "b"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	exec := func(q string, args ...any) int64 {
		t.Helper()
		res, err := database.Exec(q, args...)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		return id
	}
	ctx := context.Background()
	user := exec(`INSERT INTO users (username, display_name) VALUES ('ann', 'Ann')`)
	lib := exec(`INSERT INTO libraries (name, type) VALUES ('TV', 'shows')`)
	show := exec(`INSERT INTO items (library_id, type, title, sort_title) VALUES (?, 'show', 'Show', 'Show')`, lib)
	season := exec(`INSERT INTO items (library_id, type, title, sort_title, parent_id) VALUES (?, 'season', 'S1', 'S1', ?)`, lib, show)
	type tracks struct{ jpn, eng, engSigns, engFull, engSDH int64 }
	episode := func(n int, engLang string) (int64, tracks) {
		item := exec(`INSERT INTO items (library_id, type, title, sort_title, parent_id, grandparent_id) VALUES (?, 'episode', ?, ?, ?, ?)`,
			lib, fmt.Sprint("E", n), "E", season, show)
		v := exec(`INSERT INTO media_versions (item_id) VALUES (?)`, item)
		f := exec(`INSERT INTO media_files (version_id, library_id, path, size, mtime, duration_ms, available) VALUES (?, ?, ?, 1, 1, 1000000, 1)`,
			v, lib, filepath.Join(dir, fmt.Sprintf("e%d.mkv", n)))
		st := func(kind, lang, title string, def, forced bool) int64 {
			return exec(`INSERT INTO streams (file_id, kind, codec, language, title, is_default, is_forced, stream_index) VALUES (?, ?, ?, NULLIF(?, ''), NULLIF(?, ''), ?, ?, 0)`,
				f, kind, map[string]string{"audio": "aac", "subtitle": "subrip"}[kind], lang, title, def, forced)
		}
		var tr tracks
		tr.jpn = st("audio", "jpn", "", true, false)
		tr.eng = st("audio", engLang, "", false, false)
		tr.engSigns = st("subtitle", engLang, "Signs & Songs", false, true)
		tr.engFull = st("subtitle", engLang, "Full", false, false)
		tr.engSDH = st("subtitle", engLang, "SDH", false, false)
		return item, tr
	}
	e1, t1 := episode(1, "eng")
	e2, t2 := episode(2, "en") // tagged differently in the next file
	e3, t3 := episode(3, "eng")
	m := &Manager{DB: database}
	start := func(item, audio, sub int64) *Session {
		t.Helper()
		s := &Session{UserID: user, ItemID: item} // as Start sets them
		if err := m.load(ctx, s, Request{UserID: user, ItemID: item, AudioStreamID: audio, SubtitleStreamID: sub, StartMS: -1, UserSubMode: "foreign"}); err != nil {
			t.Fatal(err)
		}
		return s
	}

	// Nothing chosen yet: the default audio, forced subtitles in its language (none here).
	if s := start(e1, 0, 0); s.AudioStreamID != t1.jpn {
		t.Fatalf("default audio %d", s.AudioStreamID)
	}
	// English audio and the SDH subtitles chosen at the start of episode 1…
	start(e1, t1.eng, t1.engSDH)
	// …play on episode 2 (its English tagged "en"), even with episode 1's ids sent along.
	s := start(e2, t1.eng, t1.engSDH)
	if s.AudioStreamID != t2.eng || s.SubtitleStreamID != t2.engSDH {
		t.Fatalf("episode 2: audio %d sub %d, want %d %d", s.AudioStreamID, s.SubtitleStreamID, t2.eng, t2.engSDH)
	}
	// Signs & Songs picked in the player's own menu on episode 2: episode 3 gets the forced track.
	m.RememberChoice(ctx, s, 0, t2.engSigns)
	if s := start(e3, 0, 0); s.SubtitleStreamID != t3.engSigns || s.AudioStreamID != t3.eng {
		t.Fatalf("episode 3: audio %d sub %d, want %d %d", s.AudioStreamID, s.SubtitleStreamID, t3.eng, t3.engSigns)
	}
	// Subtitles turned off in the player: off from then on. A report repeating it writes nothing new.
	s = start(e3, 0, 0)
	m.RememberChoice(ctx, s, 0, -1)
	m.RememberChoice(ctx, s, 0, -1)
	if s := start(e1, 0, 0); s.SubtitleStreamID != 0 || s.AudioStreamID != t1.eng {
		t.Fatalf("off: audio %d sub %d", s.AudioStreamID, s.SubtitleStreamID)
	}
	// A report of what the session started with isn't a choice.
	var before sql.NullString
	database.QueryRow(`SELECT subtitle_language FROM user_item_state WHERE user_id = ? AND item_id = ?`, user, show).Scan(&before)
	s = start(e2, 0, 0)
	m.RememberChoice(ctx, s, s.AudioStreamID, -1)
	var after sql.NullString
	database.QueryRow(`SELECT subtitle_language FROM user_item_state WHERE user_id = ? AND item_id = ?`, user, show).Scan(&after)
	if before != after || after.String != "off" {
		t.Fatalf("memory changed by a repeat: %v → %v", before, after)
	}
}
