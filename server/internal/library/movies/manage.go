package movies

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	lfs "github.com/mmzlopasa/locarr/server/internal/fs"
	"github.com/mmzlopasa/locarr/server/internal/library"
	"github.com/mmzlopasa/locarr/server/internal/library/importer"
	"github.com/mmzlopasa/locarr/server/internal/library/naming"
	"github.com/mmzlopasa/locarr/server/internal/metadata"
	"github.com/mmzlopasa/locarr/server/internal/parser"
	"github.com/mmzlopasa/locarr/server/internal/quality"
	"github.com/mmzlopasa/locarr/server/internal/settings"
	"github.com/mmzlopasa/locarr/server/internal/timeline"
)

var (
	tmdbInName = regexp.MustCompile(`(?i)[\[{(]?tmdb(?:id)?[-= ]?(\d{1,9})[\]})]?`)
	imdbInName = regexp.MustCompile(`(?i)\b(tt\d{5,10})\b`)
)

// idsFromName reads ids that folder names often carry: "{tmdb-603}", "[imdbid-tt0133093]".
func idsFromName(name string) (tmdb int64, imdb string) {
	if m := tmdbInName.FindStringSubmatch(name); m != nil {
		tmdb, _ = strconv.ParseInt(m[1], 10, 64)
	}
	if m := imdbInName.FindStringSubmatch(name); m != nil {
		imdb = strings.ToLower(m[1])
	}
	return
}

// cleanName drops id markers so the parser sees only title and year.
func cleanName(name string) string {
	name = tmdbInName.ReplaceAllString(name, "")
	name = regexp.MustCompile(`(?i)[\[{(]?imdb(?:id)?[-= ]?tt\d+[\]})]?`).ReplaceAllString(name, "")
	return strings.TrimSpace(name)
}

// matchLocal finds a library movie for a file or folder name.
func (s *Service) matchLocal(ctx context.Context, names ...string) (*Movie, error) {
	for _, n := range names {
		tmdb, imdb := idsFromName(n)
		if tmdb > 0 {
			if m, err := s.ByTMDb(ctx, tmdb); err == nil {
				return &m, nil
			}
		}
		if imdb != "" {
			var id int64
			if err := s.DB.R.QueryRowContext(ctx, `SELECT item_id FROM movies WHERE imdb_id = ?`, imdb).Scan(&id); err == nil {
				m, err := s.Get(ctx, id)
				return &m, err
			}
		}
	}
	for _, n := range names {
		p := parser.Parse(cleanName(n))
		if p.Title == "" {
			continue
		}
		rows, err := s.DB.R.QueryContext(ctx, `SELECT i.id, i.year, i.title, m.original_title, m.alternate_titles FROM media_items i
			JOIN movies m ON m.item_id = i.id WHERE i.kind = 'movie'`)
		if err != nil {
			return nil, err
		}
		want := normTitle(p.Title)
		var found int64
		for rows.Next() {
			var (
				id                int64
				year              int
				title, orig, alts string
			)
			if err := rows.Scan(&id, &year, &title, &orig, &alts); err != nil {
				_ = rows.Close()
				return nil, err
			}
			if p.Year > 0 && year > 0 && (p.Year < year-1 || p.Year > year+1) {
				continue
			}
			for _, t := range append([]string{title, orig}, jsonList(alts)...) {
				if normTitle(t) == want {
					found = id
				}
			}
		}
		_ = rows.Close()
		if found > 0 {
			m, err := s.Get(ctx, found)
			return &m, err
		}
	}
	return nil, nil
}

// ImportCandidate is one file of a manual import preview.
type ImportCandidate struct {
	Path       string
	Rel        string
	Size       int64
	Skip       string
	MovieID    int64
	MovieTitle string
	MovieYear  int
	Quality    quality.Quality
	Release    parser.Release
	Rejection  *ImportError
	Sidecars   []string
}

