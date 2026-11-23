package series

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"github.com/mmzlopasa/locarr/server/internal/mediaserver"
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
	tvdbInName = regexp.MustCompile(`(?i)[\[{(]?tvdb(?:id)?[-= ]?(\d{1,9})[\]})]?`)
	tmdbInName = regexp.MustCompile(`(?i)[\[{(]?tmdb(?:id)?[-= ]?(\d{1,9})[\]})]?`)
	imdbInName = regexp.MustCompile(`(?i)[\[{(]?(?:imdb(?:id)?[-= ]?)?(tt\d{5,10})[\]})]?`)
)

// idsFromName reads ids folder names often carry: "{tvdb-121361}", "[tmdbid-1399]", "tt0944947".
func idsFromName(name string) (tvdb, tmdb int64, imdb string) {
	if m := tvdbInName.FindStringSubmatch(name); m != nil {
		tvdb, _ = strconv.ParseInt(m[1], 10, 64)
	}
	if m := tmdbInName.FindStringSubmatch(name); m != nil {
		tmdb, _ = strconv.ParseInt(m[1], 10, 64)
	}
	if m := imdbInName.FindStringSubmatch(name); m != nil {
		imdb = strings.ToLower(m[1])
	}
	return
}

func cleanName(name string) string {
	for _, re := range []*regexp.Regexp{tvdbInName, tmdbInName, imdbInName} {
		name = re.ReplaceAllString(name, "")
	}
	return strings.TrimSpace(name)
}

// ExistingFolder is a folder in a root folder that is not in the library yet.
type ExistingFolder struct {
	Path       string
	Name       string
	Title      string
	Year       int
	Videos     int
	Match      *metadata.SeriesSummary
	Candidates []metadata.SeriesSummary
	Error      string
}

const maxScan = 2000

// ScanRootFolder lists the folders of a series root folder that are not in
// the library, with how many videos they hold and a provider match.
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
		if rows.Scan(&p) == nil {
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
		if f.Title == "" {
			f.Title = cleanName(e.Name())
		}
		if d, err := importer.Discover(p, extras); err == nil {
			f.Videos = len(d.Videos)
		}
		s.matchProvider(ctx, &f)
		out = append(out, f)
	}
	sort.SliceStable(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, nil
}

func (s *Service) matchProvider(ctx context.Context, f *ExistingFolder) {
	source := s.defaultSource(ctx)
	p, err := s.provider(source)
	if err != nil {
		f.Error = err.Error()
		return
	}
	tvdb, tmdb, imdb := idsFromName(f.Name)
	var pid int64
	switch {
	case source == "tmdb" && tmdb > 0:
		pid = tmdb
	case source == "tvdb" && tvdb > 0:
		pid = tvdb
	case tvdb > 0:
		pid, _ = p.FindSeries(ctx, "tvdb", strconv.FormatInt(tvdb, 10))
	case tmdb > 0:
		pid, _ = p.FindSeries(ctx, "tmdb", strconv.FormatInt(tmdb, 10))
	case imdb != "":
		pid, _ = p.FindSeries(ctx, "imdb", imdb)
	}
	if pid > 0 {
		if md, err := p.Series(ctx, pid); err == nil {
			sm := md.SeriesSummary
			f.Match, f.Candidates = &sm, []metadata.SeriesSummary{sm}
			return
		}
	}
	if f.Title == "" {
		return
	}
	list, err := p.SearchSeries(ctx, f.Title, f.Year)
	if err != nil {
		f.Error = err.Error()
		return
	}
	if len(list) == 0 && f.Year > 0 {
		list, _ = p.SearchSeries(ctx, f.Title, 0)
	}
	if len(list) > 5 {
		list = list[:5]
	}
	f.Candidates = list
	for i := range list {
		d := list[i].Year - f.Year
		if normTitle(list[i].Title) == normTitle(f.Title) && (f.Year == 0 || list[i].Year == 0 || (d >= -1 && d <= 1)) {
			f.Match = &list[i]
			return
		}
	}
	if len(list) == 1 {
		f.Match = &list[0]
	}
}

// ExistingImport adds one existing folder as a series.
type ExistingImport struct {
	Path string
	AddInput
}

// ExistingResult is the outcome for one folder.
type ExistingResult struct {
	Series Series
	Found  int // episode files registered
	Err    error
}

