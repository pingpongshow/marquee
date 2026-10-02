// Package config loads process-level configuration from environment variables.
// Everything an admin changes at runtime lives in the settings store instead.
package config

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Version is set at build time via -ldflags "-X marquee/internal/config.Version=…".
var Version = "dev"

type Config struct {
	// ConfigDir holds the database, backups, image cache and logs.
	ConfigDir string
	// TranscodeDir holds temporary HLS segments.
	TranscodeDir string
	// ListenAddr is the HTTP listen address, e.g. ":32500".
	ListenAddr string
	// FFmpegPath and FFprobePath point at the jellyfin-ffmpeg binaries.
	FFmpegPath  string
	FFprobePath string
	// WebDir, if set, serves the web client from disk instead of the embedded build (development).
	WebDir string
	// PlexDir is the Plex data folder (containing "Library"), mounted read-only for import.
	PlexDir string
	// RecordingsDir holds Live TV recordings (LIVE-5).
	RecordingsDir string
	LogLevel      slog.Level
}

func Load() (Config, error) {
	c := Config{
		ConfigDir:     env("MARQUEE_CONFIG_DIR", "/config"),
		TranscodeDir:  env("MARQUEE_TRANSCODE_DIR", "/transcode"),
		ListenAddr:    ":" + env("MARQUEE_PORT", "32500"),
		FFmpegPath:    env("MARQUEE_FFMPEG", "/usr/lib/jellyfin-ffmpeg/ffmpeg"),
		FFprobePath:   env("MARQUEE_FFPROBE", "/usr/lib/jellyfin-ffmpeg/ffprobe"),
		WebDir:        os.Getenv("MARQUEE_WEB_DIR"),
		PlexDir:       env("MARQUEE_PLEX_DIR", "/plex"),
		RecordingsDir: env("MARQUEE_RECORDINGS_DIR", "/recordings"),
	}
	if _, err := strconv.Atoi(strings.TrimPrefix(c.ListenAddr, ":")); err != nil {
		return c, fmt.Errorf("invalid MARQUEE_PORT: %w", err)
	}
	if err := c.LogLevel.UnmarshalText([]byte(env("MARQUEE_LOG_LEVEL", "info"))); err != nil {
		return c, fmt.Errorf("invalid MARQUEE_LOG_LEVEL: %w", err)
	}
	for _, dir := range []string{c.ConfigDir, c.TranscodeDir, c.BackupDir()} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return c, fmt.Errorf("create %s: %w", dir, err)
		}
	}
	return c, nil
}

func (c Config) DBPath() string    { return filepath.Join(c.ConfigDir, "marquee.db") }
func (c Config) BackupDir() string { return filepath.Join(c.ConfigDir, "backups") }

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
