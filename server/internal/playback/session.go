package playback

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"marquee/internal/settings"
)

var (
	ErrNoMedia        = errors.New("this item has no playable file")
	ErrUnavailable    = errors.New("the file for this item is unavailable")
	ErrBusy           = errors.New("the server is already running its maximum number of transcodes")
	ErrRemoteDisabled = errors.New("your account isn't allowed to stream away from home")
	ErrNoSession      = errors.New("playback session not found")
)

// Request starts a session.
type Request struct {
	UserID, DeviceID int64
	UserName         string
	DeviceName       string
	ItemID           int64
	FileID           int64 // 0 = best file
	Preload          bool  // gapless: don't end the device's current session yet
	AudioStreamID    int64 // 0 = automatic
	SubtitleStreamID int64 // 0 = automatic (forced/preferred), -1 = off
	StartMS          int64 // -1 = resume position
	Profile          DeviceProfile
	Remote           bool
	ClientIP         string
	Quality          QualityInputs
	UserAudioLang    string
	UserSubLang      string
	UserSubMode      string // off, forced, foreign, always
	RemoteAllowed    bool
}

// Session is an active playback.
type Session struct {
	Plan []float64 // HLS segment start times (seconds)
	// Volume levelling for music (MUSIC-9), in dB; nil when unknown.
	TrackGainDB, AlbumGainDB, Peak *float64
	// TextSubs are the file's text subtitles, offered as WebVTT renditions to clients that
	// need subtitles inside HLS (HLSSubs).
	TextSubs         []SubtitleRendition
	HLSSubs          bool
	ID               string
	UserID           int64
	UserName         string
	DeviceID         int64
	DeviceName       string
	ItemID           int64
	ItemType         string
	Title            string
	FileID           int64
	Path             string
	Media            Media
	AudioStreamID    int64
	SubtitleStreamID int64
	SubtitleCodec    string
	Decision         Decision
	LimitKbps        int
	LimitReason      string
	StartMS          int64
	Remote           bool
	ClientIP         string
	StartedAt        time.Time

	mu         sync.Mutex
	lastActive time.Time
	positionMS int64
	state      string
	watched    bool
	started    bool // has reported playing (for webhooks)
	preload    bool // prepared for gapless playback; not yet playing
	historyID  int64
	transcoder *Transcoder
	rungs      []Rung // lower ABR variants of a remote transcode
}

// Snapshot is a copy of a session's live state.
type Snapshot struct {
	PositionMS int64
	State      string
	LastActive time.Time
	Encoder    string
	Throttled  bool
}

func (s *Session) Snapshot() Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	snap := Snapshot{PositionMS: s.positionMS, State: s.state, LastActive: s.lastActive}
	if s.transcoder != nil {
		snap.Encoder = s.transcoder.Encoder()
	}
	return snap
}

func (s *Session) touch() {
	s.mu.Lock()
	s.lastActive = time.Now()
	s.mu.Unlock()
}

// Transcoder returns the session's transcoder, or nil for direct play.
func (s *Session) Transcoder() *Transcoder { return s.transcoder }

// Preloading reports whether the session is a queued gapless track that hasn't started.
func (s *Session) Preloading() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.preload
}

// BitrateKbps is what the stream uses (for bandwidth sharing).
func (s *Session) BitrateKbps() int {
	if s.Decision.Method == Transcode {
		return s.Decision.VideoKbps + audioKbps(s.Decision.AudioChannels)
	}
	return s.Media.BitrateKbps
}

type Manager struct {
	DB           *sql.DB
	Settings     *settings.Store
	FFmpeg       string
	TranscodeDir string
	Encoders     Encoders
	FFprobe      string
	// IFrames, when set, returns a file's scrubbing-thumbnail stream (an all-keyframe MP4
	// and its HLS I-frame playlist) for Apple players.
	IFrames func(fileID, durationMS int64) (file string, playlist []byte, ok bool)
	// Events, when set, hears playback.started/paused/resumed/watched/stopped (webhooks).
	Events func(kind string, s *Session, positionMS int64)

	mu       sync.Mutex
	sessions map[string]*Session
	history  []Sample // last hour, one a minute
}

