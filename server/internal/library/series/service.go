package series

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/customformats"
	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/downloads"
	lfs "github.com/mmzlopasa/locarr/server/internal/fs"
	"github.com/mmzlopasa/locarr/server/internal/library"
	"github.com/mmzlopasa/locarr/server/internal/library/naming"
	"github.com/mmzlopasa/locarr/server/internal/library/recycle"
	"github.com/mmzlopasa/locarr/server/internal/mediaserver"
	"github.com/mmzlopasa/locarr/server/internal/metadata"
	"github.com/mmzlopasa/locarr/server/internal/quality"
	"github.com/mmzlopasa/locarr/server/internal/rootfolders"
	"github.com/mmzlopasa/locarr/server/internal/settings"
	"github.com/mmzlopasa/locarr/server/internal/timeline"
)

// Service is the series library.
type Service struct {
	DB       *db.DB
	Settings *settings.Store
	// TMDb answers series lookups by default; TVDB when the setting says so
	// (nil = not available).
	TMDb      metadata.SeriesProvider
	TVDB      metadata.SeriesProvider
	Quality   *quality.Store
	Formats   *customformats.Store
	Roots     *rootfolders.Service
	DataRoots []string
	Timeline  *timeline.Store
	Recycle   *recycle.Bin
	Downloads *downloads.Service
	Notifier  mediaserver.Notifier
	Log       *slog.Logger
	Now       func() time.Time
	// SearchRequested asks the wanted orchestration to search an item
	// (episode or season ref); MakeDue makes it due for the next scheduled
	// search. Both are set by the app (nil = nothing happens).
	SearchRequested func(ctx context.Context, ref library.Ref, reason string)
	MakeDue         func(ctx context.Context, ref library.Ref)
	// Imported is told about every imported episode file (subtitles, P7).
	Imported func(ctx context.Context, refs []library.Ref)
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) log() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

func (s *Service) record(ctx context.Context, ref library.Ref, typ, msg string, data any, downloadID *int64) {
	if s.Timeline == nil {
		return
	}
	if err := s.Timeline.Record(ctx, ref.String(), typ, msg, data, downloadID); err != nil {
		s.log().Warn("timeline", "err", err)
	}
}

func (s *Service) provider(source string) (metadata.SeriesProvider, error) {
	if source == "tvdb" {
		if s.TVDB == nil {
			return nil, metadata.ErrNoKey
		}
		return s.TVDB, nil
	}
	if s.TMDb == nil {
		return nil, metadata.ErrNoKey
	}
	return s.TMDb, nil
}

func (s *Service) defaultSource(ctx context.Context) string {
	v, _ := settings.Get(ctx, s.Settings, metadata.SeriesSource)
	if v == "tvdb" && s.TVDB != nil {
		return "tvdb"
	}
	return "tmdb"
}

// LookupResult is a provider result, marked when it is in the library.
type LookupResult struct {
	metadata.SeriesSummary
	Source   string
	SeriesID *int64
}

var (
	tmdbTerm = regexp.MustCompile(`^(?i)tmdb[: ]?(\d+)$`)
	tvdbTerm = regexp.MustCompile(`^(?i)tvdb[: ]?(\d+)$`)
	imdbTerm = regexp.MustCompile(`^(?i)(?:imdb[: ]?)?(tt\d{4,10})$`)
	yearTail = regexp.MustCompile(`^(.*?)[\s(]+((?:19|20)\d{2})\)?$`)
)

