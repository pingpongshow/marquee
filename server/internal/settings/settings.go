// Package settings stores admin-editable server settings as JSON in the settings table.
// Values are cached in memory; Update persists atomically and notifies subscribers.
package settings

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
)

const (
	keyServer   = "server_settings"
	keyServerID = "server_id"
)

type Settings struct {
	Security     Security     `json:"security"`
	General      General      `json:"general"`
	Network      Network      `json:"network"`
	RemoteAccess RemoteAccess `json:"remoteAccess"`
	Transcoder   Transcoder   `json:"transcoder"`
	Library      Library      `json:"library"`
	Metadata     Metadata     `json:"metadata"`
	Tasks        Tasks        `json:"tasks"`
	Music        Music        `json:"music"`
	Cinema       Cinema       `json:"cinema"`
	Webhooks     []Webhook    `json:"webhooks"`
	Integrations Integrations `json:"integrations"`
	AI           AI           `json:"ai"`
	// Notifications: outgoing email and admin alerts.
	Notifications Notifications `json:"notifications"`
	// VODSave saves movies-on-demand films and episodes into the library (admins only).
	VODSave VODSave `json:"vodSave"`
	// PlexSync keeps watch state, ratings, resume positions and plays in step with a Plex
	// Media Server, both ways. Exposed through /plex-sync, not /settings.
	PlexSync PlexSync `json:"plexSync"`
}

// PlexSync configures two-way sync with a Plex Media Server through its HTTP API.
type PlexSync struct {
	// URL is the Plex server's address (http://host:32400).
	URL string `json:"url"`
	// Token is the server owner's Plex token. A secret: never returned by the API, never logged.
	Token string `json:"token"`
	// Enabled turns automatic sync on (every IntervalMinutes). Manual runs work either way.
	Enabled         bool `json:"enabled"`
	IntervalMinutes int  `json:"intervalMinutes"`
	// What syncs.
	WatchState      bool `json:"watchState"`
	Ratings         bool `json:"ratings"`
	ResumePositions bool `json:"resumePositions"`
	PlayHistory     bool `json:"playHistory"`
	// PathMappings pair Plex folders with Marquee's (saved by the Plex import); empty uses
	// suggested ones.
	PathMappings []PlexPathMapping `json:"pathMappings,omitempty"`
}

// PlexPathMapping rewrites a Plex path prefix to a Marquee one.
type PlexPathMapping struct {
	Plex    string `json:"plex"`
	Marquee string `json:"marquee"`
}

// Configured reports whether a Plex server and token are set.
func (p PlexSync) Configured() bool { return p.URL != "" && p.Token != "" }

// VODSave configures saving films and episodes from movies-on-demand (Xtream Codes) sources
// into the library's folders. An empty folder turns saving of that kind off.
type VODSave struct {
	// MoviesFolder and ShowsFolder are paths as the server sees them.
	MoviesFolder string `json:"moviesFolder"`
	ShowsFolder  string `json:"showsFolder"`
	// Quality: original (the file as it is; only an unusual container is changed, to MKV),
	// remux (to MKV, every stream copied) or transcode (the settings below).
	Quality string `json:"quality"`
	// Transcode: MaxHeight caps the resolution (never upscaled): 2160, 1080, 720 or 480.
	MaxHeight int `json:"maxHeight"`
	// VideoCodec is hevc or h264.
	VideoCodec string `json:"videoCodec"`
	// VideoQuality is a constant-quality level (high, balanced, small), used unless
	// VideoKbps (a target bitrate) is set.
	VideoQuality string `json:"videoQuality"`
	VideoKbps    int    `json:"videoKbps"`
	// Audio: copy, aac_stereo or aac_51 (at most 5.1; stereo stays stereo).
	Audio string `json:"audio"`
	// ConcurrentPerSource is how many saves run at once per source (1–5).
	ConcurrentPerSource int `json:"concurrentPerSource"`
	// ScanAfterSave scans the library whose folder holds a saved file.
	ScanAfterSave bool `json:"scanAfterSave"`
	// SkipExisting skips a title already in a library (by TMDB id) or whose file exists.
	SkipExisting bool `json:"skipExisting"`
	// PauseWhileWatching pauses saves while someone watches: from the same source (source:
	// a film or episode from it, or Live TV from the same host) or anything at all (any).
	PauseWhileWatching bool   `json:"pauseWhileWatching"`
	PauseScope         string `json:"pauseScope"`
	// RetryFailed queues failed saves again every few hours.
	RetryFailed bool `json:"retryFailed"`
	// Destinations are more folders to save to besides the two above (e.g. Anime), chosen
	// when saving ("Save to Anime").
	Destinations []VODSaveDestination `json:"destinations,omitempty"`
}

