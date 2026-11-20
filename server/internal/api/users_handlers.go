package api

import (
	"context"

	"github.com/mmzlopasa/locarr/server/internal/api/gen"
	"github.com/mmzlopasa/locarr/server/internal/auth"
)

// ListUsers implements GET /users.
func (h *Handler) ListUsers(ctx context.Context) ([]gen.User, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	users, err := h.Auth.Users(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]gen.User, 0, len(users))
	for _, u := range users {
		out = append(out, toUser(u))
	}
	return out, nil
}

// GetUser implements GET /users/{id}.
func (h *Handler) GetUser(ctx context.Context, params gen.GetUserParams) (*gen.User, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	u, err := h.Auth.User(ctx, params.ID)
	if err != nil {
		return nil, err
	}
	out := toUser(u)
	return &out, nil
}

// CreateUser implements POST /users.
func (h *Handler) CreateUser(ctx context.Context, req *gen.CreateUserRequest) (*gen.User, error) {
	actor, err := admin(ctx)
	if err != nil {
		return nil, err
	}
	in := auth.NewUser{Username: string(req.Username), Password: string(req.Password), Role: auth.Role(req.Role),
		Language: string(req.Language.Or(gen.LanguageEn))}
	if perms, ok := req.Permissions.Get(); ok {
		p := fromPermissions(perms)
		in.Permissions = &p
	}
	u, err := h.Auth.CreateUser(ctx, in)
	if err != nil {
		return nil, err
	}
	h.Log.Info("user created", "user", u.Username, "role", string(u.Role), "by", actor.User.Username)
	out := toUser(u)
	return &out, nil
}

// UpdateUser implements PATCH /users/{id}.
func (h *Handler) UpdateUser(ctx context.Context, req *gen.UpdateUserRequest, params gen.UpdateUserParams) (*gen.User, error) {
	actor, err := admin(ctx)
	if err != nil {
		return nil, err
	}
	var ch auth.UserChange
	if r, ok := req.Role.Get(); ok {
		role := auth.Role(r)
		ch.Role = &role
	}
	if perms, ok := req.Permissions.Get(); ok {
		p := fromPermissions(perms)
		ch.Permissions = &p
	}
	if d, ok := req.Disabled.Get(); ok {
		ch.Disabled = &d
	}
	u, err := h.Auth.UpdateUser(ctx, actor, params.ID, ch)
	if err != nil {
		return nil, err
	}
	out := toUser(u)
	return &out, nil
}

// ResetUserPassword implements POST /users/{id}/password.
func (h *Handler) ResetUserPassword(ctx context.Context, req *gen.ResetPasswordRequest, params gen.ResetUserPasswordParams) error {
	actor, err := admin(ctx)
	if err != nil {
		return err
	}
	if err := h.Auth.ResetPassword(ctx, params.ID, string(req.Password)); err != nil {
		return err
	}
	h.Log.Info("password reset by an administrator", "user_id", params.ID, "by", actor.User.Username)
	return nil
}

// DeleteUser implements DELETE /users/{id}.
func (h *Handler) DeleteUser(ctx context.Context, params gen.DeleteUserParams) error {
	actor, err := admin(ctx)
	if err != nil {
		return err
	}
	if err := h.Auth.DeleteUser(ctx, actor, params.ID); err != nil {
		return err
	}
	h.Log.Info("user deleted", "user_id", params.ID, "by", actor.User.Username)
	return nil
}
