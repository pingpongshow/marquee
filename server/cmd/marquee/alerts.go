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
	"marquee/internal/optimize"
	"marquee/internal/playback"
	"marquee/internal/requests"
	"marquee/internal/settings"
	"marquee/internal/soundprint"
	"marquee/internal/tasks"
	"marquee/internal/vod"
	"marquee/internal/vodsave"
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
	saves      *vodsave.Service
	optimizer  *optimize.Service
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
			Message: fmt.Sprintf("Couldn't load the VOD source %q: %v", src.Name, err)})
	}
	d.saves.OnFailed = func(v vodsave.Save) {
		raise(notify.Alert{Type: settings.AlertVODSaveFailed, Subject: "vodsave:" + strconv.FormatInt(v.ID, 10),
			Title: "Saving failed: " + v.Title, Message: fmt.Sprintf("Saving %q into the library failed: %s", v.Title, v.Error)})
	}
	d.saves.OnDrained = func(done, failed int, titles []string) {
		msg := fmt.Sprintf("%d saved into the library", done)
		if failed > 0 {
			msg += fmt.Sprintf(", %d failed (see VOD → Saves)", failed)
		}
		if len(titles) > 0 {
			msg += ": " + strings.Join(titles, ", ")
			if done > len(titles) {
				msg += ", …"
			}
		}
		raise(notify.Alert{Type: settings.AlertVODSaveDone, Subject: "vodsave:" + time.Now().UTC().Format(time.RFC3339),
			Title: "Saving finished", Message: msg + "."})
	}
	if d.optimizer != nil {
		d.optimizer.OnFailed = func(f optimize.Failure) { raise(optimizeFailedAlert(d.store.Get(), f)) }
	}
	d.dvr.OnFailed = func(id int64, title, errMsg string) {
		if title == "" {
			title = "recording " + strconv.FormatInt(id, 10)
		}
		raise(notify.Alert{Type: settings.AlertRecordingFailed, Subject: strconv.FormatInt(id, 10), Title: "Recording failed: " + title,
			Message: fmt.Sprintf("The recording of %q failed: %s", title, errMsg)})
	}
	d.dvr.OnQuota = func(userID int64, owner, title, msg string) {
		raise(notify.Alert{Type: settings.AlertDVRQuota, Subject: "dvr-quota:" + strconv.FormatInt(userID, 10) + ":" + title,
			Title: "Over DVR quota: " + owner, Message: msg})
	}
	d.saves.OnFollowQuota = func(userID int64, owner, title, msg string) {
		raise(notify.Alert{Type: settings.AlertDVRQuota, Subject: "vod-follow-quota:" + strconv.FormatInt(userID, 10) + ":" + title,
			Title: "Over DVR quota: " + owner, Message: msg})
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
		Volumes: func(ctx context.Context) []notify.Volume { return alertVolumes(ctx, cfg, d.libraries) },
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

// optimizeFailedAlert is the alert for a failed Large files conversion: one per file.
func optimizeFailedAlert(cfg settings.Settings, f optimize.Failure) notify.Alert {
	title := f.Title
	if title == "" {
		title = f.Path
	}
	where := "Settings → Library Health → Large files"
	if base := cfg.LinkBase(); base != "" {
		where = base + "/settings/library-health#large-files"
	}
	subject := "optimize:" + strconv.FormatInt(f.FileID, 10)
	if f.FileID == 0 {
		subject = "optimize-job:" + strconv.FormatInt(f.JobID, 10)
	}
	return notify.Alert{Type: settings.AlertOptimizeFailed, Subject: subject, Title: "Large file conversion failed: " + title,
		Message: fmt.Sprintf("Converting %q failed: %s. See %s to retry it.", title, strings.TrimRight(f.Error, "."), where)}
}