// VODSaveDestination is a folder to save to besides the default Movies and Shows ones.
type VODSaveDestination struct {
	ID     string `json:"id"`
	Name   string `json:"name"`   // shown in buttons; unique, ignoring case
	Folder string `json:"folder"` // as the server sees it
	Kind   string `json:"kind"`   // movies, shows, both
}

// Takes reports whether films (kind "movie") or episodes ("episode") can be saved there.
func (d VODSaveDestination) Takes(kind string) bool {
	switch d.Kind {
	case "both":
		return true
	case "movies":
		return kind == "movie"
	case "shows":
		return kind == "episode"
	}
	return false
}

// Destination is the destination with an id.
func (v VODSave) Destination(id string) (VODSaveDestination, bool) {
	for _, d := range v.Destinations {
		if d.ID == id {
			return d, true
		}
	}
	return VODSaveDestination{}, false
}

// VODSave quality modes.
const (
	VODSaveOriginal  = "original"
	VODSaveRemux     = "remux"
	VODSaveTranscode = "transcode"
)

// Alert types an administrator can turn on (all off by default).
const (
	AlertTaskFailed      = "task_failed"
	AlertDiskLow         = "disk_low"
	AlertRecordingFailed = "recording_failed"
	AlertSourceFailed    = "source_failed"
	AlertIntegrationDown = "integration_down"
	AlertFailedSignIns   = "failed_signins"
	AlertNewAdminDevice  = "new_admin_device"
	AlertPlaybackFailed  = "playback_failed"
	AlertVODSaveFailed   = "vod_save_failed"
	AlertVODSaveDone     = "vod_save_done"
	// AlertDVRQuota: a recording wasn't made, or older ones were removed, because its owner
	// is over their DVR quota. It isn't in the API's AlertType enum (shipped apps decode it
	// strictly); the API exposes it as NotificationSettings.dvrQuotaAlert.
	AlertDVRQuota = "dvr_quota"
	// AlertPlexSync: two-way Plex sync is failing (Plex rejects the token, or has been
	// unreachable for several runs). Like dvr_quota it isn't in the API's AlertType enum; the
	// API exposes it as NotificationSettings.plexSyncAlert.
	AlertPlexSync = "plex_sync"
)

// AlertTypes lists every alert type.
var AlertTypes = []string{AlertTaskFailed, AlertDiskLow, AlertRecordingFailed, AlertSourceFailed,
	AlertIntegrationDown, AlertFailedSignIns, AlertNewAdminDevice, AlertPlaybackFailed, AlertVODSaveFailed, AlertVODSaveDone, AlertDVRQuota, AlertPlexSync}

// Notifications configures outgoing email (SMTP) and which alerts administrators get.
type Notifications struct {
	SMTPHost     string `json:"smtpHost"`
	SMTPPort     int    `json:"smtpPort"`
	SMTPSecurity string `json:"smtpSecurity"` // none, starttls, tls
	SMTPUsername string `json:"smtpUsername"`
	SMTPPassword string `json:"smtpPassword"`
	FromAddress  string `json:"fromAddress"`
	FromName     string `json:"fromName"`
	// LinkURL is the address used in links in mail (reset links); empty = the remote URL.
	LinkURL string `json:"linkUrl"`
	// Alerts are the alert types that are on.
	Alerts []string `json:"alerts"`
	// RecipientsAllAdmins mails every administrator with an email address; otherwise
	// Recipients lists the administrators (user ids) who get alert mail.
	RecipientsAllAdmins bool    `json:"recipientsAllAdmins"`
	Recipients          []int64 `json:"recipients"`
	// DiskThresholdPercent: alert when a volume has less free space than this.
	DiskThresholdPercent int `json:"diskThresholdPercent"`
}

