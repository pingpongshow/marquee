package soundprint

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"marquee/internal/db"
)

// Audio tracks without a language are heard once (PLAY-19): a clear answer becomes the
// track's language, marked as detected; an unclear one (music) stays unknown and isn't asked
// again until the file changes; tracks with a language and music tracks aren't asked.
func TestSpokenLanguagePass(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	d, err := db.Open(ctx, filepath.Join(dir, "t.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	exec := func(q string, args ...any) int64 {
		t.Helper()
		r, err := d.Exec(q, args...)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := r.LastInsertId()
		return id
	}
	lib := exec(`INSERT INTO libraries (name, type) VALUES ('Movies', 'movies')`)
	film := exec(`INSERT INTO items (library_id, type, title, sort_title) VALUES (?, 'movie', 'Film', 'Film')`, lib)
	v := exec(`INSERT INTO media_versions (item_id) VALUES (?)`, film)
	f := exec(`INSERT INTO media_files (version_id, library_id, path, size, mtime, available) VALUES (?, ?, '/m/film.mkv', 10, 100, 1)`, v, lib)
	exec(`INSERT INTO streams (file_id, stream_index, kind, codec) VALUES (?, 0, 'video', 'h264')`, f)
	exec(`INSERT INTO streams (file_id, stream_index, kind, codec) VALUES (?, 1, 'audio', 'aac')`, f)
	exec(`INSERT INTO streams (file_id, stream_index, kind, codec, language) VALUES (?, 2, 'audio', 'aac', 'eng')`, f)
	exec(`INSERT INTO streams (file_id, stream_index, kind, codec, language) VALUES (?, 3, 'audio', 'aac', 'und')`, f)
	music := exec(`INSERT INTO libraries (name, type) VALUES ('Music', 'music')`)
	song := exec(`INSERT INTO items (library_id, type, title, sort_title) VALUES (?, 'track', 'Song', 'Song')`, music)
	sv := exec(`INSERT INTO media_versions (item_id) VALUES (?)`, song)
	sf := exec(`INSERT INTO media_files (version_id, library_id, path, size, mtime, available) VALUES (?, ?, '/m/song.flac', 1, 1, 1)`, sv, music)
	exec(`INSERT INTO streams (file_id, stream_index, kind, codec) VALUES (?, 0, 'audio', 'flac')`, sf)

	// A stand-in FFmpeg: a second of silence for any file but a broken one.
	ff := filepath.Join(dir, "ffmpeg")
	os.WriteFile(ff, []byte("#!/bin/sh\ncase \"$*\" in *broken*) echo 'Invalid data' >&2; exit 1;; esac\nhead -c 32000 /dev/zero\n"), 0o755)
	broken := exec(`INSERT INTO items (library_id, type, title, sort_title) VALUES (?, 'movie', 'Broken', 'Broken')`, lib)
	bv := exec(`INSERT INTO media_versions (item_id) VALUES (?)`, broken)
	bf := exec(`INSERT INTO media_files (version_id, library_id, path, size, mtime, available) VALUES (?, ?, '/m/broken.mkv', 1, 1, 1)`, bv, lib)
	exec(`INSERT INTO streams (file_id, stream_index, kind, codec) VALUES (?, 1, 'audio', 'aac')`, bf)

	type sentItem struct {
		ID    string
		Clips []string
	}
	var asked []sentItem
	sidecarFails := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/spoken_language_clips" {
			http.NotFound(w, r)
			return
		}
		var req struct{ Items []sentItem }
		json.NewDecoder(r.Body).Decode(&req)
		asked = append(asked, req.Items...)
		out := struct {
			Model   string         `json:"model"`
			Results []SpokenResult `json:"results"`
		}{Model: "whisper#3"}
		for _, it := range req.Items {
			res := SpokenResult{ID: it.ID}
			switch {
			case sidecarFails:
				res.Error = "model wouldn't load"
			case len(asked)-len(req.Items) == 0 && it.ID == "0": // the first track sent: the film's stream 1
				res.Language, res.Confidence, res.Heard, res.Agree = "fr", 0.98, 3, 3
			}
			out.Results = append(out.Results, res)
		}
		json.NewEncoder(w).Encode(out)
	}))
	defer srv.Close()
	s := &Service{DB: d, FFmpeg: ff, Client: &Client{BaseURL: srv.URL}, Enabled: func() bool { return true }}
	h := Health{Speech: "whisper#3"}

	// A sidecar that fails every track: the run stops with its error and keeps nothing.
	sidecarFails = true
	if _, err := s.spokenPass(ctx, h); err == nil || !strings.Contains(err.Error(), "model wouldn't load") {
		t.Fatalf("sidecar failure: %v", err)
	}
	var kept int
	d.QueryRow(`SELECT COUNT(*) FROM spoken_languages WHERE file_id = ?`, f).Scan(&kept)
	if kept != 0 {
		t.Fatalf("kept %d after the sidecar failed", kept)
	}
	sidecarFails = false
	asked = nil

	if _, err := s.spokenPass(ctx, h); err != nil {
		t.Fatal(err)
	}
	// The film's two unknown tracks were sent, cut by the server (three stretches each); the
	// broken file wasn't sent but is kept as heard; music isn't asked.
	if len(asked) != 2 || len(asked[0].Clips) == 0 {
		t.Fatalf("asked %+v", asked)
	}
	var brokenKept int
	d.QueryRow(`SELECT COUNT(*) FROM spoken_languages WHERE file_id = ? AND language IS NULL`, bf).Scan(&brokenKept)
	if brokenKept != 1 {
		t.Fatal("an unreadable file should be kept as heard")
	}
	lang := func(index int) (string, bool) {
		var l sql.NullString
		var detected bool
		d.QueryRow(`SELECT language, language_detected FROM streams WHERE file_id = ? AND stream_index = ?`, f, index).Scan(&l, &detected)
		return l.String, detected
	}
	if l, det := lang(1); l != "fre" || !det {
		t.Fatalf("stream 1: %q detected=%v", l, det)
	}
	if l, det := lang(3); l != "und" || det {
		t.Fatalf("music stream: %q detected=%v", l, det)
	}
	if l, det := lang(2); l != "eng" || det {
		t.Fatalf("tagged stream changed: %q detected=%v", l, det)
	}
	// A rescan writes the streams again: the heard language comes back while the file's the same.
	exec(`DELETE FROM streams WHERE file_id = ? AND kind = 'audio'`, f)
	exec(`INSERT INTO streams (file_id, stream_index, kind, codec) VALUES (?, 1, 'audio', 'aac')`, f)
	exec(`INSERT INTO streams (file_id, stream_index, kind, codec, language) VALUES (?, 2, 'audio', 'aac', 'eng')`, f)
	exec(`INSERT INTO streams (file_id, stream_index, kind, codec, language) VALUES (?, 3, 'audio', 'aac', 'und')`, f)
	if err := db.ReapplySpokenLanguages(ctx, d, f); err != nil {
		t.Fatal(err)
	}
	if l, det := lang(1); l != "fre" || !det {
		t.Fatalf("after a rescan: %q detected=%v", l, det)
	}
	if l, det := lang(3); l != "und" || det {
		t.Fatalf("music stream after a rescan: %q detected=%v", l, det)
	}
	// Heard once: the next run asks nothing.
	asked = nil
	s.spokenPass(ctx, h)
	if len(asked) != 0 {
		t.Fatalf("asked again: %+v", asked)
	}
	// The file changed: heard again.
	exec(`UPDATE media_files SET mtime = 200 WHERE id = ?`, f)
	exec(`UPDATE streams SET language = NULL, language_detected = 0 WHERE file_id = ? AND stream_index = 1`, f)
	s.spokenPass(ctx, h)
	if len(asked) != 2 {
		t.Fatalf("changed file not heard again: %+v", asked)
	}
	// An older sidecar (one that can't take the server's stretches): nothing asked.
	asked = nil
	exec(`DELETE FROM spoken_languages`)
	s.spokenPass(ctx, Health{Speech: "whisper#2"})
	if len(asked) != 0 {
		t.Fatal("asked an older sidecar")
	}
	if SpokenCode("de") != "ger" || SpokenCode("xx") != "xx" {
		t.Fatal("codes")
	}
}

