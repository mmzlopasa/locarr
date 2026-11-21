// Package series is the TV library: TMDb (or TVDB) lookup, adding series
// with monitor options, seasons and episodes, the metadata refresh merge,
// episode matching for standard, daily and anime numbering (with a manual
// scene mapping), the wanted sources for episodes and season packs, the
// importer for single episodes, multi-episode files and season packs, an
// existing library import, renaming and the calendar. It plugs series into
// the shared library core like movies do.
package series

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/mmzlopasa/locarr/server/internal/db"
	"github.com/mmzlopasa/locarr/server/internal/library"
	"github.com/mmzlopasa/locarr/server/internal/parser"
	"github.com/mmzlopasa/locarr/server/internal/quality"
)

// Errors.
var (
	ErrNotFound        = errors.New("series not found")
	ErrSeasonNotFound  = errors.New("season not found")
	ErrEpisodeNotFound = errors.New("episode not found")
	ErrExists          = errors.New("this series is already in the library")
	ErrPathTaken       = errors.New("another library item uses this folder")
	ErrInvalid         = errors.New("invalid series")
	ErrHasFiles        = errors.New("the series has files; move or delete them first")
)

func invalid(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, a...))
}

// Series types.
const (
	TypeStandard = "standard"
	TypeDaily    = "daily"
	TypeAnime    = "anime"
)

// ValidType reports a known series type.
func ValidType(t string) bool { return t == TypeStandard || t == TypeDaily || t == TypeAnime }

// MediaInfo describes a file's streams, read from names (D-033).
type MediaInfo struct {
	Source        string   `json:"source"`
	Resolution    int      `json:"resolution,omitempty"`
	VideoCodec    string   `json:"videoCodec,omitempty"`
	BitDepth      int      `json:"bitDepth,omitempty"`
	HDR           []string `json:"hdr,omitempty"`
	AudioCodec    string   `json:"audioCodec,omitempty"`
	AudioChannels string   `json:"audioChannels,omitempty"`
	Atmos         bool     `json:"atmos,omitempty"`
	Languages     []string `json:"languages,omitempty"`
	Subtitles     []string `json:"subtitles,omitempty"`
}

// File is an episode file (one file may hold several episodes).
type File struct {
	ID            int64
	Path          string
	Size          int64
	Quality       string
	Revision      parser.Revision
	CustomFormats []string
	Score         int
	ReleaseGroup  string
	Languages     []string
	MediaInfo     MediaInfo
	SceneName     string
	DownloadID    *int64
	AddedAt       time.Time
}

// Series is a series in the library.
type Series struct {
	ID                int64
	TMDbID            int64
	TVDbID            int64
	IMDbID            string
	TVMazeID          int64
	Source            string
	Title             string
	SortTitle         string
	OriginalTitle     string
	OriginalLanguage  string
	AlternateTitles   []string
	Year              int
	Overview          string
	Status            string
	Network           string
	TimeZone          string
	AirTime           string
	Runtime           int
	Genres            []string
	Certification     string
	Rating            float64
	Votes             int
	Popularity        float64
	FirstAired        string
	LastAired         string
	SeriesType        string
	SeasonFolder      bool
	MonitorNewSeasons string
	Poster            string
	Backdrop          string
	Monitored         bool
	QualityProfileID  int64
	RootFolderID      *int64
	Path              string
	Tags              []string
	AddedAt           time.Time
	UpdatedAt         time.Time
	RefreshedAt       *time.Time
}

// Ref is the series' item reference.
func (s *Series) Ref() library.Ref { return library.SeriesRef(s.ID) }

// Season is one season.
type Season struct {
	ID        int64
	SeriesID  int64
	Number    int
	Monitored bool
	Title     string
	Overview  string
	Poster    string
	AirDate   string
}

// Ref is the season's item reference.
func (s *Season) Ref() library.Ref { return library.SeasonRef(s.ID) }

