package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base32"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Two-factor sign-in (TOTP, RFC 6238): 6 digits, 30 s steps, one step of clock drift allowed.

var (
	ErrTOTPRequired = errors.New("enter the code from your authenticator app")
	ErrTOTPInvalid  = errors.New("that code isn't right")
	ErrTOTPNotSetUp = errors.New("start two-factor setup first")
	ErrTOTPEnabled  = errors.New("two-factor sign-in is already on")
)

const totpStep = 30

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

func totpCode(secret []byte, step int64) string {
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], uint64(step))
	m := hmac.New(sha1.New, secret)
	m.Write(msg[:])
	h := m.Sum(nil)
	off := h[len(h)-1] & 0x0f
	v := binary.BigEndian.Uint32(h[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", v%1_000_000)
}

// totpMatch returns the step a code matches (now ±1), or 0.
func totpMatch(secretB32, code string, now time.Time) int64 {
	secret, err := b32.DecodeString(strings.ToUpper(secretB32))
	if err != nil {
		return 0
	}
	cur := now.Unix() / totpStep
	for _, st := range []int64{cur, cur - 1, cur + 1} {
		if subtle.ConstantTimeCompare([]byte(totpCode(secret, st)), []byte(code)) == 1 {
			return st
		}
	}
	return 0
}

func hashCode(c string) string {
	h := sha256.Sum256([]byte(strings.ToLower(strings.ReplaceAll(strings.TrimSpace(c), "-", ""))))
	return hex.EncodeToString(h[:])
}

// TOTPEnabled reports whether a user has two-factor sign-in, and how many recovery codes remain.
func (s *Service) TOTPEnabled(ctx context.Context, userID int64) (bool, int) {
	var secret sql.NullString
	var rec string
	if s.db.QueryRowContext(ctx, `SELECT secret, recovery FROM user_totp WHERE user_id = ?`, userID).Scan(&secret, &rec) != nil || !secret.Valid {
		return false, 0
	}
	var codes []string
	json.Unmarshal([]byte(rec), &codes)
	return true, len(codes)
}

// SetupTOTP makes a new secret for the user's authenticator; it takes effect once confirmed.
func (s *Service) SetupTOTP(ctx context.Context, userID int64, account, issuer string) (secret, otpauth string, err error) {
	if on, _ := s.TOTPEnabled(ctx, userID); on {
		return "", "", ErrTOTPEnabled
	}
	b := make([]byte, 20)
	rand.Read(b)
	secret = b32.EncodeToString(b)
	if _, err := s.db.ExecContext(ctx, `INSERT INTO user_totp (user_id, pending) VALUES (?, ?) ON CONFLICT(user_id) DO UPDATE SET pending = excluded.pending`, userID, secret); err != nil {
		return "", "", err
	}
	label := url.PathEscape(issuer + ":" + account)
	q := url.Values{"secret": {secret}, "issuer": {issuer}, "algorithm": {"SHA1"}, "digits": {"6"}, "period": {"30"}}
	return secret, "otpauth://totp/" + label + "?" + q.Encode(), nil
}

// EnableTOTP confirms setup with a code and returns ten one-time recovery codes.
func (s *Service) EnableTOTP(ctx context.Context, userID int64, code string) ([]string, error) {
	var pending sql.NullString
	if err := s.db.QueryRowContext(ctx, `SELECT pending FROM user_totp WHERE user_id = ?`, userID).Scan(&pending); err != nil || !pending.Valid {
		return nil, ErrTOTPNotSetUp
	}
	step := totpMatch(pending.String, strings.TrimSpace(code), time.Now())
	if step == 0 {
		return nil, ErrTOTPInvalid
	}
	codes := make([]string, 10)
	hashes := make([]string, 10)
	for i := range codes {
		b := make([]byte, 5)
		rand.Read(b)
		c := strings.ToLower(b32.EncodeToString(b)) // 8 characters
		codes[i] = c[:4] + "-" + c[4:]
		hashes[i] = hashCode(codes[i])
	}
	raw, _ := json.Marshal(hashes)
	_, err := s.db.ExecContext(ctx, `UPDATE user_totp SET secret = pending, pending = NULL, recovery = ?, last_step = ?,
		enabled_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE user_id = ?`, string(raw), step, userID)
	return codes, err
}

// CheckTOTP verifies a sign-in code (or uses up a recovery code). Users without two-factor
// sign-in always pass.
func (s *Service) CheckTOTP(ctx context.Context, userID int64, code string) error {
	var secret sql.NullString
	var rec string
	var last int64
	if s.db.QueryRowContext(ctx, `SELECT secret, recovery, last_step FROM user_totp WHERE user_id = ?`, userID).Scan(&secret, &rec, &last) != nil || !secret.Valid {
		return nil
	}
	code = strings.TrimSpace(code)
	if code == "" {
		return ErrTOTPRequired
	}
	// Wrong codes are limited per account, so knowing the password doesn't allow guessing.
	key := "totp:" + strconv.FormatInt(userID, 10)
	if wait := s.totp.blocked("", key); wait > 0 {
		return &RateLimitedError{RetryAfter: wait}
	}
	// Each code and recovery code works once: the updates only succeed if nothing else used
	// it first (two sign-ins racing with the same code).
	if step := totpMatch(secret.String, code, time.Now()); step > last {
		res, err := s.db.ExecContext(ctx, `UPDATE user_totp SET last_step = ? WHERE user_id = ? AND last_step < ?`, step, userID, step)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 1 {
			s.totp.success("", key)
			return nil
		}
	}
	var hashes []string
	json.Unmarshal([]byte(rec), &hashes)
	h := hashCode(code)
	for i, x := range hashes {
		if subtle.ConstantTimeCompare([]byte(x), []byte(h)) == 1 {
			left := append(append([]string{}, hashes[:i]...), hashes[i+1:]...)
			raw, _ := json.Marshal(left)
			res, err := s.db.ExecContext(ctx, `UPDATE user_totp SET recovery = ? WHERE user_id = ? AND recovery = ?`, string(raw), userID, rec)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n == 1 {
				s.totp.success("", key)
				return nil
			}
			break
		}
	}
	s.totp.fail("", key)
	return ErrTOTPInvalid
}

// DisableTOTP turns two-factor sign-in off.
func (s *Service) DisableTOTP(ctx context.Context, userID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM user_totp WHERE user_id = ?`, userID)
	return err
}
