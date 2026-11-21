package movies

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/library"
	"github.com/mmzlopasa/locarr/server/internal/metadata"
	"github.com/mmzlopasa/locarr/server/internal/quality"
)

// CollectionPart is a collection member with its library state.
type CollectionPart struct {
	metadata.MovieSummary
	MovieID *int64
}

// CollectionView is a collection with library state.
type CollectionView struct {
	metadata.Collection
	Members []CollectionPart
}

// Collection returns a provider collection with which members are in the library.
func (s *Service) Collection(ctx context.Context, id int64) (CollectionView, error) {
	c, err := s.Meta.Collection(ctx, id)
	if err != nil {
		return CollectionView{}, err
	}
	marked, err := s.mark(ctx, c.Parts)
	if err != nil {
		return CollectionView{}, err
	}
	v := CollectionView{Collection: c, Members: []CollectionPart{}}
	for _, m := range marked {
		v.Members = append(v.Members, CollectionPart(m))
	}
	return v, nil
}

// AddMissing adds every collection member not in the library with tmpl's options.
func (s *Service) AddMissing(ctx context.Context, collectionID int64, tmpl AddInput) ([]Movie, error) {
	v, err := s.Collection(ctx, collectionID)
	if err != nil {
		return nil, err
	}
	added := []Movie{}
	for _, p := range v.Members {
		if p.MovieID != nil {
			continue
		}
		in := tmpl
		in.TMDbID, in.Path = p.TMDbID, ""
		m, err := s.Add(ctx, in)
		if err != nil && !errors.Is(err, ErrExists) {
			return added, fmt.Errorf("add %s: %w", p.Title, err)
		}
		if err == nil {
			added = append(added, m)
		}
	}
	return added, nil
}

// List kinds.
const (
	ListTMDbList       = "tmdb_list"
	ListTMDbCollection = "tmdb_collection"
)

// ImportList adds movies automatically.
type ImportList struct {
	ID                  int64
	Name                string
	Kind                string
	ListID              string
	Enabled             bool
	RootFolderID        int64
	QualityProfileID    int64
	MinimumAvailability library.Availability
	Monitored           bool
	SearchOnAdd         bool
	Tags                []string
	LastSyncAt          *time.Time
	LastAdded           int
	LastError           string
	CreatedAt           time.Time
	UpdatedAt           time.Time
}

// ErrListNotFound is returned for an unknown import list.
var ErrListNotFound = errors.New("import list not found")

const listColumns = `id, name, kind, list_id, enabled, root_folder_id, quality_profile_id, minimum_availability, monitored, search_on_add,
	tags, last_sync_at, last_added, last_error, created_at, updated_at`

func scanList(row scanner) (ImportList, error) {
	var (
		l            ImportList
		avail, tags  string
		last         sql.NullInt64
		created, upd int64
	)
	err := row.Scan(&l.ID, &l.Name, &l.Kind, &l.ListID, &l.Enabled, &l.RootFolderID, &l.QualityProfileID, &avail, &l.Monitored,
		&l.SearchOnAdd, &tags, &last, &l.LastAdded, &l.LastError, &created, &upd)
	if errors.Is(err, sql.ErrNoRows) {
		return l, ErrListNotFound
	}
	l.MinimumAvailability, l.Tags, l.LastSyncAt = library.Availability(avail), jsonList(tags), db.NullTime(last)
	l.CreatedAt, l.UpdatedAt = db.Time(created), db.Time(upd)
	return l, err
}

