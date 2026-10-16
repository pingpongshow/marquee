package api

import (
	"context"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"marquee/internal/auth"
	"marquee/internal/notify"
	"marquee/internal/settings"
)

// ---------- settings ----------

// hiddenAlert are alert types the API exposes as their own booleans, not as AlertType values.
var hiddenAlert = map[string]bool{settings.AlertDVRQuota: true, settings.AlertPlexSync: true}

func toAPINotifications(n settings.Notifications) *NotificationSettings {
	// dvr_quota and plex_sync aren't AlertType values (shipped apps decode the enum
	// strictly): they're dvrQuotaAlert and plexSyncAlert instead.
	alerts := make([]AlertType, 0, len(n.Alerts))
	for _, a := range n.Alerts {
		if !hiddenAlert[a] {
			alerts = append(alerts, AlertType(a))
		}
	}
	return &NotificationSettings{
		SmtpHost: ptr(n.SMTPHost), SmtpPort: ptr(n.SMTPPort), SmtpSecurity: ptr(NotificationSettingsSmtpSecurity(n.SMTPSecurity)),
		SmtpUsername: ptr(n.SMTPUsername), SmtpPasswordSet: ptr(n.SMTPPassword != ""),
		FromAddress: ptr(n.FromAddress), FromName: ptr(n.FromName), MailConfigured: ptr(n.MailConfigured()),
		LinkUrl: ptr(n.LinkURL), Alerts: &alerts, DvrQuotaAlert: ptr(n.AlertOn(settings.AlertDVRQuota)),
		PlexSyncAlert: ptr(n.AlertOn(settings.AlertPlexSync)), RecipientsAllAdmins: ptr(n.RecipientsAllAdmins),
		Recipients: ptr(append([]int64{}, n.Recipients...)), DiskThresholdPercent: ptr(n.DiskThresholdPercent),
	}
}

func applyNotifications(n *settings.Notifications, u NotificationSettingsUpdate) {
	trim := func(dst *string, src *string) {
		if src != nil {
			*dst = strings.TrimSpace(*src)
		}
	}
	trim(&n.SMTPHost, u.SmtpHost)
	set(&n.SMTPPort, u.SmtpPort)
	if u.SmtpSecurity != nil {
		n.SMTPSecurity = string(*u.SmtpSecurity)
	}
	trim(&n.SMTPUsername, u.SmtpUsername)
	set(&n.SMTPPassword, u.SmtpPassword)
	trim(&n.FromAddress, u.FromAddress)
	trim(&n.FromName, u.FromName)
	if u.LinkUrl != nil {
		n.LinkURL = strings.TrimRight(strings.TrimSpace(*u.LinkUrl), "/")
	}
	// Alert types outside the AlertType enum keep their state unless their own flag is sent.
	flags := map[string]bool{settings.AlertDVRQuota: n.AlertOn(settings.AlertDVRQuota), settings.AlertPlexSync: n.AlertOn(settings.AlertPlexSync)}
	if u.DvrQuotaAlert != nil {
		flags[settings.AlertDVRQuota] = *u.DvrQuotaAlert
	}
	if u.PlexSyncAlert != nil {
		flags[settings.AlertPlexSync] = *u.PlexSyncAlert
	}
	if u.Alerts != nil {
		n.Alerts = []string{}
		for _, a := range *u.Alerts {
			if !hiddenAlert[string(a)] {
				n.Alerts = append(n.Alerts, string(a))
			}
		}
	}
	if u.Alerts != nil || u.DvrQuotaAlert != nil || u.PlexSyncAlert != nil {
		kept := n.Alerts[:0:0]
		for _, a := range n.Alerts {
			if !hiddenAlert[a] {
				kept = append(kept, a)
			}
		}
		for _, a := range []string{settings.AlertDVRQuota, settings.AlertPlexSync} {
			if flags[a] {
				kept = append(kept, a)
			}
		}
		n.Alerts = kept
	}
	set(&n.RecipientsAllAdmins, u.RecipientsAllAdmins)
	if u.Recipients != nil {
		n.Recipients = append([]int64{}, *u.Recipients...)
	}
	set(&n.DiskThresholdPercent, u.DiskThresholdPercent)
}

