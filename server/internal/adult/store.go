package adult

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/library"
	"github.com/mmzlopasa/locarr/server/internal/library/naming"
	"github.com/mmzlopasa/locarr/server/internal/parser"
)

// Item kinds.
const (
	KindScene   = "scene"
	KindMovie   = "movie"
	KindGallery = "gallery"
)

// Studio is a studio or site.
type Studio struct {
	ID               int64      `json:"id"`
	Name             string     `json:"name"`
	StashDBID        string     `json:"stashdbId,omitempty"`
	TPDBID           string     `json:"tpdbId,omitempty"`
	URL              string     `json:"url,omitempty"`
	Image            string     `json:"image,omitempty"`
	Monitored        bool       `json:"monitored"`
	MonitorSince     string     `json:"monitorSince,omitempty"`
	QualityProfileID int64      `json:"qualityProfileId"`
	RootFolderID     *int64     `json:"rootFolderId,omitempty"`
	Path             string     `json:"path"`
	AddedAt          time.Time  `json:"addedAt"`
	RefreshedAt      *time.Time `json:"refreshedAt,omitempty"`
	Scenes           int        `json:"scenes"`
	Files            int        `json:"files"`
}

// Performer is a performer.
type Performer struct {
	ID               int64      `json:"id"`
	Name             string     `json:"name"`
	Disambiguation   string     `json:"disambiguation,omitempty"`
	StashDBID        string     `json:"stashdbId,omitempty"`
	TPDBID           string     `json:"tpdbId,omitempty"`
	Image            string     `json:"image,omitempty"`
	Monitored        bool       `json:"monitored"`
	MonitorSince     string     `json:"monitorSince,omitempty"`
	QualityProfileID int64      `json:"qualityProfileId,omitempty"`
	RootFolderID     *int64     `json:"rootFolderId,omitempty"`
	AddedAt          time.Time  `json:"addedAt"`
	RefreshedAt      *time.Time `json:"refreshedAt,omitempty"`
	Items            int        `json:"items"`
	// Galleries: each new scene of a followed performer also gets its
	// photo set as a wanted gallery (D-112).
	Galleries bool `json:"galleries"`
}

// File is a scene's or movie's file.
type File struct {
	ID           int64           `json:"id"`
	Path         string          `json:"path"`
	Size         int64           `json:"size"`
	Quality      string          `json:"quality"`
	Revision     parser.Revision `json:"revision"`
	Score        int             `json:"score"`
	ReleaseGroup string          `json:"releaseGroup,omitempty"`
	SceneName    string          `json:"sceneName,omitempty"`
	AddedAt      time.Time       `json:"addedAt"`
	// ImageCount is a gallery's number of images (Path is its folder, Size
	// their total).
	ImageCount int `json:"imageCount,omitempty"`
}

// Item is a scene or a movie.
type Item struct {
	ID               int64     `json:"id"`
	Kind             string    `json:"kind"`
	StudioID         *int64    `json:"studioId,omitempty"`
	Studio           string    `json:"studio"`
	Title            string    `json:"title"`
	ReleaseDate      string    `json:"releaseDate,omitempty"`
	Year             int       `json:"year,omitempty"`
	Duration         int       `json:"duration,omitempty"`
	Overview         string    `json:"overview,omitempty"`
	StashDBID        string    `json:"stashdbId,omitempty"`
	TPDBID           string    `json:"tpdbId,omitempty"`
	PHash            string    `json:"phash,omitempty"`
	SourceTags       []string  `json:"sourceTags"`
	Image            string    `json:"image,omitempty"`
	Monitored        bool      `json:"monitored"`
	QualityProfileID int64     `json:"qualityProfileId"`
	RootFolderID     *int64    `json:"rootFolderId,omitempty"`
	Path             string    `json:"path"`
	AddedAt          time.Time `json:"addedAt"`
	Performers       []PerfRef `json:"performers"`
	File             *File     `json:"file,omitempty"`
	// SceneID links a gallery to the scene it is the photo set of.
	SceneID *int64 `json:"sceneId,omitempty"`
	// Status: downloaded (in the library), downloading, wanted (monitored,
	// no file) or missing (not monitored, no file).
	Status string `json:"status"`
}

