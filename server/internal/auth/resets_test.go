package auth

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"marquee/internal/db"
)

func testService(t *testing.T) (*Service, *sql.DB) {
	t.Helper()
	dir := t.TempDir()
	database, err := db.Open(context.Background(), filepath.Join(dir, "t.db"), filepath.Join(dir, "b"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	return NewService(database), database
}

func TestResetLifecycle(t *testing.T) {
	s, database := testService(t)
	ctx := context.Background()
	u, err := s.Create(ctx, NewUser{Username: "sam", Password: "old-password", Email: "Sam@Example.test"})
	if err != nil {
		t.Fatal(err)
	}
	// Signed in on two devices.
	for _, c := range []string{"phone", "tv"} {
		if _, err := s.IssueToken(ctx, database, u.ID, Device{ClientID: c, Name: c, Platform: "web"}, "10.0.0.2"); err != nil {
			t.Fatal(err)
		}
	}
	// Found by username or by email, ignoring case.
	for _, login := range []string{"sam", "SAM", "sam@example.test"} {
		if f, err := s.FindAccount(ctx, login); err != nil || f.ID != u.ID {
			t.Fatalf("find %q: %v", login, err)
		}
	}
	if _, err := s.FindAccount(ctx, "nobody"); !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("unknown account: %v", err)
	}

	now := time.Now()
	first, _ := s.CreateReset(ctx, u.ID, ResetPassword, now)
	second, err := s.CreateReset(ctx, u.ID, ResetPassword, now)
	if err != nil || first == second || len(second) < 40 {
		t.Fatalf("tokens: %q %q %v", first, second, err)
	}
	// Only the hash is stored.
	var stored int
	database.QueryRow(`SELECT COUNT(*) FROM password_resets WHERE token_hash = ?`, second).Scan(&stored)
	if stored != 0 {
		t.Fatal("token stored in the clear")
	}
	if got, kind, err := s.ResetInfo(ctx, second, now); err != nil || got.ID != u.ID || kind != ResetPassword {
		t.Fatalf("info: %v %v", kind, err)
	}
	// Too short a password doesn't use up the link.
	if _, _, err := s.CompleteReset(ctx, second, "short", "", now); !errors.Is(err, ErrResetValue) {
		t.Fatalf("short password: %v", err)
	}
	// Expired after 30 minutes.
	if _, _, err := s.CompleteReset(ctx, second, "new-password", "", now.Add(31*time.Minute)); !errors.Is(err, ErrResetInvalid) {
		t.Fatalf("expired: %v", err)
	}
	if _, _, err := s.CompleteReset(ctx, second, "new-password", "", now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	// Single use, and the other link stops working too.
	if _, _, err := s.CompleteReset(ctx, second, "another-password", "", now.Add(2*time.Minute)); !errors.Is(err, ErrResetInvalid) {
		t.Fatalf("reused: %v", err)
	}
	if _, _, err := s.CompleteReset(ctx, first, "another-password", "", now.Add(2*time.Minute)); !errors.Is(err, ErrResetInvalid) {
		t.Fatalf("other token: %v", err)
	}
	// The new password works, the old one doesn't, and every session ended.
	if _, err := s.Authenticate(ctx, "10.0.0.3", "sam", "old-password"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("old password: %v", err)
	}
	if _, err := s.Authenticate(ctx, "10.0.0.3", "sam", "new-password"); err != nil {
		t.Fatalf("new password: %v", err)
	}
	var devices int
	database.QueryRow(`SELECT COUNT(*) FROM devices WHERE user_id = ?`, u.ID).Scan(&devices)
	if devices != 0 {
		t.Fatalf("%d sessions left", devices)
	}

	// A PIN link sets the PIN.
	pinTok, _ := s.CreateReset(ctx, u.ID, ResetPIN, now)
	if _, _, err := s.CompleteReset(ctx, pinTok, "", "12a4", now); !errors.Is(err, ErrResetValue) {
		t.Fatalf("bad PIN: %v", err)
	}
	if _, kind, err := s.CompleteReset(ctx, pinTok, "", "4321", now); err != nil || kind != ResetPIN {
		t.Fatalf("PIN reset: %v", err)
	}
	if _, err := s.PINSignIn(ctx, "10.0.0.3", u.ID, "4321"); err != nil {
		t.Fatalf("PIN sign-in: %v", err)
	}
	if _, _, err := s.ResetInfo(ctx, "not-a-token", now); !errors.Is(err, ErrResetInvalid) {
		t.Fatalf("unknown token: %v", err)
	}
}

func TestEmailUniqueIgnoringCase(t *testing.T) {
	s, _ := testService(t)
	ctx := context.Background()
	if _, err := s.Create(ctx, NewUser{Username: "a", Password: "password1", Email: "same@example.test"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create(ctx, NewUser{Username: "b", Password: "password1", Email: "SAME@example.test"}); !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("duplicate email: %v", err)
	}
	b, err := s.Create(ctx, NewUser{Username: "b", Password: "password1"})
	if err != nil {
		t.Fatal(err)
	}
	e := "Same@Example.test"
	if _, err := s.Update(ctx, b.ID, UserUpdate{Email: &e}); !errors.Is(err, ErrEmailTaken) {
		t.Fatalf("duplicate on update: %v", err)
	}
	none := ""
	if u, err := s.Update(ctx, b.ID, UserUpdate{Email: &none}); err != nil || u.Email != "" {
		t.Fatalf("clear: %v", err)
	}
	for _, bad := range []string{"no-at-sign", "a@b", "Name <a@example.test>", "a@example.test\r\nBcc: x@y.z"} {
		if _, err := NormalizeEmail(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestForgotRateLimit(t *testing.T) {
	s, _ := testService(t)
	for i := 0; i < 3; i++ {
		if ip, acct := s.ForgotAllowed("10.0.0.9", "user:1"); !ip || !acct {
			t.Fatalf("request %d refused", i)
		}
	}
	if ip, acct := s.ForgotAllowed("10.0.0.10", "user:1"); !ip || acct {
		t.Fatal("fourth request for the account allowed")
	}
	for i := 0; i < 7; i++ { // ten from this address so far
		s.ForgotAllowed("10.0.0.9", "")
	}
	if ip, _ := s.ForgotAllowed("10.0.0.9", ""); ip {
		t.Fatal("eleventh request from the address allowed")
	}
}

func TestFailedLoginAndNewDeviceHooks(t *testing.T) {
	s, database := testService(t)
	ctx := context.Background()
	u, _ := s.Create(ctx, NewUser{Username: "admin", Password: "password1", IsAdmin: true})
	var counts []int
	s.OnFailedLogin = func(_, _ string, n int) { counts = append(counts, n) }
	s.Authenticate(ctx, "10.0.0.1", "admin", "wrong")
	s.Authenticate(ctx, "10.0.0.1", "admin", "wrong")
	s.Authenticate(ctx, "10.0.0.1", "ghost", "wrong") // no such account: not reported
	if len(counts) != 2 || counts[1] != 2 {
		t.Fatalf("failures: %v", counts)
	}
	seen := make(chan string, 4)
	s.OnNewDevice = func(_ int64, d Device, _ string) { seen <- d.ClientID }
	issue := func(c string) {
		if _, err := s.IssueToken(ctx, database, u.ID, Device{ClientID: c, Name: c, Platform: "web"}, "10.0.0.1"); err != nil {
			t.Fatal(err)
		}
	}
	issue("first")  // the first device ever isn't news
	issue("first")  // nor is signing in again
	issue("second") // a new one is
	select {
	case c := <-seen:
		if c != "second" {
			t.Fatalf("new device %q", c)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("new device not reported")
	}
	select {
	case c := <-seen:
		t.Fatalf("also reported %q", c)
	case <-time.After(100 * time.Millisecond):
	}
}