// Lookup searches the provider by text ("Title", "Title 2015"), TMDb id
// ("tmdb:1399"), TVDB id ("tvdb:121361") or IMDb id ("tt0944947").
func (s *Service) Lookup(ctx context.Context, term string) ([]LookupResult, error) {
	term = strings.TrimSpace(term)
	if term == "" {
		return []LookupResult{}, nil
	}
	source := s.defaultSource(ctx)
	p, err := s.provider(source)
	if err != nil {
		return nil, err
	}
	var list []metadata.SeriesSummary
	byID := func(src, id string) error {
		pid, err := p.FindSeries(ctx, src, id)
		if errors.Is(err, metadata.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		md, err := p.Series(ctx, pid)
		if errors.Is(err, metadata.ErrNotFound) {
			return nil
		}
		if err != nil {
			return err
		}
		list = append(list, md.SeriesSummary)
		return nil
	}
	switch {
	case tmdbTerm.MatchString(term):
		err = byID("tmdb", tmdbTerm.FindStringSubmatch(term)[1])
	case tvdbTerm.MatchString(term):
		err = byID("tvdb", tvdbTerm.FindStringSubmatch(term)[1])
	case imdbTerm.MatchString(term):
		err = byID("imdb", imdbTerm.FindStringSubmatch(term)[1])
	default:
		q, y := term, 0
		if mm := yearTail.FindStringSubmatch(term); mm != nil && strings.TrimSpace(mm[1]) != "" {
			q = strings.TrimSpace(mm[1])
			y, _ = strconv.Atoi(mm[2])
		}
		if list, err = p.SearchSeries(ctx, q, y); err == nil && len(list) == 0 && y > 0 {
			list, err = p.SearchSeries(ctx, term, 0)
		}
	}
	if err != nil {
		return nil, err
	}
	out := make([]LookupResult, 0, len(list))
	for _, r := range list {
		lr := LookupResult{SeriesSummary: r, Source: source}
		if ex, err := s.ByProviderID(ctx, r.TMDbID, r.TVDbID); err == nil {
			id := ex.ID
			lr.SeriesID = &id
		}
		out = append(out, lr)
	}
	return out, nil
}

// AddInput adds a series.
type AddInput struct {
	TMDbID            int64
	TVDbID            int64
	RootFolderID      int64
	QualityProfileID  int64
	SeriesType        string
	SeasonFolder      bool
	Monitor           string
	MonitorNewSeasons string
	Monitored         bool
	Tags              []string
	SearchOnAdd       bool
	// Path overrides the folder (inside the root folder); "" = from the template.
	Path string
}

func (s *Service) rootFolder(ctx context.Context, id int64) (rootfolders.Folder, error) {
	list, err := s.Roots.List(ctx)
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

func (s *Service) checkProfile(ctx context.Context, id int64) error {
	p, err := s.Quality.Profile(ctx, id)
	if err != nil {
		return err
	}
	if p.Kind != quality.Series {
		return invalid("quality profile %s is for %s, not series", p.Name, p.Kind)
	}
	return nil
}

func fromMetadata(v *Series, md metadata.Series) {
	if md.TMDbID > 0 {
		v.TMDbID = md.TMDbID
	}
	if md.TVDbID > 0 {
		v.TVDbID = md.TVDbID
	}
	if md.IMDbID != "" {
		v.IMDbID = md.IMDbID
	}
	if md.TVMazeID > 0 {
		v.TVMazeID = md.TVMazeID
	}
	v.Title, v.OriginalTitle, v.OriginalLanguage, v.Overview = md.Title, md.OriginalTitle, md.OriginalLanguage, md.Overview
	v.SortTitle = naming.SortTitle(md.Title)
	v.AlternateTitles, v.Year, v.Status, v.Network = md.AlternateTitles, md.Year, md.Status, md.Network
	v.TimeZone, v.AirTime, v.Runtime, v.Genres, v.Certification = md.TimeZone, md.AirTime, md.Runtime, md.Genres, md.Certification
	v.Rating, v.Votes, v.Popularity, v.FirstAired, v.LastAired = md.Rating, md.Votes, md.Popularity, md.FirstAired, md.LastAired
	v.Poster, v.Backdrop = md.Poster, md.Backdrop
	if v.Status == "" {
		v.Status = metadata.SeriesContinuing
	}
	if v.AlternateTitles == nil {
		v.AlternateTitles = []string{}
	}
	if v.Genres == nil {
		v.Genres = []string{}
	}
}

func (s *Service) info(v *Series) naming.SeriesInfo {
	return naming.SeriesInfo{Title: v.Title, Year: v.Year, TVDbID: v.TVDbID, TMDbID: v.TMDbID, IMDbID: v.IMDbID}
}

func (s *Service) colon(ctx context.Context) naming.Options {
	c, _ := settings.Get(ctx, s.Settings, library.ColonReplacement)
	return naming.Options{Colon: c}
}

// folderName renders the series folder name.
func (s *Service) folderName(ctx context.Context, v *Series) (string, error) {
	tmpl, err := settings.Get(ctx, s.Settings, SeriesFolderFormat)
	if err != nil {
		return "", err
	}
	return naming.Render(tmpl, naming.SeriesValues(s.info(v)), s.colon(ctx))
}

// libraryPath checks that p lies inside the root folder (and the data roots).
func (s *Service) libraryPath(root, p string) (string, error) {
	sp, err := lfs.SafePath(s.DataRoots, p)
	if err != nil {
		return "", err
	}
	sroot, err := lfs.SafePath(s.DataRoots, root)
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(sroot, sp)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return "", invalid("the series folder must be inside the root folder %s", root)
	}
	return sp, nil
}

func (s *Service) airUTC(v *Series, date string) *time.Time {
	t, ok := metadata.AirTimeUTC(date, v.AirTime, v.TimeZone)
	if !ok {
		return nil
	}
	return &t
}

func nullMillis(t *time.Time) any {
	if t == nil {
		return nil
	}
	return db.Millis(*t)
}

func insertSeason(ctx context.Context, q db.Querier, seriesID int64, sn metadata.Season, monitored bool) (int64, error) {
	var id int64
	err := q.QueryRowContext(ctx, `INSERT INTO seasons (series_id, number, monitored, title, overview, poster, air_date) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT (series_id, number) DO UPDATE SET title = excluded.title, overview = excluded.overview, poster = excluded.poster,
		air_date = excluded.air_date RETURNING id`, seriesID, sn.Number, monitored, sn.Title, sn.Overview, sn.Poster, sn.AirDate).Scan(&id)
	return id, err
}

func (s *Service) insertEpisode(ctx context.Context, q db.Querier, v *Series, seasonID int64, e metadata.Episode, monitored bool, now time.Time) error {
	_, err := q.ExecContext(ctx, `INSERT INTO episodes (series_id, season_id, season_number, episode_number, absolute_number, tmdb_id, tvdb_id,
		title, overview, air_date, air_date_utc, runtime, monitored, added_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		v.ID, seasonID, e.Season, e.Number, e.Absolute, e.TMDbID, e.TVDbID, e.Title, e.Overview, e.AirDate, nullMillis(s.airUTC(v, e.AirDate)),
		e.Runtime, monitored, db.Millis(now))
	return err
}

// Add adds a series from the provider with its seasons and episodes.
func (s *Service) Add(ctx context.Context, in AddInput) (Series, error) {
	if in.TMDbID <= 0 && in.TVDbID <= 0 {
		return Series{}, invalid("tmdbId or tvdbId is required")
	}
	if in.SeriesType == "" {
		in.SeriesType = TypeStandard
	}
	if !ValidType(in.SeriesType) {
		return Series{}, invalid("unknown series type %q", in.SeriesType)
	}
	if in.Monitor == "" {
		in.Monitor = MonitorAll
	}
	if !ValidMonitor(in.Monitor) {
		return Series{}, invalid("unknown monitor option %q", in.Monitor)
	}
	if in.MonitorNewSeasons == "" {
		in.MonitorNewSeasons = MonitorAll
	}
	if in.MonitorNewSeasons != MonitorAll && in.MonitorNewSeasons != MonitorNone {
		return Series{}, invalid("monitorNewSeasons must be all or none")
	}
	if existing, err := s.ByProviderID(ctx, in.TMDbID, in.TVDbID); err == nil {
		return existing, ErrExists
	}
	root, err := s.rootFolder(ctx, in.RootFolderID)
	if err != nil {
		return Series{}, err
	}
	if err := s.checkProfile(ctx, in.QualityProfileID); err != nil {
		return Series{}, err
	}
	source, pid := "tmdb", in.TMDbID
	if in.TMDbID <= 0 || (s.defaultSource(ctx) == "tvdb" && in.TVDbID > 0) {
		source, pid = "tvdb", in.TVDbID
	}
	p, err := s.provider(source)
	if err != nil {
		return Series{}, err
	}
	md, err := p.Series(ctx, pid)
	if err != nil {
		return Series{}, err
	}
	if existing, err := s.ByProviderID(ctx, md.TMDbID, md.TVDbID); err == nil {
		return existing, ErrExists
	}
	v := Series{Source: source, Monitored: in.Monitored, QualityProfileID: in.QualityProfileID, RootFolderID: &root.ID,
		SeriesType: in.SeriesType, SeasonFolder: in.SeasonFolder, MonitorNewSeasons: in.MonitorNewSeasons, Tags: quality.NormTags(in.Tags)}
	if v.Tags == nil {
		v.Tags = []string{}
	}
	fromMetadata(&v, md)
	path := in.Path
	if path == "" {
		name, err := s.folderName(ctx, &v)
		if err != nil {
			return Series{}, err
		}
		path = filepath.Join(root.Path, name)
	}
	if v.Path, err = s.libraryPath(root.Path, path); err != nil {
		return Series{}, err
	}
	FillAbsolute(md.Seasons)
	now := s.now()
	var states []EpisodeState
	var numbers []int
	for _, sn := range md.Seasons {
		numbers = append(numbers, sn.Number)
		for _, e := range sn.Episodes {
			states = append(states, EpisodeState{Season: e.Season, Number: e.Number, AirUTC: s.airUTC(&v, e.AirDate)})
		}
	}
	epMon, seasonMon := ApplyMonitor(in.Monitor, states, numbers, now)
	err = s.DB.Tx(ctx, func(tx *sql.Tx) error {
		if err := insertSeries(ctx, tx, &v, now); err != nil {
			return err
		}
		k := 0
		for _, sn := range md.Seasons {
			sid, err := insertSeason(ctx, tx, v.ID, sn, seasonMon[sn.Number])
			if err != nil {
				return err
			}
			for _, e := range sn.Episodes {
				if err := s.insertEpisode(ctx, tx, &v, sid, e, epMon[k], now); err != nil {
					return err
				}
				k++
			}
		}
		return nil
	})
	if err != nil {
		return Series{}, err
	}
	monitoredCount := 0
	for _, m := range epMon {
		if m {
			monitoredCount++
		}
	}
	s.record(ctx, v.Ref(), timeline.TypeAdded, fmt.Sprintf("added %s (%d) to %s; %d of %d episodes monitored (%s)", v.Title, v.Year, v.Path,
		monitoredCount, len(epMon), in.Monitor), map[string]any{"path": v.Path, "monitored": v.Monitored, "monitor": in.Monitor,
		"profileId": v.QualityProfileID, "seriesType": v.SeriesType, "episodes": len(epMon), "monitoredEpisodes": monitoredCount}, nil)
	if in.SearchOnAdd && v.Monitored && s.SearchRequested != nil {
		s.SearchRequested(ctx, v.Ref(), "added")
	}
	return s.Get(ctx, v.ID)
}

// Update changes how a series is managed.
type Update struct {
	Monitored         *bool
	QualityProfileID  *int64
	SeriesType        *string
	SeasonFolder      *bool
	MonitorNewSeasons *string
	Tags              *[]string
	RootFolderID      *int64
	Path              *string
	// Monitor re-applies a monitor option to every episode.
	Monitor *string
}

func (s *Service) hasFiles(ctx context.Context, id int64) bool {
	var n int
	_ = s.DB.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM media_files WHERE item_id = ?`, id).Scan(&n)
	return n > 0
}

// Edit applies an update.
func (s *Service) Edit(ctx context.Context, id int64, u Update) (Series, error) {
	v, err := s.Get(ctx, id)
	if err != nil {
		return v, err
	}
	changes := map[string]any{}
	if u.Monitored != nil && *u.Monitored != v.Monitored {
		v.Monitored, changes["monitored"] = *u.Monitored, *u.Monitored
	}
	if u.QualityProfileID != nil && *u.QualityProfileID != v.QualityProfileID {
		if err := s.checkProfile(ctx, *u.QualityProfileID); err != nil {
			return v, err
		}
		v.QualityProfileID, changes["profileId"] = *u.QualityProfileID, *u.QualityProfileID
	}
	if u.SeriesType != nil && *u.SeriesType != v.SeriesType {
		if !ValidType(*u.SeriesType) {
			return v, invalid("unknown series type %q", *u.SeriesType)
		}
		v.SeriesType, changes["seriesType"] = *u.SeriesType, *u.SeriesType
	}
	if u.SeasonFolder != nil && *u.SeasonFolder != v.SeasonFolder {
		v.SeasonFolder, changes["seasonFolder"] = *u.SeasonFolder, *u.SeasonFolder
	}
	if u.MonitorNewSeasons != nil && *u.MonitorNewSeasons != v.MonitorNewSeasons {
		if *u.MonitorNewSeasons != MonitorAll && *u.MonitorNewSeasons != MonitorNone {
			return v, invalid("monitorNewSeasons must be all or none")
		}
		v.MonitorNewSeasons, changes["monitorNewSeasons"] = *u.MonitorNewSeasons, *u.MonitorNewSeasons
	}
	if u.Tags != nil {
		t := quality.NormTags(*u.Tags)
		if t == nil {
			t = []string{}
		}
		v.Tags, changes["tags"] = t, t
	}
	if u.RootFolderID != nil || (u.Path != nil && *u.Path != "") {
		rootID := int64(0)
		if v.RootFolderID != nil {
			rootID = *v.RootFolderID
		}
		if u.RootFolderID != nil {
			rootID = *u.RootFolderID
		}
		root, err := s.rootFolder(ctx, rootID)
		if err != nil {
			return v, err
		}
		path := ""
		if u.Path != nil && *u.Path != "" {
			path = *u.Path
		} else {
			name, err := s.folderName(ctx, &v)
			if err != nil {
				return v, err
			}
			path = filepath.Join(root.Path, name)
		}
		sp, err := s.libraryPath(root.Path, path)
		if err != nil {
			return v, err
		}
		if sp != v.Path {
			if s.hasFiles(ctx, v.ID) {
				return v, ErrHasFiles
			}
			v.Path, v.RootFolderID, changes["path"] = sp, &root.ID, sp
		}
	}
	if u.Monitor != nil {
		if !ValidMonitor(*u.Monitor) {
			return v, invalid("unknown monitor option %q", *u.Monitor)
		}
		if err := s.applyMonitor(ctx, &v, *u.Monitor); err != nil {
			return v, err
		}
		changes["monitor"] = *u.Monitor
	}
	if len(changes) == 0 {
		return v, nil
	}
	_, err = s.DB.W.ExecContext(ctx, `UPDATE media_items SET monitored = ?, quality_profile_id = ?, tags = ?, path = ?, root_folder_id = ?,
		updated_at = ? WHERE id = ?`, v.Monitored, v.QualityProfileID, marshal(v.Tags), v.Path, v.RootFolderID, db.Millis(s.now()), v.ID)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return v, ErrPathTaken
		}
		return v, err
	}
	if _, err := s.DB.W.ExecContext(ctx, `UPDATE series SET series_type = ?, season_folder = ?, monitor_new_seasons = ? WHERE item_id = ?`,
		v.SeriesType, v.SeasonFolder, v.MonitorNewSeasons, v.ID); err != nil {
		return v, err
	}
	s.record(ctx, v.Ref(), timeline.TypeEdited, "settings changed", changes, nil)
	return s.Get(ctx, id)
}

