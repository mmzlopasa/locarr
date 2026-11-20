package api

import (
	"context"
	"net/http"
	"net/netip"
)

type reqKey struct{}

// RequestInfo is what handlers need to know about the HTTP request.
type RequestInfo struct {
	Method   string
	Secure   bool // the client connection is HTTPS (directly or through the proxy)
	ClientIP netip.Addr
}

// WithRequest stores request facts for the handlers.
func WithRequest(ctx context.Context, info RequestInfo) context.Context {
	return context.WithValue(ctx, reqKey{}, info)
}

func requestInfo(ctx context.Context) RequestInfo {
	ri, _ := ctx.Value(reqKey{}).(RequestInfo)
	return ri
}

func sessionCookie(ctx context.Context, token string, maxAge int) string {
	// Secure follows the client connection: plain-HTTP LAN access (http://host:8484)
	// must keep working, and browsers drop Secure cookies there. HttpOnly and
	// SameSite=Strict are always on.
	c := &http.Cookie{ // #nosec G124 -- Secure is conditional by design, see above
		Name:     SessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   requestInfo(ctx).Secure,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   maxAge,
	}
	return c.String()
}
