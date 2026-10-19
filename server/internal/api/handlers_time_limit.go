package api

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"marquee/internal/auth"
	"marquee/internal/timelimit"
	"marquee/internal/vod"
)

// Time limits for kids' profiles: a daily allowance of video, a different one at weekends,
// and viewing hours (UserRestrictions). Starting video past them is refused with 403
// time_limit; a stream playing when the time runs out is ended (RunTimeLimits), and its
// progress reports answer 403 time_limit so its player shows why. Music and trailers don't
// count and are never stopped.

// errCodeTimeLimit is the error code players look for.
const errCodeTimeLimit = "time_limit"

var timeLimitsMu sync.Mutex

// timeLimits is the watch-time tracker, made on first use when not set.
func (h *Handlers) timeLimits() *timelimit.Tracker {
	timeLimitsMu.Lock()
	defer timeLimitsMu.Unlock()
	if h.TimeLimits == nil {
		h.TimeLimits = &timelimit.Tracker{DB: h.DB}
	}
	return h.TimeLimits
}

// userLimits are a user's time limits; administrators have none.
func userLimits(u auth.User) timelimit.Limits {
	if u.IsAdmin {
		return timelimit.Limits{}
	}
	r := u.Restrictions
	return timelimit.Limits{Daily: r.DailyLimitMinutes, Weekend: r.WeekendLimitMinutes, From: r.AllowedFrom, Until: r.AllowedUntil}
}

// timeStatus is u's time limit status now.
func (h *Handlers) timeStatus(ctx context.Context, u auth.User) timelimit.Status {
	l := userLimits(u)
	t := h.timeLimits()
	now := t.Clock()
	if !l.Active() {
		return timelimit.Evaluate(l, 0, now)
	}
	return timelimit.Evaluate(l, t.UsedMS(ctx, u.ID, now), now)
}

// timeLimitRefusal is the error to refuse starting video with, or nil when u may watch now.
func (h *Handlers) timeLimitRefusal(ctx context.Context, u auth.User) *Error {
	if !userLimits(u).Active() {
		return nil
	}
	if st := h.timeStatus(ctx, u); !st.AllowedNow {
		return ptr(apiErr(errCodeTimeLimit, st.Reason))
	}
	return nil
}

// countsAsVideo reports whether an item's playback counts towards (and is held by) time
// limits: movies, episodes and home videos, but not music or trailers.
func countsAsVideo(itemType, extra string) bool {
	switch itemType {
	case "track", "album", "artist":
		return false
	}
	return extra != "trailer"
}

// itemCountsAsVideo looks the item up for countsAsVideo.
func (h *Handlers) itemCountsAsVideo(ctx context.Context, itemID int64) bool {
	var typ, extra string
	if err := h.DB.QueryRowContext(ctx, `SELECT type, COALESCE(extra_type, '') FROM items WHERE id = ?`, itemID).Scan(&typ, &extra); err != nil {
		return false
	}
	return countsAsVideo(typ, extra)
}

func toAPITimeLimit(st timelimit.Status) TimeLimitStatus {
	return TimeLimitStatus{Limited: st.Limited, AllowedNow: st.AllowedNow, MinutesLeft: st.MinutesLeft,
		LimitMinutes: st.LimitMinutes, UsedMinutes: st.UsedMinutes, AllowedFrom: nz(st.From), AllowedUntil: nz(st.Until),
		Reason: nz(st.Reason), ResetsAt: st.ResetsAt}
}

// MyTimeLimit is the caller's time limit today.
func (h *Handlers) MyTimeLimit(ctx context.Context, _ MyTimeLimitRequestObject) (MyTimeLimitResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return MyTimeLimit401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	return MyTimeLimit200JSONResponse(toAPITimeLimit(h.timeStatus(ctx, s.User))), nil
}

// timeLimitsFromAPI reads and checks the time limit fields of UserRestrictions.
func timeLimitsFromAPI(r *UserRestrictions, out *auth.Restrictions) error {
	l := timelimit.Limits{Daily: r.DailyLimitMinutes, Weekend: r.WeekendLimitMinutes}
	if r.AllowedFrom != nil {
		l.From = strings.TrimSpace(*r.AllowedFrom)
	}
	if r.AllowedUntil != nil {
		l.Until = strings.TrimSpace(*r.AllowedUntil)
	}
	if err := l.Validate(); err != nil {
		return err
	}
	if l.Daily != nil && *l.Daily > 0 {
		out.DailyLimitMinutes = ptr(*l.Daily)
	}
	if l.Weekend != nil {
		out.WeekendLimitMinutes = ptr(*l.Weekend) // 0 is kept: no limit at weekends
	}
	out.AllowedFrom, out.AllowedUntil = l.From, l.Until
	return nil
}

// timeLimitResponse is a 403 time_limit answer for endpoints whose spec lists no 403 (a
// session's progress report).
type timeLimitResponse struct{ Error }

func (r timeLimitResponse) visit(w http.ResponseWriter) error {
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(r.Error); err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusForbidden)
	_, err := buf.WriteTo(w)
	return err
}

func (r timeLimitResponse) VisitReportPlaybackResponse(w http.ResponseWriter) error {
	return r.visit(w)
}

func (r timeLimitResponse) VisitReportVodPlaybackResponse(w http.ResponseWriter) error {
	return r.visit(w)
}

// Stream keys for the tracker.
func playbackKey(id string) string      { return "pb:" + id }
func vodKey(id string) string           { return "vod:" + id }
func liveKey(id string, u int64) string { return "live:" + id + ":" + itoa(u) }

// watchStream is a video stream that counts towards time limits, and how to end it.
type watchStream struct {
	timelimit.Activity
	stop func(reason string)
}