// Sample is one minute of the dashboard's bandwidth graph.
type Sample struct {
	At                    time.Time
	LocalKbps, RemoteKbps int
	Streams, Transcodes   int
}

// Usage sums the current streams.
func (m *Manager) Usage() Sample {
	u := Sample{At: time.Now()}
	for _, s := range m.List() {
		if s.Preloading() {
			continue
		}
		u.Streams++
		if s.Transcoder() != nil {
			u.Transcodes++
		}
		if s.Remote {
			u.RemoteKbps += s.BitrateKbps()
		} else {
			u.LocalKbps += s.BitrateKbps()
		}
	}
	return u
}

// History returns the last hour of usage samples, oldest first.
func (m *Manager) History() []Sample {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Sample(nil), m.history...)
}

func (m *Manager) record() {
	u := m.Usage()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.history = append(m.history, u)
	if len(m.history) > 60 {
		m.history = m.history[len(m.history)-60:]
	}
}

func (m *Manager) init() {
	if m.sessions == nil {
		m.sessions = map[string]*Session{}
	}
}

func newID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// Get returns an active session.
func (m *Manager) Get(id string) (*Session, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.init()
	s, ok := m.sessions[id]
	if ok {
		s.touch()
	}
	return s, ok
}

// List returns active sessions, newest first.
func (m *Manager) List() []*Session {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	return out
}

func (m *Manager) remoteUsage(excluding string) (kbps, count int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, s := range m.sessions {
		if s.Remote && id != excluding {
			kbps += s.BitrateKbps()
			count++
		}
	}
	return
}

func (m *Manager) transcodeCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, s := range m.sessions {
		if s.transcoder != nil {
			n++
		}
	}
	return n
}

