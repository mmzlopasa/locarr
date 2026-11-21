// Package movies is the movie library: TMDb lookup, adding and editing
// movies, availability, collections, import lists, the movie importer,
// renaming and importing an existing library. It plugs movies into the
// shared library core (wanted orchestration, timeline, importer, renamer,
// recycle bin).
package movies

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
)

// Errors.
var (
	ErrNotFound     = errors.New("movie not found")
	ErrExists       = errors.New("this movie is already in the library")
	ErrPathTaken    = errors.New("another library item uses this folder")
	ErrInvalid      = errors.New("invalid movie")
	ErrHasFile      = errors.New("the movie has a file; move or delete it first")
	ErrFileNotFound = errors.New("movie file not found")
)

func invalid(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, a...))
}

// MediaInfo describes a file's streams. Without ffprobe in the image it is
// read from the release or file name (Source = "name"), see D-033.
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

// File is a movie's file.
type File struct {
	ID            int64
	Path          string
	Size          int64
	Quality       string
	Revision      parser.Revision
	CustomFormats []string
	Score         int
	ReleaseGroup  string
	Edition       string
	Languages     []string
	MediaInfo     MediaInfo
	SceneName     string
	DownloadID    *int64
	AddedAt       time.Time
}

// Movie is a movie in the library.
type Movie struct {
	ID                  int64
	TMDbID              int64
	IMDbID              string
	Title               string
	SortTitle           string
	OriginalTitle       string
	OriginalLanguage    string
	AlternateTitles     []string
	Year                int
	Overview            string
	Runtime             int
	Status              string
	Studio              string
	Genres              []string
	Certification       string
	Rating              float64
	Votes               int
	Popularity          float64
	InCinemas           string
	DigitalRelease      string
	PhysicalRelease     string
	CollectionID        int64
	CollectionName      string
	Poster              string
	Backdrop            string
	Monitored           bool
	QualityProfileID    int64
	RootFolderID        *int64
	Path                string
	MinimumAvailability library.Availability
	Tags                []string
	AddedByListID       *int64
	AddedAt             time.Time
	UpdatedAt           time.Time
	RefreshedAt         *time.Time
	File                *File
}

// Ref is the movie's item reference.
func (m *Movie) Ref() library.Ref { return library.MovieRef(m.ID) }

// Dates are the movie's release dates.
func (m *Movie) Dates() library.ReleaseDates {
	return library.ReleaseDates{InCinemas: library.ParseDate(m.InCinemas), Digital: library.ParseDate(m.DigitalRelease),
		Physical: library.ParseDate(m.PhysicalRelease), Released: strings.EqualFold(m.Status, "Released")}
}

const movieColumns = `i.id, m.tmdb_id, m.imdb_id, i.title, i.sort_title, m.original_title, m.original_language, m.alternate_titles,
	i.year, m.overview, m.runtime, m.status, m.studio, m.genres, m.certification, m.rating, m.votes, m.popularity,
	m.in_cinemas, m.digital_release, m.physical_release, m.collection_tmdb_id, m.collection_name, m.poster, m.backdrop,
	i.monitored, i.quality_profile_id, i.root_folder_id, i.path, m.minimum_availability, i.tags, m.added_by_list_id,
	i.added_at, i.updated_at, m.metadata_refreshed_at,
	f.id, f.path, f.size, f.quality, f.revision, f.custom_formats, f.score, f.release_group, f.edition, f.languages, f.media_info,
	f.scene_name, f.download_id, f.added_at`

const movieFrom = ` FROM media_items i JOIN movies m ON m.item_id = i.id LEFT JOIN media_files f ON f.item_id = i.id`

type scanner interface{ Scan(...any) error }

func jsonList(s string) []string {
	var out []string
	_ = json.Unmarshal([]byte(s), &out)
	if out == nil {
		out = []string{}
	}
	return out
}

