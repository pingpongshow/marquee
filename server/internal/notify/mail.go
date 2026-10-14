// Package notify sends email (SMTP) and raises admin alerts: an in-app list under the bell,
// mail to the chosen administrators and an "alert" webhook event.
package notify

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"marquee/internal/settings"
)

// ErrNotConfigured: no mail server is set up.
var ErrNotConfigured = errors.New("email isn't set up (Settings → Notifications)")

// MailTimeout bounds a whole delivery: connecting, TLS, authentication and sending.
const MailTimeout = 30 * time.Second

// Message is one email. HTML is optional; Text is always sent.
type Message struct {
	To      []string
	Subject string
	Text    string
	HTML    string
}

// Mailer sends mail through the SMTP server in the settings.
type Mailer struct {
	Settings *settings.Store
	// TLSConfig, when set, is used for TLS and STARTTLS (tests trust a self-signed server).
	TLSConfig *tls.Config
}

// Configured reports whether mail can be sent.
func (m *Mailer) Configured() bool {
	return m != nil && m.Settings != nil && m.Settings.Get().Notifications.MailConfigured()
}

// Send delivers msg with the saved settings.
func (m *Mailer) Send(ctx context.Context, msg Message) error {
	if !m.Configured() {
		return ErrNotConfigured
	}
	return m.SendWith(ctx, m.Settings.Get().Notifications, msg)
}

// SendWith delivers msg through the server described by n.
func (m *Mailer) SendWith(ctx context.Context, n settings.Notifications, msg Message) error {
	if !n.MailConfigured() {
		return ErrNotConfigured
	}
	if len(msg.To) == 0 {
		return errors.New("no recipients")
	}
	for _, to := range msg.To {
		if a, err := mail.ParseAddress(to); err != nil || a.Address != to {
			return fmt.Errorf("invalid recipient %q", to)
		}
	}
	body, err := build(n, msg)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, MailTimeout)
	defer cancel()
	addr := net.JoinHostPort(n.SMTPHost, strconv.Itoa(n.SMTPPort))
	tlsCfg := &tls.Config{ServerName: n.SMTPHost, MinVersion: tls.VersionTLS12}
	if m != nil && m.TLSConfig != nil {
		tlsCfg = m.TLSConfig.Clone()
		if tlsCfg.ServerName == "" {
			tlsCfg.ServerName = n.SMTPHost
		}
	}
	d := &net.Dialer{}
	var conn net.Conn
	if n.SMTPSecurity == "tls" {
		conn, err = (&tls.Dialer{NetDialer: d, Config: tlsCfg}).DialContext(ctx, "tcp", addr)
	} else {
		conn, err = d.DialContext(ctx, "tcp", addr)
	}
	if err != nil {
		return fmt.Errorf("connect to %s: %w", addr, err)
	}
	defer conn.Close()
	deadline, _ := ctx.Deadline()
	conn.SetDeadline(deadline)
	stop := context.AfterFunc(ctx, func() { conn.SetDeadline(time.Now()) })
	defer stop()

	c, err := smtp.NewClient(conn, n.SMTPHost)
	if err != nil {
		return fmt.Errorf("mail server: %w", err)
	}
	defer c.Close()
	if err := c.Hello("marquee"); err != nil {
		return fmt.Errorf("mail server: %w", err)
	}
	if n.SMTPSecurity == "starttls" {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return errors.New("the mail server doesn't offer STARTTLS; choose another security setting")
		}
		if err := c.StartTLS(tlsCfg); err != nil {
			return fmt.Errorf("STARTTLS: %w", err)
		}
	}
	if n.SMTPUsername != "" {
		if ok, _ := c.Extension("AUTH"); !ok {
			return errors.New("the mail server doesn't accept a sign-in; clear the username")
		}
		// PlainAuth refuses to send the password unencrypted (except to localhost).
		if err := c.Auth(smtp.PlainAuth("", n.SMTPUsername, n.SMTPPassword, n.SMTPHost)); err != nil {
			return fmt.Errorf("sign-in: %w", err)
		}
	}
	if err := c.Mail(n.FromAddress); err != nil {
		return fmt.Errorf("from address: %w", err)
	}
	for _, to := range msg.To {
		if err := c.Rcpt(to); err != nil {
			return fmt.Errorf("recipient %s: %w", to, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(body); err != nil {
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return c.Quit()
}

// clean drops line breaks so values can't add headers.
func clean(s string) string {
	return strings.Join(strings.Fields(strings.NewReplacer("\r", " ", "\n", " ").Replace(s)), " ")
}

// build renders the message: headers, then the text (and HTML) as quoted-printable.
func build(n settings.Notifications, msg Message) ([]byte, error) {
	var b bytes.Buffer
	from := (&mail.Address{Name: clean(n.FromName), Address: n.FromAddress}).String()
	id := make([]byte, 12)
	rand.Read(id)
	domain := n.FromAddress[strings.LastIndex(n.FromAddress, "@")+1:]
	hdr := func(k, v string) { fmt.Fprintf(&b, "%s: %s\r\n", k, v) }
	hdr("From", from)
	hdr("To", strings.Join(msg.To, ", "))
	hdr("Subject", mime.QEncoding.Encode("utf-8", clean(msg.Subject)))
	hdr("Date", time.Now().Format(time.RFC1123Z))
	hdr("Message-ID", "<"+hex.EncodeToString(id)+"@"+domain+">")
	hdr("MIME-Version", "1.0")
	hdr("Auto-Submitted", "auto-generated")
	part := func(ctype, content string) error {
		fmt.Fprintf(&b, "Content-Type: %s; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n\r\n", ctype)
		qp := quotedprintable.NewWriter(&b)
		if _, err := qp.Write([]byte(strings.ReplaceAll(content, "\n", "\r\n"))); err != nil {
			return err
		}
		if err := qp.Close(); err != nil {
			return err
		}
		b.WriteString("\r\n")
		return nil
	}
	if msg.HTML == "" {
		if err := part("text/plain", msg.Text); err != nil {
			return nil, err
		}
		return b.Bytes(), nil
	}
	boundary := "marquee-" + hex.EncodeToString(id)
	hdr("Content-Type", `multipart/alternative; boundary="`+boundary+`"`)
	b.WriteString("\r\n")
	for _, p := range [][2]string{{"text/plain", msg.Text}, {"text/html", msg.HTML}} {
		b.WriteString("--" + boundary + "\r\n")
		if err := part(p[0], p[1]); err != nil {
			return nil, err
		}
	}
	b.WriteString("--" + boundary + "--\r\n")
	return b.Bytes(), nil
}
