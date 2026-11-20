package api

import (
	"github.com/mmzlopasa/locarr/server/internal/api/gen"
	"github.com/mmzlopasa/locarr/server/internal/auth"
)

func toPermissions(p auth.Permissions) gen.Permissions {
	return gen.Permissions{
		RequestMovies:  p.Has(auth.PermRequestMovies),
		RequestSeries:  p.Has(auth.PermRequestSeries),
		RequestMusic:   p.Has(auth.PermRequestMusic),
		AutoApprove:    p.Has(auth.PermAutoApprove),
		Request4k:      p.Has(auth.PermRequest4K),
		ManageLibrary:  p.Has(auth.PermManageLibrary),
		ManageRequests: p.Has(auth.PermManageRequests),
	}
}

func fromPermissions(g gen.Permissions) auth.Permissions {
	var p auth.Permissions
	set := func(on bool, bit auth.Permissions) {
		if on {
			p |= bit
		}
	}
	set(g.RequestMovies, auth.PermRequestMovies)
	set(g.RequestSeries, auth.PermRequestSeries)
	set(g.RequestMusic, auth.PermRequestMusic)
	set(g.AutoApprove, auth.PermAutoApprove)
	set(g.Request4k, auth.PermRequest4K)
	set(g.ManageLibrary, auth.PermManageLibrary)
	set(g.ManageRequests, auth.PermManageRequests)
	return p
}

func toUser(u auth.User) gen.User {
	perms := u.Permissions
	if u.Role == auth.RoleAdmin {
		perms = auth.DefaultPermissions(auth.RoleAdmin)
	}
	return gen.User{
		ID: u.ID, Username: u.Username, Role: gen.Role(u.Role), Permissions: toPermissions(perms),
		Language: gen.Language(u.Language), Disabled: u.Disabled, TotpEnabled: u.TOTPEnabled,
		LockedUntil: optTime(u.LockedUntil), CreatedAt: u.CreatedAt, LastLoginAt: optTime(u.LastLoginAt),
	}
}
