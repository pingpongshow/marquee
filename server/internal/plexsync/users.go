package plexsync

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"marquee/internal/plex"
	"marquee/internal/settings"
)

// Plex account kinds.
const (
	KindOwner   = "owner"   // the server owner (Plex account 1): the configured token
	KindHome    = "home"    // a Plex Home user: token through plex.tv's user switch
	KindManaged = "managed" // a managed (restricted) Plex Home user: same
	KindShared  = "shared"  // a friend the server is shared with
	KindUnknown = "unknown"
)

// ownerAccount is the server owner's account id in Plex's own account list.
const ownerAccount = 1

// User is a Plex account and the Marquee user it syncs with.
type User struct {
	PlexAccountID int64
	Name          string
	UserID        int64 // 0 = not linked
	Kind          string
	CanSync       bool
	Reason        string // why it can't sync

	token string // its token for this server (only during a run)
}

// Users lists the Plex accounts known to the sync.
func (s *Service) Users(ctx context.Context) ([]User, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT plex_account_id, name, COALESCE(user_id, 0), kind, can_sync, reason
		FROM plex_sync_users ORDER BY plex_account_id != 1, name COLLATE NOCASE, plex_account_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		var u User
		if err := rows.Scan(&u.PlexAccountID, &u.Name, &u.UserID, &u.Kind, &u.CanSync, &u.Reason); err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// ErrNoAccount: no such Plex account.
var ErrNoAccount = errors.New("no such Plex account")

// ErrNoUser: no such Marquee user.
var ErrNoUser = errors.New("no such Marquee user")

// SetUser links a Plex account to a Marquee user (0 unlinks).
func (s *Service) SetUser(ctx context.Context, plexAccountID, userID int64) error {
	if userID != 0 {
		var n int
		s.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM users WHERE id = ?`, userID).Scan(&n)
		if n == 0 {
			return ErrNoUser
		}
	}
	var uid any
	if userID != 0 {
		uid = userID
	}
	res, err := s.DB.ExecContext(ctx, `UPDATE plex_sync_users SET user_id = ?, updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
		WHERE plex_account_id = ?`, uid, plexAccountID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNoAccount
	}
	return nil
}

// SaveImport keeps the accounts and folders a Plex import used (plex.Importer.OnMapped).
func (s *Service) SaveImport(ctx context.Context, mappings []plex.PathMapping, users map[int64]int64, names map[int64]string) {
	for acct, uid := range users {
		if _, err := s.DB.ExecContext(ctx, `INSERT INTO plex_sync_users(plex_account_id, name, user_id) VALUES (?, ?, ?)
			ON CONFLICT(plex_account_id) DO UPDATE SET user_id = excluded.user_id,
			name = CASE WHEN excluded.name != '' THEN excluded.name ELSE name END,
			updated_at = strftime('%Y-%m-%dT%H:%M:%fZ', 'now')`, acct, names[acct], uid); err != nil {
			slog.Warn("Plex sync: saving the import's accounts", "err", err)
		}
	}
	if len(mappings) == 0 || s.Settings == nil {
		return
	}
	if _, err := s.Settings.Update(ctx, func(st *settings.Settings) error {
		st.PlexSync.PathMappings = nil
		for _, m := range mappings {
			st.PlexSync.PathMappings = append(st.PlexSync.PathMappings, settings.PlexPathMapping{Plex: m.Plex, Marquee: m.Marquee})
		}
		return nil
	}); err != nil {
		slog.Warn("Plex sync: saving the import's folders", "err", err)
	}
}

// resolveUsers refreshes the account list from Plex and finds a server token for every
// linked account: the owner's own, Plex Home users' through plex.tv, friends' when the
// owner's token can list them.
func (s *Service) resolveUsers(ctx context.Context, srv, tv *client, ownerToken, machineID string) ([]User, error) {
	accounts, err := srv.accounts(ctx, ownerToken)
	if errors.Is(err, ErrUnreachable) {
		return nil, err
	}
	if err != nil {
		slog.Warn("Plex sync: listing accounts", "err", srv.secrets.redact(err.Error()))
	}
	for _, a := range accounts {
		if a.ID == 0 || (a.Name == "" && a.ID != ownerAccount) {
			continue
		}
		if _, err := s.DB.ExecContext(ctx, `INSERT INTO plex_sync_users(plex_account_id, name) VALUES (?, ?)
			ON CONFLICT(plex_account_id) DO UPDATE SET name = CASE WHEN excluded.name != '' THEN excluded.name ELSE name END`, a.ID, a.Name); err != nil {
			return nil, err
		}
	}
	// The owner syncs with Marquee's first administrator until the admin says otherwise.
	s.DB.ExecContext(ctx, `UPDATE plex_sync_users SET user_id = (SELECT id FROM users WHERE is_admin = 1 ORDER BY id LIMIT 1)
		WHERE plex_account_id = 1 AND user_id IS NULL AND kind = 'unknown' AND can_sync = 0 AND reason = ''`)

	users, err := s.Users(ctx)
	if err != nil {
		return nil, err
	}
	var (
		home       map[int64]homeUser
		homeErr    error
		shared     map[int64]string
		sharedDone bool
	)
	for i := range users {
		u := &users[i]
		u.CanSync, u.Reason, u.token = false, "", ""
		if u.PlexAccountID == ownerAccount {
			u.Kind, u.token = KindOwner, ownerToken
		} else {
			if home == nil && homeErr == nil {
				var list []homeUser
				list, homeErr = tv.homeUsers(ctx, ownerToken)
				home = map[int64]homeUser{}
				for _, h := range list {
					home[h.ID] = h
				}
			}
			if h, ok := home[u.PlexAccountID]; ok {
				u.Kind = KindHome
				if h.Restricted {
					u.Kind = KindManaged
				}
				if u.UserID != 0 {
					if h.Protected {
						u.Reason = "can't sync: this Plex Home user has a PIN"
					} else if tok, err := s.homeToken(ctx, tv, ownerToken, h.UUID, machineID); err != nil {
						u.Reason = "can't sync: " + tv.secrets.redact(err.Error())
					} else {
						u.token = tok
					}
				}
			} else {
				if u.UserID != 0 && !sharedDone {
					sharedDone = true
					shared, _ = tv.sharedTokens(ctx, ownerToken, machineID)
				}
				if tok, ok := shared[u.PlexAccountID]; ok {
					u.Kind, u.token = KindShared, tok
				} else if u.UserID != 0 {
					if u.Kind == KindUnknown || u.Kind == "" {
						u.Kind = KindShared
					}
					switch {
					case errors.Is(homeErr, ErrUnreachable):
						u.Reason = "can't sync now: plex.tv isn't reachable"
					case errors.Is(homeErr, ErrUnauthorized):
						u.Reason = "can't sync: plex.tv doesn't accept the token (use Sign in with Plex to sync other Plex users)"
					default:
						u.Reason = "can't sync (shared user): the owner's token can't act for this account"
					}
				}
			}
		}
		switch {
		case u.UserID == 0:
			if u.Reason == "" {
				u.Reason = "not linked to a Marquee user"
			}
		case u.token != "":
			u.CanSync = true
		}
		if _, err := s.DB.ExecContext(ctx, `UPDATE plex_sync_users SET kind = ?, can_sync = ?, reason = ? WHERE plex_account_id = ?`,
			u.Kind, u.CanSync, u.Reason, u.PlexAccountID); err != nil {
			return nil, err
		}
	}
	return users, nil
}

// homeToken switches to a Plex Home user on plex.tv and returns their token for the server.
func (s *Service) homeToken(ctx context.Context, tv *client, ownerToken, uuid, machineID string) (string, error) {
	userTok, err := tv.switchHomeUser(ctx, ownerToken, uuid)
	if err != nil {
		return "", err
	}
	tok, err := tv.serverToken(ctx, userTok, machineID)
	if err != nil {
		return "", fmt.Errorf("finding the user's token for this server: %w", err)
	}
	return tok, nil
}
