package series

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/customformats"
	lfs "github.com/mmzlopasa/locarr/server/internal/fs"
	"github.com/mmzlopasa/locarr/server/internal/library/naming"
	"github.com/mmzlopasa/locarr/server/internal/metadata"
	"github.com/mmzlopasa/locarr/server/internal/parser"
	"github.com/mmzlopasa/locarr/server/internal/quality"
	"github.com/mmzlopasa/locarr/server/internal/rootfolders"
	"github.com/mmzlopasa/locarr/server/internal/timeline"
)

// Imported is a series read from another application (P10), with its
// seasons, episodes (monitoring kept) and episode files. It is added without
// asking the metadata provider; the next refresh fills in the rest.
type Imported struct {
	TMDbID            int64
	TVDbID            int64
	IMDbID            string
	TVMazeID          int64
	Title             string
	OriginalLanguage  string
	Year              int
	Overview          string
	Status            string // continuing, ended, upcoming
	Network           string
	Runtime           int
	Genres            []string
	Certification     string
	FirstAired        string
	Poster            string
	SeriesType        string
	SeasonFolder      bool
	MonitorNewSeasons string
	Monitored         bool
	QualityProfileID  int64
	RootFolderID      int64
	Tags              []string
	Path              string
	Seasons           []ImportedSeason
	Episodes          []ImportedEpisode
	Files             []ImportedFile
	Note              string
	Data              map[string]any
}

// ImportedSeason is a season and whether it is monitored.
type ImportedSeason struct {
	Number    int
	Monitored bool
}

// ImportedEpisode is one episode; FileKey points into Imported.Files.
type ImportedEpisode struct {
	Season     int
	Number     int
	Absolute   int
	TVDbID     int64
	Title      string
	Overview   string
	AirDate    string
	AirDateUTC *time.Time
	Runtime    int
	Monitored  bool
	FileKey    string
}

// ImportedFile is an episode file the other application had.
type ImportedFile struct {
	Key          string
	Path         string
	Quality      string // Locarr quality id ("" = from the name)
	ReleaseGroup string
	SceneName    string
	Languages    []string
	Revision     int
	AddedAt      time.Time
}

// ImportOutcome is what happened to one imported series.
type ImportOutcome struct {
	ID       int64
	Created  bool
	Episodes int
	// Files linked now; Missing files the source had that are not on disk.
	Files   int
	Missing []string
	Err     error
}