// Episode is one episode.
type Episode struct {
	ID            int64
	SeriesID      int64
	SeasonID      int64
	Season        int
	Number        int
	Absolute      int
	TMDbID        int64
	TVDbID        int64
	Title         string
	Overview      string
	AirDate       string
	AirDateUTC    *time.Time
	Runtime       int
	Monitored     bool
	FileID        *int64
	SceneSeason   *int
	SceneEpisode  *int
	SceneAbsolute *int
	Removed       bool
	AddedAt       time.Time
	File          *File
}

// Ref is the episode's item reference.
func (e *Episode) Ref() library.Ref { return library.EpisodeRef(e.ID) }

// Aired reports whether the episode has aired at now.
func (e *Episode) Aired(now time.Time) bool { return e.AirDateUTC != nil && !e.AirDateUTC.After(now) }

// ReleaseNumbers are the season and episode releases use (scene mapping, D-040).
func (e *Episode) ReleaseNumbers() (int, int) {
	s, n := e.Season, e.Number
	if e.SceneSeason != nil {
		s = *e.SceneSeason
	}
	if e.SceneEpisode != nil {
		n = *e.SceneEpisode
	}
	return s, n
}

// ReleaseAbsolute is the absolute number releases use.
func (e *Episode) ReleaseAbsolute() int {
	if e.SceneAbsolute != nil {
		return *e.SceneAbsolute
	}
	return e.Absolute
}

type scanner interface{ Scan(...any) error }

func jsonList(s string) []string {
	var out []string
	_ = json.Unmarshal([]byte(s), &out)
	if out == nil {
		out = []string{}
	}
	return out
}

func marshal(v any) string {
	b, _ := json.Marshal(v)
	if string(b) == "null" {
		return "[]"
	}
	return string(b)
}

const seriesColumns = `i.id, s.tmdb_id, s.tvdb_id, s.imdb_id, s.tvmaze_id, s.metadata_source, i.title, i.sort_title, s.original_title,
	s.original_language, s.alternate_titles, i.year, s.overview, s.status, s.network, s.time_zone, s.air_time, s.runtime, s.genres,
	s.certification, s.rating, s.votes, s.popularity, s.first_aired, s.last_aired, s.series_type, s.season_folder, s.monitor_new_seasons,
	s.poster, s.backdrop, i.monitored, i.quality_profile_id, i.root_folder_id, i.path, i.tags, i.added_at, i.updated_at, s.metadata_refreshed_at`

const seriesFrom = ` FROM media_items i JOIN series s ON s.item_id = i.id`

func scanSeries(row scanner) (Series, error) {
	var (
		s                 Series
		alt, genres, tags string
		added, updated    int64
		refreshed, rootID sql.NullInt64
	)
	err := row.Scan(&s.ID, &s.TMDbID, &s.TVDbID, &s.IMDbID, &s.TVMazeID, &s.Source, &s.Title, &s.SortTitle, &s.OriginalTitle,
		&s.OriginalLanguage, &alt, &s.Year, &s.Overview, &s.Status, &s.Network, &s.TimeZone, &s.AirTime, &s.Runtime, &genres,
		&s.Certification, &s.Rating, &s.Votes, &s.Popularity, &s.FirstAired, &s.LastAired, &s.SeriesType, &s.SeasonFolder, &s.MonitorNewSeasons,
		&s.Poster, &s.Backdrop, &s.Monitored, &s.QualityProfileID, &rootID, &s.Path, &tags, &added, &updated, &refreshed)
	if errors.Is(err, sql.ErrNoRows) {
		return s, ErrNotFound
	}
	if err != nil {
		return s, err
	}
	s.AlternateTitles, s.Genres, s.Tags = jsonList(alt), jsonList(genres), jsonList(tags)
	s.AddedAt, s.UpdatedAt, s.RefreshedAt = db.Time(added), db.Time(updated), db.NullTime(refreshed)
	if rootID.Valid {
		s.RootFolderID = &rootID.Int64
	}
	return s, nil
}