func (h *Handlers) TestEmail(ctx context.Context, req TestEmailRequestObject) (TestEmailResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return TestEmail401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return TestEmail403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	to, err := auth.NormalizeEmail(req.Body.To)
	if err != nil || to == "" {
		return TestEmail400JSONResponse{BadRequestJSONResponse(apiErr("invalid", "enter the address to send the test to"))}, nil
	}
	if h.Mail == nil {
		return TestEmail200JSONResponse{Ok: false, Error: ptr(notify.ErrNotConfigured.Error())}, nil
	}
	if err := h.Mail.Send(ctx, notify.TestMail(to, h.Settings.Get().General.ServerName)); err != nil {
		return TestEmail200JSONResponse{Ok: false, Error: ptr(err.Error())}, nil
	}
	return TestEmail200JSONResponse{Ok: true}, nil
}

// ---------- the bell ----------

func (h *Handlers) ListNotifications(ctx context.Context, _ ListNotificationsRequestObject) (ListNotificationsResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return ListNotifications401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return ListNotifications403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	out := ListNotifications200JSONResponse{Items: []AdminNotification{}}
	if h.Alerts == nil {
		return out, nil
	}
	list, unread, err := h.Alerts.List(ctx, 100)
	if err != nil {
		return nil, internal(ctx, "notifications", err)
	}
	out.Unread = unread
	for _, n := range list {
		out.Items = append(out.Items, AdminNotification{Id: n.ID, Type: n.Type, Subject: nz(n.Subject), Title: n.Title,
			Message: n.Message, CreatedAt: n.CreatedAt, Read: n.Read})
	}
	return out, nil
}

func (h *Handlers) MarkAllNotificationsRead(ctx context.Context, _ MarkAllNotificationsReadRequestObject) (MarkAllNotificationsReadResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return MarkAllNotificationsRead401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return MarkAllNotificationsRead403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	if h.Alerts != nil {
		if err := h.Alerts.MarkRead(ctx, 0); err != nil {
			return nil, internal(ctx, "notificationsRead", err)
		}
	}
	return MarkAllNotificationsRead204Response{}, nil
}

func (h *Handlers) MarkNotificationRead(ctx context.Context, req MarkNotificationReadRequestObject) (MarkNotificationReadResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return MarkNotificationRead401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return MarkNotificationRead403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	if h.Alerts == nil || req.NotificationId <= 0 {
		return MarkNotificationRead404JSONResponse{NotFoundJSONResponse(apiErr("not_found", notify.ErrNotFound.Error()))}, nil
	}
	err := h.Alerts.MarkRead(ctx, req.NotificationId)
	if errors.Is(err, notify.ErrNotFound) {
		return MarkNotificationRead404JSONResponse{NotFoundJSONResponse(apiErr("not_found", err.Error()))}, nil
	}
	if err != nil {
		return nil, internal(ctx, "notificationRead", err)
	}
	return MarkNotificationRead204Response{}, nil
}

func (h *Handlers) DismissNotification(ctx context.Context, req DismissNotificationRequestObject) (DismissNotificationResponseObject, error) {
	switch authed, admin := isAdmin(ctx); {
	case !authed:
		return DismissNotification401JSONResponse{UnauthorizedJSONResponse(errUnauthorized)}, nil
	case !admin:
		return DismissNotification403JSONResponse{ForbiddenJSONResponse(errForbidden)}, nil
	}
	if h.Alerts == nil {
		return DismissNotification404JSONResponse{NotFoundJSONResponse(apiErr("not_found", notify.ErrNotFound.Error()))}, nil
	}
	err := h.Alerts.Dismiss(ctx, req.NotificationId)
	if errors.Is(err, notify.ErrNotFound) {
		return DismissNotification404JSONResponse{NotFoundJSONResponse(apiErr("not_found", err.Error()))}, nil
	}
	if err != nil {
		return nil, internal(ctx, "dismissNotification", err)
	}
	return DismissNotification204Response{}, nil
}

// ---------- forgot / reset ----------

// ForgotPassword mails a reset link when the account exists, has an email address and
// mail works. The answer is the same in every case, and the work happens after answering,
// so neither the response nor its timing tells whether the account exists.
func (h *Handlers) ForgotPassword(ctx context.Context, req ForgotPasswordRequestObject) (ForgotPasswordResponseObject, error) {
	b := req.Body
	kind := string(b.Kind)
	if kind != auth.ResetPassword && kind != auth.ResetPIN {
		return ForgotPassword400JSONResponse{BadRequestJSONResponse(apiErr("invalid", "kind must be password or pin"))}, nil
	}
	login := strings.TrimSpace(b.Login)
	if login == "" || len(login) > 254 {
		return ForgotPassword400JSONResponse{BadRequestJSONResponse(apiErr("invalid", "enter your username or email address"))}, nil
	}
	ip := requestInfo(ctx).ClientIP
	// The per-address limit is the same for every login, so saying so reveals nothing.
	if ipOK, _ := h.Auth.ForgotAllowed(ip, ""); !ipOK {
		return ForgotPassword429JSONResponse{TooManyRequestsJSONResponse(apiErr("rate_limited", "too many reset requests; try again later"))}, nil
	}
	go h.sendResetLink(context.WithoutCancel(ctx), login, kind, ip)
	return ForgotPassword204Response{}, nil
}

