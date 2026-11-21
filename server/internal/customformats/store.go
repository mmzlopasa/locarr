package customformats

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
)

// Errors.
var (
	ErrNotFound  = errors.New("not found")
	ErrNameTaken = errors.New("name already in use")
)

// Store keeps custom formats in the database.
type Store struct {
	DB  *db.DB
	Now func() time.Time
}

// NewStore returns a Store.
func NewStore(d *db.DB) *Store { return &Store{DB: d, Now: time.Now} }

// List returns every format by name.
func (s *Store) List(ctx context.Context) ([]Format, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT id, data FROM custom_formats ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Format{}
	for rows.Next() {
		var id int64
		var data string
		if err := rows.Scan(&id, &data); err != nil {
			return nil, err
		}
		var f Format
		if err := json.Unmarshal([]byte(data), &f); err != nil {
			return nil, err
		}
		f.ID = id
		out = append(out, f)
	}
	return out, rows.Err()
}

// Get returns one format.
func (s *Store) Get(ctx context.Context, id int64) (Format, error) {
	var data string
	err := s.DB.R.QueryRowContext(ctx, `SELECT data FROM custom_formats WHERE id = ?`, id).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return Format{}, ErrNotFound
	}
	var f Format
	if err == nil {
		err = json.Unmarshal([]byte(data), &f)
	}
	f.ID = id
	return f, err
}

func isUnique(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// Save creates (ID 0) or replaces a format after checking it.
func (s *Store) Save(ctx context.Context, f Format) (Format, error) {
	f.Name = strings.TrimSpace(f.Name)
	if _, err := Compile(f); err != nil {
		return f, err
	}
	return f, s.DB.Tx(ctx, func(tx *sql.Tx) error {
		var err error
		f, err = save(ctx, tx, f, db.Millis(s.Now()))
		return err
	})
}

func save(ctx context.Context, q db.Querier, f Format, now int64) (Format, error) {
	data, err := json.Marshal(f)
	if err != nil {
		return f, err
	}
	if f.ID == 0 {
		res, err := q.ExecContext(ctx, `INSERT INTO custom_formats (name, data, created_at, updated_at) VALUES (?, ?, ?, ?)`,
			f.Name, string(data), now, now)
		if isUnique(err) {
			return f, ErrNameTaken
		}
		if err != nil {
			return f, err
		}
		f.ID, err = res.LastInsertId()
		return f, err
	}
	res, err := q.ExecContext(ctx, `UPDATE custom_formats SET name = ?, data = ?, updated_at = ? WHERE id = ?`, f.Name, string(data), now, f.ID)
	if isUnique(err) {
		return f, ErrNameTaken
	}
	if err != nil {
		return f, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return f, ErrNotFound
	}
	return f, nil
}

// SaveAll stores imported formats in one transaction. A format whose name
// already exists replaces it when replace is set, otherwise the import fails.
func (s *Store) SaveAll(ctx context.Context, list []Format, replace bool) ([]Format, error) {
	out := make([]Format, 0, len(list))
	err := s.DB.Tx(ctx, func(tx *sql.Tx) error {
		now := db.Millis(s.Now())
		for _, f := range list {
			var id int64
			err := tx.QueryRowContext(ctx, `SELECT id FROM custom_formats WHERE name = ? COLLATE NOCASE`, f.Name).Scan(&id)
			switch {
			case err == nil && !replace:
				return ErrNameTaken
			case err == nil:
				f.ID = id
			case !errors.Is(err, sql.ErrNoRows):
				return err
			default:
				f.ID = 0
			}
			saved, err := save(ctx, tx, f, now)
			if err != nil {
				return err
			}
			out = append(out, saved)
		}
		return nil
	})
	return out, err
}

// Delete removes a format. Profiles that scored it simply stop seeing it.
func (s *Store) Delete(ctx context.Context, id int64) error {
	res, err := s.DB.W.ExecContext(ctx, `DELETE FROM custom_formats WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}
