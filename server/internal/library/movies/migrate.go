package movies

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
	"github.com/mmzlopasa/locarr/server/internal/db"
	lfs "github.com/mmzlopasa/locarr/server/internal/fs"
	"github.com/mmzlopasa/locarr/server/internal/library"
	"github.com/mmzlopasa/locarr/server/internal/library/naming"
	"github.com/mmzlopasa/locarr/server/internal/parser"
	"github.com/mmzlopasa/locarr/server/internal/quality"
	"github.com/mmzlopasa/locarr/server/internal/rootfolders"
	"github.com/mmzlopasa/locarr/server/internal/timeline"
)

// Imported is a movie read from another application (P10). It is added as
// it is there, without asking the metadata provider; its metadata is left
// unrefreshed so the next refresh fills in the rest.
type Imported struct {
	TMDbID              int64
	IMDbID              string
	Title               string
	OriginalTitle       string
	OriginalLanguage    string
	Year                int
	Overview            string
	Runtime             int
	Status              string
	Studio              string
	Genres              []string
	Certification       string
	InCinemas           string
	DigitalRelease      string
	PhysicalRelease     string
	CollectionID        int64
	CollectionName      string
	Poster              string
	Monitored           bool
	QualityProfileID    int64
	RootFolderID        int64
	MinimumAvailability library.Availability
	Tags                []string
	// Path is the movie folder as Locarr sees it (inside the root folder).
	Path string
	File *ImportedFile
	// Note is written to the movie's timeline ("imported from …").
	Note string
	Data map[string]any
}

// ImportedFile is a file the other application already had for the item.
type ImportedFile struct {
	Path         string
	Size         int64
	Quality      string // Locarr quality id ("" = from the name)
	ReleaseGroup string
	SceneName    string
	Edition      string
	Languages    []string
	Revision     int
	AddedAt      time.Time
}

// ImportOutcome is what happened to one imported movie.
type ImportOutcome struct {
	ID         int64
	Created    bool
	FileLinked bool
	// FileMissing: the source had a file that is not on disk here.
	FileMissing bool
	Err         error
}

// importBatch is how many movies share one transaction.
const importBatch = 250

// ImportMovies adds movies from another application in batches: a movie
// already in the library (same TMDb id) is merged (its file is linked when it
// has none), never duplicated. Files stay where they are.
func (s *Service) ImportMovies(ctx context.Context, list []Imported) []ImportOutcome {
	out := make([]ImportOutcome, len(list))
	roots, err := s.Roots.List(ctx)
	if err != nil {
		for i := range out {
			out[i].Err = err
		}
		return out
	}
	scorer := newFileScorer(ctx, s)
	for start := 0; start < len(list); start += importBatch {
		end := min(start+importBatch, len(list))
		if ctx.Err() != nil {
			for i := start; i < len(list); i++ {
				out[i].Err = ctx.Err()
			}
			return out
		}
		now := s.now()
		err := s.DB.Tx(ctx, func(tx *sql.Tx) error {
			for i := start; i < end; i++ {
				out[i] = s.importOne(ctx, tx, &list[i], roots, scorer, now)
			}
			return nil
		})
		if err != nil {
			for i := start; i < end; i++ {
				if out[i].Err == nil {
					out[i] = ImportOutcome{Err: err}
				}
			}
		}
	}
	return out
}

