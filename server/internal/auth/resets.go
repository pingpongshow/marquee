package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"strings"
	"time"
)

// Password and PIN resets by email: a single-use link, valid for ResetTTL. Only the token's
// SHA-256 is stored.

// ResetTTL is how long a reset link works.
const ResetTTL = 30 * time.Minute

// Reset kinds.
const (
	ResetPassword = "password"
	ResetPIN      = "pin"
)

var (
	// ErrResetInvalid: the link is unknown, used or expired.
	ErrResetInvalid = errors.New("this reset link has expired or was already used; ask for a new one")
	// ErrResetValue: the new password or PIN is missing or doesn't meet the rules.
	ErrResetValue = errors.New("enter a new password of at least 8 characters, or a 4-digit PIN")
)

const timeFmt = "2006-01-02T15:04:05.000Z"

// ForgotAllowed rate-limits reset requests: per address (ip) and per account (key, e.g. the
// user id). It reports false when either has asked too often in the last 15 minutes, and
// otherwise counts this request.
func (s *Service) ForgotAllowed(ip, key string) (ipOK, accountOK bool) {
	if s.forgotIP.blocked(ip, "") > 0 {
		return false, true
	}
	s.forgotIP.fail(ip, "")
	if key == "" {
		return true, true
	}
	if s.forgotUser.blocked("", key) > 0 {
		return true, false
	}
	s.forgotUser.fail("", key)
	return true, true
}

// FindAccount finds a user by username or email address (ignoring case).
func (s *Service) FindAccount(ctx context.Context, login string) (User, error) {
	login = strings.TrimSpace(login)
	if login == "" {
		return User{}, ErrUserNotFound
	}
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM users WHERE username = ? OR (email IS NOT NULL AND email = ? COLLATE NOCASE)
		ORDER BY username = ? DESC LIMIT 1`, login, login, login).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrUserNotFound
	}
	if err != nil {
		return User{}, err
	}
	return getUser(ctx, s.db, id)
}

// CreateReset makes a reset token for a user and returns it (the only copy).
func (s *Service) CreateReset(ctx context.Context, userID int64, kind string, now time.Time) (string, error) {
	if kind != ResetPassword && kind != ResetPIN {
		return "", errors.New("unknown reset kind")
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	// Old, expired links aren't needed any more.
	s.db.ExecContext(ctx, `DELETE FROM password_resets WHERE expires_at < ?`, now.UTC().Add(-24*time.Hour).Format(timeFmt))
	_, err := s.db.ExecContext(ctx, `INSERT INTO password_resets (user_id, token_hash, kind, expires_at) VALUES (?, ?, ?, ?)`,
		userID, hashToken(token), kind, now.UTC().Add(ResetTTL).Format(timeFmt))
	return token, err
}

// ResetInfo describes a usable reset token: whose it is and what it resets.
func (s *Service) ResetInfo(ctx context.Context, token string, now time.Time) (User, string, error) {
	var uid int64
	var kind string
	err := s.db.QueryRowContext(ctx, `SELECT user_id, kind FROM password_resets WHERE token_hash = ? AND used_at IS NULL AND expires_at > ?`,
		hashToken(token), now.UTC().Format(timeFmt)).Scan(&uid, &kind)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, "", ErrResetInvalid
	}
	if err != nil {
		return User{}, "", err
	}
	u, err := getUser(ctx, s.db, uid)
	if errors.Is(err, ErrUserNotFound) {
		return User{}, "", ErrResetInvalid
	}
	return u, kind, err
}

// CompleteReset sets the new password (or PIN) a reset token is for, marks the token and
// the user's other reset tokens used, and signs the user out everywhere. Two-factor sign-in
// is left as it is.
func (s *Service) CompleteReset(ctx context.Context, token, password, pin string, now time.Time) (User, string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return User{}, "", err
	}
	defer tx.Rollback()
	var id, uid int64
	var kind string
	err = tx.QueryRowContext(ctx, `SELECT id, user_id, kind FROM password_resets WHERE token_hash = ? AND used_at IS NULL AND expires_at > ?`,
		hashToken(token), now.UTC().Format(timeFmt)).Scan(&id, &uid, &kind)
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, "", ErrResetInvalid
	}
	if err != nil {
		return User{}, "", err
	}
	var col, secret string
	switch kind {
	case ResetPassword:
		if len(password) < 8 || len(password) > 256 {
			return User{}, kind, ErrResetValue
		}
		col, secret = "password_hash", password
	case ResetPIN:
		if !ValidPIN(pin) {
			return User{}, kind, ErrResetValue
		}
		col, secret = "pin_hash", pin
	default:
		return User{}, "", ErrResetInvalid
	}
	h, err := HashPassword(secret)
	if err != nil {
		return User{}, "", err
	}
	stamp := now.UTC().Format(timeFmt)
	if _, err := tx.ExecContext(ctx, `UPDATE users SET `+col+` = ?, updated_at = ? WHERE id = ?`, h, stamp, uid); err != nil {
		return User{}, "", err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE password_resets SET used_at = ? WHERE user_id = ? AND used_at IS NULL`, stamp, uid); err != nil {
		return User{}, "", err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM devices WHERE user_id = ?`, uid); err != nil {
		return User{}, "", err
	}
	if err := tx.Commit(); err != nil {
		return User{}, "", err
	}
	// A successful reset also lifts a lockout from earlier wrong guesses.
	u, err := getUser(ctx, s.db, uid)
	if err == nil {
		s.limiter.success("", strings.ToLower(u.Username))
	}
	return u, kind, err
}

// ValidPIN reports whether p is four digits.
func ValidPIN(p string) bool {
	if len(p) != 4 {
		return false
	}
	for _, c := range p {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}
