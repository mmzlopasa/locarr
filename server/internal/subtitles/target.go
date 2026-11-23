package subtitles

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/mmzlopasa/locarr/server/internal/library"
	"github.com/mmzlopasa/locarr/server/internal/parser"
	"github.com/mmzlopasa/locarr/server/internal/subtitles/providers"
)

// Target is a movie or an episode with its file: what a subtitle is for.
type Target struct {
	Ref    library.Ref
	Kind   string // movie, episode
	ItemID int64  // media_items id (the movie or the series)
	Title  string // movie title, or "Series S01E02"
	// Movie or series facts.
	ItemTitle string
	Year      int
	IMDbID    string
	TMDbID    int64
	// Episode facts.
	EpisodeTitle string
	Season       int
	Episode      int
	Absolute     int
	Anime        bool
	// The file.
	FileID    int64
	Path      string
	Size      int64
	SceneName string
	Group     string
	AddedAt   int64 // millis
}

// Release is what the file's name (or the release it came from) says.
func (t *Target) Release() parser.Release {
	rel := parser.ParsePath(t.Path)
	if t.SceneName != "" {
		named := parser.Parse(t.SceneName)
		if named.Confidence >= rel.Confidence || rel.Obfuscated {
			rel = named
		}
	}
	if rel.Group == "" {
		rel.Group = t.Group
	}
	return rel
}

// ReleaseName is the name subtitles are matched against.
func (t *Target) ReleaseName() string {
	if t.SceneName != "" {
		return t.SceneName
	}
	return strings.TrimSuffix(filepath.Base(t.Path), filepath.Ext(t.Path))
}

// Query is the provider query for languages.
func (t *Target) Query(langs []string, hash string) providers.Query {
	q := providers.Query{Kind: t.Kind, Languages: langs, Title: t.ItemTitle, Year: t.Year, FileName: t.ReleaseName(),
		FileSize: t.Size, Hash: hash, Anime: t.Anime}
	if t.Kind == providers.KindEpisode {
		q.SeriesIMDbID, q.SeriesTMDbID, q.Season, q.Episode, q.Absolute = t.IMDbID, t.TMDbID, t.Season, t.Episode, t.Absolute
	} else {
		q.IMDbID, q.TMDbID = t.IMDbID, t.TMDbID
	}
	return q
}

const movieTarget = `SELECT mi.id, mi.title, mi.year, mv.imdb_id, mv.tmdb_id, f.id, f.path, f.size, f.scene_name, f.release_group, f.added_at
	FROM movies mv JOIN media_items mi ON mi.id = mv.item_id JOIN media_files f ON f.item_id = mi.id`

const episodeTarget = `SELECT e.id, mi.id, mi.title, mi.year, sr.imdb_id, sr.tmdb_id, sr.series_type, e.title, e.season_number, e.episode_number,
	e.absolute_number, f.id, f.path, f.size, f.scene_name, f.release_group, f.added_at
	FROM episodes e JOIN media_items mi ON mi.id = e.series_id JOIN series sr ON sr.item_id = mi.id JOIN media_files f ON f.id = e.file_id`

func scanMovie(row interface{ Scan(...any) error }) (Target, error) {
	t := Target{Kind: providers.KindMovie}
	err := row.Scan(&t.ItemID, &t.ItemTitle, &t.Year, &t.IMDbID, &t.TMDbID, &t.FileID, &t.Path, &t.Size, &t.SceneName, &t.Group, &t.AddedAt)
	t.Ref, t.Title = library.MovieRef(t.ItemID), t.ItemTitle
	return t, err
}

