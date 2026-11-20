package app

import "context"

type cookieAuthKey struct{}

func withCookieAuth(ctx context.Context) context.Context {
	return context.WithValue(ctx, cookieAuthKey{}, true)
}

// cookieAuth reports a request authenticated by the web session cookie.
func cookieAuth(ctx context.Context) bool {
	v, _ := ctx.Value(cookieAuthKey{}).(bool)
	return v
}
