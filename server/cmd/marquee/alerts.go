package main

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"marquee/internal/ai"
	"marquee/internal/auth"
	"marquee/internal/bazarr"
	"marquee/internal/config"
	"marquee/internal/library"
	"marquee/internal/livetv"
	"marquee/internal/notify"
	"marquee/internal/playback"
	"marquee/internal/requests"
	"marquee/internal/settings"
	"marquee/internal/soundprint"
	"marquee/internal/tasks"
	"marquee/internal/vod"
)

// Failed sign-ins for one account in the lockout window before an alert (the account is
// locked at 10).
const failedSignInAlert = 5

type alertDeps struct {
	cfg        *config.Config
	store      *settings.Store
	alerts     *notify.Alerts
	auth       *auth.Service
	scheduler  *tasks.Scheduler
	live       *livetv.Service
	dvr        *livetv.Recorder
	vod        *vod.Service
	player     *playback.Manager
	libraries  *library.Store
	ai         *ai.Client
	soundprint *soundprint.Service
}

// wireAlerts connects the places that can go wrong to the admin alerts and returns the
// watcher for disk space and services.
func wireAlerts(ctx context.Context, d alertDeps) *notify.Watcher {
	raise := func(a notify.Alert) { d.alerts.Raise(ctx, a) }
	d.scheduler.OnFailure = func(t tasks.Task, err error) {
		// Bounded tasks stop when the maintenance window ends; that's not a failure worth a mail.
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return
		}
		raise(notify.Alert{Type: settings.AlertTaskFailed, Subject: t.ID, Title: "Task failed: " + t.Name,
			Message: fmt.Sprintf("The task %q failed: %v", t.Name, err)})
	}
	d.live.OnSourceFailed = func(src settings.LiveTVSource, err error) {
		raise(notify.Alert{Type: settings.AlertSourceFailed, Subject: "livetv:" + src.ID, Title: "Live TV source failed: " + src.Name,
			Message: fmt.Sprintf("Couldn't load the Live TV source %q: %v", src.Name, err)})
	}
	d.vod.OnSourceFailed = func(src settings.VODSource, err error) {
		raise(notify.Alert{Type: settings.AlertSourceFailed, Subject: "vod:" + src.ID, Title: "Movie source failed: " + src.Name,
			Message: fmt.Sprintf("Couldn't load the movies-on-demand source %q: %v", src.Name, err)})
	}
	d.dvr.OnFailed = func(id int64, title, errMsg string) {
		if title == "" {
			title = "recording " + strconv.FormatInt(id, 10)
		}
		raise(notify.Alert{Type: settings.AlertRecordingFailed, Subject: strconv.FormatInt(id, 10), Title: "Recording failed: " + title,
			Message: fmt.Sprintf("The recording of %q failed: %s", title, errMsg)})
	}
	d.player.OnFailure = func(s *playback.Session, err error) {
		raise(notify.Alert{Type: settings.AlertPlaybackFailed, Subject: s.ID, Title: "Playback failed: " + s.Title,
			Message: fmt.Sprintf("The transcoder couldn't produce video of %q for %s on %s: %v", s.Title, s.UserName, s.DeviceName, err)})
	}
	d.auth.OnFailedLogin = func(username, ip string, failures int) {
		if failures < failedSignInAlert {
			return
		}
		raise(notify.Alert{Type: settings.AlertFailedSignIns, Subject: strings.ToLower(username), Title: "Repeated failed sign-ins: " + username,
			Message: fmt.Sprintf("%d wrong passwords for %q in the last 15 minutes, the latest from %s. After 10 the account is locked for 15 minutes.", failures, username, ip)})
	}
	d.auth.OnNewDevice = func(userID int64, dev auth.Device, ip string) {
		u, err := d.auth.GetUser(ctx, userID)
		if err != nil || !u.IsAdmin {
			return
		}
		raise(notify.Alert{Type: settings.AlertNewAdminDevice, Subject: strconv.FormatInt(userID, 10) + ":" + dev.ClientID,
			Title: "New sign-in for " + u.DisplayName,
			Message: fmt.Sprintf("The administrator %s signed in from a device they haven't used before: %s (%s) at %s. If that wasn't them, sign the device out in Settings → Devices and change the password.",
				u.DisplayName, dev.Name, dev.Platform, ip)})
	}

	cfg := d.cfg
	return &notify.Watcher{Alerts: d.alerts, Every: 15 * time.Minute,
		Volumes: func(ctx context.Context) []notify.Volume {
			vols := []notify.Volume{{Name: "Config", Path: cfg.ConfigDir}, {Name: "Transcode", Path: cfg.TranscodeDir},
				{Name: "Recordings", Path: cfg.RecordingsDir}}
			if libs, err := d.libraries.List(ctx); err == nil {
				for _, l := range libs {
					for _, p := range l.Paths {
						vols = append(vols, notify.Volume{Name: "Library " + l.Name, Path: p})
					}
				}
			}
			return vols
		},
		Probes: []notify.Probe{
			{Name: "Seerr", Enabled: func() bool { i := d.store.Get().Integrations; return i.SeerrURL != "" && i.SeerrAPIKey != "" },
				Check: func(ctx context.Context) error {
					i := d.store.Get().Integrations
					_, err := (&requests.Seerr{URL: i.SeerrURL, APIKey: i.SeerrAPIKey}).Status(ctx)
					return err
				}},
			{Name: "Bazarr", Enabled: func() bool { i := d.store.Get().Integrations; return i.BazarrURL != "" && i.BazarrAPIKey != "" },
				Check: func(ctx context.Context) error {
					i := d.store.Get().Integrations
					_, err := (&bazarr.Client{URL: i.BazarrURL, APIKey: i.BazarrAPIKey}).Status(ctx)
					return err
				}},
			{Name: "Ollama (AI)", Enabled: func() bool { return d.store.Get().AI.Enabled },
				Check: func(ctx context.Context) error {
					a := d.store.Get().AI
					st := d.ai.Check(ctx, ai.Config{Enabled: a.Enabled, URL: a.URL, ChatModel: a.ChatModel, EmbedModel: a.EmbedModel})
					if !st.Reachable {
						return errors.New(st.Error)
					}
					return nil
				}},
			{Name: "Soundprint", Enabled: func() bool { return d.store.Get().Music.SoundprintAnalysis },
				Check: func(ctx context.Context) error {
					_, err := d.soundprint.Client.Health(ctx)
					return err
				}},
		},
	}
}
