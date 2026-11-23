package auth

import "context"

// Sign-in providers (D-058). Locarr has one built in: local accounts with a
// username and password. P9 adds "streamero" so the same people who watch on
// Streamero can request here without a second password. This file only
// reserves the shape; nothing outside tests implements Provider yet.

// ProviderKind tells clients how to sign in with a provider.
type ProviderKind string

// Provider kinds.
const (
	// ProviderPassword: a username and password form (local accounts, or a
	// provider that checks credentials against its own server).
	ProviderPassword ProviderKind = "password"
	// ProviderRedirect: the browser or app goes to the provider and comes back
	// with a one-time code (reserved).
	ProviderRedirect ProviderKind = "redirect"
)

// LocalProvider is the id of the built-in accounts.
const LocalProvider = "local"

// ProviderInfo is what GET /auth/providers lists.
type ProviderInfo struct {
	ID   string
	Kind ProviderKind
	Name string
}

// Identity is an account at a provider. Subject is the provider's stable id
// for the account and never changes; Username is only a suggestion for the
// local account created on the first sign-in.
type Identity struct {
	Provider string
	Subject  string
	Username string
	// Admin reports that the provider calls this account an administrator.
	// It is only a hint: Locarr never promotes a user to admin because of it.
	Admin bool
}

// Credentials are what a client sends to a provider's sign-in: a username
// and password for ProviderPassword, a code (and its redirect URL) for
// ProviderRedirect.
type Credentials struct {
	Username, Password string
	// TOTPCode is the provider's own two-factor code, when it asks for one.
	TOTPCode          string
	Code, RedirectURL string
}

// Provider verifies credentials at another service.
//
// Sign-in through a provider (P9): Authenticate returns the Identity; Locarr
// looks it up in user_identities (provider, subject) and signs that user in
// with a normal session (same cookie, same bearer tokens, same roles and
// permissions). An unknown identity creates a local user only if the
// provider's settings allow it, with the role, permissions and quotas those
// settings give (like an invite); otherwise sign-in is refused. A local user
// can link an identity from the account page. Disabled users stay disabled
// whatever the provider says.
type Provider interface {
	Info() ProviderInfo
	Authenticate(ctx context.Context, c Credentials) (Identity, error)
}

// Available is implemented by providers that can be switched off: they are
// listed and usable only while Available is true.
type Available interface {
	Available(ctx context.Context) bool
}

// SignUpPolicy is implemented by providers whose unknown accounts may create
// local users. The role is never admin (enforced here).
type SignUpPolicy interface {
	AutoCreate(ctx context.Context) (allowed bool, role Role, perms Permissions)
}

// Providers lists the ways to sign in: local accounts and every registered
// provider that is available.
func (s *Service) Providers(ctx context.Context) []ProviderInfo {
	out := []ProviderInfo{{ID: LocalProvider, Kind: ProviderPassword, Name: "Locarr"}}
	for _, p := range s.providers {
		if a, ok := p.(Available); ok && !a.Available(ctx) {
			continue
		}
		out = append(out, p.Info())
	}
	return out
}

// RegisterProvider adds a sign-in provider (P9).
func (s *Service) RegisterProvider(p Provider) { s.providers = append(s.providers, p) }
