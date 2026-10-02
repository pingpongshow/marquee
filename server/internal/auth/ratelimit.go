package auth

import (
	"fmt"
	"sync"
	"time"
)

// Login lockout policy (WAN-5): after maxFailures failed attempts within window for the same
// IP or the same username, further attempts are refused until the window expires.
const (
	maxFailures = 10
	window      = 15 * time.Minute
)

type RateLimitedError struct{ RetryAfter time.Duration }

func (e *RateLimitedError) Error() string {
	return fmt.Sprintf("too many failed logins; retry in %s", e.RetryAfter.Round(time.Second))
}

type failures struct {
	count int
	first time.Time
}

type loginLimiter struct {
	max    int
	mu     sync.Mutex
	byIP   map[string]*failures
	byUser map[string]*failures
}

func newLoginLimiter() *loginLimiter { return newLimiter(maxFailures) }

// PINs have only 10,000 combinations, so they get a much lower limit.
func newLimiter(max int) *loginLimiter {
	return &loginLimiter{max: max, byIP: map[string]*failures{}, byUser: map[string]*failures{}}
}

func (l *loginLimiter) blocked(ip, user string) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	var wait time.Duration
	for _, f := range []*failures{l.byIP[ip], l.byUser[user]} {
		if f == nil || f.count < l.max {
			continue
		}
		if w := window - now.Sub(f.first); w > wait {
			wait = w
		}
	}
	return wait
}

func (l *loginLimiter) fail(ip, user string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	for _, entry := range []struct {
		m   map[string]*failures
		key string
	}{{l.byIP, ip}, {l.byUser, user}} {
		f := entry.m[entry.key]
		if f == nil || now.Sub(f.first) > window {
			f = &failures{first: now}
			entry.m[entry.key] = f
		}
		f.count++
	}
}

func (l *loginLimiter) success(ip, user string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.byIP, ip)
	delete(l.byUser, user)
}
