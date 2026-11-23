package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/mmzlopasa/locarr/server/internal/api/gen"
	"github.com/mmzlopasa/locarr/server/internal/auth"
	"github.com/mmzlopasa/locarr/server/internal/indexers/httpx"
	"github.com/mmzlopasa/locarr/server/internal/mediaserver/streamero"
)

// mapMediaServerError maps Streamero, provider and channel errors.
func mapMediaServerError(err error) *StatusError {
	table := []struct {
		err    error
		status int
		code   string
	}{
		{auth.ErrUnknownProvider, http.StatusNotFound, "not_found"},
		{auth.ErrIdentityNotLinked, http.StatusForbidden, "identity_not_linked"},
		{auth.ErrIdentityTaken, http.StatusConflict, "identity_taken"},
		{auth.ErrProviderUnavailable, http.StatusServiceUnavailable, "provider_unavailable"},
		{streamero.ErrNotConfigured, http.StatusConflict, "not_configured"},
		{streamero.ErrBadURL, http.StatusBadRequest, "validation"},
		{streamero.ErrBadMapping, http.StatusBadRequest, "validation"},
		{streamero.ErrAuth, http.StatusBadRequest, "auth_failed"},
		{streamero.ErrNotAdmin, http.StatusBadRequest, "not_admin"},
		{streamero.ErrNotStreamero, http.StatusBadRequest, "not_streamero"},
		{streamero.ErrUnreachable, http.StatusBadGateway, "unreachable"},
		{httpx.ErrBlocked, http.StatusBadRequest, "address_blocked"},
	}
	for _, t := range table {
		if errors.Is(err, t.err) {
			return &StatusError{Status: t.status, Code: t.code, Message: err.Error()}
		}
	}
	return mapChannelError(err)
}

// streameroCode is the Test result code for an error.
func streameroCode(err error) string {
	switch {
	case errors.Is(err, httpx.ErrBlocked):
		return "address_blocked"
	case errors.Is(err, streamero.ErrAuth):
		return "auth_failed"
	case errors.Is(err, streamero.ErrNotAdmin):
		return "not_admin"
	case errors.Is(err, streamero.ErrNotStreamero):
		return "not_streamero"
	case errors.Is(err, streamero.ErrBadURL):
		return "invalid"
	}
	return "unreachable"
}

func (h *Handler) streameroSettings(ctx context.Context) (*gen.StreameroSettings, error) {
	c, err := h.Streamero.Load(ctx)
	if err != nil {
		return nil, err
	}
	keySet, err := h.Settings.Configured(ctx, streamero.APIKey)
	if err != nil {
		return nil, err
	}
	st, items, pending, err := h.Streamero.StatusInfo(ctx)
	if err != nil {
		return nil, err
	}
	out := &gen.StreameroSettings{Enabled: c.Enabled, URL: c.URL, PublicUrl: c.PublicURL, ApiKey: gen.SecretState{Configured: keySet},
		PathMappings: []gen.PathMapping{}, LibraryMap: []gen.StreameroLibraryMapping{},
		SignIn: gen.StreameroSignIn{Enabled: c.SignIn.Enabled, AutoCreate: c.SignIn.AutoCreate},
		Status: gen.StreameroStatus{IndexedItems: items, IndexedAt: optTime(st.IndexedAt), IndexError: optStr(st.IndexError),
			LastScanAt: optTime(st.LastScanAt), LastScanError: optStr(st.LastScanError), PendingChanges: gen.NewOptInt(pending)}}
	for _, m := range c.PathMappings {
		out.PathMappings = append(out.PathMappings, gen.PathMapping{LocarrPath: m.Locarr, StreameroPath: m.Streamero})
	}
	for _, m := range c.LibraryMap {
		ids := m.LibraryIDs
		if ids == nil {
			ids = []string{}
		}
		out.LibraryMap = append(out.LibraryMap, gen.StreameroLibraryMapping{RootFolderId: m.RootFolderID, LibraryIds: ids})
	}
	return out, nil
}

// GetStreameroSettings implements GET /mediaserver/streamero.
func (h *Handler) GetStreameroSettings(ctx context.Context) (*gen.StreameroSettings, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	return h.streameroSettings(ctx)
}