// MailConfigured reports whether outgoing mail is set up.
func (n Notifications) MailConfigured() bool {
	return n.SMTPHost != "" && n.SMTPPort > 0 && n.FromAddress != ""
}

// AlertOn reports whether an alert type is turned on.
func (n Notifications) AlertOn(kind string) bool {
	for _, a := range n.Alerts {
		if a == kind {
			return true
		}
	}
	return false
}

// LinkBase is the address links in mail start with: the link URL, else the remote URL, else
// empty (links can't be made). It never comes from a request's Host header.
func (s Settings) LinkBase() string {
	if u := strings.TrimRight(strings.TrimSpace(s.Notifications.LinkURL), "/"); u != "" {
		return u
	}
	return strings.TrimRight(strings.TrimSpace(s.RemoteAccess.RemoteURL), "/")
}

// AI is the optional language model (a local Ollama) that makes Muse and recommendations
// smarter. With Enabled off, or Ollama unreachable, everything works as without it.
type AI struct {
	Enabled    bool   `json:"enabled"`
	URL        string `json:"url"`
	ChatModel  string `json:"chatModel"`
	EmbedModel string `json:"embedModel"`
	// UnloadMinutes is sent as keep_alive on every request, so Ollama frees the GPU after
	// this much idle time (0 = right after each request).
	UnloadMinutes int `json:"unloadMinutes"`
	// Per-feature switches, used only while Enabled is on.
	UnderstandPrompts bool `json:"understandPrompts"`
	RerankMovies      bool `json:"rerankMovies"`
	LyricsThemes      bool `json:"lyricsThemes"`
	Reasons           bool `json:"reasons"`
	// LearnFromBehaviour uses skips, abandoned titles and ratings; it needs no model, so
	// it works with Enabled off.
	LearnFromBehaviour bool `json:"learnFromBehaviour"`
}

// On reports whether the model may be used for a feature switched on by flag.
func (a AI) On(flag bool) bool { return a.Enabled && flag }

// Integrations are other services on the network Marquee talks to.
type Integrations struct {
	// Seerr takes requests for titles that aren't in the library (REQ-1).
	SeerrURL    string `json:"seerrUrl"`
	SeerrAPIKey string `json:"seerrApiKey"`
	// Last.fm scrobbling (MUSIC-12): the owner's API account (www.last.fm/api/account/create).
	LastFMAPIKey string `json:"lastFmApiKey"`
	LastFMSecret string `json:"lastFmSecret"`
	// Bazarr finds and downloads subtitles for movies and episodes (META-12).
	BazarrURL    string `json:"bazarrUrl"`
	BazarrAPIKey string `json:"bazarrApiKey"`
	// LiveTVSources feed Live TV (LIVE-1).
	LiveTVSources []LiveTVSource `json:"liveTvSources"`
	// DVR padding in minutes (LIVE-5).
	DVRPaddingBefore int `json:"dvrPaddingBefore"`
	DVRPaddingAfter  int `json:"dvrPaddingAfter"`
	// Commercials is what happens to finished recordings (and, nightly, older ones):
	// CommercialsOff, CommercialsDetect (find the breaks with Comskip, leave the file) or
	// CommercialsRemove (cut them out; the original goes to the media trash). Off by default,
	// since removing changes files.
	Commercials string `json:"commercials"`
	// DVRDefaultQuotaGB is every user's recording quota unless their own overrides it
	// (0 = unlimited). DVRQuotaAction is what happens when a recording is due and its owner
	// is at or over quota: DVRQuotaBlock (it isn't recorded) or DVRQuotaDeleteOldest (their
	// oldest recordings they've watched go to the media trash to make room).
	DVRDefaultQuotaGB int    `json:"dvrDefaultQuotaGB"`
	DVRQuotaAction    string `json:"dvrQuotaAction"`
	// DVRFolder is where new recordings go ("" = the server's configured recordings
	// folder). Recordings made before a change stay where they are.
	DVRFolder string `json:"dvrFolder"`
	// Movies on demand under Live TV (LIVE-6): off until the admin turns it on.
	VODEnabled bool        `json:"vodEnabled"`
	VODSources []VODSource `json:"vodSources"`
}

