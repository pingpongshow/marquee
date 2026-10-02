package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrLastAdmin        = errors.New("there must be at least one administrator")
	ErrPasswordRequired = errors.New("this profile signs in with a password")
	ErrWrongPIN         = errors.New("incorrect PIN or password")
	ErrNoAuthMeans      = errors.New("administrators need a password")
)

// Restrictions limit what a user can see and how they can stream (USER-2, WAN-4).
type Restrictions struct {
	LibraryIDs        *[]int64 `json:"libraryIds,omitempty"` // nil = all libraries
	MaxContentRating  *string  `json:"maxContentRating,omitempty"`
	AllowRemote       *bool    `json:"allowRemote,omitempty"` // nil = allowed
	RemoteQualityKbps int      `json:"remoteQualityKbps,omitempty"`
}

// RemoteAllowed reports whether the user may stream outside the LAN.
func (r Restrictions) RemoteAllowed() bool { return r.AllowRemote == nil || *r.AllowRemote }

// Preferences are per-user playback defaults.
type Preferences struct {
	AudioLanguage     string `json:"audioLanguage,omitempty"`
	SubtitleLanguage  string `json:"subtitleLanguage,omitempty"`
	SubtitleMode      string `json:"subtitleMode,omitempty"`
	LocalQualityKbps  int    `json:"localQualityKbps,omitempty"`
	RemoteQualityKbps int    `json:"remoteQualityKbps,omitempty"`
}

type NewUser struct {
	Username, DisplayName, Password, PIN string
	IsAdmin, IsManaged                   bool
	Restrictions                         Restrictions
	// Imported users (from Plex) may start without a password; they can't sign in or be
	// switched to until an administrator sets one.
	Imported bool
}

type UserUpdate struct {
	DisplayName  *string
	Password     *string
	PIN          *string // "" removes
	IsAdmin      *bool
	Restrictions *Restrictions
	Preferences  *Preferences
}

func (s *Service) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+userCols+` FROM users u ORDER BY u.is_admin DESC, u.display_name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Service) GetUser(ctx context.Context, id int64) (User, error) { return getUser(ctx, s.db, id) }

// Create adds a user. Managed profiles may have no password (they sign in by PIN through
// profile switching); everyone else needs one.
func (s *Service) Create(ctx context.Context, n NewUser) (User, error) {
	// Only administrators must have a password; everyone else may have a password, a PIN,
	// both or neither (Plex Home style: tap your profile on the home network).
	if n.Password == "" && n.IsAdmin && !n.IsManaged {
		return User{}, ErrNoAuthMeans
	}
	if n.DisplayName == "" {
		n.DisplayName = n.Username
	}
	var pw, pin any
	if n.Password != "" {
		h, err := HashPassword(n.Password)
		if err != nil {
			return User{}, err
		}
		pw = h
	}
	if n.PIN != "" {
		h, err := HashPassword(n.PIN)
		if err != nil {
			return User{}, err
		}
		pin = h
	}
	restr, _ := json.Marshal(n.Restrictions)
	r, err := s.db.ExecContext(ctx, `INSERT INTO users(username, display_name, password_hash, pin_hash, is_admin, is_managed, restrictions)
		VALUES (?, ?, ?, ?, ?, ?, ?)`, n.Username, n.DisplayName, pw, pin, n.IsAdmin && !n.IsManaged, n.IsManaged, string(restr))
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return User{}, ErrUsernameTaken
		}
		return User{}, err
	}
	id, _ := r.LastInsertId()
	return getUser(ctx, s.db, id)
}

// Update applies non-nil fields. Demoting the last admin is refused.
func (s *Service) Update(ctx context.Context, id int64, u UserUpdate) (User, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback()
	cur, err := getUser(ctx, tx, id)
	if err != nil {
		return User{}, err
	}
	if u.DisplayName != nil {
		if _, err := tx.ExecContext(ctx, `UPDATE users SET display_name = ? WHERE id = ?`, *u.DisplayName, id); err != nil {
			return User{}, err
		}
	}
	if u.Password != nil {
		h, err := HashPassword(*u.Password)
		if err != nil {
			return User{}, err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE users SET password_hash = ? WHERE id = ?`, h, id); err != nil {
			return User{}, err
		}
	}
	if u.PIN != nil {
		var pin any
		if *u.PIN != "" {
			h, err := HashPassword(*u.PIN)
			if err != nil {
				return User{}, err
			}
			pin = h
		}
		if _, err := tx.ExecContext(ctx, `UPDATE users SET pin_hash = ? WHERE id = ?`, pin, id); err != nil {
			return User{}, err
		}
	}
	if u.IsAdmin != nil && *u.IsAdmin != cur.IsAdmin {
		if cur.IsManaged && *u.IsAdmin {
			return User{}, fmt.Errorf("managed profiles cannot be administrators")
		}
		if !*u.IsAdmin {
			if err := ensureAnotherAdmin(ctx, tx, id); err != nil {
				return User{}, err
			}
		} else if !cur.HasPassword && (u.Password == nil || *u.Password == "") {
			return User{}, ErrNoAuthMeans
		}
		if _, err := tx.ExecContext(ctx, `UPDATE users SET is_admin = ? WHERE id = ?`, *u.IsAdmin, id); err != nil {
			return User{}, err
		}
	}
	if u.Restrictions != nil {
		raw, _ := json.Marshal(u.Restrictions)
		if _, err := tx.ExecContext(ctx, `UPDATE users SET restrictions = ? WHERE id = ?`, string(raw), id); err != nil {
			return User{}, err
		}
	}
	if u.Preferences != nil {
		raw, _ := json.Marshal(u.Preferences)
		if _, err := tx.ExecContext(ctx, `UPDATE users SET preferences = ? WHERE id = ?`, string(raw), id); err != nil {
			return User{}, err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE users SET updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now') WHERE id = ?`, id); err != nil {
		return User{}, err
	}
	if err := tx.Commit(); err != nil {
		return User{}, err
	}
	return getUser(ctx, s.db, id)
}

