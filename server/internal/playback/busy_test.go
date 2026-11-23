package playback

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"marquee/internal/db"
	"marquee/internal/settings"
)

// A start refused because the server is at its transcode limit leaves the device's current
// session playing; the device's own transcode doesn't count against a start that replaces it.
func TestBusyStartKeepsCurrentSession(t *testing.T) {
	dir := t.TempDir()
	ctx := context.Background()
	database, err := db.Open(ctx, filepath.Join(dir, "t.db"), filepath.Join(dir, "b"))
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
	user := exec(`INSERT INTO users (username, display_name) VALUES ('ann', 'Ann')`)
	lib := exec(`INSERT INTO libraries (name, type) VALUES ('Films', 'movies')`)
	film := exec(`INSERT INTO items (library_id, type, title, sort_title) VALUES (?, 'movie', 'Film', 'Film')`, lib)
	v := exec(`INSERT INTO media_versions (item_id) VALUES (?)`, film)
	f := exec(`INSERT INTO media_files (version_id, library_id, path, size, mtime, duration_ms, container, available) VALUES (?, ?, ?, 1, 1, 1000000, 'mp4', 1)`,
		v, lib, filepath.Join(dir, "film.mp4"))
	exec(`INSERT INTO streams (file_id, kind, codec, stream_index, width, height) VALUES (?, 'video', 'h264', 0, 1920, 1080)`, f)
	exec(`INSERT INTO streams (file_id, kind, codec, stream_index, channels) VALUES (?, 'audio', 'aac', 1, 2)`, f)
	st, err := settings.Open(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	st.Update(ctx, func(s *settings.Settings) error { s.Transcoder.MaxConcurrentTranscodes = 1; return nil })
	m := &Manager{DB: database, Settings: st, FFmpeg: "ffmpeg", TranscodeDir: filepath.Join(dir, "transcode")}
	direct := DeviceProfile{Containers: []string{"mp4"}, VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"}}
	start := func(device int64, p DeviceProfile) (*Session, error) {
		return m.Start(ctx, Request{UserID: user, DeviceID: device, ItemID: film, StartMS: 0, Profile: p, RemoteAllowed: true})
	}

	tv, err := start(1, DeviceProfile{HLS: true})
	if err != nil || tv.transcoder == nil {
		t.Fatalf("a transcode: %v %+v", err, tv)
	}
	phone, err := start(2, direct)
	if err != nil || phone.transcoder != nil {
		t.Fatalf("direct play: %v", err)
	}
	// The phone asks for something that needs a transcode while the TV has the only one.
	if _, err := start(2, DeviceProfile{HLS: true}); !errors.Is(err, ErrBusy) {
		t.Fatalf("over the limit: %v", err)
	}
	if !m.Exists(phone.ID) {
		t.Fatal("the refused start ended what the phone was playing")
	}
	// The TV changing quality replaces its own transcode: that one doesn't count.
	tv2, err := start(1, DeviceProfile{HLS: true})
	if err != nil {
		t.Fatalf("replacing its own transcode: %v", err)
	}
	if m.Exists(tv.ID) || !m.Exists(tv2.ID) || !m.Exists(phone.ID) {
		t.Fatal("the TV's previous session should be the only one ended")
	}
}
