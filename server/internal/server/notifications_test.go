package server

import (
	"io"
	"mime/quotedprintable"
	"regexp"
	"strings"
	"testing"
	"time"

	"marquee/internal/api"
	"marquee/internal/notify/fakesmtp"
	"marquee/internal/settings"
)

// The failure alerts are on by default; optimize_failed is its own boolean (not an
// AlertType), kept when the list is replaced; the VOD sync settings round-trip.
func TestFailureAlertAndVODSyncSettings(t *testing.T) {
	h := newHarness(t)
	var res api.AuthResult
	h.do("POST", "/setup", map[string]any{"serverName": "T", "username": "admin", "password": "correct horse", "device": device}, &res)
	h.token = res.Token
	var s api.ServerSettings
	if code := h.do("GET", "/settings", nil, &s); code != 200 {
		t.Fatalf("get: %d", code)
	}
	n, i := s.Notifications, s.Integrations
	if !*n.OptimizeFailedAlert || len(*n.Alerts) != 1 || (*n.Alerts)[0] != api.AlertType(settings.AlertVODSaveFailed) {
		t.Errorf("defaults: %v %v", *n.OptimizeFailedAlert, *n.Alerts)
	}
	if !*i.VodAutoRefresh || *i.VodRefreshHours != 6 {
		t.Errorf("VOD sync defaults: %v %v", *i.VodAutoRefresh, *i.VodRefreshHours)
	}
	h.do("PATCH", "/settings", map[string]any{"notifications": map[string]any{"alerts": []string{"task_failed"}}}, &s)
	if !*s.Notifications.OptimizeFailedAlert || !h.store.Get().Notifications.AlertOn(settings.AlertOptimizeFailed) {
		t.Error("optimize_failed dropped when the list was replaced")
	}
	h.do("PATCH", "/settings", map[string]any{"notifications": map[string]any{"optimizeFailedAlert": false}}, &s)
	if *s.Notifications.OptimizeFailedAlert || h.store.Get().Notifications.AlertOn(settings.AlertOptimizeFailed) || len(*s.Notifications.Alerts) != 1 {
		t.Errorf("off: %v %v", *s.Notifications.OptimizeFailedAlert, *s.Notifications.Alerts)
	}
	h.do("PATCH", "/settings", map[string]any{"notifications": map[string]any{"optimizeFailedAlert": true}}, &s)
	if !*s.Notifications.OptimizeFailedAlert {
		t.Error("on again")
	}

	if code := h.do("PATCH", "/settings", map[string]any{"integrations": map[string]any{"vodAutoRefresh": false, "vodRefreshHours": 12}}, &s); code != 200 ||
		*s.Integrations.VodAutoRefresh || *s.Integrations.VodRefreshHours != 12 {
		t.Errorf("VOD sync: %d %v %v", code, *s.Integrations.VodAutoRefresh, *s.Integrations.VodRefreshHours)
	}
	if code := h.do("PATCH", "/settings", map[string]any{"integrations": map[string]any{"vodRefreshHours": 49}}, nil); code != 400 {
		t.Errorf("49 hours: %d", code)
	}
}