func scanMovie(row scanner) (Movie, error) {
	var (
		m                            Movie
		alt, genres, tags            string
		added, updated               int64
		refreshed                    sql.NullInt64
		rootID, listID               sql.NullInt64
		avail                        string
		fid, fsize, fscore, fadded   sql.NullInt64
		fpath, fq, frev, fcf, fgroup sql.NullString
		fed, flang, fmi, fscene      sql.NullString
		fdl                          sql.NullInt64
	)
	err := row.Scan(&m.ID, &m.TMDbID, &m.IMDbID, &m.Title, &m.SortTitle, &m.OriginalTitle, &m.OriginalLanguage, &alt,
		&m.Year, &m.Overview, &m.Runtime, &m.Status, &m.Studio, &genres, &m.Certification, &m.Rating, &m.Votes, &m.Popularity,
		&m.InCinemas, &m.DigitalRelease, &m.PhysicalRelease, &m.CollectionID, &m.CollectionName, &m.Poster, &m.Backdrop,
		&m.Monitored, &m.QualityProfileID, &rootID, &m.Path, &avail, &tags, &listID, &added, &updated, &refreshed,
		&fid, &fpath, &fsize, &fq, &frev, &fcf, &fscore, &fgroup, &fed, &flang, &fmi, &fscene, &fdl, &fadded)
	if errors.Is(err, sql.ErrNoRows) {
		return m, ErrNotFound
	}
	if err != nil {
		return m, err
	}
	m.AlternateTitles, m.Genres, m.Tags = jsonList(alt), jsonList(genres), jsonList(tags)
	m.MinimumAvailability = library.Availability(avail)
	m.AddedAt, m.UpdatedAt, m.RefreshedAt = db.Time(added), db.Time(updated), db.NullTime(refreshed)
	if rootID.Valid {
		m.RootFolderID = &rootID.Int64
	}
	if listID.Valid {
		m.AddedByListID = &listID.Int64
	}
	if fid.Valid {
		f := &File{ID: fid.Int64, Path: fpath.String, Size: fsize.Int64, Quality: fq.String, Score: int(fscore.Int64),
			ReleaseGroup: fgroup.String, Edition: fed.String, SceneName: fscene.String, AddedAt: db.Time(fadded.Int64)}
		_ = json.Unmarshal([]byte(frev.String), &f.Revision)
		f.CustomFormats, f.Languages = jsonList(fcf.String), jsonList(flang.String)
		_ = json.Unmarshal([]byte(fmi.String), &f.MediaInfo)
		if fdl.Valid {
			f.DownloadID = &fdl.Int64
		}
		m.File = f
	}
	return m, nil
}

// Get returns one movie.
func (s *Service) Get(ctx context.Context, id int64) (Movie, error) {
	return scanMovie(s.DB.R.QueryRowContext(ctx, `SELECT `+movieColumns+movieFrom+` WHERE i.id = ? AND i.kind = 'movie'`, id))
}

// ByTMDb returns the movie with a TMDb id.
func (s *Service) ByTMDb(ctx context.Context, tmdbID int64) (Movie, error) {
	return scanMovie(s.DB.R.QueryRowContext(ctx, `SELECT `+movieColumns+movieFrom+` WHERE m.tmdb_id = ?`, tmdbID))
}

// All returns every movie (for matching and scheduled work).
func (s *Service) All(ctx context.Context) ([]Movie, error) {
	return s.query(ctx, `SELECT `+movieColumns+movieFrom+` ORDER BY i.sort_title, i.id`)
}

