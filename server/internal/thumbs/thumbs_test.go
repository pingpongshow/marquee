package thumbs

import (
	"context"
	"database/sql"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"marquee/internal/db"
	"marquee/internal/images"
)

func clip(t *testing.T, dir string) string {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	p := filepath.Join(dir, "clip.mkv")
	out, err := exec.Command("ffmpeg", "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=320x240:rate=25:duration=12",
		"-c:v", "libx264", "-preset", "ultrafast", p).CombinedOutput()
	if err != nil {
		t.Fatalf("make clip: %v %s", err, out)
	}
	return p
}

type env struct {
	db  *sql.DB
	dir string
	n   int64
}

func newEnv(t *testing.T) *env {
	dir := t.TempDir()
	d, err := db.Open(context.Background(), filepath.Join(dir, "t.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	d.Exec(`INSERT INTO libraries(id, name, type) VALUES (1, 'Shows', 'shows')`)
	return &env{db: d, dir: dir}
}

// item adds an item of a type with one file.
func (e *env) item(t *testing.T, typ, path string, available bool) (item, file int64) {
	t.Helper()
	e.n++
	r, err := e.db.Exec(`INSERT INTO items(library_id, type, title, sort_title, added_at) VALUES (1, ?, 'x', 'x', ?)`, typ, e.n)
	if err != nil {
		t.Fatal(err)
	}
	item, _ = r.LastInsertId()
	r, _ = e.db.Exec(`INSERT INTO media_versions(item_id) VALUES (?)`, item)
	v, _ := r.LastInsertId()
	r, err = e.db.Exec(`INSERT INTO media_files(version_id, library_id, path, size, mtime, duration_ms, width, height, video_codec, available)
		VALUES (?, 1, ?, 1, 100, 12000, 320, 240, 'h264', ?)`, v, path, available)
	if err != nil {
		t.Fatal(err)
	}
	file, _ = r.LastInsertId()
	return item, file
}

func ids(cs []Candidate) map[int64]bool {
	m := map[int64]bool{}
	for _, c := range cs {
		m[c.ItemID] = true
	}
	return m
}

// Only episodes and videos without a still (or a video with only its scan-time frame),
// not extras, not missing files, not ones that failed before.
func TestPending(t *testing.T) {
	e := newEnv(t)
	bare, _ := e.item(t, "episode", "/x/bare.mkv", true)
	still, _ := e.item(t, "episode", "/x/still.mkv", true)
	e.db.Exec(`INSERT INTO artwork(item_id, kind, source, remote_url, selected) VALUES (?, 'thumb', 'tmdb', 'http://x', 1)`, still)
	video, _ := e.item(t, "video", "/x/video.mkv", true)
	e.db.Exec(`INSERT INTO artwork(item_id, kind, source, local_path, selected) VALUES (?, 'thumb', 'frame', '/x/video.mkv', 1)`, video)
	extra, _ := e.item(t, "video", "/x/extra.mkv", true)
	e.db.Exec(`UPDATE items SET extra_type = 'trailer' WHERE id = ?`, extra)
	movie, _ := e.item(t, "movie", "/x/movie.mkv", true)
	gone, _ := e.item(t, "episode", "/x/gone.mkv", false)
	failed, failedFile := e.item(t, "episode", "/x/failed.mkv", true)
	e.db.Exec(`INSERT INTO generated_thumb_failures(file_id, mtime, error) VALUES (?, 100, 'x')`, failedFile)
	changed, changedFile := e.item(t, "episode", "/x/changed.mkv", true)
	e.db.Exec(`INSERT INTO generated_thumb_failures(file_id, mtime, error) VALUES (?, 99, 'x')`, changedFile)
	generated, _ := e.item(t, "episode", "/x/generated.mkv", true)
	e.db.Exec(`INSERT INTO artwork(item_id, kind, source, local_path, selected) VALUES (?, 'thumb', 'generated', 'generated/x.jpg', 1)`, generated)

	s := &Service{DB: e.db}
	got, err := s.Pending(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	m := ids(got)
	for _, want := range []int64{bare, video, changed} {
		if !m[want] {
			t.Errorf("item %d should be pending: %v", want, m)
		}
	}
	for _, not := range []int64{still, extra, movie, gone, failed, generated} {
		if m[not] {
			t.Errorf("item %d shouldn't be pending", not)
		}
	}
	if len(got) != 3 || got[0].ItemID != changed {
		t.Errorf("newest first: %+v", got)
	}
}

// A frame is written to the image cache, recorded as the selected thumb and served; a
// missing file is remembered as failed; a real still that arrives later wins.
func TestGenerateServeAndStillWins(t *testing.T) {
	e := newEnv(t)
	path := clip(t, e.dir)
	ep, _ := e.item(t, "episode", path, true)
	missing, missingFile := e.item(t, "episode", filepath.Join(e.dir, "nope.mkv"), true)
	cache := filepath.Join(e.dir, "cache", "images")
	s := &Service{DB: e.db, FFmpeg: "ffmpeg", CacheDir: cache}
	ctx := context.Background()
	msg, err := s.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Log(msg)

	var artID int64
	var rel string
	var w int
	if err := e.db.QueryRow(`SELECT id, local_path, width FROM artwork WHERE item_id = ? AND kind = 'thumb' AND source = 'generated' AND selected = 1`, ep).
		Scan(&artID, &rel, &w); err != nil {
		t.Fatalf("no generated thumb: %v", err)
	}
	f, err := os.Open(filepath.Join(cache, rel))
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := jpeg.DecodeConfig(f)
	f.Close()
	if err != nil || cfg.Width != Width || w != Width {
		t.Fatalf("jpeg %dx%d (%v), row width %d", cfg.Width, cfg.Height, err, w)
	}
	img := images.New(e.db, cache, "ffmpeg")
	if p, ctype, err := img.Path(ctx, artID, 480); err != nil || ctype != "image/jpeg" || p == "" {
		t.Fatalf("served: %q %q %v", p, ctype, err)
	}

	var n int
	e.db.QueryRow(`SELECT COUNT(*) FROM artwork WHERE item_id = ?`, missing).Scan(&n)
	if n != 0 {
		t.Error("a thumb for a missing file")
	}
	e.db.QueryRow(`SELECT COUNT(*) FROM generated_thumb_failures WHERE file_id = ?`, missingFile).Scan(&n)
	if n != 1 {
		t.Error("the missing file wasn't remembered")
	}
	if left, _ := s.Pending(ctx); len(left) != 0 {
		t.Errorf("still pending: %+v", left)
	}

	// A metadata refresh finds a still: it replaces the generated one, selected.
	if _, err := e.db.Exec(`INSERT INTO artwork(item_id, kind, source, remote_url, selected)
		SELECT ?, 'thumb', 'tmdb', 'http://still', NOT EXISTS (SELECT 1 FROM artwork WHERE item_id = ? AND kind = 'thumb' AND selected = 1)`, ep, ep); err != nil {
		t.Fatal(err)
	}
	var source string
	var selected int
	e.db.QueryRow(`SELECT COUNT(*) FROM artwork WHERE item_id = ? AND kind = 'thumb'`, ep).Scan(&n)
	e.db.QueryRow(`SELECT source, selected FROM artwork WHERE item_id = ? AND kind = 'thumb'`, ep).Scan(&source, &selected)
	if n != 1 || source != "tmdb" || selected != 1 {
		t.Fatalf("after a still: %d thumbs, %s selected=%d", n, source, selected)
	}
	// The next run leaves it alone and tidies the orphaned image away.
	if _, err := s.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(cache, rel)); !os.IsNotExist(err) {
		t.Errorf("orphaned image kept: %v", err)
	}
	e.db.QueryRow(`SELECT COUNT(*) FROM artwork WHERE item_id = ? AND source = 'generated'`, ep).Scan(&n)
	if n != 0 {
		t.Error("generated again for an item with a still")
	}
}

func TestPosition(t *testing.T) {
	for _, c := range []struct {
		ms   int64
		want float64
	}{{2_700_000, 810}, {12_000, 3.6}, {600_000, 180}, {200_000, 60}} {
		if got := Position(c.ms); got < c.want-0.01 || got > c.want+0.01 {
			t.Errorf("Position(%d) = %v, want %v", c.ms, got, c.want)
		}
	}
}