// VODSource is an M3U playlist of films (e.g. Pluto TV's on-demand movies), or an Xtream
// Codes server's VOD (e.g. Dispatcharr's, with a user and its XC password).
type VODSource struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Kind string `json:"kind,omitempty"` // m3u ("" before Xtream Codes sources existed), xtream
	URL  string `json:"url"`
	// Username and Password sign in to an Xtream Codes server. The password is a secret:
	// never returned by the API, never logged.
	Username  string `json:"username,omitempty"`
	Password  string `json:"password,omitempty"`
	UserAgent string `json:"userAgent"`
	Enabled   bool   `json:"enabled"`
}

// DVR quota actions.
const (
	DVRQuotaBlock        = "block"
	DVRQuotaDeleteOldest = "deleteOldest"
)

// Commercials settings.
const (
	CommercialsOff    = "off"
	CommercialsDetect = "detect"
	CommercialsRemove = "remove"
)

// VODKindXtream is an Xtream Codes source; anything else is an M3U playlist.
const VODKindXtream = "xtream"

// Xtream reports whether the source is an Xtream Codes server.
func (s VODSource) Xtream() bool { return s.Kind == VODKindXtream }

// LiveTVSource is an M3U playlist with its XMLTV guide, or a Dispatcharr server.
type LiveTVSource struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Kind      string `json:"kind"` // m3u, dispatcharr
	URL       string `json:"url"`
	EPGURL    string `json:"epgUrl"`
	UserAgent string `json:"userAgent"`
	Enabled   bool   `json:"enabled"`
}

// Webhook is a URL told about events (ADM-5).
type Webhook struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	URL     string   `json:"url"`
	Secret  string   `json:"secret"` // signs payloads (HMAC-SHA256) when set
	Events  []string `json:"events"`
	Enabled bool     `json:"enabled"`
}

// Cinema plays trailers (and a pre-roll video) before movies (PLAY-18).
type Cinema struct {
	Trailers      int    `json:"trailers"` // 0–5; 0 = off
	PrerollItemID *int64 `json:"prerollItemId,omitempty"`
}

// Music configures the M6.5 music features.
type Music struct {
	SoundprintAnalysis bool `json:"soundprintAnalysis"`
	OnlineLyrics       bool `json:"onlineLyrics"`
	LoudnessAnalysis   bool `json:"loudnessAnalysis"`
	// ArtistDetails looks artists and albums up on MusicBrainz (and bios on Wikipedia).
	ArtistDetails bool `json:"artistDetails"`
}

type Security struct {
	// PinSignIn is where profiles may sign in with a PIN: off, local, everywhere.
	PinSignIn string `json:"pinSignIn"`
}

type General struct {
	ServerName       string `json:"serverName"`
	MetadataLanguage string `json:"metadataLanguage"`
	// RatingPrompt asks people to rate a movie or episode when they finish it (USER-19).
	RatingPrompt bool `json:"ratingPrompt"`
}

type Network struct {
	LANSubnets     []string `json:"lanSubnets"`
	LANURL         string   `json:"lanUrl"`
	BonjourEnabled bool     `json:"bonjourEnabled"`
}

type RemoteAccess struct {
	Enabled                  bool   `json:"enabled"`
	RemoteURL                string `json:"remoteUrl"`
	UploadSpeedKbps          int    `json:"uploadSpeedKbps"`
	TotalRemoteLimitKbps     int    `json:"totalRemoteLimitKbps"`
	PerStreamRemoteLimitKbps int    `json:"perStreamRemoteLimitKbps"`
	MinStreamKbps            int    `json:"minStreamKbps"`
}

type QualityRung struct {
	Label     string `json:"label"`
	MaxHeight int    `json:"maxHeight"`
	VideoKbps int    `json:"videoKbps"`
}

