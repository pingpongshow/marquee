// Package watcher queues a library scan shortly after files change in its folders (LIB-4).
// Scans are incremental (unchanged files are skipped), so a whole-library rescan after a
// quiet period is cheap and avoids partial-scan edge cases.
package watcher

import (
	"context"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"

	"marquee/internal/library"
)

// Debounce is how long a library must be quiet after a change before it is scanned.
// Downloads write in bursts; waiting avoids scanning half-copied files.
var Debounce = 30 * time.Second

type Watcher struct {
	Libraries *library.Store
	// Queue is called with a library id once its folders have settled.
	Queue func(libID int64)

	mu      sync.Mutex
	w       *fsnotify.Watcher
	roots   map[string]int64 // library folder → library id
	timers  map[int64]*time.Timer
	enabled bool
}

// Sync (re)builds the watch list from the libraries. Call after library changes and when
// the "watch filesystem" setting changes.
func (wt *Watcher) Sync(ctx context.Context, enabled bool) {
	wt.mu.Lock()
	defer wt.mu.Unlock()
	if wt.w != nil {
		wt.w.Close()
		wt.w = nil
	}
	wt.enabled = enabled
	if !enabled {
		return
	}
	libs, err := wt.Libraries.List(ctx)
	if err != nil {
		slog.Error("watcher: list libraries", "err", err)
		return
	}
	w, err := fsnotify.NewWatcher()
	if err != nil {
		slog.Error("watcher: start", "err", err)
		return
	}
	wt.w = w
	wt.roots = map[string]int64{}
	if wt.timers == nil {
		wt.timers = map[int64]*time.Timer{}
	}
	count := 0
	for _, l := range libs {
		for _, root := range l.Paths {
			wt.roots[root] = l.ID
			count += addTree(w, root)
		}
	}
	slog.Info("watching library folders", "folders", count)
	go wt.loop(w)
}

func addTree(w *fsnotify.Watcher, root string) int {
	n := 0
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		if library.InTrash(d.Name()) {
			return filepath.SkipDir // deleted media waiting in the trash
		}
		if p != root && strings.HasPrefix(d.Name(), ".") {
			return filepath.SkipDir
		}
		if err := w.Add(p); err != nil {
			slog.Warn("watcher: cannot watch folder", "path", p, "err", err)
			return nil
		}
		n++
		return nil
	})
	return n
}

func (wt *Watcher) loop(w *fsnotify.Watcher) {
	for {
		select {
		case ev, ok := <-w.Events:
			if !ok {
				return
			}
			if library.InTrash(ev.Name) {
				continue // files moving into or out of the media trash
			}
			// New folders (a show's new season, a new album) need watching too.
			if ev.Has(fsnotify.Create) {
				if fi, err := os.Stat(ev.Name); err == nil && fi.IsDir() {
					addTree(w, ev.Name)
				}
			}
			if ev.Has(fsnotify.Chmod) && !ev.Has(fsnotify.Write) {
				continue
			}
			if id, ok := wt.libraryFor(ev.Name); ok {
				wt.bump(id)
			}
		case err, ok := <-w.Errors:
			if !ok {
				return
			}
			slog.Warn("watcher error", "err", err)
		}
	}
}

func (wt *Watcher) libraryFor(p string) (int64, bool) {
	wt.mu.Lock()
	defer wt.mu.Unlock()
	for root, id := range wt.roots {
		if p == root || strings.HasPrefix(p, root+string(filepath.Separator)) {
			return id, true
		}
	}
	return 0, false
}

// bump (re)starts the library's quiet-period timer.
func (wt *Watcher) bump(id int64) {
	wt.mu.Lock()
	defer wt.mu.Unlock()
	if t, ok := wt.timers[id]; ok {
		t.Reset(Debounce)
		return
	}
	wt.timers[id] = time.AfterFunc(Debounce, func() {
		wt.mu.Lock()
		delete(wt.timers, id)
		wt.mu.Unlock()
		slog.Info("files changed; scanning library", "library", id)
		wt.Queue(id)
	})
}
