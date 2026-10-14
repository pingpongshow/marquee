package notify

import (
	"fmt"
	"html"
	"strings"
)

// htmlMail is a plain, inline-styled HTML body: a title, paragraphs, an optional button and
// a small footer. Everything is escaped.
func htmlMail(title string, paras []string, linkText, linkURL, footer string) string {
	var b strings.Builder
	b.WriteString(`<!doctype html><html><body style="margin:0;padding:24px;background:#f4f4f5;font-family:-apple-system,Segoe UI,Roboto,Helvetica,Arial,sans-serif;color:#18181b">`)
	b.WriteString(`<div style="max-width:520px;margin:0 auto;background:#ffffff;border-radius:12px;padding:28px">`)
	fmt.Fprintf(&b, `<h1 style="font-size:20px;margin:0 0 16px">%s</h1>`, html.EscapeString(title))
	for _, p := range paras {
		if strings.TrimSpace(p) == "" {
			continue
		}
		fmt.Fprintf(&b, `<p style="font-size:15px;line-height:1.5;margin:0 0 14px;white-space:pre-wrap">%s</p>`, html.EscapeString(p))
	}
	if linkURL != "" {
		fmt.Fprintf(&b, `<p style="margin:22px 0"><a href="%s" style="display:inline-block;background:#e11d48;color:#ffffff;text-decoration:none;padding:10px 18px;border-radius:8px;font-weight:600">%s</a></p>`,
			html.EscapeString(linkURL), html.EscapeString(linkText))
		fmt.Fprintf(&b, `<p style="font-size:13px;color:#52525b;word-break:break-all;margin:0 0 14px">%s</p>`, html.EscapeString(linkURL))
	}
	if footer != "" {
		fmt.Fprintf(&b, `<p style="font-size:12px;color:#71717a;margin:24px 0 0">%s</p>`, html.EscapeString(footer))
	}
	b.WriteString(`</div></body></html>`)
	return b.String()
}

// ResetLinkMail is the mail with a password or PIN reset link.
func ResetLinkMail(to, server, name, kind, link string, minutes int) Message {
	what := "password"
	if kind == "pin" {
		what = "PIN"
	}
	intro := fmt.Sprintf("Hi %s,", name)
	body := fmt.Sprintf("Someone (hopefully you) asked to reset the %s for your %s account. The link below works once, for %d minutes.", what, server, minutes)
	ignore := fmt.Sprintf("If you didn't ask, ignore this mail: your %s stays as it is.", what)
	text := strings.Join([]string{intro, body, link, ignore}, "\n\n")
	return Message{To: []string{to}, Subject: fmt.Sprintf("Reset your %s %s", server, what), Text: text,
		HTML: htmlMail("Reset your "+what, []string{intro, body}, "Choose a new "+what, link, ignore)}
}

// ResetDoneMail tells someone their password or PIN was changed with a reset link.
func ResetDoneMail(to, server, name, kind string) Message {
	what := "password"
	if kind == "pin" {
		what = "PIN"
	}
	intro := fmt.Sprintf("Hi %s,", name)
	body := fmt.Sprintf("The %s for your %s account was just changed with a reset link, and every device was signed out.", what, server)
	warn := "If this wasn't you, tell the server's administrator straight away."
	text := strings.Join([]string{intro, body, warn}, "\n\n")
	return Message{To: []string{to}, Subject: fmt.Sprintf("Your %s %s was changed", server, what), Text: text,
		HTML: htmlMail("Your "+what+" was changed", []string{intro, body, warn}, "", "", "")}
}

// TestMail checks the mail settings.
func TestMail(to, server string) Message {
	body := "Email from " + server + " works. Password reset links and alerts will arrive like this."
	return Message{To: []string{to}, Subject: server + ": test email", Text: body,
		HTML: htmlMail("Test email", []string{body}, "", "", "")}
}