func (h *Handlers) sendResetLink(ctx context.Context, login, kind, ip string) {
	u, err := h.Auth.FindAccount(ctx, login)
	if err != nil {
		if !errors.Is(err, auth.ErrUserNotFound) {
			slog.Warn("reset: find account", "err", err)
		}
		return
	}
	if _, accountOK := h.Auth.ForgotAllowed("", "user:"+strconv.FormatInt(u.ID, 10)); !accountOK {
		slog.Warn("reset: too many requests for one account", "user", u.Username, "ip", ip)
		return
	}
	// Managed profiles have no password; friends don't use PINs on this server's picker.
	if u.Email == "" || (kind == auth.ResetPassword && u.IsManaged) || !h.Mail.Configured() {
		return
	}
	cfg := h.Settings.Get()
	base := cfg.LinkBase()
	if base == "" {
		slog.Warn("reset: no address for links; set Settings → Notifications → Link address")
		return
	}
	token, err := h.Auth.CreateReset(ctx, u.ID, kind, time.Now())
	if err != nil {
		slog.Warn("reset: create", "err", err)
		return
	}
	link := base + "/reset?token=" + token
	msg := notify.ResetLinkMail(u.Email, cfg.General.ServerName, u.DisplayName, kind, link, int(auth.ResetTTL/time.Minute))
	if err := h.Mail.Send(ctx, msg); err != nil {
		slog.Warn("reset: mail", "err", err)
		return
	}
	slog.Info("reset link sent", "user", u.Username, "kind", kind, "ip", ip)
}

func (h *Handlers) ResetInfo(ctx context.Context, req ResetInfoRequestObject) (ResetInfoResponseObject, error) {
	u, kind, err := h.Auth.ResetInfo(ctx, req.Body.Token, time.Now())
	if errors.Is(err, auth.ErrResetInvalid) {
		return ResetInfo400JSONResponse{BadRequestJSONResponse(apiErr("invalid_token", err.Error()))}, nil
	}
	if err != nil {
		return nil, internal(ctx, "resetInfo", err)
	}
	return ResetInfo200JSONResponse{Kind: ResetInfoKind(kind), DisplayName: u.DisplayName}, nil
}

func (h *Handlers) ResetPassword(ctx context.Context, req ResetPasswordRequestObject) (ResetPasswordResponseObject, error) {
	b := req.Body
	pw, pin := "", ""
	set(&pw, b.Password)
	set(&pin, b.Pin)
	u, kind, err := h.Auth.CompleteReset(ctx, b.Token, pw, pin, time.Now())
	switch {
	case errors.Is(err, auth.ErrResetInvalid):
		return ResetPassword400JSONResponse{BadRequestJSONResponse(apiErr("invalid_token", err.Error()))}, nil
	case errors.Is(err, auth.ErrResetValue):
		msg := "the new password must be at least 8 characters"
		if kind == auth.ResetPIN {
			msg = "the new PIN must be 4 digits"
		}
		return ResetPassword400JSONResponse{BadRequestJSONResponse(apiErr("invalid", msg))}, nil
	case err != nil:
		return nil, internal(ctx, "reset", err)
	}
	slog.InfoContext(ctx, "reset completed", "user", u.Username, "kind", kind, "ip", requestInfo(ctx).ClientIP)
	if h.Remote != nil {
		h.Remote.DropUser(u.ID)
	}
	if u.Email != "" && h.Mail.Configured() {
		msg := notify.ResetDoneMail(u.Email, h.Settings.Get().General.ServerName, u.DisplayName, kind)
		go func() {
			if err := h.Mail.Send(context.WithoutCancel(ctx), msg); err != nil {
				slog.Warn("reset: confirmation mail", "err", err)
			}
		}()
	}
	return ResetPassword204Response{}, nil
}
