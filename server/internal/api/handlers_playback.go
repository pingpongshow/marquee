package api

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"time"

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
	set(&out.ASSSubtitles, p.AssSubtitles)
	set(&out.HLSSubtitles, p.HlsSubtitles)
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
	if st := u.Preferences.SubtitleStyle; st != nil {
		r.SubtitleStyle = playback.SubtitleStyle{Size: st.Size, Color: st.Color, Background: st.Background, Position: st.Position}
	}
	set(&r.FileID, b.FileId)
	set(&r.AudioStreamID, b.AudioStreamId)
	set(&r.SubtitleStreamID, b.SubtitleStreamId)
	set(&r.StartMS, b.StartMs)
	set(&r.Preload, b.Preload)
	r.SubtitleOffsetMS, r.AudioOffsetMS = b.SubtitleOffsetMs, b.AudioOffsetMs
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
		TrackGainDb: f32(s.TrackGainDB), AlbumGainDb: f32(s.AlbumGainDB), Peak: f32(s.Peak),
		SubtitleOffsetMs: ptr(s.SubtitleOffsetMS), AudioOffsetMs: ptr(s.AudioOffsetMS), Markers: []Marker{}}
	switch {
	case s.Decision.Method == playback.DirectPlay:
		out.Url, out.Protocol, out.ContentType = base+"file", File, ptr(playback.FileContentType(s))
	case s.Media.Video == nil:
		out.Url, out.Protocol, out.ContentType = base+"audio", Progressive, ptr("audio/aac")
	default:
		out.Url, out.Protocol, out.ContentType = base+"master.m3u8", Hls, ptr("application/vnd.apple.mpegurl")
	}
	switch {
	case s.Decision.SubtitleASS && s.SubtitleStreamID > 0:
		out.SubtitleUrl = ptr(fmt.Sprintf("%ssubtitles/%d.ass", base, s.SubtitleStreamID))
		out.SubtitleFormat = ptr(Ass)
		out.FontsUrl = ptr(base + "fonts.json")
	case s.Decision.SubtitleVTT && s.SubtitleStreamID > 0:
		out.SubtitleUrl = ptr(fmt.Sprintf("%ssubtitles/%d.vtt", base, s.SubtitleStreamID))
		out.SubtitleFormat = ptr(Vtt)
	}
	rows, err := h.DB.QueryContext(ctx, `SELECT kind, start_ms, end_ms FROM markers m WHERE file_id = ? AND kind IN ('intro', 'credits')
		  -- Markers from Plex, the file or an admin beat detected ones of the same kind.
		  AND NOT (source = 'detected' AND EXISTS (SELECT 1 FROM markers o WHERE o.file_id = m.file_id AND o.kind = m.kind AND o.source != 'detected'))
		ORDER BY start_ms`, s.FileID)
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
	s, ok := h.ownSession(ctx, req.SessionId)
	if !ok {
		return ReportPlayback404JSONResponse{NotFoundJSONResponse(apiErr("not_found", playback.ErrNoSession.Error()))}, nil
	}
	if req.Body.State == PlaybackProgressStateError {
		msg := ""
		set(&msg, req.Body.Error)
		slog.WarnContext(ctx, "client playback error", "title", s.Title, "method", s.Decision.Method, "device", s.DeviceName,
			"position", req.Body.PositionMs, "error", msg)
		h.recordPlaybackError(ctx, s.ItemID, s.FileID, msg) // ADM-11
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
		if s.Preloading() {
			continue
		}
		if sess.User.IsAdmin || s.UserID == sess.User.ID {
			info := toSessionInfo(s)
			if d, err := h.Items.Get(ctx, items.Unrestricted, s.ItemID, false); err == nil {
				switch {
				case d.Thumb > 0:
					info.ImageId = ptr(d.Thumb)
				case d.Backdrop > 0:
					info.ImageId = ptr(d.Backdrop)
				case d.Poster > 0:
					info.ImageId = ptr(d.Poster)
				}
				switch d.Type {
				case "episode":
					info.Subtitle = ptr(fmt.Sprintf("%s · E%d · %s", d.ParentTitle, d.Index, d.Title))
				case "track":
					info.Subtitle = ptr(d.ParentTitle)
				}
			}
			out = append(out, info)
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

// ---------- dashboard ----------

var serverStarted = time.Now()

func (h *Handlers) SystemStatus(ctx context.Context, _ SystemStatusRequestObject) (SystemStatusResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return SystemStatus401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return SystemStatus403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	cfg := h.Settings.Get()
	out := SystemStatus{Version: h.Version, StartedAt: serverStarted, Encoders: h.Playback.Encoders.Available(cfg.Transcoder.EncoderOrder),
		MaxTranscodes: cfg.Transcoder.MaxConcurrentTranscodes, UploadSpeedKbps: nz(cfg.RemoteAccess.UploadSpeedKbps)}
	u := h.Playback.Usage()
	out.ActiveStreams, out.ActiveTranscodes, out.LocalKbps, out.RemoteKbps = u.Streams, u.Transcodes, u.LocalKbps, u.RemoteKbps
	hist := []BandwidthSample{}
	for _, x := range append(h.Playback.History(), u) {
		hist = append(hist, BandwidthSample{At: x.At, LocalKbps: x.LocalKbps, RemoteKbps: x.RemoteKbps, Streams: x.Streams, Transcodes: x.Transcodes})
	}
	out.BandwidthHistory = &hist
	libs, err := h.Libraries.List(ctx)
	if err != nil {
		return nil, internal(ctx, "status", err)
	}
	for _, l := range libs {
		out.Libraries = append(out.Libraries, struct {
			Id    int64       `json:"id"`
			Items int         `json:"items"`
			Name  string      `json:"name"`
			Type  LibraryType `json:"type"`
		}{Id: l.ID, Items: l.ItemCount, Name: l.Name, Type: LibraryType(l.Type)})
	}
	return SystemStatus200JSONResponse(out), nil
}

func (h *Handlers) PlayHistory(ctx context.Context, req PlayHistoryRequestObject) (PlayHistoryResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return PlayHistory401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return PlayHistory403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	limit := 50
	if req.Params.Limit != nil && *req.Params.Limit > 0 && *req.Params.Limit <= 500 {
		limit = *req.Params.Limit
	}
	rows, err := h.DB.QueryContext(ctx, `SELECT h.id, COALESCE(u.display_name, ''), COALESCE(d.name, ''), COALESCE(h.item_id, 0), h.item_title,
		h.started_at, COALESCE(h.stopped_at, ''), COALESCE(h.position_ms, 0), COALESCE(h.decision, ''), COALESCE(h.network_class, ''),
		COALESCE(h.video_encoder, ''), COALESCE(h.bitrate_kbps, 0), h.source
		FROM play_history h LEFT JOIN users u ON u.id = h.user_id LEFT JOIN devices d ON d.id = h.device_id
		ORDER BY h.started_at DESC LIMIT ?`, limit)
	if err != nil {
		return nil, internal(ctx, "history", err)
	}
	defer rows.Close()
	out := PlayHistory200JSONResponse{}
	for rows.Next() {
		var e HistoryEntry
		var user, dev, started, stopped, method, nc, enc, src string
		var item, pos int64
		var kbps int
		if err := rows.Scan(&e.Id, &user, &dev, &item, &e.Title, &started, &stopped, &pos, &method, &nc, &enc, &kbps, &src); err != nil {
			return nil, internal(ctx, "history", err)
		}
		e.UserName, e.DeviceName, e.ItemId, e.PositionMs = nz(user), nz(dev), nz(item), nz(pos)
		e.Method, e.NetworkClass, e.Encoder, e.BitrateKbps = nz(method), nz(nc), nz(enc), nz(kbps)
		e.Source = HistoryEntrySource(src)
		e.StartedAt, _ = time.Parse(time.RFC3339Nano, started)
		if t, err := time.Parse(time.RFC3339Nano, stopped); err == nil {
			e.StoppedAt = &t
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func f32(v *float64) *float32 {
	if v == nil {
		return nil
	}
	f := float32(*v)
	return &f
}