// Start creates a session: picks the file and tracks, decides how to play it, and prepares
// a transcoder if needed. A previous session from the same device is stopped.
func (m *Manager) Start(ctx context.Context, r Request) (*Session, error) {
	if r.Remote && !r.RemoteAllowed {
		return nil, ErrRemoteDisabled
	}
	cfg := m.Settings.Get()
	s := &Session{ID: newID(), UserID: r.UserID, UserName: r.UserName, DeviceID: r.DeviceID, DeviceName: r.DeviceName,
		ItemID: r.ItemID, Remote: r.Remote, ClientIP: r.ClientIP, StartedAt: time.Now(), lastActive: time.Now(), state: "playing"}
	if err := m.load(ctx, s, r); err != nil {
		return nil, err
	}

	// Stop this device's previous session (switching tracks or quality starts a new one).
	// A preloaded session waits until it starts playing.
	if r.Preload {
		s.preload = true
	} else {
		m.stopOthers(ctx, s)
	}

	q := r.Quality
	q.Remote = r.Remote
	q.OtherRemoteKbps, q.OtherRemoteCount = m.remoteUsage("")
	s.LimitKbps, s.LimitReason = MaxKbps(q, cfg.RemoteAccess)
	limits := Limits{MaxKbps: s.LimitKbps, Remote: r.Remote, PreferHEVC: r.Remote && cfg.Transcoder.PreferHEVCRemote}
	s.Decision = Decide(s.Media, r.Profile, limits)
	// Copied video is cut on its own keyframes; without an index of them, transcode instead.
	var plan []float64
	if s.Decision.Method == DirectStream && s.Media.Video != nil {
		kf, err := m.keyframes(ctx, s.FileID, s.Path, s.Media.Container, 15*time.Second)
		if err == nil {
			plan = KeyframePlan(kf, float64(s.Media.DurationMS)/1000, SegmentSeconds)
		} else {
			slog.Warn("no keyframe index; transcoding video", "file", s.Path, "err", err)
			limits.NoVideoCopy = true
			s.Decision = Decide(s.Media, r.Profile, limits)
		}
	}
	if plan == nil {
		plan = FixedPlan(s.Media.DurationMS)
	}
	s.Plan = plan
	if s.Decision.ToneMap && !cfg.Transcoder.ToneMapping {
		s.Decision.ToneMap = false
	}

	// Music that needs converting streams progressively from /audio, without HLS.
	if s.Decision.Method != DirectPlay && s.Media.Video != nil {
		if m.transcodeCount() >= cfg.Transcoder.MaxConcurrentTranscodes {
			return nil, ErrBusy
		}
		job := Job{Input: s.Path, Decision: s.Decision, VideoIndex: -1, AudioIndex: -1, SubIndex: -1, SubRelIndex: -1,
			Dir: filepath.Join(m.TranscodeDir, s.ID), Preset: cfg.Transcoder.Preset, QSVDevice: m.Encoders.QSVDevice, Plan: plan}
		if v := s.Media.Video; v != nil {
			job.VideoIndex, job.VideoCodec = v.Index, v.Codec
		}
		if a := s.Media.Audio; a != nil {
			job.AudioIndex = a.Index
		}
		if sub := s.Media.Subtitle; sub != nil && s.Decision.BurnSubtitle {
			job.SubIndex, job.SubExternal, job.SubImage = sub.Index, sub.External, sub.IsImage()
			job.SubRelIndex = m.subtitleRelIndex(ctx, s.FileID, sub.Index)
		}
		s.transcoder = &Transcoder{FFmpeg: m.FFmpeg, Job: job, Encoders: m.encodersFor(cfg.Transcoder.EncoderOrder),
			TotalSegments: len(plan), ThrottleAhead: cfg.Transcoder.ThrottleSegmentsAhead}
		if s.Decision.VideoCopy {
			s.transcoder.Encoders = []string{"software"} // copying video needs no encoder
		}
		s.transcoder.lastRequest = segmentFor(plan, float64(s.StartMS)/1000)
		if s.Remote && s.Decision.Method == Transcode && s.Media.Video != nil {
			s.rungs = ladderFor(s.Decision, s.Media.Video.Height, cfg.Transcoder.RemoteLadder)
		}
	}

	res, err := m.DB.ExecContext(ctx, `INSERT INTO play_history(user_id, item_id, device_id, item_title, started_at, position_ms,
		network_class, decision, bitrate_kbps) VALUES (?, ?, ?, ?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), ?, ?, ?, ?)`,
		s.UserID, s.ItemID, s.DeviceID, s.Title, s.StartMS, map[bool]string{true: "remote", false: "local"}[s.Remote],
		string(s.Decision.Method), s.BitrateKbps())
	if err == nil {
		s.historyID, _ = res.LastInsertId()
	}

	m.mu.Lock()
	m.init()
	m.sessions[s.ID] = s
	m.mu.Unlock()
	slog.Info("playback started", "user", s.UserName, "title", s.Title, "method", s.Decision.Method,
		"remote", s.Remote, "limitKbps", s.LimitKbps, "reasons", strings.Join(s.Decision.Reasons, "; "))
	return s, nil
}

