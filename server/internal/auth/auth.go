// Package auth manages users, device sessions (bearer tokens) and login rate limiting.
package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"
)

var (
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrUsernameTaken      = errors.New("username already exists")
	ErrUserNotFound       = errors.New("user not found")
)

type User struct {
	ID           int64
	Username     string
	DisplayName  string
	IsAdmin      bool
	IsManaged    bool
	HasPIN       bool
	HasPassword  bool
	HasTOTP      bool  // two-factor sign-in is on
	Avatar       int64 // profile picture version; 0 = none
	CreatedAt    time.Time
	LastSeenAt   *time.Time
	Restrictions Restrictions
	Preferences  Preferences
}

type Device struct {
	ClientID, Name, Platform, Product, Version string
}

// Session is the authenticated identity attached to a request.
type Session struct {
	User     User
	DeviceID int64
}

type Service struct {
	db        *sql.DB
	limiter   *loginLimiter
	pins      *loginLimiter
	seenMu    sync.Mutex
	lastSeen  map[int64]time.Time // device id → last persisted last_seen_at
	imgSecret []byte
}

func NewService(db *sql.DB) *Service {
	return &Service{db: db, limiter: newLoginLimiter(), pins: newLimiter(5), lastSeen: map[int64]time.Time{}}
}

// UserCount returns the number of users; zero means first-run setup is required.
func (s *Service) UserCount(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

// CreateUser inserts a user. Pass tx to participate in a caller's transaction.
func (s *Service) CreateUser(ctx context.Context, tx *sql.Tx, username, displayName, password string, isAdmin bool) (User, error) {
	hash, err := HashPassword(password)
	if err != nil {
		return User{}, err
	}
	res, err := tx.ExecContext(ctx,
		`INSERT INTO users(username, display_name, password_hash, is_admin) VALUES (?, ?, ?, ?)`,
		username, displayName, hash, isAdmin)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return User{}, ErrUsernameTaken
		}
		return User{}, err
	}
	id, _ := res.LastInsertId()
	return getUser(ctx, tx, id)
}

// Authenticate verifies credentials, enforcing per-IP and per-username lockouts.
func (s *Service) Authenticate(ctx context.Context, clientIP, username, password string) (User, error) {
	key := strings.ToLower(username)
	if wait := s.limiter.blocked(clientIP, key); wait > 0 {
		return User{}, &RateLimitedError{RetryAfter: wait}
	}
	var id int64
	var hash sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT id, password_hash FROM users WHERE username = ?`, username).Scan(&id, &hash)
	if errors.Is(err, sql.ErrNoRows) || (err == nil && !hash.Valid) {
		// Burn comparable time so response timing doesn't reveal whether a username exists.
		VerifyPassword(dummyHash, password)
		s.limiter.fail(clientIP, key)
		return User{}, ErrInvalidCredentials
	}
	if err != nil {
		return User{}, err
	}
	ok, err := VerifyPassword(hash.String, password)
	if err != nil {
		return User{}, err
	}
	if !ok {
		s.limiter.fail(clientIP, key)
		return User{}, ErrInvalidCredentials
	}
	s.limiter.success(clientIP, key)
	return getUser(ctx, s.db, id)
}

// IssueToken creates (or replaces) the session for this user's device and returns the bearer token.
func (s *Service) IssueToken(ctx context.Context, q querier, userID int64, d Device, ip string) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)
	_, err := q.ExecContext(ctx, `
		INSERT INTO devices(user_id, client_id, name, platform, product, version, token_hash, last_ip)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(user_id, client_id) DO UPDATE SET
			name = excluded.name, platform = excluded.platform, product = excluded.product,
			version = excluded.version, token_hash = excluded.token_hash, last_ip = excluded.last_ip,
			last_seen_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')`,
		userID, d.ClientID, d.Name, d.Platform, d.Product, d.Version, hashToken(token), ip)
	return token, err
}