func (s *Service) applyMonitor(ctx context.Context, v *Series, option string) error {
	eps, err := s.Episodes(ctx, v.ID)
	if err != nil {
		return err
	}
	seasons, err := s.Seasons(ctx, v.ID)
	if err != nil {
		return err
	}
	states := make([]EpisodeState, len(eps))
	for i, e := range eps {
		states[i] = EpisodeState{Season: e.Season, Number: e.Number, AirUTC: e.AirDateUTC, HasFile: e.FileID != nil}
	}
	numbers := make([]int, len(seasons))
	for i, sn := range seasons {
		numbers[i] = sn.Number
	}
	epMon, seasonMon := ApplyMonitor(option, states, numbers, s.now())
	return s.DB.Tx(ctx, func(tx *sql.Tx) error {
		for i, e := range eps {
			if e.Season == 0 && option != MonitorNone {
				continue // specials keep their own setting
			}
			if _, err := tx.ExecContext(ctx, `UPDATE episodes SET monitored = ? WHERE id = ?`, epMon[i], e.ID); err != nil {
				return err
			}
		}
		for _, sn := range seasons {
			if sn.Number == 0 && option != MonitorNone {
				continue
			}
			if _, err := tx.ExecContext(ctx, `UPDATE seasons SET monitored = ? WHERE id = ?`, seasonMon[sn.Number], sn.ID); err != nil {
				return err
			}
		}
		return nil
	})
}

