package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"net/http"
	"strconv"
	"strings"
)

// Image keys (D85): artwork, people photos and avatars are loaded by URLs that players,
// notifications, Android Auto, CarPlay and Cast devices fetch themselves. Those URLs carry an
// image key rather than the sign-in token: "<user id>.<HMAC>", accepted only for GET requests
// to image endpoints, so a key that leaks shows pictures but grants nothing else.

const imageSecretKey = "image_key_secret"

func (s *Service) imageSecret(ctx context.Context) ([]byte, error) {
	s.seenMu.Lock()
	defer s.seenMu.Unlock()
	if s.imgSecret != nil {
		return s.imgSecret, nil
	}
	var hexed string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, imageSecretKey).Scan(&hexed)
	if errors.Is(err, sql.ErrNoRows) {
		b := make([]byte, 32)
		rand.Read(b)
		hexed = hex.EncodeToString(b)
		if _, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO settings(key, value) VALUES (?, ?)`, imageSecretKey, hexed); err != nil {
			return nil, err
		}
		// Another start may have won the race; use what's stored.
		if err := s.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, imageSecretKey).Scan(&hexed); err != nil {
			return nil, err
		}
	} else if err != nil {
		return nil, err
	}
	b, err := hex.DecodeString(hexed)
	if err != nil {
		return nil, err
	}
	s.imgSecret = b
	return b, nil
}

func imageMAC(secret []byte, userID int64) string {
	m := hmac.New(sha256.New, secret)
	m.Write([]byte("images:" + strconv.FormatInt(userID, 10)))
	return hex.EncodeToString(m.Sum(nil))[:32]
}

// ImageKey is the user's key for image URLs.
func (s *Service) ImageKey(ctx context.Context, userID int64) string {
	secret, err := s.imageSecret(ctx)
	if err != nil {
		return ""
	}
	return strconv.FormatInt(userID, 10) + "." + imageMAC(secret, userID)
}

// ResolveImageKey turns a valid image key into a session for that user (image requests only).
func (s *Service) ResolveImageKey(ctx context.Context, key string) (Session, bool) {
	idPart, mac, ok := strings.Cut(key, ".")
	id, err := strconv.ParseInt(idPart, 10, 64)
	if !ok || err != nil {
		return Session{}, false
	}
	secret, err := s.imageSecret(ctx)
	if err != nil || !hmac.Equal([]byte(mac), []byte(imageMAC(secret, id))) {
		return Session{}, false
	}
	u, err := s.GetUser(ctx, id)
	if err != nil {
		return Session{}, false
	}
	return Session{User: u}, true
}

// IsImageRequest reports whether a request may be authorised by an image key: a GET for
// artwork, a person's photo or a user's avatar.
func IsImageRequest(r *http.Request) bool {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	p := r.URL.Path
	switch {
	case strings.HasPrefix(p, "/api/v1/images/"):
		return true
	case strings.HasPrefix(p, "/api/v1/people/") && strings.HasSuffix(p, "/photo"):
		return true
	case strings.HasPrefix(p, "/api/v1/users/") && strings.HasSuffix(p, "/avatar"):
		return true
	}
	return false
}
