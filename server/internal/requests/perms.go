package requests

import "github.com/mmzlopasa/locarr/server/internal/auth"

func requestPerm(k Kind) auth.Permissions {
	switch k {
	case KindMovie:
		return auth.PermRequestMovies
	case KindSeries:
		return auth.PermRequestSeries
	}
	return auth.PermRequestMusic
}

func autoPerm(k Kind) auth.Permissions {
	switch k {
	case KindMovie:
		return auth.PermAutoApproveMovies
	case KindSeries:
		return auth.PermAutoApproveSeries
	}
	return auth.PermAutoApproveMusic
}

// CanRequest reports whether p may request kind (4K needs its own permission
// and only exists for movies and series).
func CanRequest(p auth.Principal, k Kind, is4K bool) bool {
	if is4K && (k == KindAlbum || k == KindArtist) {
		return false
	}
	if !p.Can(requestPerm(k)) {
		return false
	}
	return !is4K || p.Can(auth.PermRequest4K)
}

// AutoApproves reports whether a request of p is approved at once: the
// "everything" permission or the kind's own, and for 4K also the 4K one.
func AutoApproves(p auth.Principal, k Kind, is4K bool) bool {
	if p.IsAdmin() {
		return true
	}
	ok := p.User.Permissions.Has(auth.PermAutoApprove) || p.User.Permissions.Has(autoPerm(k))
	if is4K {
		ok = ok && p.User.Permissions.Has(auth.PermAutoApprove4K)
	}
	return ok
}

// CanManage reports whether p approves, declines and edits other people's
// requests and handles issues.
func CanManage(p auth.Principal) bool { return p.Can(auth.PermManageRequests) }

// QuotaExempt reports users whose requests do not count against quotas.
func QuotaExempt(p auth.Principal) bool { return CanManage(p) }