func (s *Service) query(ctx context.Context, q string, args ...any) ([]Movie, error) {
	rows, err := s.DB.R.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Movie{}
	for rows.Next() {
		m, err := scanMovie(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

func marshal(v any) string {
	b, _ := json.Marshal(v)
	if string(b) == "null" {
		return "[]"
	}
	return string(b)
}

// insert stores a new movie (in tx).
func insert(ctx context.Context, q db.Querier, m *Movie, now time.Time) error {
	err := q.QueryRowContext(ctx, `INSERT INTO media_items (kind, title, sort_title, year, monitored, quality_profile_id, root_folder_id,
		path, tags, added_at, updated_at) VALUES ('movie', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		m.Title, m.SortTitle, m.Year, m.Monitored, m.QualityProfileID, m.RootFolderID, m.Path, marshal(m.Tags),
		db.Millis(now), db.Millis(now)).Scan(&m.ID)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return ErrPathTaken
		}
		return err
	}
	_, err = q.ExecContext(ctx, `INSERT INTO movies (item_id, tmdb_id, imdb_id, original_title, original_language, alternate_titles, overview,
		runtime, status, studio, genres, certification, rating, votes, popularity, in_cinemas, digital_release, physical_release,
		collection_tmdb_id, collection_name, poster, backdrop, minimum_availability, added_by_list_id, metadata_refreshed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		m.ID, m.TMDbID, m.IMDbID, m.OriginalTitle, m.OriginalLanguage, marshal(m.AlternateTitles), m.Overview, m.Runtime, m.Status,
		m.Studio, marshal(m.Genres), m.Certification, m.Rating, m.Votes, m.Popularity, m.InCinemas, m.DigitalRelease, m.PhysicalRelease,
		m.CollectionID, m.CollectionName, m.Poster, m.Backdrop, string(m.MinimumAvailability), m.AddedByListID, db.Millis(now))
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
		return ErrExists
	}
	m.AddedAt, m.UpdatedAt = now.UTC(), now.UTC()
	return err
}

// saveMetadata writes provider facts of m.
func saveMetadata(ctx context.Context, q db.Querier, m *Movie, now time.Time) error {
	if _, err := q.ExecContext(ctx, `UPDATE media_items SET title = ?, sort_title = ?, year = ?, updated_at = ? WHERE id = ?`,
		m.Title, m.SortTitle, m.Year, db.Millis(now), m.ID); err != nil {
		return err
	}
	_, err := q.ExecContext(ctx, `UPDATE movies SET imdb_id = ?, original_title = ?, original_language = ?, alternate_titles = ?,
		overview = ?, runtime = ?, status = ?, studio = ?, genres = ?, certification = ?, rating = ?, votes = ?, popularity = ?,
		in_cinemas = ?, digital_release = ?, physical_release = ?, collection_tmdb_id = ?, collection_name = ?, poster = ?, backdrop = ?,
		metadata_refreshed_at = ? WHERE item_id = ?`,
		m.IMDbID, m.OriginalTitle, m.OriginalLanguage, marshal(m.AlternateTitles), m.Overview, m.Runtime, m.Status, m.Studio,
		marshal(m.Genres), m.Certification, m.Rating, m.Votes, m.Popularity, m.InCinemas, m.DigitalRelease, m.PhysicalRelease,
		m.CollectionID, m.CollectionName, m.Poster, m.Backdrop, db.Millis(now), m.ID)
	return err
}

// saveFile replaces the movie's file row.
func saveFile(ctx context.Context, q db.Querier, movieID int64, f *File, now time.Time) error {
	if _, err := q.ExecContext(ctx, `DELETE FROM media_files WHERE item_id = ?`, movieID); err != nil {
		return err
	}
	if f == nil {
		return nil
	}
	rev, _ := json.Marshal(f.Revision)
	mi, _ := json.Marshal(f.MediaInfo)
	if f.AddedAt.IsZero() {
		f.AddedAt = now.UTC()
	}
	return q.QueryRowContext(ctx, `INSERT INTO media_files (item_id, path, size, quality, revision, custom_formats, score, release_group,
		edition, languages, media_info, scene_name, download_id, added_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		movieID, f.Path, f.Size, f.Quality, string(rev), marshal(f.CustomFormats), f.Score, f.ReleaseGroup, f.Edition,
		marshal(f.Languages), string(mi), f.SceneName, f.DownloadID, db.Millis(f.AddedAt)).Scan(&f.ID)
}