// PreviewImport lists the files of folder (or one file) with the movie each
// would go to and whether it would be imported. movieID forces a movie.
func (s *Service) PreviewImport(ctx context.Context, folder string, movieID int64) ([]ImportCandidate, error) {
	p, err := lfs.MustExist(s.DataRoots, folder)
	if err != nil {
		return nil, err
	}
	extras, _ := settings.Get(ctx, s.Settings, library.ExtraExtensions)
	disc, err := importer.Discover(p, extras)
	if err != nil {
		return nil, err
	}
	out := []ImportCandidate{}
	for _, v := range disc.Videos {
		c := ImportCandidate{Path: v.Path, Rel: v.Rel, Size: v.Size}
		var m *Movie
		if movieID > 0 {
			mm, err := s.Get(ctx, movieID)
			if err != nil {
				return nil, err
			}
			m = &mm
		} else if m, err = s.matchLocal(ctx, filepath.Base(v.Path), filepath.Base(filepath.Dir(v.Path))); err != nil {
			return nil, err
		}
		dir := filepath.Dir(v.Path)
		for _, sc := range disc.Sidecars {
			if filepath.Dir(sc.Path) == dir || strings.HasPrefix(sc.Path, dir+string(filepath.Separator)) {
				c.Sidecars = append(c.Sidecars, sc.Path)
			}
		}
		if m == nil {
			c.Release = parser.ParsePath(v.Path)
			c.Quality = quality.Identify(&c.Release, quality.Movies)
			c.Rejection = &ImportError{Code: RejectNoMovie, Message: "no movie in the library matches this file; choose one"}
			out = append(out, c)
			continue
		}
		c.MovieID, c.MovieTitle, c.MovieYear = m.ID, m.Title, m.Year
		a, err := s.analyse(ctx, m, v.Path, "")
		if err != nil {
			return nil, err
		}
		c.Release, c.Quality = a.Release, a.Quality
		if m.File != nil {
			if same, _ := sameFile(v.Path, m.File.Path); same {
				c.Rejection = &ImportError{Code: RejectExists, Message: "this is already the movie's file"}
			}
		}
		if c.Rejection == nil {
			if up, why, err := s.isUpgrade(ctx, m, a); err != nil {
				return nil, err
			} else if !up {
				c.Rejection = &ImportError{Code: RejectNotUpgrade, Message: why}
			}
		}
		out = append(out, c)
	}
	for _, sk := range disc.Skipped {
		out = append(out, ImportCandidate{Path: sk.Path, Rel: sk.Rel, Size: sk.Size, Skip: sk.Skip})
	}
	return out, nil
}

// ImportFiles imports chosen files; each result carries its own error.
func (s *Service) ImportFiles(ctx context.Context, reqs []ImportRequest) ([]ImportResult, []error) {
	res := make([]ImportResult, len(reqs))
	errs := make([]error, len(reqs))
	for i, r := range reqs {
		res[i], errs[i] = s.ImportFile(ctx, r)
		if errs[i] != nil {
			if m, err := s.Get(ctx, r.MovieID); err == nil {
				s.record(ctx, &m, timeline.TypeImportFailed, "manual import of "+filepath.Base(r.Path)+" failed: "+errs[i].Error(),
					map[string]any{"path": r.Path}, nil)
			}
		}
	}
	return res, errs
}

// RenameItem is one planned rename.
type RenameItem struct {
	MovieID int64
	Title   string
	From    string
	To      string
}

// RenamePreview lists files whose name differs from the naming template.
func (s *Service) RenamePreview(ctx context.Context, ids []int64) ([]RenameItem, error) {
	var list []Movie
	if len(ids) == 0 {
		all, err := s.All(ctx)
		if err != nil {
			return nil, err
		}
		list = all
	} else {
		for _, id := range ids {
			m, err := s.Get(ctx, id)
			if err != nil {
				return nil, err
			}
			list = append(list, m)
		}
	}
	out := []RenameItem{}
	for i := range list {
		m := &list[i]
		if m.File == nil {
			continue
		}
		a, err := s.analyse(ctx, m, m.File.Path, m.File.SceneName)
		if err != nil {
			return nil, err
		}
		// The stored facts win over a fresh parse of an already renamed file.
		if q, ok := quality.ByID(m.File.Quality); ok {
			a.Quality = q
		}
		a.Formats = m.File.CustomFormats
		tmpl, _ := settings.Get(ctx, s.Settings, library.MovieFileFormat)
		colon, _ := settings.Get(ctx, s.Settings, library.ColonReplacement)
		rel := a.Release
		name, err := naming.Render(tmpl, naming.MovieValues(s.info(m), &naming.FileInfo{Release: &rel, Quality: a.Quality, CustomFormats: a.Formats}),
			naming.Options{Colon: colon})
		if err != nil {
			return nil, err
		}
		to := filepath.Join(filepath.Dir(m.File.Path), naming.WithExtension(name, filepath.Ext(m.File.Path)))
		if to != m.File.Path {
			out = append(out, RenameItem{MovieID: m.ID, Title: m.Title, From: m.File.Path, To: to})
		}
	}
	return out, nil
}