// load fills the session's file, media and track choices from the database.
func (m *Manager) load(ctx context.Context, s *Session, r Request) error {
	var title, typ string
	var parent, grand sql.NullString
	var grandID sql.NullInt64
	err := m.DB.QueryRowContext(ctx, `SELECT i.title, i.type, p.title, g.title, i.grandparent_id FROM items i
		LEFT JOIN items p ON p.id = i.parent_id LEFT JOIN items g ON g.id = i.grandparent_id WHERE i.id = ?`, r.ItemID).
		Scan(&title, &typ, &parent, &grand, &grandID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNoMedia
	}
	if err != nil {
		return err
	}
	s.ItemType = typ
	s.Title = title
	defer func() {
		if typ == "track" && s.FileID > 0 {
			m.DB.QueryRowContext(ctx, `SELECT track_gain_db, album_gain_db, track_peak FROM media_files WHERE id = ?`, s.FileID).
				Scan(&s.TrackGainDB, &s.AlbumGainDB, &s.Peak)
		}
	}()
	if typ == "episode" && grand.Valid {
		s.Title = grand.String + " – " + title
	} else if typ == "track" && grand.Valid {
		s.Title = grand.String + " – " + title
	}

	q := `SELECT f.id, f.path, COALESCE(f.container, ''), COALESCE(f.bitrate_kbps, 0), COALESCE(f.duration_ms, 0),
		COALESCE(f.hdr_format, ''), f.available, COALESCE(f.probe_json, '') FROM media_files f JOIN media_versions v ON v.id = f.version_id
		WHERE v.item_id = ?`
	args := []any{r.ItemID}
	if r.FileID > 0 {
		q += ` AND f.id = ?`
		args = append(args, r.FileID)
	}
	// Prefer an available file, then the highest quality, then the first part.
	q += ` ORDER BY f.available DESC, COALESCE(f.height, 0) DESC, f.part_index LIMIT 1`
	var hdr, probeJSON string
	var available bool
	err = m.DB.QueryRowContext(ctx, q, args...).Scan(&s.FileID, &s.Path, &s.Media.Container, &s.Media.BitrateKbps,
		&s.Media.DurationMS, &hdr, &available, &probeJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNoMedia
	}
	if err != nil {
		return err
	}
	if !available {
		return ErrUnavailable
	}

	type stream struct {
		id                    int64
		index                 int
		kind, codec, lang     string
		title                 string
		def, forced           bool
		channels, w, h, depth int
		kbps                  int
		fps                   float64
		external              string
	}
	rows, err := m.DB.QueryContext(ctx, `SELECT id, COALESCE(stream_index, -1), kind, codec, COALESCE(language, ''), is_default, is_forced,
		COALESCE(channels, 0), COALESCE(width, 0), COALESCE(height, 0), COALESCE(bit_depth, 8), COALESCE(bitrate_kbps, 0),
		COALESCE(frame_rate, 0), COALESCE(external_path, ''), COALESCE(title, '') FROM streams WHERE file_id = ? ORDER BY id`, s.FileID)
	if err != nil {
		return err
	}
	var vids, auds, subs []stream
	for rows.Next() {
		var st stream
		if err := rows.Scan(&st.id, &st.index, &st.kind, &st.codec, &st.lang, &st.def, &st.forced, &st.channels, &st.w, &st.h,
			&st.depth, &st.kbps, &st.fps, &st.external, &st.title); err != nil {
			rows.Close()
			return err
		}
		switch st.kind {
		case "video":
			vids = append(vids, st)
		case "audio":
			auds = append(auds, st)
		case "subtitle":
			subs = append(subs, st)
		}
	}
	rows.Close()

	// Track choices are remembered per show (or per movie) and reused when the client asks
	// for automatic selection (PLAY-8).
	memoryID := r.ItemID
	if typ == "episode" && grandID.Valid {
		memoryID = grandID.Int64
	}
	if typ != "track" && r.UserID > 0 {
		var memAudio, memSub sql.NullString
		m.DB.QueryRowContext(ctx, `SELECT audio_language, subtitle_language FROM user_item_state WHERE user_id = ? AND item_id = ?`,
			r.UserID, memoryID).Scan(&memAudio, &memSub)
		if r.AudioStreamID == 0 && memAudio.String != "" {
			r.UserAudioLang = memAudio.String
		}
		if r.SubtitleStreamID == 0 && memSub.String != "" {
			if memSub.String == "off" {
				r.UserSubMode = "off"
			} else {
				r.UserSubMode, r.UserSubLang = "always", memSub.String
			}
		}
	}

	if len(vids) > 0 {
		v := vids[0]
		s.Media.Video = &VideoStream{Index: v.index, Codec: v.codec, Width: v.w, Height: v.h, BitDepth: v.depth, HDR: hdr,
			BitrateKbps: v.kbps, FrameRate: v.fps, Tag: codecTag(probeJSON, v.index)}
	}

	// Audio: the requested track, else the user's language, else the default, else the first.
	var audio *stream
	for i := range auds {
		a := &auds[i]
		switch {
		case r.AudioStreamID > 0 && a.id == r.AudioStreamID:
			audio = a
		case r.AudioStreamID == 0 && audio == nil && r.UserAudioLang != "" && a.lang == r.UserAudioLang:
			audio = a
		}
	}
	if audio == nil {
		for i := range auds {
			if auds[i].def {
				audio = &auds[i]
				break
			}
		}
	}
	if audio == nil && len(auds) > 0 {
		audio = &auds[0]
	}
	if audio != nil {
		s.Media.Audio = &AudioStream{Index: audio.index, Codec: audio.codec, Channels: audio.channels, BitrateKbps: audio.kbps, Language: audio.lang}
		s.AudioStreamID = audio.id
	}

	// Subtitles: the requested track; otherwise follow the user's subtitle mode.
	var sub *stream
	if r.SubtitleStreamID > 0 {
		for i := range subs {
			if subs[i].id == r.SubtitleStreamID {
				sub = &subs[i]
			}
		}
	} else if r.SubtitleStreamID == 0 {
		sub = autoSubtitle(subs, r, audio, func(st *stream) (string, bool) { return st.lang, st.forced })
	}
	s.HLSSubs = r.Profile.HLSSubtitles
	for _, st := range subs {
		if !(&SubtitleStream{Codec: st.codec}).IsImage() {
			s.TextSubs = append(s.TextSubs, SubtitleRendition{ID: st.id, Language: st.lang, Title: st.title, Forced: st.forced})
		}
	}
	if sub != nil {
		s.Media.Subtitle = &SubtitleStream{ID: sub.id, Index: sub.index, Codec: sub.codec, External: sub.external}
		s.SubtitleStreamID, s.SubtitleCodec = sub.id, sub.codec
	}

	if typ != "track" && r.UserID > 0 && (r.AudioStreamID != 0 || r.SubtitleStreamID != 0) {
		rememberTracks(ctx, m, r, memoryID, audio, sub, func(st *stream) string { return st.lang })
	}

	s.StartMS = r.StartMS
	if s.StartMS < 0 {
		var off sql.NullInt64
		m.DB.QueryRowContext(ctx, `SELECT view_offset_ms FROM user_item_state WHERE user_id = ? AND item_id = ?`, r.UserID, r.ItemID).Scan(&off)
		s.StartMS = off.Int64
	}
	if s.StartMS >= s.Media.DurationMS {
		s.StartMS = 0
	}
	s.positionMS = s.StartMS
	return nil
}

