// Package fakesmtp is an in-process SMTP server for tests: it accepts every message (and any
// AUTH PLAIN sign-in when no credentials are set) and keeps what it received.
package fakesmtp

import (
	"bufio"
	"encoding/base64"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// Mail is one received message.
type Mail struct {
	From string
	To   []string
	Data string
	User string // who signed in, if anyone
}

// Server listens on 127.0.0.1.
type Server struct {
	Host string
	Port int
	// Username and Password, when set, are required (AUTH PLAIN).
	Username, Password string

	ln   net.Listener
	mu   sync.Mutex
	mail []Mail
}

// Start runs a server until the test ends.
func Start(t testing.TB) *Server {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{ln: ln, Host: "127.0.0.1", Port: ln.Addr().(*net.TCPAddr).Port}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go s.serve(c)
		}
	}()
	return s
}

// Received returns the messages so far.
func (s *Server) Received() []Mail {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Mail(nil), s.mail...)
}

// Wait waits up to d for at least n messages and returns what arrived.
func (s *Server) Wait(n int, d time.Duration) []Mail {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if m := s.Received(); len(m) >= n {
			return m
		}
		time.Sleep(10 * time.Millisecond)
	}
	return s.Received()
}

func (s *Server) serve(c net.Conn) {
	defer c.Close()
	c.SetDeadline(time.Now().Add(10 * time.Second))
	r := bufio.NewReader(c)
	say := func(line string) { c.Write([]byte(line + "\r\n")) }
	say("220 fake ESMTP")
	var cur Mail
	authed := s.Username == ""
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		cmd := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(cmd, "EHLO"), strings.HasPrefix(cmd, "HELO"):
			say("250-fake")
			say("250-AUTH PLAIN")
			say("250 8BITMIME")
		case strings.HasPrefix(cmd, "AUTH PLAIN"):
			raw, _ := base64.StdEncoding.DecodeString(strings.TrimSpace(line[len("AUTH PLAIN"):]))
			parts := strings.Split(string(raw), "\x00")
			if len(parts) == 3 && (s.Username == "" || (parts[1] == s.Username && parts[2] == s.Password)) {
				authed = true
				cur.User = parts[1]
				say("235 ok")
			} else {
				say("535 bad credentials")
			}
		case strings.HasPrefix(cmd, "MAIL FROM:"):
			if !authed {
				say("530 sign in first")
				continue
			}
			cur.From = addr(line[len("MAIL FROM:"):])
			say("250 ok")
		case strings.HasPrefix(cmd, "RCPT TO:"):
			cur.To = append(cur.To, addr(line[len("RCPT TO:"):]))
			say("250 ok")
		case cmd == "DATA":
			say("354 go ahead")
			var b strings.Builder
			for {
				l, err := r.ReadString('\n')
				if err != nil {
					return
				}
				if l == ".\r\n" {
					break
				}
				b.WriteString(strings.TrimPrefix(l, "."))
			}
			cur.Data = b.String()
			s.mu.Lock()
			s.mail = append(s.mail, cur)
			s.mu.Unlock()
			cur = Mail{User: cur.User}
			say("250 queued as " + strconv.Itoa(len(s.Received())))
		case cmd == "RSET":
			cur = Mail{User: cur.User}
			say("250 ok")
		case cmd == "NOOP":
			say("250 ok")
		case cmd == "QUIT":
			say("221 bye")
			return
		default:
			say("502 not implemented")
		}
	}
}

// addr is the address in "<a@b> PARAM=…".
func addr(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, ">"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimPrefix(s, "<")
}
