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
	ID         int64
	Name       string
	Prefix     string
	ReadOnly   bool
	CreatedAt  time.Time
	LastUsedAt *time.Time
}

// CreateAPIKey creates a key for the caller and returns it once. Only a session
// may create keys, so a leaked key cannot mint more.
func (s *Service) CreateAPIKey(ctx context.Context, p Principal, name string, readOnly bool) (APIKey, string, error) {
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
	k := APIKey{Name: truncate(name, 100), Prefix: key[:apiKeyDisplayLen], ReadOnly: readOnly, CreatedAt: now.UTC()}
	if err := s.db.W.QueryRowContext(ctx, `INSERT INTO api_keys (user_id, name, prefix, key_hash, read_only, created_at)
		VALUES (?, ?, ?, ?, ?, ?) RETURNING id`, p.User.ID, k.Name, k.Prefix, hash, readOnly, db.Millis(now)).Scan(&k.ID); err != nil {
		return APIKey{}, "", err
	}
	return k, key, nil
}

// APIKeys lists a user's keys, newest first.
func (s *Service) APIKeys(ctx context.Context, userID int64) ([]APIKey, error) {
	rows, err := s.db.R.QueryContext(ctx, `SELECT id, name, prefix, read_only, created_at, last_used_at FROM api_keys
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
		if err := rows.Scan(&k.ID, &k.Name, &k.Prefix, &k.ReadOnly, &created, &used); err != nil {
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
