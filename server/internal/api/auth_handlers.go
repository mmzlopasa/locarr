package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/mmzlopasa/locarr/server/internal/api/gen"
	"github.com/mmzlopasa/locarr/server/internal/auth"
	"github.com/mmzlopasa/locarr/server/internal/settings"
)

func device(ctx context.Context, d gen.OptDeviceInfo) auth.Device {
	dev := auth.Device{Name: d.Value.Name.Value, Client: d.Value.Client.Value}
	if ip := requestInfo(ctx).ClientIP; ip.IsValid() {
		dev.IP = ip.String()
	}
	return dev
}

func signedIn(ctx context.Context, p auth.Principal, token string, useCookie bool) *gen.SignedInHeaders {
	res := &gen.SignedInHeaders{Response: gen.AuthResult{User: toUser(p.User)}}
	if useCookie {
		res.SetCookie = gen.NewOptString(sessionCookie(ctx, token, int(auth.SessionTTL.Seconds())))
	} else {
		res.Response.Token = gen.NewOptString(token)
	}
	return res
}

// Setup implements POST /setup.
func (h *Handler) Setup(ctx context.Context, req *gen.SetupRequest) (*gen.SignedInHeaders, error) {
	p, token, err := h.Auth.Setup(ctx, req.SetupCode, string(req.Username), string(req.Password),
		string(req.Language.Or(gen.LanguageEn)), device(ctx, req.Device))
	if err != nil {
		return nil, err
	}
	if name, ok := req.InstanceName.Get(); ok && name != "" {
		if err := settings.Set(ctx, h.Settings, nil, settings.InstanceName, name); err != nil {
			return nil, err
		}
	}
	h.Log.Info("first-run setup completed", "user", p.User.Username)
	return signedIn(ctx, p, token, req.UseCookie.Or(false)), nil
}

// Login implements POST /auth/login.
func (h *Handler) Login(ctx context.Context, req *gen.LoginRequest) (*gen.SignedInHeaders, error) {
	p, token, err := h.Auth.Login(ctx, auth.LoginInput{
		Username: req.Username, Password: req.Password,
		TOTPCode: req.TotpCode.Or(""), RecoveryCode: req.RecoveryCode.Or(""),
		Device: device(ctx, req.Device),
	})
	if err != nil {
		var locked *auth.LockedError
		if errors.Is(err, auth.ErrInvalidCredentials) || errors.Is(err, auth.ErrInvalidTOTP) || errors.As(err, &locked) {
			h.Log.Warn("sign-in failed", "user", req.Username, "ip", requestInfo(ctx).ClientIP.String(), "reason", err.Error())
		}
		return nil, err
	}
	return signedIn(ctx, p, token, req.UseCookie.Or(false)), nil
}

// Logout implements POST /auth/logout.
func (h *Handler) Logout(ctx context.Context) (*gen.LogoutNoContent, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.Auth.Logout(ctx, p); err != nil {
		return nil, err
	}
	return &gen.LogoutNoContent{SetCookie: gen.NewOptString(sessionCookie(ctx, "", -1))}, nil
}

// GetMe implements GET /auth/me.
func (h *Handler) GetMe(ctx context.Context) (*gen.User, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	u := toUser(p.User)
	return &u, nil
}

// UpdateMe implements PATCH /auth/me.
func (h *Handler) UpdateMe(ctx context.Context, req *gen.UpdateMeRequest) (*gen.User, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	u := p.User
	if lang, ok := req.Language.Get(); ok {
		if u, err = h.Auth.UpdateLanguage(ctx, p.User.ID, string(lang)); err != nil {
			return nil, err
		}
	}
	out := toUser(u)
	return &out, nil
}

// ChangePassword implements POST /auth/password.
func (h *Handler) ChangePassword(ctx context.Context, req *gen.ChangePasswordRequest) error {
	p, err := principal(ctx)
	if err != nil {
		return err
	}
	return h.Auth.ChangePassword(ctx, p, req.CurrentPassword, string(req.NewPassword))
}

