package notify

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"marquee/internal/db"
	"marquee/internal/notify/fakesmtp"
	"marquee/internal/settings"
)

func openStore(t *testing.T) (*sql.DB, *settings.Store) {
	t.Helper()
	dir := t.TempDir()
	database, err := db.Open(context.Background(), filepath.Join(dir, "t.db"), filepath.Join(dir, "b"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	store, err := settings.Open(context.Background(), database)
	if err != nil {
		t.Fatal(err)
	}
	return database, store
}

func useSMTP(t *testing.T, store *settings.Store, srv *fakesmtp.Server, mutate func(*settings.Notifications)) {
	t.Helper()
	if _, err := store.Update(context.Background(), func(s *settings.Settings) error {
		n := &s.Notifications
		n.SMTPHost, n.SMTPPort, n.SMTPSecurity = srv.Host, srv.Port, "none"
		n.FromAddress, n.FromName = "marquee@example.test", "Marquee Server"
		if mutate != nil {
			mutate(n)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestSendMail(t *testing.T) {
	_, store := openStore(t)
	srv := fakesmtp.Start(t)
	srv.Username, srv.Password = "mailer", "s3cret"
	useSMTP(t, store, srv, func(n *settings.Notifications) { n.SMTPUsername, n.SMTPPassword = "mailer", "s3cret" })
	m := &Mailer{Settings: store}
	msg := Message{To: []string{"admin@example.test"}, Subject: "Hello\r\nBcc: evil@example.test", Text: "Plain body", HTML: "<p>HTML body</p>"}
	if err := m.Send(context.Background(), msg); err != nil {
		t.Fatal(err)
	}
	got := srv.Received()
	if len(got) != 1 {
		t.Fatalf("got %d messages", len(got))
	}
	g := got[0]
	if g.From != "marquee@example.test" || len(g.To) != 1 || g.To[0] != "admin@example.test" || g.User != "mailer" {
		t.Fatalf("envelope: %+v", g)
	}
	for _, want := range []string{"Subject: Hello Bcc: evil@example.test", `From: "Marquee Server" <marquee@example.test>`,
		"multipart/alternative", "Plain body", "<p>HTML body</p>"} {
		if !strings.Contains(g.Data, want) {
			t.Errorf("message lacks %q:\n%s", want, g.Data)
		}
	}
	if strings.Contains(g.Data, "\r\nBcc:") {
		t.Error("a header was injected through the subject")
	}

	// A wrong password is reported.
	useSMTP(t, store, srv, func(n *settings.Notifications) { n.SMTPUsername, n.SMTPPassword = "mailer", "wrong" })
	if err := m.Send(context.Background(), msg); err == nil {
		t.Fatal("wrong password: no error")
	}
}

func TestSendNotConfiguredAndTimeout(t *testing.T) {
	_, store := openStore(t)
	m := &Mailer{Settings: store}
	if err := m.Send(context.Background(), Message{To: []string{"a@example.test"}}); !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("unconfigured: %v", err)
	}
	// A server that never answers times out with the context.
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	n := store.Get().Notifications
	n.SMTPHost, n.SMTPPort, n.SMTPSecurity, n.FromAddress = "10.255.255.1", 25, "none", "m@example.test"
	start := time.Now()
	if err := m.SendWith(ctx, n, Message{To: []string{"a@example.test"}, Text: "x"}); err == nil {
		t.Fatal("no error")
	}
	if time.Since(start) > 5*time.Second {
		t.Fatal("didn't time out")
	}
}

func addAdmin(t *testing.T, database *sql.DB, name, email string, admin bool) int64 {
	t.Helper()
	var e any
	if email != "" {
		e = email
	}
	r, err := database.Exec(`INSERT INTO users (username, display_name, is_admin, email) VALUES (?, ?, ?, ?)`, name, name, admin, e)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := r.LastInsertId()
	return id
}

func countNotifications(t *testing.T, database *sql.DB) int {
	var n int
	database.QueryRow(`SELECT COUNT(*) FROM notifications`).Scan(&n)
	return n
}

// Every alert reaches the bell; the types chosen for email are also mailed. Duplicates within
// the hour are dropped.
func TestAlertsBellEmailDedup(t *testing.T) {
	database, store := openStore(t)
	srv := fakesmtp.Start(t)
	useSMTP(t, store, srv, nil)
	a1 := addAdmin(t, database, "alice", "alice@example.test", true)
	addAdmin(t, database, "bob", "bob@example.test", true)
	addAdmin(t, database, "carol", "", true)
	addAdmin(t, database, "dave", "dave@example.test", false)
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	a := &Alerts{DB: database, Settings: store, Mail: &Mailer{Settings: store}, Now: func() time.Time { return now }, Sync: true}
	ctx := context.Background()
	al := Alert{Type: settings.AlertTaskFailed, Subject: "backup", Title: "Task failed: Backup", Message: "disk full"}

	// Not chosen for email (the default): it reaches the bell, but no mail goes out.
	if !a.Raise(ctx, Alert{Type: settings.AlertTaskFailed, Subject: "scan", Title: "Task failed: Scan"}) || countNotifications(t, database) != 1 || len(srv.Received()) != 0 {
		t.Fatal("an alert not chosen for email should reach the bell only")
	}
	store.Update(ctx, func(s *settings.Settings) error {
		s.Notifications.Alerts = []string{settings.AlertTaskFailed}
		return nil
	})
	if !a.Raise(ctx, al) {
		t.Fatal("not raised")
	}
	mail := srv.Received()
	if len(mail) != 1 || strings.Join(mail[0].To, ",") != "alice@example.test,bob@example.test" {
		t.Fatalf("mail: %+v", mail)
	}
	if !strings.Contains(mail[0].Data, "Task failed: Backup") {
		t.Fatalf("subject missing: %s", mail[0].Data)
	}
	// The same alert within the hour is dropped; another subject isn't.
	now = now.Add(30 * time.Minute)
	if a.Raise(ctx, al) {
		t.Fatal("duplicate raised")
	}
	if !a.Raise(ctx, Alert{Type: settings.AlertTaskFailed, Subject: "optimize", Title: "Task failed: Optimize"}) {
		t.Fatal("other subject dropped")
	}
	now = now.Add(31 * time.Minute)
	if !a.Raise(ctx, al) {
		t.Fatal("not raised again after an hour")
	}
	if n := countNotifications(t, database); n != 4 {
		t.Fatalf("stored %d", n)
	}
	// Only the chosen admins get mail.
	store.Update(ctx, func(s *settings.Settings) error {
		s.Notifications.RecipientsAllAdmins, s.Notifications.Recipients = false, []int64{a1}
		return nil
	})
	if to, _ := a.Recipients(ctx); len(to) != 1 || to[0] != "alice@example.test" {
		t.Fatalf("recipients: %v", to)
	}

	// The list: unread count, mark read, mark all, dismiss, prune.
	list, unread, err := a.List(ctx, 10)
	if err != nil || len(list) != 4 || unread != 4 || list[0].Subject != "backup" {
		t.Fatalf("list: %v %d %+v", err, unread, list)
	}
	if err := a.MarkRead(ctx, list[0].ID); err != nil {
		t.Fatal(err)
	}
	if _, unread, _ = a.List(ctx, 10); unread != 3 {
		t.Fatalf("unread after one read: %d", unread)
	}
	if err := a.Dismiss(ctx, list[1].ID); err != nil {
		t.Fatal(err)
	}
	if err := a.Dismiss(ctx, list[1].ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("dismiss twice: %v", err)
	}
	a.MarkRead(ctx, 0)
	if _, unread, _ = a.List(ctx, 10); unread != 0 {
		t.Fatalf("unread after all read: %d", unread)
	}
	now = now.Add(31 * 24 * time.Hour)
	a.Prune(ctx)
	if n := countNotifications(t, database); n != 0 {
		t.Fatalf("after prune: %d", n)
	}
}

func TestWatcherDiskAndProbes(t *testing.T) {
	database, store := openStore(t)
	ctx := context.Background()
	store.Update(ctx, func(s *settings.Settings) error {
		s.Notifications.Alerts = []string{settings.AlertDiskLow, settings.AlertIntegrationDown}
		s.Notifications.DiskThresholdPercent = 10
		return nil
	})
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	a := &Alerts{DB: database, Settings: store, Now: func() time.Time { return now }}
	free := 50.0
	up := true
	dir := t.TempDir()
	w := &Watcher{Alerts: a,
		Volumes: func(context.Context) []Volume {
			return []Volume{{Name: "Config", Path: dir}, {Name: "Transcode", Path: dir}}
		},
		FreePercent: func(string) (float64, error) { return free, nil },
		Probes: []Probe{{Name: "Seerr", Check: func(context.Context) error {
			if up {
				return nil
			}
			return errors.New("connection refused")
		}}},
	}
	titles := func() []string {
		list, _, _ := a.List(ctx, 50)
		var out []string
		for i := len(list) - 1; i >= 0; i-- {
			out = append(out, list[i].Title)
		}
		return out
	}
	w.Check(ctx)
	if got := titles(); len(got) != 0 {
		t.Fatalf("healthy: %v", got)
	}
	free, up = 5, false
	w.Check(ctx) // disk low once (both folders share a volume); Seerr failed once
	now = now.Add(15 * time.Minute)
	w.Check(ctx) // Seerr failed twice: reported; the disk stays low: not repeated
	now = now.Add(15 * time.Minute)
	w.Check(ctx)
	free, up = 40, true
	now = now.Add(15 * time.Minute)
	w.Check(ctx)
	want := []string{"Disk space low: Config", "Seerr isn't answering", "Disk space OK again: Config", "Seerr is answering again"}
	if got := titles(); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestFreePercent(t *testing.T) {
	pct, err := freePercent(t.TempDir())
	if err != nil || pct <= 0 || pct > 100 {
		t.Fatalf("%v %v", pct, err)
	}
}
