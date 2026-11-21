package quality

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
)

// Store keeps definitions and profiles in the database.
type Store struct {
	DB  *db.DB
	Now func() time.Time
}

// NewStore returns a Store.
func NewStore(d *db.DB) *Store { return &Store{DB: d, Now: time.Now} }

const seedKey = "seed.qualityProfiles"

func (s *Store) now() int64 { return db.Millis(s.Now()) }

func isUnique(err error) bool {
	return err != nil && strings.Contains(err.Error(), "UNIQUE constraint failed")
}

// Seed adds missing size definitions, the default delay profile, and (once,
// on first start) the default quality profiles.
func (s *Store) Seed(ctx context.Context) error {
	return s.DB.Tx(ctx, func(tx *sql.Tx) error {
		now := s.now()
		for _, d := range DefaultDefinitions() {
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO quality_definitions (kind, quality, min_mb, preferred_mb, max_mb, updated_at)
				VALUES (?, ?, ?, ?, ?, ?)`, d.Kind, d.Quality, d.Min, d.Preferred, d.Max, now); err != nil {
				return err
			}
		}
		var n int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM delay_profiles WHERE is_default = 1`).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			if _, err := insertDelay(ctx, tx, DefaultDelayProfile(), now); err != nil {
				return err
			}
		}
		err := tx.QueryRowContext(ctx, `SELECT 1 FROM settings WHERE key = ?`, seedKey).Scan(&n)
		if err == nil {
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		for _, p := range DefaultProfiles() {
			if _, err := insertProfile(ctx, tx, &p, now); err != nil && !isUnique(err) {
				return err
			}
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO settings (key, value, updated_at) VALUES (?, 'true', ?)`, seedKey, now)
		return err
	})
}

// Definitions returns every size definition (ladder order within each kind).
func (s *Store) Definitions(ctx context.Context) ([]Definition, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT kind, quality, min_mb, preferred_mb, max_mb FROM quality_definitions`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Definition
	for rows.Next() {
		var d Definition
		if err := rows.Scan(&d.Kind, &d.Quality, &d.Min, &d.Preferred, &d.Max); err != nil {
			return nil, err
		}
		if _, ok := ByID(d.Quality); ok {
			out = append(out, d)
		}
	}
	kindOrder := map[Kind]int{Movies: 0, Series: 1, Music: 2}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return kindOrder[out[i].Kind] < kindOrder[out[j].Kind]
		}
		return ByIDMust(out[i].Quality).Rank < ByIDMust(out[j].Quality).Rank
	})
	return out, rows.Err()
}

// UpdateDefinitions stores changed definitions (all or nothing).
func (s *Store) UpdateDefinitions(ctx context.Context, defs []Definition) error {
	for _, d := range defs {
		if err := d.Validate(); err != nil {
			return err
		}
	}
	return s.DB.Tx(ctx, func(tx *sql.Tx) error {
		now := s.now()
		for _, d := range defs {
			if _, err := tx.ExecContext(ctx, `INSERT INTO quality_definitions (kind, quality, min_mb, preferred_mb, max_mb, updated_at)
				VALUES (?, ?, ?, ?, ?, ?) ON CONFLICT (kind, quality) DO UPDATE SET min_mb = excluded.min_mb,
				preferred_mb = excluded.preferred_mb, max_mb = excluded.max_mb, updated_at = excluded.updated_at`,
				d.Kind, d.Quality, d.Min, d.Preferred, d.Max, now); err != nil {
				return err
			}
		}
		return nil
	})
}

// ResetDefinitions restores the shipped sizes for a kind ("" = all kinds).
func (s *Store) ResetDefinitions(ctx context.Context, k Kind) error {
	var defs []Definition
	for _, d := range DefaultDefinitions() {
		if k == "" || d.Kind == k {
			defs = append(defs, d)
		}
	}
	return s.UpdateDefinitions(ctx, defs)
}

// DefinitionMap indexes definitions by kind and quality.
func DefinitionMap(defs []Definition) map[Kind]map[string]Definition {
	m := map[Kind]map[string]Definition{}
	for _, d := range defs {
		if m[d.Kind] == nil {
			m[d.Kind] = map[string]Definition{}
		}
		m[d.Kind][d.Quality] = d
	}
	return m
}

// --- quality profiles ---