func scanEpisode(row interface{ Scan(...any) error }) (Target, error) {
	t := Target{Kind: providers.KindEpisode}
	var (
		id   int64
		kind string
	)
	err := row.Scan(&id, &t.ItemID, &t.ItemTitle, &t.Year, &t.IMDbID, &t.TMDbID, &kind, &t.EpisodeTitle, &t.Season, &t.Episode,
		&t.Absolute, &t.FileID, &t.Path, &t.Size, &t.SceneName, &t.Group, &t.AddedAt)
	t.Ref, t.Anime = library.EpisodeRef(id), kind == "anime"
	t.Title = t.ItemTitle + " " + episodeCode(t.Season, t.Episode)
	return t, err
}

func episodeCode(s, e int) string { return fmt.Sprintf("S%02dE%02d", s, e) }

// Target resolves a movie or episode ref with a file.
func (s *Service) Target(ctx context.Context, ref library.Ref) (Target, error) {
	var (
		t   Target
		err error
	)
	switch ref.Kind {
	case library.RefMovie:
		t, err = scanMovie(s.DB.R.QueryRowContext(ctx, movieTarget+` WHERE mi.id = ? ORDER BY f.id DESC LIMIT 1`, ref.ID))
	case library.RefEpisode:
		t, err = scanEpisode(s.DB.R.QueryRowContext(ctx, episodeTarget+` WHERE e.id = ?`, ref.ID))
	default:
		return t, invalid("subtitles are for movies and episodes, not %s", ref.Kind)
	}
	if errors.Is(err, sql.ErrNoRows) {
		if s.exists(ctx, ref) {
			return t, ErrNoFile
		}
		return t, ErrNotFound
	}
	return t, err
}

func (s *Service) exists(ctx context.Context, ref library.Ref) bool {
	var one int
	q := `SELECT 1 FROM movies WHERE item_id = ?`
	if ref.Kind == library.RefEpisode {
		q = `SELECT 1 FROM episodes WHERE id = ?`
	}
	return s.DB.R.QueryRowContext(ctx, q, ref.ID).Scan(&one) == nil
}

// ItemTargets returns the targets of a library item: a movie, or every
// episode with a file of a series.
func (s *Service) ItemTargets(ctx context.Context, itemID int64) ([]Target, error) {
	var kind string
	if err := s.DB.R.QueryRowContext(ctx, `SELECT kind FROM media_items WHERE id = ?`, itemID).Scan(&kind); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	switch kind {
	case "movie":
		t, err := s.Target(ctx, library.MovieRef(itemID))
		if errors.Is(err, ErrNoFile) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		return []Target{t}, nil
	case "series":
		return s.queryTargets(ctx, episodeTarget+` WHERE e.series_id = ? ORDER BY e.season_number, e.episode_number`, scanEpisode, itemID)
	}
	return nil, nil
}

// FileTargets returns the targets of one media file.
func (s *Service) FileTargets(ctx context.Context, fileID int64) ([]Target, error) {
	mv, err := s.queryTargets(ctx, movieTarget+` WHERE f.id = ? AND NOT EXISTS (SELECT 1 FROM episodes x WHERE x.file_id = f.id)`, scanMovie, fileID)
	if err != nil || len(mv) > 0 {
		return mv, err
	}
	return s.queryTargets(ctx, episodeTarget+` WHERE f.id = ?`, scanEpisode, fileID)
}

// AllTargets calls fn for every movie and episode with a file.
func (s *Service) AllTargets(ctx context.Context, fn func(Target) error) error {
	mv, err := s.queryTargets(ctx, movieTarget+` ORDER BY mi.id`, scanMovie)
	if err != nil {
		return err
	}
	for _, t := range mv {
		if err := fn(t); err != nil {
			return err
		}
	}
	eps, err := s.queryTargets(ctx, episodeTarget+` ORDER BY e.id`, scanEpisode)
	if err != nil {
		return err
	}
	for _, t := range eps {
		if err := fn(t); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) queryTargets(ctx context.Context, q string, scan func(interface{ Scan(...any) error }) (Target, error), args ...any) ([]Target, error) {
	rows, err := s.DB.R.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Target
	for rows.Next() {
		t, err := scan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
