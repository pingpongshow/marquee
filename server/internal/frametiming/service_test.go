package frametiming

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"marquee/internal/db"
	"marquee/internal/library"
	"marquee/internal/mediatrash"
)

func noLook(string) (string, error) { return "", errors.New("not here") }

// A repair asked for in Library Health puts a repaired copy in the file's place (the record
// kept, pointed to be read again), sends the original to the media trash and leaves the
// queue. A file that isn't an MP4 can't be queued; a failed repair says why and can be
// asked for again; a playing file waits.
func TestQueueRepairs(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	ctx := context.Background()
	dir := t.TempDir()
	d, err := db.Open(ctx, filepath.Join(dir, "t.db"), filepath.Join(dir, "b"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	media := filepath.Join(dir, "media")
	os.MkdirAll(filepath.Join(media, "Sunshine (2007)"), 0o755)
	lib, err := library.NewStore(d).Create(ctx, "Movies", library.Movies, []string{media}, library.Options{})
	if err != nil {
		t.Fatal(err)
	}
	good := filepath.Join(dir, "good.mp4")
	if out, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=320x180:rate=24:duration=4",
		"-c:v", "libx264", "-bf", "3", "-g", "48", "-movflags", "+faststart", good).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	path := filepath.Join(media, "Sunshine (2007)", "Sunshine (2007) Bluray-1080p.mp4")
	breakTiming(t, good, path)
	add := func(title, p, container string) int64 {
		r, _ := d.Exec(`INSERT INTO items (library_id, type, title, sort_title) VALUES (?, 'movie', ?, ?)`, lib.ID, title, title)
		item, _ := r.LastInsertId()
		r, _ = d.Exec(`INSERT INTO media_versions (item_id) VALUES (?)`, item)
		v, _ := r.LastInsertId()
		r, err := d.Exec(`INSERT INTO media_files (version_id, library_id, path, size, mtime, container, duration_ms, timing_checked_at, timing_error)
			VALUES (?, ?, ?, 1, 1, ?, 4000, '2026-01-01T00:00:00Z', 'Frames are timed in decode order')`, v, lib.ID, p, container)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := r.LastInsertId()
		return id
	}
	fileID := add("Sunshine", path, "mov,mp4,m4a,3gp,3g2,mj2")
	mkvID := add("Other", filepath.Join(media, "Other.mkv"), "matroska,webm")

	playing := map[int64]bool{}
	var rescanned int64
	s := &Service{DB: d, LookPath: noLook, Trash: &mediatrash.Service{DB: d},
		Rescan: func(id int64) { rescanned = id }, Playing: func(id int64) bool { return playing[id] }}
	if _, err := s.Queue(ctx, []int64{mkvID}); !errors.Is(err, ErrNotRepairable) {
		t.Errorf("an MKV was queued: %v", err)
	}
	if n, err := s.Queue(ctx, nil); err != nil || n != 1 {
		t.Fatalf("queue all: %d %v", n, err)
	}
	if n, _ := s.Queue(ctx, []int64{fileID}); n != 0 || s.Waiting(ctx) != 1 {
		t.Errorf("queued twice: %d, %d waiting", n, s.Waiting(ctx))
	}

	// Playing: it waits.
	playing[fileID] = true
	if s.next(ctx) {
		t.Fatal("repaired a playing file")
	}
	playing[fileID] = false
	if !s.next(ctx) {
		t.Fatal("nothing repaired")
	}
	if s.Waiting(ctx) != 0 {
		t.Errorf("%d still waiting", s.Waiting(ctx))
	}
	if reason, err := Check(ctx, nil, "", path); err != nil || reason != "" {
		t.Errorf("the file in place still has its timing wrong: %q %v", reason, err)
	}
	var size int64
	var timingErr sql.NullString
	d.QueryRow(`SELECT size, timing_error FROM media_files WHERE id = ?`, fileID).Scan(&size, &timingErr)
	if size != -1 || timingErr.Valid || rescanned != lib.ID {
		t.Errorf("record: size %d, error %v, rescanned %d", size, timingErr, rescanned)
	}
	trashed, _ := filepath.Glob(filepath.Join(media, library.TrashDir, "*", "Sunshine (2007)", "*.mp4"))
	if len(trashed) != 1 {
		t.Errorf("original in the trash: %v", trashed)
	} else if reason, _ := Check(ctx, nil, "", trashed[0]); reason == "" {
		t.Error("the trash holds the repaired file, not the original")
	}
	if _, err := os.Stat(path + PartSuffix); !os.IsNotExist(err) {
		t.Error("the part file was left behind")
	}

	// A repair that fails says why, and can be asked for again.
	d.Exec(`UPDATE media_files SET timing_error = 'Frames are timed in decode order' WHERE id = ?`, fileID)
	os.WriteFile(path, []byte("not a video"), 0o644)
	s.Queue(ctx, []int64{fileID})
	s.next(ctx)
	st, _ := s.States(ctx, []int64{fileID})
	if st[fileID].State != StateFailed || !strings.Contains(st[fileID].Error, "frame order") {
		t.Errorf("failed repair: %+v", st[fileID])
	}
	if n, _ := s.Queue(ctx, []int64{fileID}); n != 1 {
		t.Error("a failed repair couldn't be asked for again")
	}
	s.Cancel(ctx, fileID)
	if s.Waiting(ctx) != 0 {
		t.Error("cancel left it waiting")
	}
}
