package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/mmzlopasa/locarr/server/internal/api/gen"
	"github.com/mmzlopasa/locarr/server/internal/auth"
	"github.com/mmzlopasa/locarr/server/internal/notify"
	"github.com/mmzlopasa/locarr/server/internal/notify/channels"
	"github.com/mmzlopasa/locarr/server/internal/notify/delivery"
)

func mapChannelError(err error) *StatusError {
	switch {
	case errors.Is(err, delivery.ErrNotFound):
		return statusErr(http.StatusNotFound, "not_found", err)
	case errors.Is(err, delivery.ErrKindForbidden), errors.Is(err, delivery.ErrEventNotAllow):
		return statusErr(http.StatusForbidden, "forbidden", err)
	case errors.Is(err, delivery.ErrInvalid), errors.Is(err, channels.ErrInvalid), errors.Is(err, channels.ErrUnsupported):
		return statusErr(http.StatusBadRequest, "validation", err)
	}
	return nil
}

func toChannel(c delivery.Channel) gen.NotificationChannel {
	out := gen.NotificationChannel{ID: c.ID, Name: c.Name, Kind: gen.NotificationChannelKind(c.Kind), Enabled: c.Enabled, Personal: c.Personal(),
		Events: []gen.NotificationEventType{}, SecretsSet: c.Secrets.Names(), Failures: c.Failures, LastError: optStr(c.LastError),
		LastErrorAt: optTime(c.LastErrorAt), LastSentAt: optTime(c.LastSentAt), CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt}
	if out.SecretsSet == nil {
		out.SecretsSet = []string{}
	}
	if c.UserID > 0 {
		out.OwnerId = gen.NewOptInt64(c.UserID)
	}
	for _, e := range c.Events {
		out.Events = append(out.Events, gen.NotificationEventType(e))
	}
	cfg := c.Config
	s := gen.NotificationChannelSettings{URL: optStr(cfg.URL), Topic: optStr(cfg.Topic), Tag: optStr(cfg.Tag), ChatId: optStr(cfg.ChatID),
		Sound: optStr(cfg.Sound), Device: optStr(cfg.Device), Username: optStr(cfg.Username), Tags: cfg.Tags, To: cfg.To}
	if cfg.Method != "" {
		s.Method = gen.NewOptNotificationChannelSettingsMethod(gen.NotificationChannelSettingsMethod(cfg.Method))
	}
	if cfg.Priority != 0 {
		s.Priority = gen.NewOptInt(cfg.Priority)
	}
	if cfg.Language != "" {
		s.Language = gen.NewOptNotificationChannelSettingsLanguage(gen.NotificationChannelSettingsLanguage(cfg.Language))
	}
	out.Settings = s
	return out
}

func fromChannelInput(req *gen.NotificationChannelInput) delivery.Input {
	in := delivery.Input{Name: req.Name, Kind: channels.Kind(req.Kind), Enabled: req.Enabled.Or(true)}
	for _, e := range req.Events {
		in.Events = append(in.Events, notify.Type(e))
	}
	if s, ok := req.Settings.Get(); ok {
		in.Config = channels.Config{URL: s.URL.Or(""), Method: string(s.Method.Or("")), Topic: s.Topic.Or(""), Priority: s.Priority.Or(0),
			Tags: s.Tags, Tag: s.Tag.Or(""), ChatID: s.ChatId.Or(""), Sound: s.Sound.Or(""), Device: s.Device.Or(""), To: s.To,
			Username: s.Username.Or(""), Language: string(s.Language.Or(""))}
	}
	if sec, ok := req.Secrets.Get(); ok {
		ptr := func(o gen.OptString) *string {
			if v, ok := o.Get(); ok {
				return &v
			}
			return nil
		}
		in.Secrets = delivery.SecretsPatch{Token: ptr(sec.Token), Secret: ptr(sec.Secret), BotToken: ptr(sec.BotToken),
			WebhookURL: ptr(sec.WebhookUrl), UserKey: ptr(sec.UserKey), Password: ptr(sec.Password)}
	}
	return in
}

// channelAccess returns the channel if p may see and change it: its owner,
// or an administrator for shared channels.
func (h *Handler) channelAccess(ctx context.Context, p auth.Principal, id int64) (delivery.Channel, error) {
	c, err := h.Channels.Get(ctx, id)
	if err != nil {
		return c, err
	}
	if c.UserID == p.User.ID || (c.UserID == 0 && p.IsAdmin()) {
		return c, nil
	}
	return c, delivery.ErrNotFound
}

// ListNotificationChannels implements GET /notificationchannels.
func (h *Handler) ListNotificationChannels(ctx context.Context, params gen.ListNotificationChannelsParams) ([]gen.NotificationChannel, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	owner := p.User.ID
	if !params.Personal.Or(false) {
		if !p.IsAdmin() {
			return nil, errForbidden
		}
		owner = 0
	}
	list, err := h.Channels.List(ctx, owner)
	if err != nil {
		return nil, err
	}
	out := make([]gen.NotificationChannel, 0, len(list))
	for _, c := range list {
		out = append(out, toChannel(c))
	}
	return out, nil
}

