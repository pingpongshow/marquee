package settings

import (
	"fmt"
	"net/netip"
	"net/url"
	"path"
	"path/filepath"
	"strings"
)

// ValidationError is returned for invalid admin input and maps to HTTP 400.
type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }

func invalid(format string, args ...any) error {
	return &ValidationError{Msg: fmt.Sprintf(format, args...)}
}

var validEncoders = map[string]bool{"nvenc": true, "qsv": true, "vaapi": true, "software": true}

func (s Settings) Validate() error {
	for _, src := range s.Integrations.LiveTVSources {
		if src.Kind != "m3u" && src.Kind != "dispatcharr" {
			return invalid("a Live TV source must be an M3U playlist or Dispatcharr")
		}
		if src.Name == "" || !(strings.HasPrefix(src.URL, "http://") || strings.HasPrefix(src.URL, "https://")) {
			return invalid("each Live TV source needs a name and an http(s) address")
		}
	}
	if s.Integrations.DVRPaddingBefore < 0 || s.Integrations.DVRPaddingBefore > 60 || s.Integrations.DVRPaddingAfter < 0 || s.Integrations.DVRPaddingAfter > 120 {
		return invalid("recording padding must be 0–60 minutes before and 0–120 after")
	}
	switch s.Security.PinSignIn {
	case "off", "local", "everywhere":
	default:
		return invalid("PIN sign-in must be off, local or everywhere")
	}
	if strings.TrimSpace(s.General.ServerName) == "" {
		return invalid("server name is required")
	}
	for _, c := range s.Network.LANSubnets {
		if _, err := netip.ParsePrefix(c); err != nil {
			return invalid("invalid LAN subnet %q", c)
		}
	}
	for _, u := range []string{s.Network.LANURL, s.RemoteAccess.RemoteURL, s.Integrations.BazarrURL} {
		if u == "" {
			continue
		}
		if p, err := url.Parse(u); err != nil || (p.Scheme != "http" && p.Scheme != "https") || p.Host == "" {
			return invalid("invalid URL %q (expected http:// or https://)", u)
		}
	}
	ra := s.RemoteAccess
	if ra.UploadSpeedKbps < 0 || ra.TotalRemoteLimitKbps < 0 || ra.PerStreamRemoteLimitKbps < 0 || ra.MinStreamKbps < 0 {
		return invalid("bandwidth values cannot be negative")
	}
	t := s.Transcoder
	if len(t.EncoderOrder) == 0 {
		return invalid("at least one encoder is required")
	}
	for _, e := range t.EncoderOrder {
		if !validEncoders[e] {
			return invalid("unknown encoder %q", e)
		}
	}
	if t.MaxConcurrentTranscodes < 1 {
		return invalid("max concurrent transcodes must be at least 1")
	}
	switch t.Preset {
	case "speed", "balanced", "quality":
	default:
		return invalid("unknown preset %q", t.Preset)
	}
	if t.ThrottleSegmentsAhead < 2 {
		return invalid("throttle must keep at least 2 segments ahead")
	}
	if t.NVENCSessions < 0 || t.NVENCSessions > 64 {
		return invalid("NVENC sessions must be between 0 and 64")
	}
	for _, r := range t.RemoteLadder {
		if r.MaxHeight <= 0 || r.VideoKbps <= 0 || r.Label == "" {
			return invalid("invalid quality rung %+v", r)
		}
	}
	for _, p := range s.Library.IgnorePatterns {
		if _, err := path.Match(p, ""); err != nil {
			return invalid("invalid ignore pattern %q", p)
		}
	}
	for _, r := range s.Library.BrowseRoots {
		if !filepath.IsAbs(r) {
			return invalid("browse root %q must be an absolute path", r)
		}
	}
	if w := s.Library.WatchedThresholdPercent; w < 50 || w > 100 {
		return invalid("watched threshold must be between 50 and 100")
	}
	if s.Metadata.OMDbDailyLimit < 1 {
		return invalid("OMDb daily limit must be at least 1")
	}
	switch s.Metadata.AnimeEpisodeOrdering {
	case "seasonal", "absolute":
	default:
		return invalid("unknown anime ordering %q", s.Metadata.AnimeEpisodeOrdering)
	}
	if _, err := parseClock(s.Tasks.MaintenanceWindowStart); err != nil {
		return invalid("maintenance window start must be HH:MM")
	}
	if h := s.Tasks.MaintenanceWindowHours; h < 1 || h > 12 {
		return invalid("maintenance window must be 1–12 hours")
	}
	if r := s.Tasks.BackupRetention; r < 1 || r > 365 {
		return invalid("backup retention must be 1–365 days")
	}
	if len(s.Webhooks) > 20 {
		return invalid("at most 20 webhooks")
	}
	for _, w := range s.Webhooks {
		u, err := url.Parse(w.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return invalid("webhook %q needs an http(s) URL", w.Name)
		}
		for _, e := range w.Events {
			if !validWebhookEvents[e] {
				return invalid("unknown webhook event %q", e)
			}
		}
	}
	return nil
}

var validWebhookEvents = map[string]bool{"playback.started": true, "playback.paused": true, "playback.resumed": true,
	"playback.stopped": true, "playback.watched": true, "library.added": true}

func parseClock(s string) (int, error) {
	var h, m int
	if n, err := fmt.Sscanf(s, "%d:%d", &h, &m); err != nil || n != 2 || len(s) != 5 || h > 23 || m > 59 || h < 0 || m < 0 {
		return 0, fmt.Errorf("bad clock %q", s)
	}
	return h*60 + m, nil
}
