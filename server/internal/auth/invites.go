package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
)

// ErrInviteInvalid is returned for an unknown, revoked, expired or used-up invite.
var ErrInviteInvalid = errors.New("this invite link is not valid (unknown, expired, used up or revoked)")

// DefaultInviteDays is how long an invite works when no expiry is chosen.
const DefaultInviteDays = 7

// Invite is a sign-up link for family and friends (D-059). The code itself
// is only returned when the invite is created.
type Invite struct {
	ID          int64
	Label       string
	Role        Role
	Permissions Permissions
	// Quotas are request quota overrides given to users who sign up with it
	// (stored as JSON, applied by the requests service).
	Quotas    json.RawMessage
	MaxUses   int // 0 = unlimited
	Uses      int
	ExpiresAt *time.Time
	CreatedBy string
	CreatedAt time.Time
	Revoked   bool
}

// Usable reports whether the invite still works at now.
func (i Invite) Usable(now time.Time) bool {
	return !i.Revoked && (i.ExpiresAt == nil || now.Before(*i.ExpiresAt)) && (i.MaxUses == 0 || i.Uses < i.MaxUses)
}

// NewInvite describes an invite to create.
type NewInvite struct {
	Label       string
	Role        Role
	Permissions *Permissions // nil = the role's defaults
	Quotas      json.RawMessage
	MaxUses     int
	ExpiresIn   time.Duration // 0 = DefaultInviteDays
}

const inviteColumns = `i.id, i.label, i.role, i.permissions, i.quotas, i.max_uses, i.uses, i.expires_at, COALESCE(u.username, ''),
	i.created_at, i.revoked_at IS NOT NULL`

func scanInvite(row interface{ Scan(...any) error }) (Invite, error) {
	var (
		in      Invite
		role    string
		perms   int64
		quotas  string
		expires sql.NullInt64
		created int64
	)
	if err := row.Scan(&in.ID, &in.Label, &role, &perms, &quotas, &in.MaxUses, &in.Uses, &expires, &in.CreatedBy, &created, &in.Revoked); err != nil {
		return in, err
	}
	in.Role, in.Permissions, in.Quotas = Role(role), Permissions(perms).WithoutAdult(), json.RawMessage(quotas) // #nosec G115
	in.CreatedAt = db.Time(created)
	if expires.Valid {
		t := db.Time(expires.Int64)
		in.ExpiresAt = &t
	}
	return in, nil
}

// CreateInvite stores a new invite and returns it with its code.
func (s *Service) CreateInvite(ctx context.Context, createdBy int64, in NewInvite) (Invite, string, error) {
	if in.Role != RoleManager && in.Role != RoleRequester {
		return Invite{}, "", ErrInvalidRole
	}
	if in.MaxUses < 0 {
		in.MaxUses = 0
	}
	perms := DefaultPermissions(in.Role)
	if in.Permissions != nil {
		perms = in.Permissions.WithoutAdult()
	}
	if len(in.Quotas) == 0 {
		in.Quotas = json.RawMessage("{}")
	}
	if in.ExpiresIn <= 0 {
		in.ExpiresIn = DefaultInviteDays * 24 * time.Hour
	}
	code, hash, err := newToken("")
	if err != nil {
		return Invite{}, "", err
	}
	now := s.now()
	var id int64
	err = s.db.W.QueryRowContext(ctx, `INSERT INTO invites (label, code_hash, role, permissions, quotas, max_uses, expires_at, created_by, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`, truncate(strings.TrimSpace(in.Label), 100), hash, string(in.Role),
		int64(perms), string(in.Quotas), in.MaxUses, db.Millis(now.Add(in.ExpiresIn)), createdBy, db.Millis(now)).Scan(&id) // #nosec G115
	if err != nil {
		return Invite{}, "", err
	}
	inv, err := s.invite(ctx, s.db.W, id)
	return inv, code, err
}

func (s *Service) invite(ctx context.Context, q db.Querier, id int64) (Invite, error) {
	inv, err := scanInvite(q.QueryRowContext(ctx, `SELECT `+inviteColumns+` FROM invites i LEFT JOIN users u ON u.id = i.created_by WHERE i.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return inv, ErrNotFound
	}
	return inv, err
}

// Invites lists every invite, newest first.
func (s *Service) Invites(ctx context.Context) ([]Invite, error) {
	rows, err := s.db.R.QueryContext(ctx, `SELECT `+inviteColumns+` FROM invites i LEFT JOIN users u ON u.id = i.created_by ORDER BY i.id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Invite{}
	for rows.Next() {
		inv, err := scanInvite(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, inv)
	}
	return out, rows.Err()
}

// RevokeInvite stops an invite from working (users who signed up stay).
func (s *Service) RevokeInvite(ctx context.Context, id int64) error {
	res, err := s.db.W.ExecContext(ctx, `UPDATE invites SET revoked_at = COALESCE(revoked_at, ?) WHERE id = ?`, db.Millis(s.now()), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// CheckInvite returns the invite behind code if it can still be used.
func (s *Service) CheckInvite(ctx context.Context, code string) (Invite, error) {
	return s.usableInvite(ctx, s.db.R, code)
}

func (s *Service) usableInvite(ctx context.Context, q db.Querier, code string) (Invite, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return Invite{}, ErrInviteInvalid
	}
	var id int64
	err := q.QueryRowContext(ctx, `SELECT id FROM invites WHERE code_hash = ?`, hashToken(code)).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return Invite{}, ErrInviteInvalid
	}
	if err != nil {
		return Invite{}, err
	}
	inv, err := s.invite(ctx, q, id)
	if err != nil {
		return inv, err
	}
	if !inv.Usable(s.now()) {
		return Invite{}, ErrInviteInvalid
	}
	return inv, nil
}

// SignUp creates an account from an invite and signs it in. Using the invite
// and creating the user happen in one transaction, so an invite with one use
// left cannot make two accounts.
func (s *Service) SignUp(ctx context.Context, code, username, password, language string, dev Device) (Principal, string, Invite, error) {
	username, err := NormalizeUsername(username)
	if err != nil {
		return Principal{}, "", Invite{}, err
	}
	if err := checkPassword(username, password); err != nil {
		return Principal{}, "", Invite{}, err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return Principal{}, "", Invite{}, err
	}
	var (
		p     Principal
		token string
		inv   Invite
	)
	err = s.db.Tx(ctx, func(tx *sql.Tx) error {
		if inv, err = s.usableInvite(ctx, tx, code); err != nil {
			return err
		}
		u, err := s.insertUser(ctx, tx, username, hash, inv.Role, inv.Permissions, normLanguage(language))
		if err != nil {
			return err
		}
		now := db.Millis(s.now())
		if _, err := tx.ExecContext(ctx, `UPDATE invites SET uses = uses + 1 WHERE id = ?`, inv.ID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO invite_signups (invite_id, user_id, created_at) VALUES (?, ?, ?)`, inv.ID, u.ID, now); err != nil {
			return err
		}
		p, token, err = s.createSession(ctx, tx, u, dev)
		return err
	})
	return p, token, inv, err
}