// Item statuses.
const (
	StatusDownloaded  = "downloaded"
	StatusDownloading = "downloading"
	StatusWanted      = "wanted"
	StatusMissing     = "missing"
)

// PerfRef names a performer of an item.
type PerfRef struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// Ref is the item's timeline/download ref.
func (it *Item) Ref() library.Ref {
	switch it.Kind {
	case KindMovie:
		return library.Ref{Kind: library.RefAdultMovie, ID: it.ID}
	case KindGallery:
		return library.Ref{Kind: library.RefAdultGallery, ID: it.ID}
	}
	return library.Ref{Kind: library.RefScene, ID: it.ID}
}

// ItemID returns the item id behind an adult ref.
func ItemID(r library.Ref) (int64, bool) {
	if !library.IsAdult(r.Kind) {
		return 0, false
	}
	return r.ID, true
}

const itemColumns = `i.id, i.kind, i.studio_id, i.studio_name, i.title, i.release_date, i.year, i.duration, i.overview, i.stashdb_id,
	i.tpdb_id, i.phash, i.source_tags, i.image, i.monitored, i.quality_profile_id, i.root_folder_id, i.path, i.added_at,
	f.id, f.path, f.size, f.quality, f.revision, f.score, f.release_group, f.scene_name, f.added_at, f.image_count, i.scene_id, ` + itemStatus

// itemStatus is computed per row; download_refs is keyed by item_ref.
const itemStatus = `CASE WHEN f.id IS NOT NULL THEN 'downloaded'
	WHEN EXISTS (SELECT 1 FROM download_refs r JOIN downloads d ON d.id = r.download_id
		WHERE r.item_ref = (CASE i.kind WHEN 'movie' THEN 'adult_movie:' WHEN 'gallery' THEN 'adult_gallery:' ELSE 'scene:' END) || i.id AND d.status IN ('active', 'completed'))
		THEN 'downloading'
	WHEN i.monitored = 1 THEN 'wanted' ELSE 'missing' END`

const itemFrom = ` FROM adult_items i LEFT JOIN adult_files f ON f.item_id = i.id`

func scanItem(row interface{ Scan(...any) error }) (Item, error) {
	var (
		it                      Item
		studio, root            sql.NullInt64
		tags                    string
		added                   int64
		fid, fsize, fscore, fat sql.NullInt64
		fcount, sceneID         sql.NullInt64
		fpath, fq, frev, fg, fs sql.NullString
	)
	if err := row.Scan(&it.ID, &it.Kind, &studio, &it.Studio, &it.Title, &it.ReleaseDate, &it.Year, &it.Duration, &it.Overview,
		&it.StashDBID, &it.TPDBID, &it.PHash, &tags, &it.Image, &it.Monitored, &it.QualityProfileID, &root, &it.Path, &added,
		&fid, &fpath, &fsize, &fq, &frev, &fscore, &fg, &fs, &fat, &fcount, &sceneID, &it.Status); err != nil {
		return it, err
	}
	if studio.Valid {
		it.StudioID = &studio.Int64
	}
	if root.Valid {
		it.RootFolderID = &root.Int64
	}
	if sceneID.Valid {
		it.SceneID = &sceneID.Int64
	}
	_ = json.Unmarshal([]byte(tags), &it.SourceTags)
	if it.SourceTags == nil {
		it.SourceTags = []string{}
	}
	it.AddedAt = db.Time(added)
	it.Performers = []PerfRef{}
	if fid.Valid {
		f := &File{ID: fid.Int64, Path: fpath.String, Size: fsize.Int64, Quality: fq.String, Score: int(fscore.Int64),
			ReleaseGroup: fg.String, SceneName: fs.String, AddedAt: db.Time(fat.Int64), ImageCount: int(fcount.Int64)}
		_ = json.Unmarshal([]byte(frev.String), &f.Revision)
		it.File = f
	}
	return it, nil
}

