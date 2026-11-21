package movies

import (
	"context"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/quality"
)

// List statuses.
const (
	StatusMissing     = "missing"      // monitored, available, no file
	StatusDownloaded  = "downloaded"   // has a file
	StatusDownloading = "downloading"  // an active download
	StatusCutoffUnmet = "cutoff_unmet" // has a file below the cutoff
	StatusUnavailable = "unavailable"  // not yet at minimum availability
	StatusUnmonitored = "unmonitored"
)

// ListFilter selects and orders movies.
type ListFilter struct {
	Monitored *bool
	Status    string
	ProfileID int64
	Tag       string
	Search    string
	Sort      string // title, year, added, rating, popularity, size, inCinemas, digital, physical
	Desc      bool
	Offset    int
	Limit     int
}

// DownloadState is a movie's current download.
type DownloadState struct {
	ID       int64
	Title    string
	Progress float64
	State    string
	Status   string
}

// ListItem is a movie with what the library list shows about it.
type ListItem struct {
	Movie
	Available     bool
	AvailableFrom *time.Time
	CutoffMet     bool
	Download      *DownloadState
}

// Status summarises the item for badges.
func (it *ListItem) Status() string {
	switch {
	case it.Download != nil && it.Download.Status == "active":
		return StatusDownloading
	case it.File != nil && !it.CutoffMet:
		return StatusCutoffUnmet
	case it.File != nil:
		return StatusDownloaded
	case !it.Monitored:
		return StatusUnmonitored
	case !it.Available:
		return StatusUnavailable
	}
	return StatusMissing
}

// CutoffMet reports whether f satisfies the profile (no upgrade wanted).
func CutoffMet(p *quality.Profile, f *File) bool {
	if f == nil || p == nil {
		return false
	}
	pos, _ := p.Position(f.Quality)
	if !p.UpgradesAllowed {
		return true
	}
	if pos < 0 {
		return false
	}
	if cut := p.CutoffPosition(); cut >= 0 && pos > cut {
		return false
	}
	return p.CutoffFormatScore <= 0 || f.Score >= p.CutoffFormatScore
}

var sortColumns = map[string]string{"title": "i.sort_title", "year": "i.year", "added": "i.added_at", "rating": "m.rating",
	"popularity": "m.popularity", "size": "COALESCE(f.size, 0)", "inCinemas": "m.in_cinemas", "digital": "m.digital_release",
	"physical": "m.physical_release"}

func (s *Service) profiles(ctx context.Context) (map[int64]*quality.Profile, error) {
	list, err := s.Quality.Profiles(ctx)
	if err != nil {
		return nil, err
	}
	out := map[int64]*quality.Profile{}
	for i := range list {
		list[i].Normalize()
		out[list[i].ID] = &list[i]
	}
	return out, nil
}