type Transcoder struct {
	EncoderOrder            []string      `json:"encoderOrder"`
	MaxConcurrentTranscodes int           `json:"maxConcurrentTranscodes"`
	Preset                  string        `json:"preset"`
	PreferHEVCRemote        bool          `json:"preferHevcRemote"`
	ToneMapping             bool          `json:"toneMapping"`
	ThrottleSegmentsAhead   int           `json:"throttleSegmentsAhead"`
	RemoteLadder            []QualityRung `json:"remoteLadder"`
	// NVENCSessions is how many NVENC encodes may run at once before new jobs go to
	// Quick Sync or the CPU (consumer NVIDIA drivers cap concurrent sessions). 0 = no cap.
	NVENCSessions int `json:"nvencSessions"`
}

type Library struct {
	WatchFilesystem         bool     `json:"watchFilesystem"`
	ScanOnStartup           bool     `json:"scanOnStartup"`
	IgnorePatterns          []string `json:"ignorePatterns"`
	BrowseRoots             []string `json:"browseRoots"`
	WatchedThresholdPercent int      `json:"watchedThresholdPercent"`
	Trickplay               bool     `json:"trickplay"`
	DetectIntros            bool     `json:"detectIntros"`
	// AllowMediaDeletion lets admins delete media files from Library Health; they move to
	// a .marquee-trash folder in the library folder and are removed after 30 days.
	AllowMediaDeletion bool `json:"allowMediaDeletion"`
}

type Metadata struct {
	TMDBAPIKey           string `json:"tmdbApiKey"`
	FanartAPIKey         string `json:"fanartApiKey"`
	OpenSubtitlesAPIKey  string `json:"openSubtitlesApiKey"`
	OpenSubtitlesUser    string `json:"openSubtitlesUsername"`
	OpenSubtitlesPass    string `json:"openSubtitlesPassword"`
	OMDbAPIKey           string `json:"omdbApiKey"`
	OMDbDailyLimit       int    `json:"omdbDailyLimit"`
	AnimeEpisodeOrdering string `json:"animeEpisodeOrdering"`
}

type Tasks struct {
	MaintenanceWindowStart string `json:"maintenanceWindowStart"`
	MaintenanceWindowHours int    `json:"maintenanceWindowHours"`
	BackupRetention        int    `json:"backupRetention"` // days to keep nightly backups
}

// Defaults are applied first; stored JSON is unmarshalled over them, so new settings
// added in later versions pick up their defaults automatically.
func Defaults() Settings {
	return Settings{
		Security: Security{PinSignIn: "local"},
		General:  General{ServerName: "Marquee", MetadataLanguage: "en-US"},
		Network: Network{
			LANSubnets:     []string{"10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"},
			BonjourEnabled: true,
		},
		RemoteAccess: RemoteAccess{
			Enabled:       true,
			MinStreamKbps: 1500,
		},
		Transcoder: Transcoder{
			EncoderOrder:            []string{"nvenc", "qsv", "software"},
			MaxConcurrentTranscodes: 12,
			Preset:                  "balanced",
			PreferHEVCRemote:        true,
			ToneMapping:             true,
			ThrottleSegmentsAhead:   10,
			NVENCSessions:           8,
			RemoteLadder: []QualityRung{
				{Label: "1080p 12 Mbps", MaxHeight: 1080, VideoKbps: 12000},
				{Label: "1080p 8 Mbps", MaxHeight: 1080, VideoKbps: 8000},
				{Label: "720p 4 Mbps", MaxHeight: 720, VideoKbps: 4000},
				{Label: "480p 1.5 Mbps", MaxHeight: 480, VideoKbps: 1500},
				{Label: "360p 750 kbps", MaxHeight: 360, VideoKbps: 750},
			},
		},
		Library: Library{
			WatchFilesystem:         true,
			ScanOnStartup:           false,
			IgnorePatterns:          []string{"*_staging.*", "*.part", "*.partial", "*.tmp", "*sample*", ".*"},
			BrowseRoots:             []string{"/media"},
			WatchedThresholdPercent: 90,
			Trickplay:               true,
			DetectIntros:            true,
		},
		Metadata:     Metadata{AnimeEpisodeOrdering: "seasonal", OMDbDailyLimit: 950},
		Tasks:        Tasks{MaintenanceWindowStart: "03:00", MaintenanceWindowHours: 4, BackupRetention: 7},
		Music:        Music{SoundprintAnalysis: true, OnlineLyrics: false, LoudnessAnalysis: true, ArtistDetails: true},
		Integrations: Integrations{DVRPaddingBefore: 1, DVRPaddingAfter: 3, Commercials: CommercialsOff, DVRQuotaAction: DVRQuotaBlock},
		Notifications: Notifications{SMTPPort: 587, SMTPSecurity: "starttls", FromName: "Marquee",
			RecipientsAllAdmins: true, DiskThresholdPercent: 10},
		VODSave: VODSave{Quality: VODSaveOriginal, MaxHeight: 1080, VideoCodec: "hevc", VideoQuality: "balanced",
			Audio: "copy", ConcurrentPerSource: 1, ScanAfterSave: true, SkipExisting: true,
			PauseWhileWatching: true, PauseScope: "source", RetryFailed: true},
		PlexSync: PlexSync{IntervalMinutes: 30, WatchState: true, Ratings: true, ResumePositions: true, PlayHistory: true},
		AI: AI{URL: "http://127.0.0.1:11434", ChatModel: "qwen3:8b", EmbedModel: "mxbai-embed-large", UnloadMinutes: 5,
			UnderstandPrompts: true, RerankMovies: true, LyricsThemes: true, Reasons: true, LearnFromBehaviour: true},
	}
}

