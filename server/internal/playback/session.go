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
	historyID  int64
	transcoder *Transcoder
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

	mu       sync.Mutex
	sessions map[string]*Session
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
	for _, old := range m.List() {
		if old.DeviceID == r.DeviceID && old.UserID == r.UserID {
			m.Stop(ctx, old.ID)
		}
	}

	q := r.Quality
	q.Remote = r.Remote
	q.OtherRemoteKbps, q.OtherRemoteCount = m.remoteUsage("")
	s.LimitKbps, s.LimitReason = MaxKbps(q, cfg.RemoteAccess)
	s.Decision = Decide(s.Media, r.Profile, Limits{MaxKbps: s.LimitKbps, Remote: r.Remote, PreferHEVC: r.Remote && cfg.Transcoder.PreferHEVCRemote})
	if s.Decision.ToneMap && !cfg.Transcoder.ToneMapping {
		s.Decision.ToneMap = false
	}

	// Music that needs converting streams progressively from /audio, without HLS.
	if s.Decision.Method != DirectPlay && s.Media.Video != nil {
		if m.transcodeCount() >= cfg.Transcoder.MaxConcurrentTranscodes {
			return nil, ErrBusy
		}
		job := Job{Input: s.Path, Decision: s.Decision, VideoIndex: -1, AudioIndex: -1, SubIndex: -1, SubRelIndex: -1,
			Dir: filepath.Join(m.TranscodeDir, s.ID), Preset: cfg.Transcoder.Preset, QSVDevice: m.Encoders.QSVDevice}
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
		s.transcoder = &Transcoder{FFmpeg: m.FFmpeg, Job: job, Encoders: m.Encoders.Available(cfg.Transcoder.EncoderOrder),
			TotalSegments: SegmentCount(s.Media.DurationMS), ThrottleAhead: cfg.Transcoder.ThrottleSegmentsAhead}
		if s.Decision.VideoCopy {
			s.transcoder.Encoders = []string{"software"} // copying video needs no encoder
		}
		s.transcoder.lastRequest = int(s.StartMS / 1000 / SegmentSeconds)
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
	err := m.DB.QueryRowContext(ctx, `SELECT i.title, i.type, p.title, g.title FROM items i
		LEFT JOIN items p ON p.id = i.parent_id LEFT JOIN items g ON g.id = i.grandparent_id WHERE i.id = ?`, r.ItemID).
		Scan(&title, &typ, &parent, &grand)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNoMedia
	}
	if err != nil {
		return err
	}
	s.ItemType = typ
	s.Title = title
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
		def, forced           bool
		channels, w, h, depth int
		kbps                  int
		fps                   float64
		external              string
	}
	rows, err := m.DB.QueryContext(ctx, `SELECT id, COALESCE(stream_index, -1), kind, codec, COALESCE(language, ''), is_default, is_forced,
		COALESCE(channels, 0), COALESCE(width, 0), COALESCE(height, 0), COALESCE(bit_depth, 8), COALESCE(bitrate_kbps, 0),
		COALESCE(frame_rate, 0), COALESCE(external_path, '') FROM streams WHERE file_id = ? ORDER BY id`, s.FileID)
	if err != nil {
		return err
	}
	var vids, auds, subs []stream
	for rows.Next() {
		var st stream
		if err := rows.Scan(&st.id, &st.index, &st.kind, &st.codec, &st.lang, &st.def, &st.forced, &st.channels, &st.w, &st.h,
			&st.depth, &st.kbps, &st.fps, &st.external); err != nil {
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
	if sub != nil {
		s.Media.Subtitle = &SubtitleStream{ID: sub.id, Index: sub.index, Codec: sub.codec, External: sub.external}
		s.SubtitleStreamID, s.SubtitleCodec = sub.id, sub.codec
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

func (m *Manager) subtitleRelIndex(ctx context.Context, fileID int64, index int) int {
	var n int
	m.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM streams WHERE file_id = ? AND kind = 'subtitle' AND stream_index IS NOT NULL AND stream_index < ?`,
		fileID, index).Scan(&n)
	return n
}

// Progress records the playback position. Past the watched threshold the item is marked
// watched (once per session) and its resume point cleared.
func (m *Manager) Progress(ctx context.Context, id string, positionMS int64, state string) error {
	s, ok := m.Get(id)
	if !ok {
		return ErrNoSession
	}
	s.mu.Lock()
	s.positionMS, s.state = positionMS, state
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
		_, err := m.DB.ExecContext(ctx, `INSERT INTO user_item_state(user_id, item_id, play_count, view_offset_ms, last_viewed_at)
			VALUES (?, ?, 1, 0, strftime('%Y-%m-%dT%H:%M:%fZ', 'now'))
			ON CONFLICT(user_id, item_id) DO UPDATE SET play_count = play_count + 1, view_offset_ms = 0,
			last_viewed_at = excluded.last_viewed_at, updated_at = excluded.last_viewed_at`, s.UserID, s.ItemID)
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
	}
	snap := s.Snapshot()
	m.DB.ExecContext(ctx, `UPDATE play_history SET stopped_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), position_ms = ?, video_encoder = ?
		WHERE id = ?`, snap.PositionMS, nullStr(encoder), s.historyID)
	slog.Info("playback stopped", "user", s.UserName, "title", s.Title, "position", time.Duration(snap.PositionMS)*time.Millisecond)
	return nil
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
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			for _, s := range m.List() {
				m.Stop(context.Background(), s.ID)
			}
			return
		case <-t.C:
			m.Reap(ctx, 3*time.Minute)
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