func ownerFor(p auth.Principal, personal bool) (delivery.Owner, error) {
	if personal {
		return delivery.Owner{UserID: p.User.ID, Admin: p.IsAdmin()}, nil
	}
	if !p.IsAdmin() {
		return delivery.Owner{}, errForbidden
	}
	return delivery.Owner{}, nil
}

// CreateNotificationChannel implements POST /notificationchannels.
func (h *Handler) CreateNotificationChannel(ctx context.Context, req *gen.NotificationChannelInput) (*gen.NotificationChannel, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	o, err := ownerFor(p, req.Personal.Or(false))
	if err != nil {
		return nil, err
	}
	c, err := h.Channels.Create(ctx, o, fromChannelInput(req))
	if err != nil {
		return nil, err
	}
	out := toChannel(c)
	return &out, nil
}

// GetNotificationChannel implements GET /notificationchannels/{id}.
func (h *Handler) GetNotificationChannel(ctx context.Context, params gen.GetNotificationChannelParams) (*gen.NotificationChannel, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	c, err := h.channelAccess(ctx, p, params.ID)
	if err != nil {
		return nil, err
	}
	out := toChannel(c)
	return &out, nil
}

// UpdateNotificationChannel implements PUT /notificationchannels/{id}.
func (h *Handler) UpdateNotificationChannel(ctx context.Context, req *gen.NotificationChannelInput,
	params gen.UpdateNotificationChannelParams) (*gen.NotificationChannel, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := h.channelAccess(ctx, p, params.ID); err != nil {
		return nil, err
	}
	c, err := h.Channels.Update(ctx, params.ID, p.IsAdmin(), fromChannelInput(req))
	if err != nil {
		return nil, err
	}
	out := toChannel(c)
	return &out, nil
}

// DeleteNotificationChannel implements DELETE /notificationchannels/{id}.
func (h *Handler) DeleteNotificationChannel(ctx context.Context, params gen.DeleteNotificationChannelParams) error {
	p, err := principal(ctx)
	if err != nil {
		return err
	}
	if _, err := h.channelAccess(ctx, p, params.ID); err != nil {
		return err
	}
	return h.Channels.Delete(ctx, params.ID)
}

// TestNotificationChannel implements POST /notificationchannels/test.
func (h *Handler) TestNotificationChannel(ctx context.Context, req *gen.NotificationChannelTestRequest) (*gen.NotificationTestResult, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	if p.ReadOnly {
		return nil, errReadOnly
	}
	personal := req.Channel.Personal.Or(false)
	id := req.ID.Or(0)
	if id > 0 {
		c, err := h.channelAccess(ctx, p, id)
		if err != nil {
			return nil, err
		}
		personal = c.Personal()
	}
	o, err := ownerFor(p, personal)
	if err != nil {
		return nil, err
	}
	if err := h.Channels.Test(ctx, o, id, fromChannelInput(&req.Channel)); err != nil {
		if se := mapChannelError(err); se != nil && se.Status != http.StatusBadRequest {
			return nil, err
		}
		return &gen.NotificationTestResult{Ok: false, Message: gen.NewOptString(err.Error())}, nil
	}
	return &gen.NotificationTestResult{Ok: true}, nil
}

func (h *Handler) notificationSettings(ctx context.Context) (*gen.NotificationSettings, error) {
	base, smtp, pwSet, err := h.Channels.LoadSettings(ctx)
	if err != nil {
		return nil, err
	}
	return &gen.NotificationSettings{BaseUrl: base, SMTP: gen.SmtpSettings{Host: smtp.Host, Port: smtp.Port,
		Security: gen.SmtpSecurity(smtp.Security), Username: smtp.Username, Password: gen.SecretState{Configured: pwSet}, From: smtp.From}}, nil
}

// GetNotificationSettings implements GET /settings/notifications.
func (h *Handler) GetNotificationSettings(ctx context.Context) (*gen.NotificationSettings, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	return h.notificationSettings(ctx)
}

// UpdateNotificationSettings implements PUT /settings/notifications.
func (h *Handler) UpdateNotificationSettings(ctx context.Context, req *gen.NotificationSettingsInput) (*gen.NotificationSettings, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	var base *string
	if v, ok := req.BaseUrl.Get(); ok {
		base = &v
	}
	var mail *delivery.MailInput
	if m, ok := req.SMTP.Get(); ok {
		_, cur, _, err := h.Channels.LoadSettings(ctx)
		if err != nil {
			return nil, err
		}
		cur.Host, cur.Port, cur.Security = m.Host.Or(cur.Host), m.Port.Or(cur.Port), string(m.Security.Or(gen.SmtpSecurity(cur.Security)))
		cur.Username, cur.From = m.Username.Or(cur.Username), m.From.Or(cur.From)
		mail = &delivery.MailInput{SMTP: cur}
		if pw, ok := m.Password.Get(); ok {
			mail.Password = &pw
		}
	}
	if err := h.Channels.SaveSettings(ctx, base, mail); err != nil {
		return nil, err
	}
	return h.notificationSettings(ctx)
}
