package movies

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/mmzlopasa/locarr/server/internal/customformats"
	"github.com/mmzlopasa/locarr/server/internal/downloads"
	lfs "github.com/mmzlopasa/locarr/server/internal/fs"
	"github.com/mmzlopasa/locarr/server/internal/library"
	"github.com/mmzlopasa/locarr/server/internal/library/importer"
	"github.com/mmzlopasa/locarr/server/internal/library/naming"
	"github.com/mmzlopasa/locarr/server/internal/mediaserver"
	"github.com/mmzlopasa/locarr/server/internal/parser"
	"github.com/mmzlopasa/locarr/server/internal/quality"
	"github.com/mmzlopasa/locarr/server/internal/settings"
	"github.com/mmzlopasa/locarr/server/internal/timeline"
)

// Import rejection codes.
const (
	RejectNotUpgrade = "not_an_upgrade"
	RejectNoVideo    = "no_video_file"
	RejectDisc       = "disc_structure"
	RejectNoMovie    = "no_matching_movie"
	RejectExists     = "already_imported"
)

// ImportError is an import that cannot go ahead, with a stable code.
type ImportError struct {
	Code    string
	Message string
}

func (e *ImportError) Error() string { return e.Message }

func rejectf(code, format string, a ...any) error {
	return &ImportError{Code: code, Message: fmt.Sprintf(format, a...)}
}

// analysis is what Locarr learns about a file before importing it.
type analysis struct {
	Release parser.Release
	Quality quality.Quality
	Formats []string
	Score   int
}

func (s *Service) analyse(ctx context.Context, m *Movie, path, releaseTitle string) (analysis, error) {
	rel := parser.ParsePath(path)
	if releaseTitle != "" {
		named := parser.Parse(releaseTitle)
		// The release name usually says more than the file name.
		if named.Confidence >= rel.Confidence || rel.Obfuscated {
			named.Extension = rel.Extension
			rel = named
		}
	}
	a := analysis{Release: rel, Quality: quality.Identify(&rel, quality.Movies)}
	p, err := s.Quality.Profile(ctx, m.QualityProfileID)
	if err != nil {
		return a, err
	}
	list, err := s.Formats.List(ctx)
	if err != nil {
		return a, err
	}
	compiled, _ := customformats.CompileAll(list)
	title := releaseTitle
	if title == "" {
		title = filepath.Base(path)
	}
	matches, score := customformats.Score(compiled, &p, customformats.Input{Title: title, Release: &rel, Quality: a.Quality,
		OriginalLanguage: m.OriginalLanguage})
	for _, mt := range matches {
		a.Formats = append(a.Formats, mt.Name)
	}
	a.Score = score
	return a, nil
}

// isUpgrade reports whether a beats the movie's current file under its profile.
func (s *Service) isUpgrade(ctx context.Context, m *Movie, a analysis) (bool, string, error) {
	if m.File == nil {
		return true, "", nil
	}
	p, err := s.Quality.Profile(ctx, m.QualityProfileID)
	if err != nil {
		return false, "", err
	}
	p.Normalize()
	newPos, _ := p.Position(a.Quality.ID)
	oldPos, _ := p.Position(m.File.Quality)
	if newPos < 0 {
		newPos = len(p.Items)
	}
	if oldPos < 0 {
		oldPos = len(p.Items)
	}
	switch {
	case newPos < oldPos:
		return true, "", nil
	case newPos > oldPos:
		return false, fmt.Sprintf("%s is not better than the current %s", a.Quality.Name, qualityName(m.File.Quality)), nil
	case a.Release.Revision.Version > max(m.File.Revision.Version, 1):
		return true, "", nil
	case a.Score > m.File.Score:
		return true, "", nil
	}
	return false, fmt.Sprintf("same quality (%s) and custom format score (%d) as the current file", a.Quality.Name, a.Score), nil
}

func qualityName(id string) string {
	if q, ok := quality.ByID(id); ok {
		return q.Name
	}
	return id
}

// fileName renders the movie file name (without extension).
func (s *Service) fileName(ctx context.Context, m *Movie, a analysis, original string) (string, error) {
	rename, err := settings.Get(ctx, s.Settings, library.RenameMovies)
	if err != nil {
		return "", err
	}
	if !rename {
		return strings.TrimSuffix(filepath.Base(original), filepath.Ext(original)), nil
	}
	tmpl, err := settings.Get(ctx, s.Settings, library.MovieFileFormat)
	if err != nil {
		return "", err
	}
	colon, _ := settings.Get(ctx, s.Settings, library.ColonReplacement)
	rel := a.Release
	return naming.Render(tmpl, naming.MovieValues(s.info(m), &naming.FileInfo{Release: &rel, Quality: a.Quality, CustomFormats: a.Formats}),
		naming.Options{Colon: colon})
}