// A track the sidecar fails on its own is tried on a few runs, then left alone until its file
// changes; answers are matched to tracks by id, whatever order they come back in.
func TestSpokenLanguageTrackFailures(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	d, err := db.Open(ctx, filepath.Join(dir, "t.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	exec := func(q string, args ...any) int64 {
		t.Helper()
		r, err := d.Exec(q, args...)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := r.LastInsertId()
		return id
	}
	lib := exec(`INSERT INTO libraries (name, type) VALUES ('Movies', 'movies')`)
	film := exec(`INSERT INTO items (library_id, type, title, sort_title) VALUES (?, 'movie', 'Film', 'Film')`, lib)
	v := exec(`INSERT INTO media_versions (item_id) VALUES (?)`, film)
	f := exec(`INSERT INTO media_files (version_id, library_id, path, size, mtime, available) VALUES (?, ?, '/m/film.mkv', 10, 100, 1)`, v, lib)
	exec(`INSERT INTO streams (file_id, stream_index, kind, codec) VALUES (?, 1, 'audio', 'aac')`, f)
	exec(`INSERT INTO streams (file_id, stream_index, kind, codec) VALUES (?, 2, 'audio', 'aac')`, f)
	ff := filepath.Join(dir, "ffmpeg")
	os.WriteFile(ff, []byte("#!/bin/sh\nhead -c 32000 /dev/zero\n"), 0o755)

	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Items []struct{ ID string } }
		json.NewDecoder(r.Body).Decode(&req)
		calls++
		out := struct {
			Model   string         `json:"model"`
			Results []SpokenResult `json:"results"`
		}{Model: "whisper#3"}
		// Last first: stream 1 (id "0") is German, stream 2 (id "1") can't be heard.
		for i := len(req.Items) - 1; i >= 0; i-- {
			res := SpokenResult{ID: req.Items[i].ID}
			if res.ID == "0" && len(req.Items) == 2 {
				res.Language, res.Confidence, res.Heard, res.Agree = "de", 0.95, 3, 3
			} else {
				res.Error = "couldn't decode the clip"
			}
			out.Results = append(out.Results, res)
		}
		json.NewEncoder(w).Encode(out)
	}))
	defer srv.Close()
	s := &Service{DB: d, FFmpeg: ff, Client: &Client{BaseURL: srv.URL}, Enabled: func() bool { return true }}
	h := Health{Speech: "whisper#3"}

	if _, err := s.spokenPass(ctx, h); err != nil {
		t.Fatal(err)
	}
	var lang sql.NullString
	d.QueryRow(`SELECT language FROM streams WHERE file_id = ? AND stream_index = 1`, f).Scan(&lang)
	if lang.String != "ger" {
		t.Fatalf("stream 1 is %q: answers should be matched by id", lang.String)
	}
	d.QueryRow(`SELECT language FROM streams WHERE file_id = ? AND stream_index = 2`, f).Scan(&lang)
	if lang.Valid {
		t.Fatalf("the failed track got %q", lang.String)
	}
	failures := func() (n int) {
		d.QueryRow(`SELECT failures FROM spoken_languages WHERE file_id = ? AND stream_index = 2`, f).Scan(&n)
		return n
	}
	if failures() != 1 {
		t.Fatalf("failures %d after one run", failures())
	}
	// Tried again on the next runs, then left alone.
	for i := 0; i < 4; i++ {
		s.spokenPass(ctx, h)
	}
	if calls != spokenAttempts || failures() != spokenAttempts {
		t.Fatalf("asked %d times, %d failures, want %d of each", calls, failures(), spokenAttempts)
	}
	// The file changed: its tries start again.
	exec(`UPDATE media_files SET mtime = 200 WHERE id = ?`, f)
	exec(`UPDATE streams SET language = 'ger' WHERE file_id = ? AND stream_index = 1`, f)
	s.spokenPass(ctx, h)
	if calls != spokenAttempts+1 || failures() != 1 {
		t.Fatalf("after the file changed: asked %d times, %d failures", calls, failures())
	}
}