const episodeColumns = `e.id, e.series_id, e.season_id, e.season_number, e.episode_number, e.absolute_number, e.tmdb_id, e.tvdb_id,
	e.title, e.overview, e.air_date, e.air_date_utc, e.runtime, e.monitored, e.file_id, e.scene_season, e.scene_episode, e.scene_absolute,
	e.removed, e.added_at,
	f.id, f.path, f.size, f.quality, f.revision, f.custom_formats, f.score, f.release_group, f.languages, f.media_info, f.scene_name,
	f.download_id, f.added_at`

const episodeFrom = ` FROM episodes e LEFT JOIN media_files f ON f.id = e.file_id`

func scanEpisode(row scanner) (Episode, error) {
	var (
		e                          Episode
		air                        sql.NullInt64
		fileID                     sql.NullInt64
		ss, se, sa                 sql.NullInt64
		added                      int64
		fid, fsize, fscore, fadded sql.NullInt64
		fpath, fq, frev, fcf       sql.NullString
		fgroup, flang, fmi, fscene sql.NullString
		fdl                        sql.NullInt64
	)
	err := row.Scan(&e.ID, &e.SeriesID, &e.SeasonID, &e.Season, &e.Number, &e.Absolute, &e.TMDbID, &e.TVDbID, &e.Title, &e.Overview,
		&e.AirDate, &air, &e.Runtime, &e.Monitored, &fileID, &ss, &se, &sa, &e.Removed, &added,
		&fid, &fpath, &fsize, &fq, &frev, &fcf, &fscore, &fgroup, &flang, &fmi, &fscene, &fdl, &fadded)
	if errors.Is(err, sql.ErrNoRows) {
		return e, ErrEpisodeNotFound
	}
	if err != nil {
		return e, err
	}
	e.AirDateUTC, e.AddedAt = db.NullTime(air), db.Time(added)
	if fileID.Valid {
		e.FileID = &fileID.Int64
	}
	opt := func(v sql.NullInt64) *int {
		if !v.Valid {
			return nil
		}
		n := int(v.Int64)
		return &n
	}
	e.SceneSeason, e.SceneEpisode, e.SceneAbsolute = opt(ss), opt(se), opt(sa)
	if fid.Valid {
		f := &File{ID: fid.Int64, Path: fpath.String, Size: fsize.Int64, Quality: fq.String, Score: int(fscore.Int64),
			ReleaseGroup: fgroup.String, SceneName: fscene.String, AddedAt: db.Time(fadded.Int64)}
		_ = json.Unmarshal([]byte(frev.String), &f.Revision)
		f.CustomFormats, f.Languages = jsonList(fcf.String), jsonList(flang.String)
		_ = json.Unmarshal([]byte(fmi.String), &f.MediaInfo)
		if fdl.Valid {
			f.DownloadID = &fdl.Int64
		}
		e.File = f
	}
	return e, nil
}

