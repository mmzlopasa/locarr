package streamero

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/auth"
)

// ProviderID is the sign-in provider id.
const ProviderID = "streamero"

// Provider is "Sign in with Streamero" (D-063). Credentials are checked with
// Streamero's own sign-in; the session Streamero creates for the check is
// ended at once, and neither the password nor the token is kept.
type Provider struct{ S *Service }

var (
	_ auth.Provider     = Provider{}
	_ auth.Available    = Provider{}
	_ auth.SignUpPolicy = Provider{}
)

// Info implements auth.Provider.
func (Provider) Info() auth.ProviderInfo {
	return auth.ProviderInfo{ID: ProviderID, Kind: auth.ProviderPassword, Name: "Streamero"}
}

// Available implements auth.Available: Streamero is set up and sign-in is on.
func (p Provider) Available(ctx context.Context) bool {
	c, err := p.S.Load(ctx)
	return err == nil && c.Enabled && c.SignIn.Enabled && c.URL != ""
}

// AutoCreate implements auth.SignUpPolicy: requesters with their default
// permissions (quotas are the request defaults).
func (p Provider) AutoCreate(ctx context.Context) (bool, auth.Role, auth.Permissions) {
	c, err := p.S.Load(ctx)
	if err != nil || !c.SignIn.AutoCreate {
		return false, "", 0
	}
	return true, auth.RoleRequester, auth.DefaultPermissions(auth.RoleRequester)
}

// Authenticate implements auth.Provider.
func (p Provider) Authenticate(ctx context.Context, c auth.Credentials) (auth.Identity, error) {
	if strings.TrimSpace(c.Username) == "" || c.Password == "" {
		return auth.Identity{}, auth.ErrInvalidCredentials
	}
	cfg, err := p.S.Load(ctx)
	if err != nil {
		return auth.Identity{}, err
	}
	cl, err := p.S.NewClient(ctx, cfg.URL, "")
	if err != nil {
		return auth.Identity{}, fmt.Errorf("%w: %v", auth.ErrProviderUnavailable, err)
	}
	token, u, err := cl.Login(ctx, c.Username, c.Password, c.TOTPCode)
	switch {
	case errors.Is(err, ErrInvalidCredentials):
		return auth.Identity{}, auth.ErrInvalidCredentials
	case errors.Is(err, ErrTOTPRequired):
		return auth.Identity{}, auth.ErrTOTPRequired
	case errors.Is(err, ErrInvalidTOTP):
		return auth.Identity{}, auth.ErrInvalidTOTP
	case errors.Is(err, ErrLocked):
		return auth.Identity{}, auth.ErrInvalidCredentials
	case err != nil:
		return auth.Identity{}, fmt.Errorf("%w: %v", auth.ErrProviderUnavailable, err)
	}
	// End the session Streamero just created; a failure is logged (the token
	// is never stored, and Streamero expires sessions itself).
	lctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if err := cl.Logout(lctx, token); err != nil {
		p.S.log().Warn("could not end the Streamero session used for sign-in", "err", err)
	}
	if u.Disabled {
		return auth.Identity{}, auth.ErrInvalidCredentials
	}
	name := u.Username
	if name == "" {
		name = c.Username
	}
	return auth.Identity{Provider: ProviderID, Subject: u.ID, Username: name, Admin: u.IsAdmin}, nil
}
