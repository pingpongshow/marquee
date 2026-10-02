package api

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"strings"

	"marquee/internal/auth"
	"marquee/internal/avatars"
	"marquee/internal/downloads"
	"marquee/internal/images"
	"marquee/internal/items"
	"marquee/internal/library"
	"marquee/internal/livetv"
	"marquee/internal/scrobble"
	"marquee/internal/logbuf"
	"marquee/internal/lyrics"
	"marquee/internal/metadata"
	"marquee/internal/netclass"
	"marquee/internal/playback"
	"marquee/internal/plex"
	"marquee/internal/requests"
	"marquee/internal/settings"
	"marquee/internal/sonic"
	"marquee/internal/subtitles"
	"marquee/internal/syncplay"
	"marquee/internal/tasks"
	"marquee/internal/trickplay"
	"marquee/internal/webhooks"
)

// Handlers implements StrictServerInterface.
type Handlers struct {
	DB        *sql.DB
	Auth      *auth.Service
	Settings  *settings.Store
	Libraries *library.Store
	Items     *items.Store
	Images    *images.Service
	Avatars   *avatars.Store
	Logs      *logbuf.Buffer
	Metadata  *metadata.Service
	Plex      *plex.Importer
	Playback  *playback.Manager
	// LibrariesChanged is called after a library is created, edited or deleted.
	LibrariesChanged func()
	Scans            *tasks.Scans
	Tasks            *tasks.Scheduler
	Trickplay        *trickplay.Service
	Webhooks         *webhooks.Dispatcher
	Subtitles        *subtitles.Service
	Downloads        *downloads.Service
	QuickConnect     auth.QuickConnect
	Sonic            *sonic.Service
	Lyrics           *lyrics.Service
	Backups          *tasks.Backups
	Requests         *requests.Service
	LiveTV           *livetv.Service
	DVR              *livetv.Recorder
	Scrobble         *scrobble.Service
	SyncPlay         *syncplay.Service
	// Restart stops the server gracefully; Docker starts it again.
	Restart func()
	Version string
}

var _ StrictServerInterface = (*Handlers)(nil)

// RequestInfo is attached to every request context by the HTTP middleware.
type RequestInfo struct {
	ClientIP string
	Class    netclass.Class
}

type reqInfoKey struct{}

func WithRequestInfo(ctx context.Context, ri RequestInfo) context.Context {
	return context.WithValue(ctx, reqInfoKey{}, ri)
}

func requestInfo(ctx context.Context) RequestInfo {
	ri, _ := ctx.Value(reqInfoKey{}).(RequestInfo)
	return ri
}

func apiErr(code, msg string) Error { return Error{Code: code, Message: msg} }

var (
	errUnauthorized = apiErr("unauthorized", "authentication required")
	errForbidden    = apiErr("forbidden", "administrator access required")
)

// session returns the caller's session; ok=false means unauthenticated.
func session(ctx context.Context) (auth.Session, bool) { return auth.SessionFrom(ctx) }

func isAdmin(ctx context.Context) (authed, admin bool) {
	s, ok := session(ctx)
	return ok, ok && s.User.IsAdmin
}

func internal(ctx context.Context, op string, err error) error {
	slog.ErrorContext(ctx, "request failed", "op", op, "err", err)
	return err // strict handler turns this into a 500
}

func ptr[T any](v T) *T { return &v }

// ---------- system ----------

func (h *Handlers) GetSystemInfo(ctx context.Context, _ GetSystemInfoRequestObject) (GetSystemInfoResponseObject, error) {
	n, err := h.Auth.UserCount(ctx)
	if err != nil {
		return nil, internal(ctx, "systemInfo", err)
	}
	return GetSystemInfo200JSONResponse{
		ServerId:      h.Settings.ServerID(),
		ServerName:    h.Settings.Get().General.ServerName,
		Version:       h.Version,
		ApiVersion:    "1",
		SetupRequired: n == 0,
		NetworkClass:  NetworkClass(requestInfo(ctx).Class),
		PinSignIn:     ptr(h.pinSignInAllowed(ctx)),
		LanUrl:        nz(h.Settings.Get().Network.LANURL),
		RemoteUrl:     remoteURL(h.Settings.Get()),
	}, nil
}

