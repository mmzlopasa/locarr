package auth

// Role is a user's role.
type Role string

// Roles.
const (
	RoleAdmin     Role = "admin"     // everything
	RoleManager   Role = "manager"   // library and requests, as permissions allow
	RoleRequester Role = "requester" // asks for titles
)

// Valid reports whether r is a known role.
func (r Role) Valid() bool { return r == RoleAdmin || r == RoleManager || r == RoleRequester }

// Permissions is a bitset of what a non-administrator may do. It is stored as
// an integer, so new permissions only need a new bit (D-005).
type Permissions uint64

// Permission bits. Never renumber: the values are stored.
const (
	PermRequestMovies Permissions = 1 << iota
	PermRequestSeries
	PermRequestMusic
	PermAutoApprove
	PermRequest4K
	PermManageLibrary
	PermManageRequests
	// Per-kind auto-approval (P8); PermAutoApprove still covers every kind.
	PermAutoApproveMovies
	PermAutoApproveSeries
	PermAutoApproveMusic
	PermAutoApprove4K
	// PermViewAdult sees the optional adult library (D-102). It is never part
	// of a default, an invite or a role: an administrator grants it per user,
	// requesters can never hold it, and administrators need it too.
	PermViewAdult

	permAll = PermRequestMovies | PermRequestSeries | PermRequestMusic | PermAutoApprove |
		PermRequest4K | PermManageLibrary | PermManageRequests | PermAutoApproveMovies | PermAutoApproveSeries |
		PermAutoApproveMusic | PermAutoApprove4K
)

// Has reports whether every bit of want is set.
func (p Permissions) Has(want Permissions) bool { return p&want == want }

// Clean drops unknown bits.
func (p Permissions) Clean() Permissions { return p & (permAll | PermViewAdult) }

// ForRole drops what role r may never hold (view_adult for requesters).
func (p Permissions) ForRole(r Role) Permissions {
	p = p.Clean()
	if r != RoleAdmin && r != RoleManager {
		p &^= PermViewAdult
	}
	return p
}

// WithoutAdult drops view_adult (invites and sign-ins through other services
// never grant it).
func (p Permissions) WithoutAdult() Permissions { return p.Clean() &^ PermViewAdult }

// DefaultPermissions are what a new user of role r gets unless chosen otherwise.
func DefaultPermissions(r Role) Permissions {
	switch r {
	case RoleAdmin:
		return permAll
	case RoleManager:
		return PermRequestMovies | PermRequestSeries | PermRequestMusic | PermAutoApprove | PermManageLibrary | PermManageRequests |
			PermAutoApproveMovies | PermAutoApproveSeries | PermAutoApproveMusic
	default:
		return PermRequestMovies | PermRequestSeries | PermRequestMusic
	}
}

// Can reports whether the principal may do what perm allows (admins always
// may, except view adult content, which every account needs explicitly).
func (p Principal) Can(perm Permissions) bool {
	if perm&PermViewAdult != 0 {
		return p.CanViewAdult()
	}
	return p.User.Role == RoleAdmin || p.User.Permissions.Has(perm)
}

// CanViewAdult reports whether the caller may see adult content at all
// (whether the feature is enabled is checked by the adult package): the
// account holds view_adult explicitly, is not a requester, and an API key
// was created with "include adult".
func (p Principal) CanViewAdult() bool {
	if p.User.Role != RoleAdmin && p.User.Role != RoleManager {
		return false
	}
	if !p.User.Permissions.Has(PermViewAdult) {
		return false
	}
	return p.APIKeyID == 0 || p.IncludeAdult
}

// IsAdmin reports an administrator.
func (p Principal) IsAdmin() bool { return p.User.Role == RoleAdmin }