func (s *Service) importOne(ctx context.Context, tx *sql.Tx, in *Imported, roots []rootfolders.Folder, sc *fileScorer, now time.Time) ImportOutcome {
	var o ImportOutcome
	if in.TMDbID <= 0 {
		o.Err = invalid("tmdbId is required")
		return o
	}
	var existing int64
	var hasFile bool
	err := tx.QueryRowContext(ctx, `SELECT m.item_id, EXISTS (SELECT 1 FROM media_files f WHERE f.item_id = m.item_id)
		FROM movies m WHERE m.tmdb_id = ?`, in.TMDbID).Scan(&existing, &hasFile)
	switch {
	case err == nil:
		o.ID = existing
		if !hasFile && in.File != nil {
			o.FileLinked, o.FileMissing, o.Err = s.linkImportedFile(ctx, tx, existing, in, sc, now)
		}
		return o
	case !errors.Is(err, sql.ErrNoRows):
		o.Err = err
		return o
	}
	var root *rootfolders.Folder
	for i := range roots {
		if roots[i].ID == in.RootFolderID {
			root = &roots[i]
		}
	}
	if root == nil {
		o.Err = rootfolders.ErrNotFound
		return o
	}
	if root.Kind != rootfolders.KindMovies {
		o.Err = invalid("root folder %s is for %s, not movies", root.Path, root.Kind)
		return o
	}
	if in.MinimumAvailability == "" || !library.ValidAvailability(in.MinimumAvailability) {
		in.MinimumAvailability = library.Released
	}
	path, err := s.libraryPath(root.Path, in.Path)
	if err != nil {
		o.Err = err
		return o
	}
	m := Movie{TMDbID: in.TMDbID, IMDbID: in.IMDbID, Title: strings.TrimSpace(in.Title), OriginalTitle: in.OriginalTitle,
		OriginalLanguage: in.OriginalLanguage, AlternateTitles: []string{}, Year: in.Year, Overview: in.Overview, Runtime: in.Runtime,
		Status: in.Status, Studio: in.Studio, Genres: in.Genres, Certification: in.Certification, InCinemas: in.InCinemas,
		DigitalRelease: in.DigitalRelease, PhysicalRelease: in.PhysicalRelease, CollectionID: in.CollectionID,
		CollectionName: in.CollectionName, Poster: in.Poster, Monitored: in.Monitored, QualityProfileID: in.QualityProfileID,
		RootFolderID: &root.ID, Path: path, MinimumAvailability: in.MinimumAvailability, Tags: quality.NormTags(in.Tags)}
	if m.Title == "" {
		m.Title = fmt.Sprintf("TMDb %d", in.TMDbID)
	}
	if m.Genres == nil {
		m.Genres = []string{}
	}
	m.SortTitle = naming.SortTitle(m.Title)
	if _, err := tx.ExecContext(ctx, `SAVEPOINT movie_import`); err != nil {
		o.Err = err
		return o
	}
	if err := insert(ctx, tx, &m, now); err != nil {
		_, _ = tx.ExecContext(ctx, `ROLLBACK TO movie_import`)
		o.Err = err
		return o
	}
	// Not refreshed yet: the scheduled refresh (and the import's own
	// refresh job) picks these first.
	if _, err := tx.ExecContext(ctx, `UPDATE movies SET metadata_refreshed_at = NULL WHERE item_id = ?`, m.ID); err != nil {
		_, _ = tx.ExecContext(ctx, `ROLLBACK TO movie_import`)
		o.Err = err
		return o
	}
	_, _ = tx.ExecContext(ctx, `RELEASE movie_import`)
	o.ID, o.Created = m.ID, true
	note := in.Note
	if note == "" {
		note = "imported from another application"
	}
	data := in.Data
	if data == nil {
		data = map[string]any{}
	}
	data["path"], data["monitored"] = m.Path, m.Monitored
	if err := timeline.RecordAt(ctx, tx, m.Ref().String(), timeline.TypeMigrated, note, data, now); err != nil {
		o.Err = err
		return o
	}
	if in.File != nil {
		o.FileLinked, o.FileMissing, o.Err = s.linkImportedFile(ctx, tx, m.ID, in, sc, now)
	}
	return o
}