// ImportSeries adds a series from another application, or merges it into
// the series already in the library (same TMDb or TVDB id): episodes that
// have no file get the source's file. Files are linked in place.
func (s *Service) ImportSeries(ctx context.Context, in Imported) ImportOutcome {
	var o ImportOutcome
	if in.TMDbID <= 0 && in.TVDbID <= 0 {
		o.Err = invalid("tmdbId or tvdbId is required")
		return o
	}
	if ex, err := s.ByProviderID(ctx, in.TMDbID, in.TVDbID); err == nil {
		o.ID = ex.ID
		o.Files, o.Missing, o.Err = s.mergeImported(ctx, &ex, &in)
		return o
	} else if !errors.Is(err, ErrNotFound) {
		o.Err = err
		return o
	}
	root, err := s.importRoot(ctx, in.RootFolderID)
	if err != nil {
		o.Err = err
		return o
	}
	if in.SeriesType == "" || !ValidType(in.SeriesType) {
		in.SeriesType = TypeStandard
	}
	if in.MonitorNewSeasons != MonitorNone {
		in.MonitorNewSeasons = MonitorAll
	}
	switch in.Status {
	case "continuing", "ended", "upcoming":
	default:
		in.Status = "continuing"
	}
	v := Series{TMDbID: in.TMDbID, TVDbID: in.TVDbID, IMDbID: in.IMDbID, TVMazeID: in.TVMazeID, Source: "tmdb",
		Title: strings.TrimSpace(in.Title), OriginalLanguage: in.OriginalLanguage, AlternateTitles: []string{}, Year: in.Year,
		Overview: in.Overview, Status: in.Status, Network: in.Network, Runtime: in.Runtime, Genres: in.Genres,
		Certification: in.Certification, FirstAired: in.FirstAired, SeriesType: in.SeriesType, SeasonFolder: in.SeasonFolder,
		MonitorNewSeasons: in.MonitorNewSeasons, Poster: in.Poster, Monitored: in.Monitored, QualityProfileID: in.QualityProfileID,
		RootFolderID: &root.ID, Tags: quality.NormTags(in.Tags)}
	if in.TMDbID <= 0 && s.TVDB != nil {
		v.Source = "tvdb"
	}
	if v.Title == "" {
		v.Title = fmt.Sprintf("TVDB %d", in.TVDbID)
	}
	if v.Genres == nil {
		v.Genres = []string{}
	}
	v.SortTitle = naming.SortTitle(v.Title)
	if v.Path, err = s.libraryPath(root.Path, in.Path); err != nil {
		o.Err = err
		return o
	}
	files, missing := s.checkFiles(in.Files)
	o.Missing = missing
	sc := s.newScorer(ctx, v.QualityProfileID, v.OriginalLanguage)
	now := s.now()
	note := in.Note
	if note == "" {
		note = "imported from another application"
	}
	err = s.DB.Tx(ctx, func(tx *sql.Tx) error {
		if err := insertSeries(ctx, tx, &v, now); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE series SET metadata_refreshed_at = NULL WHERE item_id = ?`, v.ID); err != nil {
			return err
		}
		seasonIDs := map[int]int64{}
		seasonMon := map[int]bool{}
		for _, sn := range in.Seasons {
			seasonMon[sn.Number] = sn.Monitored
		}
		ensure := func(n int) (int64, error) {
			if id, ok := seasonIDs[n]; ok {
				return id, nil
			}
			mon, ok := seasonMon[n]
			if !ok {
				mon = n != 0
			}
			title := fmt.Sprintf("Season %d", n)
			if n == 0 {
				title = "Specials"
			}
			id, err := insertSeason(ctx, tx, v.ID, metadata.Season{Number: n, Title: title}, mon)
			seasonIDs[n] = id
			return id, err
		}
		for _, sn := range in.Seasons {
			if _, err := ensure(sn.Number); err != nil {
				return err
			}
		}
		fileIDs := map[string]int64{}
		epStmt, err := tx.PrepareContext(ctx, `INSERT INTO episodes (series_id, season_id, season_number, episode_number, absolute_number,
			tvdb_id, title, overview, air_date, air_date_utc, runtime, monitored, file_id, added_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT DO NOTHING`)
		if err != nil {
			return err
		}
		defer epStmt.Close()
		for _, e := range in.Episodes {
			sid, err := ensure(e.Season)
			if err != nil {
				return err
			}
			air := e.AirDateUTC
			if air == nil {
				air = s.airUTC(&v, e.AirDate)
			}
			var fileID any
			if f, ok := files[e.FileKey]; ok && e.FileKey != "" {
				id, ok := fileIDs[e.FileKey]
				if !ok {
					id, err = s.saveImportedFile(ctx, tx, &v, f, sc, now)
					if err != nil {
						return err
					}
					fileIDs[e.FileKey] = id
				}
				fileID = id
				o.Files++
			}
			if _, err := epStmt.ExecContext(ctx, v.ID, sid, e.Season, e.Number, e.Absolute, e.TVDbID, e.Title, e.Overview, e.AirDate,
				nullMillis(air), e.Runtime, e.Monitored, fileID, now.UnixMilli()); err != nil {
				return err
			}
			o.Episodes++
		}
		data := in.Data
		if data == nil {
			data = map[string]any{}
		}
		data["path"], data["episodes"], data["files"] = v.Path, o.Episodes, o.Files
		return timeline.RecordAt(ctx, tx, v.Ref().String(), timeline.TypeMigrated, note, data, now)
	})
	if err != nil {
		o.Err, o.Files, o.Episodes = err, 0, 0
		return o
	}
	o.ID, o.Created = v.ID, true
	return o
}

// mergeImported links the source's files to episodes of an existing series
// that have none yet (matched by season and episode number).
func (s *Service) mergeImported(ctx context.Context, v *Series, in *Imported) (int, []string, error) {
	files, missing := s.checkFiles(in.Files)
	if len(files) == 0 {
		return 0, missing, nil
	}
	eps, err := s.Episodes(ctx, v.ID)
	if err != nil {
		return 0, missing, err
	}
	byNum := map[[2]int]*Episode{}
	for i := range eps {
		byNum[[2]int{eps[i].Season, eps[i].Number}] = &eps[i]
	}
	sc := s.newScorer(ctx, v.QualityProfileID, v.OriginalLanguage)
	now := s.now()
	n := 0
	err = s.DB.Tx(ctx, func(tx *sql.Tx) error {
		fileIDs := map[string]int64{}
		for _, e := range in.Episodes {
			f, ok := files[e.FileKey]
			if !ok || e.FileKey == "" {
				continue
			}
			local := byNum[[2]int{e.Season, e.Number}]
			if local == nil || local.FileID != nil {
				continue
			}
			id, ok := fileIDs[e.FileKey]
			if !ok {
				var err error
				if id, err = s.saveImportedFile(ctx, tx, v, f, sc, now); err != nil {
					return err
				}
				fileIDs[e.FileKey] = id
			}
			if _, err := tx.ExecContext(ctx, `UPDATE episodes SET file_id = ? WHERE id = ?`, id, local.ID); err != nil {
				return err
			}
			if err := timeline.RecordAt(ctx, tx, local.Ref().String(), timeline.TypeFileFound,
				"linked "+filepath.Base(f.Path)+" in place (imported)", map[string]any{"path": f.Path}, now); err != nil {
				return err
			}
			n++
		}
		return nil
	})
	if err != nil {
		return 0, missing, err
	}
	return n, missing, nil
}

type checkedFile struct {
	ImportedFile
	size int64
}

// checkFiles keeps the files that are on disk inside the data roots.
func (s *Service) checkFiles(list []ImportedFile) (map[string]checkedFile, []string) {
	out := map[string]checkedFile{}
	var missing []string
	cache := &lfs.PathCache{Roots: s.DataRoots}
	for _, f := range list {
		p, err := cache.SafePath(f.Path)
		if err != nil {
			missing = append(missing, f.Path)
			continue
		}
		st, err := os.Stat(p)
		if err != nil || st.IsDir() {
			missing = append(missing, f.Path)
			continue
		}
		f.Path = p
		out[f.Key] = checkedFile{ImportedFile: f, size: st.Size()}
	}
	return out, missing
}

type importScorer struct {
	compiled []*customformats.Compiled
	profile  *quality.Profile
	lang     string
}

func (s *Service) newScorer(ctx context.Context, profileID int64, lang string) *importScorer {
	sc := &importScorer{lang: lang}
	if p, err := s.Quality.Profile(ctx, profileID); err == nil {
		sc.profile = &p
	}
	if s.Formats != nil {
		if list, err := s.Formats.List(ctx); err == nil {
			sc.compiled, _ = customformats.CompileAll(list)
		}
	}
	return sc
}

func (s *Service) saveImportedFile(ctx context.Context, tx *sql.Tx, v *Series, f checkedFile, sc *importScorer, now time.Time) (int64, error) {
	rel := parser.ParsePath(f.Path)
	if f.SceneName != "" {
		if named := parser.Parse(f.SceneName); named.Confidence >= rel.Confidence {
			named.Extension = rel.Extension
			rel = named
		}
	}
	q := quality.Identify(&rel, quality.Series)
	if x, ok := quality.ByID(f.Quality); ok && !x.Music {
		q = x
	}
	file := &File{Path: f.Path, Size: f.size, Quality: q.ID, Revision: rel.Revision, ReleaseGroup: f.ReleaseGroup,
		Languages: f.Languages, MediaInfo: mediaInfo(&rel), SceneName: f.SceneName, AddedAt: f.AddedAt}
	if f.Revision > 0 {
		file.Revision.Version = f.Revision
	}
	if file.Revision.Version == 0 {
		file.Revision.Version = 1
	}
	if file.ReleaseGroup == "" {
		file.ReleaseGroup = rel.Group
	}
	if len(file.Languages) == 0 {
		file.Languages = rel.Languages
	}
	if file.Languages == nil {
		file.Languages = []string{}
	}
	if sc.profile != nil && len(sc.compiled) > 0 {
		matches, score := customformats.Score(sc.compiled, sc.profile, customformats.Input{Title: filepath.Base(f.Path), Release: &rel,
			Quality: q, OriginalLanguage: sc.lang})
		for _, m := range matches {
			file.CustomFormats = append(file.CustomFormats, m.Name)
		}
		file.Score = score
	}
	if err := saveFile(ctx, tx, v.ID, file, now); err != nil {
		return 0, err
	}
	return file.ID, nil
}

// RefreshImported refreshes a series added by ImportSeries: one known only by
// its TVDB id gets its TMDb id from TMDb first when TVDB is not set up.
func (s *Service) RefreshImported(ctx context.Context, id int64) error {
	v, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	if v.Source == "tmdb" && v.TMDbID <= 0 && v.TVDbID > 0 {
		if s.TMDb == nil {
			return metadata.ErrNoKey
		}
		tmdbID, err := s.TMDb.FindSeries(ctx, "tvdb", fmt.Sprint(v.TVDbID))
		if err != nil {
			return err
		}
		if _, err := s.DB.W.ExecContext(ctx, `UPDATE series SET tmdb_id = ? WHERE item_id = ?`, tmdbID, id); err != nil {
			return err
		}
	}
	_, _, err = s.Refresh(ctx, id)
	return err
}

// NeedsRefresh lists the series (of ids, or all) never refreshed.
func (s *Service) NeedsRefresh(ctx context.Context, ids []int64) ([]int64, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT item_id FROM series WHERE metadata_refreshed_at IS NULL`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	want := map[int64]bool{}
	for _, id := range ids {
		want[id] = true
	}
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		if len(want) == 0 || want[id] {
			out = append(out, id)
		}
	}
	return out, rows.Err()
}

// importRoot checks a root folder without looking at the disk (imports add
// thousands of items).
func (s *Service) importRoot(ctx context.Context, id int64) (rootfolders.Folder, error) {
	list, err := s.Roots.Records(ctx)
	if err != nil {
		return rootfolders.Folder{}, err
	}
	for _, f := range list {
		if f.ID == id {
			if f.Kind != rootfolders.KindSeries {
				return f, invalid("root folder %s is for %s, not series", f.Path, f.Kind)
			}
			return f, nil
		}
	}
	return rootfolders.Folder{}, rootfolders.ErrNotFound
}
