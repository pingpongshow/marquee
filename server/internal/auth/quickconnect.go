package auth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"sync"
	"time"
)

// Quick Connect (USER-4): a TV shows a short code, a signed-in user approves it, and the TV
// picks up a token for that user. Requests live in memory for a few minutes.

var (
	ErrQuickConnectUnknown = errors.New("that code isn't valid or has expired")
	ErrQuickConnectBusy    = errors.New("too many codes are waiting; try again in a minute")
)

const (
	quickConnectTTL     = 10 * time.Minute
	quickConnectPending = 200
	codeAlphabet        = "ABCDEFGHJKLMNPQRSTUVWXYZ23456789" // no 0/O/1/I
)

type QuickConnectRequest struct {
	Code, Secret string
	Device       Device
	IP           string
	Expires      time.Time
	// Set when approved; the token is handed out once.
	UserID   int64
	Approved bool
}

type QuickConnect struct {
	mu   sync.Mutex
	reqs map[string]*QuickConnectRequest // by secret
}

func randomString(alphabet string, n int) string {
	b := make([]byte, n)
	rand.Read(b)
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b)
}

func (q *QuickConnect) prune(now time.Time) {
	for k, r := range q.reqs {
		if now.After(r.Expires) {
			delete(q.reqs, k)
		}
	}
}

// Start creates a pending request for a device.
func (q *QuickConnect) Start(d Device, ip string) (*QuickConnectRequest, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.reqs == nil {
		q.reqs = map[string]*QuickConnectRequest{}
	}
	now := time.Now()
	q.prune(now)
	if len(q.reqs) >= quickConnectPending {
		return nil, ErrQuickConnectBusy
	}
	secret := make([]byte, 32)
	rand.Read(secret)
	r := &QuickConnectRequest{Code: randomString(codeAlphabet, 6), Secret: hex.EncodeToString(secret), Device: d, IP: ip, Expires: now.Add(quickConnectTTL)}
	q.reqs[r.Secret] = r
	return r, nil
}

// Approve links the request with this code to userID and returns the device asking.
func (q *QuickConnect) Approve(code string, userID int64) (Device, error) {
	code = strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(code), " ", ""))
	q.mu.Lock()
	defer q.mu.Unlock()
	q.prune(time.Now())
	for _, r := range q.reqs {
		if r.Code == code && !r.Approved {
			r.Approved, r.UserID = true, userID
			return r.Device, nil
		}
	}
	return Device{}, ErrQuickConnectUnknown
}

// Poll returns the request for a secret. An approved request is removed, so its token is
// issued only once.
func (q *QuickConnect) Poll(secret string) (QuickConnectRequest, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.prune(time.Now())
	r, ok := q.reqs[secret]
	if !ok {
		return QuickConnectRequest{}, false
	}
	if r.Approved {
		delete(q.reqs, secret)
	}
	return *r, true
}
