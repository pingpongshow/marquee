package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"

	"marquee/internal/items"
	"marquee/internal/netclass"
	"marquee/internal/playback"
)

func toProfile(p DeviceProfile) playback.DeviceProfile {
	out := playback.DeviceProfile{Containers: p.Containers, VideoCodecs: p.VideoCodecs, AudioCodecs: p.AudioCodecs,
		HLS: p.Hls, HLSVideoCodecs: p.HlsVideoCodecs, HLSAudioCodecs: p.HlsAudioCodecs}
	set(&out.MaxAudioChannels, p.MaxAudioChannels)
	set(&out.MaxHeight, p.MaxHeight)
	set(&out.TenBit, p.TenBit)
	set(&out.TextSubtitles, p.TextSubtitles)
	if p.Hdr != nil {
		for _, h := range *p.Hdr {
			out.HDR = append(out.HDR, string(h))
		}
	}
	return out
}

func toAPIDecision(d playback.Decision) PlaybackDecision {
	return PlaybackDecision{Method: PlaybackDecisionMethod(d.Method), Summary: d.String(), Reasons: orEmpty(d.Reasons),
		VideoCopy: d.VideoCopy, VideoCodec: nz(d.VideoCodec), Height: nz(d.Height), VideoKbps: nz(d.VideoKbps),
		AudioCopy: d.AudioCopy, AudioCodec: nz(d.AudioCodec), AudioChannels: nz(d.AudioChannels),
		BurnSubtitle: d.BurnSubtitle, ToneMap: d.ToneMap}
}

func (h *Handlers) StartPlayback(ctx context.Context, req StartPlaybackRequestObject) (StartPlaybackResponseObject, error) {
	sess, ok := session(ctx)
	if !ok {
		return StartPlayback401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	b := req.Body
	if err := h.Items.Visible(ctx, access(ctx), b.ItemId); errors.Is(err, items.ErrNotFound) {
		return StartPlayback404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "item not found"))}, nil
	} else if err != nil {
		return nil, internal(ctx, "startPlayback", err)
	}
	ri := requestInfo(ctx)
	u := sess.User
	devName := ""
	for _, d := range must(h.Auth.ListDevices(ctx, u.ID)) {
		if d.ID == sess.DeviceID {
			devName = d.Name
		}
	}
	r := playback.Request{
		UserID: u.ID, UserName: u.DisplayName, DeviceID: sess.DeviceID, DeviceName: devName, ItemID: b.ItemId,
		StartMS: -1, Profile: toProfile(b.Profile), Remote: ri.Class == netclass.Remote, ClientIP: ri.ClientIP,
		RemoteAllowed: u.IsAdmin || u.Restrictions.RemoteAllowed(),
		UserAudioLang: u.Preferences.AudioLanguage, UserSubLang: u.Preferences.SubtitleLanguage, UserSubMode: u.Preferences.SubtitleMode,
	}
	set(&r.FileID, b.FileId)
	set(&r.AudioStreamID, b.AudioStreamId)
	set(&r.SubtitleStreamID, b.SubtitleStreamId)
	set(&r.StartMS, b.StartMs)
	set(&r.Quality.RequestedKbps, b.MaxBitrateKbps)
	set(&r.Quality.MeasuredKbps, b.MeasuredKbps)
	if r.Remote {
		r.Quality.UserPrefKbps = u.Preferences.RemoteQualityKbps
		r.Quality.UserCapKbps = u.Restrictions.RemoteQualityKbps
	} else {
		r.Quality.UserPrefKbps = u.Preferences.LocalQualityKbps
	}

	s, err := h.Playback.Start(ctx, r)
	switch {
	case errors.Is(err, playback.ErrNoMedia), errors.Is(err, playback.ErrUnavailable):
		return StartPlayback404JSONResponse{NotFoundJSONResponse(apiErr("unavailable", err.Error()))}, nil
	case errors.Is(err, playback.ErrRemoteDisabled):
		return StartPlayback403JSONResponse{ForbiddenJSONResponse(apiErr("remote_disabled", err.Error()))}, nil
	case errors.Is(err, playback.ErrBusy):
		return StartPlayback503JSONResponse(apiErr("busy", err.Error())), nil
	case err != nil:
		return nil, internal(ctx, "startPlayback", err)
	}

	base := playback.StreamPrefix + s.ID + "/"
	out := PlaybackSession{Id: s.ID, ItemId: s.ItemID, FileId: ptr(s.FileID), StartMs: s.StartMS, DurationMs: s.Media.DurationMS,
		Decision: toAPIDecision(s.Decision), LimitKbps: nz(s.LimitKbps), LimitReason: nz(s.LimitReason),
		NetworkClass: NetworkClass(ri.Class), AudioStreamId: nz(s.AudioStreamID), SubtitleStreamId: nz(s.SubtitleStreamID),
		Markers: []Marker{}}
	switch {
	case s.Decision.Method == playback.DirectPlay:
		out.Url, out.Protocol = base+"file", File
	case s.Media.Video == nil:
		out.Url, out.Protocol = base+"audio", Progressive
	default:
		out.Url, out.Protocol = base+"master.m3u8", Hls
	}
	if s.Decision.SubtitleVTT && s.SubtitleStreamID > 0 {
		out.SubtitleUrl = ptr(fmt.Sprintf("%ssubtitles/%d.vtt", base, s.SubtitleStreamID))
	}
	rows, err := h.DB.QueryContext(ctx, `SELECT kind, start_ms, end_ms FROM markers WHERE file_id = ? AND kind IN ('intro', 'credits') ORDER BY start_ms`, s.FileID)
	if err == nil {
		for rows.Next() {
			var mk Marker
			var kind string
			rows.Scan(&kind, &mk.StartMs, &mk.EndMs)
			mk.Kind = MarkerKind(kind)
			out.Markers = append(out.Markers, mk)
		}
		rows.Close()
	}
	return StartPlayback200JSONResponse(out), nil
}

