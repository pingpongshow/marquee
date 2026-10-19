package settings

import (
	"fmt"
	"net/mail"
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
	for _, src := range s.Integrations.VODSources {
		if strings.TrimSpace(src.Name) == "" || !(strings.HasPrefix(src.URL, "http://") || strings.HasPrefix(src.URL, "https://")) {
			return invalid("each movie source needs a name and an http(s) address")
		}
		switch src.Kind {
		case "", "m3u":
		case VODKindXtream:
			if strings.TrimSpace(src.Username) == "" || src.Password == "" {
				return invalid("the Xtream Codes source %q needs a username and password", src.Name)
			}
		default:
			return invalid("a movie source must be an M3U playlist or an Xtream Codes server")
		}
	}
	if s.Integrations.DVRPaddingBefore < 0 || s.Integrations.DVRPaddingBefore > 60 || s.Integrations.DVRPaddingAfter < 0 || s.Integrations.DVRPaddingAfter > 120 {
		return invalid("recording padding must be 0–60 minutes before and 0–120 after")
	}
	if h := s.Integrations.VODRefreshHours; h < 1 || h > 48 {
		return invalid("movie sources must sync every 1–48 hours")
	}
	switch s.Integrations.Commercials {
	case CommercialsOff, CommercialsDetect, CommercialsRemove:
	default:
		return invalid("commercials must be off, detect or remove")
	}
	if q := s.Integrations.DVRDefaultQuotaGB; q < 0 || q > 1_000_000 {
		return invalid("the recording quota must be 0 (unlimited) to 1,000,000 GB")
	}
	switch s.Integrations.DVRQuotaAction {
	case DVRQuotaBlock, DVRQuotaDeleteOldest:
	default:
		return invalid("the quota action must be block or deleteOldest")
	}
	if f := s.Integrations.DVRFolder; f != "" && !filepath.IsAbs(f) {
		return invalid("the recordings folder must be a full path, e.g. /recordings")
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
	if m := s.PlexSync.IntervalMinutes; m < 5 || m > 24*60 {
		return invalid("the Plex sync interval must be 5 minutes to 24 hours")
	}
	for _, m := range s.PlexSync.PathMappings {
		if strings.TrimSpace(m.Plex) == "" || strings.TrimSpace(m.Marquee) == "" {
			return invalid("each Plex folder mapping needs a Plex folder and a Marquee folder")
		}
	}
	for _, u := range []string{s.Network.LANURL, s.RemoteAccess.RemoteURL, s.Integrations.BazarrURL, s.PlexSync.URL} {
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
	if s.AI.Enabled || s.AI.URL != "" {
		if p, err := url.Parse(s.AI.URL); err != nil || (p.Scheme != "http" && p.Scheme != "https") || p.Host == "" {
			return invalid("the Ollama address must be an http(s) URL")
		}
	}
	if s.AI.Enabled && (strings.TrimSpace(s.AI.ChatModel) == "" || strings.TrimSpace(s.AI.EmbedModel) == "") {
		return invalid("choose a chat model and an embedding model")
	}
	if m := s.AI.UnloadMinutes; m < 0 || m > 1440 {
		return invalid("unload after must be 0–1440 minutes")
	}
	if err := s.Notifications.validate(); err != nil {
		return err
	}
	if err := s.VODSave.validate(); err != nil {
		return err
	}
	if err := s.Optimize.validate(); err != nil {
		return err
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
	"playback.stopped": true, "playback.watched": true, "library.added": true, "alert": true}

func (n Notifications) validate() error {
	switch n.SMTPSecurity {
	case "none", "starttls", "tls":
	default:
		return invalid("mail security must be none, starttls or tls")
	}
	if n.SMTPPort < 0 || n.SMTPPort > 65535 {
		return invalid("the mail server port must be 1–65535")
	}
	if strings.ContainsAny(n.SMTPHost, " /\r\n") {
		return invalid("the mail server must be a host name or address")
	}
	if n.SMTPHost != "" && n.SMTPPort == 0 {
		return invalid("the mail server needs a port")
	}
	for _, v := range []string{n.FromAddress, n.FromName, n.SMTPUsername} {
		if strings.ContainsAny(v, "\r\n") {
			return invalid("mail settings can't contain line breaks")
		}
	}
	if n.FromAddress != "" {
		if a, err := mail.ParseAddress(n.FromAddress); err != nil || a.Address != n.FromAddress {
			return invalid("the from address must be an email address like marquee@example.com")
		}
	}
	if n.LinkURL != "" {
		if p, err := url.Parse(n.LinkURL); err != nil || (p.Scheme != "http" && p.Scheme != "https") || p.Host == "" {
			return invalid("the link address must be an http(s) URL")
		}
	}
	valid := map[string]bool{}
	for _, t := range AlertTypes {
		valid[t] = true
	}
	for _, a := range n.Alerts {
		if !valid[a] {
			return invalid("unknown alert type %q", a)
		}
	}
	if t := n.DiskThresholdPercent; t < 1 || t > 50 {
		return invalid("the disk space threshold must be 1–50%%")
	}
	return nil
}

func parseClock(s string) (int, error) {
	var h, m int
	if n, err := fmt.Sscanf(s, "%d:%d", &h, &m); err != nil || n != 2 || len(s) != 5 || h > 23 || m > 59 || h < 0 || m < 0 {
		return 0, fmt.Errorf("bad clock %q", s)
	}
	return h*60 + m, nil
}

func (v VODSave) validate() error {
	for _, f := range []string{v.MoviesFolder, v.ShowsFolder} {
		if f != "" && !filepath.IsAbs(f) {
			return invalid("the folder %q to save to must be an absolute path", f)
		}
	}
	ids, names := map[string]bool{}, map[string]bool{}
	for _, d := range v.Destinations {
		name := strings.ToLower(strings.TrimSpace(d.Name))
		switch {
		case name == "":
			return invalid("each save folder needs a name")
		case names[name]:
			return invalid("there are two save folders named %q", strings.TrimSpace(d.Name))
		case d.ID == "" || ids[d.ID]:
			return invalid("each save folder needs its own id")
		case d.Folder == "":
			return invalid("the save folder %q needs a folder", d.Name)
		case !filepath.IsAbs(d.Folder):
			return invalid("the folder %q to save to must be an absolute path", d.Folder)
		}
		switch d.Kind {
		case "movies", "shows", "both":
		default:
			return invalid("the save folder %q must take movies, shows or both", d.Name)
		}
		ids[d.ID], names[name] = true, true
	}
	if v.FollowDefaultMaxGB < 0 || v.FollowDefaultMaxGB > 1_000_000 {
		return invalid("the size limit for followed series must be 0 (none) to 1,000,000 GB")
	}
	switch v.Quality {
	case VODSaveOriginal, VODSaveRemux, VODSaveTranscode:
	default:
		return invalid("saving quality must be original, remux or transcode")
	}
	switch v.MaxHeight {
	case 2160, 1080, 720, 480:
	default:
		return invalid("the resolution to save at must be 2160, 1080, 720 or 480")
	}
	switch v.VideoCodec {
	case "hevc", "h264":
	default:
		return invalid("the video codec to save with must be hevc or h264")
	}
	switch v.VideoQuality {
	case "high", "balanced", "small":
	default:
		return invalid("the video quality to save at must be high, balanced or small")
	}
	if v.VideoKbps < 0 || v.VideoKbps > 200000 {
		return invalid("the video bitrate to save at must be 0 (use the quality) to 200000 kbps")
	}
	switch v.Audio {
	case "copy", "aac_stereo", "aac_51":
	default:
		return invalid("the audio to save must be copy, aac_stereo or aac_51")
	}
	if c := v.ConcurrentPerSource; c < 1 || c > 5 {
		return invalid("saves at once per source must be 1–5")
	}
	switch v.PauseScope {
	case "source", "any":
	default:
		return invalid("pause while watching must be source or any")
	}
	return nil
}
