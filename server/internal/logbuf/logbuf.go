// Package logbuf keeps the most recent log records in memory for the admin log viewer
// (ADM-6), alongside normal output to stderr.
package logbuf

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

type Entry struct {
	Time    time.Time
	Level   slog.Level
	Message string
	Attrs   map[string]string
}

// Buffer is a fixed-size ring of log entries.
type Buffer struct {
	mu      sync.Mutex
	entries []Entry
	next    int
	full    bool
}

func New(size int) *Buffer { return &Buffer{entries: make([]Entry, size)} }

func (b *Buffer) add(e Entry) {
	b.mu.Lock()
	b.entries[b.next] = e
	b.next = (b.next + 1) % len(b.entries)
	if b.next == 0 {
		b.full = true
	}
	b.mu.Unlock()
}

// Recent returns up to limit entries at or above min, oldest first.
func (b *Buffer) Recent(min slog.Level, limit int) []Entry {
	b.mu.Lock()
	defer b.mu.Unlock()
	var ordered []Entry
	if b.full {
		ordered = append(append(ordered, b.entries[b.next:]...), b.entries[:b.next]...)
	} else {
		ordered = append(ordered, b.entries[:b.next]...)
	}
	out := make([]Entry, 0, limit)
	for i := len(ordered) - 1; i >= 0 && len(out) < limit; i-- {
		if ordered[i].Level >= min {
			out = append(out, ordered[i])
		}
	}
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out
}

// Handler tees records into the buffer and forwards them to next.
type Handler struct {
	next  slog.Handler
	buf   *Buffer
	attrs []slog.Attr
}

func NewHandler(next slog.Handler, buf *Buffer) *Handler { return &Handler{next: next, buf: buf} }

// Enabled captures debug records too so the viewer can show them; forwarding still
// respects next's level.
func (h *Handler) Enabled(ctx context.Context, l slog.Level) bool { return l >= slog.LevelDebug }

func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	e := Entry{Time: r.Time, Level: r.Level, Message: r.Message, Attrs: map[string]string{}}
	for _, a := range h.attrs {
		e.Attrs[a.Key] = a.Value.String()
	}
	r.Attrs(func(a slog.Attr) bool {
		e.Attrs[a.Key] = a.Value.String()
		return true
	})
	h.buf.add(e)
	if h.next.Enabled(ctx, r.Level) {
		return h.next.Handle(ctx, r)
	}
	return nil
}

func (h *Handler) WithAttrs(as []slog.Attr) slog.Handler {
	return &Handler{next: h.next.WithAttrs(as), buf: h.buf, attrs: append(append([]slog.Attr{}, h.attrs...), as...)}
}

func (h *Handler) WithGroup(name string) slog.Handler {
	return &Handler{next: h.next.WithGroup(name), buf: h.buf, attrs: h.attrs}
}
