package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"marquee/internal/auth"
	"marquee/internal/config"
	"marquee/internal/db"
	"marquee/internal/livetv"
	"marquee/internal/notify"
	"marquee/internal/notify/fakesmtp"
	"marquee/internal/optimize"
	"marquee/internal/playback"
	"marquee/internal/settings"
	"marquee/internal/tasks"
	"marquee/internal/vod"
	"marquee/internal/vodsave"
)

// A failed VOD download and a failed Large files conversion each raise their alert with the
// default settings (both on): stored for the bell and mailed, one per item.
func TestFailureAlertsOnByDefault(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	database, err := db.Open(ctx, filepath.Join(dir, "t.db"), filepath.Join(dir, "b"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	store, err := settings.Open(ctx, database)
	if err != nil {
		t.Fatal(err)
	}
	srv := fakesmtp.Start(t)
	store.Update(ctx, func(s *settings.Settings) error {
		n := &s.Notifications
		n.SMTPHost, n.SMTPPort, n.SMTPSecurity, n.FromAddress = srv.Host, srv.Port, "none", "marquee@example.test"
		s.RemoteAccess.RemoteURL = "https://media.example.test"
		return nil
	})
	if _, err := database.Exec(`INSERT INTO users (username, display_name, is_admin, email) VALUES ('a', 'A', 1, 'a@example.test')`); err != nil {
		t.Fatal(err)
	}
	alerts := &notify.Alerts{DB: database, Settings: store, Mail: &notify.Mailer{Settings: store}, Sync: true}
	d := alertDeps{cfg: &config.Config{}, store: store, alerts: alerts, auth: &auth.Service{}, scheduler: &tasks.Scheduler{},
		live: &livetv.Service{}, dvr: &livetv.Recorder{}, vod: &vod.Service{}, saves: &vodsave.Service{},
		optimizer: &optimize.Service{}, player: &playback.Manager{}}
	wireAlerts(ctx, d)

	d.saves.OnFailed(vodsave.Save{ID: 7, Title: "Alpha", Error: "the source answered 404"})
	d.optimizer.OnFailed(optimize.Failure{JobID: 3, FileID: 11, Title: "Beta", Error: "Couldn't re-encode it (the original is unchanged): exit 1"})
	// The same file failing again within the hour is one alert.
	d.optimizer.OnFailed(optimize.Failure{JobID: 4, FileID: 11, Title: "Beta", Error: "again"})

	rows, err := database.Query(`SELECT type, subject, title, message FROM notifications ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got [][4]string
	for rows.Next() {
		var r [4]string
		rows.Scan(&r[0], &r[1], &r[2], &r[3])
		got = append(got, r)
	}
	if len(got) != 2 {
		t.Fatalf("notifications: %v", got)
	}
	if got[0][0] != settings.AlertVODSaveFailed || got[0][1] != "vodsave:7" || !strings.Contains(got[0][2], "Alpha") {
		t.Errorf("VOD save alert: %v", got[0])
	}
	if got[1][0] != settings.AlertOptimizeFailed || got[1][1] != "optimize:11" || !strings.Contains(got[1][2], "Beta") ||
		!strings.Contains(got[1][3], "exit 1") || !strings.Contains(got[1][3], "https://media.example.test/settings/library-health#large-files") {
		t.Errorf("optimize alert: %v", got[1])
	}
	if mail := srv.Received(); len(mail) != 2 {
		t.Errorf("mailed %d", len(mail))
	}
}