// Rename applies the template to the movies' files (same folder, never over
// an existing file). Sidecars with the same base name are renamed too.
func (s *Service) Rename(ctx context.Context, ids []int64) ([]RenameItem, error) {
	plan, err := s.RenamePreview(ctx, ids)
	if err != nil {
		return nil, err
	}
	done := []RenameItem{}
	for _, it := range plan {
		from, err := lfs.MustExist(s.DataRoots, it.From)
		if err != nil {
			return done, err
		}
		to, err := lfs.SafePath(s.DataRoots, it.To)
		if err != nil {
			return done, err
		}
		if _, err := os.Lstat(to); err == nil {
			return done, fmt.Errorf("cannot rename %s: %s exists", from, to)
		}
		if err := os.Rename(from, to); err != nil {
			return done, err
		}
		oldBase := strings.TrimSuffix(filepath.Base(from), filepath.Ext(from))
		newBase := strings.TrimSuffix(filepath.Base(to), filepath.Ext(to))
		if entries, err := os.ReadDir(filepath.Dir(from)); err == nil {
			for _, e := range entries {
				if e.IsDir() || !strings.HasPrefix(e.Name(), oldBase+".") {
					continue
				}
				target := filepath.Join(filepath.Dir(to), newBase+e.Name()[len(oldBase):])
				if _, err := os.Lstat(target); err != nil {
					_ = os.Rename(filepath.Join(filepath.Dir(from), e.Name()), target)
				}
			}
		}
		if _, err := s.DB.W.ExecContext(ctx, `UPDATE media_files SET path = ? WHERE item_id = ?`, to, it.MovieID); err != nil {
			return done, err
		}
		if m, err := s.Get(ctx, it.MovieID); err == nil {
			s.record(ctx, &m, timeline.TypeRenamed, fmt.Sprintf("renamed %s to %s", filepath.Base(from), filepath.Base(to)),
				map[string]any{"from": from, "to": to}, nil)
			s.notify(ctx, &m, "renamed", to)
		}
		done = append(done, it)
	}
	return done, nil
}

// ExistingFolder is a folder in a root folder that is not in the library yet.
type ExistingFolder struct {
	Path       string
	Name       string
	Title      string
	Year       int
	Video      string
	VideoSize  int64
	Match      *metadata.MovieSummary
	Candidates []metadata.MovieSummary
	Error      string
}

// maxScan bounds one existing-library scan.
const maxScan = 500

