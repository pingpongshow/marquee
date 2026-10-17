package settings

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"slices"
	"testing"

	"marquee/internal/db"
)

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	dir := t.TempDir()
	d, err := db.Open(context.Background(), filepath.Join(dir, "t.db"), filepath.Join(dir, "b"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

// New servers have the VOD download and Large files conversion failure alerts on.
func TestDefaultAlertsOnNewServer(t *testing.T) {
	s, err := Open(context.Background(), openDB(t))
	if err != nil {
		t.Fatal(err)
	}
	n := s.Get().Notifications
	if !n.AlertOn(AlertVODSaveFailed) || !n.AlertOn(AlertOptimizeFailed) || n.AlertsVersion != AlertsVersion {
		t.Errorf("new server alerts: %v (version %d)", n.Alerts, n.AlertsVersion)
	}
	if i := s.Get().Integrations; !i.VODAutoRefresh || i.VODRefreshHours != 6 {
		t.Errorf("VOD sync defaults: %v %d", i.VODAutoRefresh, i.VODRefreshHours)
	}
}

// Existing servers get the two alerts added once; an admin turning one off later keeps it
// off across restarts.
func TestAlertsUpgradeOnce(t *testing.T) {
	ctx := context.Background()
	d := openDB(t)
	// Settings saved before alert versions existed, with one alert on.
	old := map[string]any{"notifications": map[string]any{"alerts": []string{AlertTaskFailed}, "diskThresholdPercent": 10, "smtpPort": 587}}
	raw, _ := json.Marshal(old)
	if _, err := d.Exec(`INSERT INTO settings(key, value) VALUES (?, ?)`, keyServer, string(raw)); err != nil {
		t.Fatal(err)
	}
	s, err := Open(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	n := s.Get().Notifications
	if !slices.Equal(n.Alerts, []string{AlertTaskFailed, AlertVODSaveFailed, AlertOptimizeFailed}) || n.AlertsVersion != 2 {
		t.Fatalf("upgraded: %v (version %d)", n.Alerts, n.AlertsVersion)
	}
	// Persisted: the stored settings carry the version.
	var stored string
	d.QueryRow(`SELECT value FROM settings WHERE key = ?`, keyServer).Scan(&stored)
	var back Settings
	json.Unmarshal([]byte(stored), &back)
	if back.Notifications.AlertsVersion != 2 || !back.Notifications.AlertOn(AlertOptimizeFailed) {
		t.Errorf("not persisted: %s", stored)
	}

	// The admin turns both off; a restart doesn't turn them on again.
	if _, err := s.Update(ctx, func(st *Settings) error {
		st.Notifications.Alerts = []string{AlertTaskFailed}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	s2, err := Open(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	if n := s2.Get().Notifications; !slices.Equal(n.Alerts, []string{AlertTaskFailed}) {
		t.Errorf("re-added after opt-out: %v", n.Alerts)
	}
}

func TestVODRefreshHoursValidated(t *testing.T) {
	s, err := Open(context.Background(), openDB(t))
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range []int{0, 49} {
		if _, err := s.Update(context.Background(), func(st *Settings) error { st.Integrations.VODRefreshHours = h; return nil }); err == nil {
			t.Errorf("%d hours accepted", h)
		}
	}
	if _, err := s.Update(context.Background(), func(st *Settings) error { st.Integrations.VODRefreshHours = 48; return nil }); err != nil {
		t.Errorf("48 hours: %v", err)
	}
}
