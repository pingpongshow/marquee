package soundprint

import (
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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

	var asked []SpokenItem
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct{ Items []SpokenItem }
		json.NewDecoder(r.Body).Decode(&req)
		asked = append(asked, req.Items...)
		out := struct {
			Model   string         `json:"model"`
			Results []SpokenResult `json:"results"`
		}{Model: "whisper#1"}
		for _, it := range req.Items {
			res := SpokenResult{Path: it.Path, Stream: it.Stream}
			if it.Stream == 1 {
				res.Language, res.Confidence, res.Heard = "fr", 0.98, 4
			}
			out.Results = append(out.Results, res)
		}
		json.NewEncoder(w).Encode(out)
	}))
	defer srv.Close()
	s := &Service{DB: d, Client: &Client{BaseURL: srv.URL}, Enabled: func() bool { return true }}
	h := Health{Speech: "whisper#1"}

	if _, err := s.spokenPass(ctx, h); err != nil {
		t.Fatal(err)
	}
	if len(asked) != 2 || asked[0].Stream != 1 || asked[1].Stream != 3 || asked[0].Path != "/m/film.mkv" {
		t.Fatalf("asked %+v", asked)
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
	// An older sidecar without speech: nothing asked.
	asked = nil
	exec(`DELETE FROM spoken_languages`)
	s.spokenPass(ctx, Health{})
	if len(asked) != 0 {
		t.Fatal("asked an older sidecar")
	}
	if SpokenCode("de") != "ger" || SpokenCode("xx") != "xx" {
		t.Fatal("codes")
	}
}