func must[T any](v T, _ error) T { return v }

func (h *Handlers) ownSession(ctx context.Context, id string) (*playback.Session, bool) {
	sess, ok := session(ctx)
	if !ok {
		return nil, false
	}
	s, found := h.Playback.Get(id)
	if !found || (s.UserID != sess.User.ID && !sess.User.IsAdmin) {
		return nil, false
	}
	return s, true
}

func (h *Handlers) ReportPlayback(ctx context.Context, req ReportPlaybackRequestObject) (ReportPlaybackResponseObject, error) {
	if _, ok := session(ctx); !ok {
		return ReportPlayback401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	if _, ok := h.ownSession(ctx, req.SessionId); !ok {
		return ReportPlayback404JSONResponse{NotFoundJSONResponse(apiErr("not_found", playback.ErrNoSession.Error()))}, nil
	}
	if req.Body.State == PlaybackProgressStateError {
		s, _ := h.Playback.Get(req.SessionId)
		msg := ""
		set(&msg, req.Body.Error)
		slog.WarnContext(ctx, "client playback error", "title", s.Title, "method", s.Decision.Method, "device", s.DeviceName,
			"position", req.Body.PositionMs, "error", msg)
		return ReportPlayback204Response{}, nil
	}
	if err := h.Playback.Progress(ctx, req.SessionId, req.Body.PositionMs, string(req.Body.State)); err != nil {
		return nil, internal(ctx, "progress", err)
	}
	return ReportPlayback204Response{}, nil
}

func (h *Handlers) StopPlayback(ctx context.Context, req StopPlaybackRequestObject) (StopPlaybackResponseObject, error) {
	if _, ok := session(ctx); !ok {
		return StopPlayback401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	if _, ok := h.ownSession(ctx, req.SessionId); !ok {
		return StopPlayback404JSONResponse{NotFoundJSONResponse(apiErr("not_found", playback.ErrNoSession.Error()))}, nil
	}
	h.Playback.Stop(ctx, req.SessionId)
	return StopPlayback204Response{}, nil
}

func toSessionInfo(s *playback.Session) PlaybackSessionInfo {
	snap := s.Snapshot()
	nc := NetworkClass(netclass.Local)
	if s.Remote {
		nc = NetworkClass(netclass.Remote)
	}
	return PlaybackSessionInfo{Id: s.ID, UserId: s.UserID, UserName: s.UserName, DeviceName: s.DeviceName, ClientIp: nz(s.ClientIP),
		ItemId: s.ItemID, ItemType: ItemType(s.ItemType), Title: s.Title, Method: PlaybackSessionInfoMethod(s.Decision.Method),
		Summary: s.Decision.String(), Reasons: ptr(orEmpty(s.Decision.Reasons)), Encoder: nz(snap.Encoder), NetworkClass: nc,
		PositionMs: snap.PositionMS, DurationMs: s.Media.DurationMS, State: snap.State, BitrateKbps: s.BitrateKbps(), StartedAt: s.StartedAt}
}

func (h *Handlers) ListPlaybackSessions(ctx context.Context, _ ListPlaybackSessionsRequestObject) (ListPlaybackSessionsResponseObject, error) {
	sess, ok := session(ctx)
	if !ok {
		return ListPlaybackSessions401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	out := ListPlaybackSessions200JSONResponse{}
	for _, s := range h.Playback.List() {
		if sess.User.IsAdmin || s.UserID == sess.User.ID {
			out = append(out, toSessionInfo(s))
		}
	}
	return out, nil
}

func (h *Handlers) BandwidthTest(ctx context.Context, req BandwidthTestRequestObject) (BandwidthTestResponseObject, error) {
	if _, ok := session(ctx); !ok {
		return BandwidthTest401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	kb := 1024
	if req.Params.Kb != nil && *req.Params.Kb >= 16 && *req.Params.Kb <= 8192 {
		kb = *req.Params.Kb
	}
	buf := make([]byte, kb*1024)
	rand.Read(buf) // incompressible, so proxies can't shrink it
	return BandwidthTest200ApplicationoctetStreamResponse{Body: bytes.NewReader(buf), ContentLength: int64(len(buf))}, nil
}

func (h *Handlers) MarkWatched(ctx context.Context, req MarkWatchedRequestObject) (MarkWatchedResponseObject, error) {
	sess, ok := session(ctx)
	if !ok {
		return MarkWatched401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	if err := h.Items.Visible(ctx, access(ctx), req.ItemId); err != nil {
		return MarkWatched404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "item not found"))}, nil
	}
	if err := h.Items.SetWatched(ctx, sess.User.ID, req.ItemId, true); err != nil {
		return nil, internal(ctx, "markWatched", err)
	}
	return MarkWatched204Response{}, nil
}

func (h *Handlers) MarkUnwatched(ctx context.Context, req MarkUnwatchedRequestObject) (MarkUnwatchedResponseObject, error) {
	sess, ok := session(ctx)
	if !ok {
		return MarkUnwatched401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	if err := h.Items.Visible(ctx, access(ctx), req.ItemId); err != nil {
		return MarkUnwatched404JSONResponse{NotFoundJSONResponse(apiErr("not_found", "item not found"))}, nil
	}
	if err := h.Items.SetWatched(ctx, sess.User.ID, req.ItemId, false); err != nil {
		return nil, internal(ctx, "markUnwatched", err)
	}
	return MarkUnwatched204Response{}, nil
}

func (h *Handlers) NextItem(ctx context.Context, req NextItemRequestObject) (NextItemResponseObject, error) {
	if _, ok := session(ctx); !ok {
		return NextItem401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	next, err := h.Items.Next(ctx, access(ctx), req.ItemId)
	if errors.Is(err, items.ErrNotFound) {
		return NextItem204Response{}, nil
	}
	if err != nil {
		return nil, internal(ctx, "next", err)
	}
	return NextItem200JSONResponse(toAPISummary(next)), nil
}

func (h *Handlers) HomeHubs(ctx context.Context, _ HomeHubsRequestObject) (HomeHubsResponseObject, error) {
	if _, ok := session(ctx); !ok {
		return HomeHubs401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	acc := access(ctx)
	libs, err := h.Libraries.List(ctx)
	if err != nil {
		return nil, internal(ctx, "hubs", err)
	}
	var home []int64
	for _, l := range libs {
		if canSeeLibrary(ctx, l.ID) && (l.Options.IncludeInHome == nil || *l.Options.IncludeInHome) {
			home = append(home, l.ID)
		}
	}
	out := HomeHubs200JSONResponse{}
	add := func(id, title string, lib int64, list []items.Summary) {
		if len(list) == 0 {
			return
		}
		hub := Hub{Id: id, Title: title, LibraryId: nz(lib), Items: make([]ItemSummary, len(list))}
		for i, it := range list {
			hub.Items[i] = toAPISummary(it)
		}
		out = append(out, hub)
	}
	cw, err := h.Items.ContinueWatching(ctx, acc, home, 20)
	if err != nil {
		return nil, internal(ctx, "hubs", err)
	}
	add("continue-watching", "Continue Watching", 0, cw)
	for _, l := range libs {
		if !canSeeLibrary(ctx, l.ID) || (l.Options.IncludeInHome != nil && !*l.Options.IncludeInHome) {
			continue
		}
		list, err := h.Items.RecentlyAdded(ctx, acc, l.ID, string(l.Type), 20)
		if err != nil {
			return nil, internal(ctx, "hubs", err)
		}
		add(fmt.Sprintf("recent-%d", l.ID), "Recently Added "+l.Name, l.ID, list)
	}
	return out, nil
}
