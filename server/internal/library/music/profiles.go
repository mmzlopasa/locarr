package music

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/metadata"
)

// MetadataProfile chooses which albums of an artist the library holds: by
// primary type, by secondary types ("studio" stands for none) and by the
// statuses of their releases (D-041).
type MetadataProfile struct {
	ID              int64
	Name            string
	PrimaryTypes    []string
	SecondaryTypes  []string
	ReleaseStatuses []string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// Studio is the pseudo secondary type of an album without secondary types.
const Studio = "studio"

// WantsGroup reports whether an album of these types belongs in the library.
// An album without a primary type counts as "other"; every secondary type it
// has must be allowed (none = "studio").
func (p *MetadataProfile) WantsGroup(primary string, secondary []string) bool {
	primary = strings.ToLower(primary)
	if primary == "" {
		primary = "other"
	}
	if !slices.Contains(p.PrimaryTypes, primary) {
		return false
	}
	if len(secondary) == 0 {
		return slices.Contains(p.SecondaryTypes, Studio)
	}
	for _, s := range secondary {
		if !slices.Contains(p.SecondaryTypes, strings.ToLower(s)) {
			return false
		}
	}
	return true
}

// WantsStatus reports whether a release of this status counts.
func (p *MetadataProfile) WantsStatus(status string) bool {
	status = strings.ToLower(status)
	if status == "" {
		status = "official"
	}
	return slices.Contains(p.ReleaseStatuses, status)
}

func normList(in, allowed []string) ([]string, error) {
	out := []string{}
	for _, v := range in {
		v = strings.ToLower(strings.TrimSpace(v))
		if v == "" || slices.Contains(out, v) {
			continue
		}
		if !slices.Contains(allowed, v) {
			return nil, invalid("unknown value %q (known: %s)", v, strings.Join(allowed, ", "))
		}
		out = append(out, v)
	}
	return out, nil
}

// Validate normalises and checks a profile.
func (p *MetadataProfile) Validate() error {
	p.Name = strings.TrimSpace(p.Name)
	if p.Name == "" || len(p.Name) > 100 {
		return invalid("a metadata profile needs a name of at most 100 characters")
	}
	var err error
	if p.PrimaryTypes, err = normList(p.PrimaryTypes, metadata.PrimaryTypes); err != nil {
		return err
	}
	if p.SecondaryTypes, err = normList(p.SecondaryTypes, metadata.SecondaryTypes); err != nil {
		return err
	}
	if p.ReleaseStatuses, err = normList(p.ReleaseStatuses, metadata.ReleaseStatuses); err != nil {
		return err
	}
	switch {
	case len(p.PrimaryTypes) == 0:
		return invalid("choose at least one primary type")
	case len(p.SecondaryTypes) == 0:
		return invalid("choose at least one secondary type (studio = albums without one)")
	case len(p.ReleaseStatuses) == 0:
		return invalid("choose at least one release status")
	}
	return nil
}

const profileColumns = `id, name, primary_types, secondary_types, release_statuses, created_at, updated_at`

func scanProfile(row interface{ Scan(...any) error }) (MetadataProfile, error) {
	var (
		p                 MetadataProfile
		pt, st, rs        string
		created, updated  int64
	)
	if err := row.Scan(&p.ID, &p.Name, &pt, &st, &rs, &created, &updated); err != nil {
		return p, err
	}
	p.PrimaryTypes, p.SecondaryTypes, p.ReleaseStatuses = jsonList(pt), jsonList(st), jsonList(rs)
	p.CreatedAt, p.UpdatedAt = db.Time(created), db.Time(updated)
	return p, nil
}

// Profiles lists the metadata profiles.
func (s *Service) Profiles(ctx context.Context) ([]MetadataProfile, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT `+profileColumns+` FROM metadata_profiles ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []MetadataProfile{}
	for rows.Next() {
		p, err := scanProfile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Profile returns one metadata profile.
func (s *Service) Profile(ctx context.Context, id int64) (MetadataProfile, error) {
	p, err := scanProfile(s.DB.R.QueryRowContext(ctx, `SELECT `+profileColumns+` FROM metadata_profiles WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrProfileNotFound
	}
	return p, err
}

// SaveProfile creates (ID 0) or updates a profile. Artists using it are
// refreshed in the background so new albums appear and filtered ones go
// (albums with files always stay).
func (s *Service) SaveProfile(ctx context.Context, p MetadataProfile) (MetadataProfile, error) {
	if err := p.Validate(); err != nil {
		return p, err
	}
	now := db.Millis(s.now())
	var err error
	if p.ID == 0 {
		var res sql.Result
		res, err = s.DB.W.ExecContext(ctx, `INSERT INTO metadata_profiles (name, primary_types, secondary_types, release_statuses, created_at, updated_at)
			VALUES (?, ?, ?, ?, ?, ?)`, p.Name, jsonText(p.PrimaryTypes), jsonText(p.SecondaryTypes), jsonText(p.ReleaseStatuses), now, now)
		if err == nil {
			p.ID, _ = res.LastInsertId()
		}
	} else {
		if _, err := s.Profile(ctx, p.ID); err != nil {
			return p, err
		}
		_, err = s.DB.W.ExecContext(ctx, `UPDATE metadata_profiles SET name = ?, primary_types = ?, secondary_types = ?, release_statuses = ?,
			updated_at = ? WHERE id = ?`, p.Name, jsonText(p.PrimaryTypes), jsonText(p.SecondaryTypes), jsonText(p.ReleaseStatuses), now, p.ID)
		if err == nil {
			s.refreshUsers(ctx, p.ID)
		}
	}
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return p, invalid("a metadata profile named %q exists", p.Name)
		}
		return p, err
	}
	return s.Profile(ctx, p.ID)
}

func (s *Service) refreshUsers(ctx context.Context, profileID int64) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT item_id FROM artists WHERE metadata_profile_id = ?`, profileID)
	if err != nil {
		return
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			ids = append(ids, id)
		}
	}
	_ = rows.Close()
	for _, id := range ids {
		s.requestRefresh(ctx, id)
	}
}

// DeleteProfile removes an unused profile.
func (s *Service) DeleteProfile(ctx context.Context, id int64) error {
	var n int
	if err := s.DB.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM artists WHERE metadata_profile_id = ?`, id).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return ErrProfileInUse
	}
	var total int
	_ = s.DB.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM metadata_profiles`).Scan(&total)
	if total <= 1 {
		return invalid("the last metadata profile cannot be deleted")
	}
	res, err := s.DB.W.ExecContext(ctx, `DELETE FROM metadata_profiles WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrProfileNotFound
	}
	return nil
}