// linkImportedFile records the source's file in place when it is on disk.
func (s *Service) linkImportedFile(ctx context.Context, tx *sql.Tx, id int64, in *Imported, sc *fileScorer, now time.Time) (bool, bool, error) {
	f := in.File
	p, err := lfs.SafePath(s.DataRoots, f.Path)
	if err != nil {
		return false, true, nil
	}
	st, err := os.Stat(p)
	if err != nil || st.IsDir() {
		return false, true, nil
	}
	rel := parser.ParsePath(p)
	if f.SceneName != "" {
		if named := parser.Parse(f.SceneName); named.Confidence >= rel.Confidence {
			named.Extension = rel.Extension
			rel = named
		}
	}
	q := quality.Identify(&rel, quality.Movies)
	if x, ok := quality.ByID(f.Quality); ok && !x.Music {
		q = x
	}
	rev := rel.Revision
	if f.Revision > 0 {
		rev.Version = f.Revision
	}
	if rev.Version == 0 {
		rev.Version = 1
	}
	group := f.ReleaseGroup
	if group == "" {
		group = rel.Group
	}
	langs := f.Languages
	if len(langs) == 0 {
		langs = rel.Languages
	}
	edition := f.Edition
	if edition == "" {
		edition = strings.Join(rel.Editions, " ")
	}
	formats, score := sc.score(ctx, in.QualityProfileID, filepath.Base(p), &rel, q, in.OriginalLanguage)
	file := &File{Path: p, Size: st.Size(), Quality: q.ID, Revision: rev, CustomFormats: formats, Score: score, ReleaseGroup: group,
		Edition: edition, Languages: langs, MediaInfo: mediaInfo(&rel), SceneName: f.SceneName, AddedAt: f.AddedAt}
	if file.Languages == nil {
		file.Languages = []string{}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM media_files WHERE path = ? AND item_id <> ?`, p, id); err != nil {
		return false, false, err
	}
	if err := saveFile(ctx, tx, id, file, now); err != nil {
		return false, false, err
	}
	msg := fmt.Sprintf("linked %s (%s) in place", filepath.Base(p), q.Name)
	if err := timeline.RecordAt(ctx, tx, library.MovieRef(id).String(), timeline.TypeFileFound, msg,
		map[string]any{"path": p, "quality": q.Name, "size": st.Size()}, now); err != nil {
		return false, false, err
	}
	return true, false, nil
}

// fileScorer caches compiled custom formats and profiles while a batch runs.
type fileScorer struct {
	s        *Service
	compiled []*customformats.Compiled
	profiles map[int64]*quality.Profile
}

func newFileScorer(ctx context.Context, s *Service) *fileScorer {
	sc := &fileScorer{s: s, profiles: map[int64]*quality.Profile{}}
	if s.Formats != nil {
		if list, err := s.Formats.List(ctx); err == nil {
			sc.compiled, _ = customformats.CompileAll(list)
		}
	}
	return sc
}

func (sc *fileScorer) score(ctx context.Context, profileID int64, title string, rel *parser.Release, q quality.Quality, lang string) ([]string, int) {
	p, ok := sc.profiles[profileID]
	if !ok {
		if pp, err := sc.s.Quality.Profile(ctx, profileID); err == nil {
			p = &pp
		}
		sc.profiles[profileID] = p
	}
	if p == nil || len(sc.compiled) == 0 {
		return []string{}, 0
	}
	matches, score := customformats.Score(sc.compiled, p, customformats.Input{Title: title, Release: rel, Quality: q, OriginalLanguage: lang})
	names := make([]string, 0, len(matches))
	for _, m := range matches {
		names = append(names, m.Name)
	}
	return names, score
}

// AddExclusion keeps import lists from adding a movie (merged on TMDb id).
func (s *Service) AddExclusion(ctx context.Context, e Exclusion) (bool, error) {
	if e.TMDbID <= 0 {
		return false, invalid("tmdbId is required")
	}
	res, err := s.DB.W.ExecContext(ctx, `INSERT INTO import_exclusions (tmdb_id, title, year, created_at) VALUES (?, ?, ?, ?)
		ON CONFLICT DO NOTHING`, e.TMDbID, e.Title, e.Year, db.Millis(s.now()))
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// NeedsRefresh lists the movies whose metadata was never refreshed (imported
// from another application), oldest first.
func (s *Service) NeedsRefresh(ctx context.Context, ids []int64) ([]int64, error) {
	return pendingRefresh(ctx, s.DB, `SELECT item_id FROM movies WHERE metadata_refreshed_at IS NULL`, ids)
}

func pendingRefresh(ctx context.Context, d *db.DB, q string, ids []int64) ([]int64, error) {
	rows, err := d.R.QueryContext(ctx, q)
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