// playingWindow is how recently a stream must have been fetched or reported to count as
// playing.
const playingWindow = 45 * time.Second

// watching lists the video streams now open (music, trailers and admins' streams aren't).
func (h *Handlers) watching(ctx context.Context, admins map[int64]bool) []watchStream {
	var out []watchStream
	if h.Playback != nil {
		for _, s := range h.Playback.List() {
			if admins[s.UserID] || s.Media.Video == nil || !countsAsVideo(s.ItemType, s.Extra) || s.Preloading() {
				continue
			}
			snap := s.Snapshot()
			id := s.ID
			out = append(out, watchStream{
				Activity: timelimit.Activity{Key: playbackKey(id), UserID: s.UserID, PositionMS: snap.PositionMS,
					Playing: snap.State == "playing" && time.Since(snap.LastActive) <= playingWindow},
				stop: func(reason string) {
					h.Playback.Stop(ctx, id)
					if h.VOD != nil { // a film on demand made into HLS here: its own session too
						for _, st := range h.VOD.Streams.List() {
							if st.PlaybackID == id {
								h.timeLimits().MarkStopped(ctx, vodKey(st.ID), reason)
								h.VOD.Streams.Stop(st.ID)
							}
						}
					}
				},
			})
		}
	}
	if h.VOD != nil {
		for _, st := range h.VOD.Streams.List() {
			if st.PlaybackID != "" || admins[st.UserID] {
				continue // counted as its playback session
			}
			id := st.ID
			out = append(out, watchStream{
				Activity: timelimit.Activity{Key: vodKey(id), UserID: st.UserID, PositionMS: -1, Playing: st.ActiveWithin(playingWindow)},
				stop:     func(string) { h.VOD.Streams.Stop(id) },
			})
		}
	}
	if h.LiveTV != nil && h.LiveTV.Sessions != nil {
		for _, ls := range h.LiveTV.Sessions.List() {
			users, last := ls.Viewers()
			for _, u := range users {
				if admins[u] {
					continue
				}
				id, uid, all := ls.ID, u, len(users)
				out = append(out, watchStream{
					Activity: timelimit.Activity{Key: liveKey(id, uid), UserID: uid, PositionMS: -1, Playing: time.Since(last) <= playingWindow},
					stop: func(reason string) {
						if all == 1 {
							h.LiveTV.Sessions.StopWithReason(id, "Stopped: "+reason)
						} else {
							h.LiveTV.Sessions.Release(id, uid, false) // others keep watching
						}
					},
				})
			}
		}
	}
	return out
}

// sweepTimeLimits counts the minutes of the streams playing and ends the streams of anyone
// past their limits or outside their viewing hours.
func (h *Handlers) sweepTimeLimits(ctx context.Context, t *timelimit.Tracker, streams []watchStream) {
	acts := make([]timelimit.Activity, len(streams))
	for i, s := range streams {
		acts[i] = s.Activity
	}
	t.Sweep(ctx, acts, t.Clock())
	type verdict struct {
		allowed bool
		reason  string
	}
	verdicts := map[int64]verdict{}
	for _, s := range streams {
		v, ok := verdicts[s.UserID]
		if !ok {
			v.allowed = true
			if u, err := h.Auth.GetUser(ctx, s.UserID); err == nil && userLimits(u).Active() {
				st := h.timeStatus(ctx, u)
				v = verdict{st.AllowedNow, st.Reason}
				if !st.AllowedNow {
					slog.Info("time limit: ending streams", "user", u.DisplayName, "reason", st.Reason)
				}
			}
			verdicts[s.UserID] = v
		}
		if !v.allowed {
			t.MarkStopped(ctx, s.Key, v.reason)
			s.stop(v.reason)
		}
	}
}

// adminIDs are the administrators (never limited, so their streams aren't counted).
func (h *Handlers) adminIDs(ctx context.Context) map[int64]bool {
	out := map[int64]bool{}
	if users, err := h.Auth.ListUsers(ctx); err == nil {
		for _, u := range users {
			if u.IsAdmin {
				out[u.ID] = true
			}
		}
	}
	return out
}

// RunTimeLimits counts watching time and enforces time limits until ctx ends.
func (h *Handlers) RunTimeLimits(ctx context.Context) {
	t := h.timeLimits()
	tick := time.NewTicker(10 * time.Second)
	defer tick.Stop()
	pruned := ""
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		h.sweepTimeLimits(ctx, t, h.watching(ctx, h.adminIDs(ctx)))
		if now := t.Clock(); timelimit.Day(now) != pruned {
			pruned = timelimit.Day(now)
			t.Prune(ctx, now, 60*24*time.Hour)
		}
	}
}

// timeLimitOnReport answers a progress report for a stream a time limit ended (or one
// running when it should end now): the stream is stopped and the reason returned.
func (h *Handlers) timeLimitOnReport(ctx context.Context, u auth.User, key string, video bool, stop func()) *Error {
	if reason := h.timeLimits().StoppedReason(key); reason != "" {
		return ptr(apiErr(errCodeTimeLimit, reason))
	}
	if !video {
		return nil
	}
	if e := h.timeLimitRefusal(ctx, u); e != nil {
		h.timeLimits().MarkStopped(ctx, key, e.Message)
		if stop != nil {
			stop()
		}
		return e
	}
	return nil
}

// stopVodStream ends a film or episode on demand: its own session and, for one made into
// HLS here, the playback session.
func (h *Handlers) stopVodStream(ctx context.Context, st *vod.Stream) {
	if st.PlaybackID != "" && h.Playback != nil {
		h.Playback.Stop(ctx, st.PlaybackID)
	}
	h.VOD.Streams.Stop(st.ID)
}