// List returns one page of movies and the number that match.
func (s *Service) List(ctx context.Context, f ListFilter) ([]ListItem, int, error) {
	where := []string{"i.kind = 'movie'"}
	var args []any
	if f.Monitored != nil {
		where, args = append(where, "i.monitored = ?"), append(args, *f.Monitored)
	}
	if f.ProfileID > 0 {
		where, args = append(where, "i.quality_profile_id = ?"), append(args, f.ProfileID)
	}
	if t := strings.ToLower(strings.TrimSpace(f.Tag)); t != "" {
		where, args = append(where, "EXISTS (SELECT 1 FROM json_each(i.tags) WHERE value = ?)"), append(args, t)
	}
	if q := strings.TrimSpace(f.Search); q != "" {
		like := "%" + strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(strings.ToLower(q)) + "%"
		where = append(where, `(lower(i.title) LIKE ? ESCAPE '\' OR lower(m.original_title) LIKE ? ESCAPE '\' OR m.imdb_id = ? OR CAST(m.tmdb_id AS TEXT) = ?)`)
		args = append(args, like, like, q, q)
	}
	goFilter := false
	switch f.Status {
	case StatusDownloaded:
		where = append(where, "f.id IS NOT NULL")
	case StatusMissing:
		where = append(where, "f.id IS NULL", "i.monitored = 1")
		goFilter = true // availability is computed
	case StatusUnmonitored:
		where = append(where, "i.monitored = 0")
	case StatusDownloading:
		where = append(where, `EXISTS (SELECT 1 FROM downloads d WHERE json_extract(d.item_ref, '$.ref') = 'movie:' || i.id AND d.status = 'active')`)
	case StatusCutoffUnmet:
		where = append(where, "f.id IS NOT NULL", "i.monitored = 1")
		goFilter = true
	case StatusUnavailable:
		where = append(where, "f.id IS NULL")
		goFilter = true
	}
	col, ok := sortColumns[f.Sort]
	if !ok {
		col = "i.sort_title"
	}
	dir := "ASC"
	if f.Desc {
		dir = "DESC"
	}
	if f.Limit <= 0 || f.Limit > 1000 {
		f.Limit = 100
	}
	clause := " WHERE " + strings.Join(where, " AND ")
	order := " ORDER BY " + col + " " + dir + ", i.sort_title, i.id"
	profiles, err := s.profiles(ctx)
	if err != nil {
		return nil, 0, err
	}
	var movies []Movie
	total := 0
	delay := s.availabilityDelay(ctx)
	if goFilter {
		all, err := s.query(ctx, `SELECT `+movieColumns+movieFrom+clause+order, args...) // #nosec G202 -- clauses are constants
		if err != nil {
			return nil, 0, err
		}
		var keep []Movie
		for i := range all {
			m := &all[i]
			avail, _ := s.availableWith(m, delay)
			met := CutoffMet(profiles[m.QualityProfileID], m.File)
			switch f.Status {
			case StatusMissing:
				if !avail {
					continue
				}
			case StatusCutoffUnmet:
				if met {
					continue
				}
			case StatusUnavailable:
				if avail || !m.Monitored {
					continue
				}
			}
			keep = append(keep, *m)
		}
		total = len(keep)
		lo := min(f.Offset, len(keep))
		hi := min(lo+f.Limit, len(keep))
		movies = keep[lo:hi]
	} else {
		if err := s.DB.R.QueryRowContext(ctx, `SELECT COUNT(*)`+movieFrom+clause, args...).Scan(&total); err != nil { // #nosec G202
			return nil, 0, err
		}
		movies, err = s.query(ctx, `SELECT `+movieColumns+movieFrom+clause+order+` LIMIT ? OFFSET ?`, append(args, f.Limit, max(f.Offset, 0))...) // #nosec G202
		if err != nil {
			return nil, 0, err
		}
	}
	items, err := s.decorate(ctx, movies, profiles, delay)
	return items, total, err
}

// decorate adds availability, cutoff and download state.
func (s *Service) decorate(ctx context.Context, movies []Movie, profiles map[int64]*quality.Profile, delay time.Duration) ([]ListItem, error) {
	items := make([]ListItem, len(movies))
	refs := make([]any, 0, len(movies))
	byRef := map[string]int{}
	for i := range movies {
		items[i] = ListItem{Movie: movies[i]}
		items[i].Available, items[i].AvailableFrom = s.availableWith(&movies[i], delay)
		items[i].CutoffMet = CutoffMet(profiles[movies[i].QualityProfileID], movies[i].File)
		ref := movies[i].Ref().String()
		refs = append(refs, ref)
		byRef[ref] = i
	}
	if len(refs) == 0 {
		return items, nil
	}
	rows, err := s.DB.R.QueryContext(ctx, `SELECT json_extract(item_ref, '$.ref'), id, title, progress, state, status FROM downloads
		WHERE json_extract(item_ref, '$.ref') IN (`+strings.TrimSuffix(strings.Repeat("?,", len(refs)), ",")+`)
		AND status IN ('active', 'completed') ORDER BY added_at`, refs...) // #nosec G202 -- placeholders only
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var (
			ref string
			d   DownloadState
		)
		if err := rows.Scan(&ref, &d.ID, &d.Title, &d.Progress, &d.State, &d.Status); err != nil {
			return nil, err
		}
		if i, ok := byRef[ref]; ok {
			dd := d
			items[i].Download = &dd
		}
	}
	return items, rows.Err()
}

// Item returns one movie with its list facts.
func (s *Service) Item(ctx context.Context, id int64) (ListItem, error) {
	m, err := s.Get(ctx, id)
	if err != nil {
		return ListItem{}, err
	}
	profiles, err := s.profiles(ctx)
	if err != nil {
		return ListItem{}, err
	}
	items, err := s.decorate(ctx, []Movie{m}, profiles, s.availabilityDelay(ctx))
	if err != nil {
		return ListItem{}, err
	}
	return items[0], nil
}
