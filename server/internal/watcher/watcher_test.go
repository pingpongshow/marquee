package watcher

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"marquee/internal/db"
	"marquee/internal/library"
)

func TestWatcherQueuesScanAfterQuietPeriod(t *testing.T) {
	Debounce = 200 * time.Millisecond
	dir := t.TempDir()
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(dir, "t.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	root := filepath.Join(dir, "tv")
	os.MkdirAll(filepath.Join(root, "Show"), 0o755)
	libs := library.NewStore(d)
	l, _ := libs.Create(ctx, "TV", library.Shows, []string{root}, library.Options{})

	queued := make(chan int64, 10)
	w := &Watcher{Libraries: libs, Queue: func(id int64) { queued <- id }}
	w.Sync(ctx, true)

	// A new season folder, then a file inside it: one scan after things go quiet.
	os.MkdirAll(filepath.Join(root, "Show", "Season 02"), 0o755)
	time.Sleep(50 * time.Millisecond)
	os.WriteFile(filepath.Join(root, "Show", "Season 02", "Show - S02E01.mkv"), []byte("x"), 0o644)

	select {
	case id := <-queued:
		if id != l.ID {
			t.Fatalf("queued library %d", id)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no scan queued")
	}
	select {
	case <-queued:
		t.Fatal("burst of changes should queue one scan")
	case <-time.After(400 * time.Millisecond):
	}
	w.Sync(ctx, false)
}

func TestWatcherIgnoresMediaTrash(t *testing.T) {
	Debounce = 200 * time.Millisecond
	dir := t.TempDir()
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(dir, "t.db"), dir)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	root := filepath.Join(dir, "movies")
	os.MkdirAll(filepath.Join(root, library.TrashDir, "2026-10-01"), 0o755)
	libs := library.NewStore(d)
	libs.Create(ctx, "Movies", library.Movies, []string{root}, library.Options{})

	queued := make(chan int64, 10)
	w := &Watcher{Libraries: libs, Queue: func(id int64) { queued <- id }}
	w.Sync(ctx, true)
	defer w.Sync(ctx, false)

	// The trash emptying or filling queues nothing.
	os.WriteFile(filepath.Join(root, library.TrashDir, "2026-10-01", "Old.mkv"), []byte("x"), 0o644)
	os.MkdirAll(filepath.Join(root, library.TrashDir, "2026-10-02", "New"), 0o755)
	os.RemoveAll(filepath.Join(root, library.TrashDir, "2026-10-01"))
	select {
	case <-queued:
		t.Fatal("changes in the trash queued a scan")
	case <-time.After(600 * time.Millisecond):
	}
	if library.InTrash(filepath.Join(root, "Heat", "Heat.mkv")) || !library.InTrash(filepath.Join(root, library.TrashDir)) {
		t.Error("InTrash")
	}
}