func (s *Service) querySeries(ctx context.Context, q string, args ...any) ([]Series, error) {
	rows, err := s.DB.R.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Series{}
	for rows.Next() {
		v, err := scanSeries(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// Get returns one series.
func (s *Service) Get(ctx context.Context, id int64) (Series, error) {
	return scanSeries(s.DB.R.QueryRowContext(ctx, `SELECT `+seriesColumns+seriesFrom+` WHERE i.id = ? AND i.kind = 'series'`, id))
}

// ByProviderID returns the series with a TMDb or TVDB id.
func (s *Service) ByProviderID(ctx context.Context, tmdbID, tvdbID int64) (Series, error) {
	return scanSeries(s.DB.R.QueryRowContext(ctx, `SELECT `+seriesColumns+seriesFrom+`
		WHERE (s.tmdb_id = ? AND s.tmdb_id <> 0) OR (s.tvdb_id = ? AND s.tvdb_id <> 0) LIMIT 1`, tmdbID, tvdbID))
}

// Seasons returns a series' seasons in order.
func (s *Service) Seasons(ctx context.Context, seriesID int64) ([]Season, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT id, series_id, number, monitored, title, overview, poster, air_date FROM seasons
		WHERE series_id = ? ORDER BY number`, seriesID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Season{}
	for rows.Next() {
		var sn Season
		if err := rows.Scan(&sn.ID, &sn.SeriesID, &sn.Number, &sn.Monitored, &sn.Title, &sn.Overview, &sn.Poster, &sn.AirDate); err != nil {
			return nil, err
		}
		out = append(out, sn)
	}
	return out, rows.Err()
}

// SeasonByID returns one season.
func (s *Service) SeasonByID(ctx context.Context, id int64) (Season, error) {
	var sn Season
	err := s.DB.R.QueryRowContext(ctx, `SELECT id, series_id, number, monitored, title, overview, poster, air_date FROM seasons WHERE id = ?`, id).
		Scan(&sn.ID, &sn.SeriesID, &sn.Number, &sn.Monitored, &sn.Title, &sn.Overview, &sn.Poster, &sn.AirDate)
	if errors.Is(err, sql.ErrNoRows) {
		return sn, ErrSeasonNotFound
	}
	return sn, err
}

// SeasonByNumber returns a series' season.
func (s *Service) SeasonByNumber(ctx context.Context, seriesID int64, number int) (Season, error) {
	var id int64
	err := s.DB.R.QueryRowContext(ctx, `SELECT id FROM seasons WHERE series_id = ? AND number = ?`, seriesID, number).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return Season{}, ErrSeasonNotFound
	}
	if err != nil {
		return Season{}, err
	}
	return s.SeasonByID(ctx, id)
}

func (s *Service) queryEpisodes(ctx context.Context, q string, args ...any) ([]Episode, error) {
	rows, err := s.DB.R.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Episode{}
	for rows.Next() {
		e, err := scanEpisode(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Episodes returns every episode of a series in order (with files).
func (s *Service) Episodes(ctx context.Context, seriesID int64) ([]Episode, error) {
	return s.queryEpisodes(ctx, `SELECT `+episodeColumns+episodeFrom+` WHERE e.series_id = ? ORDER BY e.season_number, e.episode_number`, seriesID)
}

// Episode returns one episode.
func (s *Service) Episode(ctx context.Context, id int64) (Episode, error) {
	return scanEpisode(s.DB.R.QueryRowContext(ctx, `SELECT `+episodeColumns+episodeFrom+` WHERE e.id = ?`, id))
}

// insertSeries stores a new series row (in tx).
func insertSeries(ctx context.Context, q db.Querier, v *Series, now time.Time) error {
	err := q.QueryRowContext(ctx, `INSERT INTO media_items (kind, title, sort_title, year, monitored, quality_profile_id, root_folder_id,
		path, tags, added_at, updated_at) VALUES ('series', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		v.Title, v.SortTitle, v.Year, v.Monitored, v.QualityProfileID, v.RootFolderID, v.Path, marshal(v.Tags),
		db.Millis(now), db.Millis(now)).Scan(&v.ID)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return ErrPathTaken
		}
		return err
	}
	_, err = q.ExecContext(ctx, `INSERT INTO series (item_id, tmdb_id, tvdb_id, imdb_id, tvmaze_id, metadata_source, original_title,
		original_language, alternate_titles, overview, status, network, time_zone, air_time, runtime, genres, certification, rating, votes,
		popularity, first_aired, last_aired, series_type, season_folder, monitor_new_seasons, poster, backdrop, metadata_refreshed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		v.ID, v.TMDbID, v.TVDbID, v.IMDbID, v.TVMazeID, v.Source, v.OriginalTitle, v.OriginalLanguage, marshal(v.AlternateTitles), v.Overview,
		v.Status, v.Network, v.TimeZone, v.AirTime, v.Runtime, marshal(v.Genres), v.Certification, v.Rating, v.Votes, v.Popularity,
		v.FirstAired, v.LastAired, v.SeriesType, v.SeasonFolder, v.MonitorNewSeasons, v.Poster, v.Backdrop, db.Millis(now))
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
		return ErrExists
	}
	v.AddedAt, v.UpdatedAt = now.UTC(), now.UTC()
	return err
}

// saveSeriesMetadata writes the provider's facts.
func saveSeriesMetadata(ctx context.Context, q db.Querier, v *Series, now time.Time) error {
	if _, err := q.ExecContext(ctx, `UPDATE media_items SET title = ?, sort_title = ?, year = ?, updated_at = ? WHERE id = ?`,
		v.Title, v.SortTitle, v.Year, db.Millis(now), v.ID); err != nil {
		return err
	}
	_, err := q.ExecContext(ctx, `UPDATE series SET tmdb_id = ?, tvdb_id = ?, imdb_id = ?, tvmaze_id = ?, original_title = ?,
		original_language = ?, alternate_titles = ?, overview = ?, status = ?, network = ?, time_zone = ?, air_time = ?, runtime = ?,
		genres = ?, certification = ?, rating = ?, votes = ?, popularity = ?, first_aired = ?, last_aired = ?, poster = ?, backdrop = ?,
		metadata_refreshed_at = ? WHERE item_id = ?`,
		v.TMDbID, v.TVDbID, v.IMDbID, v.TVMazeID, v.OriginalTitle, v.OriginalLanguage, marshal(v.AlternateTitles), v.Overview, v.Status,
		v.Network, v.TimeZone, v.AirTime, v.Runtime, marshal(v.Genres), v.Certification, v.Rating, v.Votes, v.Popularity, v.FirstAired,
		v.LastAired, v.Poster, v.Backdrop, db.Millis(now), v.ID)
	return err
}

// saveFile inserts an episode file row and returns its id.
func saveFile(ctx context.Context, q db.Querier, seriesID int64, f *File, now time.Time) error {
	rev, _ := json.Marshal(f.Revision)
	mi, _ := json.Marshal(f.MediaInfo)
	if f.AddedAt.IsZero() {
		f.AddedAt = now.UTC()
	}
	if f.CustomFormats == nil {
		f.CustomFormats = []string{}
	}
	return q.QueryRowContext(ctx, `INSERT INTO media_files (item_id, path, size, quality, revision, custom_formats, score, release_group,
		edition, languages, media_info, scene_name, download_id, added_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, '', ?, ?, ?, ?, ?)
		ON CONFLICT (path) DO UPDATE SET size = excluded.size, quality = excluded.quality, revision = excluded.revision,
		custom_formats = excluded.custom_formats, score = excluded.score, release_group = excluded.release_group, languages = excluded.languages,
		media_info = excluded.media_info, scene_name = excluded.scene_name, download_id = excluded.download_id RETURNING id`,
		seriesID, f.Path, f.Size, f.Quality, string(rev), marshal(f.CustomFormats), f.Score, f.ReleaseGroup, marshal(f.Languages), string(mi),
		f.SceneName, f.DownloadID, db.Millis(f.AddedAt)).Scan(&f.ID)
}

// CutoffMet reports whether f satisfies the profile (no upgrade wanted).
func CutoffMet(p *quality.Profile, f *File) bool {
	if f == nil || p == nil {
		return false
	}
	if !p.UpgradesAllowed {
		return true
	}
	pos, _ := p.Position(f.Quality)
	if pos < 0 {
		return false
	}
	if cut := p.CutoffPosition(); cut >= 0 && pos > cut {
		return false
	}
	return p.CutoffFormatScore <= 0 || f.Score >= p.CutoffFormatScore
}