// autoSubtitle applies the user's subtitle mode: forced (only forced tracks in the audio's
// language), foreign (subtitles when the audio isn't in the user's language), always.
func autoSubtitle[T any](subs []T, r Request, audio *T, info func(*T) (string, bool)) *T {
	mode := r.UserSubMode
	if mode == "" {
		mode = "foreign"
	}
	if mode == "off" || len(subs) == 0 {
		return nil
	}
	want := r.UserSubLang
	if want == "" {
		want = r.UserAudioLang
	}
	audioLang := ""
	if audio != nil {
		audioLang, _ = info(audio)
	}
	pick := func(forced bool, lang string) *T {
		for i := range subs {
			l, f := info(&subs[i])
			if f == forced && (lang == "" || l == lang) {
				return &subs[i]
			}
		}
		return nil
	}
	switch mode {
	case "always":
		if s := pick(false, want); s != nil {
			return s
		}
		return pick(true, want)
	case "foreign":
		if want != "" && audioLang != "" && audioLang != want {
			if s := pick(false, want); s != nil {
				return s
			}
		}
	}
	// forced subtitles (signs, foreign dialogue) in the language being heard
	if audioLang != "" {
		return pick(true, audioLang)
	}
	return nil
}

// rememberTracks stores the languages of explicitly chosen tracks on the show or movie.
func rememberTracks[T any](ctx context.Context, m *Manager, r Request, itemID int64, audio, sub *T, lang func(*T) string) {
	audioLang, subLang := "", ""
	if r.AudioStreamID > 0 && audio != nil {
		audioLang = lang(audio)
	}
	switch {
	case r.SubtitleStreamID < 0:
		subLang = "off"
	case r.SubtitleStreamID > 0 && sub != nil:
		subLang = lang(sub)
	}
	if audioLang == "" && subLang == "" {
		return
	}
	_, err := m.DB.ExecContext(ctx, `INSERT INTO user_item_state (user_id, item_id, audio_language, subtitle_language) VALUES (?, ?, NULLIF(?, ''), NULLIF(?, ''))
		ON CONFLICT (user_id, item_id) DO UPDATE SET audio_language = COALESCE(excluded.audio_language, audio_language),
		subtitle_language = COALESCE(excluded.subtitle_language, subtitle_language)`, r.UserID, itemID, audioLang, subLang)
	if err != nil {
		slog.WarnContext(ctx, "remember tracks", "err", err)
	}
}