// ScanRootFolder lists the root folder's sub-folders that no movie uses, with
// the best provider match for each (by id in the name, else title and year).
func (s *Service) ScanRootFolder(ctx context.Context, rootID int64) ([]ExistingFolder, error) {
	root, err := s.rootFolder(ctx, rootID)
	if err != nil {
		return nil, err
	}
	rp, err := lfs.MustExist(s.DataRoots, root.Path)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(rp)
	if err != nil {
		return nil, err
	}
	used := map[string]bool{}
	rows, err := s.DB.R.QueryContext(ctx, `SELECT path FROM media_items`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var p string
		if err := rows.Scan(&p); err == nil {
			used[p] = true
		}
	}
	_ = rows.Close()
	bin, _ := s.Recycle.Folder(ctx)
	extras, _ := settings.Get(ctx, s.Settings, library.ExtraExtensions)
	out := []ExistingFolder{}
	for _, e := range entries {
		if len(out) >= maxScan || ctx.Err() != nil {
			break
		}
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		p := filepath.Join(rp, e.Name())
		if used[p] || p == bin {
			continue
		}
		f := ExistingFolder{Path: p, Name: e.Name()}
		pr := parser.Parse(cleanName(e.Name()))
		f.Title, f.Year = pr.Title, pr.Year
		if d, err := importer.Discover(p, extras); err == nil {
			if v, ok := d.MainVideo(); ok {
				f.Video, f.VideoSize = v.Path, v.Size
			}
		}
		s.matchProvider(ctx, &f)
		out = append(out, f)
	}
	sort.SliceStable(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, nil
}

func (s *Service) matchProvider(ctx context.Context, f *ExistingFolder) {
	tmdb, imdb := idsFromName(f.Name)
	if tmdb == 0 && imdb != "" {
		if id, err := s.Meta.FindByIMDb(ctx, imdb); err == nil {
			tmdb = id
		}
	}
	if tmdb > 0 {
		if m, err := s.Meta.Movie(ctx, tmdb); err == nil {
			sm := m.MovieSummary
			f.Match, f.Candidates = &sm, []metadata.MovieSummary{sm}
			return
		}
	}
	if f.Title == "" {
		return
	}
	list, err := s.Meta.SearchMovies(ctx, f.Title, f.Year)
	if err != nil {
		f.Error = err.Error()
		return
	}
	if len(list) == 0 && f.Year > 0 {
		list, _ = s.Meta.SearchMovies(ctx, f.Title, 0)
	}
	if len(list) > 5 {
		list = list[:5]
	}
	f.Candidates = list
	for i := range list {
		if normTitle(list[i].Title) == normTitle(f.Title) && (f.Year == 0 || list[i].Year == 0 || abs(list[i].Year-f.Year) <= 1) {
			f.Match = &list[i]
			return
		}
	}
	if len(list) == 1 {
		f.Match = &list[0]
	}
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// ExistingImport adds a movie for an existing folder.
type ExistingImport struct {
	Path string
	AddInput
}

// ImportExisting adds movies for existing folders and registers the video
// already in each folder as the movie's file (nothing is moved or renamed).
func (s *Service) ImportExisting(ctx context.Context, list []ExistingImport) ([]Movie, []error) {
	out := make([]Movie, len(list))
	errs := make([]error, len(list))
	extras, _ := settings.Get(ctx, s.Settings, library.ExtraExtensions)
	for i, it := range list {
		in := it.AddInput
		in.Path = it.Path
		search := in.SearchOnAdd
		in.SearchOnAdd = false
		m, err := s.Add(ctx, in)
		if err != nil {
			errs[i] = err
			continue
		}
		if d, err := importer.Discover(m.Path, extras); err == nil {
			if v, ok := d.MainVideo(); ok {
				if err := s.registerFile(ctx, &m, v); err != nil {
					errs[i] = err
				}
			}
		}
		if mm, err := s.Get(ctx, m.ID); err == nil {
			m = mm
		}
		out[i] = m
		if search && s.SearchRequested != nil {
			s.SearchRequested(ctx, m.Ref(), "added")
		}
	}
	return out, errs
}

// registerFile records a file that is already in place.
func (s *Service) registerFile(ctx context.Context, m *Movie, v importer.File) error {
	a, err := s.analyse(ctx, m, v.Path, "")
	if err != nil {
		return err
	}
	rel := a.Release
	rev := rel.Revision
	if rev.Version == 0 {
		rev.Version = 1
	}
	f := &File{Path: v.Path, Size: v.Size, Quality: a.Quality.ID, Revision: rev, CustomFormats: a.Formats, Score: a.Score,
		ReleaseGroup: rel.Group, Edition: strings.Join(rel.Editions, " "), Languages: rel.Languages, MediaInfo: mediaInfo(&rel)}
	if f.CustomFormats == nil {
		f.CustomFormats = []string{}
	}
	if err := s.DB.Tx(ctx, func(tx *sql.Tx) error { return saveFile(ctx, tx, m.ID, f, s.now()) }); err != nil {
		return err
	}
	s.record(ctx, m, timeline.TypeFileFound, fmt.Sprintf("found %s (%s) in the existing folder", filepath.Base(v.Path), a.Quality.Name),
		map[string]any{"path": v.Path, "quality": a.Quality.Name, "size": v.Size}, nil)
	return nil
}

// IsImportError returns the import rejection in err, if any.
func IsImportError(err error) (*ImportError, bool) {
	var ie *ImportError
	ok := errors.As(err, &ie)
	return ie, ok
}