// ItemFilter narrows List.
type ItemFilter struct {
	Kind        string
	StudioID    int64
	// SceneID keeps the galleries linked to one scene.
	SceneID     int64
	PerformerID int64
	Term        string
	Missing     bool
	// Status keeps only items with that status (see Item.Status).
	Status string
	// Monitored, when set, keeps only (un)monitored items.
	Monitored *bool
	// Sort: date (newest first, the default), added (newest first), title.
	Sort     string
	From, To string // release date range (calendar)
	Limit    int
	Offset   int
}

// Items lists items (scenes newest first, movies by title).
func (s *Service) Items(ctx context.Context, f ItemFilter) ([]Item, int, error) {
	where := []string{"1 = 1"}
	var args []any
	if f.Kind != "" {
		where, args = append(where, "i.kind = ?"), append(args, f.Kind)
	}
	if f.StudioID > 0 {
		where, args = append(where, "i.studio_id = ?"), append(args, f.StudioID)
	}
	if f.SceneID > 0 {
		where, args = append(where, "i.scene_id = ?"), append(args, f.SceneID)
	}
	if f.PerformerID > 0 {
		where, args = append(where, "i.id IN (SELECT item_id FROM adult_item_performers WHERE performer_id = ?)"), append(args, f.PerformerID)
	}
	if t := strings.TrimSpace(f.Term); t != "" {
		where, args = append(where, "(i.title LIKE ? OR i.studio_name LIKE ?)"), append(args, "%"+t+"%", "%"+t+"%")
	}
	if f.Missing {
		where = append(where, "i.monitored = 1 AND f.id IS NULL")
	}
	if f.Status != "" {
		where, args = append(where, "("+itemStatus+") = ?"), append(args, f.Status)
	}
	if f.Monitored != nil {
		where, args = append(where, "i.monitored = ?"), append(args, *f.Monitored)
	}
	if f.From != "" {
		where, args = append(where, "i.release_date >= ?"), append(args, f.From)
	}
	if f.To != "" {
		where, args = append(where, "i.release_date <= ?"), append(args, f.To)
	}
	cond := " WHERE " + strings.Join(where, " AND ")
	var total int
	// #nosec G202 -- fixed fragments, values bound
	if err := s.DB.R.QueryRowContext(ctx, `SELECT count(*)`+itemFrom+cond, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	switch {
	case f.Limit < 0: // internal callers: everything
	case f.Limit == 0 || f.Limit > 500:
		f.Limit = 100
	}
	order := " ORDER BY i.release_date DESC, i.id DESC"
	switch {
	case f.Sort == "added":
		order = " ORDER BY i.added_at DESC, i.id DESC"
	case f.Sort == "title", f.Sort == "" && f.Kind == KindMovie:
		order = " ORDER BY i.sort_title, i.id"
	}
	rows, err := s.DB.R.QueryContext(ctx, `SELECT `+itemColumns+itemFrom+cond+order+` LIMIT ? OFFSET ?`, append(args, f.Limit, f.Offset)...) // #nosec G202 -- fixed fragments, values bound
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Item{}
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, it)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return out, total, s.loadPerformers(ctx, out)
}

func (s *Service) loadPerformers(ctx context.Context, items []Item) error {
	for i := range items {
		rows, err := s.DB.R.QueryContext(ctx, `SELECT p.id, p.name FROM adult_item_performers ip JOIN adult_performers p ON p.id = ip.performer_id
			WHERE ip.item_id = ? ORDER BY p.sort_name`, items[i].ID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var p PerfRef
			if err := rows.Scan(&p.ID, &p.Name); err != nil {
				_ = rows.Close()
				return err
			}
			items[i].Performers = append(items[i].Performers, p)
		}
		_ = rows.Close()
	}
	return nil
}

