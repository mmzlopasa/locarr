package movies

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

// Service is the movie library.
type Service struct {
	DB        *db.DB
	Settings  *settings.Store
	Meta      metadata.MovieProvider
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
	// SearchRequested asks the wanted orchestration to search a movie (set by
	// the app; nil = nothing happens).
	SearchRequested func(ctx context.Context, ref library.Ref, reason string)
	// Imported is told about every imported movie file (subtitles, P7).
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

func (s *Service) record(ctx context.Context, m *Movie, typ, msg string, data any, downloadID *int64) {
	if s.Timeline == nil {
		return
	}
	if err := s.Timeline.Record(ctx, m.Ref().String(), typ, msg, data, downloadID); err != nil {
		s.log().Warn("timeline", "err", err)
	}
}

// LookupResult is a provider result, marked when it is in the library.
type LookupResult struct {
	metadata.MovieSummary
	MovieID *int64
}

var (
	tmdbTerm = regexp.MustCompile(`^(?i)tmdb[: ]?(\d+)$`)
	imdbTerm = regexp.MustCompile(`^(?i)(?:imdb[: ]?)?(tt\d{4,10})$`)
	yearTail = regexp.MustCompile(`^(.*?)[\s(]+((?:19|20)\d{2})\)?$`)
)

// Lookup searches the provider by text ("Title", "Title 2008"), TMDb id
// ("tmdb:603") or IMDb id ("tt0133093").
func (s *Service) Lookup(ctx context.Context, term string) ([]LookupResult, error) {
	term = strings.TrimSpace(term)
	if term == "" {
		return []LookupResult{}, nil
	}
	var list []metadata.MovieSummary
	switch {
	case tmdbTerm.MatchString(term):
		id, _ := strconv.ParseInt(tmdbTerm.FindStringSubmatch(term)[1], 10, 64)
		m, err := s.Meta.Movie(ctx, id)
		if errors.Is(err, metadata.ErrNotFound) {
			return []LookupResult{}, nil
		}
		if err != nil {
			return nil, err
		}
		list = append(list, m.MovieSummary)
	case imdbTerm.MatchString(term):
		id, err := s.Meta.FindByIMDb(ctx, imdbTerm.FindStringSubmatch(term)[1])
		if errors.Is(err, metadata.ErrNotFound) {
			return []LookupResult{}, nil
		}
		if err != nil {
			return nil, err
		}
		m, err := s.Meta.Movie(ctx, id)
		if err != nil {
			return nil, err
		}
		list = append(list, m.MovieSummary)
	default:
		q, y := term, 0
		if mm := yearTail.FindStringSubmatch(term); mm != nil && strings.TrimSpace(mm[1]) != "" {
			q = strings.TrimSpace(mm[1])
			y, _ = strconv.Atoi(mm[2])
		}
		var err error
		if list, err = s.Meta.SearchMovies(ctx, q, y); err != nil {
			return nil, err
		}
		if len(list) == 0 && y > 0 {
			if list, err = s.Meta.SearchMovies(ctx, term, 0); err != nil {
				return nil, err
			}
		}
	}
	return s.mark(ctx, list)
}

func (s *Service) mark(ctx context.Context, list []metadata.MovieSummary) ([]LookupResult, error) {
	out := make([]LookupResult, 0, len(list))
	for _, r := range list {
		lr := LookupResult{MovieSummary: r}
		var id int64
		err := s.DB.R.QueryRowContext(ctx, `SELECT item_id FROM movies WHERE tmdb_id = ?`, r.TMDbID).Scan(&id)
		if err == nil {
			lr.MovieID = &id
		} else if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		out = append(out, lr)
	}
	return out, nil
}

// AddInput adds a movie.
type AddInput struct {
	TMDbID              int64
	RootFolderID        int64
	QualityProfileID    int64
	MinimumAvailability library.Availability
	Monitored           bool
	Tags                []string
	SearchOnAdd         bool
	AddedByListID       *int64
	// Path overrides the folder (absolute, inside the root folder); "" = from the naming template.
	Path string
}

func (s *Service) rootFolder(ctx context.Context, id int64) (rootfolders.Folder, error) {
	list, err := s.Roots.List(ctx)
	if err != nil {
		return rootfolders.Folder{}, err
	}
	for _, f := range list {
		if f.ID == id {
			if f.Kind != rootfolders.KindMovies {
				return f, invalid("root folder %s is for %s, not movies", f.Path, f.Kind)
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
	if p.Kind != quality.Movies {
		return invalid("quality profile %s is for %s, not movies", p.Name, p.Kind)
	}
	return nil
}

func fromMetadata(m *Movie, md metadata.Movie) {
	m.TMDbID, m.IMDbID, m.Title, m.OriginalTitle, m.OriginalLanguage = md.TMDbID, md.IMDbID, md.Title, md.OriginalTitle, md.OriginalLanguage
	m.SortTitle = naming.SortTitle(md.Title)
	m.AlternateTitles, m.Year, m.Overview, m.Runtime, m.Status = md.AlternateTitles, md.Year, md.Overview, md.Runtime, md.Status
	m.Studio, m.Genres, m.Certification, m.Rating, m.Votes, m.Popularity = md.Studio, md.Genres, md.Certification, md.Rating, md.Votes, md.Popularity
	m.InCinemas, m.DigitalRelease, m.PhysicalRelease = md.InCinemas, md.DigitalRelease, md.PhysicalRelease
	m.CollectionID, m.CollectionName, m.Poster, m.Backdrop = md.CollectionID, md.CollectionName, md.Poster, md.Backdrop
	if m.Year == 0 {
		for _, d := range []string{md.InCinemas, md.DigitalRelease, md.PhysicalRelease} {
			if len(d) >= 4 {
				m.Year, _ = strconv.Atoi(d[:4])
				break
			}
		}
	}
	if m.AlternateTitles == nil {
		m.AlternateTitles = []string{}
	}
	if m.Genres == nil {
		m.Genres = []string{}
	}
}

// folderName renders the movie folder name.
func (s *Service) folderName(ctx context.Context, m *Movie) (string, error) {
	tmpl, err := settings.Get(ctx, s.Settings, library.MovieFolderFormat)
	if err != nil {
		return "", err
	}
	colon, _ := settings.Get(ctx, s.Settings, library.ColonReplacement)
	return naming.Render(tmpl, naming.MovieValues(s.info(m), nil), naming.Options{Colon: colon})
}

func (s *Service) info(m *Movie) naming.MovieInfo {
	return naming.MovieInfo{Title: m.Title, OriginalTitle: m.OriginalTitle, Year: m.Year, TMDbID: m.TMDbID, IMDbID: m.IMDbID, Collection: m.CollectionName}
}

// Add adds a movie from the provider.
func (s *Service) Add(ctx context.Context, in AddInput) (Movie, error) {
	if in.TMDbID <= 0 {
		return Movie{}, invalid("tmdbId is required")
	}
	if in.MinimumAvailability == "" {
		in.MinimumAvailability = library.Released
	}
	if !library.ValidAvailability(in.MinimumAvailability) {
		return Movie{}, invalid("unknown minimum availability %q", in.MinimumAvailability)
	}
	if existing, err := s.ByTMDb(ctx, in.TMDbID); err == nil {
		return existing, ErrExists
	}
	root, err := s.rootFolder(ctx, in.RootFolderID)
	if err != nil {
		return Movie{}, err
	}
	if err := s.checkProfile(ctx, in.QualityProfileID); err != nil {
		return Movie{}, err
	}
	md, err := s.Meta.Movie(ctx, in.TMDbID)
	if err != nil {
		return Movie{}, err
	}
	m := Movie{Monitored: in.Monitored, QualityProfileID: in.QualityProfileID, RootFolderID: &root.ID,
		MinimumAvailability: in.MinimumAvailability, Tags: quality.NormTags(in.Tags), AddedByListID: in.AddedByListID}
	if m.Tags == nil {
		m.Tags = []string{}
	}
	fromMetadata(&m, md)
	path := in.Path
	if path == "" {
		name, err := s.folderName(ctx, &m)
		if err != nil {
			return Movie{}, err
		}
		path = filepath.Join(root.Path, name)
	}
	if m.Path, err = s.libraryPath(root.Path, path); err != nil {
		return Movie{}, err
	}
	now := s.now()
	if err := s.DB.Tx(ctx, func(tx *sql.Tx) error { return insert(ctx, tx, &m, now) }); err != nil {
		return Movie{}, err
	}
	msg := fmt.Sprintf("added %s (%d) to %s", m.Title, m.Year, m.Path)
	s.record(ctx, &m, timeline.TypeAdded, msg, map[string]any{"path": m.Path, "monitored": m.Monitored,
		"minimumAvailability": m.MinimumAvailability, "profileId": m.QualityProfileID, "listId": in.AddedByListID}, nil)
	if in.SearchOnAdd && m.Monitored && s.SearchRequested != nil {
		s.SearchRequested(ctx, m.Ref(), "added")
	}
	return s.Get(ctx, m.ID)
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
		return "", invalid("the movie folder must be inside the root folder %s", root)
	}
	return sp, nil
}

// Update changes how a movie is managed.
type Update struct {
	Monitored           *bool
	QualityProfileID    *int64
	MinimumAvailability *library.Availability
	Tags                *[]string
	RootFolderID        *int64
	Path                *string
}

// Edit applies an update.
func (s *Service) Edit(ctx context.Context, id int64, u Update) (Movie, error) {
	m, err := s.Get(ctx, id)
	if err != nil {
		return m, err
	}
	changes := map[string]any{}
	if u.Monitored != nil && *u.Monitored != m.Monitored {
		m.Monitored, changes["monitored"] = *u.Monitored, *u.Monitored
	}
	if u.QualityProfileID != nil && *u.QualityProfileID != m.QualityProfileID {
		if err := s.checkProfile(ctx, *u.QualityProfileID); err != nil {
			return m, err
		}
		m.QualityProfileID, changes["profileId"] = *u.QualityProfileID, *u.QualityProfileID
	}
	if u.MinimumAvailability != nil && *u.MinimumAvailability != m.MinimumAvailability {
		if !library.ValidAvailability(*u.MinimumAvailability) {
			return m, invalid("unknown minimum availability %q", *u.MinimumAvailability)
		}
		m.MinimumAvailability, changes["minimumAvailability"] = *u.MinimumAvailability, *u.MinimumAvailability
	}
	if u.Tags != nil {
		t := quality.NormTags(*u.Tags)
		if t == nil {
			t = []string{}
		}
		m.Tags, changes["tags"] = t, t
	}
	if u.RootFolderID != nil || u.Path != nil {
		rootID := 0
		if m.RootFolderID != nil {
			rootID = int(*m.RootFolderID)
		}
		if u.RootFolderID != nil {
			rootID = int(*u.RootFolderID)
		}
		root, err := s.rootFolder(ctx, int64(rootID))
		if err != nil {
			return m, err
		}
		path := ""
		if u.Path != nil && *u.Path != "" {
			path = *u.Path
		} else {
			name, err := s.folderName(ctx, &m)
			if err != nil {
				return m, err
			}
			path = filepath.Join(root.Path, name)
		}
		sp, err := s.libraryPath(root.Path, path)
		if err != nil {
			return m, err
		}
		if sp != m.Path {
			if m.File != nil {
				return m, ErrHasFile
			}
			m.Path, m.RootFolderID, changes["path"] = sp, &root.ID, sp
		}
	}
	if len(changes) == 0 {
		return m, nil
	}
	_, err = s.DB.W.ExecContext(ctx, `UPDATE media_items SET monitored = ?, quality_profile_id = ?, tags = ?, path = ?, root_folder_id = ?,
		updated_at = ? WHERE id = ?`, m.Monitored, m.QualityProfileID, marshal(m.Tags), m.Path, m.RootFolderID, db.Millis(s.now()), m.ID)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return m, ErrPathTaken
		}
		return m, err
	}
	if _, err := s.DB.W.ExecContext(ctx, `UPDATE movies SET minimum_availability = ? WHERE item_id = ?`, string(m.MinimumAvailability), m.ID); err != nil {
		return m, err
	}
	s.record(ctx, &m, timeline.TypeEdited, "settings changed", changes, nil)
	return s.Get(ctx, id)
}

// DeleteOptions control Delete.
type DeleteOptions struct {
	// DeleteFiles moves the movie folder to the recycle bin.
	DeleteFiles bool
	// Exclude keeps import lists from adding it again.
	Exclude bool
}

// Delete removes a movie from the library. Files are never deleted: with
// DeleteFiles they go to the recycle bin.
func (s *Service) Delete(ctx context.Context, id int64, o DeleteOptions) error {
	m, err := s.Get(ctx, id)
	if err != nil {
		return err
	}
	msg := "removed from the library; files left in place"
	if o.DeleteFiles {
		if _, err := os.Stat(m.Path); err == nil {
			e, err := s.Recycle.Recycle(ctx, m.Path, m.Ref().String(), "movie deleted")
			if err != nil {
				return fmt.Errorf("move %s to the recycle bin: %w", m.Path, err)
			}
			msg = "removed from the library; folder moved to the recycle bin (" + e.RecycledPath + ")"
		} else {
			msg = "removed from the library; the folder did not exist"
		}
	}
	if o.Exclude {
		if _, err := s.DB.W.ExecContext(ctx, `INSERT INTO import_exclusions (tmdb_id, title, year, created_at) VALUES (?, ?, ?, ?)
			ON CONFLICT DO NOTHING`, m.TMDbID, m.Title, m.Year, db.Millis(s.now())); err != nil {
			return err
		}
	}
	s.record(ctx, &m, timeline.TypeDeleted, msg, map[string]any{"deleteFiles": o.DeleteFiles, "exclude": o.Exclude}, nil)
	if _, err := s.DB.W.ExecContext(ctx, `DELETE FROM media_items WHERE id = ?`, id); err != nil {
		return err
	}
	_, _ = s.DB.W.ExecContext(ctx, `DELETE FROM search_state WHERE item_ref = ?`, m.Ref().String())
	_, _ = s.DB.W.ExecContext(ctx, `DELETE FROM pending_releases WHERE item_ref = ?`, m.Ref().String())
	return nil
}

// Refresh reloads a movie's metadata (skipping the cache) and checks that its
// file is still there.
func (s *Service) Refresh(ctx context.Context, id int64) (Movie, error) {
	m, err := s.Get(ctx, id)
	if err != nil {
		return m, err
	}
	md, err := s.Meta.Movie(metadata.Fresh(ctx), m.TMDbID)
	if err != nil {
		return m, err
	}
	before := m
	fromMetadata(&m, md)
	if err := saveMetadata(ctx, s.DB.W, &m, s.now()); err != nil {
		return m, err
	}
	changed := map[string]any{}
	for k, pair := range map[string][2]string{"inCinemas": {before.InCinemas, m.InCinemas}, "digitalRelease": {before.DigitalRelease, m.DigitalRelease},
		"physicalRelease": {before.PhysicalRelease, m.PhysicalRelease}, "status": {before.Status, m.Status}, "title": {before.Title, m.Title}} {
		if pair[0] != pair[1] {
			changed[k] = pair[1]
		}
	}
	if len(changed) > 0 {
		s.record(ctx, &m, timeline.TypeRefreshed, "metadata changed", changed, nil)
	}
	if m.File != nil {
		if _, err := os.Stat(m.File.Path); errors.Is(err, os.ErrNotExist) {
			if err := saveFile(ctx, s.DB.W, m.ID, nil, s.now()); err != nil {
				return m, err
			}
			s.record(ctx, &m, timeline.TypeDeleted, "the file is no longer on disk: "+m.File.Path, map[string]any{"path": m.File.Path}, nil)
		}
	}
	return s.Get(ctx, id)
}

// RefreshAll refreshes every movie (the scheduled job); errors are logged.
func (s *Service) RefreshAll(ctx context.Context) (int, error) {
	rows, err := s.DB.R.QueryContext(ctx, `SELECT item_id FROM movies ORDER BY COALESCE(metadata_refreshed_at, 0)`)
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
		if _, err := s.Refresh(ctx, id); err != nil {
			s.log().Warn("refresh movie", "id", id, "err", err)
			if errors.Is(err, metadata.ErrNoKey) || errors.Is(err, metadata.ErrInvalidKey) {
				return n, err
			}
			continue
		}
		n++
	}
	return n, nil
}

// Available reports whether m reached its minimum availability, and when it does.
func (s *Service) Available(ctx context.Context, m *Movie) (bool, *time.Time) {
	return s.availableWith(m, s.availabilityDelay(ctx))
}

func (s *Service) availabilityDelay(ctx context.Context) time.Duration {
	days, _ := settings.Get(ctx, s.Settings, library.AvailabilityDelay)
	return time.Duration(days) * 24 * time.Hour
}

func (s *Service) availableWith(m *Movie, delay time.Duration) (bool, *time.Time) {
	at := library.AvailableFrom(m.Dates(), m.MinimumAvailability, delay)
	ok := library.IsAvailable(m.Dates(), m.MinimumAvailability, delay, s.now())
	if at.IsZero() || m.MinimumAvailability == library.Announced {
		return ok, nil
	}
	return ok, &at
}