func ensureAnotherAdmin(ctx context.Context, q querier, excluding int64) error {
	var n int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE is_admin = 1 AND id != ?`, excluding).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return ErrLastAdmin
	}
	return nil
}

// Delete removes a user; devices, watch state and playlists cascade.
func (s *Service) Delete(ctx context.Context, id int64) error {
	u, err := getUser(ctx, s.db, id)
	if err != nil {
		return err
	}
	if u.IsAdmin {
		if err := ensureAnotherAdmin(ctx, s.db, id); err != nil {
			return err
		}
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	return err
}

// VerifyPassword checks a user's current password.
func (s *Service) CheckPassword(ctx context.Context, id int64, password string) (bool, error) {
	var hash sql.NullString
	if err := s.db.QueryRowContext(ctx, `SELECT password_hash FROM users WHERE id = ?`, id).Scan(&hash); err != nil {
		return false, err
	}
	if !hash.Valid {
		return false, nil
	}
	return VerifyPassword(hash.String, password)
}

// SwitchCredentials reports what choosing a profile requires: its PIN if it has one, else
// its password if it has one, else nothing.
func SwitchCredentials(u User) string {
	switch {
	case u.HasPIN:
		return "pin"
	case u.HasPassword:
		return "password"
	}
	return "none"
}

// VerifySwitch checks the PIN (or password, for profiles without a PIN) and rate-limits
// attempts per device, since PINs are short.
func (s *Service) VerifySwitch(ctx context.Context, clientIP string, target User, pin, password string) error {
	key := fmt.Sprintf("profile:%d", target.ID)
	if wait := s.pins.blocked(clientIP, key); wait > 0 {
		return &RateLimitedError{RetryAfter: wait}
	}
	var pinHash, pwHash sql.NullString
	if err := s.db.QueryRowContext(ctx, `SELECT pin_hash, password_hash FROM users WHERE id = ?`, target.ID).Scan(&pinHash, &pwHash); err != nil {
		return err
	}
	ok := false
	switch {
	case pinHash.Valid:
		ok, _ = VerifyPassword(pinHash.String, pin)
	case pwHash.Valid:
		ok, _ = VerifyPassword(pwHash.String, password)
	default:
		ok = true // no PIN or password set: open by choosing the profile
	}
	if !ok {
		s.pins.fail(clientIP, key)
		return ErrWrongPIN
	}
	s.pins.success(clientIP, key)
	return nil
}

type DeviceInfo struct {
	ID, UserID                                 int64
	UserName, Name, Platform, Product, Version string
	LastIP                                     string
	LastSeenAt, CreatedAt                      time.Time
}

// ListDevices returns devices, optionally limited to one user (userID > 0).
func (s *Service) ListDevices(ctx context.Context, userID int64) ([]DeviceInfo, error) {
	q := `SELECT d.id, d.user_id, u.display_name, d.name, d.platform, d.product, d.version, d.last_ip, d.last_seen_at, d.created_at
		FROM devices d JOIN users u ON u.id = d.user_id`
	var args []any
	if userID > 0 {
		q += ` WHERE d.user_id = ?`
		args = append(args, userID)
	}
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY d.last_seen_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DeviceInfo
	for rows.Next() {
		var d DeviceInfo
		var seen, created string
		if err := rows.Scan(&d.ID, &d.UserID, &d.UserName, &d.Name, &d.Platform, &d.Product, &d.Version, &d.LastIP, &seen, &created); err != nil {
			return nil, err
		}
		d.LastSeenAt, _ = time.Parse(time.RFC3339Nano, seen)
		d.CreatedAt, _ = time.Parse(time.RFC3339Nano, created)
		out = append(out, d)
	}
	return out, rows.Err()
}

// DeviceOwner returns the user owning a device, or ErrUserNotFound.
func (s *Service) DeviceOwner(ctx context.Context, deviceID int64) (int64, error) {
	var uid int64
	err := s.db.QueryRowContext(ctx, `SELECT user_id FROM devices WHERE id = ?`, deviceID).Scan(&uid)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrUserNotFound
	}
	return uid, err
}

// PINSignIn signs a profile in with its PIN. Profiles without a PIN use their password if they
// have one, otherwise nothing. Five wrong PINs lock the profile (and the
// device's address) for 15 minutes.
func (s *Service) PINSignIn(ctx context.Context, clientIP string, userID int64, pin string) (User, error) {
	key := fmt.Sprintf("pin:%d", userID)
	if wait := s.pins.blocked(clientIP, key); wait > 0 {
		return User{}, &RateLimitedError{RetryAfter: wait}
	}
	var pinHash, pwHash sql.NullString
	err := s.db.QueryRowContext(ctx, `SELECT pin_hash, password_hash FROM users WHERE id = ?`, userID).Scan(&pinHash, &pwHash)
	if errors.Is(err, sql.ErrNoRows) {
		s.pins.fail(clientIP, key)
		return User{}, ErrWrongPIN
	}
	if err != nil {
		return User{}, err
	}
	switch {
	case pinHash.Valid:
		if ok, _ := VerifyPassword(pinHash.String, pin); !ok {
			s.pins.fail(clientIP, key)
			return User{}, ErrWrongPIN
		}
	case pwHash.Valid:
		return User{}, ErrPasswordRequired
	}
	s.pins.success(clientIP, key)
	return getUser(ctx, s.db, userID)
}