// Item returns one item.
func (s *Service) Item(ctx context.Context, id int64) (Item, error) {
	it, err := scanItem(s.DB.R.QueryRowContext(ctx, `SELECT `+itemColumns+itemFrom+` WHERE i.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return it, ErrNotFound
	}
	if err != nil {
		return it, err
	}
	list := []Item{it}
	err = s.loadPerformers(ctx, list)
	return list[0], err
}

const studioColumns = `s.id, s.name, s.stashdb_id, s.tpdb_id, s.url, s.image, s.monitored, s.monitor_since, s.quality_profile_id, s.root_folder_id,
	s.path, s.added_at, s.refreshed_at,
	(SELECT count(*) FROM adult_items i WHERE i.studio_id = s.id AND i.kind <> 'gallery'),
	(SELECT count(*) FROM adult_items i JOIN adult_files f ON f.item_id = i.id WHERE i.studio_id = s.id AND i.kind <> 'gallery')`

func scanStudio(row interface{ Scan(...any) error }) (Studio, error) {
	var (
		st        Studio
		root, ref sql.NullInt64
		added     int64
	)
	err := row.Scan(&st.ID, &st.Name, &st.StashDBID, &st.TPDBID, &st.URL, &st.Image, &st.Monitored, &st.MonitorSince, &st.QualityProfileID,
		&root, &st.Path, &added, &ref, &st.Scenes, &st.Files)
	if root.Valid {
		st.RootFolderID = &root.Int64
	}
	st.AddedAt, st.RefreshedAt = db.Time(added), db.NullTime(ref)
	return st, err
}

// Studios lists studios by name.
func (s *Service) Studios(ctx context.Context) ([]Studio, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT `+studioColumns+` FROM adult_studios s ORDER BY s.sort_name, s.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Studio{}
	for rows.Next() {
		st, err := scanStudio(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, st)
	}
	return out, rows.Err()
}

// Studio returns one studio.
func (s *Service) Studio(ctx context.Context, id int64) (Studio, error) {
	st, err := scanStudio(s.DB.R.QueryRowContext(ctx, `SELECT `+studioColumns+` FROM adult_studios s WHERE s.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return st, ErrNotFound
	}
	return st, err
}

const performerColumns = `p.id, p.name, p.disambiguation, p.stashdb_id, p.tpdb_id, p.image, p.monitored, p.monitor_since,
	COALESCE(p.quality_profile_id, 0), p.root_folder_id, p.added_at, p.refreshed_at,
	(SELECT count(*) FROM adult_item_performers ip WHERE ip.performer_id = p.id), p.galleries`

func scanPerformer(row interface{ Scan(...any) error }) (Performer, error) {
	var (
		p         Performer
		root, ref sql.NullInt64
		added     int64
	)
	err := row.Scan(&p.ID, &p.Name, &p.Disambiguation, &p.StashDBID, &p.TPDBID, &p.Image, &p.Monitored, &p.MonitorSince, &p.QualityProfileID,
		&root, &added, &ref, &p.Items, &p.Galleries)
	if root.Valid {
		p.RootFolderID = &root.Int64
	}
	p.AddedAt, p.RefreshedAt = db.Time(added), db.NullTime(ref)
	return p, err
}

// Performers lists performers by name.
func (s *Service) Performers(ctx context.Context) ([]Performer, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT `+performerColumns+` FROM adult_performers p ORDER BY p.sort_name, p.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Performer{}
	for rows.Next() {
		p, err := scanPerformer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Performer returns one performer.
func (s *Service) Performer(ctx context.Context, id int64) (Performer, error) {
	p, err := scanPerformer(s.DB.R.QueryRowContext(ctx, `SELECT `+performerColumns+` FROM adult_performers p WHERE p.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

// Count is the number of adult items (for the opt-in aggregate in /api/stats).
func (s *Service) Count(ctx context.Context) (int, error) {
	var n int
	err := s.DB.R.QueryRowContext(ctx, `SELECT count(*) FROM adult_items`).Scan(&n)
	return n, err
}

func sortName(s string) string { return naming.SortTitle(s) }