// ListSessions implements GET /auth/sessions.
func (h *Handler) ListSessions(ctx context.Context) ([]gen.Session, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	list, err := h.Auth.Sessions(ctx, p.User.ID)
	if err != nil {
		return nil, err
	}
	out := make([]gen.Session, 0, len(list))
	for _, s := range list {
		out = append(out, gen.Session{ID: s.ID, DeviceName: s.DeviceName, Client: s.Client, IP: s.IP,
			CreatedAt: s.CreatedAt, LastSeenAt: s.LastSeenAt, Current: s.ID == p.SessionID})
	}
	return out, nil
}

// RevokeSession implements DELETE /auth/sessions/{id}.
func (h *Handler) RevokeSession(ctx context.Context, params gen.RevokeSessionParams) error {
	p, err := principal(ctx)
	if err != nil {
		return err
	}
	return h.Auth.RevokeSession(ctx, p.User.ID, params.ID)
}

func toAPIKey(k auth.APIKey) gen.ApiKey {
	return gen.ApiKey{ID: k.ID, Name: k.Name, Prefix: k.Prefix, ReadOnly: k.ReadOnly, CreatedAt: k.CreatedAt,
		LastUsedAt: optTime(k.LastUsedAt)}
}

// ListApiKeys implements GET /auth/apikeys.
func (h *Handler) ListApiKeys(ctx context.Context) ([]gen.ApiKey, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	keys, err := h.Auth.APIKeys(ctx, p.User.ID)
	if err != nil {
		return nil, err
	}
	out := make([]gen.ApiKey, 0, len(keys))
	for _, k := range keys {
		out = append(out, toAPIKey(k))
	}
	return out, nil
}

// CreateApiKey implements POST /auth/apikeys.
func (h *Handler) CreateApiKey(ctx context.Context, req *gen.CreateApiKeyRequest) (*gen.CreatedApiKey, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	k, key, err := h.Auth.CreateAPIKey(ctx, p, req.Name, req.ReadOnly.Or(false))
	if err != nil {
		return nil, err
	}
	return &gen.CreatedApiKey{ApiKey: toAPIKey(k), Key: key}, nil
}

// DeleteApiKey implements DELETE /auth/apikeys/{id}.
func (h *Handler) DeleteApiKey(ctx context.Context, params gen.DeleteApiKeyParams) error {
	p, err := principal(ctx)
	if err != nil {
		return err
	}
	return h.Auth.DeleteAPIKey(ctx, p.User.ID, params.ID)
}

// StartTotpSetup implements POST /auth/totp/setup.
func (h *Handler) StartTotpSetup(ctx context.Context) (*gen.TotpSetup, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	secret, uri, err := h.Auth.StartTOTP(ctx, p)
	if err != nil {
		return nil, err
	}
	return &gen.TotpSetup{Secret: secret, URI: uri}, nil
}

// EnableTotp implements POST /auth/totp/enable.
func (h *Handler) EnableTotp(ctx context.Context, req *gen.TotpCodeRequest) (*gen.RecoveryCodes, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	codes, err := h.Auth.EnableTOTP(ctx, p, req.Code)
	if errors.Is(err, auth.ErrInvalidTOTP) {
		// A typo while signed in is not a lost session: 400, not 401.
		return nil, statusErr(http.StatusBadRequest, "invalid_totp", err)
	}
	if err != nil {
		return nil, err
	}
	return &gen.RecoveryCodes{Codes: codes}, nil
}

// DisableTotp implements POST /auth/totp/disable.
func (h *Handler) DisableTotp(ctx context.Context, req *gen.PasswordConfirm) error {
	p, err := principal(ctx)
	if err != nil {
		return err
	}
	return h.Auth.DisableTOTP(ctx, p, req.Password)
}

// RegenerateRecoveryCodes implements POST /auth/totp/recovery-codes.
func (h *Handler) RegenerateRecoveryCodes(ctx context.Context, req *gen.PasswordConfirm) (*gen.RecoveryCodes, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	codes, err := h.Auth.RegenerateRecoveryCodes(ctx, p, req.Password)
	if err != nil {
		return nil, err
	}
	return &gen.RecoveryCodes{Codes: codes}, nil
}