func (m *Manager) subtitleRelIndex(ctx context.Context, fileID int64, index int) int {
	var n int
	m.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM streams WHERE file_id = ? AND kind = 'subtitle' AND stream_index IS NOT NULL AND stream_index < ?`,
		fileID, index).Scan(&n)
	return n
}

// Progress records the playback position. Past the watched threshold the item is marked
// watched (once per session) and its resume point cleared.
// stopOthers ends the device's other sessions.
func (m *Manager) stopOthers(ctx context.Context, s *Session) {
	for _, old := range m.List() {
		if old.ID != s.ID && old.DeviceID == s.DeviceID && old.UserID == s.UserID {
			m.Stop(ctx, old.ID)
		}
	}
}

func (m *Manager) Progress(ctx context.Context, id string, positionMS int64, state string) error {
	s, ok := m.Get(id)
	if !ok {
		return ErrNoSession
	}
	s.mu.Lock()
	takeOver := s.preload && state == "playing"
	if takeOver {
		s.preload = false
	}
	prev, started := s.state, s.started
	if state == "playing" {
		s.started = true
	}
	s.positionMS, s.state = positionMS, state
	s.mu.Unlock()
	if takeOver {
		m.stopOthers(ctx, s)
	}
	switch {
	case state == "playing" && !started:
		m.emit("playback.started", s, positionMS)
	case started && prev == "playing" && state == "paused":
		m.emit("playback.paused", s, positionMS)
	case started && prev == "paused" && state == "playing":
		m.emit("playback.resumed", s, positionMS)
	}
	s.mu.Lock()
	alreadyWatched := s.watched
	dur := s.Media.DurationMS
	threshold := int64(m.Settings.Get().Library.WatchedThresholdPercent)
	if s.ItemType == "track" {
		threshold = 50 // music counts as played at half way
	}
	nowWatched := dur > 0 && positionMS*100 >= dur*threshold
	if nowWatched {
		s.watched = true
	}
	s.mu.Unlock()

	switch {
	case nowWatched && !alreadyWatched:
		m.emit("playback.watched", s, positionMS)
		_, err := m.DB.ExecContext(ctx, `INSERT INTO user_item_state(user_id, item_id, play_count, view_offset_ms, last_viewed_at)
			VALUES (?, ?, 1, 0, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
			ON CONFLICT(user_id, item_id) DO UPDATE SET play_count = play_count + 1, view_offset_ms = 0,
			last_viewed_at = excluded.last_viewed_at, updated_at = excluded.last_viewed_at,
			watchlisted_at = NULL`, s.UserID, s.ItemID) // watched: off the watchlist (USER-8)
		return err
	case !nowWatched && positionMS > 10_000 && s.ItemType != "track":
		_, err := m.DB.ExecContext(ctx, `INSERT INTO user_item_state(user_id, item_id, view_offset_ms, last_viewed_at)
			VALUES (?, ?, ?, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
			ON CONFLICT(user_id, item_id) DO UPDATE SET view_offset_ms = excluded.view_offset_ms,
			last_viewed_at = excluded.last_viewed_at, updated_at = excluded.last_viewed_at`, s.UserID, s.ItemID, positionMS)
		return err
	}
	return nil
}

// Stop ends a session: kills any transcode and closes the history entry.
func (m *Manager) Stop(ctx context.Context, id string) error {
	m.mu.Lock()
	m.init()
	s, ok := m.sessions[id]
	delete(m.sessions, id)
	m.mu.Unlock()
	if !ok {
		return ErrNoSession
	}
	encoder := ""
	if s.transcoder != nil {
		encoder = s.transcoder.Encoder()
		if s.Decision.VideoCopy {
			encoder = "copy"
		}
		s.transcoder.Stop()
		s.stopRungs()
	}
	snap := s.Snapshot()
	m.DB.ExecContext(ctx, `UPDATE play_history SET stopped_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), position_ms = ?, video_encoder = ?
		WHERE id = ?`, snap.PositionMS, nullStr(encoder), s.historyID)
	slog.Info("playback stopped", "user", s.UserName, "title", s.Title, "position", time.Duration(snap.PositionMS)*time.Millisecond)
	s.mu.Lock()
	started := s.started
	s.mu.Unlock()
	if started {
		m.emit("playback.stopped", s, snap.PositionMS)
	}
	return nil
}

func (m *Manager) emit(kind string, s *Session, positionMS int64) {
	if m.Events != nil {
		m.Events(kind, s, positionMS)
	}
}

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// Reap stops sessions that stopped reporting (closed tab, lost connection).
func (m *Manager) Reap(ctx context.Context, idle time.Duration) {
	for _, s := range m.List() {
		if time.Since(s.Snapshot().LastActive) > idle {
			slog.Info("ending idle playback session", "user", s.UserName, "title", s.Title)
			m.Stop(ctx, s.ID)
		}
	}
}

// Run reaps idle sessions until ctx ends, then stops everything.
func (m *Manager) Run(ctx context.Context) {
	// No session survives a restart, so anything left in the transcode directory is stale.
	if entries, err := os.ReadDir(m.TranscodeDir); err == nil {
		for _, e := range entries {
			os.RemoveAll(filepath.Join(m.TranscodeDir, e.Name()))
		}
	}
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	minute := time.NewTicker(time.Minute)
	defer minute.Stop()
	m.record()
	for {
		select {
		case <-ctx.Done():
			for _, s := range m.List() {
				m.Stop(context.Background(), s.ID)
			}
			return
		case <-t.C:
			m.Reap(ctx, 3*time.Minute)
			now := time.Now()
			for _, s := range m.List() {
				s.reapRungs(now)
			}
		case <-minute.C:
			m.record()
		}
	}
}

// String describes the decision for logs and the info panel.
func (d Decision) String() string {
	switch d.Method {
	case DirectPlay:
		return "Direct Play"
	case DirectStream:
		return "Direct Stream"
	}
	return fmt.Sprintf("Transcode %s %dp %d kbps", d.VideoCodec, d.Height, d.VideoKbps)
}

// codecTag reads a stream's container codec tag (hvc1, hev1, avc1…) from the stored ffprobe output.
func codecTag(probeJSON string, index int) string {
	var out struct {
		Streams []struct {
			Index int    `json:"index"`
			Tag   string `json:"codec_tag_string"`
		} `json:"streams"`
	}
	if json.Unmarshal([]byte(probeJSON), &out) != nil {
		return ""
	}
	for _, st := range out.Streams {
		if st.Index == index {
			return st.Tag
		}
	}
	return ""
}

// keyframes returns the cached keyframe index of a file, building it if needed. Indexes
// from Matroska Cues or MP4 tables take milliseconds; others may need an ffprobe scan,
// bounded by timeout here (a background task fills them in ahead of time).
func (m *Manager) keyframes(ctx context.Context, fileID int64, path, container string, timeout time.Duration) ([]float64, error) {
	var size, mtime int64
	if err := m.DB.QueryRowContext(ctx, `SELECT size, mtime FROM media_files WHERE id = ?`, fileID).Scan(&size, &mtime); err != nil {
		return nil, err
	}
	var cachedSize, cachedMtime int64
	var data string
	err := m.DB.QueryRowContext(ctx, `SELECT size, mtime, times FROM keyframes WHERE file_id = ?`, fileID).Scan(&cachedSize, &cachedMtime, &data)
	if err == nil && cachedSize == size && cachedMtime == mtime {
		var kf []float64
		if json.Unmarshal([]byte(data), &kf) == nil && len(kf) > 0 {
			return kf, nil
		}
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	kf, err := Keyframes(ctx, m.FFprobe, path, normalizeContainer(container))
	if err != nil {
		return nil, err
	}
	for i := range kf {
		kf[i] = math.Round(kf[i]*1000) / 1000
	}
	b, _ := json.Marshal(kf)
	m.DB.ExecContext(context.WithoutCancel(ctx), `INSERT INTO keyframes(file_id, size, mtime, times) VALUES (?, ?, ?, ?)
		ON CONFLICT(file_id) DO UPDATE SET size = excluded.size, mtime = excluded.mtime, times = excluded.times`, fileID, size, mtime, string(b))
	return kf, nil
}

// SubtitleRendition is a text subtitle track offered inside HLS.
type SubtitleRendition struct {
	ID              int64
	Language, Title string
	Forced          bool
}