type Store struct {
	db       *sql.DB
	mu       sync.RWMutex
	cur      Settings
	serverID string
	subs     []func(Settings)
}

func Open(ctx context.Context, db *sql.DB) (*Store, error) {
	s := &Store{db: db, cur: Defaults()}
	var raw string
	err := db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, keyServer).Scan(&raw)
	switch {
	case errors.Is(err, sql.ErrNoRows):
	case err != nil:
		return nil, err
	default:
		if err := json.Unmarshal([]byte(raw), &s.cur); err != nil {
			return nil, fmt.Errorf("decode settings: %w", err)
		}
	}
	if err := s.loadServerID(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) loadServerID(ctx context.Context) error {
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, keyServerID).Scan(&s.serverID)
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	b := make([]byte, 16)
	rand.Read(b)
	s.serverID = hex.EncodeToString(b)
	_, err = s.db.ExecContext(ctx, `INSERT INTO settings(key, value) VALUES (?, ?)`, keyServerID, s.serverID)
	return err
}

// ServerID is a stable random identifier clients use to recognise this server across addresses.
func (s *Store) ServerID() string { return s.serverID }

// Get returns a copy of the current settings.
func (s *Store) Get() Settings {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return clone(s.cur)
}

// Update applies fn to a copy of the settings, validates, persists, then swaps it in.
func (s *Store) Update(ctx context.Context, fn func(*Settings) error) (Settings, error) {
	s.mu.Lock()
	next := clone(s.cur)
	if err := fn(&next); err != nil {
		s.mu.Unlock()
		return Settings{}, err
	}
	if err := next.Validate(); err != nil {
		s.mu.Unlock()
		return Settings{}, err
	}
	raw, err := json.Marshal(next)
	if err != nil {
		s.mu.Unlock()
		return Settings{}, err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO settings(key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value,
		updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')`, keyServer, string(raw))
	if err != nil {
		s.mu.Unlock()
		return Settings{}, err
	}
	s.cur = next
	subs := append([]func(Settings){}, s.subs...)
	s.mu.Unlock()
	for _, fn := range subs {
		fn(clone(next))
	}
	return clone(next), nil
}

// Subscribe registers fn to be called after every successful update.
func (s *Store) Subscribe(fn func(Settings)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.subs = append(s.subs, fn)
}

func clone(in Settings) Settings {
	raw, _ := json.Marshal(in)
	var out Settings
	_ = json.Unmarshal(raw, &out)
	return out
}