// ResolveToken returns the session for a bearer token, or ok=false if it is unknown.
func (s *Service) ResolveToken(ctx context.Context, token, ip string) (Session, bool, error) {
	var deviceID, userID int64
	err := s.db.QueryRowContext(ctx, `SELECT id, user_id FROM devices WHERE token_hash = ?`, hashToken(token)).Scan(&deviceID, &userID)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, false, nil
	}
	if err != nil {
		return Session{}, false, err
	}
	u, err := getUser(ctx, s.db, userID)
	if err != nil {
		return Session{}, false, err
	}
	s.touch(ctx, deviceID, ip)
	return Session{User: u, DeviceID: deviceID}, true, nil
}

// RevokeDevice deletes a device session.
func (s *Service) RevokeDevice(ctx context.Context, deviceID int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM devices WHERE id = ?`, deviceID)
	return err
}

// touch updates last_seen at most once a minute per device to avoid a write per request.
func (s *Service) touch(ctx context.Context, deviceID int64, ip string) {
	s.seenMu.Lock()
	if time.Since(s.lastSeen[deviceID]) < time.Minute {
		s.seenMu.Unlock()
		return
	}
	s.lastSeen[deviceID] = time.Now()
	s.seenMu.Unlock()
	s.db.ExecContext(ctx, `UPDATE devices SET last_seen_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now'), last_ip = ? WHERE id = ?`, ip, deviceID)
}

type querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

const userCols = `u.id, u.username, u.display_name, u.is_admin, u.is_managed, u.pin_hash IS NOT NULL,
	u.password_hash IS NOT NULL, u.avatar_version, u.created_at, u.restrictions, u.preferences,
	(SELECT MAX(last_seen_at) FROM devices d WHERE d.user_id = u.id),
	EXISTS(SELECT 1 FROM user_totp t WHERE t.user_id = u.id AND t.secret IS NOT NULL)`

func scanUser(row interface{ Scan(...any) error }) (User, error) {
	var u User
	var created, restr, prefs string
	var seen sql.NullString
	if err := row.Scan(&u.ID, &u.Username, &u.DisplayName, &u.IsAdmin, &u.IsManaged, &u.HasPIN, &u.HasPassword, &u.Avatar,
		&created, &restr, &prefs, &seen, &u.HasTOTP); err != nil {
		return User{}, err
	}
	u.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
	if seen.Valid {
		t, _ := time.Parse(time.RFC3339Nano, seen.String)
		u.LastSeenAt = &t
	}
	json.Unmarshal([]byte(restr), &u.Restrictions)
	json.Unmarshal([]byte(prefs), &u.Preferences)
	return u, nil
}

func getUser(ctx context.Context, q querier, id int64) (User, error) {
	u, err := scanUser(q.QueryRowContext(ctx, `SELECT `+userCols+` FROM users u WHERE u.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return User{}, ErrUserNotFound
	}
	return u, err
}

func hashToken(t string) string {
	sum := sha256.Sum256([]byte(t))
	return hex.EncodeToString(sum[:])
}

// BearerToken extracts the token from the Authorization header, or the `token` query
// parameter (needed for <video>/<img> and AVPlayer URLs that cannot set headers).
func BearerToken(r *http.Request) string {
	if h := r.Header.Get("Authorization"); len(h) > 7 && strings.EqualFold(h[:7], "bearer ") {
		return strings.TrimSpace(h[7:])
	}
	// Only API URLs take a token in the query. Web-app pages don't: other services send
	// people back to them with their own ?token= (Last.fm's sign-in, D81).
	if !strings.HasPrefix(r.URL.Path, "/api/") {
		return ""
	}
	return r.URL.Query().Get("token")
}

var dummyHash, _ = HashPassword("marquee-timing-equaliser")

type ctxKey struct{}

func WithSession(ctx context.Context, s Session) context.Context {
	return context.WithValue(ctx, ctxKey{}, s)
}

func SessionFrom(ctx context.Context) (Session, bool) {
	s, ok := ctx.Value(ctxKey{}).(Session)
	return s, ok
}