func mediaInfo(r *parser.Release) MediaInfo {
	mi := MediaInfo{Source: "name", Resolution: r.Video.Resolution, VideoCodec: r.Video.Codec, BitDepth: r.Video.BitDepth, HDR: r.Video.HDR,
		AudioChannels: r.Audio.Channels, Atmos: r.Audio.Atmos, Languages: r.Languages, Subtitles: r.SubtitleLanguages}
	if r.Video.Encoder != "" {
		mi.VideoCodec = r.Video.Encoder
	}
	if len(r.Audio.Codecs) > 0 {
		mi.AudioCodec = r.Audio.Codecs[0]
	}
	return mi
}

// ImportRequest imports one file into one movie.
type ImportRequest struct {
	MovieID int64
	Path    string
	// ReleaseTitle helps identify quality ("" = from the file name).
	ReleaseTitle string
	// Mode overrides the transfer mode ("" = the setting).
	Mode     string
	Sidecars []string
	// DownloadID links the file to the download it came from.
	DownloadID *int64
}

// ImportResult says what happened to one file.
type ImportResult struct {
	MovieID  int64
	Source   string
	Path     string
	Mode     string
	Upgraded bool
	Recycled []string
	Sidecars []string
}

func (s *Service) transferMode(ctx context.Context, requested string) (string, error) {
	if requested != "" {
		switch requested {
		case library.TransferHardlink, library.TransferCopy, library.TransferMove:
			return requested, nil
		}
		return "", invalid("unknown transfer mode %q", requested)
	}
	return settings.Get(ctx, s.Settings, library.TransferMode)
}

func (s *Service) perms(ctx context.Context) importer.Permissions {
	on, _ := settings.Get(ctx, s.Settings, library.SetPermissions)
	if !on {
		return importer.Permissions{}
	}
	f, _ := settings.Get(ctx, s.Settings, library.FileMode)
	d, _ := settings.Get(ctx, s.Settings, library.FolderMode)
	return importer.Permissions{File: importer.ParseMode(f), Folder: importer.ParseMode(d)}
}

// ImportFile imports one file (and its sidecars) into a movie: checks it is
// an upgrade, transfers it (hardlink when possible), verifies it, renames it,
// moves a replaced file to the recycle bin and records every step.
func (s *Service) ImportFile(ctx context.Context, req ImportRequest) (ImportResult, error) {
	m, err := s.Get(ctx, req.MovieID)
	if err != nil {
		return ImportResult{}, err
	}
	src, err := lfs.MustExist(s.DataRoots, req.Path)
	if err != nil {
		return ImportResult{}, err
	}
	a, err := s.analyse(ctx, &m, src, req.ReleaseTitle)
	if err != nil {
		return ImportResult{}, err
	}
	if m.File != nil {
		if same, _ := sameFile(src, m.File.Path); same {
			return ImportResult{}, rejectf(RejectExists, "%s is already the movie's file", src)
		}
	}
	up, why, err := s.isUpgrade(ctx, &m, a)
	if err != nil {
		return ImportResult{}, err
	}
	if !up {
		return ImportResult{}, rejectf(RejectNotUpgrade, "not imported: %s", why)
	}
	mode, err := s.transferMode(ctx, req.Mode)
	if err != nil {
		return ImportResult{}, err
	}
	name, err := s.fileName(ctx, &m, a, src)
	if err != nil {
		return ImportResult{}, err
	}
	res := ImportResult{MovieID: m.ID, Source: src}
	ref := m.Ref().String()
	old := m.File
	replace := func(p string) error {
		e, err := s.Recycle.Recycle(ctx, p, ref, "replaced by an upgrade")
		if err != nil {
			return err
		}
		res.Recycled = append(res.Recycled, e.RecycledPath)
		return nil
	}
	main, sides, err := importer.Place(importer.Placement{Roots: s.DataRoots, Source: src, Sidecars: req.Sidecars, Folder: m.Path,
		Name: name, Mode: mode, Perms: s.perms(ctx), Replace: replace})
	if err != nil {
		return res, err
	}
	res.Path, res.Mode = main.Path, main.Mode
	for _, sd := range sides {
		res.Sidecars = append(res.Sidecars, sd.Path)
	}
	// An old file under another name goes to the recycle bin too (never deleted).
	if old != nil && old.Path != main.Path {
		if _, err := os.Stat(old.Path); err == nil {
			if err := replace(old.Path); err != nil {
				s.record(ctx, &m, timeline.TypeImportFailed, "imported the new file but could not move the old one to the recycle bin: "+err.Error(),
					map[string]any{"path": old.Path}, req.DownloadID)
			}
		}
	}
	rel := a.Release
	f := &File{Path: main.Path, Size: main.Size, Quality: a.Quality.ID, Revision: rel.Revision, CustomFormats: a.Formats, Score: a.Score,
		ReleaseGroup: rel.Group, Edition: strings.Join(rel.Editions, " "), Languages: rel.Languages, MediaInfo: mediaInfo(&rel),
		SceneName: req.ReleaseTitle, DownloadID: req.DownloadID}
	if f.Revision.Version == 0 {
		f.Revision.Version = 1
	}
	if f.CustomFormats == nil {
		f.CustomFormats = []string{}
	}
	if err := s.DB.Tx(ctx, func(tx *sql.Tx) error { return saveFile(ctx, tx, m.ID, f, s.now()) }); err != nil {
		return res, err
	}
	res.Upgraded = old != nil
	data := map[string]any{"source": src, "path": main.Path, "mode": main.Mode, "quality": a.Quality.Name, "score": a.Score,
		"customFormats": a.Formats, "size": main.Size, "sidecars": res.Sidecars}
	s.record(ctx, &m, timeline.TypeFileImported, fmt.Sprintf("imported %s (%s, %s)", filepath.Base(main.Path), a.Quality.Name, main.Mode), data, req.DownloadID)
	if filepath.Base(src) != filepath.Base(main.Path) {
		s.record(ctx, &m, timeline.TypeRenamed, fmt.Sprintf("renamed %s to %s", filepath.Base(src), filepath.Base(main.Path)),
			map[string]any{"from": filepath.Base(src), "to": filepath.Base(main.Path)}, req.DownloadID)
	}
	if old != nil {
		s.record(ctx, &m, timeline.TypeUpgraded, fmt.Sprintf("upgraded from %s to %s", qualityName(old.Quality), a.Quality.Name),
			map[string]any{"from": qualityName(old.Quality), "to": a.Quality.Name, "oldPath": old.Path, "recycled": res.Recycled}, req.DownloadID)
	}
	for _, r := range res.Recycled {
		s.record(ctx, &m, timeline.TypeRecycled, "moved the replaced file to the recycle bin: "+r, map[string]any{"path": r}, req.DownloadID)
	}
	s.notify(ctx, &m, map[bool]string{true: "upgraded", false: "imported"}[old != nil], main.Path)
	return res, nil
}