func (h *Handlers) GetHealth(ctx context.Context, _ GetHealthRequestObject) (GetHealthResponseObject, error) {
	checks := map[string]string{"database": "ok"}
	if err := h.DB.PingContext(ctx); err != nil {
		checks["database"] = err.Error()
		return GetHealth503JSONResponse{Status: HealthStatusError, Checks: checks}, nil
	}
	return GetHealth200JSONResponse{Status: HealthStatusOk, Checks: checks}, nil
}

// ---------- auth ----------

func toAPIUser(u auth.User) User {
	return User{
		Id: u.ID, Username: u.Username, DisplayName: u.DisplayName,
		IsAdmin: u.IsAdmin, IsManaged: u.IsManaged, HasPin: ptr(u.HasPIN), HasPassword: ptr(u.HasPassword), HasTwoFactor: ptr(u.HasTOTP), AvatarUrl: avatarURL(u),
		CreatedAt: u.CreatedAt, LastSeenAt: u.LastSeenAt,
		Restrictions: toAPIRestrictions(u.Restrictions), Preferences: toAPIPrefs(u.Preferences),
	}
}

func toDevice(d DeviceInfo) auth.Device {
	dev := auth.Device{ClientID: d.ClientId, Name: d.Name, Platform: string(d.Platform)}
	if d.Product != nil {
		dev.Product = *d.Product
	}
	if d.Version != nil {
		dev.Version = *d.Version
	}
	return dev
}

func validDevice(d DeviceInfo) bool {
	return strings.TrimSpace(d.ClientId) != "" && strings.TrimSpace(d.Name) != "" && len(d.ClientId) <= 128
}