// UpdateStreameroSettings implements PUT /mediaserver/streamero.
func (h *Handler) UpdateStreameroSettings(ctx context.Context, req *gen.StreameroSettingsInput) (*gen.StreameroSettings, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	cur, err := h.Streamero.Load(ctx)
	if err != nil {
		return nil, err
	}
	c := streamero.Config{Enabled: req.Enabled, URL: req.URL, PublicURL: req.PublicUrl.Or(""), SignIn: cur.SignIn,
		PathMappings: []streamero.PathMapping{}, LibraryMap: []streamero.LibraryMapping{}}
	for _, m := range req.PathMappings {
		c.PathMappings = append(c.PathMappings, streamero.PathMapping{Locarr: m.LocarrPath, Streamero: m.StreameroPath})
	}
	for _, m := range req.LibraryMap {
		c.LibraryMap = append(c.LibraryMap, streamero.LibraryMapping{RootFolderID: m.RootFolderId, LibraryIDs: m.LibraryIds})
	}
	if v, ok := req.SignIn.Get(); ok {
		c.SignIn = streamero.SignIn{Enabled: v.Enabled, AutoCreate: v.AutoCreate}
	}
	in := streamero.Input{Config: c}
	if k, ok := req.ApiKey.Get(); ok {
		in.APIKey = &k
	}
	if _, err := h.Streamero.Save(ctx, in); err != nil {
		return nil, err
	}
	return h.streameroSettings(ctx)
}

func toStreameroLibraries(list []streamero.LibraryView) []gen.StreameroLibrary {
	out := make([]gen.StreameroLibrary, 0, len(list))
	for _, l := range list {
		out = append(out, gen.StreameroLibrary{ID: l.ID, Name: l.Name, Kind: l.Kind, Paths: l.Paths, ItemCount: l.ItemCount,
			SuggestedRootFolderIds: l.Suggested})
	}
	return out
}

// TestStreamero implements POST /mediaserver/streamero/test.
func (h *Handler) TestStreamero(ctx context.Context, req *gen.StreameroTestRequest) (*gen.StreameroTestResult, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	key, ok := req.ApiKey.Get()
	if !ok {
		var err error
		if key, err = h.Streamero.StoredKey(ctx); err != nil {
			return nil, err
		}
	}
	res, err := h.Streamero.Check(ctx, req.URL, key)
	out := &gen.StreameroTestResult{Ok: err == nil, Libraries: []gen.StreameroLibrary{}}
	if res.Info.Name != "" {
		out.ServerName, out.Version = optStr(res.Info.Name), optStr(res.Info.Version)
	}
	if err != nil {
		out.ErrorCode, out.Message = gen.NewOptString(streameroCode(err)), gen.NewOptString(err.Error())
		return out, nil
	}
	c, _ := h.Streamero.Load(ctx)
	roots, err := h.RootFolders.List(ctx)
	if err != nil {
		return nil, err
	}
	out.Libraries = toStreameroLibraries(streamero.Suggest(res.Libraries, roots, c.PathMappings))
	return out, nil
}

// ListStreameroLibraries implements GET /mediaserver/streamero/libraries.
func (h *Handler) ListStreameroLibraries(ctx context.Context) ([]gen.StreameroLibrary, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	list, err := h.Streamero.Libraries(ctx)
	if err != nil {
		return nil, err
	}
	return toStreameroLibraries(list), nil
}

// SyncStreamero implements POST /mediaserver/streamero/sync.
func (h *Handler) SyncStreamero(ctx context.Context) error {
	if _, err := admin(ctx); err != nil {
		return err
	}
	if !h.Streamero.Enabled(ctx) {
		return streamero.ErrNotConfigured
	}
	h.Streamero.QueueIndex(ctx, 0)
	return nil
}

// ListStreameroUsers implements GET /mediaserver/streamero/users.
func (h *Handler) ListStreameroUsers(ctx context.Context) ([]gen.StreameroUser, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	list, err := h.Streamero.Users(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]gen.StreameroUser, 0, len(list))
	for _, u := range list {
		x := gen.StreameroUser{ID: u.ID, Username: u.Username, IsAdmin: u.IsAdmin, Disabled: u.Disabled}
		if u.LinkedUserID > 0 {
			x.LinkedUserId, x.LinkedUsername = gen.NewOptInt64(u.LinkedUserID), gen.NewOptString(u.LinkedUsername)
		}
		out = append(out, x)
	}
	return out, nil
}

// --- provider sign-in and identities ---

