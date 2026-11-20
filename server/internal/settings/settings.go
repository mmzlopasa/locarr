// Package settings is the typed settings store. Plain values are JSON in the
// settings table; secret values (third-party credentials) are encrypted in the
// secrets table and can be written and used by the server but never read back
// through the API, which only learns whether they are configured.
package settings

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/secrets"
)

// Key is a typed plain setting with its default.
type Key[T any] struct {
	Name    string
	Default T
}

// SecretKey names a write-only secret setting.
type SecretKey struct{ Name string }

// Settings known to the server.
var (
	InstanceName = Key[string]{Name: "general.instanceName", Default: "Locarr"}
	TMDbAPIKey   = SecretKey{Name: "metadata.tmdbApiKey"}
)

// Store reads and writes settings.
type Store struct {
	DB  *db.DB
	Box *secrets.Box
	Now func() time.Time
}

// New returns a Store.
func New(d *db.DB, box *secrets.Box) *Store { return &Store{DB: d, Box: box, Now: time.Now} }

// Get returns the value of k, or its default when unset.
func Get[T any](ctx context.Context, s *Store, k Key[T]) (T, error) {
	var raw string
	err := s.DB.R.QueryRowContext(ctx, `SELECT value FROM settings WHERE key = ?`, k.Name).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return k.Default, nil
	}
	if err != nil {
		return k.Default, err
	}
	var v T
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return k.Default, err
	}
	return v, nil
}

// Set stores v for k (on q, so it can join a transaction; nil = the writer).
func Set[T any](ctx context.Context, s *Store, q db.Querier, k Key[T], v T) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if q == nil {
		q = s.DB.W
	}
	_, err = q.ExecContext(ctx, `INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT (key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		k.Name, string(raw), db.Millis(s.Now()))
	return err
}

// SetSecret encrypts and stores value; an empty value removes the secret.
func (s *Store) SetSecret(ctx context.Context, k SecretKey, value string) error {
	if value == "" {
		_, err := s.DB.W.ExecContext(ctx, `DELETE FROM secrets WHERE key = ?`, k.Name)
		return err
	}
	ct, err := s.Box.Seal([]byte(value), "setting:"+k.Name)
	if err != nil {
		return err
	}
	_, err = s.DB.W.ExecContext(ctx, `INSERT INTO secrets (key, ciphertext, updated_at) VALUES (?, ?, ?)
		ON CONFLICT (key) DO UPDATE SET ciphertext = excluded.ciphertext, updated_at = excluded.updated_at`,
		k.Name, ct, db.Millis(s.Now()))
	return err
}

// Secret returns the decrypted value and whether it is set. For server-side use
// only (talking to the third-party service); never send it to a client.
func (s *Store) Secret(ctx context.Context, k SecretKey) (string, bool, error) {
	var ct []byte
	err := s.DB.R.QueryRowContext(ctx, `SELECT ciphertext FROM secrets WHERE key = ?`, k.Name).Scan(&ct)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	pt, err := s.Box.Open(ct, "setting:"+k.Name)
	if err != nil {
		return "", false, err
	}
	return string(pt), true, nil
}

// Configured reports whether a secret is set, without decrypting it.
func (s *Store) Configured(ctx context.Context, k SecretKey) (bool, error) {
	var n int
	err := s.DB.R.QueryRowContext(ctx, `SELECT count(*) FROM secrets WHERE key = ?`, k.Name).Scan(&n)
	return n > 0, err
}