// SetSeasonMonitored (un)monitors a season and every episode in it.
func (s *Service) SetSeasonMonitored(ctx context.Context, seriesID int64, number int, on bool) (Season, error) {
	sn, err := s.SeasonByNumber(ctx, seriesID, number)
	if err != nil {
		return sn, err
	}
	err = s.DB.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE seasons SET monitored = ? WHERE id = ?`, on, sn.ID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE episodes SET monitored = ? WHERE season_id = ? AND removed = 0`, on, sn.ID)
		return err
	})
	if err != nil {
		return sn, err
	}
	s.record(ctx, library.SeriesRef(seriesID), timeline.TypeEdited, fmt.Sprintf("season %d %s", number, map[bool]string{true: "monitored", false: "unmonitored"}[on]),
		map[string]any{"season": number, "monitored": on}, nil)
	if on && s.MakeDue != nil {
		s.MakeDue(ctx, sn.Ref())
	}
	return s.SeasonByID(ctx, sn.ID)
}

// SetEpisodesMonitored (un)monitors episodes; monitoring an episode
// monitors its season too (so it is searched).
func (s *Service) SetEpisodesMonitored(ctx context.Context, ids []int64, on bool) (int, error) {
	n := 0
	err := s.DB.Tx(ctx, func(tx *sql.Tx) error {
		for _, id := range ids {
			res, err := tx.ExecContext(ctx, `UPDATE episodes SET monitored = ? WHERE id = ?`, on, id)
			if err != nil {
				return err
			}
			if c, _ := res.RowsAffected(); c > 0 {
				n++
			}
			if on {
				if _, err := tx.ExecContext(ctx, `UPDATE seasons SET monitored = 1 WHERE id = (SELECT season_id FROM episodes WHERE id = ?)`, id); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err == nil && on && s.MakeDue != nil {
		for _, id := range ids {
			s.MakeDue(ctx, library.EpisodeRef(id))
		}
	}
	return n, err
}

// SceneMapping is how releases number an episode (nil = the provider's numbers).
type SceneMapping struct {
	Season   *int
	Episode  *int
	Absolute *int
}

// SetSceneMapping stores an episode's manual scene numbering (D-047).
func (s *Service) SetSceneMapping(ctx context.Context, id int64, m SceneMapping) (Episode, error) {
	e, err := s.Episode(ctx, id)
	if err != nil {
		return e, err
	}
	for _, v := range []*int{m.Season, m.Episode, m.Absolute} {
		if v != nil && (*v < 0 || *v > 100000) {
			return e, invalid("scene numbers must be between 0 and 100000")
		}
	}
	if _, err := s.DB.W.ExecContext(ctx, `UPDATE episodes SET scene_season = ?, scene_episode = ?, scene_absolute = ? WHERE id = ?`,
		m.Season, m.Episode, m.Absolute, id); err != nil {
		return e, err
	}
	s.record(ctx, e.Ref(), timeline.TypeEdited, "scene numbering changed", map[string]any{"sceneSeason": m.Season, "sceneEpisode": m.Episode,
		"sceneAbsolute": m.Absolute}, nil)
	return s.Episode(ctx, id)
}

// DeleteOptions control Delete.
type DeleteOptions struct {
	// DeleteFiles moves the series folder to the recycle bin.
	DeleteFiles bool
}

// Delete removes a series from the library. Files are never deleted: with
// DeleteFiles the folder goes to the recycle bin.
func (s *Service) Delete(ctx context.Context, id int64, o DeleteOptions) error {
	v, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	msg := "removed from the library; files left in place"
	if o.DeleteFiles {
		if _, err := os.Stat(v.Path); err == nil {
			e, err := s.Recycle.Recycle(ctx, v.Path, v.Ref().String(), "series deleted")
			if err != nil {
				return fmt.Errorf("move %s to the recycle bin: %w", v.Path, err)
			}
			msg = "removed from the library; folder moved to the recycle bin (" + e.RecycledPath + ")"
		} else {
			msg = "removed from the library; the folder did not exist"
		}
	}
	s.record(ctx, v.Ref(), timeline.TypeDeleted, msg, map[string]any{"deleteFiles": o.DeleteFiles}, nil)
	var refs []string
	rows, err := s.DB.R.QueryContext(ctx, `SELECT 'episode:' || id FROM episodes WHERE series_id = ? UNION ALL SELECT 'season:' || id FROM seasons WHERE series_id = ?`, id, id)
	if err == nil {
		for rows.Next() {
			var r string
			if rows.Scan(&r) == nil {
				refs = append(refs, r)
			}
		}
		_ = rows.Close()
	}
	if _, err := s.DB.W.ExecContext(ctx, `DELETE FROM media_items WHERE id = ?`, id); err != nil {
		return err
	}
	// Season and episode ids can be reused by SQLite after a delete: their
	// history must not attach to a series added later (the series' own
	// "deleted" event stays).
	for _, r := range refs {
		_, _ = s.DB.W.ExecContext(ctx, `DELETE FROM timeline_events WHERE item_ref = ?`, r)
		_, _ = s.DB.W.ExecContext(ctx, `DELETE FROM download_refs WHERE item_ref = ?`, r)
		_, _ = s.DB.W.ExecContext(ctx, `UPDATE search_log SET item_ref = NULL WHERE item_ref = ?`, r)
	}
	for _, r := range append(refs, v.Ref().String()) {
		_, _ = s.DB.W.ExecContext(ctx, `DELETE FROM search_state WHERE item_ref = ?`, r)
		_, _ = s.DB.W.ExecContext(ctx, `DELETE FROM pending_releases WHERE item_ref = ?`, r)
	}
	return nil
}

// RefreshResult says what a refresh changed.
type RefreshResult struct {
	Added, Updated, Renumbered, Deleted, Kept, MissingFiles int
}

// Refresh reloads a series from its provider (skipping the cache) and merges
// seasons and episodes: new ones are added and monitored by the rules,
// renumbered ones keep their files and monitoring, removed ones are deleted
// (or kept, unmonitored, when they have a file). Files that vanished from
// disk are unlinked.
func (s *Service) Refresh(ctx context.Context, id int64) (Series, RefreshResult, error) {
	var res RefreshResult
	v, err := s.Get(ctx, id)
	if err != nil {
		return v, res, err
	}
	p, err := s.provider(v.Source)
	if err != nil {
		return v, res, err
	}
	pid := v.TMDbID
	if v.Source == "tvdb" {
		pid = v.TVDbID
	}
	md, err := p.Series(metadata.Fresh(ctx), pid)
	if err != nil {
		return v, res, err
	}
	before := v
	fromMetadata(&v, md)
	FillAbsolute(md.Seasons)
	existing, err := s.Episodes(ctx, id)
	if err != nil {
		return v, res, err
	}
	seasons, err := s.Seasons(ctx, id)
	if err != nil {
		return v, res, err
	}
	seasonMon := map[int]bool{}
	for _, sn := range seasons {
		seasonMon[sn.Number] = sn.Monitored
	}
	plan := PlanMerge(existing, md.Seasons, v.Source, seasonMon, v.MonitorNewSeasons == MonitorAll)
	// A kept episode whose numbers the provider gave to another one moves
	// out of the way (negative number) so both survive.
	targets := map[[2]int]bool{}
	for _, u := range plan.Update {
		targets[[2]int{u.Season, u.Number}] = true
	}
	for _, n := range plan.Insert {
		targets[[2]int{n.Season, n.Number}] = true
	}
	clash := map[int64]bool{}
	for _, e := range existing {
		for _, k := range plan.Keep {
			if e.ID == k && targets[[2]int{e.Season, e.Number}] {
				clash[k] = true
			}
		}
	}
	now := s.now()
	err = s.DB.Tx(ctx, func(tx *sql.Tx) error {
		if err := saveSeriesMetadata(ctx, tx, &v, now); err != nil {
			return err
		}
		seasonIDs := map[int]int64{}
		newSeason := map[int]bool{}
		for _, n := range plan.NewSeasons {
			newSeason[n] = true
		}
		for _, sn := range md.Seasons {
			mon := seasonMon[sn.Number]
			if newSeason[sn.Number] {
				mon = v.MonitorNewSeasons == MonitorAll && sn.Number != 0
			}
			sid, err := insertSeason(ctx, tx, v.ID, sn, mon)
			if err != nil {
				return err
			}
			seasonIDs[sn.Number] = sid
		}
		ensureSeason := func(n int) (int64, error) {
			if sid, ok := seasonIDs[n]; ok {
				return sid, nil
			}
			sid, err := insertSeason(ctx, tx, v.ID, metadata.Season{Number: n, Title: fmt.Sprintf("Season %d", n)}, false)
			seasonIDs[n] = sid
			return sid, err
		}
		for _, d := range plan.Delete {
			if _, err := tx.ExecContext(ctx, `DELETE FROM episodes WHERE id = ?`, d); err != nil {
				return err
			}
		}
		for _, k := range plan.Keep {
			if _, err := tx.ExecContext(ctx, `UPDATE episodes SET removed = 1, monitored = 0,
				episode_number = CASE WHEN ? THEN -id ELSE episode_number END WHERE id = ?`, clash[k], k); err != nil {
				return err
			}
		}
		// Renumbering in two steps so (season, number) stays unique meanwhile.
		for _, u := range plan.Update {
			if u.Renumbered {
				if _, err := tx.ExecContext(ctx, `UPDATE episodes SET episode_number = -id WHERE id = ?`, u.ID); err != nil {
					return err
				}
			}
		}
		for _, u := range plan.Update {
			sid, err := ensureSeason(u.Season)
			if err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE episodes SET season_id = ?, season_number = ?, episode_number = ?, absolute_number = ?,
				tmdb_id = CASE WHEN ? > 0 THEN ? ELSE tmdb_id END, tvdb_id = CASE WHEN ? > 0 THEN ? ELSE tvdb_id END, title = ?, overview = ?,
				air_date = ?, air_date_utc = ?, runtime = ?, removed = 0 WHERE id = ?`,
				sid, u.Season, u.Number, u.Absolute, u.TMDbID, u.TMDbID, u.TVDbID, u.TVDbID, u.Title, u.Overview, u.AirDate,
				nullMillis(s.airUTC(&v, u.AirDate)), u.Runtime, u.ID); err != nil {
				return err
			}
		}
		for _, n := range plan.Insert {
			sid, err := ensureSeason(n.Season)
			if err != nil {
				return err
			}
			if err := s.insertEpisode(ctx, tx, &v, sid, n.Episode, n.Monitored, now); err != nil {
				return err
			}
		}
		// Seasons the provider dropped and that hold no episodes any more.
		_, err := tx.ExecContext(ctx, `DELETE FROM seasons WHERE series_id = ? AND NOT EXISTS (SELECT 1 FROM episodes e WHERE e.season_id = seasons.id)`, v.ID)
		return err
	})
	if err != nil {
		return v, res, err
	}
	res.Added, res.Deleted, res.Kept = len(plan.Insert), len(plan.Delete), len(plan.Keep)
	for _, u := range plan.Update {
		res.Updated++
		if u.Renumbered {
			res.Renumbered++
			s.record(ctx, library.EpisodeRef(u.ID), timeline.TypeRefreshed, fmt.Sprintf("renumbered from S%02dE%02d to S%02dE%02d by %s",
				u.FromSeason, u.FromNumber, u.Season, u.Number, v.Source), map[string]any{"fromSeason": u.FromSeason, "fromEpisode": u.FromNumber,
				"season": u.Season, "episode": u.Number}, nil)
		}
	}
	for _, k := range plan.Keep {
		s.record(ctx, library.EpisodeRef(k), timeline.TypeRefreshed, "no longer listed by "+v.Source+"; kept because it has a file (unmonitored)",
			map[string]any{"removed": true}, nil)
	}
	// Files that are gone from disk.
	eps, err := s.Episodes(ctx, id)
	if err != nil {
		return v, res, err
	}
	gone := map[int64]string{}
	for _, e := range eps {
		if e.File != nil {
			if _, err := os.Stat(e.File.Path); errors.Is(err, os.ErrNotExist) {
				gone[e.File.ID] = e.File.Path
			}
		}
	}
	for fid, path := range gone {
		if _, err := s.DB.W.ExecContext(ctx, `DELETE FROM media_files WHERE id = ?`, fid); err != nil {
			return v, res, err
		}
		res.MissingFiles++
		s.record(ctx, v.Ref(), timeline.TypeDeleted, "the file is no longer on disk: "+path, map[string]any{"path": path}, nil)
	}
	changed := map[string]any{}
	for k, pair := range map[string][2]string{"status": {before.Status, v.Status}, "title": {before.Title, v.Title}} {
		if pair[0] != pair[1] {
			changed[k] = pair[1]
		}
	}
	if len(changed) > 0 || res.Added+res.Renumbered+res.Deleted+res.Kept+res.MissingFiles > 0 {
		changed["added"], changed["renumbered"], changed["deleted"], changed["kept"], changed["missingFiles"] =
			res.Added, res.Renumbered, res.Deleted, res.Kept, res.MissingFiles
		s.record(ctx, v.Ref(), timeline.TypeRefreshed, fmt.Sprintf("metadata refreshed: %d episodes added, %d renumbered, %d removed, %d kept without the provider",
			res.Added, res.Renumbered, res.Deleted, res.Kept), changed, nil)
	}
	out, err := s.Get(ctx, id)
	return out, res, err
}

// RefreshAll refreshes every series (the scheduled job); errors are logged.
func (s *Service) RefreshAll(ctx context.Context) (int, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT item_id FROM series ORDER BY COALESCE(metadata_refreshed_at, 0)`)
	if err != nil {
		return 0, err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			_ = rows.Close()
			return 0, err
		}
		ids = append(ids, id)
	}
	_ = rows.Close()
	n := 0
	for _, id := range ids {
		if ctx.Err() != nil {
			return n, ctx.Err()
		}
		if _, _, err := s.Refresh(ctx, id); err != nil {
			s.log().Warn("refresh series", "id", id, "err", err)
			if errors.Is(err, metadata.ErrNoKey) || errors.Is(err, metadata.ErrInvalidKey) {
				return n, err
			}
			continue
		}
		n++
	}
	return n, nil
}