// LoginWithProvider implements POST /auth/providers/{id}/login.
func (h *Handler) LoginWithProvider(ctx context.Context, req *gen.ProviderLoginRequest, params gen.LoginWithProviderParams) (*gen.SignedInHeaders, error) {
	p, token, created, err := h.Auth.LoginWithProvider(ctx, params.ID, auth.Credentials{Username: req.Username, Password: req.Password,
		TOTPCode: req.TotpCode.Or("")}, device(ctx, req.Device))
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) || errors.Is(err, auth.ErrInvalidTOTP) || errors.Is(err, auth.ErrIdentityNotLinked) {
			h.Log.Warn("provider sign-in failed", "provider", params.ID, "user", req.Username, "ip", requestInfo(ctx).ClientIP.String(),
				"reason", err.Error())
		}
		return nil, err
	}
	if created {
		h.Log.Info("user created from a provider sign-in", "provider", params.ID, "user", p.User.Username)
	}
	return signedIn(ctx, p, token, req.UseCookie.Or(false)), nil
}

func toIdentity(l auth.LinkedIdentity) gen.LinkedIdentity {
	return gen.LinkedIdentity{Provider: l.Provider, Subject: l.Subject, Username: l.Username, UserId: l.UserID, LinkedAt: l.LinkedAt,
		LastLoginAt: optTime(l.LastLoginAt)}
}

// ListMyIdentities implements GET /auth/identities.
func (h *Handler) ListMyIdentities(ctx context.Context) ([]gen.LinkedIdentity, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	list, err := h.Auth.Identities(ctx, p.User.ID)
	if err != nil {
		return nil, err
	}
	out := make([]gen.LinkedIdentity, 0, len(list))
	for _, l := range list {
		out = append(out, toIdentity(l))
	}
	return out, nil
}

// LinkMyIdentity implements POST /auth/identities/{provider}.
func (h *Handler) LinkMyIdentity(ctx context.Context, req *gen.ProviderCredentials, params gen.LinkMyIdentityParams) (*gen.LinkedIdentity, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	if p.SessionID == 0 {
		return nil, auth.ErrSessionRequired
	}
	id, err := h.Auth.VerifyWithProvider(ctx, params.Provider, auth.Credentials{Username: req.Username, Password: req.Password,
		TOTPCode: req.TotpCode.Or("")})
	switch {
	// The caller is signed in: a wrong password at the provider is not a lost
	// session (403, still counted towards the ban; 400 for the 2FA step).
	case errors.Is(err, auth.ErrInvalidCredentials), errors.Is(err, auth.ErrInvalidTOTP):
		return nil, statusErr(http.StatusForbidden, "invalid_credentials", err)
	case errors.Is(err, auth.ErrTOTPRequired):
		return nil, statusErr(http.StatusBadRequest, "totp_required", err)
	case err != nil:
		return nil, err
	}
	l, err := h.Auth.Link(ctx, p.User.ID, params.Provider, id.Subject, id.Username)
	if err != nil {
		return nil, err
	}
	out := toIdentity(l)
	return &out, nil
}

// UnlinkMyIdentity implements DELETE /auth/identities/{provider}.
func (h *Handler) UnlinkMyIdentity(ctx context.Context, params gen.UnlinkMyIdentityParams) error {
	p, err := principal(ctx)
	if err != nil {
		return err
	}
	return h.Auth.Unlink(ctx, p.User.ID, params.Provider)
}

// LinkUserIdentity implements PUT /users/{id}/identities/{provider}.
func (h *Handler) LinkUserIdentity(ctx context.Context, req *gen.LinkIdentityRequest, params gen.LinkUserIdentityParams) (*gen.LinkedIdentity, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	l, err := h.Auth.Link(ctx, params.ID, params.Provider, req.Subject, req.Username.Or(""))
	if err != nil {
		return nil, err
	}
	out := toIdentity(l)
	return &out, nil
}

// UnlinkUserIdentity implements DELETE /users/{id}/identities/{provider}.
func (h *Handler) UnlinkUserIdentity(ctx context.Context, params gen.UnlinkUserIdentityParams) error {
	if _, err := admin(ctx); err != nil {
		return err
	}
	return h.Auth.Unlink(ctx, params.ID, params.Provider)
}

// streameroLinks returns "Watch in Streamero" links by position (none when
// Streamero is off or the lookup fails: the page works without them).
func (h *Handler) streameroLinks(ctx context.Context, titles []streamero.Title) map[int]string {
	if h.Streamero == nil {
		return nil
	}
	links, err := h.Streamero.Links(ctx, titles)
	if err != nil {
		h.Log.Warn("Streamero links", "err", err)
		return nil
	}
	return links
}
