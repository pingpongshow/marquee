package auth

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// Sharing (USER-13, D86): an admin makes a one-time link; whoever opens it picks a username
// and password and joins as a friend with the invite's restrictions. Only the token's hash
// is stored. Lookups are rate limited per address, like sign-in.

var ErrInviteInvalid = errors.New("this invite link isn't valid any more")

type Invite struct {
	ID           int64
	Note         string
	CreatedAt    time.Time
	ExpiresAt    time.Time
	UsedAt       *time.Time
	UsedBy       string
	InvitedBy    string
	Restrictions Restrictions
}

// CreateInvite stores an invite and returns it with its token (shown once).
func (s *Service) CreateInvite(ctx context.Context, by int64, note string, valid time.Duration, r Restrictions) (Invite, string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return Invite{}, "", err
	}
	token := base64.RawURLEncoding.EncodeToString(b)
	r.Friend = true
	restr, _ := json.Marshal(r)
	now := time.Now().UTC()
	res, err := s.db.ExecContext(ctx, `INSERT INTO invites (token_hash, created_by, note, restrictions, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?)`,
		hashToken(token), by, strings.TrimSpace(note), string(restr), now.Format(time.RFC3339), now.Add(valid).Format(time.RFC3339))
	if err != nil {
		return Invite{}, "", err
	}
	id, _ := res.LastInsertId()
	return Invite{ID: id, Note: strings.TrimSpace(note), CreatedAt: now.Truncate(time.Second), ExpiresAt: now.Add(valid).Truncate(time.Second), Restrictions: r}, token, nil
}

const inviteCols = `SELECT i.id, i.note, i.created_at, i.expires_at, COALESCE(i.used_at, ''), COALESCE(u.display_name, ''),
	COALESCE(c.display_name, ''), i.restrictions FROM invites i LEFT JOIN users u ON u.id = i.used_by LEFT JOIN users c ON c.id = i.created_by`

func scanInvite(row interface{ Scan(...any) error }) (Invite, error) {
	var in Invite
	var created, expires, used, restr string
	if err := row.Scan(&in.ID, &in.Note, &created, &expires, &used, &in.UsedBy, &in.InvitedBy, &restr); err != nil {
		return in, err
	}
	in.CreatedAt, _ = time.Parse(time.RFC3339, created)
	in.ExpiresAt, _ = time.Parse(time.RFC3339, expires)
	if t, err := time.Parse(time.RFC3339, used); err == nil {
		in.UsedAt = &t
	}
	json.Unmarshal([]byte(restr), &in.Restrictions)
	return in, nil
}

// ListInvites returns invites, newest first.
func (s *Service) ListInvites(ctx context.Context) ([]Invite, error) {
	rows, err := s.db.QueryContext(ctx, inviteCols+` ORDER BY i.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Invite
	for rows.Next() {
		in, err := scanInvite(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, in)
	}
	return out, rows.Err()
}

func (s *Service) DeleteInvite(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM invites WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrInviteInvalid
	}
	return nil
}

// LookupInvite finds an unused, unexpired invite by its token.
func (s *Service) LookupInvite(ctx context.Context, clientIP, token string) (Invite, error) {
	return s.lookupInvite(ctx, s.db, clientIP, token)
}

func (s *Service) lookupInvite(ctx context.Context, q querier, clientIP, token string) (Invite, error) {
	if wait := s.invites.blocked(clientIP, ""); wait > 0 {
		return Invite{}, &RateLimitedError{RetryAfter: wait}
	}
	in, err := scanInvite(q.QueryRowContext(ctx, inviteCols+` WHERE i.token_hash = ?`, hashToken(token)))
	if errors.Is(err, sql.ErrNoRows) || (err == nil && (in.UsedAt != nil || time.Now().After(in.ExpiresAt))) {
		s.invites.fail(clientIP, "")
		return Invite{}, ErrInviteInvalid
	}
	return in, err
}

// AcceptInvite creates the friend's account and uses up the invite.
func (s *Service) AcceptInvite(ctx context.Context, clientIP, token string, n NewUser) (User, error) {
	if len(n.Password) < 8 {
		return User{}, errors.New("password must be at least 8 characters")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return User{}, err
	}
	defer tx.Rollback()
	in, err := s.lookupInvite(ctx, tx, clientIP, token)
	if err != nil {
		return User{}, err
	}
	pw, err := HashPassword(n.Password)
	if err != nil {
		return User{}, err
	}
	if n.DisplayName == "" {
		n.DisplayName = n.Username
	}
	in.Restrictions.Friend = true
	restr, _ := json.Marshal(in.Restrictions)
	res, err := tx.ExecContext(ctx, `INSERT INTO users(username, display_name, password_hash, is_admin, is_managed, restrictions)
		VALUES (?, ?, ?, 0, 0, ?)`, n.Username, n.DisplayName, pw, string(restr))
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			return User{}, ErrUsernameTaken
		}
		return User{}, err
	}
	id, _ := res.LastInsertId()
	// Only the first acceptance wins, even if two arrive at once.
	res, err = tx.ExecContext(ctx, `UPDATE invites SET used_by = ?, used_at = ? WHERE id = ? AND used_at IS NULL`,
		id, time.Now().UTC().Format(time.RFC3339), in.ID)
	if err != nil {
		return User{}, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return User{}, ErrInviteInvalid
	}
	if err := tx.Commit(); err != nil {
		return User{}, err
	}
	return getUser(ctx, s.db, id)
}
