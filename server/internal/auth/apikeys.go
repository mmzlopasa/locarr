package auth

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
)

// APIKey describes a key (never the key itself, which is only stored hashed).
type APIKey struct {
	ID       int64
	Name     string
	Prefix   string
	ReadOnly bool
	// IncludeAdult: the key may see the adult library (when its owner may).
	IncludeAdult bool
	CreatedAt    time.Time
	LastUsedAt   *time.Time
}

// CreateAPIKey creates a key for the caller and returns it once. Only a session
// may create keys, so a leaked key cannot mint more.
func (s *Service) CreateAPIKey(ctx context.Context, p Principal, name string, readOnly bool) (APIKey, string, error) {
	return s.CreateAPIKeyWith(ctx, p, name, KeyOptions{ReadOnly: readOnly})
}

// KeyOptions are the choices made when a key is created.
type KeyOptions struct {
	ReadOnly bool
	// IncludeAdult lets the key see the adult library; it is dropped unless
	// the owner holds view_adult (D-100).
	IncludeAdult bool
}

// CreateAPIKeyWith creates a key with options.
func (s *Service) CreateAPIKeyWith(ctx context.Context, p Principal, name string, o KeyOptions) (APIKey, string, error) {
	readOnly := o.ReadOnly
	includeAdult := o.IncludeAdult && p.CanViewAdult()
	if p.SessionID == 0 {
		return APIKey{}, "", ErrSessionRequired
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return APIKey{}, "", ErrInvalidName
	}
	key, hash, err := newToken(APIKeyPrefix)
	if err != nil {
		return APIKey{}, "", err
	}
	now := s.now()
	k := APIKey{Name: truncate(name, 100), Prefix: key[:apiKeyDisplayLen], ReadOnly: readOnly, IncludeAdult: includeAdult, CreatedAt: now.UTC()}
	if err := s.db.W.QueryRowContext(ctx, `INSERT INTO api_keys (user_id, name, prefix, key_hash, read_only, include_adult, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?) RETURNING id`, p.User.ID, k.Name, k.Prefix, hash, readOnly, includeAdult, db.Millis(now)).Scan(&k.ID); err != nil {
		return APIKey{}, "", err
	}
	return k, key, nil
}

// APIKeys lists a user's keys, newest first.
func (s *Service) APIKeys(ctx context.Context, userID int64) ([]APIKey, error) {
	rows, err := s.db.R.QueryContext(ctx, `SELECT id, name, prefix, read_only, include_adult, created_at, last_used_at FROM api_keys
		WHERE user_id = ? ORDER BY id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []APIKey{}
	for rows.Next() {
		var (
			k       APIKey
			created int64
			used    sql.NullInt64
		)
		if err := rows.Scan(&k.ID, &k.Name, &k.Prefix, &k.ReadOnly, &k.IncludeAdult, &created, &used); err != nil {
			return nil, err
		}
		k.CreatedAt, k.LastUsedAt = db.Time(created), db.NullTime(used)
		out = append(out, k)
	}
	return out, rows.Err()
}

// DeleteAPIKey revokes one of the user's keys.
func (s *Service) DeleteAPIKey(ctx context.Context, userID, id int64) error {
	res, err := s.db.W.ExecContext(ctx, `DELETE FROM api_keys WHERE id = ? AND user_id = ?`, id, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