// ImportExisting adds series for existing folders and registers the episode
// files already in them (nothing is moved or renamed).
func (s *Service) ImportExisting(ctx context.Context, list []ExistingImport) []ExistingResult {
	out := make([]ExistingResult, 0, len(list))
	for _, it := range list {
		in := it.AddInput
		in.Path, in.SearchOnAdd = it.Path, false
		search := it.SearchOnAdd
		v, err := s.Add(ctx, in)
		if err != nil {
			out = append(out, ExistingResult{Err: fmt.Errorf("%s: %w", it.Path, err)})
			continue
		}
		n, err := s.RegisterExisting(ctx, v.ID)
		out = append(out, ExistingResult{Series: v, Found: n, Err: err})
		if search && v.Monitored && s.SearchRequested != nil {
			s.SearchRequested(ctx, v.Ref(), "added")
		}
	}
	return out
}

// RegisterExisting finds the episode files in a series folder and links
// them to their episodes where an episode has none yet.
func (s *Service) RegisterExisting(ctx context.Context, seriesID int64) (int, error) {
	vw, err := s.loadView(ctx, seriesID)
	if err != nil {
		return 0, err
	}
	v := &vw.series
	if _, err := os.Stat(v.Path); err != nil {
		return 0, nil
	}
	extras, _ := settings.Get(ctx, s.Settings, library.ExtraExtensions)
	disc, err := importer.Discover(v.Path, extras)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, vid := range disc.Videos {
		rel := parser.ParsePath(vid.Path)
		eps := MatchRelease(v.SeriesType, vw.eps, &rel)
		var free []*Episode
		for _, e := range eps {
			if e.File == nil {
				free = append(free, e)
			}
		}
		if len(free) == 0 {
			continue
		}
		a, err := s.analyse(ctx, v, rel, filepath.Base(vid.Path))
		if err != nil {
			return n, err
		}
		f := &File{Path: vid.Path, Size: vid.Size, Quality: a.Quality.ID, Revision: rel.Revision, CustomFormats: a.Formats, Score: a.Score,
			ReleaseGroup: rel.Group, Languages: rel.Languages, MediaInfo: mediaInfo(&rel)}
		if f.Revision.Version == 0 {
			f.Revision.Version = 1
		}
		if f.Languages == nil {
			f.Languages = []string{}
		}
		err = s.DB.Tx(ctx, func(tx *sql.Tx) error {
			if err := saveFile(ctx, tx, v.ID, f, s.now()); err != nil {
				return err
			}
			for _, e := range free {
				if _, err := tx.ExecContext(ctx, `UPDATE episodes SET file_id = ? WHERE id = ?`, f.ID, e.ID); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return n, err
		}
		for _, e := range free {
			fid := f.ID
			e.FileID, e.File = &fid, f
			s.record(ctx, e.Ref(), timeline.TypeFileFound, fmt.Sprintf("found %s (%s) in the series folder", filepath.Base(vid.Path), a.Quality.Name),
				map[string]any{"path": vid.Path, "quality": a.Quality.Name}, nil)
			n++
		}
	}
	return n, nil
}

// ImportCandidate is one file of a manual import preview.
type ImportCandidate struct {
	Path        string
	Rel         string
	Size        int64
	SeriesID    int64
	SeriesTitle string
	Episodes    []Episode
	Quality     quality.Quality
	Release     parser.Release
	Rejection   *ImportError
	Sidecars    []string
}

// matchLocal finds a library series for a name.
func (s *Service) matchLocal(ctx context.Context, names ...string) (int64, error) {
	for _, n := range names {
		tvdb, tmdb, imdb := idsFromName(n)
		if tvdb > 0 || tmdb > 0 {
			if v, err := s.ByProviderID(ctx, tmdb, tvdb); err == nil {
				return v.ID, nil
			}
		}
		if imdb != "" {
			var id int64
			if err := s.DB.R.QueryRowContext(ctx, `SELECT item_id FROM series WHERE imdb_id = ?`, imdb).Scan(&id); err == nil {
				return id, nil
			}
		}
	}
	rows, err := s.DB.R.QueryContext(ctx, `SELECT i.id, i.year, i.title, s.original_title, s.alternate_titles FROM media_items i
		JOIN series s ON s.item_id = i.id`)
	if err != nil {
		return 0, err
	}
	type cand struct {
		id     int64
		year   int
		titles []string
	}
	var all []cand
	for rows.Next() {
		var (
			c                 cand
			title, orig, alts string
		)
		if err := rows.Scan(&c.id, &c.year, &title, &orig, &alts); err != nil {
			_ = rows.Close()
			return 0, err
		}
		c.titles = append([]string{title, orig}, jsonList(alts)...)
		all = append(all, c)
	}
	_ = rows.Close()
	for _, n := range names {
		p := parser.Parse(cleanName(n))
		want := normTitle(p.Title)
		if want == "" {
			continue
		}
		for _, c := range all {
			for _, t := range c.titles {
				if t != "" && normTitle(t) == want {
					return c.id, nil
				}
			}
		}
	}
	return 0, nil
}

// PreviewImport lists the video files of a folder (or one file) with the
// series and episodes each would go to and whether it would be imported.
func (s *Service) PreviewImport(ctx context.Context, folder string, seriesID int64) ([]ImportCandidate, error) {
	p, err := lfs.MustExist(s.DataRoots, folder)
	if err != nil {
		return nil, err
	}
	extras, _ := settings.Get(ctx, s.Settings, library.ExtraExtensions)
	disc, err := importer.Discover(p, extras)
	if err != nil {
		return nil, err
	}
	single := len(disc.Videos) == 1
	views := map[int64]*view{}
	out := []ImportCandidate{}
	for _, vid := range disc.Videos {
		c := ImportCandidate{Path: vid.Path, Rel: vid.Rel, Size: vid.Size, Sidecars: sidecarsFor(vid.Path, disc.Sidecars, single)}
		rel := parseFile(vid.Path, filepath.Base(p), single)
		c.Release = rel
		c.Quality = quality.Identify(&rel, quality.Series)
		sid := seriesID
		if sid == 0 {
			if sid, err = s.matchLocal(ctx, filepath.Base(vid.Path), filepath.Base(filepath.Dir(vid.Path)), filepath.Base(p)); err != nil {
				return nil, err
			}
		}
		if sid == 0 {
			c.Rejection = &ImportError{Code: RejectNoSeries, Message: "no series in the library matches the name"}
			out = append(out, c)
			continue
		}
		vw, ok := views[sid]
		if !ok {
			if vw, err = s.loadView(ctx, sid); err != nil {
				return nil, err
			}
			views[sid] = vw
		}
		c.SeriesID, c.SeriesTitle = sid, vw.series.Title
		eps := MatchRelease(vw.series.SeriesType, vw.eps, &rel)
		if IsPack(&rel) {
			eps = nil
		}
		for _, e := range eps {
			c.Episodes = append(c.Episodes, *e)
		}
		if len(eps) == 0 {
			c.Rejection = &ImportError{Code: RejectNoEpisode, Message: "no episode matches the name; choose them"}
		} else {
			a, err := s.analyse(ctx, &vw.series, rel, filepath.Base(vid.Path))
			if err != nil {
				return nil, err
			}
			for _, e := range eps {
				if e.File != nil {
					if sameFile(vid.Path, e.File.Path) {
						c.Rejection = &ImportError{Code: RejectExists, Message: "already the episode's file"}
						break
					}
					if ok, why := better(vw.profile, a, e.File); !ok {
						c.Rejection = &ImportError{Code: RejectNotUpgrade, Message: why}
						break
					}
				}
			}
		}
		out = append(out, c)
	}
	return out, nil
}

// ImportFiles imports chosen files (manual import), one result per request.
func (s *Service) ImportFiles(ctx context.Context, reqs []ImportRequest) ([]ImportResult, []error) {
	res := make([]ImportResult, len(reqs))
	errs := make([]error, len(reqs))
	for i, r := range reqs {
		res[i], errs[i] = s.ImportFile(ctx, r)
	}
	return res, errs
}

// RenameItem is one planned or done rename.
type RenameItem struct {
	SeriesID int64
	Title    string
	From     string
	To       string
}

func (s *Service) seriesIDs(ctx context.Context, ids []int64) ([]int64, error) {
	if len(ids) > 0 {
		return ids, nil
	}
	rows, err := s.DB.R.QueryContext(ctx, `SELECT item_id FROM series`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// RenamePreview shows which episode files a rename would change (within
// each series folder; the season folder may change).
func (s *Service) RenamePreview(ctx context.Context, ids []int64) ([]RenameItem, error) {
	ids, err := s.seriesIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	out := []RenameItem{}
	for _, id := range ids {
		vw, err := s.loadView(ctx, id)
		if errors.Is(err, ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		byFile := map[int64][]*Episode{}
		files := map[int64]*File{}
		var order []int64
		for i := range vw.eps {
			e := &vw.eps[i]
			if e.File == nil {
				continue
			}
			if _, ok := byFile[e.File.ID]; !ok {
				order = append(order, e.File.ID)
			}
			byFile[e.File.ID] = append(byFile[e.File.ID], e)
			files[e.File.ID] = e.File
		}
		for _, fid := range order {
			f := files[fid]
			rel := parser.ParsePath(f.Path)
			if f.SceneName != "" {
				rel = parseFile(f.Path, f.SceneName, true)
			}
			q, _ := quality.ByID(f.Quality)
			a := analysis{Release: rel, Quality: q, Formats: f.CustomFormats, Score: f.Score}
			folder, name, err := s.destination(ctx, &vw.series, byFile[fid], &a, f.Path)
			if err != nil {
				return nil, err
			}
			to := filepath.Join(folder, naming.WithExtension(name, filepath.Ext(f.Path)))
			if to != f.Path {
				out = append(out, RenameItem{SeriesID: id, Title: vw.series.Title, From: f.Path, To: to})
			}
		}
	}
	return out, nil
}

// Rename applies the episode templates to files already in the library.
// Sidecars named after a file move with it; nothing is ever overwritten.
func (s *Service) Rename(ctx context.Context, ids []int64) ([]RenameItem, error) {
	plan, err := s.RenamePreview(ctx, ids)
	if err != nil {
		return nil, err
	}
	done := []RenameItem{}
	for _, r := range plan {
		v, err := s.Get(ctx, r.SeriesID)
		if err != nil {
			continue
		}
		to, err := lfs.SafePath(s.DataRoots, r.To)
		if err != nil {
			return done, err
		}
		if rel, err := filepath.Rel(v.Path, to); err != nil || strings.HasPrefix(rel, "..") {
			return done, invalid("rename target %s leaves the series folder", to)
		}
		if _, err := os.Lstat(to); err == nil {
			continue // never over another file
		}
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil { // #nosec G301 -- library folders are read by media servers
			return done, err
		}
		if err := os.Rename(r.From, to); err != nil {
			return done, err
		}
		oldBase := strings.TrimSuffix(filepath.Base(r.From), filepath.Ext(r.From))
		newBase := strings.TrimSuffix(filepath.Base(to), filepath.Ext(to))
		if entries, err := os.ReadDir(filepath.Dir(r.From)); err == nil {
			for _, e := range entries {
				if !e.IsDir() && strings.HasPrefix(e.Name(), oldBase+".") {
					dst := filepath.Join(filepath.Dir(to), newBase+e.Name()[len(oldBase):])
					if _, err := os.Lstat(dst); err != nil {
						_ = os.Rename(filepath.Join(filepath.Dir(r.From), e.Name()), dst)
					}
				}
			}
		}
		if _, err := s.DB.W.ExecContext(ctx, `UPDATE media_files SET path = ? WHERE path = ?`, to, r.From); err != nil {
			return done, err
		}
		var epIDs []int64
		rows, err := s.DB.R.QueryContext(ctx, `SELECT e.id FROM episodes e JOIN media_files f ON f.id = e.file_id WHERE f.path = ?`, to)
		if err == nil {
			for rows.Next() {
				var id int64
				if rows.Scan(&id) == nil {
					epIDs = append(epIDs, id)
				}
			}
			_ = rows.Close()
		}
		for _, id := range epIDs {
			s.record(ctx, library.EpisodeRef(id), timeline.TypeRenamed, fmt.Sprintf("renamed %s to %s", filepath.Base(r.From), filepath.Base(to)),
				map[string]any{"from": r.From, "to": to}, nil)
		}
		done = append(done, RenameItem{SeriesID: r.SeriesID, Title: r.Title, From: r.From, To: to})
		if len(epIDs) > 0 {
			notifyChange(ctx, s.Notifier, mediaserver.Change{ItemRef: library.EpisodeRef(epIDs[0]).String(), Kind: "episode", Action: "renamed",
				Title: v.Title, Year: v.Year, Paths: []string{to}})
		}
	}
	return done, nil
}