func (s *Service) notify(ctx context.Context, m *Movie, action, path string) {
	if s.Notifier == nil || s.Notifier.Name() == "" {
		return
	}
	err := s.Notifier.Notify(ctx, mediaserver.Change{ItemRef: m.Ref().String(), Kind: "movie", Action: action, Title: m.Title, Year: m.Year,
		Paths: []string{path}, IDs: map[string]string{"tmdb": fmt.Sprint(m.TMDbID), "imdb": m.IMDbID}})
	if err == nil && mediaserver.RecordsOutcome(s.Notifier) {
		return
	}
	msg := "notified " + s.Notifier.Name()
	if err != nil {
		msg = "could not notify " + s.Notifier.Name() + ": " + err.Error()
	}
	s.record(ctx, m, timeline.TypeNotified, msg, map[string]any{"server": s.Notifier.Name(), "ok": err == nil}, nil)
}

func sameFile(a, b string) (bool, error) {
	sa, err := os.Stat(a)
	if err != nil {
		return false, err
	}
	sb, err := os.Stat(b)
	if err != nil {
		return false, err
	}
	return os.SameFile(sa, sb), nil
}

// ImportDownload imports a completed download that was grabbed for a movie.
// It always ends with MarkImported or MarkImportFailed.
func (s *Service) ImportDownload(ctx context.Context, downloadID int64) error {
	d, err := s.Downloads.Get(ctx, downloadID)
	if err != nil {
		return err
	}
	if d.Status != downloads.StatusCompleted {
		return nil // imported, removed or failed meanwhile
	}
	ref := library.RefFromJSON(d.ItemRef)
	fail := func(m *Movie, err error) error {
		msg := err.Error()
		if m != nil {
			s.record(ctx, m, timeline.TypeImportFailed, msg, map[string]any{"download": d.Title, "path": d.LocalPath}, &d.ID)
		}
		return s.Downloads.MarkImportFailed(ctx, d.ID, msg)
	}
	if ref.Kind != library.RefMovie {
		return fail(nil, rejectf(RejectNoMovie, "the download is not linked to a movie"))
	}
	m, err := s.Get(ctx, ref.ID)
	if errors.Is(err, ErrNotFound) {
		return fail(nil, rejectf(RejectNoMovie, "the movie was removed from the library"))
	}
	if err != nil {
		return err
	}
	extras, _ := settings.Get(ctx, s.Settings, library.ExtraExtensions)
	disc, err := importer.Discover(d.LocalPath, extras)
	if err != nil {
		return fail(&m, fmt.Errorf("cannot read %s: %w", d.LocalPath, err))
	}
	main, ok := disc.MainVideo()
	if !ok {
		if disc.Disc {
			return fail(&m, rejectf(RejectDisc, "%s is a disc structure (BDMV, VIDEO_TS or an image); import it manually", d.LocalPath))
		}
		return fail(&m, rejectf(RejectNoVideo, "no video file in %s (%d samples or extras skipped)", d.LocalPath, len(disc.Skipped)))
	}
	var sidecars []string
	for _, sc := range disc.Sidecars {
		sidecars = append(sidecars, sc.Path)
	}
	res, err := s.ImportFile(ctx, ImportRequest{MovieID: m.ID, Path: main.Path, ReleaseTitle: d.Title, Sidecars: sidecars, DownloadID: &d.ID})
	if err != nil {
		return fail(&m, err)
	}
	return s.Downloads.MarkImported(ctx, d.ID, fmt.Sprintf("imported to %s (%s)", res.Path, res.Mode))
}
