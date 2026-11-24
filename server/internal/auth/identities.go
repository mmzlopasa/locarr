package auth

import (
	"context"
	"database/sql"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
)

// Errors of provider sign-in and linking.
var (
	ErrUnknownProvider     = errors.New("unknown or disabled sign-in provider")
	ErrIdentityNotLinked   = errors.New("no Locarr account is linked to this account; ask an administrator to link it")
	ErrIdentityTaken       = errors.New("this account is already linked to another Locarr user")
	ErrProviderUnavailable = errors.New("the sign-in service cannot be reached")
)

// LinkedIdentity is a stored link between a user and a provider account.
type LinkedIdentity struct {
	Provider    string
	Subject     string
	Username    string
	UserID      int64
	LinkedAt    time.Time
	LastLoginAt *time.Time
}

func (s *Service) provider(ctx context.Context, id string) (Provider, error) {
	for _, p := range s.providers {
		if p.Info().ID != id {
			continue
		}
		if a, ok := p.(Available); ok && !a.Available(ctx) {
			return nil, ErrUnknownProvider
		}
		return p, nil
	}
	return nil, ErrUnknownProvider
}

// LoginWithProvider checks credentials at a provider and signs the linked
// user in (D-058, D-068). An unknown account creates a local user only when
// the provider allows it, never as an administrator.
func (s *Service) LoginWithProvider(ctx context.Context, providerID string, c Credentials, dev Device) (Principal, string, bool, error) {
	p, err := s.provider(ctx, providerID)
	if err != nil {
		return Principal{}, "", false, err
	}
	id, err := p.Authenticate(ctx, c)
	if err != nil {
		return Principal{}, "", false, err
	}
	id.Provider = providerID
	var (
		pr      Principal
		token   string
		created bool
	)
	err = s.db.Tx(ctx, func(tx *sql.Tx) error {
		var uid int64
		err := tx.QueryRowContext(ctx, `SELECT user_id FROM user_identities WHERE provider = ? AND subject = ?`, id.Provider, id.Subject).Scan(&uid)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			pol, ok := p.(SignUpPolicy)
			if !ok {
				return ErrIdentityNotLinked
			}
			allowed, role, perms := pol.AutoCreate(ctx)
			if !allowed {
				return ErrIdentityNotLinked
			}
			if role != RoleManager {
				role = RoleRequester // a provider never creates administrators
			}
			u, err := s.createFromIdentity(ctx, tx, id, role, perms)
			if err != nil {
				return err
			}
			uid, created = u.ID, true
		case err != nil:
			return err
		}
		u, err := getUser(ctx, tx, uid)
		if err != nil {
			return err
		}
		if u.Disabled {
			return ErrInvalidCredentials
		}
		if _, err := tx.ExecContext(ctx, `UPDATE user_identities SET username = ?, last_login_at = ? WHERE provider = ? AND subject = ?`,
			truncate(id.Username, 64), db.Millis(s.now()), id.Provider, id.Subject); err != nil {
			return err
		}
		pr, token, err = s.createSession(ctx, tx, u, dev)
		return err
	})
	return pr, token, created, err
}

// createFromIdentity makes a local user for a provider account: the
// provider's name (or name-2, name-3 … when taken) and an unusable random
// password, so it signs in only through the provider until an admin sets one.
func (s *Service) createFromIdentity(ctx context.Context, tx *sql.Tx, id Identity, role Role, perms Permissions) (User, error) {
	pw, err := NewPassword()
	if err != nil {
		return User{}, err
	}
	hash, err := HashPassword(pw + pw)
	if err != nil {
		return User{}, err
	}
	base, err := NormalizeUsername(truncate(id.Username, 58))
	if err != nil {
		base = "user"
	}
	if perms == 0 {
		perms = DefaultPermissions(role)
	}
	perms = perms.WithoutAdult() // never through another service (D-101)
	var u User
	for i := 1; i <= 50; i++ {
		name := base
		if i > 1 {
			name = base + "-" + strconv.Itoa(i)
		}
		u, err = s.insertUser(ctx, tx, name, hash, role, perms, "en")
		if errors.Is(err, ErrUsernameTaken) {
			continue
		}
		if err != nil {
			return User{}, err
		}
		break
	}
	if u.ID == 0 {
		return User{}, ErrUsernameTaken
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO user_identities (provider, subject, user_id, username, linked_at) VALUES (?, ?, ?, ?, ?)`,
		id.Provider, id.Subject, u.ID, truncate(id.Username, 64), db.Millis(s.now()))
	return u, err
}

// VerifyWithProvider checks credentials at a provider without signing in
// (used to link the caller's own account).
func (s *Service) VerifyWithProvider(ctx context.Context, providerID string, c Credentials) (Identity, error) {
	p, err := s.provider(ctx, providerID)
	if err != nil {
		return Identity{}, err
	}
	id, err := p.Authenticate(ctx, c)
	id.Provider = providerID
	return id, err
}

// Link links a user to a provider account (replacing the user's earlier
// link to that provider).
func (s *Service) Link(ctx context.Context, userID int64, provider, subject, username string) (LinkedIdentity, error) {
	provider, subject = strings.TrimSpace(provider), strings.TrimSpace(subject)
	if provider == "" || subject == "" || provider == LocalProvider {
		return LinkedIdentity{}, ErrUnknownProvider
	}
	if _, err := getUser(ctx, s.db.R, userID); err != nil {
		return LinkedIdentity{}, err
	}
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		var other int64
		err := tx.QueryRowContext(ctx, `SELECT user_id FROM user_identities WHERE provider = ? AND subject = ?`, provider, subject).Scan(&other)
		if err == nil && other != userID {
			return ErrIdentityTaken
		}
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM user_identities WHERE provider = ? AND user_id = ?`, provider, userID); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO user_identities (provider, subject, user_id, username, linked_at) VALUES (?, ?, ?, ?, ?)`,
			provider, subject, userID, truncate(strings.TrimSpace(username), 64), db.Millis(s.now()))
		return err
	})
	if err != nil {
		return LinkedIdentity{}, err
	}
	list, err := s.Identities(ctx, userID)
	for _, l := range list {
		if l.Provider == provider {
			return l, err
		}
	}
	return LinkedIdentity{}, err
}

// Unlink removes a user's link to a provider.
func (s *Service) Unlink(ctx context.Context, userID int64, provider string) error {
	res, err := s.db.W.ExecContext(ctx, `DELETE FROM user_identities WHERE provider = ? AND user_id = ?`, provider, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Identities lists a user's links.
func (s *Service) Identities(ctx context.Context, userID int64) ([]LinkedIdentity, error) {
	rows, err := s.db.R.QueryContext(ctx, `SELECT provider, subject, username, user_id, linked_at, last_login_at FROM user_identities
		WHERE user_id = ? ORDER BY provider`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LinkedIdentity{}
	for rows.Next() {
		var (
			l      LinkedIdentity
			linked int64
			last   sql.NullInt64
		)
		if err := rows.Scan(&l.Provider, &l.Subject, &l.Username, &l.UserID, &linked, &last); err != nil {
			return nil, err
		}
		l.LinkedAt, l.LastLoginAt = db.Time(linked), db.NullTime(last)
		out = append(out, l)
	}
	return out, rows.Err()
}
