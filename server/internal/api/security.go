package api

import (
	"context"
	"net/http"

	"github.com/mmzlopasa/locarr/server/internal/api/gen"
	"github.com/mmzlopasa/locarr/server/internal/auth"
)

type principalKey struct{}

// Security implements gen.SecurityHandler.
type Security struct{ Auth *auth.Service }

// HandleSessionToken resolves the bearer token (session or API key). A
// read-only API key is refused for anything but GET.
func (s Security) HandleSessionToken(ctx context.Context, _ gen.OperationName, t gen.SessionToken) (context.Context, error) {
	p, err := s.Auth.Authenticate(ctx, t.Token)
	if err != nil {
		return ctx, err
	}
	if p.ReadOnly && requestInfo(ctx).Method != http.MethodGet {
		return ctx, errReadOnly
	}
	return context.WithValue(ctx, principalKey{}, p), nil
}

func principal(ctx context.Context) (auth.Principal, error) {
	p, ok := ctx.Value(principalKey{}).(auth.Principal)
	if !ok {
		return auth.Principal{}, auth.ErrUnauthenticated
	}
	return p, nil
}

func admin(ctx context.Context) (auth.Principal, error) {
	p, err := principal(ctx)
	if err != nil {
		return p, err
	}
	if !p.IsAdmin() {
		return p, errForbidden
	}
	return p, nil
}