func insertProfile(ctx context.Context, q db.Querier, p *Profile, now int64) (int64, error) {
	data, err := json.Marshal(p)
	if err != nil {
		return 0, err
	}
	res, err := q.ExecContext(ctx, `INSERT INTO quality_profiles (name, kind, data, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		p.Name, p.Kind, string(data), now, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func scanProfile(id int64, data string) (Profile, error) {
	var p Profile
	if err := json.Unmarshal([]byte(data), &p); err != nil {
		return p, err
	}
	p.ID = id
	p.Normalize()
	return p, nil
}

// Profiles lists quality profiles (by kind, then name).
func (s *Store) Profiles(ctx context.Context) ([]Profile, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT id, data FROM quality_profiles ORDER BY kind, name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Profile{}
	for rows.Next() {
		var id int64
		var data string
		if err := rows.Scan(&id, &data); err != nil {
			return nil, err
		}
		p, err := scanProfile(id, data)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Profile returns one quality profile.
func (s *Store) Profile(ctx context.Context, id int64) (Profile, error) {
	var data string
	err := s.DB.R.QueryRowContext(ctx, `SELECT data FROM quality_profiles WHERE id = ?`, id).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return Profile{}, ErrNotFound
	}
	if err != nil {
		return Profile{}, err
	}
	return scanProfile(id, data)
}

// CreateProfile validates and adds a profile.
func (s *Store) CreateProfile(ctx context.Context, p Profile) (Profile, error) {
	p.Normalize()
	if err := p.Validate(); err != nil {
		return p, err
	}
	id, err := insertProfile(ctx, s.DB.W, &p, s.now())
	if isUnique(err) {
		return p, ErrNameTaken
	}
	if err != nil {
		return p, err
	}
	p.ID = id
	return p, nil
}

// UpdateProfile replaces a profile. The kind cannot change.
func (s *Store) UpdateProfile(ctx context.Context, p Profile) (Profile, error) {
	cur, err := s.Profile(ctx, p.ID)
	if err != nil {
		return p, err
	}
	p.Kind = cur.Kind
	p.Normalize()
	if err := p.Validate(); err != nil {
		return p, err
	}
	data, err := json.Marshal(p)
	if err != nil {
		return p, err
	}
	_, err = s.DB.W.ExecContext(ctx, `UPDATE quality_profiles SET name = ?, data = ?, updated_at = ? WHERE id = ?`,
		p.Name, string(data), s.now(), p.ID)
	if isUnique(err) {
		return p, ErrNameTaken
	}
	return p, err
}

// DeleteProfile removes a profile.
func (s *Store) DeleteProfile(ctx context.Context, id int64) error {
	return deleteRow(ctx, s.DB.W, `DELETE FROM quality_profiles WHERE id = ?`, id)
}

func deleteRow(ctx context.Context, q db.Querier, query string, id int64) error {
	res, err := q.ExecContext(ctx, query, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// --- release profiles ---

// ReleaseProfiles lists release profiles by name.
func (s *Store) ReleaseProfiles(ctx context.Context) ([]ReleaseProfile, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT id, data FROM release_profiles ORDER BY name COLLATE NOCASE, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ReleaseProfile{}
	for rows.Next() {
		var id int64
		var data string
		if err := rows.Scan(&id, &data); err != nil {
			return nil, err
		}
		var r ReleaseProfile
		if err := json.Unmarshal([]byte(data), &r); err != nil {
			return nil, err
		}
		r.ID = id
		out = append(out, r)
	}
	return out, rows.Err()
}

// ReleaseProfile returns one release profile.
func (s *Store) ReleaseProfile(ctx context.Context, id int64) (ReleaseProfile, error) {
	var data string
	err := s.DB.R.QueryRowContext(ctx, `SELECT data FROM release_profiles WHERE id = ?`, id).Scan(&data)
	if errors.Is(err, sql.ErrNoRows) {
		return ReleaseProfile{}, ErrNotFound
	}
	var r ReleaseProfile
	if err == nil {
		err = json.Unmarshal([]byte(data), &r)
	}
	r.ID = id
	return r, err
}

// SaveReleaseProfile creates (ID 0) or replaces a release profile.
func (s *Store) SaveReleaseProfile(ctx context.Context, r ReleaseProfile) (ReleaseProfile, error) {
	if err := r.Validate(); err != nil {
		return r, err
	}
	if r.ID != 0 {
		if _, err := s.ReleaseProfile(ctx, r.ID); err != nil {
			return r, err
		}
	}
	data, err := json.Marshal(r)
	if err != nil {
		return r, err
	}
	now := s.now()
	if r.ID == 0 {
		res, err := s.DB.W.ExecContext(ctx, `INSERT INTO release_profiles (name, data, created_at, updated_at) VALUES (?, ?, ?, ?)`,
			r.Name, string(data), now, now)
		if err != nil {
			return r, err
		}
		r.ID, err = res.LastInsertId()
		return r, err
	}
	_, err = s.DB.W.ExecContext(ctx, `UPDATE release_profiles SET name = ?, data = ?, updated_at = ? WHERE id = ?`, r.Name, string(data), now, r.ID)
	return r, err
}

// DeleteReleaseProfile removes a release profile.
func (s *Store) DeleteReleaseProfile(ctx context.Context, id int64) error {
	return deleteRow(ctx, s.DB.W, `DELETE FROM release_profiles WHERE id = ?`, id)
}

// --- delay profiles ---

func insertDelay(ctx context.Context, q db.Querier, d DelayProfile, now int64) (int64, error) {
	data, err := json.Marshal(d)
	if err != nil {
		return 0, err
	}
	res, err := q.ExecContext(ctx, `INSERT INTO delay_profiles (name, sort_order, is_default, data, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`, d.Name, d.Order, d.Default, string(data), now, now)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// DelayProfiles lists delay profiles in evaluation order (the default last).
func (s *Store) DelayProfiles(ctx context.Context) ([]DelayProfile, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT id, sort_order, is_default, data FROM delay_profiles ORDER BY is_default, sort_order, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DelayProfile{}
	for rows.Next() {
		var d DelayProfile
		var id int64
		var order int
		var def bool
		var data string
		if err := rows.Scan(&id, &order, &def, &data); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(data), &d); err != nil {
			return nil, err
		}
		d.ID, d.Order, d.Default = id, order, def
		if d.Tags == nil {
			d.Tags = []string{}
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// DelayProfile returns one delay profile.
func (s *Store) DelayProfile(ctx context.Context, id int64) (DelayProfile, error) {
	list, err := s.DelayProfiles(ctx)
	if err != nil {
		return DelayProfile{}, err
	}
	for _, d := range list {
		if d.ID == id {
			return d, nil
		}
	}
	return DelayProfile{}, ErrNotFound
}

// SaveDelayProfile creates (ID 0, placed before the default) or replaces a delay profile.
func (s *Store) SaveDelayProfile(ctx context.Context, d DelayProfile) (DelayProfile, error) {
	if d.ID != 0 {
		cur, err := s.DelayProfile(ctx, d.ID)
		if err != nil {
			return d, err
		}
		d.Default, d.Order = cur.Default, cur.Order
	} else {
		d.Default = false
	}
	if err := d.Validate(); err != nil {
		return d, err
	}
	now := s.now()
	if d.ID == 0 {
		var maxOrder sql.NullInt64
		if err := s.DB.W.QueryRowContext(ctx, `SELECT MAX(sort_order) FROM delay_profiles WHERE is_default = 0`).Scan(&maxOrder); err != nil {
			return d, err
		}
		d.Order = int(maxOrder.Int64) + 1
		id, err := insertDelay(ctx, s.DB.W, d, now)
		d.ID = id
		return d, err
	}
	data, err := json.Marshal(d)
	if err != nil {
		return d, err
	}
	_, err = s.DB.W.ExecContext(ctx, `UPDATE delay_profiles SET name = ?, data = ?, updated_at = ? WHERE id = ?`, d.Name, string(data), now, d.ID)
	return d, err
}

// DeleteDelayProfile removes a delay profile (never the default).
func (s *Store) DeleteDelayProfile(ctx context.Context, id int64) error {
	d, err := s.DelayProfile(ctx, id)
	if err != nil {
		return err
	}
	if d.Default {
		return ErrDefault
	}
	return deleteRow(ctx, s.DB.W, `DELETE FROM delay_profiles WHERE id = ?`, id)
}

// ReorderDelayProfiles sets the evaluation order of the non-default profiles.
func (s *Store) ReorderDelayProfiles(ctx context.Context, ids []int64) error {
	list, err := s.DelayProfiles(ctx)
	if err != nil {
		return err
	}
	known := map[int64]bool{}
	for _, d := range list {
		if !d.Default {
			known[d.ID] = true
		}
	}
	if len(ids) != len(known) {
		return invalid("list every delay profile except the default exactly once")
	}
	for _, id := range ids {
		if !known[id] {
			return invalid("list every delay profile except the default exactly once")
		}
		delete(known, id)
	}
	return s.DB.Tx(ctx, func(tx *sql.Tx) error {
		for i, id := range ids {
			if _, err := tx.ExecContext(ctx, `UPDATE delay_profiles SET sort_order = ?, updated_at = ? WHERE id = ?`, i+1, s.now(), id); err != nil {
				return err
			}
		}
		return nil
	})
}