// Lists returns the import lists.
func (s *Service) Lists(ctx context.Context) ([]ImportList, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT `+listColumns+` FROM import_lists ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ImportList{}
	for rows.Next() {
		l, err := scanList(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// GetList returns one import list.
func (s *Service) GetList(ctx context.Context, id int64) (ImportList, error) {
	return scanList(s.DB.R.QueryRowContext(ctx, `SELECT `+listColumns+` FROM import_lists WHERE id = ?`, id))
}

func (s *Service) validList(ctx context.Context, l *ImportList) error {
	l.Name, l.ListID = strings.TrimSpace(l.Name), strings.TrimSpace(l.ListID)
	if l.Name == "" || len(l.Name) > 100 {
		return invalid("name is required (up to 100 characters)")
	}
	if l.Kind != ListTMDbList && l.Kind != ListTMDbCollection {
		return invalid("unknown list kind %q", l.Kind)
	}
	if l.ListID == "" || len(l.ListID) > 100 || strings.ContainsAny(l.ListID, "/?#") {
		return invalid("list id is required")
	}
	if l.MinimumAvailability == "" {
		l.MinimumAvailability = library.Released
	}
	if !library.ValidAvailability(l.MinimumAvailability) {
		return invalid("unknown minimum availability %q", l.MinimumAvailability)
	}
	if _, err := s.rootFolder(ctx, l.RootFolderID); err != nil {
		return err
	}
	if err := s.checkProfile(ctx, l.QualityProfileID); err != nil {
		return err
	}
	l.Tags = quality.NormTags(l.Tags)
	if l.Tags == nil {
		l.Tags = []string{}
	}
	return nil
}

// SaveList creates (ID 0) or updates an import list.
func (s *Service) SaveList(ctx context.Context, l ImportList) (ImportList, error) {
	if err := s.validList(ctx, &l); err != nil {
		return l, err
	}
	now := db.Millis(s.now())
	var err error
	if l.ID == 0 {
		err = s.DB.W.QueryRowContext(ctx, `INSERT INTO import_lists (name, kind, list_id, enabled, root_folder_id, quality_profile_id,
			minimum_availability, monitored, search_on_add, tags, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
			l.Name, l.Kind, l.ListID, l.Enabled, l.RootFolderID, l.QualityProfileID, string(l.MinimumAvailability), l.Monitored, l.SearchOnAdd,
			marshal(l.Tags), now, now).Scan(&l.ID)
	} else {
		var res sql.Result
		res, err = s.DB.W.ExecContext(ctx, `UPDATE import_lists SET name = ?, kind = ?, list_id = ?, enabled = ?, root_folder_id = ?,
			quality_profile_id = ?, minimum_availability = ?, monitored = ?, search_on_add = ?, tags = ?, updated_at = ? WHERE id = ?`,
			l.Name, l.Kind, l.ListID, l.Enabled, l.RootFolderID, l.QualityProfileID, string(l.MinimumAvailability), l.Monitored, l.SearchOnAdd,
			marshal(l.Tags), now, l.ID)
		if err == nil {
			if n, _ := res.RowsAffected(); n == 0 {
				return l, ErrListNotFound
			}
		}
	}
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return l, invalid("a list named %q exists", l.Name)
		}
		return l, err
	}
	return s.GetList(ctx, l.ID)
}

// DeleteList removes an import list (its movies stay).
func (s *Service) DeleteList(ctx context.Context, id int64) error {
	res, err := s.DB.W.ExecContext(ctx, `DELETE FROM import_lists WHERE id = ?`, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrListNotFound
	}
	_, err = s.DB.W.ExecContext(ctx, `UPDATE movies SET added_by_list_id = NULL WHERE added_by_list_id = ?`, id)
	return err
}

func (s *Service) listItems(ctx context.Context, l ImportList) ([]metadata.MovieSummary, error) {
	if l.Kind == ListTMDbCollection {
		var id int64
		if _, err := fmt.Sscan(l.ListID, &id); err != nil {
			return nil, invalid("collection id must be a number")
		}
		c, err := s.Meta.Collection(ctx, id)
		return c.Parts, err
	}
	return s.Meta.List(ctx, l.ListID)
}

// SyncResult is what a list sync did.
type SyncResult struct {
	ListID   int64
	Found    int
	Added    []string
	Excluded int
	Error    string
}

// SyncList adds the list's movies that are missing and not excluded.
func (s *Service) SyncList(ctx context.Context, id int64) (SyncResult, error) {
	l, err := s.GetList(ctx, id)
	if err != nil {
		return SyncResult{}, err
	}
	res := SyncResult{ListID: id, Added: []string{}}
	items, err := s.listItems(metadata.Fresh(ctx), l)
	if err == nil {
		res.Found = len(items)
		for _, it := range items {
			var x int
			if s.DB.R.QueryRowContext(ctx, `SELECT 1 FROM import_exclusions WHERE tmdb_id = ?`, it.TMDbID).Scan(&x) == nil {
				res.Excluded++
				continue
			}
			lid := l.ID
			m, aerr := s.Add(ctx, AddInput{TMDbID: it.TMDbID, RootFolderID: l.RootFolderID, QualityProfileID: l.QualityProfileID,
				MinimumAvailability: l.MinimumAvailability, Monitored: l.Monitored, Tags: l.Tags, SearchOnAdd: l.SearchOnAdd, AddedByListID: &lid})
			switch {
			case aerr == nil:
				res.Added = append(res.Added, fmt.Sprintf("%s (%d)", m.Title, m.Year))
			case errors.Is(aerr, ErrExists):
			default:
				err = aerr
			}
			if err != nil {
				break
			}
		}
	}
	if err != nil {
		res.Error = err.Error()
	}
	_, _ = s.DB.W.ExecContext(ctx, `UPDATE import_lists SET last_sync_at = ?, last_added = ?, last_error = ? WHERE id = ?`,
		db.Millis(s.now()), len(res.Added), res.Error, id)
	return res, err
}

// SyncLists syncs every enabled list.
func (s *Service) SyncLists(ctx context.Context) ([]SyncResult, error) {
	lists, err := s.Lists(ctx)
	if err != nil {
		return nil, err
	}
	var out []SyncResult
	for _, l := range lists {
		if !l.Enabled {
			continue
		}
		r, err := s.SyncList(ctx, l.ID)
		if err != nil {
			s.log().Warn("import list sync", "list", l.Name, "err", err)
		}
		out = append(out, r)
	}
	return out, nil
}

// Exclusion is a movie lists never add.
type Exclusion struct {
	TMDbID    int64
	Title     string
	Year      int
	CreatedAt time.Time
}

// Exclusions lists them.
func (s *Service) Exclusions(ctx context.Context) ([]Exclusion, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT tmdb_id, title, year, created_at FROM import_exclusions ORDER BY title`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Exclusion{}
	for rows.Next() {
		var (
			e  Exclusion
			at int64
		)
		if err := rows.Scan(&e.TMDbID, &e.Title, &e.Year, &at); err != nil {
			return nil, err
		}
		e.CreatedAt = db.Time(at)
		out = append(out, e)
	}
	return out, rows.Err()
}

// DeleteExclusion lets lists add a movie again.
func (s *Service) DeleteExclusion(ctx context.Context, tmdbID int64) error {
	_, err := s.DB.W.ExecContext(ctx, `DELETE FROM import_exclusions WHERE tmdb_id = ?`, tmdbID)
	return err
}

// Calendar date kinds.
const (
	DateInCinemas = "in_cinemas"
	DateDigital   = "digital"
	DatePhysical  = "physical"
)

// CalendarEntry is one dated event of a movie.
type CalendarEntry struct {
	Date      string // YYYY-MM-DD
	Kind      string
	MovieID   int64
	Title     string
	Year      int
	HasFile   bool
	Monitored bool
	Poster    string
}

// Calendar returns movie dates between from and to (YYYY-MM-DD, inclusive).
func (s *Service) Calendar(ctx context.Context, from, to string) ([]CalendarEntry, error) {
	rows, err := s.DB.R.QueryContext(ctx, `
		SELECT d, k, i.id, i.title, i.year, f.id IS NOT NULL, i.monitored, m.poster FROM (
			SELECT item_id, in_cinemas AS d, 'in_cinemas' AS k FROM movies WHERE in_cinemas BETWEEN ? AND ?
			UNION ALL SELECT item_id, digital_release, 'digital' FROM movies WHERE digital_release BETWEEN ? AND ?
			UNION ALL SELECT item_id, physical_release, 'physical' FROM movies WHERE physical_release BETWEEN ? AND ?
		) x JOIN media_items i ON i.id = x.item_id JOIN movies m ON m.item_id = i.id LEFT JOIN media_files f ON f.item_id = i.id
		ORDER BY d, i.sort_title`, from, to, from, to, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CalendarEntry{}
	for rows.Next() {
		var e CalendarEntry
		if err := rows.Scan(&e.Date, &e.Kind, &e.MovieID, &e.Title, &e.Year, &e.HasFile, &e.Monitored, &e.Poster); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}
