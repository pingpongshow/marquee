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
	Webhooks     []Webhook    `json:"webhooks"`
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

// Music configures the M6.5 music features.
type Music struct {
	SonicAnalysis    bool `json:"sonicAnalysis"`
	OnlineLyrics     bool `json:"onlineLyrics"`
	LoudnessAnalysis bool `json:"loudnessAnalysis"`
}

type Security struct {
	// PinSignIn is where profiles may sign in with a PIN: off, local, everywhere.
	PinSignIn string `json:"pinSignIn"`
}

type General struct {
	ServerName       string `json:"serverName"`
	MetadataLanguage string `json:"metadataLanguage"`
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
}

type Metadata struct {
	TMDBAPIKey           string `json:"tmdbApiKey"`
	FanartAPIKey         string `json:"fanartApiKey"`
	OpenSubtitlesAPIKey  string `json:"openSubtitlesApiKey"`
	OMDbAPIKey           string `json:"omdbApiKey"`
	OMDbDailyLimit       int    `json:"omdbDailyLimit"`
	AnimeEpisodeOrdering string `json:"animeEpisodeOrdering"`
}

type Tasks struct {
	MaintenanceWindowStart string `json:"maintenanceWindowStart"`
	MaintenanceWindowHours int    `json:"maintenanceWindowHours"`
	BackupRetention        int    `json:"backupRetention"`
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
		Metadata: Metadata{AnimeEpisodeOrdering: "seasonal", OMDbDailyLimit: 950},
		Tasks:    Tasks{MaintenanceWindowStart: "03:00", MaintenanceWindowHours: 4, BackupRetention: 7},
		Music:    Music{SonicAnalysis: true, OnlineLyrics: false, LoudnessAnalysis: true},
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
