package api

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"marquee/internal/netclass"
)

// Prometheus metrics (ADM-7, D80), written by hand in the text format: a handful of gauges
// doesn't need a client library.

func (h *Handlers) GetMetrics(ctx context.Context, _ GetMetricsRequestObject) (GetMetricsResponseObject, error) {
	if requestInfo(ctx).Class == netclass.Remote {
		if _, admin := isAdmin(ctx); !admin {
			return GetMetrics403JSONResponse{ForbiddenJSONResponse(apiErr("forbidden", "metrics are available on the home network, or to admins"))}, nil
		}
	}
	var b strings.Builder
	gauge := func(name, help string) { fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s gauge\n", name, help, name) }
	label := func(v string) string { return strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", " ").Replace(v) }

	gauge("marquee_build_info", "Marquee's version.")
	fmt.Fprintf(&b, "marquee_build_info{version=%q} 1\n", label(h.Version))

	// Playback sessions by kind, method, encoder and network.
	type key struct{ kind, method, encoder, network string }
	counts := map[key]int{}
	if h.Playback != nil {
		for _, s := range h.Playback.List() {
			kind := "video"
			if s.ItemType == "track" {
				kind = "music"
			}
			network := "local"
			if s.Remote {
				network = "remote"
			}
			counts[key{kind, string(s.Decision.Method), s.Snapshot().Encoder, network}]++
		}
	}
	gauge("marquee_playback_sessions", "Playback sessions now, by kind, method (direct_play, direct_stream, transcode), encoder and network.")
	keys := make([]key, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return fmt.Sprint(keys[i]) < fmt.Sprint(keys[j]) })
	for _, k := range keys {
		fmt.Fprintf(&b, "marquee_playback_sessions{kind=%q,method=%q,encoder=%q,network=%q} %d\n", k.kind, k.method, label(k.encoder), k.network, counts[k])
	}

	if h.LiveTV != nil && h.LiveTV.Sessions != nil {
		gauge("marquee_livetv_sessions", "Live TV streams now.")
		fmt.Fprintf(&b, "marquee_livetv_sessions %d\n", h.LiveTV.Sessions.Count())
	}
	if h.DVR != nil {
		gauge("marquee_recordings_active", "Live TV recordings in progress.")
		fmt.Fprintf(&b, "marquee_recordings_active %d\n", h.DVR.Active())
		var n int
		h.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM dvr_recordings WHERE status = 'scheduled'`).Scan(&n)
		gauge("marquee_recordings_scheduled", "Upcoming Live TV recordings.")
		fmt.Fprintf(&b, "marquee_recordings_scheduled %d\n", n)
	}
	if h.SyncPlay != nil {
		gauge("marquee_watch_groups", "Watch-together groups now.")
		fmt.Fprintf(&b, "marquee_watch_groups %d\n", len(h.SyncPlay.List()))
	}

	if libs, err := h.Libraries.List(ctx); err == nil {
		gauge("marquee_library_items", "Top-level items per library (movies, shows, artists…).")
		for _, l := range libs {
			fmt.Fprintf(&b, "marquee_library_items{library=%q,type=%q} %d\n", label(l.Name), l.Type, l.ItemCount)
		}
	}
	var users, pending int
	h.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&users)
	gauge("marquee_users", "Accounts and profiles.")
	fmt.Fprintf(&b, "marquee_users %d\n", users)
	if h.Requests != nil {
		pending = h.Requests.PendingCount(ctx)
		gauge("marquee_requests_pending", "Seerr requests waiting for an admin.")
		fmt.Fprintf(&b, "marquee_requests_pending %d\n", pending)
	}
	return GetMetrics200TextResponse(b.String()), nil
}