func (h *Handlers) CompleteSetup(ctx context.Context, req CompleteSetupRequestObject) (CompleteSetupResponseObject, error) {
	b := req.Body
	name := strings.TrimSpace(b.ServerName)
	username := strings.TrimSpace(b.Username)
	switch {
	case name == "" || len(name) > 64:
		return CompleteSetup400JSONResponse{BadRequestJSONResponse(apiErr("invalid", "server name must be 1–64 characters"))}, nil
	case username == "" || len(username) > 64:
		return CompleteSetup400JSONResponse{BadRequestJSONResponse(apiErr("invalid", "username must be 1–64 characters"))}, nil
	case len(b.Password) < 8:
		return CompleteSetup400JSONResponse{BadRequestJSONResponse(apiErr("invalid", "password must be at least 8 characters"))}, nil
	case !validDevice(b.Device):
		return CompleteSetup400JSONResponse{BadRequestJSONResponse(apiErr("invalid", "device clientId and name are required"))}, nil
	}

	// The user-count check and insert share one IMMEDIATE transaction so two concurrent
	// setup requests cannot both create an admin.
	tx, err := h.DB.BeginTx(ctx, nil)
	if err != nil {
		return nil, internal(ctx, "setup", err)
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM users`).Scan(&n); err != nil {
		return nil, internal(ctx, "setup", err)
	}
	if n > 0 {
		return CompleteSetup409JSONResponse{ConflictJSONResponse(apiErr("setup_complete", "setup has already been completed"))}, nil
	}
	u, err := h.Auth.CreateUser(ctx, tx, username, username, b.Password, true)
	if err != nil {
		return nil, internal(ctx, "setup", err)
	}
	token, err := h.Auth.IssueToken(ctx, tx, u.ID, toDevice(b.Device), requestInfo(ctx).ClientIP)
	if err != nil {
		return nil, internal(ctx, "setup", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, internal(ctx, "setup", err)
	}
	if _, err := h.Settings.Update(ctx, func(s *settings.Settings) error {
		s.General.ServerName = name
		return nil
	}); err != nil {
		return nil, internal(ctx, "setup", err)
	}
	slog.InfoContext(ctx, "first-run setup complete", "admin", u.Username)
	return CompleteSetup200JSONResponse{Token: token, User: toAPIUser(u)}, nil
}

func (h *Handlers) Login(ctx context.Context, req LoginRequestObject) (LoginResponseObject, error) {
	b := req.Body
	if !validDevice(b.Device) {
		return Login401JSONResponse{UnauthorizedJSONResponse(apiErr("invalid_device", "device clientId and name are required"))}, nil
	}
	ip := requestInfo(ctx).ClientIP
	u, err := h.Auth.Authenticate(ctx, ip, strings.TrimSpace(b.Username), b.Password)
	var rl *auth.RateLimitedError
	switch {
	case errors.As(err, &rl):
		return Login429JSONResponse{TooManyRequestsJSONResponse(apiErr("rate_limited", rl.Error()))}, nil
	case errors.Is(err, auth.ErrInvalidCredentials):
		slog.WarnContext(ctx, "failed login", "username", b.Username, "ip", ip)
		return Login401JSONResponse{UnauthorizedJSONResponse(apiErr("invalid_credentials", err.Error()))}, nil
	case err != nil:
		return nil, internal(ctx, "login", err)
	}
	// Two-factor sign-in.
	code := ""
	set(&code, b.TotpCode)
	if err := h.Auth.CheckTOTP(ctx, u.ID, code); err != nil {
		if errors.Is(err, auth.ErrTOTPRequired) {
			return Login401JSONResponse{UnauthorizedJSONResponse(apiErr("totp_required", err.Error()))}, nil
		}
		if errors.As(err, &rl) {
			return Login429JSONResponse{TooManyRequestsJSONResponse(apiErr("rate_limited", rl.Error()))}, nil
		}
		slog.WarnContext(ctx, "wrong two-factor code", "username", b.Username, "ip", ip)
		return Login401JSONResponse{UnauthorizedJSONResponse(apiErr("invalid_totp", err.Error()))}, nil
	}
	token, err := h.Auth.IssueToken(ctx, h.DB, u.ID, toDevice(b.Device), ip)
	if err != nil {
		return nil, internal(ctx, "login", err)
	}
	return Login200JSONResponse{Token: token, User: toAPIUser(u)}, nil
}

func (h *Handlers) Logout(ctx context.Context, _ LogoutRequestObject) (LogoutResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return Logout401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	if err := h.Auth.RevokeDevice(ctx, s.DeviceID); err != nil {
		return nil, internal(ctx, "logout", err)
	}
	return Logout204Response{}, nil
}

func (h *Handlers) GetMe(ctx context.Context, _ GetMeRequestObject) (GetMeResponseObject, error) {
	s, ok := session(ctx)
	if !ok {
		return GetMe401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	}
	u := toAPIUser(s.User)
	u.ImageKey = nz(h.Auth.ImageKey(ctx, s.User.ID))
	return GetMe200JSONResponse(u), nil
}

// pinSignInAllowed reports whether PIN sign-in is enabled for the caller's network.
func (h *Handlers) pinSignInAllowed(ctx context.Context) bool {
	switch h.Settings.Get().Security.PinSignIn {
	case "everywhere":
		return true
	case "local":
		return requestInfo(ctx).Class == netclass.Local
	}
	return false
}

func (h *Handlers) ListSignInProfiles(ctx context.Context, _ ListSignInProfilesRequestObject) (ListSignInProfilesResponseObject, error) {
	out := ListSignInProfiles200JSONResponse{}
	if !h.pinSignInAllowed(ctx) {
		return out, nil
	}
	list, err := h.Auth.ListUsers(ctx)
	if err != nil {
		return nil, internal(ctx, "signInProfiles", err)
	}
	for _, u := range list {
		out = append(out, toAPIProfile(u))
	}
	return out, nil
}

func (h *Handlers) PinLogin(ctx context.Context, req PinLoginRequestObject) (PinLoginResponseObject, error) {
	if !h.pinSignInAllowed(ctx) {
		return PinLogin403JSONResponse{ForbiddenJSONResponse(apiErr("pin_disabled", "PIN sign-in isn't allowed from this network; use your password"))}, nil
	}
	b := req.Body
	if !validDevice(b.Device) {
		return PinLogin401JSONResponse{UnauthorizedJSONResponse(apiErr("invalid_device", "device clientId and name are required"))}, nil
	}
	pin := ""
	set(&pin, b.Pin)
	ip := requestInfo(ctx).ClientIP
	u, err := h.Auth.PINSignIn(ctx, ip, b.UserId, pin)
	var rl *auth.RateLimitedError
	switch {
	case errors.As(err, &rl):
		return PinLogin429JSONResponse{TooManyRequestsJSONResponse(apiErr("rate_limited", rl.Error()))}, nil
	case errors.Is(err, auth.ErrWrongPIN):
		slog.WarnContext(ctx, "wrong PIN", "user", b.UserId, "ip", ip)
		return PinLogin401JSONResponse{UnauthorizedJSONResponse(apiErr("wrong_pin", "incorrect PIN"))}, nil
	case err == nil && pinBlockedBy2FA(ctx, u):
		return PinLogin401JSONResponse{UnauthorizedJSONResponse(apiErr("password_required", errPinNeeds2FA))}, nil
	case errors.Is(err, auth.ErrPasswordRequired):
		if b.Password == nil || *b.Password == "" {
			return PinLogin401JSONResponse{UnauthorizedJSONResponse(apiErr("password_required", err.Error()))}, nil
		}
		target, gerr := h.Auth.GetUser(ctx, b.UserId)
		if gerr != nil {
			return nil, internal(ctx, "pinLogin", gerr)
		}
		u, err = h.Auth.Authenticate(ctx, ip, target.Username, *b.Password)
		switch {
		case errors.As(err, &rl):
			return PinLogin429JSONResponse{TooManyRequestsJSONResponse(apiErr("rate_limited", rl.Error()))}, nil
		case errors.Is(err, auth.ErrInvalidCredentials):
			return PinLogin401JSONResponse{UnauthorizedJSONResponse(apiErr("invalid_credentials", "incorrect password"))}, nil
		case err != nil:
			return nil, internal(ctx, "pinLogin", err)
		}
		code := ""
		set(&code, b.TotpCode)
		if err := h.Auth.CheckTOTP(ctx, u.ID, code); err != nil {
			if errors.Is(err, auth.ErrTOTPRequired) {
				return PinLogin401JSONResponse{UnauthorizedJSONResponse(apiErr("totp_required", err.Error()))}, nil
			}
			return PinLogin401JSONResponse{UnauthorizedJSONResponse(apiErr("invalid_totp", err.Error()))}, nil
		}
	case err != nil:
		return nil, internal(ctx, "pinLogin", err)
	}
	token, err := h.Auth.IssueToken(ctx, h.DB, u.ID, toDevice(b.Device), ip)
	if err != nil {
		return nil, internal(ctx, "pinLogin", err)
	}
	return PinLogin200JSONResponse{Token: token, User: toAPIUser(u)}, nil
}

const errPinNeeds2FA = "This account uses two-factor sign-in: away from home, sign in with its password and authenticator code."

// pinBlockedBy2FA: a PIN alone mustn't open a two-factor account from outside the home (D75).
func pinBlockedBy2FA(ctx context.Context, u auth.User) bool {
	return u.HasTOTP && requestInfo(ctx).Class == netclass.Remote
}

// remoteURL is the Tailscale address apps use away from home, when remote access is on.
func remoteURL(s settings.Settings) *string {
	if !s.RemoteAccess.Enabled {
		return nil
	}
	return nz(s.RemoteAccess.RemoteURL)
}