// TestForgotAndReset covers mail settings, email addresses, the forgot-password request
// (which never says whether an account exists) and resetting with the mailed link.
func TestForgotAndReset(t *testing.T) {
	h := newHarness(t)
	var res api.AuthResult
	h.do("POST", "/setup", map[string]any{"serverName": "T", "username": "admin", "password": "correct horse", "device": device}, &res)
	h.token = res.Token
	adminToken := res.Token
	smtp := fakesmtp.Start(t)

	// Mail settings: the password is write-only.
	var set api.ServerSettings
	if code := h.do("PATCH", "/settings", map[string]any{"notifications": map[string]any{
		"smtpHost": smtp.Host, "smtpPort": smtp.Port, "smtpSecurity": "none", "smtpUsername": "u", "smtpPassword": "p",
		"fromAddress": "marquee@example.test", "linkUrl": "https://media.example.test/",
		"alerts": []string{"task_failed"}}}, &set); code != 200 {
		t.Fatalf("settings: %d", code)
	}
	n := set.Notifications
	if n == nil || !*n.SmtpPasswordSet || !*n.MailConfigured || *n.LinkUrl != "https://media.example.test" || len(*n.Alerts) != 1 {
		t.Fatalf("notifications: %+v", n)
	}
	if code := h.do("PATCH", "/settings", map[string]any{"notifications": map[string]any{"alerts": []string{"bogus"}}}, nil); code != 400 {
		t.Fatalf("unknown alert type: %d", code)
	}
	// Webhooks can subscribe to alerts.
	if code := h.do("PATCH", "/settings", map[string]any{"webhooks": []map[string]any{{"name": "n", "url": "http://127.0.0.1:9/x",
		"events": []string{"alert"}, "enabled": false}}}, nil); code != 200 {
		t.Fatalf("alert webhook: %d", code)
	}

	// Test email.
	var test struct {
		Ok    bool    `json:"ok"`
		Error *string `json:"error"`
	}
	if code := h.do("POST", "/notifications/test-email", map[string]any{"to": "admin@example.test"}, &test); code != 200 || !test.Ok {
		t.Fatalf("test email: %d %+v", code, test)
	}
	smtp.Wait(1, 2*time.Second)

	// Emails: set your own; an admin sets another's; duplicates conflict.
	var me api.User
	if code := h.do("PATCH", "/me", map[string]any{"email": "admin@example.test"}, &me); code != 200 || me.Email == nil || *me.Email != "admin@example.test" {
		t.Fatalf("own email: %d %+v", code, me.Email)
	}
	if code := h.do("PATCH", "/me", map[string]any{"email": "not an email"}, nil); code != 400 {
		t.Fatalf("bad email: %d", code)
	}
	var sam api.User
	if code := h.do("POST", "/users", map[string]any{"username": "sam", "password": "sams-password", "email": "Sam@example.test"}, &sam); code != 201 {
		t.Fatalf("create: %d", code)
	}
	if code := h.do("POST", "/users", map[string]any{"username": "sam2", "password": "sams-password", "email": "sam@EXAMPLE.test"}, nil); code != 409 {
		t.Fatalf("duplicate email: %d", code)
	}
	var kid api.User
	h.do("POST", "/users", map[string]any{"username": "kid", "isManaged": true, "pin": "1111"}, &kid)
	if code := h.do("PATCH", "/users/"+itoa(kid.Id), map[string]any{"email": "kid@example.test"}, nil); code != 200 {
		t.Fatalf("admin sets email: %d", code)
	}
	h.do("POST", "/users", map[string]any{"username": "noemail", "password": "no-email-pw"}, nil)

	// Sam is signed in on a phone.
	h.token = ""
	var samSession api.AuthResult
	h.do("POST", "/auth/login", map[string]any{"username": "sam", "password": "sams-password",
		"device": map[string]any{"clientId": "sam-phone", "name": "Phone", "platform": "ios"}}, &samSession)

	// Every forgot request gets the same answer.
	before := len(smtp.Received())
	for _, req := range []map[string]any{
		{"login": "nobody", "kind": "password"},
		{"login": "noemail", "kind": "password"},
		{"login": "kid", "kind": "password"}, // managed profiles have no password
		{"login": "sam@example.test", "kind": "password"},
	} {
		if code := h.do("POST", "/auth/forgot", req, nil); code != 204 {
			t.Fatalf("forgot %v: %d", req, code)
		}
	}
	mail := smtp.Wait(before+1, 3*time.Second)
	time.Sleep(200 * time.Millisecond)
	mail = smtp.Received()
	if len(mail) != before+1 || mail[before].To[0] != "Sam@example.test" {
		t.Fatalf("expected one mail to Sam, got %d: %+v", len(mail)-before, mail[before:])
	}
	// The link uses the configured address, never the request's Host.
	decoded, _ := io.ReadAll(quotedprintable.NewReader(strings.NewReader(mail[before].Data)))
	m := regexp.MustCompile(`https://media\.example\.test/reset\?token=([A-Za-z0-9_-]+)`).FindStringSubmatch(string(decoded))
	if m == nil {
		t.Fatalf("no link in:\n%s", decoded)
	}
	token := m[1]
	if strings.Contains(mail[before].Data, h.srv.URL) {
		t.Fatal("the link used the request's address")
	}

	var info api.ResetInfo
	if code := h.do("POST", "/auth/reset/info", map[string]any{"token": token}, &info); code != 200 || info.Kind != "password" || info.DisplayName != "sam" {
		t.Fatalf("info: %d %+v (token %q)", code, info, token)
	}
	if code := h.do("POST", "/auth/reset", map[string]any{"token": token, "password": "short"}, nil); code != 400 {
		t.Fatalf("short password: %d", code)
	}
	if code := h.do("POST", "/auth/reset", map[string]any{"token": token, "password": "brand-new-password"}, nil); code != 204 {
		t.Fatalf("reset: %d", code)
	}
	if code := h.do("POST", "/auth/reset", map[string]any{"token": token, "password": "brand-new-password-2"}, nil); code != 400 {
		t.Fatalf("reused link: %d", code)
	}
	// Sam's phone was signed out; the new password works.
	h.token = samSession.Token
	if code := h.do("GET", "/me", nil, nil); code != 401 {
		t.Fatalf("old session still works: %d", code)
	}
	h.token = ""
	if code := h.do("POST", "/auth/login", map[string]any{"username": "sam", "password": "brand-new-password", "device": device}, nil); code != 200 {
		t.Fatalf("new password: %d", code)
	}
	// And a "was this you?" mail went out.
	if got := smtp.Wait(before+2, 3*time.Second); len(got) < before+2 || !strings.Contains(got[before+1].Data, "was changed") {
		t.Fatalf("no confirmation mail")
	}

	// Too many requests from one address are refused (the same for any login).
	limited := false
	for i := 0; i < 15; i++ {
		if h.do("POST", "/auth/forgot", map[string]any{"login": "nobody", "kind": "pin"}, nil) == 429 {
			limited = true
			break
		}
	}
	if !limited {
		t.Fatal("no rate limit")
	}

	// The bell: an alert raised by a task shows up for admins only.
	h.token = adminToken
	var list api.AdminNotificationList
	if code := h.do("GET", "/notifications", nil, &list); code != 200 || list.Unread != 0 {
		t.Fatalf("notifications: %d %+v", code, list)
	}
}
