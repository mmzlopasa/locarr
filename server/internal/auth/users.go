package auth

import (
	"context"
	"database/sql"
	"errors"

	"github.com/mmzlopasa/locarr/server/internal/db"
)

// Users lists every account, by name.
func (s *Service) Users(ctx context.Context) ([]User, error) {
	rows, err := s.db.R.QueryContext(ctx, `SELECT `+userColumns+` FROM users u ORDER BY u.username_key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []User{}
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// User returns one account.
func (s *Service) User(ctx context.Context, id int64) (User, error) { return getUser(ctx, s.db.R, id) }

// NewUser is an account to create.
type NewUser struct {
	Username, Password string
	Role               Role
	Permissions        *Permissions // nil = the role's defaults
	Language           string
}

// CreateUser creates an account.
func (s *Service) CreateUser(ctx context.Context, in NewUser) (User, error) {
	username, err := NormalizeUsername(in.Username)
	if err != nil {
		return User{}, err
	}
	if !in.Role.Valid() {
		return User{}, ErrInvalidRole
	}
	if err := checkPassword(username, in.Password); err != nil {
		return User{}, err
	}
	perms := DefaultPermissions(in.Role)
	if in.Permissions != nil {
		perms = *in.Permissions
	}
	hash, err := HashPassword(in.Password)
	if err != nil {
		return User{}, err
	}
	return s.insertUser(ctx, s.db.W, username, hash, in.Role, perms, normLanguage(in.Language))
}

// UserChange is a partial update; nil fields stay as they are.
type UserChange struct {
	Role        *Role
	Permissions *Permissions
	Disabled    *bool
}

// enabledAdminsExcept counts enabled administrators other than id.
func enabledAdminsExcept(ctx context.Context, tx *sql.Tx, id int64) (int, error) {
	var n int
	err := tx.QueryRowContext(ctx, `SELECT count(*) FROM users WHERE role = 'admin' AND disabled = 0 AND id <> ?`, id).Scan(&n)
	return n, err
}

// UpdateUser changes role, permissions or the disabled flag. actor is the
// administrator making the change (who cannot disable themselves).
func (s *Service) UpdateUser(ctx context.Context, actor Principal, id int64, ch UserChange) (User, error) {
	if ch.Role != nil && !ch.Role.Valid() {
		return User{}, ErrInvalidRole
	}
	if ch.Disabled != nil && *ch.Disabled && id == actor.User.ID {
		return User{}, ErrSelf
	}
	var out User
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		u, err := getUser(ctx, tx, id)
		if err != nil {
			return err
		}
		losesAdmin := u.Role == RoleAdmin && !u.Disabled &&
			((ch.Role != nil && *ch.Role != RoleAdmin) || (ch.Disabled != nil && *ch.Disabled))
		if losesAdmin {
			n, err := enabledAdminsExcept(ctx, tx, id)
			if err != nil {
				return err
			}
			if n == 0 {
				return ErrLastAdmin
			}
		}
		now := db.Millis(s.now())
		if ch.Role != nil {
			if _, err := tx.ExecContext(ctx, `UPDATE users SET role = ?, updated_at = ? WHERE id = ?`, string(*ch.Role), now, id); err != nil {
				return err
			}
		}
		if ch.Permissions != nil {
			if _, err := tx.ExecContext(ctx, `UPDATE users SET permissions = ?, updated_at = ? WHERE id = ?`,
				int64(ch.Permissions.Clean()), now, id); err != nil { // #nosec G115 -- bitset fits
				return err
			}
		}
		// Requesters can never hold view_adult (D-100), whatever was sent.
		if _, err := tx.ExecContext(ctx, `UPDATE users SET permissions = permissions & ? WHERE id = ? AND role = 'requester'`,
			^int64(PermViewAdult), id); err != nil { // #nosec G115 -- bitset fits
			return err
		}
		if ch.Disabled != nil {
			if _, err := tx.ExecContext(ctx, `UPDATE users SET disabled = ?, updated_at = ? WHERE id = ?`, *ch.Disabled, now, id); err != nil {
				return err
			}
			if *ch.Disabled { // a disabled account is signed out everywhere
				if _, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, id); err != nil {
					return err
				}
			}
		}
		out, err = getUser(ctx, tx, id)
		return err
	})
	return out, err
}

// ResetPassword sets a user's password, clears a lockout and signs the user
// out everywhere.
func (s *Service) ResetPassword(ctx context.Context, id int64, password string) error {
	u, err := getUser(ctx, s.db.R, id)
	if err != nil {
		return err
	}
	if err := checkPassword(u.Username, password); err != nil {
		return err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return err
	}
	return s.db.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE users SET password_hash = ?, failed_logins = 0, locked_until = NULL,
			updated_at = ? WHERE id = ?`, hash, db.Millis(s.now()), id); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, id)
		return err
	})
}

// DeleteUser deletes an account with its sessions and keys.
func (s *Service) DeleteUser(ctx context.Context, actor Principal, id int64) error {
	if id == actor.User.ID {
		return ErrSelf
	}
	return s.db.Tx(ctx, func(tx *sql.Tx) error {
		u, err := getUser(ctx, tx, id)
		if err != nil {
			return err
		}
		if u.Role == RoleAdmin && !u.Disabled {
			n, err := enabledAdminsExcept(ctx, tx, id)
			if err != nil {
				return err
			}
			if n == 0 {
				return ErrLastAdmin
			}
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
		return err
	})
}

// ResetAdmin gives an administrator a new random password and makes sure it
// can sign in: it enables the account, clears the lockout and two-factor, and
// ends its sessions. With an empty username the oldest administrator is used.
// It is the way back in for an owner locked out of their own server (run from
// the container's shell), so it never goes through the API.
func (s *Service) ResetAdmin(ctx context.Context, username string) (string, string, error) {
	password, err := NewPassword()
	if err != nil {
		return "", "", err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return "", "", err
	}
	var name string
	err = s.db.Tx(ctx, func(tx *sql.Tx) error {
		var id int64
		var err error
		if username == "" {
			err = tx.QueryRowContext(ctx, `SELECT id, username FROM users WHERE role = 'admin' ORDER BY id LIMIT 1`).Scan(&id, &name)
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNoAdmin
			}
		} else {
			var role string
			err = tx.QueryRowContext(ctx, `SELECT id, username, role FROM users WHERE username_key = ?`, usernameKey(username)).
				Scan(&id, &name, &role)
			if errors.Is(err, sql.ErrNoRows) {
				return ErrNotFound
			}
			if err == nil && Role(role) != RoleAdmin {
				return errors.New("user is not an administrator")
			}
		}
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE users SET password_hash = ?, disabled = 0, failed_logins = 0,
			locked_until = NULL, updated_at = ? WHERE id = ?`, hash, db.Millis(s.now()), id); err != nil {
			return err
		}
		if err := clearTOTP(ctx, tx, id); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM sessions WHERE user_id = ?`, id)
		return err
	})
	return name, password, err
}
